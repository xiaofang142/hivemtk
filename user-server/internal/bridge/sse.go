// Package bridge 提供 SSE（Server-Sent Events）下行业务实现。
//
// 业界依据：
//   - HTML5 SSE 标准（W3C 2015 候选推荐）
//   - Twilio Flex / Intercom / Front App 全部从长轮询迁移到 SSE（延迟 1-3s → <500ms）
//   - EventSource 浏览器 API 自动重连、断线恢复
//
// 协议格式：
//
//	data: <json>
//	id: <event_id>
//	event: <type>
//	retry: <ms>
//	<blank line>
//
// 设计要点：
//   - 复用现有长轮询的「取 outbox」逻辑（同一数据源）
//   - HTTP 响应头：text/event-stream, Cache-Control: no-cache, Connection: keep-alive
//   - 心跳：每 15s 发送 :keepalive 注释行（防代理超时）
//   - 自动重连：客户端用 Last-Event-ID header 续传
//   - 并发安全：每客户端独立 goroutine + ctx cancel
//   - Phase 1 新增：SSEBus 事件驱动推送（轮询 → 即时推送，延迟降至 <500ms）
package bridge

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"sync"
	"time"

	"hivemtk-user/internal/pkg/utils/logger"
	"hivemtk-user/internal/service"

	"github.com/gin-gonic/gin"
)

// SSE 配置（DB 驱动，以下为 fallback 默认值）
// 实际运行时通过 service.GlobalConfigParam() 按 group=bridge 读取 DB 参数：
//
//	bridge.sse_heartbeat_interval → SSEDefaultHeartbeatInterval (fallback)
//	bridge.sse_max_stream_duration → SSEDefaultMaxStreamDuration (fallback)
//
// B-1 心跳约束（强制）：
//
//	Chrome MV3 extension service worker 有硬性 30s 不活跃就被杀死的窗口限制。
//	SSE 下行必须以 ≤20s 间隔发送心跳注释帧 ": ping\n\n"，为网络抖动/代理缓冲
//	预留 10s 余量。15s 默认值符合该约束。
//	- SSE 协议注释帧格式：以 ":" 开头，客户端 EventSource 完全忽略
//	- 同时防止 反向代理层 /CDN proxy_read_timeout（通常 60s）切断长连接
const (
	SSEDefaultHeartbeatInterval = 15 * time.Second
	SSEDefaultMaxStreamDuration = 5 * time.Minute
	SSEMaxBacklogEvents         = 1000
	SSEBusBufferSize            = 100
)

// EventNewOutbound 出站投递事件名。出站认领门（acquireOutboundPush）按它判定，
// 而 R-B1 已规定一切投递路径的事件都必须经 BuildOutboundSSEEvent 构造——两者配对，
// 手拼一个新事件名就绕过了认领，故这里给字面量一个可被测试锁住的符号。
const EventNewOutbound = "new_outbound"

func runtimeSSEHeartbeatInterval(ctx context.Context) time.Duration {
	return service.GlobalConfigParam().GetDuration(ctx, "bridge", "sse_heartbeat_interval", SSEDefaultHeartbeatInterval)
}
func runtimeSSEMaxStreamDuration(ctx context.Context) time.Duration {
	return service.GlobalConfigParam().GetDuration(ctx, "bridge", "sse_max_stream_duration", SSEDefaultMaxStreamDuration)
}

// SSEOutboxFetcher 拉取 outbox 事件的接口
//
// 解耦：让 SSE handler 不知道具体的 outbox 存储；便于测试 mock
type SSEOutboxFetcher interface {
	// FetchOutboxSince 拉取 lastEventID 之后的所有事件
	// channel + accountId 用于过滤；empty 表示不过滤
	// 返回 (events, newLastEventID, error)
	FetchOutboxSince(ctx context.Context, channel, accountID, lastEventID string) ([]SSEEvent, string, error)
}

