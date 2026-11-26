package bridge

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"runtime/debug"
	"strconv"
	"strings"
	"time"

	"hivemtk-user/internal/channelgw"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/metrics"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/pkg/tracing"
	"hivemtk-user/internal/pkg/utils/logger"
	"hivemtk-user/internal/service"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// HTTP 端点参数（DB 驱动，以下为 fallback 默认值）
// 实际运行时通过 service.GlobalConfigParam() 按 group=bridge 读取 DB 参数：
//
//	bridge.ingest_max_body_bytes → HTTPIngestMaxBodySize (fallback)
//	bridge.ingest_max_messages → HTTPIngestMaxMessages (fallback)
//
// 长轮询超时两个旋钮（bridge.polling_max_timeout / polling_default_timeout）
// 已下架，对应的 HTTPPollingMaxTimeout / HTTPPollingDefaultTimeout
// 常量同时删除。删除理由不是「顺手清理」，而是那两个常量全树只有 defaults_test.go
// 引用——它们的存在本身就在证明「有个旋钮改了不生效」。留着一个改了不生效的旋钮
// 比没有旋钮更糟：运营改完排查半天，问题却在代码里。
//
// 真要长轮询时：把新的兜底常量登记回 config_params（bridge.polling_max_timeout），
// 并给等待加 ctx.Done() 退出。
const (
	HTTPIngestMaxBodySize = 4 << 20
	HTTPIngestMaxMessages = 200
)

func runtimeIngestMaxBodySize(ctx context.Context) int {
	return service.GlobalConfigParam().GetInt(ctx, "bridge", "ingest_max_body_bytes", HTTPIngestMaxBodySize)
}
func runtimeIngestMaxMessages(ctx context.Context) int {
	return service.GlobalConfigParam().GetInt(ctx, "bridge", "ingest_max_messages", HTTPIngestMaxMessages)
}
func runtimeMaxAckMsgIDs(ctx context.Context) int {
	return service.GlobalConfigParam().GetInt(ctx, "bridge", "max_ack_msg_ids", 500)
}

type HTTPIngestRequest = channelgw.IngestRequest

// HTTPIngestMessage 单条消息（别名 channelgw.IngestMessage，与前端 types.js UnifiedMessage 对齐）。
//
// SenderType 仅供前端参考；服务端在入库环节按“内容是否命中本会话平台下发(outbound)”
// 权威重判自/他（见 InboxIngressService.isPlatformOutboundEcho），不再信任此字段。
// 故：命中 outbound → 视为平台自己的回显(SELF)跳过；否则一律按用户消息(CUSTOMER)处理。
type HTTPIngestMessage = channelgw.IngestMessage

// HTTPIngestResponse 上报响应（别名 channelgw.IngestResponse）。
type HTTPIngestResponse = channelgw.IngestResponse

// HTTPIngestResult 单条消息处理结果（别名 channelgw.IngestResult）。
type HTTPIngestResult = channelgw.IngestResult

// HTTPIngestPending 等待中的长轮询请求（用于将来"扩展发来多轮等 AI 一起返回"扩展）
//
// 当前实现：单条消息的 AI 回复即时在请求内完成；本结构为预留，便于将来"批量合并推理"
// （同会话多条消息 → 合并为单次 AI 调用 → 统一返回）。当前不写、不读，保留接口稳定。
type HTTPIngestPending struct {
}

type httpRequestInfo struct {
	Method         string
	Path           string
	RawQuery       string
	FullURL        string
	ContentType    string
	ContentLength  int64
	Channel        string
	AccountID      string
	ConversationID string
	TokenMasked    string
	RemoteAddr     string
	Origin         string
	UserAgent      string
	ParsedQuery    map[string]string
	BodyPreview    string
	BodySize       int
}

// collectHTTPRequestInfoWithLimit 最多读取 maxBodyBytes+1 字节；多读的 1 字节只用于
// 判定超限，不再把整个超大 body 缓存在内存或日志预览里。
func collectHTTPRequestInfoWithLimit(c *gin.Context, maxBodyBytes int) (httpRequestInfo, []byte, bool) {
	if maxBodyBytes <= 0 {
		maxBodyBytes = HTTPIngestMaxBodySize
	}
	token := c.Query("token")
	origContentLength := c.Request.ContentLength
	fullBody, bodySize, bodyPreview, tooLarge := readBodyForLog(c.Request.Body, 4096, maxBodyBytes)
	if c.Request != nil && c.Request.Body != nil {
		c.Request.Body = io.NopCloser(strings.NewReader(fullBody))
		c.Request.ContentLength = int64(len(fullBody))
	}
	return httpRequestInfo{
		Method:         c.Request.Method,
		Path:           c.Request.URL.Path,
		RawQuery:       c.Request.URL.RawQuery,
		FullURL:        c.Request.URL.String(),
		ContentType:    c.GetHeader("Content-Type"),
		ContentLength:  origContentLength,
		Channel:        c.Query("channel"),
		AccountID:      c.Query("account_id"),
		ConversationID: c.Query("conversation_id"),
		TokenMasked:    maskTokenBridge(token),
		RemoteAddr:     c.Request.RemoteAddr,
		Origin:         c.GetHeader("Origin"),
		UserAgent:      c.GetHeader("User-Agent"),
		ParsedQuery:    describeUpstreamQuery(c.Request.URL.Query()),
		BodyPreview:    bodyPreview,
		BodySize:       bodySize,
	}, []byte(fullBody), tooLarge
}

func readBodyForLog(rc io.ReadCloser, previewBytes, maxBodyBytes int) (string, int, string, bool) {
	if rc == nil {
		return "", 0, "", false
	}
	if maxBodyBytes <= 0 {
		maxBodyBytes = HTTPIngestMaxBodySize
	}
	all, err := io.ReadAll(io.LimitReader(rc, int64(maxBodyBytes)+1))
	_ = rc.Close()
	if err != nil {
		msg := fmt.Sprintf("[read body failed: %v]", err)
		return msg, 0, msg, false
	}
	tooLarge := len(all) > maxBodyBytes
	if tooLarge {
		all = all[:maxBodyBytes]
	}
	total := len(all)
	full := string(all)
	if total <= previewBytes {
		return full, total, full, tooLarge
	}
	preview := string(all[:previewBytes]) + fmt.Sprintf("... [truncated, total=%d bytes]", total)
	return full, total, preview, tooLarge
}

type BridgeIngestHandler struct {
	ingress       *service.InboxIngressService
	mockHandle    func(ctx context.Context, ev *model.MessageEvent) (*service.InboxIngressResult, error)
	mockPersist   func(ctx context.Context, ev *model.MessageEvent, direction string) error
	leadMiner     func(ctx context.Context, ev *model.MessageEvent)
	sseHandler    *SSEHandler
	outboxFetcher *outboxDBFetcher
	// inboundDispatcher 三路归一化投递收口（T015）；nil 时跳过，不改变既有行为。
	inboundDispatcher service.InboundDispatcher
}

// NewBridgeIngestHandler 构造 HTTP ingest 处理器
func NewBridgeIngestHandler(ingress *service.InboxIngressService) *BridgeIngestHandler {
	h := &BridgeIngestHandler{ingress: ingress}
	h.outboxFetcher = &outboxDBFetcher{}
	h.sseHandler = NewSSEHandler(h.outboxFetcher)
	return h
}

// SetLeadMiner 设置线索挖掘回调（Douyin/TikTok 等群聊渠道用）
func (h *BridgeIngestHandler) SetLeadMiner(fn func(ctx context.Context, ev *model.MessageEvent)) {
	h.leadMiner = fn
}

