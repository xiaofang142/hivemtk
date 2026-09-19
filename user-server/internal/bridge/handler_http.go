package bridge

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"hivemtk-user/internal/channelgw"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/metrics"
	"hivemtk-user/internal/pkg/tracing"
	"hivemtk-user/internal/pkg/utils/logger"
	"hivemtk-user/internal/service"

	"github.com/gin-gonic/gin"
)

// HTTP 端点参数（DB 驱动，以下为 fallback 默认值）
// 实际运行时通过 service.GlobalConfigParam() 按 group=bridge 读取 DB 参数：
//
//	bridge.polling_max_timeout → HTTPPollingMaxTimeout (fallback)
//	bridge.polling_default_timeout → HTTPPollingDefaultTimeout (fallback)
//	bridge.ingest_max_body_bytes → HTTPIngestMaxBodySize (fallback)
//	bridge.ingest_max_messages → HTTPIngestMaxMessages (fallback)
const (
	HTTPPollingMaxTimeout     = 500 * time.Second
	HTTPPollingDefaultTimeout = 30 * time.Second
	HTTPIngestMaxBodySize     = 4 << 20
	HTTPIngestMaxMessages     = 200
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

func collectHTTPRequestInfo(c *gin.Context) httpRequestInfo {
	token := c.Query("token")
	origContentLength := c.Request.ContentLength
	fullBody, bodySize, bodyPreview, _ := readBodyForLog(c.Request.Body, 4096)
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
	}
}

func readBodyForLog(rc io.ReadCloser, previewBytes int) (string, int, string, bool) {
	if rc == nil {
		return "", 0, "", false
	}
	all, err := io.ReadAll(rc)
	_ = rc.Close()
	if err != nil {
		msg := fmt.Sprintf("[read body failed: %v]", err)
		return msg, 0, msg, false
	}
	total := len(all)
	full := string(all)
	if total <= previewBytes {
		return full, total, full, false
	}
	preview := string(all[:previewBytes]) + fmt.Sprintf("... [truncated, total=%d bytes]", total)
	return full, total, preview, true
}

type BridgeIngestHandler struct {
	ingress       *service.InboxIngressService
	mockHandle    func(ctx context.Context, ev *model.MessageEvent) (*service.InboxIngressResult, error)
	mockPersist   func(ctx context.Context, ev *model.MessageEvent, direction string) error
	leadMiner     func(ctx context.Context, ev *model.MessageEvent)
	sseHandler    *SSEHandler
	outboxFetcher *outboxDBFetcher
}

// NewBridgeIngestHandler 构造 HTTP ingest 处理器
func NewBridgeIngestHandler(ingress *service.InboxIngressService) *BridgeIngestHandler {
	h := &BridgeIngestHandler{ingress: ingress}
	h.outboxFetcher = &outboxDBFetcher{}
	h.sseHandler = NewSSEHandler(h.outboxFetcher)
	return h
}

// SetOutboxQuerier 注入 OutboxQuerier（由 router 在装配完成后调用一次）
func (h *BridgeIngestHandler) SetOutboxQuerier(q OutboxQuerier) {
	if h.outboxFetcher != nil {
		h.outboxFetcher.SetQuerier(q)
	}
}

// SetLeadMiner 设置线索挖掘回调（Douyin/TikTok 等群聊渠道用）
func (h *BridgeIngestHandler) SetLeadMiner(fn func(ctx context.Context, ev *model.MessageEvent)) {
	h.leadMiner = fn
}

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

type outboxDBFetcher struct {
	querier OutboxQuerier
}

func (f *outboxDBFetcher) SetQuerier(q OutboxQuerier) {
	f.querier = q
}