// SSEEvent 推送给客户端的事件
//
// Phase 1 扩展：增加会话路由字段，前端可按 conversation_id 精确路由
type SSEEvent struct {
	ID             string         `json:"id"`
	Event          string         `json:"event"`
	ConversationID string         `json:"conversation_id"`
	MsgType        string         `json:"msg_type"`
	ReceiverID     string         `json:"receiver_id"`
	Seq            int            `json:"seq"`
	Data           map[string]any `json:"data"`
	Timestamp      time.Time      `json:"timestamp"`
}

// OutboundEventData 下行出站事件的业务字段（SSEEvent.Data 的单一构造源）。
//
// R-B1 契约（2026-09-19）：SSE 总线路径与 DB 补拉路径必须产出**逐键一致**的 Data——
// 扩展端 downlink 以 Data.msg_id 为去重/ack 复合键（msg_id|conversation_id），
// 曾因总线路径缺 msg_id → 键退化为 "undefined|conv" → 同会话第二条起全被误判重复
// → 静默丢消息。任何新增投递路径一律经 BuildOutboundSSEEvent，禁止手拼 Data。
type OutboundEventData struct {
	HubID          uint64
	MsgID          string
	Platform       string
	AccountID      string
	ConversationID string
	Content        string
	MsgType        string
	ReceiverID     string
	IsAIReply      bool
	Extra          any
	CreatedAt      time.Time
}

// BuildOutboundSSEEvent 由 OutboundEventData 统一构造 new_outbound 事件。
func BuildOutboundSSEEvent(d OutboundEventData) SSEEvent {
	return SSEEvent{
		ID:             strconv.FormatUint(d.HubID, 10),
		Event:          EventNewOutbound,
		ConversationID: d.ConversationID,
		MsgType:        d.MsgType,
		ReceiverID:     d.ReceiverID,
		Seq:            int(d.HubID),
		Data: map[string]any{
			"hub_id":          d.HubID,
			"msg_id":          d.MsgID,
			"platform":        d.Platform,
			"account_id":      d.AccountID,
			"conversation_id": d.ConversationID,
			"content":         d.Content,
			"msg_type":        d.MsgType,
			"receiver_id":     d.ReceiverID,
			"is_ai_reply":     d.IsAIReply,
			"extra":           d.Extra,
		},
		Timestamp: d.CreatedAt,
	}
}

// OutboundPushClaimer 出站行「推送前认领」（批11 §3.7-2），由 repository.MessageHubRepository 实现。
//
// 为什么必须在服务端：SSE 与长轮询是**同一个 message_hub 待办集合**的两条投递路径，
// 而轮询侧早有 ClaimPendingOutbound 排他认领、SSE 侧此前从不认领（一条 go func 直接 Publish）。
// 两端都不认领的同一条 pending 行可被两条路径各拿一次 → 同一句话打给真实客户两遍（不可逆写）。
// 更常见的是：SSE 推送后发送失败，行仍是 pending，客户端游标已前移 → 永不再投 → 静默丢消息。
//
// 统一口径 = 「先认领、再推送、ack 落状态；断连不回收已认领行，由可见性超时重投」，
// 与 service.InboxOutboundClaimTimeout（轮询侧同一个超时来源）配对。
type OutboundPushClaimer interface {
	ClaimOutboundForPush(ctx context.Context, id uint64, claimTimeout time.Duration) (bool, error)
}

// SSEBus 事件通知总线：message_hub 写入后立即通知 SSE 连接
//
// 设计要点：
//   - 按 channel:account_id 和 conversation_id 双维度订阅
//   - buffer 满时丢弃（非阻塞），消费者不应被拖慢
//   - Subscribe 返回 cancel 函数，调用方负责释放
//   - new_outbound 事件先过 claimer 认领再投递（见 OutboundPushClaimer）
type SSEBus struct {
	mu     sync.RWMutex
	subs   map[string][]chan SSEEvent
	buffer int

	claimer      OutboundPushClaimer
	claimTimeout time.Duration
}

// GlobalSSEBus 全局 SSE 事件总线
var GlobalSSEBus = NewSSEBus()

// NewSSEBus 构造 SSEBus
func NewSSEBus() *SSEBus {
	return &SSEBus{
		subs:   make(map[string][]chan SSEEvent),
		buffer: SSEBusBufferSize,
	}
}