// @Summary      桥接出站 SSE 长连接拉取（outbox 轮询推送）
// @Tags      bridge-ingest-handler
// @Router    /api/bridge/outbox/sse [get]
func (h *BridgeIngestHandler) HandleOutboxSSE(c *gin.Context) {
	if h.sseHandler == nil {
		if h.outboxFetcher == nil {
			h.outboxFetcher = &outboxDBFetcher{}
		}
		h.sseHandler = NewSSEHandler(h.outboxFetcher)
	}
	h.sseHandler.HandleOutboxSSE(c)
}

// OutboxQuerier 由 repository.MessageHubRepository 实现的最小接口
//
// 解耦原因：bridge 包不能直接 import repository（避免与 service 形成循环依赖），
// 故在 bridge 内定义最小化接口，由 repository.MessageHubRepository 隐式实现。
type OutboxQuerier interface {
	FetchOutboundSince(ctx context.Context, channel, accountID string, sinceID uint64, limit int) ([]model.MessageHub, error)
}

// OutboxOwedQuerier 「仍欠交付」出站集合查询（批11 §3.7-2），同理由 repository 隐式实现。
//
// 与 OutboxQuerier 的分工就是本批要修的语义差：游标查询答不出「这条还没交付」，
// 它只答「id 比游标大的」——推送失败/发送超时的行一旦落在游标之下就再也不被看见。
type OutboxOwedQuerier interface {
	FetchOutboundUndelivered(ctx context.Context, channel, accountID string, claimTimeout time.Duration, limit int) ([]model.MessageHub, error)
}

type outboxDBFetcher struct {
	querier OutboxQuerier
	owed    OutboxOwedQuerier
}

func (f *outboxDBFetcher) SetQuerier(q OutboxQuerier) {
	f.querier = q
}

// SetOwedQuerier 注入未交付查询器；未注入时 FetchOutboxSince 退回游标旧语义（由装配处 Warn 留痕）。
func (f *outboxDBFetcher) SetOwedQuerier(q OutboxOwedQuerier) {
	f.owed = q
}

// SetOutboxQuerier 注入 OutboxQuerier（由 router 在装配完成后调用一次）。
//
// 原生 repository 先包装为 service，统一执行当前授权与认领；可选查询口只影响查询方式，
// 不得绕开消费裁决。未装配认领服务时，总线和补拉均保持阻塞并记录原因。
func (h *BridgeIngestHandler) SetOutboxQuerier(q OutboxQuerier) {
	// 原生仓储必须经过 service 的消费裁决，不能直接装作总线认领器。
	if store, ok := q.(service.OutboundDeliveryRepository); ok {
		q = service.NewOutboundDeliveryService(store)
	}
	owed, hasOwed := q.(OutboxOwedQuerier)
	if h.outboxFetcher != nil {
		h.outboxFetcher.SetQuerier(q)
		h.outboxFetcher.SetOwedQuerier(owed)
		if !hasOwed {
			logger.GetLogger().Error().
				Msg("[SSE] outbox 查询器缺未交付查询口，使用兼容游标查询，关闭即时出站推送")
		}
	}
	claimer, _ := q.(OutboundPushClaimer)
	GlobalSSEBus.setOutboundClaimer(claimer, !hasOwed)
}

// FetchOutboxSince 取「此刻该发给这条连接」的出站事件。
//
// 两条语义，取决于注入方是否实现了未交付查询（装配缺件时 SetOutboxQuerier 已 Error 留痕）：
//   - owed（生产主路径，批11 §3.7-2）：按状态取仍欠交付的行，**与游标无关**。
//     每条先过 GlobalSSEBus.claimForPush 认领门，抢不到就不发（同一行只可能有一条路径发出去）。
//     交付完成的行靠 ack 离开集合，所以这条查询天然收敛、也不会重放历史。
//   - 游标（兼容查询口）：id > 游标，仍须逐行经过同一消费裁决和认领门。
//
// newLastID 只推进至本次实际获准交付的事件；拒绝不能让兼容游标跳过历史待发事实。
func (f *outboxDBFetcher) FetchOutboxSince(ctx context.Context, channel, accountID, lastEventID string) ([]SSEEvent, string, error) {
	if f.querier == nil {
		return nil, "", nil
	}
	if f.owed != nil {
		rows, err := f.owed.FetchOutboundUndelivered(ctx, channel, accountID, service.InboxOutboundClaimTimeout, sseBacklogLimit)
		if err != nil && len(rows) == 0 {
			return nil, "", err
		}
		events := make([]SSEEvent, 0, len(rows))
		for _, row := range rows {
			ev := buildOutboundEvent(row)
			// 认领门与总线路径同一个函数：补拉发出去的行同样占 inflight，
			// 于是轮询拿不到它（双投收口），超时未 ack 又会回到欠交付集合（重投收口）。
			if !GlobalSSEBus.claimForPush(ctx, ev, false) {
				continue
			}
			events = append(events, ev)
		}
		// 游标只作协议记账（id: 帧 / Last-Event-ID），不再是读取闸门
		newLastID := lastEventID
		if len(events) > 0 {
			newLastID = events[len(events)-1].ID
		}
		return events, newLastID, err
	}

	var sinceID uint64
	if lastEventID != "" {
		id, err := strconv.ParseUint(lastEventID, 10, 64)
		if err != nil {
			// B8：非法游标 ≠ 无游标。旧行为把垃圾游标当 0 → 全量回放
			// 最多 200 条已投递 outbound → 浏览器重复转发。fail-closed：返回 0 行、
			// 游标原样保留（连接继续收增量），Warn 留现场。空游标（首连）仍走全量补齐。
			logger.Ctx(ctx).Warn().
				Str("channel", channel).
				Str("account_id", accountID).
				Str("last_event_id", lastEventID).
				Msg("[SSE] 非法 Last-Event-ID，游标 fail-closed 不回放大批量历史")
			return nil, lastEventID, nil
		}
		sinceID = id
	}

	rows, err := f.querier.FetchOutboundSince(ctx, channel, accountID, sinceID, sseBacklogLimit)
	if err != nil && len(rows) == 0 {
		return nil, "", err
	}
	events := make([]SSEEvent, 0, len(rows))
	for _, row := range rows {
		// 旧游标查询也必须经过当前消费裁决与认领，缺装配不能直接放行。
		ev := buildOutboundEvent(row)
		claimed, blocked := GlobalSSEBus.tryClaimForPush(ctx, ev, false)
		if blocked {
			break
		}
		if claimed {
			events = append(events, ev)
		}
	}

	newLastID := lastEventID
	if len(events) > 0 {
		newLastID = events[len(events)-1].ID
	}
	return events, newLastID, err
}

// sseBacklogLimit 单次补拉上限（两条语义共用同一个数，避免兜底路径比主路径更宽）。
const sseBacklogLimit = 200

func buildOutboundEvent(row model.MessageHub) SSEEvent {
	return BuildOutboundSSEEvent(OutboundEventData{
		HubID:          uint64(row.ID),
		MsgID:          row.MsgID,
		Platform:       row.Platform,
		AccountID:      row.AccountID,
		ConversationID: row.ConversationID,
		Content:        row.Content,
		MsgType:        row.MsgType,
		MediaURL:       row.MediaURL,
		ReceiverID:     row.ReceiverID,
		IsAIReply:      row.IsAIReply,
		Extra:          row.Extra,
		CreatedAt:      row.CreatedAt,
	})
}

// NewBridgeIngestHandlerWithMock 构造带 mock 的 HTTP ingest 处理器（仅测试用）
//
// mockHandle / mockPersist 任一为 nil 时，对应路径回退到真实 ingress（若 ingress 也 nil 则 panic）。
// 用于本地跑通 HTTP 长轮询 e2e，无需 DB/Redis/AI 引擎。
func NewBridgeIngestHandlerWithMock(
	mockHandle func(ctx context.Context, ev *model.MessageEvent) (*service.InboxIngressResult, error),
	mockPersist func(ctx context.Context, ev *model.MessageEvent, direction string) error,
) *BridgeIngestHandler {
	return &BridgeIngestHandler{mockHandle: mockHandle, mockPersist: mockPersist}
}

