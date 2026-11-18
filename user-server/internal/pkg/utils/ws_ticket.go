package utils

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"sync"
	"time"

	"hivemtk-user/internal/pkg/utils/logger"
)

// WebSocket 一次性 ticket（2026-11-15 审计 L7）。
//
// 背景：WS 升级无法带自定义 header，JWT/visitor_token 此前只能走 URL query
// （?token= / ?visitor_token=），会进网关/反代 access log 与 APM，日志泄露即会话
// 劫持。ticket 把「长期凭据」换成「60 秒单次消费的一次性凭证」：客户端先经
// HTTP（Authorization 头 / visitor_token 头）换 ticket，再带着 ticket 连 WS；
// 换发与消费都成功才写身份进上下文，URL 上不再出现长期凭据。
//
// 边界声明（与进程内兜底黑名单同口径）：ticket 是内存态，多实例部署时 A 实例
// 铸造的 ticket 在 B 实例不可消费（单实例与实例内直连形态完全覆盖）；TTL 60s
// + 单次消费 + 身份绑定（ visitor 票绑定 channel/visitor/session 三元组）三重
// 收紧，重放与转用都无窗口。
const (
	wsTicketTTL   = 60 * time.Second
	wsTicketBytes = 32
	// wsTicketStoreCap 内存票据商店上限：防已认证调用方以铸造洪泛撑爆内存（janitor 只清过期项）。
	wsTicketStoreCap = 100000
)

// WsTicketClaims ticket 里快照的身份。Visitor=true 的票走访客 WS 通道，
// 其余字段（UserID 等）走坐席/管理端 JWT 通道，两类票互不通用。
type WsTicketClaims struct {
	Visitor bool
	// 坐席/管理端身份快照（铸造时从 gin 上下文取，来源是已验签的 JWT）
	UserID       uint
	Username     string
	Role         string
	DataScope    string
	DepartmentID uint
	TeamID       uint
	TenantID     uint
	// 访客身份绑定（铸造时已通过 visitor_token HMAC 校验）
	ChannelID string
	VisitorID string
	SessionID string
}

type wsTicketEntry struct {
	claims    WsTicketClaims
	expiresAt time.Time
}

var (
	wsTicketMu    sync.Mutex
	wsTicketStore = map[string]wsTicketEntry{}
	wsTicketOnce  sync.Once
	// wsTicketJanitorInterval 过期回收巡检周期（var 仅为测试可调速，生产勿改）。
	wsTicketJanitorInterval = 30 * time.Second
)

// MintWSTicket 铸造一次性 WS ticket（随机 32 字节 hex，60s 有效）。
func MintWSTicket(claims WsTicketClaims) string {
	raw := make([]byte, wsTicketBytes)
	if _, err := rand.Read(raw); err != nil {
		// crypto/rand 读取失败：绝不退化成可预测值，直接拒绝铸造（fail-closed）
		logger.Errorf("[SECURITY] MintWSTicket rand.Read failed: %v", err)
		return ""
	}
	// 首铸即拉起过期回收巡检（2026-11-18 审计补接线）：票据只在消费路径随手删，
	// 「铸了没连」的废弃票全靠 janitor 回收——不接线的话商店只涨不清，涨满
	// wsTicketStoreCap 后铸造永久 fail-closed，全部 WS 连接失联到重启为止。
	startWsTicketJanitor()
	ticket := hex.EncodeToString(raw)
	wsTicketMu.Lock()
	// 2026-11-18 七审收口：内存商店上限（上限=10 万条 ≈ 满额 60s 窗口内的极端
	// 铸造速率；正常流量远低于此）。超限时按"过期即弃"语义拒绝铸造新票，
	// fail-closed——内存不设上限会被已认证调用方缓慢撑爆（janitor 只清过期项）。
	if len(wsTicketStore) >= wsTicketStoreCap {
		logger.Errorf("[SECURITY] WS ticket store full (%d)，拒绝铸造（60s 后随过期回收）", len(wsTicketStore))
		wsTicketMu.Unlock()
		return ""
	}
	wsTicketStore[ticket] = wsTicketEntry{claims: claims, expiresAt: time.Now().Add(wsTicketTTL)}
	wsTicketMu.Unlock()
	return ticket
}

// ConsumeWSTicket 消费 ticket：命中且未过期即删除（单次消费）并返回身份；
// 未命中/已过期一律 false（调用方按 401 处理，不区分原因防探测）。
func ConsumeWSTicket(ticket string) (WsTicketClaims, bool) {
	if ticket == "" {
		return WsTicketClaims{}, false
	}
	wsTicketMu.Lock()
	defer wsTicketMu.Unlock()
	entry, ok := wsTicketStore[ticket]
	if !ok {
		return WsTicketClaims{}, false
	}
	if time.Now().After(entry.expiresAt) {
		delete(wsTicketStore, ticket)
		return WsTicketClaims{}, false
	}
	delete(wsTicketStore, ticket)
	return entry.claims, true
}

func wsTicketJanitor() {
	for {
		time.Sleep(wsTicketJanitorInterval)
		sweepExpiredWSTickets()
	}
}

// sweepExpiredWSTickets 清掉过期末消费的废弃票（消费路径已随手删，漏网的只有
// 铸了没连的废弃票；独立成函数供回归测试直接驱动，不必等巡检周期）。
func sweepExpiredWSTickets() {
	now := time.Now()
	wsTicketMu.Lock()
	for k, e := range wsTicketStore {
		if now.After(e.expiresAt) {
			delete(wsTicketStore, k)
		}
	}
	wsTicketMu.Unlock()
}

func startWsTicketJanitor() {
	wsTicketOnce.Do(func() {
		SafeGo(context.Background(), "ws_ticket.janitor", func(ctx context.Context) {
			wsTicketJanitor()
		})
	})
}