// Subscribe 订阅指定 channel:account_id 的事件
// 返回接收通道和取消函数
func (b *SSEBus) Subscribe(channel, accountID string) (chan SSEEvent, func()) {
	ch := make(chan SSEEvent, b.buffer)
	key := channel + ":" + accountID
	b.mu.Lock()
	b.subs[key] = append(b.subs[key], ch)
	b.mu.Unlock()

	cancel := func() {
		b.mu.Lock()
		defer b.mu.Unlock()
		subs := b.subs[key]
		for i, sub := range subs {
			if sub == ch {
				b.subs[key] = append(subs[:i], subs[i+1:]...)
				break
			}
		}
		close(ch)
	}
	return ch, cancel
}

// SubscribeByConversation 订阅指定 conversation_id 的事件（用于会话级精准推送）
func (b *SSEBus) SubscribeByConversation(conversationID string) (chan SSEEvent, func()) {
	ch := make(chan SSEEvent, b.buffer)
	b.mu.Lock()
	b.subs[conversationID] = append(b.subs[conversationID], ch)
	b.mu.Unlock()

	cancel := func() {
		b.mu.Lock()
		defer b.mu.Unlock()
		subs := b.subs[conversationID]
		for i, sub := range subs {
			if sub == ch {
				b.subs[conversationID] = append(subs[:i], subs[i+1:]...)
				break
			}
		}
		close(ch)
	}
	return ch, cancel
}

// SetOutboundClaimer 注入出站认领器（由 handler 在装配 repository 查询器时调用一次）。
//
// 传 nil 表示「认领不可用」——此时 new_outbound 退回旧行为（直接推、不认领）。
// 这不是一个安全的稳态：装配齐全的生产进程里它必须非 nil，故未注入时打一条 Warn，
// 让「双路可能重投」在日志里留痕而不是静默。
func (b *SSEBus) SetOutboundClaimer(c OutboundPushClaimer) {
	b.mu.Lock()
	b.claimer = c
	if c != nil {
		b.claimTimeout = service.InboxOutboundClaimTimeout
	}
	b.mu.Unlock()
	// 同一处把「谁在线」交给回扫补投门：推送认领与补投门必须读同一张订阅表，
	// 分两处注入就会漂移（一处以为可达、另一处判无人在线）。闭包晚绑定 GlobalSSEBus，
	// 测试换总线实例也不会指向旧对象。认领器撤走（装配缺件）时探针一并撤走，
	// 让 service 侧走「缺探针 ⇒ 放行 + 一次性 Warn」的退化路径。
	if c != nil {
		service.SetBridgeChannelOnlineProbe(BridgeChannelOnline)
	} else {
		service.SetBridgeChannelOnlineProbe(nil)
	}
	if c == nil {
		logger.GetLogger().Warn().
			Msg("[SSE] 出站认领器未注入，new_outbound 退回不认领直推（SSE/轮询双路重投面未收口）")
		return
	}
	logger.GetLogger().Info().Str("claim_timeout", b.claimTimeout.String()).
		Msg("[SSE] 出站认领器已注入：先认领再推送")
}

// hasSubscribersFor 这条事件此刻是否有活的接收者（会话级或账号级任一维度）。
//
// 只有「有人在线」时才值得为推送认领——否则认领回来没人收，行要压到可见性超时才回到待办，
// 白白给一条本来可以由轮询立刻取走的消息加一轮超时延迟。
// 两个维度都要看：Publish 对 conv 级与账号级订阅都投递，只查一边会把纯会话级订阅者饿死。
func (b *SSEBus) hasSubscribersFor(ev SSEEvent) bool {
	key := ""
	if platform, ok := ev.Data["platform"].(string); ok {
		if accountID, ok2 := ev.Data["account_id"].(string); ok2 {
			key = platform + ":" + accountID
		}
	}
	b.mu.RLock()
	defer b.mu.RUnlock()
	if ev.ConversationID != "" && len(b.subs[ev.ConversationID]) > 0 {
		return true
	}
	return key != "" && len(b.subs[key]) > 0
}