func (h *BridgeIngestHandler) callPersistHistory(ctx context.Context, ev *model.MessageEvent, direction string) error {
	if h.mockPersist != nil {
		return h.mockPersist(ctx, ev, direction)
	}
	return h.ingress.PersistBridgeHistory(ctx, ev, direction)
}

// @Summary      抖音网页
// @Tags      bridge-ingest-handler
// @Router    /api/bridge/ingest [post]
func (h *BridgeIngestHandler) HandleHTTPIngest(c *gin.Context) {
	maxBodySize := runtimeIngestMaxBodySize(c.Request.Context())
	if maxBodySize <= 0 {
		maxBodySize = HTTPIngestMaxBodySize
	}
	if c.Request.ContentLength > int64(maxBodySize) {
		logger.Ctx(c.Request.Context()).Warn().
			Str("module", "bridge").
			Str("event", "http_ingest_body_too_large").
			Str("full_url", c.Request.URL.String()).
			Int64("content_length", c.Request.ContentLength).
			Msg("[Bridge HTTP] body 超过上限")
		c.JSON(http.StatusRequestEntityTooLarge, HTTPIngestResponse{
			OK: false, Reason: fmt.Sprintf("body too large (max %d bytes)", maxBodySize),
		})
		return
	}
	info, rawBody, tooLarge := collectHTTPRequestInfoWithLimit(c, maxBodySize)
	if tooLarge {
		logger.Ctx(c.Request.Context()).Warn().
			Str("module", "bridge").
			Str("event", "http_ingest_body_too_large").
			Str("full_url", info.FullURL).
			Int("body_size", info.BodySize).
			Msg("[Bridge HTTP] body 超过上限")
		c.JSON(http.StatusRequestEntityTooLarge, HTTPIngestResponse{
			OK: false, Reason: fmt.Sprintf("body too large (max %d bytes)", maxBodySize),
		})
		return
	}
	channel := info.Channel
	accountID := info.AccountID
	conversationID := info.ConversationID
	ctx0 := c.Request.Context()
	start := time.Now()
	bm := metrics.GetBridge()

	var req HTTPIngestRequest
	bodyBindErr := json.Unmarshal(rawBody, &req)
	if bodyBindErr == nil {
		if channel == "" {
			channel = req.Channel
		}
		if accountID == "" {
			accountID = req.AccountID
		}
		if conversationID == "" {
			conversationID = req.ConversationID
		}
		// 平铺字段守卫（T2 验收缺陷，2026-11-04）：顶层没有 "messages" 键、
		// 且带了 content/sender_type/events 等平铺字段 = 调用方协议用错。
		// 旧代码 Bind 成功但 Messages 为空后一路 OK:true，调用方以为上报成功
		// 实际一条没进库。显式 "messages":[]（扩展空批次心跳）保持既有契约：
		// ok=true + ingested=[]。
		if !bytes.Contains(rawBody, []byte(`"messages"`)) && len(rawBody) > 2 {
			logger.Ctx(ctx0).Warn().
				Str("module", "bridge").
				Str("event", "http_ingest_flat_payload").
				Str("full_url", info.FullURL).
				Str("body_preview", info.BodyPreview).
				Msg("[Bridge HTTP] body 缺 messages 键：请按 {channel,account_id,conversation_id,messages:[{...}]} 上报")
			bm.IngestErrors.WithLabel(NormalizeBridgeChannel(channel), "messages_required").Inc()
			c.JSON(http.StatusBadRequest, HTTPIngestResponse{
				OK:     false,
				Reason: `messages required: body must be {"messages":[{...}]}; flat top-level content/sender_type fields are not accepted`,
			})
			return
		}
	}

	channelNorm := NormalizeBridgeChannel(channel)

	bridgeHTTPIngestError := func(errCode string) {
		// 标签必须用规范名：成功计数（IngestTotal/IngestDuration）都按 channelNorm 上报，
		// 错误计数若按原始入参打标，同一个渠道的"抖音网页"会被拆成 douyin_web 与 douyin 两条曲线。
		bm.IngestErrors.WithLabel(channelNorm, errCode).Inc()
		bm.IngestDuration.WithLabel(channelNorm).Observe(float64(time.Since(start).Milliseconds()))
	}

	logger.Ctx(ctx0).Info().
		Str("module", "bridge").
		Str("event", "http_ingest_request").
		Str("full_url", info.FullURL).
		Str("method", info.Method).
		Str("path", info.Path).
		Str("raw_query", info.RawQuery).
		Str("content_type", info.ContentType).
		Int64("content_length", info.ContentLength).
		Str("channel", channel).
		Str("channel_norm", channelNorm).
		Str("account_id", accountID).
		Str("conversation_id", conversationID).
		Str("token", info.TokenMasked).
		Str("remote_addr", info.RemoteAddr).
		Str("origin", info.Origin).
		Str("user_agent", info.UserAgent).
		Interface("parsed_query", info.ParsedQuery).
		Str("body_preview", info.BodyPreview).
		Int("body_size", info.BodySize).
		Msg("[Bridge HTTP] 收到 ingest 请求（完整 URL + 全部参数 + body 预览）")

	if bodyBindErr != nil && len(rawBody) > 0 {
		logger.Ctx(ctx0).Warn().
			Str("module", "bridge").
			Str("event", "http_ingest_bind_json_failed").
			Err(bodyBindErr).
			Str("full_url", info.FullURL).
			Msg("[Bridge HTTP] body JSON 解析失败")
		c.JSON(http.StatusBadRequest, HTTPIngestResponse{
			OK:     false,
			Reason: "invalid json body: " + bodyBindErr.Error(),
		})
		return
	}

	identifyCode := bridgeIdentifyHintFromRequest(c.Request)
	needsIdentity := channel == "" || accountID == ""
	if needsIdentity && identifyCode == "" {
		if channel == "" {
			bridgeHTTPIngestError("channel_required")
			c.JSON(http.StatusBadRequest, HTTPIngestResponse{OK: false, Reason: "channel required"})
			return
		}
		bridgeHTTPIngestError("account_id_required")
		c.JSON(http.StatusBadRequest, HTTPIngestResponse{OK: false, Reason: "account_id required (or provide X-Channel-Identify-Code)"})
		return
	}
	if channel != "" && !IsBridgeChannel(channel) {
		bridgeHTTPIngestError("unsupported_channel")
		c.JSON(http.StatusBadRequest, HTTPIngestResponse{OK: false, Reason: "unsupported bridge channel"})
		return
	}

	// 租户自证必须排在任何写之前；缺省 channel/account 仅可由合法 identify_code 补全。
	reqCtx, terr := bindBridgeTenant(c, channelNorm, accountID)
	if terr != nil {
		logger.Ctx(reqCtx).Warn().
			Err(terr).
			Str("module", "bridge").
			Str("event", "http_ingest_tenant_unresolved").
			Str("channel", channelNorm).
			Str("account_id", accountID).
			Str("remote_addr", info.RemoteAddr).
			Msg("[Bridge HTTP] 租户归属解析失败，拒绝 ingest")
		bridgeHTTPIngestError("tenant_unresolved")
		c.JSON(bridgeTenantHTTPStatus(terr), HTTPIngestResponse{OK: false, Reason: terr.Error()})
		return
	}
	ctx0 = reqCtx
	if ident, ok := BridgeIdentityFromContext(c); ok {
		if channel == "" {
			channel = ident.Channel
		}
		if accountID == "" {
			accountID = ident.AccountID
		}
		channelNorm = NormalizeBridgeChannel(channel)
	}
	if channel == "" {
		bridgeHTTPIngestError("channel_required")
		c.JSON(http.StatusBadRequest, HTTPIngestResponse{OK: false, Reason: "channel required"})
		return
	}
	if accountID == "" {
		bridgeHTTPIngestError("account_id_required")
		c.JSON(http.StatusBadRequest, HTTPIngestResponse{OK: false, Reason: "account_id required (or provide X-Channel-Identify-Code)"})
		return
	}
	if !IsBridgeChannel(channel) {
		bridgeHTTPIngestError("unsupported_channel")
		c.JSON(http.StatusBadRequest, HTTPIngestResponse{OK: false, Reason: "unsupported bridge channel"})
		return
	}

	// 必须在截断、账号登记、历史保存和实时处理之前验证所有消息。
	if err := validateHTTPIngestScope(&req, channelNorm, accountID, conversationID); err != nil {
		bridgeHTTPIngestError("scope_mismatch")
		c.JSON(http.StatusBadRequest, HTTPIngestResponse{OK: false, Reason: err.Error()})
		return
	}
	maxMessages := runtimeIngestMaxMessages(ctx0)
	if len(req.Messages) > maxMessages {
		logger.Ctx(ctx0).Warn().
			Str("module", "bridge").
			Str("event", "http_ingest_truncated").
			Str("full_url", info.FullURL).
			Int("messages", len(req.Messages)).
			Int("truncated_to", maxMessages).
			Msg("[Bridge HTTP] 消息数超过上限，截断处理（剩余由下轮巡检补齐）")
		req.Messages = req.Messages[:maxMessages]
	}
	// 完整批次 scope 校验通过后，所有消费者只接收截断后的消息。
	h.deliverInbound(ctx0, channelNorm, accountID, conversationID, req.Messages)

	traceID := c.GetHeader("X-Trace-Id")
	if traceID == "" {
		if v, ok := c.Get("trace_id"); ok {
			traceID, _ = v.(string)
		}
	}
	ctx := c.Request.Context()
	if traceID != "" {
		ctx = logger.WithTraceID(ctx, traceID)
	} else {
		ctx = logger.WithTraceID(ctx, "")
	}
	ctx = logger.WithModule(ctx, "bridge")

	if GlobalBridgeAccountRepo != nil && (req.AgentID > 0 || req.AccountName != "" || len(req.Messages) > 0) {
		up := BridgeAccountUpsert{
			// 入站没有用户身份：账号登记为租户资产（user_id=0），
			// 管理面的可见性/归属判据见 account_service.go 与 router 的 OwnershipChecker。
			UserID:      0,
			Channel:     NormalizeBridgeChannel(channel),
			AccountID:   accountID,
			AgentID:     req.AgentID,
			AccountName: req.AccountName,
			Status:      "online",
		}
		// bridge_accounts 是租户表：入站 ctx 已由 bindBridgeTenant 绑定账号租户，
		// 异步 Update 必须带着它（只解绑取消、不丢租户），否则 on 模式写入被拒。
		bgCtx := tenant.BackgroundWithTenant(ctx)
		go func() {
			defer func() {
				if r := recover(); r != nil {
					logger.Ctx(bgCtx).Error().
						Interface("panic", r).
						Str("stack", string(debug.Stack())).
						Str("module", "bridge").
						Str("channel", up.Channel).
						Str("account_id", up.AccountID).
						Msg("[Bridge] async HTTP Upsert panic recovered")
				}
			}()
			upCtx, cancel := context.WithTimeout(bgCtx, 3*time.Second)
			defer cancel()
			if err := GlobalBridgeAccountRepo.Upsert(upCtx, up); err != nil {
				// 归属冲突也要留痕：这正是"别的用户试图上报本账号"的审计信号，
				// 不能把它当预期内静默吞掉（注释说要记日志，代码里没有）。
				event := "http_upsert_failed"
				if errors.Is(err, ErrAccountOwnedByOther) {
					event = "http_upsert_ownership_conflict"
				}
				logger.Ctx(context.Background()).Warn().Err(err).
					Str("module", "bridge").
					Str("event", event).
					Str("channel", up.Channel).Str("account_id", up.AccountID).
					Msg("bridge HTTP Upsert 未完成（non-fatal）")
			}
		}()
	}

	resp := &HTTPIngestResponse{
		OK:         true,
		Ingested:   make([]*HTTPIngestResult, 0, len(req.Messages)),
		ServerTime: time.Now().UnixMilli(),
	}

	var batchEvents []*model.MessageEvent
	batchIdxMap := make(map[int]int)

	for i, m := range req.Messages {
		if m == nil {
			continue
		}
		if m.Channel == "" {
			m.Channel = req.Channel
		}
		if m.AccountID == "" {
			m.AccountID = req.AccountID
		}
		if m.ConversationID == "" {
			m.ConversationID = req.ConversationID
		}
		if len(m.History) > 0 {
			liveEventID := m.EventID
			for hi, it := range m.History {
				if it == nil {
					continue
				}
				if liveEventID != "" && it.EventID == liveEventID {
					continue
				}
				item := historyItemToEvent(httpMessageToUnified(m), it)
				// 父消息的 sender_type 只描述当前这条，不描述整段历史；
				// 用它改写历史方向会把客户问题也标成 outbound。
				histDir := it.Direction
				if histDir == "" {
					histDir = "inbound"
				}
				if err := h.callPersistHistory(ctx, item, histDir); err != nil {
					logger.Ctx(ctx).Warn().Err(err).Str("module", "bridge").
						Str("event_id", it.EventID).Str("conv_id", m.ConversationID).
						Int("history_idx", hi).
						Msg("[Bridge HTTP] history context persist failed")
				}
			}
		}
		ev := httpMessageToEvent(m)
		if req.InternalOnly {
			if err := h.callPersistHistory(ctx, ev, "inbound"); err != nil {
				logger.Ctx(ctx).Warn().Err(err).Str("module", "bridge").
					Str("event_id", m.EventID).Str("conv_id", m.ConversationID).
					Msg("[Bridge HTTP] internal_only persist failed")
			}
			resp.Ingested = append(resp.Ingested, &HTTPIngestResult{
				EventID:  m.EventID,
				Accepted: true,
				Reason:   "internal_only: persisted only",
			})
			continue
		}
		batchIdxMap[i] = len(batchEvents)
		batchEvents = append(batchEvents, ev)
	}

	anyAIQueued := false
	if len(batchEvents) > 0 {
		batchResult, err := h.callHandleIngressBatch(ctx, batchEvents)
		if err != nil {
			logger.Ctx(ctx).Warn().Err(err).Str("module", "bridge").
				Msg("[Bridge HTTP] HandleIngressBatch failed")
		}
		if batchResult != nil {
			for origIdx, batchIdx := range batchIdxMap {
				if batchIdx >= len(batchResult.PerEvent) {
					continue
				}
				result := batchResult.PerEvent[batchIdx]
				if result == nil {
					continue
				}
				m := req.Messages[origIdx]
				r := &HTTPIngestResult{
					EventID:   m.EventID,
					Accepted:  result.Accepted,
					AIHandled: result.QueuedForAI,
					Reason:    result.Reason,
				}
				if isIngestDuplicate(result.Reason) {
					r.Duplicate = true
				}
				if result.SessionID != "" {
					resp.SessionID = result.SessionID
				}
				resp.Ingested = append(resp.Ingested, r)
				if result.QueuedForAI {
					anyAIQueued = true
				}
			}
			_ = batchResult.TriggeredAI
		}
	}

	// 可信资格已随正常入站消息写入；后台持久任务补齐和挖掘。
	// Accepted也可能代表自回显/重复，不在HTTP路径据此调用矿工。

	ingestedCount := len(resp.Ingested)
	logger.Ctx(ctx).Info().
		Str("module", "bridge").
		Str("event", "http_ingest_response").
		Str("full_url", info.FullURL).
		Str("channel", channelNorm).
		Str("channel_raw", channel).
		Str("account_id", accountID).
		Str("conv_id", conversationID).
		Int("ingested_count", ingestedCount).
		Int("any_ai_queued", boolToInt(anyAIQueued)).
		Int64("server_time_ms", resp.ServerTime).
		Msg("[Bridge HTTP] ingest 响应（每条结果）")

	bm.IngestTotal.WithLabel(channelNorm, strconv.FormatUint(uint64(req.AgentID), 10)).Inc()
	bm.IngestDuration.WithLabel(channelNorm).Observe(float64(time.Since(start).Milliseconds()))

	c.JSON(http.StatusOK, resp)
}

