package service

import (
	"fmt"
	"net"
	"net/url"
	"os"
	"strings"
)

// validateOutboundURL 发布目标校验：默认只允许 https，并拒绝回环/内网字面量，
// 防止平台账号被配置成访问内网服务（SSRF）。测试环境用
// GEO_ALLOW_PRIVATE_PUBLISH_HOSTS=1 放行 httptest，生产不得开启。
func validateOutboundURL(raw string, requireHTTPS bool) error {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return fmt.Errorf("发布目标地址为空")
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return fmt.Errorf("发布目标地址无效: %q", raw)
	}
	allowPrivate := os.Getenv("GEO_ALLOW_PRIVATE_PUBLISH_HOSTS") == "1"
	if requireHTTPS && u.Scheme != "https" && !allowPrivate {
		return fmt.Errorf("发布目标必须使用 https")
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("发布目标只支持 http(s)")
	}
	if allowPrivate {
		return nil
	}
	host := strings.ToLower(u.Hostname())
	if host == "localhost" || strings.HasSuffix(host, ".local") || strings.HasSuffix(host, ".internal") {
		return fmt.Errorf("发布目标不允许指向内网主机")
	}
	if ip := net.ParseIP(host); ip != nil {
		if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsUnspecified() {
			return fmt.Errorf("发布目标不允许指向内网地址")
		}
	}
	return nil
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if s := strings.TrimSpace(v); s != "" {
			return s
		}
	}
	return ""
}