// HasSubscribers 该账号此刻是否有活的 SSE 连接（只看账号级维度，与 Subscribe 同 key 口径）。
//
// 这是「扩展在线」的唯一权威信号：bridge_accounts.status 要靠 SSE 生命周期去翻，
// last_sync_at 只反映最后一次写入，二者都不能当门用。
func (b *SSEBus) HasSubscribers(channel, accountID string) bool {
	if channel == "" || accountID == "" {
		return false
	}
	key := channel + ":" + accountID
	b.mu.RLock()
	defer b.mu.RUnlock()
	return len(b.subs[key]) > 0
}

// BridgeChannelOnline 这条渠道账号此刻收不收得到消息——回扫补投门的唯一真值。
//
// 两个信号取或，缺一不可：
//   - SSE 订阅：默认下行就挂在流上，最确切，且命中就不必再读库（回扫每轮每渠道问一次）；
//   - 账号行在线位（status + last_sync_at 宽限窗）：长轮询下行（FF_SSE_BRIDGE=0，
//     或 SSE 启动失败回退的那条路，见 user-web/bridge/src/core/polling-loop.js）
//     根本不建订阅，只留得下轮询刷新过的同步时间。只认订阅会让这种模式下所有延后出站
//     被逐轮跳过、一行都不碰，而且不报错——门从"少烧几条判弃"变成"永不补投"。
//
// 渠道先归一再查：订阅键与 bridge_accounts 都存规范渠道，回扫读来的可能是历史别名值。
// 真值读不到（仓储未装配 / 查询失败）时放行，与探针缺件同一口径：
// 门建不起来只能退化成"照旧补投"，不能变成"谁都不投"。
func BridgeChannelOnline(ctx context.Context, channel, accountID string) bool {
	ch := NormalizeBridgeChannel(channel)
	if ch == "" || accountID == "" {
		return false
	}
	if GlobalSSEBus.HasSubscribers(ch, accountID) {
		return true
	}
	if GlobalBridgeAccountRepo == nil {
		return true
	}
	online, err := GlobalBridgeAccountRepo.IsOnline(ctx, ch, accountID)
	if err != nil {
		logger.Ctx(ctx).Warn().Err(err).
			Str("channel", ch).Str("account_id", accountID).
			Msg("[BridgeReplay] 在线位读取失败，本轮按可达放行（读不到真值不能当成所有人离线）")
		return true
	}
	return online
}

// claimForPush 判定这条 new_outbound 此刻归不归本次推送，并把行置 inflight。
//
// 返回 false 的几种情形都必须放弃推送，且都不算丢消息——行仍留在待办集合里，
// 由下一次补拉/轮询按同一口径取走：
//  1. 无在线订阅者（仅总线路径判定：不认领，把行留给轮询）
//  2. 认领失败（别的消费者已认领 / 已 delivered / 方向不对）
//  3. 认领报错（此刻无法判定归属 → 宁可少投一次，也不制造双投；超时后自愈）
//
// needSubscriber：总线 Publish 传 true；SSE 首连补拉传 false——那时读者就是这条
// HTTP 流本身，还没往总线订阅（HandleOutboxSSE 里 Subscribe 在补拉之后），按订阅判定会把
// 整个 backlog 判成「没人要」而一条都不发。
//
// claimer 未注入时退回旧行为（放行），由 SetOutboundClaimer 的 Warn 负责暴露。
func (b *SSEBus) claimForPush(ctx context.Context, ev SSEEvent, needSubscriber bool) bool {
	b.mu.RLock()
	claimer, timeout := b.claimer, b.claimTimeout
	b.mu.RUnlock()
	if claimer == nil {
		return true
	}
	hubID, ok := ev.Data["hub_id"].(uint64)
	if !ok || hubID == 0 {
		logger.GetLogger().Error().
			Str("event_id", ev.ID).
			Str("conv_id", ev.ConversationID).
			Interface("hub_id", ev.Data["hub_id"]).
			Msg("[SSE] new_outbound 缺合法 hub_id，无法认领即不推送（须排查事件构造路径）")
		return false
	}
	channel, _ := ev.Data["platform"].(string)
	accountID, _ := ev.Data["account_id"].(string)
	if needSubscriber && !b.hasSubscribersFor(ev) {
		logger.Ctx(ctx).Debug().
			Uint64("hub_id", hubID).Str("channel", channel).Str("account_id", accountID).
			Msg("[SSE] 无在线订阅者，不认领（留给轮询/下次补拉）")
		return false
	}
	claimed, err := claimer.ClaimOutboundForPush(ctx, hubID, timeout)
	if err != nil {
		logger.GetLogger().Error().Err(err).
			Uint64("hub_id", hubID).Str("channel", channel).
			Msg("[SSE] 出站认领报错，放弃本次推送（行仍待办，超时后重投）")
		return false
	}
	if !claimed {
		logger.GetLogger().Info().
			Uint64("hub_id", hubID).Str("channel", channel).Str("account_id", accountID).
			Msg("[SSE] 出站认领未命中（已被别的消费者取走或已终态），跳过低延迟推送")
		return false
	}
	return true
}