// BridgeOutboxMessage 下发队列中的一条待发消息（别名 channelgw.OutboxMessage，HTTP/WS 共用序列化）。
type BridgeOutboxMessage = channelgw.OutboxMessage

type BridgeOutboxAckItem struct {
	MsgID          string `json:"msg_id"`
	ConversationID string `json:"conversation_id"`
	Status         string `json:"status,omitempty"`
	Error          string `json:"error,omitempty"`
}

type BridgeOutboxAckRequest struct {
	V      int                   `json:"v,omitempty"`
	MsgIDs []string              `json:"msg_ids"`
	Status string                `json:"status"`
	Items  []BridgeOutboxAckItem `json:"items"`
}

func writeOutboxJSON(c *gin.Context, hubs []*model.MessageHub) {
	msgs := make([]BridgeOutboxMessage, 0, len(hubs))
	for _, hub := range hubs {
		msgs = append(msgs, BridgeOutboxMessage{
			MsgID:          hub.MsgID,
			ConversationID: hub.ConversationID,
			MsgType:        hub.MsgType,
			Content:        hub.Content,
			MediaURL:       hub.MediaURL,
			SenderID:       hub.SenderID,
			ReceiverID:     hub.ReceiverID,
			IsAIReply:      hub.IsAIReply,
			CreatedAt:      hub.CreatedAt,
			Extra:          hub.Extra,
		})
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok", "messages": msgs})
}

func isIngestDuplicate(reason string) bool {
	return channelgw.IsDuplicateReason(reason)
}

// P0-c：Bridge 侧硬编码 leadMiningChannels map 与 AddLeadMiningChannel 已删除，
// 发掘渠道资格单源自 channelgw.Default 的 MineMode 投影（service.SetLeadMiningChannels）。
// 新渠道只需在 channelgw.Default 注册带 MineMode 的 ChannelSpec 即自动纳入发掘。

func _bridgeOutboxHubsForLog(hubs []*model.MessageHub) []map[string]any {
	out := make([]map[string]any, 0, len(hubs))
	for _, h := range hubs {
		if h == nil {
			continue
		}
		out = append(out, map[string]any{
			"msg_id":          h.MsgID,
			"conversation_id": h.ConversationID,
			"platform":        h.Platform,
			"account_id":      h.AccountID,
			"sender_id":       h.SenderID,
			"receiver_id":     h.ReceiverID,
			"msg_type":        h.MsgType,
			"content":         h.Content,
			"media_url":       h.MediaURL,
			"is_ai_reply":     h.IsAIReply,
			"status":          h.Status,
			"sent_at":         h.SentAt,
			"created_at":      h.CreatedAt,
			"extra":           h.Extra,
		})
	}
	return out
}

// @Summary      客户端活着
// @Tags      bridge-ingest-handler
// @Router    /api/bridge/outbox [get]
func (h *BridgeIngestHandler) GetBridgeOutbox(c *gin.Context) {
	channel := NormalizeBridgeChannel(c.Query("channel"))
	accountID := c.Query("account_id")
	start := time.Now()
	bm := metrics.GetBridge()
	bridgeOutboxError := func(errCode string) {
		bm.IngestErrors.WithLabel(channel, errCode).Inc()
	}
	identifyCode := bridgeIdentifyHintFromRequest(c.Request)
	if (channel == "" || accountID == "") && identifyCode == "" {
		if channel == "" {
			bridgeOutboxError("channel_required")
			c.JSON(http.StatusBadRequest, gin.H{"status": "error", "message": "channel required"})
			return
		}
		bridgeOutboxError("account_id_required")
		c.JSON(http.StatusBadRequest, gin.H{"status": "error", "message": "account_id required (or provide X-Channel-Identify-Code)"})
		return
	}
	if channel != "" && !IsBridgeChannel(channel) {
		bridgeOutboxError("unsupported_channel")
		c.JSON(http.StatusBadRequest, gin.H{"status": "error", "message": "unsupported bridge channel"})
		return
	}
	ctx, terr := bindBridgeTenant(c, channel, accountID)
	if terr != nil {
		bridgeOutboxError("tenant_unresolved")
		writeBridgeTenantError(c, terr)
		return
	}
	if ident, ok := BridgeIdentityFromContext(c); ok {
		if channel == "" {
			channel = ident.Channel
		}
		if accountID == "" {
			accountID = ident.AccountID
		}
	}
	if accountID == "" {
		bridgeOutboxError("account_id_required")
		c.JSON(http.StatusBadRequest, gin.H{
			"status":  "error",
			"message": "account_id required (extension must capture account from DOM before polling outbox)",
		})
		return
	}
	if channel == "" {
		bridgeOutboxError("channel_required")
		c.JSON(http.StatusBadRequest, gin.H{"status": "error", "message": "channel required"})
		return
	}
	// 轮询本身就是「这个账号还在同步」的证据，必须刷在线位：长轮询模式下扩展不建 SSE 订阅，
	// 不刷的话补投门（订阅 OR 最近同步）两个信号都读不到，延后出站会被逐轮跳过、永不补投。
	// 放在取消息之前：outbox 空与「有待投」在这里语义相同，都是"客户端活着"。
	touchBridgeAccountOnline(ctx, channel, accountID)
	logger.Ctx(ctx).Info().
		Str("module", "bridge").
		Str("event", "http_outbox_request").
		Str("full_url", c.Request.URL.String()).
		Str("method", c.Request.Method).
		Str("path", c.Request.URL.Path).
		Str("raw_query", c.Request.URL.RawQuery).
		Str("channel", channel).
		Str("account_id", accountID).
		Str("remote_addr", c.Request.RemoteAddr).
		Interface("parsed_query", describeUpstreamQuery(c.Request.URL.Query())).
		Msg("[Bridge HTTP] 收到 outbox 请求（完整 URL + 全部参数）")

	limit := 50
	if q := c.Query("limit"); q != "" {
		if n, err := strconv.Atoi(q); err == nil && n > 0 {
			limit = n
		}
	}
	if limit > 200 {
		limit = 200
	}
	claimStarted := time.Now()
	hubs, err := h.ingress.ClaimPendingOutbound(ctx, channel, accountID, limit)
	claimCompleted := time.Now()
	if err != nil {
		var denied interface{ CapabilityReasonCode() string }
		switch {
		case len(hubs) > 0:
			logger.Ctx(ctx).Warn().Err(err).Int("claimed_count", len(hubs)).Msg("[Bridge] later outbox page failed, returning committed claims")
		case errors.As(err, &denied):
			c.JSON(http.StatusForbidden, gin.H{"status": "blocked", "reason_code": denied.CapabilityReasonCode(), "message": "outbound delivery blocked"})
			return
		default:
			logger.Ctx(ctx).Error().Err(err).Str("module", "bridge").Str("channel", channel).Msg("[Bridge] ListPendingOutbound failed")
			c.JSON(http.StatusInternalServerError, gin.H{"status": "error", "message": "query outbox failed"})
			return
		}
	}
	tracing.RecordDownlinkClaimBatch(ctx, channel, accountID, hubs, &claimStarted, &claimCompleted)
	logger.Ctx(ctx).Info().
		Str("module", "bridge").
		Str("event", "http_outbox_response").
		Str("full_url", c.Request.URL.String()).
		Str("channel", channel).
		Str("account_id", accountID).
		Int("limit", limit).
		Int("messages_count", len(hubs)).
		Interface("messages", _bridgeOutboxHubsForLog(hubs)).
		Msg("[Bridge HTTP] outbox 响应（完整待发消息列表，含 content）")
	bm.OutboxFetched.WithLabel(channel, accountID).Add(uint64(len(hubs)))
	bm.OutboxDuration.WithLabel(channel).Observe(float64(time.Since(start).Milliseconds()))
	writeOutboxJSON(c, hubs)
}

type BridgeOutboxAckResponse struct {
	Status           string `json:"status"`
	AffectedCount    int    `json:"affected_count"`
	AckedItemsCount  int    `json:"acked_items_count"`
	FailedItemsCount int    `json:"failed_items_count"`
	// UnknownItemsCount CH-37b 刀4（R2/R6）：翻转为 unknown 终态（结果不明）的行数，
	// additive 字段——旧客户端忽略未知 JSON 键不受影响。
	UnknownItemsCount int                       `json:"unknown_items_count"`
	DuplicateCount    int                       `json:"duplicate_count"`
	NotFoundCount     int                       `json:"not_found_count"`
	NotInScopeCount   int                       `json:"not_in_scope_count"`
	Items             []service.AckOutboundItem `json:"items"`
}

// @Summary      欠交付
// @Tags      bridge-ingest-handler
// @Router    /api/bridge/outbox/ack [post]
func (h *BridgeIngestHandler) AckBridgeOutbox(c *gin.Context) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 1<<20)
	// 三个 HTTP 入口里只有这里曾经不归一渠道（ingest 在 httpMessageToEvent 里归一、
	// outbox/SSE 在入口归一）。`message_hub.platform` 只存规范名，别名 ack 取不到行 ⇒
	// 每格都报 not_found 却仍回 200 status:ok，那一行离不开"欠交付"集合：
	// 30s 认领租约到期后被重新下发，最多 20 次（约 10 分钟）才落 failed —— 客户第一条
	// 其实早就收到了，剩下的 19 遍是刷屏，最后台账还把它记成"发送失败"。
	channel := NormalizeBridgeChannel(c.Query("channel"))
	accountID := c.Query("account_id")
	if ident, ok := BridgeIdentityFromContext(c); ok {
		if channel == "" {
			channel = ident.Channel
		}
		if accountID == "" {
			accountID = ident.AccountID
		}
	}
	start := time.Now()
	bm := metrics.GetBridge()
	if accountID == "" || channel == "" {
		// bind to resolve identity_code
		if _, terr := bindBridgeTenant(c, channel, accountID); terr == nil {
			if ident, ok := BridgeIdentityFromContext(c); ok {
				if channel == "" {
					channel = ident.Channel
				}
				if accountID == "" {
					accountID = ident.AccountID
				}
			}
		}
	}
	if accountID == "" {
		bm.IngestErrors.WithLabel(channel, "account_id_required").Inc()
		c.JSON(http.StatusBadRequest, gin.H{"status": "error", "message": "account_id required (or provide X-Channel-Identify-Code)"})
		return
	}
	if channel == "" {
		bm.IngestErrors.WithLabel(channel, "channel_required").Inc()
		c.JSON(http.StatusBadRequest, gin.H{"status": "error", "message": "channel required"})
		return
	}
	if _, terr := bindBridgeTenant(c, channel, accountID); terr != nil {
		bm.IngestErrors.WithLabel(channel, "tenant_unresolved").Inc()
		writeBridgeTenantError(c, terr)
		return
	}
	var req BridgeOutboxAckRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"status": "error", "message": "invalid body"})
		return
	}

	if len(req.Items) > 0 {
		for _, it := range req.Items {
			if it.MsgID == "" || it.ConversationID == "" {
				c.JSON(http.StatusBadRequest, gin.H{
					"status":  "error",
					"message": "conversation_id required (v2 items[] must carry msg_id + conversation_id)",
				})
				return
			}
			// v2 的 status 是逐项给的，下面 v1 那道校验拦不住它：未知状态会一路走到 service 报错，
			// 这里回 500。而 v2 是按 (conversation_id, status) 分组后遍历 map 逐组落库的，
			// 遍历序随机 ⇒ 同一个请求有时好项已落库、有时一格没落（实测 20 次 15 中 5 空），
			// 客户端只看到 500 无从判断该重试还是该改参数。没落库的那些行留在"欠交付"集合里，
			// 30s 认领租约到期后被重新下发，最多 20 次。所以这里在任何写之前先把整批的
			// status 验完：要么全收，要么一格不动并报出到底是哪个值不认。
			st := it.Status
			if st == "" {
				st = model.BridgeAckStatusDelivered
			}
			// CH-37b 刀4（R2/R6）：校验扩为三值（delivered/failed/unknown）。unknown=
			// 「已提交但结果不明」真终态（永不自动重发，对账走显式迁移），与其余值同样
			// 在任何写之前整批验完，维持「要么全收，要么一格不动」的批语义。
			if !model.IsBridgeAckTerminalStatus(st) {
				c.JSON(http.StatusBadRequest, gin.H{
					"status":  "error",
					"message": fmt.Sprintf("invalid status: %q (must be delivered, failed or unknown)", it.Status),
				})
				return
			}
		}
	}

	maxAckMsgIDs := runtimeMaxAckMsgIDs(c.Request.Context())
	if len(req.MsgIDs) > maxAckMsgIDs || len(req.Items) > maxAckMsgIDs {
		c.JSON(http.StatusBadRequest, gin.H{
			"status":  "error",
			"message": fmt.Sprintf("too many msg_ids (max %d per request)", maxAckMsgIDs),
		})
		return
	}
	ctx := c.Request.Context()
	logger.Ctx(ctx).Info().
		Str("module", "bridge").
		Str("event", "http_outbox_ack_request").
		Str("full_url", c.Request.URL.String()).
		Str("method", c.Request.Method).
		Str("path", c.Request.URL.Path).
		Str("raw_query", c.Request.URL.RawQuery).
		Str("channel", channel).
		Str("account_id", accountID).
		Str("remote_addr", c.Request.RemoteAddr).
		Interface("parsed_query", describeUpstreamQuery(c.Request.URL.Query())).
		Str("body_status", req.Status).
		Int("body_msg_ids_count", len(req.MsgIDs)).
		Interface("body_msg_ids", req.MsgIDs).
		Msg("[Bridge HTTP] 收到 outbox ack 请求（完整 URL + 全部参数 + 完整 msg_ids 列表）")

	terminalStatus := req.Status
	if terminalStatus == "" {
		terminalStatus = model.BridgeAckStatusDelivered
	}

	// CH-37b 刀4（R2/R6）：v1 body status 同步扩为三值（delivered/failed/unknown）。
	// unknown 走下方同一 AckOutboundDeliveredDetailed 路径，行级落 unknown 终态。
	if !model.IsBridgeAckTerminalStatus(terminalStatus) {
		c.JSON(http.StatusBadRequest, gin.H{
			"status":  "error",
			"message": fmt.Sprintf("invalid status: %q (must be delivered, failed or unknown)", terminalStatus),
		})
		return
	}

	var ackResult *service.AckOutboundResult
	if len(req.Items) > 0 {
		type groupKey struct {
			conv   string
			status string
		}
		groups := make(map[groupKey][]service.BridgeOutboundAckInput)
		for _, it := range req.Items {
			st := it.Status
			if st == "" {
				st = model.BridgeAckStatusDelivered
			}
			k := groupKey{conv: it.ConversationID, status: st}
			groups[k] = append(groups[k], service.BridgeOutboundAckInput{
				MsgID: it.MsgID, ConversationID: it.ConversationID, Status: it.Status, Error: it.Error,
			})
		}
		merged := &service.AckOutboundResult{Items: make([]service.AckOutboundItem, 0)}
		for k, inputs := range groups {
			ids := make([]string, 0, len(inputs))
			perItem := make(map[string]service.BridgeOutboundAckInput, len(inputs))
			for _, in := range inputs {
				ids = append(ids, in.MsgID)
				perItem[in.MsgID] = in
			}
			part, err := h.ingress.AckOutboundDeliveredDetailed(ctx, channel, accountID, ids, k.conv, k.status, perItem)
			if err != nil {
				logger.Ctx(ctx).Error().Err(err).Str("module", "bridge").Str("channel", channel).Msg("[Bridge] AckOutboundDeliveredDetailed(v2) failed")
				c.JSON(http.StatusInternalServerError, gin.H{"status": "error", "message": "ack failed"})
				return
			}
			merged.AffectedCount += part.AffectedCount
			merged.AckedItemsCount += part.AckedItemsCount
			merged.FailedItemsCount += part.FailedItemsCount
			merged.UnknownItemsCount += part.UnknownItemsCount
			merged.DuplicateCount += part.DuplicateCount
			merged.NotFoundCount += part.NotFoundCount
			merged.NotInScopeCount += part.NotInScopeCount
			merged.Items = append(merged.Items, part.Items...)
		}
		ackResult = merged
	} else {
		var err error
		ackResult, err = h.ingress.AckOutboundDeliveredDetailed(ctx, channel, accountID, req.MsgIDs, "", terminalStatus, nil)
		if err != nil {
			logger.Ctx(ctx).Error().Err(err).Str("module", "bridge").Str("channel", channel).Msg("[Bridge] AckOutboundDeliveredDetailed failed")
			c.JSON(http.StatusInternalServerError, gin.H{"status": "error", "message": "ack failed"})
			return
		}
	}
	logger.Ctx(ctx).Info().
		Str("module", "bridge").
		Str("event", "http_outbox_ack_response").
		Str("full_url", c.Request.URL.String()).
		Str("channel", channel).
		Str("account_id", accountID).
		Int("affected_count", ackResult.AffectedCount).
		Int("acked_items_count", ackResult.AckedItemsCount).
		Int("duplicate_count", ackResult.DuplicateCount).
		Int("not_found_count", ackResult.NotFoundCount).
		Int("request_msg_ids_count", len(req.MsgIDs)).
		Interface("request_msg_ids", req.MsgIDs).
		Interface("ack_items", ackResult.Items).
		Msg("[Bridge HTTP] outbox ack 响应（行级 affected + msg_id 级 acked + 详细 ack 结果）")
	for _, item := range ackResult.Items {
		if item.Status == "" {
			continue
		}
		bm.OutboxAcked.WithLabel(channel, item.Status).Inc()
	}
	bm.AckDuration.WithLabel(channel).Observe(float64(time.Since(start).Milliseconds()))
	c.JSON(http.StatusOK, BridgeOutboxAckResponse{
		Status:            "ok",
		AffectedCount:     ackResult.AffectedCount,
		AckedItemsCount:   ackResult.AckedItemsCount,
		FailedItemsCount:  ackResult.FailedItemsCount,
		UnknownItemsCount: ackResult.UnknownItemsCount,
		DuplicateCount:    ackResult.DuplicateCount,
		NotFoundCount:     ackResult.NotFoundCount,
		NotInScopeCount:   ackResult.NotInScopeCount,
		Items:             ackResult.Items,
	})
}

