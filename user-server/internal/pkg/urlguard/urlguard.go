// Package urlguard 出站 URL 公共防线（SSRF 防护，与爬虫 §8.1 同口径）。
//
// 两层校验，缺一不可：
//   - 配置期（导入/创建/启用/测试）走 ValidatePublicHTTPURL：scheme 限定 http/https +
//     解析期 DNS 校验，把「写进配置的坏地址」挡在落库前。
//   - 运行期（实际发起请求）走 GuardedTransport / DialControl：拨号期 Control 回调复校，
//     重定向跳转、DNS rebinding 之后依然生效，是最后一道闸。
//
// 拒绝清单（与 openapi_tools.ValidateOpenAPIBaseURL 的并集对齐）：
// loopback / 私网（RFC1918）/ 链路本地 / 未指定 / CGNAT（100.64/10）/ 云元数据 169.254.169.254。
//
// GuardedTransport 刻意不设 Proxy：出站目标必须是被校验过的 endpoint 本身，
// 不接受经环境变量的隐式代理跳板（跳板会让拨号期复校只看到代理地址，
// 且私网代理本就会被 Control 回调拒绝）。需要代理的部署属未支持场景。
package urlguard

import (
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"syscall"
	"time"
)

// CheckIP 校验单个 IP 是否公网可达（非内网/保留段）。
func CheckIP(ip net.IP) error {
	if ip == nil {
		return errors.New("无效 IP")
	}
	if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsUnspecified() {
		return fmt.Errorf("拒绝内网/保留地址: %s", ip)
	}
	if ip4 := ip.To4(); ip4 != nil {
		// CGNAT 共享地址段（100.64/10）：同样不是真正的公网目标。
		if ip4[0] == 100 && ip4[1] >= 64 && ip4[1] <= 127 {
			return fmt.Errorf("拒绝 CGNAT 地址: %s", ip)
		}
		// 云元数据地址（169.254.169.254 已被 IsLinkLocalUnicast 覆盖，此处显式点名便于审计核对）。
		if ip4[0] == 169 && ip4[1] == 254 && ip4[2] == 169 && ip4[3] == 254 {
			return fmt.Errorf("拒绝云元数据地址: %s", ip)
		}
	}
	return nil
}

// CheckHost 解析主机（支持 host:port 与裸 host 两种形态）并逐 IP 校验。
func CheckHost(host string) error {
	h := host
	if split, _, err := net.SplitHostPort(host); err == nil && split != "" {
		h = split
	}
	if h == "" {
		return errors.New("host 不能为空")
	}
	ips, err := net.LookupIP(h)
	if err != nil || len(ips) == 0 {
		return fmt.Errorf("主机解析失败: %s", h)
	}
	for _, ip := range ips {
		if err := CheckIP(ip); err != nil {
			return err
		}
	}
	return nil
}

// ValidatePublicHTTPURL 配置期校验：scheme 限定 http/https + 主机公网校验。
func ValidatePublicHTTPURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil {
		return errors.New("URL 非法")
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("仅支持 http/https，实际 %q", u.Scheme)
	}
	if u.Host == "" {
		return errors.New("URL 缺少主机")
	}
	return CheckHost(u.Host)
}

// DialControl 拨号期复校，适配 http.Transport.DialContext.Control 回调签名
// （network/address/rawConn）。重定向与 rebinding 后依然生效。
func DialControl(network, address string, _ syscall.RawConn) error {
	return CheckHost(address)
}

// GuardedTransport 返回带拨号期复校的 Transport（调用方自行决定 Timeout/连接池等）。
func GuardedTransport() *http.Transport {
	return &http.Transport{
		DialContext: (&net.Dialer{
			Timeout: 10 * time.Second, // 与爬虫同口径
			Control: DialControl,
		}).DialContext,
	}
}