// Publish 发布事件到 SSE 总线
//
// 投递策略：
//  1. new_outbound 先过服务端权威认领（claimForPush），拿不到归属就不推
//  2. 优先按 conversation_id 精准投递（会话级订阅）
//  3. 再按 channel:account_id 广播（账号级订阅）
//     - 通道满时丢弃，避免阻塞发布者；丢弃的行由可见性超时回到待办后重投
func (b *SSEBus) Publish(event SSEEvent) {
	b.PublishCtx(context.Background(), event)
}

// PublishCtx 带调用链上下文的 Publish（认领那一步要落 ctx 日志）。
func (b *SSEBus) PublishCtx(ctx context.Context, event SSEEvent) {
	if event.Event == EventNewOutbound && !b.claimForPush(ctx, event, true) {
		return
	}
	b.mu.RLock()
	defer b.mu.RUnlock()

	hasConvSubs := false
	hasBroadSubs := false

	if event.ConversationID != "" {
		if chs, ok := b.subs[event.ConversationID]; ok {
			hasConvSubs = true
			for _, ch := range chs {
				select {
				case ch <- event:
				default:
					logger.GetLogger().Warn().Str("conv_id", event.ConversationID).Msg("[SSEBus] channel buffer full, dropping event")
				}
			}
		}
	}

	if platform, ok1 := event.Data["platform"].(string); ok1 {
		if accountID, ok2 := event.Data["account_id"].(string); ok2 {
			broadKey := platform + ":" + accountID
			if chs, ok := b.subs[broadKey]; ok {
				hasBroadSubs = true
				for _, ch := range chs {
					select {
					case ch <- event:
					default:
						logger.GetLogger().Warn().Str("broad_key", broadKey).Msg("[SSEBus] broadcast channel buffer full, dropping event")
					}
				}
			}
		}
	}

	if hasConvSubs || hasBroadSubs {
		logger.GetLogger().Debug().
			Str("event_id", event.ID).
			Str("conv_id", event.ConversationID).
			Str("msg_type", event.MsgType).
			Int("seq", event.Seq).
			Bool("conv_subs", hasConvSubs).
			Bool("broad_subs", hasBroadSubs).
			Msg("[SSEBus] event published")
	}
}

// SSEHandler SSE 处理器
type SSEHandler struct {
	fetcher           SSEOutboxFetcher
	heartbeatInterval time.Duration
	maxStreamDuration time.Duration
}

// NewSSEHandler 构造 SSE 处理器
func NewSSEHandler(fetcher SSEOutboxFetcher) *SSEHandler {
	return &SSEHandler{
		fetcher:           fetcher,
		heartbeatInterval: SSEDefaultHeartbeatInterval,
		maxStreamDuration: SSEDefaultMaxStreamDuration,
	}
}

// SetHeartbeat 设置心跳间隔（用于测试 / 调优）
func (h *SSEHandler) SetHeartbeat(d time.Duration) {
	if d > 0 {
		h.heartbeatInterval = d
	}
}