func (h *BridgeIngestHandler) callHandleIngressBatch(ctx context.Context, events []*model.MessageEvent) (*service.InboxIngressBatchResult, error) {
	if h.mockHandle != nil {
		results := make([]*service.InboxIngressResult, len(events))
		triggered := false
		for i, ev := range events {
			r, err := h.mockHandle(ctx, ev)
			if err != nil {
				r = &service.InboxIngressResult{Reason: err.Error()}
			}
			results[i] = r
			if r.QueuedForAI {
				triggered = true
			}
		}
		return &service.InboxIngressBatchResult{PerEvent: results, TriggeredAI: triggered}, nil
	}
	if h.ingress == nil {
		return nil, errors.New("ingress service not configured")
	}
	return h.ingress.HandleIngressBatch(ctx, events)
}

func httpMessageToEvent(m *HTTPIngestMessage) *model.MessageEvent {
	if m == nil {
		return nil
	}
	m.Channel = NormalizeBridgeChannel(m.Channel)
	return m.ToEvent("http")
}

func httpMessageToUnified(m *HTTPIngestMessage) *UnifiedMessage {
	return &UnifiedMessage{
		Channel:        m.Channel,
		AccountID:      m.AccountID,
		ConversationID: m.ConversationID,
		ReceiverID:     m.ReceiverID,
		SenderType:     m.SenderType,
		IsGroup:        m.IsGroup,
		GroupID:        m.GroupID,
		GroupName:      m.GroupName,
	}
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

var _ = url.Values{}
var _ = bytes.NewBuffer

// =================== 统一渠道网关（路由1：identify_code 唯一凭证） ===================

type UnifiedChannelGatewayRequest struct {
	Action         string                  `json:"action"` // report|ack|heartbeat
	SchemaVersion  string                  `json:"schema_version"`
	Channel        string                  `json:"channel"`
	AccountID      string                  `json:"account_id"`
	ConversationID string                  `json:"conversation_id"`
	Messages       []UnifiedGatewayMessage `json:"messages"`
	Items          []map[string]any        `json:"items"`
}

type UnifiedGatewayMessage struct {
	EventID        string         `json:"event_id"`
	ConversationID string         `json:"conversation_id"`
	SenderID       string         `json:"sender_id"`
	SenderName     string         `json:"sender_name"`
	SenderType     string         `json:"sender_type"`
	ReceiverID     string         `json:"receiver_id"`
	Content        string         `json:"content"`
	MsgType        string         `json:"msg_type"`
	MediaURL       string         `json:"media_url"`
	IsGroup        bool           `json:"is_group"`
	GroupID        string         `json:"group_id"`
	GroupName      string         `json:"group_name"`
	Timestamp      int64          `json:"timestamp"`
	Extra          map[string]any `json:"extra"`
}

// HandleGateway 统一渠道入口：identify_code 鉴权（BridgeIngressGuard）+ 租户自证。
// action=report 上报入收件箱；action=ack 确认下发；action=heartbeat 在线心跳。
func (h *BridgeIngestHandler) HandleGateway(c *gin.Context) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 4<<20)
	var req UnifiedChannelGatewayRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"status": "error", "message": "invalid json body"})
		return
	}
	action := strings.ToLower(strings.TrimSpace(req.Action))
	if action == "" {
		if len(req.Items) > 0 {
			action = "ack"
		} else {
			action = "report"
		}
	}
	switch action {
	case "ack":
		// 复用 bridge outbox ack 语义
		h.AckBridgeOutbox(c)
		return
	case "heartbeat":
		ident, _ := BridgeIdentityFromContext(c)
		channel := NormalizeBridgeChannel(req.Channel)
		accountID := strings.TrimSpace(req.AccountID)
		if ident != nil {
			if channel == "" {
				channel = ident.Channel
			}
			if accountID == "" {
				accountID = ident.AccountID
			}
		}
		if channel != "" && accountID != "" {
			if _, err := bindBridgeTenant(c, channel, accountID); err == nil {
				touchBridgeAccountOnline(c.Request.Context(), channel, accountID)
			}
		}
		c.JSON(http.StatusOK, gin.H{"schema_version": "hivemtk.channel.v1", "status": "ok", "action": "heartbeat"})
		return
	case "report":
		h.handleGatewayReport(c, &req)
		return
	default:
		c.JSON(http.StatusBadRequest, gin.H{"schema_version": "hivemtk.channel.v1", "status": "error", "message": "unsupported action"})
	}
}

