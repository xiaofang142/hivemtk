package storage

import (
	"context"
	"errors"
	"fmt"
	"hivemtk-user/internal/model"
	"io"
	"mime/multipart"
	"os"
	"strings"
	"time"
)

// ErrInvalidEndpoint local Endpoint 形态非法（含 URL/协议头/网络路径）。
var ErrInvalidEndpoint = errors.New("invalid local endpoint")

// ValidateLocalEndpoint 守 local Endpoint 必须是本地目录路径，不能是 URL。
// 背景（AUD-01）：曾经把 https://... 直接当 baseDir，在磁盘建成 "https:/host/..."
// 目录并往里面写文件；配置面与构造面都要 fail-closed。空值合法（由调用方走
// 环境变量/默认值），其余含 "://" 或 "//" 开头一律拒绝。
func ValidateLocalEndpoint(endpoint string) error {
	ep := strings.TrimSpace(endpoint)
	if ep == "" {
		return nil
	}
	if strings.Contains(ep, "://") || strings.HasPrefix(ep, "//") {
		return fmt.Errorf("%w: %q 是 URL/网络路径形态，local 存储只接受本地目录路径", ErrInvalidEndpoint, ep)
	}
	return nil
}

// Factory 从 ObsConfig 构造对应 Driver
func Factory(cfg *model.ObsConfig) (Driver, error) {
	if cfg == nil {
		return nil, ErrNoDefaultStorage
	}
	switch cfg.Provider {
	case model.ObsProviderLocal:
		return newLocalFromConfig(cfg)
	case model.ObsProviderAliyun, model.ObsProviderTencent, model.ObsProviderQiniu, model.ObsProviderAWS:

		return newCloudStub(cfg), nil
	default:
		return nil, fmt.Errorf("%w: %s", ErrUnsupportedProvider, cfg.Provider)
	}
}

func newLocalFromConfig(cfg *model.ObsConfig) (Driver, error) {
	baseDir := strings.TrimSpace(cfg.Endpoint)
	if baseDir == "" {
		baseDir = os.Getenv("STORAGE_LOCAL_BASE_DIR")
	}
	if baseDir == "" {
		baseDir = "./uploads"
	}
	if err := ValidateLocalEndpoint(baseDir); err != nil {
		return nil, err
	}

	publicURL := strings.TrimSpace(cfg.Domain)
	if publicURL == "" {
		publicURL = os.Getenv("STORAGE_LOCAL_PUBLIC_URL")
	}
	if publicURL == "" {
		publicURL = "/files"
	}

	return NewLocalDriver(baseDir, publicURL), nil
}

func newCloudStub(cfg *model.ObsConfig) Driver {
	return &cloudStub{cfg: cfg}
}

type cloudStub struct {
	cfg *model.ObsConfig
}

func (c *cloudStub) UploadMultipart(_ context.Context, file multipart.File, header *multipart.FileHeader, folder string) (string, string, error) {
	_ = file
	_ = header
	_ = folder
	return "", "", fmt.Errorf("cloud provider %s SDK not wired yet; fall back to local storage", c.cfg.Provider)
}

func (c *cloudStub) UploadReader(_ context.Context, reader io.Reader, size int64, folder string, filename string) (string, string, error) {
	_ = reader
	_ = size
	_ = folder
	_ = filename
	return "", "", fmt.Errorf("cloud provider %s SDK not wired yet; fall back to local storage", c.cfg.Provider)
}

func (c *cloudStub) Download(_ context.Context, storagePath string) (io.ReadCloser, error) {
	_ = storagePath
	return nil, fmt.Errorf("cloud provider %s SDK not wired yet", c.cfg.Provider)
}

func (c *cloudStub) Delete(_ context.Context, storagePath string) error {
	_ = storagePath
	return fmt.Errorf("cloud provider %s SDK not wired yet", c.cfg.Provider)
}

func (c *cloudStub) PublicURL(storagePath string) string {
	return obsConfigAccessURL(c.cfg, storagePath)
}

func (c *cloudStub) SignUploadURL(_ context.Context, _, _, _ string, _ time.Duration) (string, string, error) {
	return "", "", fmt.Errorf("cloud provider %s SDK not wired yet", c.cfg.Provider)
}

func (c *cloudStub) Exists(_ context.Context, storagePath string) (bool, error) {
	_ = storagePath
	return false, fmt.Errorf("cloud provider %s SDK not wired yet", c.cfg.Provider)
}

func (c *cloudStub) Type() string { return string(c.cfg.Provider) }

// withHttpsScheme 给无 scheme 的 Domain 补 https://；http:// 升级为 https://
// （AUD-02：签名链接不得明文传输）。
func withHttpsScheme(domain string) string {
	switch {
	case strings.HasPrefix(domain, "https://"):
		return domain
	case strings.HasPrefix(domain, "http://"):
		return "https://" + strings.TrimPrefix(domain, "http://")
	default:
		return "https://" + domain
	}
}

func obsConfigAccessURL(c *model.ObsConfig, filePath string) string {
	domain := strings.TrimSpace(c.Domain)
	if domain != "" {
		return withHttpsScheme(domain) + "/" + filePath
	}
	switch c.Provider {
	case model.ObsProviderAliyun:
		return "https://" + c.Bucket + "." + c.Region + ".aliyuncs.com/" + filePath
	case model.ObsProviderQiniu:
		// qiniu 无可安全合成的全局默认域名；Domain 未配时返回对象路径，
		// 不再拼 "http:///file" 畸形串（AUD-02 fail-closed）。
		return filePath
	case model.ObsProviderTencent:
		return "https://" + c.Bucket + ".cos." + c.Region + ".myqcloud.com/" + filePath
	case model.ObsProviderAWS:
		return "https://" + c.Bucket + ".s3." + c.Region + ".amazonaws.com/" + filePath
	default:
		return filePath
	}
}
