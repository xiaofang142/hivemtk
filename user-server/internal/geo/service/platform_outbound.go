package service

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
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

// newSafePublishHTTPClient 构造发布用 HTTP 客户端：
//   - 拨号前解析域名并逐个校验 IP，阻止域名解析到内网；
//   - 每次重定向重新校验目标，最多 5 跳，生产禁止降级到 http；
//   - 不走环境代理，避免通过代理绕过校验。
func newSafePublishHTTPClient(timeout time.Duration) *http.Client {
	dialer := &net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}
	transport := &http.Transport{
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			host, port, err := net.SplitHostPort(addr)
			if err != nil {
				return nil, fmt.Errorf("发布目标地址非法: %w", err)
			}
			ips, err := net.DefaultResolver.LookupIPAddr(ctx, host)
			if err != nil {
				return nil, fmt.Errorf("发布目标域名解析失败: %w", err)
			}
			var lastErr error
			for _, ipa := range ips {
				ipAddr := net.JoinHostPort(ipa.IP.String(), port)
				if err := validateOutboundURL("https://"+ipAddr, false); err != nil {
					lastErr = err
					continue
				}
				conn, dialErr := dialer.DialContext(ctx, network, ipAddr)
				if dialErr == nil {
					return conn, nil
				}
				lastErr = dialErr
			}
			if lastErr == nil {
				lastErr = errors.New("发布目标没有可用地址")
			}
			return nil, lastErr
		},
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: 20 * time.Second,
		MaxIdleConns:          10,
		IdleConnTimeout:       30 * time.Second,
	}
	return &http.Client{
		Timeout:   timeout,
		Transport: transport,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 5 {
				return errors.New("发布目标重定向超过 5 次")
			}
			if os.Getenv("GEO_ALLOW_PRIVATE_PUBLISH_HOSTS") != "1" && req.URL.Scheme != "https" {
				return errors.New("发布目标不允许重定向到非 https 地址")
			}
			return validateOutboundURL(req.URL.String(), false)
		},
	}
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if s := strings.TrimSpace(v); s != "" {
			return s
		}
	}
	return ""
}