func (h *BridgeIngestHandler) handleGatewayReport(c *gin.Context, req *UnifiedChannelGatewayRequest) {
	channel, accountID, err := gatewayReportIdentity(c, req.Channel, req.AccountID)
	if err != nil {
		writeBridgeTenantError(c, err)
		return
	}
	// identify_code-only: resolve identity early to fill channel/account.
	if _, err := bindBridgeTenant(c, channel, accountID); err != nil {
		writeBridgeTenantError(c, err)
		return
	}
	channel, accountID, err = gatewayReportIdentity(c, channel, accountID)
	if err != nil {
		writeBridgeTenantError(c, err)
		return
	}
	if channel == "" || accountID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"schema_version": "hivemtk.channel.v1", "status": "error", "message": "channel/account_id required (or identify_code)"})
		return
	}

	// bridge 渠道走现有 ingest 管道；非 bridge 渠道直接进 InboxIngress。
	if IsBridgeChannel(channel) {
		ing := HTTPIngestRequest{Channel: channel, AccountID: accountID, ConversationID: strings.TrimSpace(req.ConversationID)}
		for _, m := range req.Messages {
			conv := m.ConversationID
			if conv == "" {
				conv = ing.ConversationID
			}
			ing.Messages = append(ing.Messages, &HTTPIngestMessage{
				EventID: m.EventID, SenderID: m.SenderID, SenderName: m.SenderName, SenderType: m.SenderType,
				Content: m.Content, MsgType: m.MsgType, MediaURL: m.MediaURL, ConversationID: conv,
				IsGroup: m.IsGroup, GroupID: m.GroupID, GroupName: m.GroupName, Timestamp: m.Timestamp, Extra: m.Extra,
			})
		}
		c.Set("bridge_gateway_ingest_req", ing)
		// 复用 HandleHTTPIngest：它会再次 bindJSON; so call internal path by rebuilding request body is complex.
		// Instead, directly invoke logic by setting a parsed request in context is not supported.
		// Simpler: respond with redirect instruction not ideal. We'll process through a small inline path.
		h.handleGatewayBridgeIngest(c, channel, accountID, req)
		return
	}

	if h.ingress == nil && h.mockHandle == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"schema_version": "hivemtk.channel.v1", "status": "error", "message": "ingress unavailable"})
		return
	}
	tenantID, _ := tenant.FromContext(c.Request.Context())
	acks := make([]map[string]any, 0, len(req.Messages))
	for _, m := range req.Messages {
		ev := gatewayMessageToEvent(channel, accountID, tenantID, m)
		var result *service.InboxIngressResult
		var err error
		if h.mockHandle != nil {
			result, err = h.mockHandle(c.Request.Context(), ev)
		} else {
			result, err = h.ingress.HandleIngressMessage(c.Request.Context(), ev)
		}
		if err != nil {
			acks = append(acks, map[string]any{"event_id": ev.EventID, "status": "retry", "code": "INGRESS_FAILED"})
			continue
		}
		if result == nil {
			acks = append(acks, map[string]any{"event_id": ev.EventID, "status": "retry", "code": "INGRESS_RESULT_MISSING"})
			continue
		}
		if !result.Accepted {
			acks = append(acks, map[string]any{"event_id": ev.EventID, "status": "rejected", "code": "INGRESS_NOT_ACCEPTED"})
			continue
		}
		acks = append(acks, map[string]any{"event_id": ev.EventID, "status": "accepted"})
	}
	c.JSON(http.StatusOK, gin.H{"schema_version": "hivemtk.channel.v1", "status": "ok", "acks": acks})
}