func (f *outboxDBFetcher) FetchOutboxSince(ctx context.Context, channel, accountID, lastEventID string) ([]SSEEvent, string, error) {
	if f.querier == nil {
		return nil, "", nil
	}

	var sinceID uint64
	if lastEventID != "" {
		id, err := strconv.ParseUint(lastEventID, 10, 64)
		if err != nil {
			// B8（2026-09-19）：非法游标 ≠ 无游标。旧行为把垃圾游标当 0 → 全量回放
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

	rows, err := f.querier.FetchOutboundSince(ctx, channel, accountID, sinceID, 200)
	if err != nil {
		return nil, "", err
	}

	events := make([]SSEEvent, 0, len(rows))
	for _, row := range rows {
		// R-B1：Data 统一经 BuildOutboundSSEEvent 构造（与总线路径逐键一致）
		events = append(events, BuildOutboundSSEEvent(OutboundEventData{
			HubID:          uint64(row.ID),
			MsgID:          row.MsgID,
			Platform:       row.Platform,
			AccountID:      row.AccountID,
			ConversationID: row.ConversationID,
			Content:        row.Content,
			MsgType:        row.MsgType,
			ReceiverID:     row.ReceiverID,
			IsAIReply:      row.IsAIReply,
			Extra:          row.Extra,
			CreatedAt:      row.CreatedAt,
		}))
	}

	newLastID := lastEventID
	if len(rows) > 0 {
		newLastID = strconv.FormatUint(uint64(rows[len(rows)-1].ID), 10)
	}
	return events, newLastID, nil
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

func (h *BridgeIngestHandler) HandleHTTPIngest(c *gin.Context) {
	info := collectHTTPRequestInfo(c)
	channel := info.Channel
	accountID := info.AccountID
	conversationID := info.ConversationID
	ctx0 := c.Request.Context()
	start := time.Now()
	bm := metrics.GetBridge()

	var req HTTPIngestRequest
	bodyBindErr := c.ShouldBindJSON(&req)
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
	}

	channelNorm := NormalizeBridgeChannel(channel)

	bridgeHTTPIngestError := func(errCode string) {
		bm.IngestErrors.WithLabel(channel, errCode).Inc()
		bm.IngestDuration.WithLabel(channel).Observe(float64(time.Since(start).Milliseconds()))
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

	if channel == "" {
		logger.Ctx(ctx0).Warn().
			Str("module", "bridge").
			Str("event", "http_ingest_missing_params").
			Str("full_url", info.FullURL).
			Str("channel", channel).
			Str("account_id", accountID).
			Msg("[Bridge HTTP] 参数缺失：channel 为空")
		bridgeHTTPIngestError("channel_required")
		c.JSON(http.StatusBadRequest, HTTPIngestResponse{
			OK:     false,
			Reason: "channel required",
		})
		return
	}
	if accountID == "" {
		logger.Ctx(ctx0).Warn().
			Str("module", "bridge").
			Str("event", "http_ingest_missing_params").
			Str("full_url", info.FullURL).
			Str("channel", channel).
			Str("account_id", accountID).
			Msg("[Bridge HTTP] 参数缺失：account_id 为空（前端必须取到账号才能上报）")
		bridgeHTTPIngestError("account_id_required")
		c.JSON(http.StatusBadRequest, HTTPIngestResponse{
			OK:     false,
			Reason: "account_id required (extension must capture account from DOM before ingesting)",
		})
		return
	}
	if !IsBridgeChannel(channel) {
		logger.Ctx(ctx0).Warn().
			Str("module", "bridge").
			Str("event", "http_ingest_unsupported_channel").
			Str("full_url", info.FullURL).
			Str("channel", channel).
			Str("account_id", accountID).
			Msg("[Bridge HTTP] 渠道不在白名单")
		bridgeHTTPIngestError("unsupported_channel")
		c.JSON(http.StatusBadRequest, HTTPIngestResponse{
			OK:     false,
			Reason: "unsupported bridge channel",
		})
		return
	}

	uidAny, _ := c.Get("user_id")
	uid, _ := uidAny.(uint)
	if GlobalOwnershipChecker != nil && uid != 0 {
		owns, err := GlobalOwnershipChecker(c.Request.Context(), uid, channel, accountID)
		if err != nil {
			logger.Ctx(ctx0).Warn().
				Str("module", "bridge").
				Str("event", "http_ingest_ownership_check_failed").
				Err(err).
				Str("full_url", info.FullURL).
				Str("channel", channel).
				Str("account_id", accountID).
				Uint("user_id", uid).
				Msg("[Bridge HTTP] 账号归属校验失败")
			c.JSON(http.StatusInternalServerError, HTTPIngestResponse{
				OK:     false,
				Reason: "ownership check failed: " + err.Error(),
			})
			return
		}
		if !owns {
			logger.Ctx(ctx0).Warn().
				Str("module", "bridge").
				Str("event", "http_ingest_ownership_denied").
				Str("full_url", info.FullURL).
				Str("channel", channel).
				Str("account_id", accountID).
				Uint("user_id", uid).
				Msg("[Bridge HTTP] 账号归属不属于当前 user，拒绝 ingest")
			c.JSON(http.StatusForbidden, HTTPIngestResponse{
				OK:     false,
				Reason: "account not owned by current user",
			})
			return
		}
	}

	maxBodySize := runtimeIngestMaxBodySize(ctx0)
	if info.ContentLength > int64(maxBodySize) {
		logger.Ctx(ctx0).Warn().
			Str("module", "bridge").
			Str("event", "http_ingest_body_too_large").
			Str("full_url", info.FullURL).
			Int64("content_length", info.ContentLength).
			Msg("[Bridge HTTP] body 超过上限")
		c.JSON(http.StatusRequestEntityTooLarge, HTTPIngestResponse{
			OK:     false,
			Reason: fmt.Sprintf("body too large (max %d bytes)", maxBodySize),
		})
		return
	}

	if bodyBindErr != nil && info.ContentLength > 0 {
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

	if channel != "" {
		req.Channel = channel
	}
	if accountID != "" {
		req.AccountID = accountID
	}
	if conversationID != "" {
		req.ConversationID = conversationID
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
			UserID:      uid,
			Channel:     NormalizeBridgeChannel(channel),
			AccountID:   accountID,
			AgentID:     req.AgentID,
			AccountName: req.AccountName,
			Status:      "online",
		}
		go func() {
			defer func() {
				if r := recover(); r != nil {
					logger.Ctx(context.Background()).Error().
						Interface("panic", r).
						Str("module", "bridge").
						Str("channel", up.Channel).
						Str("account_id", up.AccountID).
						Msg("[Bridge] async HTTP Upsert panic recovered")
				}
			}()
			upCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			if err := GlobalBridgeAccountRepo.Upsert(upCtx, up); err != nil {
				if !errors.Is(err, ErrAccountOwnedByOther) {
					logger.Ctx(context.Background()).Warn().Err(err).Str("module", "bridge").
						Str("channel", up.Channel).Str("account_id", up.AccountID).
						Msg("bridge HTTP Upsert failed (non-fatal)")
				}
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
				histDir := it.Direction
				if histDir == "" {
					histDir = "inbound"
				}
				if (m.SenderType == "self" || m.SenderType == "agent") && histDir != "outbound" {
					histDir = "outbound"
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

	if h.leadMiner != nil && isLeadMiningChannel(channelNorm) {
		for _, ev := range batchEvents {
			h.leadMiner(ctx, ev)
		}
	}

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

var leadMiningChannels = map[string]bool{
	"douyin":      true,
	"tiktok":      true,
	"kuaishou":    true,
	"xiaohongshu": true,
	"xianyu":      true,
}

func isLeadMiningChannel(channel string) bool {
	return leadMiningChannels[strings.ToLower(channel)]
}

// AddLeadMiningChannel 动态扩展支持群聊线索挖掘的 Bridge 渠道（插件使用）。
//
//	使用约束：调用方必须先在 channelgw.Default 注册同名 ChannelSpec，
//	且必须有对应的 Chrome 扩展 content script 与 Bridge 协议对齐，
//	否则 ingest 会被 IsBridgeChannel 拒绝。
func AddLeadMiningChannel(channel string) {
	leadMiningChannels[strings.ToLower(channel)] = true
}

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

func (h *BridgeIngestHandler) GetBridgeOutbox(c *gin.Context) {
	channel := c.Query("channel")
	accountID := c.Query("account_id")
	start := time.Now()
	bm := metrics.GetBridge()

	bridgeOutboxError := func(errCode string) {
		bm.IngestErrors.WithLabel(channel, errCode).Inc()
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
	if !IsBridgeChannel(channel) {
		bridgeOutboxError("unsupported_channel")
		c.JSON(http.StatusBadRequest, gin.H{"status": "error", "message": "unsupported bridge channel"})
		return
	}
	ctx := c.Request.Context()
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
	hubs, err := h.ingress.ClaimPendingOutbound(ctx, channel, accountID, limit)
	if err != nil {
		logger.Ctx(ctx).Error().Err(err).Str("module", "bridge").Str("channel", channel).Msg("[Bridge] ListPendingOutbound failed")
		c.JSON(http.StatusInternalServerError, gin.H{"status": "error", "message": "query outbox failed"})
		return
	}
	tracing.RecordDownlinkFetchBatch(ctx, channel, accountID, hubs)
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
	Status           string                    `json:"status"`
	AffectedCount    int                       `json:"affected_count"`
	AckedItemsCount  int                       `json:"acked_items_count"`
	FailedItemsCount int                       `json:"failed_items_count"`
	DuplicateCount   int                       `json:"duplicate_count"`
	NotFoundCount    int                       `json:"not_found_count"`
	NotInScopeCount  int                       `json:"not_in_scope_count"`
	Items            []service.AckOutboundItem `json:"items"`
}

func (h *BridgeIngestHandler) AckBridgeOutbox(c *gin.Context) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 1<<20)
	channel := c.Query("channel")
	accountID := c.Query("account_id")
	start := time.Now()
	bm := metrics.GetBridge()
	if accountID == "" {
		bm.IngestErrors.WithLabel(channel, "account_id_required").Inc()
		c.JSON(http.StatusBadRequest, gin.H{
			"status":  "error",
			"message": "account_id required (extension must capture account from DOM before acking)",
		})
		return
	}
	if channel == "" {
		bm.IngestErrors.WithLabel(channel, "channel_required").Inc()
		c.JSON(http.StatusBadRequest, gin.H{"status": "error", "message": "channel required"})
		return
	}
	if !IsBridgeChannel(channel) {
		bm.IngestErrors.WithLabel(channel, "unsupported_channel").Inc()
		c.JSON(http.StatusBadRequest, gin.H{"status": "error", "message": "unsupported bridge channel"})
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

	if terminalStatus != model.BridgeAckStatusDelivered && terminalStatus != model.BridgeAckStatusFailed {
		c.JSON(http.StatusBadRequest, gin.H{
			"status":  "error",
			"message": fmt.Sprintf("invalid status: %q (must be delivered or failed)", terminalStatus),
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
		Status:           "ok",
		AffectedCount:    ackResult.AffectedCount,
		AckedItemsCount:  ackResult.AckedItemsCount,
		FailedItemsCount: ackResult.FailedItemsCount,
		DuplicateCount:   ackResult.DuplicateCount,
		NotFoundCount:    ackResult.NotFoundCount,
		NotInScopeCount:  ackResult.NotInScopeCount,
		Items:            ackResult.Items,
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