// SetMaxDuration 设置最大流时长（防止无限连接）
func (h *SSEHandler) SetMaxDuration(d time.Duration) {
	if d > 0 {
		h.maxStreamDuration = d
	}
}

// bridgeAccountStateWriteTimeout 在线位落库的写超时：状态写不进去只报警，绝不断流。
const bridgeAccountStateWriteTimeout = 2 * time.Second

// touchBridgeAccountOnline 刷新账号在线位（建流时一次，此后每次心跳一次）。
func touchBridgeAccountOnline(ctx context.Context, channel, accountID string) {
	if GlobalBridgeAccountRepo == nil || channel == "" || accountID == "" {
		return
	}
	wctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), bridgeAccountStateWriteTimeout)
	defer cancel()
	if err := GlobalBridgeAccountRepo.TouchLastSync(wctx, channel, accountID); err != nil {
		logger.Ctx(wctx).Warn().Err(err).
			Str("channel", channel).Str("account_id", accountID).
			Msg("[SSE] 在线位刷新失败（status/last_sync_at 未落库）")
	}
}

// markBridgeAccountOffline 账号的最后一条流退出时置离线。
//
// 必须先看总线还有没有别的活订阅：同一账号常有多条并发流（多标签页/重连重叠），
// 无条件置离线会让还在收消息的渠道看起来掉线。
func markBridgeAccountOffline(ctx context.Context, channel, accountID string) {
	if GlobalBridgeAccountRepo == nil || channel == "" || accountID == "" {
		return
	}
	if GlobalSSEBus.HasSubscribers(channel, accountID) {
		return
	}
	wctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), bridgeAccountStateWriteTimeout)
	defer cancel()
	if err := GlobalBridgeAccountRepo.SetOffline(wctx, channel, accountID); err != nil {
		logger.Ctx(wctx).Warn().Err(err).
			Str("channel", channel).Str("account_id", accountID).
			Msg("[SSE] 离线位置写入失败（status 将停在 online 直到下次心跳刷新）")
	}
}

