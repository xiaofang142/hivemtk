package middleware

import (
	"net"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"

	"hivemtk-user/internal/pkg/utils/logger"

	"github.com/gin-gonic/gin"
)

// A13（`未完成任务.md` §9）：可信反代名单失配时必须**响亮**，不许静默降级。
//
// 背景：`cmd/api/main.go` 用 `r.SetTrustedProxies([]string{"127.0.0.0/8", "::1/128",
// "10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16"})` 硬编码私网名单。反代/云 LB 落在
// 私网里时一切正常；一旦它跑在**公网地址**上（边缘反代、跨机房 LB、隧道），gin 就不认它
// 是可信代理，于是 `c.ClientIP()` 返回的全是反代出口地址：
//   - 限流：全站共用一个桶（一个客户端能把所有人打到 429 —— 与 A5 修的是同一类症状，不同成因）；
//   - 审计/操作日志：`ip` 字段把所有人写成同一行，事后无法定位；
//   - 且**没有任何报错**：功能照跑，只是数据全错。
//
// 本文件不猜"反代在哪"，只做两件确定的事：
//  1. 名单可由 env `TRUSTED_PROXIES` 覆盖（逗号分隔 CIDR 或单 IP），配了就在启动期打一行生效值；
//  2. 每个请求检查一次"对端不在可信名单里、却带了 X-Forwarded-For"这种**确定的自相矛盾**，
//     命中即打一行 WARN（同一对端 IP 只报一次，见 seen 去重），并计数供测试/监控读取。
//
// 为什么判据是这个而不是"启动期嗅探网络"：启动期拿不到任何请求，任何"猜反代地址"的实现
// 都只能靠配置猜或靠 DNS 猜；而"带了 XFF 但来源不受信"是请求自带的证据，既不误报（真没反代
// 就没有 XFF），也不漏报（有反代且失配就一定有 XFF）。

// DefaultTrustedProxies 内置可信反代名单（与 A13 修复前的硬编码值逐项一致）。
//
// 只在 env `TRUSTED_PROXIES` 未设置时生效——保持升级前后的 `ClientIP()` 行为不变。
var DefaultTrustedProxies = []string{
	"127.0.0.0/8", "::1/128",
	"10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16",
}

// clientIPGuardWarnHook 测试注入用：为 nil 时走 logger.Warnf。
var clientIPGuardWarnHook func(peer, forwardedFor string)

// SetClientIPGuardWarnHook 注入告警出口（仅测试用；传 nil 复位为 logger）。
func SetClientIPGuardWarnHook(fn func(peer, forwardedFor string)) {
	clientIPGuardWarnHook = fn
}

// clientIPMismatchCount 累计"XFF 来自不受信对端"的观测次数（测试/监控读）。
var clientIPMismatchCount atomic.Int64

// ClientIPMismatchCount 已观测到的可信反代失配次数（进程级累计）。
func ClientIPMismatchCount() int64 { return clientIPMismatchCount.Load() }

// TrustedProxiesFromEnv 解析生效的可信反代名单。
//
// 返回 (list, fromEnv, err)：
//   - env 未设置/空白 ⇒ (DefaultTrustedProxies, false, nil)（行为零变更）
//   - env 设置 ⇒ 逐个校验，**任一条非法就整体报错**：静默丢弃非法条目会让"我配了，
//     为什么还是全站一个 IP"变成无解问题（与 TENANT_MODE 拼错拒绝启动同一口径）
func TrustedProxiesFromEnv() ([]string, bool, error) {
	raw := strings.TrimSpace(os.Getenv("TRUSTED_PROXIES"))
	if raw == "" {
		return DefaultTrustedProxies, false, nil
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		if !isValidProxyEntry(p) {
			return nil, true, &trustedProxyParseError{entry: p}
		}
		out = append(out, p)
	}
	if len(out) == 0 {
		return nil, true, &trustedProxyParseError{entry: raw}
	}
	return out, true, nil
}

type trustedProxyParseError struct{ entry string }

func (e *trustedProxyParseError) Error() string {
	return "TRUSTED_PROXIES 含非法条目 " + strconv.Quote(e.entry) +
		"（只接受 IPv4/IPv6 地址或 CIDR，逗号分隔；列表本身写错不做静默丢弃）"
}

func isValidProxyEntry(s string) bool {
	if strings.Contains(s, "/") {
		_, _, err := net.ParseCIDR(s)
		return err == nil
	}
	return net.ParseIP(s) != nil
}

// proxyTrust 可信名单的判定形态（预解析，避免每请求 ParseCIDR）。
type proxyTrust struct {
	nets []*net.IPNet
}

func newProxyTrust(entries []string) *proxyTrust {
	t := &proxyTrust{}
	for _, e := range entries {
		if strings.Contains(e, "/") {
			_, n, err := net.ParseCIDR(e)
			if err == nil {
				t.nets = append(t.nets, n)
			}
			continue
		}
		ip := net.ParseIP(e)
		if ip == nil {
			continue
		}
		bits := 32
		if ip.To4() == nil {
			bits = 128
		}
		t.nets = append(t.nets, &net.IPNet{IP: ip, Mask: net.CIDRMask(bits, bits)})
	}
	return t
}

