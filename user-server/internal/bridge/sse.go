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

// SSEBus 事件通知总线：message_hub 写入后立即通知 SSE 连接
//
// 设计要点：
//   - 按 channel:account_id 和 conversation_id 双维度订阅
//   - buffer 满时丢弃（非阻塞），消费者不应被拖慢
//   - Subscribe 返回 cancel 函数，调用方负责释放
type SSEBus struct {
	mu     sync.RWMutex
	subs   map[string][]chan SSEEvent
	buffer int
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

// Publish 发布事件到 SSE 总线
//
// 投递策略：
//  1. 优先按 conversation_id 精准投递（会话级订阅）
//  2. 再按 channel:account_id 广播（账号级订阅）
//     - 通道满时丢弃，避免阻塞发布者
func (b *SSEBus) Publish(event SSEEvent) {
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

func (h *SSEHandler) HandleOutboxSSE(c *gin.Context) {
	channel := c.Query("channel")
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

	fmt.Fprintf(c.Writer, "retry: %d\n\n", heartbeatInterval.Milliseconds())
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
	defer busCancel()

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

	for {

		if time.Since(lastBusEventAt) < 30*time.Second {
			poll.Reset(idlePollInterval)
		} else {
			poll.Reset(pollInterval)
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
