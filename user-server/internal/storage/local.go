package storage

import (
	"context"
	"fmt"
	"io"
	"mime/multipart"
	"os"
	"path/filepath"
	"strings"
	"time"

	"hivemtk-user/internal/pkg/tenant"

	"github.com/google/uuid"
)

// LocalDriver 本地文件存储实现 —— 私有化部署零云依赖。
//
// 目录结构：{baseDir}/{folder}/{yyyy}/{mm}/{uuid}.{ext}
// 访问 URL：/files/{folder}/{yyyy}/{mm}/{uuid}.{ext}
type LocalDriver struct {
	baseDir       string
	publicBaseURL string
}

// NewLocalDriver 创建本地存储驱动
func NewLocalDriver(baseDir, publicBaseURL string) *LocalDriver {
	if baseDir == "" {
		baseDir = "./uploads"
	}
	if publicBaseURL == "" {
		publicBaseURL = "/files"
	}

	publicBaseURL = strings.TrimRight(publicBaseURL, "/")

	baseDir = filepath.Clean(baseDir)
	return &LocalDriver{baseDir: baseDir, publicBaseURL: publicBaseURL}
}

func (d *LocalDriver) ensureDir(dir string) error {
	return os.MkdirAll(dir, 0o750)
}

// TenantPathPrefix 租户路径前缀（T011）。
//
//   - off 模式恒为空串（键形/路径铁律：与现状逐字节一致）
//   - on 模式且 ctx 绑定租户 → "tenant_{id}/"（路径自证租户）
//   - on 模式无租户 ctx → 空串（系统级上传，落入存量根域）
//
// **导出**的原因：知识库导入（`aiagent/knowledge/service` 的 4 处）不经 LocalDriver、
// 自己 os.Create 落盘，它们必须用同一份前缀函数——各写一遍就是"两处实现、
// 其中一处漏分域"（A7 就是这么漏的：分域只做到了走驱动的上传口）。
func TenantPathPrefix(ctx context.Context) string {
	if ctx == nil || !tenant.IsMultiTenant() {
		return ""
	}
	id, ok := tenant.FromContext(ctx)
	if !ok {
		return ""
	}
	return fmt.Sprintf("tenant_%d/", id)
}

// tenantPathPrefix 租户路径前缀（方法形态，供驱动内部调用；实现即 TenantPathPrefix）。
func (d *LocalDriver) tenantPathPrefix(ctx context.Context) string {
	return TenantPathPrefix(ctx)
}

// dangerousWebExts "浏览器同源直接执行/渲染"的扩展名集合。
//
// 本地存储目录经 /files 以应用主站同源发布（middleware.FilesGuard），
// 落盘 .html/.svg/.js 等即构成存储型 XSS。两类来源曾实测可达：
//   - 素材库上传（content/service/material.go）不校验扩展名，文件名整体透传；
//   - WhatsApp 入站媒体（service/channel_media.go）文件名 hint 来自远程消息、
//     扩展名可由 HTML 内容嗅探（http.DetectContentType → text/html → .html）推出。
//
// 落盘时一律中和为 .bin（保留下载语义、放弃执行位）；对历史已落盘文件由
// /files 服务层守卫兜底（同一清单经 IsDangerousWebExt 复用）。
var dangerousWebExts = map[string]bool{
	".html": true, ".htm": true, ".htmls": true, ".xhtml": true, ".xht": true,
	".svg": true, ".svgz": true, ".js": true, ".mjs": true, ".xml": true,
}

// IsDangerousWebExt 供服务层（/files 守卫）与存储层共用同一判定清单。
func IsDangerousWebExt(ext string) bool {
	return dangerousWebExts[strings.ToLower(ext)]
}

func (d *LocalDriver) generatePath(folder, originalFilename string) string {
	ext := strings.ToLower(filepath.Ext(originalFilename))
	if ext == "" {
		ext = ".bin"
	}
	if dangerousWebExts[ext] {
		ext = ".bin"
	}
	now := time.Now()
	return filepath.Join(folder, now.Format("2006"), now.Format("01"), uuid.New().String()+ext)
}

// UploadMultipart 从 multipart.File 上传，返回 (publicURL, storagePath, error)
func (d *LocalDriver) UploadMultipart(ctx context.Context, file multipart.File, header *multipart.FileHeader, folder string) (string, string, error) {
	_ = ctx
	if file == nil || header == nil {
		return "", "", fmt.Errorf("file and header must not be nil")
	}
	return d.UploadReader(ctx, file, header.Size, folder, header.Filename)
}

