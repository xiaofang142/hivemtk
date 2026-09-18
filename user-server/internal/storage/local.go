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

func (d *LocalDriver) generatePath(folder, originalFilename string) string {
	ext := strings.ToLower(filepath.Ext(originalFilename))
	if ext == "" {
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
func (d *LocalDriver) UploadReader(ctx context.Context, reader io.Reader, size int64, folder string, filename string) (string, string, error) {
	_ = ctx
	if reader == nil {
		return "", "", fmt.Errorf("reader must not be nil")
	}

	storagePath := d.generatePath(folder, filename)
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

// PublicURL 生成公开访问 URL：/files/{folder}/{yyyy}/{mm}/{uuid}.{ext}
func (d *LocalDriver) PublicURL(storagePath string) string {

	cleaned := filepath.ToSlash(storagePath)
	return d.publicBaseURL + "/" + cleaned
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