func (h *SSEHandler) HandleOutboxSSE(c *gin.Context) {
	// 渠道必须归一：ingest 侧写 bridge_accounts / 广播 SSE 都用规范渠道，
	// 这里原样透传别名（douyin_web）会让订阅挂在另一个键上——既收不到广播，
	// 在线位也刷新不到账号行，补投门再把这条明显在线的流判成离线。
	channel := NormalizeBridgeChannel(c.Query("channel"))
	accountID := c.Query("account_id")
	lastEventID := c.GetHeader("Last-Event-ID")
	if lastEventID == "" {
		lastEventID = c.Query("last_event_id")
	}

	ctxReq := c.Request.Context()
	heartbeatInterval := h.heartbeatInterval
	if heartbeatInterval == SSEDefaultHeartbeatInterval {
		heartbeatInterval = runtimeSSEHeartbeatInterval(ctxReq)
	}
	maxStreamDuration := h.maxStreamDuration
	if maxStreamDuration == SSEDefaultMaxStreamDuration {
		maxStreamDuration = runtimeSSEMaxStreamDuration(ctxReq)
	}

	c.Writer.Header().Set("Content-Type", "text/event-stream")
	c.Writer.Header().Set("Cache-Control", "no-cache")
	c.Writer.Header().Set("Connection", "keep-alive")
	c.Writer.Header().Set("X-Accel-Buffering", "no")
	c.Writer.WriteHeader(http.StatusOK)

	flusher, ok := c.Writer.(http.Flusher)
	if !ok {
		c.AbortWithStatus(http.StatusInternalServerError)
		return
	}

	if _, err := fmt.Fprintf(c.Writer, "retry: %d\n\n", heartbeatInterval.Milliseconds()); err != nil {
		return
	}
	flusher.Flush()

	ctx, cancel := context.WithTimeout(ctxReq, maxStreamDuration)
	defer cancel()

	logger.Ctx(ctx).Info().
		Str("channel", channel).
		Str("account_id", accountID).
		Str("last_event_id", lastEventID).
		Msg("[SSE] client connected")

	newLastID := lastEventID

	if h.fetcher != nil {
		events, newID, err := h.fetcher.FetchOutboxSince(ctx, channel, accountID, lastEventID)
		if err != nil {
			logger.Ctx(ctx).Warn().Err(err).Msg("[SSE] initial fetch failed")
		} else {
			for _, ev := range events {
				if !writeSSEEvent(c.Writer, ev) {
					logger.Ctx(ctx).Info().Msg("[SSE] client disconnected during backlog")
					return
				}
				newLastID = ev.ID
			}
			if newID != "" {
				newLastID = newID
			}
		}
		flusher.Flush()
	}

	busCh, busCancel := GlobalSSEBus.Subscribe(channel, accountID)
	logger.Ctx(ctx).Info().
		Str("channel", channel).
		Str("account_id", accountID).
		Str("bus_key", channel+":"+accountID).
		Msg("[SSE] subscribed to SSEBus")
	// defer 顺序即语义：LIFO 下后注册的先执行 —— 必须先摘掉本流订阅，再判
	// 「该账号最后一条流是否退出了」。反序注册时判离线会看见自己那条还挂着的订阅，
	// 账号永远摘不掉 online，补投门就此失真。
	defer markBridgeAccountOffline(ctx, channel, accountID)
	defer busCancel()

	touchBridgeAccountOnline(ctx, channel, accountID)

	if h.fetcher != nil {
		events, newID, err := h.fetcher.FetchOutboxSince(ctx, channel, accountID, newLastID)
		if err != nil {
			logger.Ctx(ctx).Warn().Err(err).Msg("[SSE] post-subscribe fetch failed")
		} else {
			for _, ev := range events {
				if !writeSSEEvent(c.Writer, ev) {
					logger.Ctx(ctx).Info().Msg("[SSE] client disconnected during post-subscribe backlog")
					return
				}
				newLastID = ev.ID
			}
			if newID != "" {
				newLastID = newID
			}
			if len(events) > 0 {
				logger.Ctx(ctx).Info().Int("count", len(events)).Msg("[SSE] post-subscribe catchup: filled gap")
			}
		}
		flusher.Flush()
	}

	pollInterval := 2 * heartbeatInterval
	idlePollInterval := 4 * heartbeatInterval

	heartbeat := time.NewTicker(heartbeatInterval)
	poll := time.NewTicker(pollInterval)
	defer heartbeat.Stop()
	defer poll.Stop()

	clientGone := c.Request.Context().Done()
	lastBusEventAt := time.Now()

	// R-B3（2026-09-19）：轮询定时器仅在目标间隔变化时 Reset——原实现每轮 select 前无条件
	// Reset，而心跳每 15s 也触发一轮，poll 计时被反复续期 → 永远不到点 → 总线丢事件时
	// 的 DB 兜底补拉形同虚设。
	curPollInterval := pollInterval

	for {

		desired := pollInterval
		if time.Since(lastBusEventAt) < 30*time.Second {
			desired = idlePollInterval
		}
		if desired != curPollInterval {
			poll.Reset(desired)
			curPollInterval = desired
		}

		select {
		case <-ctx.Done():
			logger.Ctx(ctx).Info().Msg("[SSE] stream ended (timeout or cancel)")
			return
		case <-clientGone:
			logger.Ctx(ctx).Info().Msg("[SSE] client disconnected")
			return
		case <-heartbeat.C:
			if _, err := c.Writer.WriteString(": ping\n\n"); err != nil {
				return
			}
			flusher.Flush()
			// 心跳即「这条流还活着」的证据，顺手刷新在线位：只在建流时刷一次的话，
			// 长连接稳定运行的账号会随 last_sync_at 老化看起来重新掉线。
			touchBridgeAccountOnline(ctx, channel, accountID)
		case ev, ok := <-busCh:
			if !ok {
				// channel 被关闭：若继续 select 会立即变 hot-spin 空转烧 CPU，直接结束流
				logger.Ctx(ctx).Info().Msg("[SSE] bus channel closed, stream ended")
				return
			}
			if !writeSSEEvent(c.Writer, ev) {
				logger.Ctx(ctx).Info().Msg("[SSE] client disconnected during bus event")
				return
			}
			newLastID = ev.ID
			lastBusEventAt = time.Now()
			flusher.Flush()

		case <-poll.C:
			if h.fetcher == nil {
				continue
			}
			events, newID, err := h.fetcher.FetchOutboxSince(ctx, channel, accountID, newLastID)
			if err != nil {
				logger.Ctx(ctx).Debug().Err(err).Msg("[SSE] poll fetch failed")
				continue
			}
			for _, ev := range events {
				if !writeSSEEvent(c.Writer, ev) {
					return
				}
				newLastID = ev.ID
			}
			if newID != "" {
				newLastID = newID
			}
			if len(events) > 0 {
				logger.Ctx(ctx).Info().Int("count", len(events)).Msg("[SSE] poll catchup delivered events")
				lastBusEventAt = time.Now()
			}
			flusher.Flush()
		}
	}
}