// UploadReader 从 io.Reader 上传，返回 (publicURL, storagePath, error)
// 写入采用 os.CreateTemp + os.Rename 原子方式，避免半写入文件被读到。
//
// 租户分域（specs/001-saas-multi-tenant T011）：
//   - off 模式：路径与旧版本逐字节一致（无租户段，存量文件零迁移）
//   - on 模式 + ctx 携带租户：storagePath 前置 "tenant_{id}/"——路径自证租户，
//     /files 公开服务零改动（URL 不可枚举 UUID 即读取防线，与现状同级）
func (d *LocalDriver) UploadReader(ctx context.Context, reader io.Reader, size int64, folder string, filename string) (string, string, error) {
	if reader == nil {
		return "", "", fmt.Errorf("reader must not be nil")
	}

	storagePath := d.tenantPathPrefix(ctx) + d.generatePath(folder, filename)
	fullPath := filepath.Join(d.baseDir, storagePath)

	if err := d.ensureDir(filepath.Dir(fullPath)); err != nil {
		return "", "", fmt.Errorf("create upload dir failed: %w", err)
	}

	tmpPath := fullPath + ".tmp"
	tmpFile, err := os.OpenFile(tmpPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o640)
	if err != nil {
		return "", "", fmt.Errorf("create tmp file failed: %w", err)
	}

	if _, err := io.Copy(tmpFile, reader); err != nil {
		_ = tmpFile.Close()
		_ = os.Remove(tmpPath)
		return "", "", fmt.Errorf("write file failed: %w", err)
	}
	if err := tmpFile.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return "", "", fmt.Errorf("close tmp file failed: %w", err)
	}

	if err := os.Rename(tmpPath, fullPath); err != nil {
		_ = os.Remove(tmpPath)
		return "", "", fmt.Errorf("rename tmp failed: %w", err)
	}

	publicURL := d.PublicURL(storagePath)
	return publicURL, storagePath, nil
}

// resolve 把存储相对路径安全地拼到 baseDir 之下，拒绝任何穿越 baseDir 的结果。
// 当前调用方都只传驱动自身生成的 storagePath，此守卫是 safe-by-construction 兜底。
func (d *LocalDriver) resolve(storagePath string) (string, error) {
	full := filepath.Join(d.baseDir, storagePath)
	rel, err := filepath.Rel(d.baseDir, full)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return "", fmt.Errorf("invalid storage path: %q", storagePath)
	}
	return full, nil
}

// Download 读取本地文件
func (d *LocalDriver) Download(ctx context.Context, storagePath string) (io.ReadCloser, error) {
	_ = ctx
	fullPath, err := d.resolve(storagePath)
	if err != nil {
		return nil, err
	}
	f, err := os.Open(fullPath)
	if err != nil {
		return nil, fmt.Errorf("open file failed: %w", err)
	}
	return f, nil
}

// Delete 删除本地文件（同时清理空目录链，最佳努力）
func (d *LocalDriver) Delete(ctx context.Context, storagePath string) error {
	_ = ctx
	fullPath, err := d.resolve(storagePath)
	if err != nil {
		return err
	}
	if err := os.Remove(fullPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("delete file failed: %w", err)
	}
	return nil
}

// PublicURL 生成公开访问 URL。
//
// off 模式：`/files/{folder}/{yyyy}/{mm}/{uuid}.{ext}` —— 与历史逐字节一致
// （离线单机不签名、URL 里本来就没有租户段）。
//
// on 模式：`/files/{folder}/{yyyy}/{mm}/{uuid}.{ext}?s=<token>` —— **租户段从 URL 里
// 消失**（A12），租户归属与过期时间搬进签名 token（A1）。判据与 token 形态见
// internal/storage/fileurl.go 文件头。路径不带 `tenant_{id}/` 段时（系统级上传、
// 存量根域文件）保持原样：这些行本来就不分租户，签名只会把它们变成"看起来像
// 有归属"的样子。
func (d *LocalDriver) PublicURL(storagePath string) string {
	cleaned := strings.TrimPrefix(filepath.ToSlash(storagePath), "/")
	if rel, sig, ok := SignStoredPath(cleaned); ok {
		return d.publicBaseURL + "/" + rel + "?s=" + sig
	}
	return d.publicBaseURL + "/" + filepath.ToSlash(storagePath)
}

// SignUploadURL 本地存储无法生成预签名 URL —— 私有化部署用服务端中转即可。
// 这里返回一个占位实现：告诉调用方直接用 UploadReader。
func (d *LocalDriver) SignUploadURL(ctx context.Context, folder, filename, contentType string, expire time.Duration) (string, string, error) {
	_ = ctx
	_ = expire
	return "", "", fmt.Errorf("local storage does not support pre-signed URL; use server-side UploadReader instead")
}

// Exists 检查文件是否存在
func (d *LocalDriver) Exists(ctx context.Context, storagePath string) (bool, error) {
	_ = ctx
	fullPath, err := d.resolve(storagePath)
	if err != nil {
		return false, err
	}
	_, err = os.Stat(fullPath)
	if err == nil {
		return true, nil
	}
	if os.IsNotExist(err) {
		return false, nil
	}
	return false, err
}

// Type 返回驱动类型
func (d *LocalDriver) Type() string { return "local" }

// BaseDir 返回本地文件根目录（供迁移/备份工具使用）
func (d *LocalDriver) BaseDir() string { return d.baseDir }
