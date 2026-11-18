package service

import (
	"context"
	"crypto/hmac"
	"crypto/sha1"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"path"
	"strings"
	"time"
	"unicode"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/repository"

	"gorm.io/gorm"
)

// 访客端七牛直传凭证（SaaS 配置面改造 B1，2026-11-17）。
//
// 凭据唯一来源是租户级 obs_config 默认行（管理端「对象存储」页，AK/SK 加密列）。
// 此前的双源——config.yaml `storage.qiniu` 节与 QINIU_* 环境变量——全部退役：
// SaaS 多租户下"哪个租户的文件落到哪个 bucket"是租户属性，不是部署属性，
// env/yaml 全局单值既装不下多租户，也会在租户间串桶。
//
// fail-closed：无默认行 / 非 active / 非 qiniu / 缺 AK/SK 一律报错，
// 不回落任何本地凭据（与 chat_public.go 旧 503「对象存储未配置」同语义）。

// QiniuUploadToken 七牛直传凭证束。
type QiniuUploadToken struct {
	UploadURL string // POST 表单上传地址
	Token     string // 上传凭证（AK:sign:policy）
	PublicURL string // 上传完成后的公开访问 URL
	ExpiresIn int    // 凭证有效期（秒）
}

// qiniuUploadHostByRegion 七牛上传域名按 Region 推导（z0 华东 / z1 华北 / z2 华南 /
// na0 北美 / as0 东南亚）。未配 Region 时沿用改造前的默认 up-z2.qiniup.com，行为不变。
func qiniuUploadHostByRegion(region string) string {
	switch strings.ToLower(strings.TrimSpace(region)) {
	case "z0":
		return "up.qiniup.com"
	case "z1":
		return "up-z1.qiniup.com"
	case "na0":
		return "up-na0.qiniup.com"
	case "as0":
		return "up-as0.qiniup.com"
	default:
		// 空 Region 与历史 z2 部署（无 Region 字段的存量行）都落这里。
		return "up-z2.qiniup.com"
	}
}

// BuildQiniuUploadToken 为指定对象 key 签发七牛上传凭证。
//
// objectKey 由调用方（controller）按 chat/yyyy/MM/<uuid>.ext 规则生成；
// maxSizeBytes 写进 policy.fsizeLimit；ttl 为凭证有效期。
func BuildQiniuUploadToken(ctx context.Context, objectKey string, maxSizeBytes int64, ttl time.Duration) (*QiniuUploadToken, error) {
	if ctx == nil || tenant.IsSystemScope(ctx) {
		return nil, errors.New("上传签发需要明确租户上下文")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	tenantID := tenant.WriteTenantID(ctx)
	if tenantID == 0 {
		return nil, errors.New("上传签发需要已确认的租户")
	}
	// 镜像访客接口的既有20MiB/1小时许可，不从配置扩大签发范围。
	if maxSizeBytes <= 0 || maxSizeBytes > 20<<20 || ttl < time.Second || ttl > time.Hour {
		return nil, errors.New("上传大小或有效期超出许可范围")
	}
	if !validUploadObjectKey(objectKey) {
		return nil, errors.New("上传对象路径不合法")
	}
	ctx = tenant.OnlyTenant(ctx, tenantID)
	cfg, err := repository.NewObsUploadConfigRepository().GetDefaultForUpload(ctx)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errors.New("对象存储未配置（请在管理端「系统设置 → 对象存储」页设置默认配置）")
		}
		return nil, fmt.Errorf("读取对象存储配置失败: %w", err)
	}
	if cfg == nil {
		return nil, errors.New("对象存储配置未确认")
	}
	if cfg.Provider != model.ObsProviderQiniu {
		return nil, fmt.Errorf("访客端直传仅支持七牛云存储，当前默认存储为 %s", cfg.Provider)
	}
	if strings.TrimSpace(cfg.AccessKey) == "" || strings.TrimSpace(cfg.SecretKey) == "" || strings.TrimSpace(cfg.Bucket) == "" {
		return nil, errors.New("七牛存储配置不完整（AccessKey/SecretKey/Bucket 必填）")
	}
	publicURL, err := uploadPublicURL(cfg.Domain, objectKey)
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	now := time.Now()
	policy := map[string]any{
		"scope":    cfg.Bucket + ":" + objectKey,
		"deadline": now.Add(ttl).Unix(),
		// insertOnly（2026-11-15 审计 L9）：scope=bucket:key 默认语义是「插入或覆盖」。
		// key 为服务端生成的不可预知 UUID，覆盖风险可忽略，但仍堵死语义面。
		// （2026-11-18 变基并入 service 版时从 chat_public 内联版恢复。）
		"insertOnly": 1,
		"fsizeLimit": maxSizeBytes,
		"returnBody": `{"key":"$(key)","hash":"$(etag)","fsize":$(fsize),"fname":"$(fname)"}`,
	}
	policyBytes, err := json.Marshal(policy)
	if err != nil {
		return nil, fmt.Errorf("生成上传策略失败: %w", err)
	}
	policyEncoded := base64.URLEncoding.EncodeToString(policyBytes)

	mac := hmac.New(sha1.New, []byte(cfg.SecretKey))
	_, _ = mac.Write([]byte(policyEncoded))
	signature := base64.URLEncoding.EncodeToString(mac.Sum(nil))

	return &QiniuUploadToken{
		UploadURL: "https://" + qiniuUploadHostByRegion(cfg.Region),
		Token:     cfg.AccessKey + ":" + signature + ":" + policyEncoded,
		PublicURL: publicURL,
		ExpiresIn: int(ttl.Seconds()),
	}, nil
}

func validUploadObjectKey(key string) bool {
	if key == "" || strings.HasPrefix(key, "/") || path.Clean(key) != key || key == "." ||
		strings.ContainsAny(key, "\\?#%:") || strings.IndexFunc(key, func(r rune) bool {
		return unicode.IsSpace(r) || unicode.IsControl(r)
	}) >= 0 {
		return false
	}
	for _, segment := range strings.Split(key, "/") {
		if segment == "" || segment == "." || segment == ".." {
			return false
		}
	}
	return true
}

func uploadPublicURL(domain, key string) (string, error) {
	if domain == "" || strings.TrimSpace(domain) != domain || strings.ContainsAny(domain, "\\?#") ||
		strings.IndexFunc(domain, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) }) >= 0 {
		return "", errors.New("对象存储公开地址不合法")
	}
	if !strings.Contains(domain, "://") {
		domain = "https://" + domain
	}
	u, err := url.Parse(domain)
	if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.Opaque != "" ||
		u.RawQuery != "" || u.Fragment != "" {
		return "", errors.New("对象存储公开地址必须是无凭据的HTTPS地址")
	}
	u.Path = strings.TrimRight(u.Path, "/") + "/" + key
	u.RawPath = ""
	return u.String(), nil
}