func (t *proxyTrust) contains(ipStr string) bool {
	ip := net.ParseIP(strings.TrimSpace(ipStr))
	if ip == nil {
		return false
	}
	for _, n := range t.nets {
		if n.Contains(ip) {
			return true
		}
	}
	return false
}

// clientIPGuard 观测"XFF 来自不受信对端"并逐对端告警一次。
type clientIPGuard struct {
	trust *proxyTrust
	mu    sync.Mutex
	seen  map[string]struct{}
	warn  func(peer, forwardedFor string)
}

func newClientIPGuard(entries []string, warn func(peer, forwardedFor string)) *clientIPGuard {
	if warn == nil {
		warn = func(peer, forwardedFor string) {
			logger.Warnf("[proxy] 请求对端 %s 不在可信反代名单里，却带了 X-Forwarded-For=%q ⇒ "+
				"gin 不认这个代理，ClientIP() 会退化成对端地址：全站限流共用一个桶、审计 IP 全写成同一个。"+
				"修法：把反代出口地址加进 env TRUSTED_PROXIES（逗号分隔，CIDR 或单 IP）后重启"+
				"（同一对端只报一次，累计可查 middleware.ClientIPMismatchCount）",
				peer, forwardedFor)
		}
	}
	return &clientIPGuard{trust: newProxyTrust(entries), seen: make(map[string]struct{}), warn: warn}
}

func (g *clientIPGuard) handler() gin.HandlerFunc {
	return func(c *gin.Context) {
		if IsMerchantCallbackPath(c.Request.URL.Path) {
			c.Next()
			return
		}
		xff := c.GetHeader("X-Forwarded-For")
		if xff == "" && c.GetHeader("X-Real-IP") == "" {
			c.Next()
			return
		}
		peer := requestPeerIP(c)
		if peer != "" && g.trust.contains(peer) {
			c.Next() // 正常形态：反代在名单里，gin 会按 XFF 取真实客户端
			return
		}
		if peer == "" {
			c.Next()
			return
		}
		clientIPMismatchCount.Add(1)
		g.mu.Lock()
		_, seen := g.seen[peer]
		if !seen {
			g.seen[peer] = struct{}{}
		}
		g.mu.Unlock()
		if !seen {
			g.warn(peer, xff)
		}
		c.Next()
	}
}

// requestPeerIP 取 TCP 层对端地址（**不**看 XFF —— 这里要的正是"谁直连的我"）。
func requestPeerIP(c *gin.Context) string {
	addr := c.Request.RemoteAddr
	if addr == "" {
		return ""
	}
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return addr // 没有端口（极少见）：原样返回，交给 ParseIP 判
	}
	return host
}

// ConfigureTrustedProxies 装配可信反代名单并挂上失配观测中间件（A13 的唯一装配入口）。
//
// 返回值 err 用于让调用方决定"配错了要不要拒绝启动"（本仓约定：配置写错必须响亮，
// 而不是带着默认值跑）。当前调用方（cmd/api/main.go）选择记录后继续，因为
// 名单错只会让限流失真，不该让服务起不来；但**告警文案里写明了修法**。
//
// 顺序要求：必须在注册**任何路由之前**调用。gin 的 `Use` 只对之后登记的路由生效，
// 反过来装的话观测中间件会被静默跳过（不留任何报错）；`SetTrustedProxies` 同理应尽早。
func ConfigureTrustedProxies(r *gin.Engine) ([]string, error) {
	list, fromEnv, err := TrustedProxiesFromEnv()
	if err != nil {
		return nil, err
	}
	if setErr := r.SetTrustedProxies(list); setErr != nil {
		return list, setErr
	}
	guard := newClientIPGuard(list, clientIPGuardWarnHook)
	r.Use(guard.handler())
	if fromEnv {
		logger.Infof("[proxy] 可信反代名单来自 env TRUSTED_PROXIES：%s（%d 条）", strings.Join(list, ","), len(list))
	} else if strings.TrimSpace(os.Getenv("PUBLIC_BASE_URL")) != "" {
		// 2026-11-16 审计 P2-2：PUBLIC_BASE_URL 非空 = 声明了反代/FRP 后的公开入口，
		// 名单却还是内置私网默认值 ⇒ 前面的反代大概率不被 gin 认作可信，登录风控/频控/
		// 审计里的来源 IP 从启动起就塌成同一个值。INFO 不够响，这里升 WARN（A13 同口径：
		// 失配必须响亮；请求级证据仍走下方 XFF 失配观测，本行只是启动期提示，零行为变更）。
		logger.Warnf("[proxy] 已声明 PUBLIC_BASE_URL（反代后的公开入口），但 TRUSTED_PROXIES 未配置，" +
			"可信名单仍是内置私网默认值：若反代不在名单内，登录风控/频控/审计日志的来源 IP 将全部塌成反代出口地址。" +
			"修复：TRUSTED_PROXIES=<反代IP或CIDR>（逗号分隔）")
	} else {
		logger.Infof("[proxy] 可信反代名单使用内置私网默认值（%d 条）。反代/云 LB 若在公网地址上，请设 env TRUSTED_PROXIES，"+
			"否则 ClientIP() 会退化成反代出口地址（全站同一限流桶）", len(list))
	}
	return list, nil
}