// gatewayReportIdentity 固定使用已解析身份；渠道提示不决定消费分支。
func gatewayReportIdentity(c *gin.Context, channel, accountID string) (string, string, error) {
	channel = NormalizeBridgeChannel(channel)
	accountID = strings.TrimSpace(accountID)
	ident, ok := BridgeIdentityFromContext(c)
	if !ok {
		return channel, accountID, nil
	}
	resolvedChannel := NormalizeBridgeChannel(ident.Channel)
	resolvedAccount := strings.TrimSpace(ident.AccountID)
	if ident.TenantID == 0 || resolvedChannel == "" || resolvedAccount == "" {
		return "", "", ErrBridgeTenantUnresolved
	}
	if tid, bound := tenant.FromContext(c.Request.Context()); bound && tid != ident.TenantID {
		return "", "", ErrBridgeTenantAmbiguous
	}
	if accountID != "" && accountID != resolvedAccount {
		return "", "", ErrBridgeTenantAmbiguous
	}
	return resolvedChannel, resolvedAccount, nil
}

func (h *BridgeIngestHandler) handleGatewayBridgeIngest(c *gin.Context, channel, accountID string, req *UnifiedChannelGatewayRequest) {
	// Convert to HTTPIngestRequest and run minimal ingest path by calling HandleHTTPIngest via reconstructed body.
	ing := HTTPIngestRequest{Channel: channel, AccountID: accountID, ConversationID: strings.TrimSpace(req.ConversationID)}
	for _, m := range req.Messages {
		conv := m.ConversationID
		if conv == "" {
			conv = ing.ConversationID
		}
		ing.Messages = append(ing.Messages, &HTTPIngestMessage{
			EventID: m.EventID, SenderID: m.SenderID, SenderName: m.SenderName, SenderType: m.SenderType,
			Content: m.Content, MsgType: m.MsgType, MediaURL: m.MediaURL, ConversationID: conv,
			IsGroup: m.IsGroup, GroupID: m.GroupID, GroupName: m.GroupName, Timestamp: m.Timestamp, Extra: m.Extra,
		})
	}
	b, _ := json.Marshal(ing)
	c.Request.Body = io.NopCloser(bytes.NewReader(b))
	h.HandleHTTPIngest(c)
}