func writeSSEEvent(w http.ResponseWriter, ev SSEEvent) bool {
	if ev.ID != "" {
		if _, err := fmt.Fprintf(w, "id: %s\n", ev.ID); err != nil {
			return false
		}
	}
	if ev.Event != "" {
		if _, err := fmt.Fprintf(w, "event: %s\n", ev.Event); err != nil {
			return false
		}
	}
	dataJSON, err := json.Marshal(ev.Data)
	if err != nil {
		// 序列化失败 = 该事件必丢——Error 级日志留痕（原来只写注释帧给客户端，
		// 客户端按协议忽略注释 → 静默丢消息且服务端零感知）。
		logger.GetLogger().Error().Err(err).
			Str("event", "sse_marshal_error").
			Str("event_id", ev.ID).
			Str("conv_id", ev.ConversationID).
			Msg("[SSE] 事件 Data 序列化失败，已降级为注释帧（消息未送达，需排查 Data 内容）")
		_, _ = fmt.Fprintf(w, ":marshal_error=%v\n\n", err)
		return true
	}
	if _, err := fmt.Fprintf(w, "data: %s\n\n", dataJSON); err != nil {
		return false
	}
	return true
}

// MemoryOutboxFetcher 内存版 outbox fetcher（用于单进程 / 测试）
type MemoryOutboxFetcher struct {
	mu     sync.RWMutex
	events []SSEEvent
	nextID int
}

func NewMemoryOutboxFetcher() *MemoryOutboxFetcher {
	return &MemoryOutboxFetcher{}
}

func (m *MemoryOutboxFetcher) Push(eventType string, data map[string]any) SSEEvent {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.nextID++
	ev := SSEEvent{
		ID:        strconv.Itoa(m.nextID),
		Event:     eventType,
		Data:      data,
		Timestamp: time.Now(),
	}
	m.events = append(m.events, ev)
	if len(m.events) > SSEMaxBacklogEvents {
		m.events = m.events[len(m.events)-SSEMaxBacklogEvents:]
	}
	return ev
}

func (m *MemoryOutboxFetcher) FetchOutboxSince(_ context.Context, _, _, lastEventID string) ([]SSEEvent, string, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if lastEventID == "" {
		start := 0
		if len(m.events) > 50 {
			start = len(m.events) - 50
		}
		out := make([]SSEEvent, len(m.events)-start)
		copy(out, m.events[start:])
		newID := ""
		if len(out) > 0 {
			newID = out[len(out)-1].ID
		}
		return out, newID, nil
	}
	lastIdx := -1
	for i, ev := range m.events {
		if ev.ID == lastEventID {
			lastIdx = i
			break
		}
	}
	if lastIdx < 0 {
		start := 0
		if len(m.events) > 50 {
			start = len(m.events) - 50
		}
		out := make([]SSEEvent, len(m.events)-start)
		copy(out, m.events[start:])
		newID := ""
		if len(out) > 0 {
			newID = out[len(out)-1].ID
		}
		return out, newID, nil
	}
	out := make([]SSEEvent, len(m.events)-lastIdx-1)
	copy(out, m.events[lastIdx+1:])
	newID := ""
	if len(out) > 0 {
		newID = out[len(out)-1].ID
	}
	return out, newID, nil
}