func gatewayMessageToEvent(channel, accountID string, tenantID uint, m UnifiedGatewayMessage) *model.MessageEvent {
	eventID := strings.TrimSpace(m.EventID)
	if eventID == "" {
		eventID = uuid.NewString()
	}
	ts := m.Timestamp
	if ts == 0 {
		ts = time.Now().UnixMilli()
	}
	msgType := strings.TrimSpace(m.MsgType)
	if msgType == "" {
		msgType = "text"
	}
	senderType := strings.TrimSpace(m.SenderType)
	if senderType == "" {
		senderType = "customer"
	}
	conv := strings.TrimSpace(m.ConversationID)
	if conv == "" {
		conv = strings.TrimSpace(m.SenderID)
	}
	extra := make(map[string]any, len(m.Extra)+2)
	for k, v := range m.Extra {
		extra[k] = v
	}
	// 身份来自已绑定的渠道/租户，客户端诊断字段不得覆盖。
	extra["account_id"] = accountID
	extra["tenant_id"] = strconv.FormatUint(uint64(tenantID), 10)
	receiver := m.ReceiverID
	if receiver == "" {
		receiver = accountID
	}
	return &model.MessageEvent{
		EventID: eventID, Channel: channel, SenderID: m.SenderID, SenderName: m.SenderName,
		SenderType: senderType, ReceiverID: receiver, MsgType: msgType, Content: m.Content,
		MediaURL: m.MediaURL, ConversationID: conv, IsGroup: m.IsGroup, GroupID: m.GroupID,
		Extra: extra, Timestamp: time.UnixMilli(ts),
	}
}
