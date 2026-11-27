package service

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/pkg/tracing"
	"hivemtk-user/internal/repository"

	"github.com/google/uuid"
)

// OutboundOrigin 明确区分 AI 与人工回复；历史出站不能再默认硬编码为 AI。
type OutboundOrigin string

const (
	OutboundOriginAI    OutboundOrigin = "ai"
	OutboundOriginHuman OutboundOrigin = "human"
)

const (
	OutboundStatusPending    = "pending"
	OutboundStatusSent       = "sent"
	OutboundStatusSendFailed = "send_failed"
	OutboundStatusUnknown    = "unknown"
)

const (
	OutboundArtifactSent     = "sent"
	OutboundArtifactFailed   = "failed"
	OutboundArtifactUnknown  = "unknown"
	OutboundArtifactDeferred = "deferred"
)

// OutboundArtifactReceipt 是单个文本分块或卡片的投递事实。
type OutboundArtifactReceipt = repository.OutboundArtifactReceipt

// OutboundSettlement 是共享生命周期交给仓储的同一行结算事实。
type OutboundSettlement = repository.OutboundSettlement

// OutboundLifecycleStore 是出站生命周期端口：sender 不拥有 canonical 行，
// prepare 必须发生在网络 I/O 之前，settle 只能更新同一 correlation 对应的一行。
type OutboundLifecycleStore interface {
	PrepareOutbound(context.Context, *model.MessageHub) (*model.MessageHub, error)
	SettleOutbound(context.Context, OutboundSettlement) (*model.MessageHub, error)
	AlignOutboundSession(context.Context, *model.MessageHub, *time.Time) error
}

type outboundLifecycle struct {
	store OutboundLifecycleStore
}

type outboundLifecycleRequest struct {
	Channel   WebhookChannel
	AccountID string
	Payload   *ParsedPayload
	Content   string
	Cards     []model.RichCard
	Source    *model.MessageHub
	Origin    OutboundOrigin
	SessionID string
}

type outboundCorrelationCtxKey struct{}
type outboundOriginCtxKey struct{}
type outboundSessionIDCtxKey struct{}
type outboundCanonicalCtxKey struct{}

// OutboundCorrelationToContext 把一次逻辑回复的不可变身份传给生成、延迟与重放链路。
func OutboundCorrelationToContext(ctx context.Context, correlationID string) context.Context {
	if ctx == nil || strings.TrimSpace(correlationID) == "" {
		return ctx
	}
	return context.WithValue(ctx, outboundCorrelationCtxKey{}, strings.TrimSpace(correlationID))
}

// OutboundCorrelationFromContext 读取逻辑回复身份；空值表示调用方尚未建立身份。
func OutboundCorrelationFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	value, _ := ctx.Value(outboundCorrelationCtxKey{}).(string)
	return strings.TrimSpace(value)
}

// OutboundOriginToContext 显式标记人工/AI 来源。
func OutboundOriginToContext(ctx context.Context, origin OutboundOrigin) context.Context {
	if ctx == nil {
		return ctx
	}
	return context.WithValue(ctx, outboundOriginCtxKey{}, origin)
}

// OutboundOriginFromContext 未标记时沿用 AI 出站默认；人工入口必须显式覆盖。
func OutboundOriginFromContext(ctx context.Context) OutboundOrigin {
	if ctx != nil {
		if origin, ok := ctx.Value(outboundOriginCtxKey{}).(OutboundOrigin); ok {
			switch origin {
			case OutboundOriginAI, OutboundOriginHuman:
				return origin
			}
		}
	}
	return OutboundOriginAI
}

// OutboundSessionIDToContext 把会话补充行身份带入生命周期，便于延迟/重放对齐。
func OutboundSessionIDToContext(ctx context.Context, sessionID string) context.Context {
	if ctx == nil || strings.TrimSpace(sessionID) == "" {
		return ctx
	}
	return context.WithValue(ctx, outboundSessionIDCtxKey{}, strings.TrimSpace(sessionID))
}

func outboundSessionIDFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	value, _ := ctx.Value(outboundSessionIDCtxKey{}).(string)
	return strings.TrimSpace(value)
}

func outboundCanonicalToContext(ctx context.Context, row *model.MessageHub) context.Context {
	if ctx == nil || row == nil {
		return ctx
	}
	return context.WithValue(ctx, outboundCanonicalCtxKey{}, row)
}

func outboundCanonicalFromContext(ctx context.Context) *model.MessageHub {
	if ctx == nil {
		return nil
	}
	row, _ := ctx.Value(outboundCanonicalCtxKey{}).(*model.MessageHub)
	return row
}

func newOutboundLifecycle(store OutboundLifecycleStore) *outboundLifecycle {
	return &outboundLifecycle{store: store}
}

// newOutboundCorrelation 每个逻辑回复只生成一次稳定身份；同 event 的重试复用同值。
func newOutboundCorrelation(channel WebhookChannel, accountID, conversationID, eventID string) string {
	if strings.TrimSpace(eventID) == "" {
		return "out-" + uuid.NewString()
	}
	sum := sha256.Sum256([]byte(strings.Join([]string{
		string(channel), accountID, conversationID, eventID,
	}, "\x00")))
	return fmt.Sprintf("out-%x", sum[:16])
}

func outboundHubMsgID(channel WebhookChannel, correlationID string) string {
	return string(channel) + "-out-" + correlationID
}

// Begin 在 sender 前建立 canonical pending 行，并把 SessionMessage 的 raw 正文
// 对齐到最终渲染正文。任何写失败都返回错误，调用方不得继续网络投递。
func (l *outboundLifecycle) Begin(ctx context.Context, req outboundLifecycleRequest) (*model.MessageHub, error) {
	if l == nil || l.store == nil {
		return nil, errors.New("outbound lifecycle store unavailable")
	}
	if req.Source == nil {
		return nil, errors.New("outbound lifecycle source scope unavailable")
	}
	if req.Channel == "" || req.AccountID == "" || req.Source.ConversationID == "" {
		return nil, errors.New("outbound lifecycle target scope unavailable")
	}
	origin := req.Origin
	if origin == "" {
		origin = OutboundOriginAI
	}
	if origin != OutboundOriginAI && origin != OutboundOriginHuman {
		return nil, fmt.Errorf("outbound lifecycle unsupported origin %q", origin)
	}
	correlationID := OutboundCorrelationFromContext(ctx)
	if correlationID == "" {
		correlationID = newOutboundCorrelation(req.Channel, req.AccountID, req.Source.ConversationID, payloadEventID(req.Payload))
	}
	ctx = OutboundCorrelationToContext(ctx, correlationID)

	extra := make(model.JSONMap, len(req.Source.Extra)+8)
	for key, value := range req.Source.Extra {
		extra[key] = value
	}
	if sourceCorrelation := hubCorrelationID(req.Source.Extra); sourceCorrelation != "" && sourceCorrelation != correlationID {
		extra["source_correlation_id"] = sourceCorrelation
	}
	extra["correlation_id"] = correlationID
	extra["llm_correlation_id"] = correlationID
	extra["origin"] = string(origin)
	extra["canonical_outbound"] = true
	extra["source_message_id"] = req.Source.MsgID
	extra["source_direction"] = req.Source.Direction
	if req.SessionID != "" {
		extra["session_id"] = req.SessionID
	}
	if len(req.Cards) > 0 {
		if raw, err := json.Marshal(req.Cards); err == nil {
			extra["cards"] = json.RawMessage(raw)
		}
	}

	now := time.Now()
	visibility := req.Source.Visibility
	if visibility == "" {
		visibility = model.VisibilityUnknown
	}
	traceID := req.Source.TraceID
	if traceID == "" {
		traceID = tracing.LinkOutboundTraceID(ctx, req.Source.ConversationID)
	}
	agentID := ""
	if origin == OutboundOriginAI {
		agentID = extractAgentIDFromCtx(ctx)
	}
	receiverID := req.Source.SenderID
	if receiverID == "" && req.Payload != nil {
		receiverID = req.Payload.Sender
	}
	row := &model.MessageHub{
		TenantID:           req.Source.TenantID,
		Platform:           string(req.Channel),
		MsgID:              outboundHubMsgID(req.Channel, correlationID),
		Direction:          "outbound",
		Status:             OutboundStatusPending,
		MsgType:            model.MsgTypeText,
		AccountID:          req.AccountID,
		SenderID:           req.AccountID,
		ReceiverID:         receiverID,
		Content:            req.Content,
		ConversationID:     req.Source.ConversationID,
		IsGroup:            req.Source.IsGroup,
		GroupID:            req.Source.GroupID,
		Visibility:         visibility,
		AudienceJSON:       append(model.JSONStrings{}, req.Source.AudienceJSON...),
		VisibilityEvidence: cloneJSONMap(req.Source.VisibilityEvidence),
		IsAIReply:          origin == OutboundOriginAI,
		AIAgent:            agentID,
		TraceID:            traceID,
		DedupHash:          ContentHashWithSender(string(req.Channel), req.AccountID, correlationID),
		IsRead:             true,
		SentAt:             now,
		Extra:              extra,
	}
	persisted, err := l.store.PrepareOutbound(ctx, row)
	if err != nil {
		return nil, fmt.Errorf("prepare canonical outbound: %w", err)
	}
	if persisted == nil || persisted.ID == 0 {
		return nil, errors.New("prepare canonical outbound: row identity missing")
	}
	if err := l.store.AlignOutboundSession(ctx, persisted, nil); err != nil {
		return nil, fmt.Errorf("align canonical outbound session: %w", err)
	}
	return persisted, nil
}

// Settle 根据 sender 的逐 artifact 回执与错误，把同一 canonical 行收敛到
// sent/send_failed/retryable pending/unknown；不新建行。
func (l *outboundLifecycle) Settle(ctx context.Context, row *model.MessageHub, result *OutboundResult, sendErr error) (*model.MessageHub, error) {
	if l == nil || l.store == nil {
		return nil, errors.New("outbound lifecycle store unavailable")
	}
	if row == nil || row.ID == 0 || row.TenantID == 0 {
		return nil, errors.New("outbound lifecycle settlement row unavailable")
	}
	correlationID := hubCorrelationID(row.Extra)
	if correlationID == "" {
		return nil, errors.New("outbound lifecycle settlement correlation unavailable")
	}

	status := OutboundStatusSendFailed
	partial := false
	unknown := false
	platformMsgID := ""
	receipts := []OutboundArtifactReceipt{}
	if result != nil {
		receipts = append(receipts, result.Receipts...)
		platformMsgID = strings.TrimSpace(result.PlatformMsgID)
		partial = result.Partial
		unknown = result.Unknown
		if result.Sent {
			status = OutboundStatusSent
		} else if result.Unknown {
			status = OutboundStatusUnknown
		} else if ce := AsChannelError(sendErr); ce != nil && ce.Retryable {
			status = OutboundStatusPending
		}
	} else if ce := AsChannelError(sendErr); ce != nil && ce.Retryable {
		status = OutboundStatusPending
	} else if sendErr == nil {
		status = OutboundStatusUnknown
		unknown = true
	}
	if platformMsgID == "" {
		for _, receipt := range receipts {
			if value := strings.TrimSpace(receipt.PlatformMsgID); value != "" {
				platformMsgID = value
				break
			}
		}
	}
	if len(receipts) == 0 && platformMsgID != "" {
		receipts = append(receipts, OutboundArtifactReceipt{
			ArtifactID: "text", PlatformMsgID: platformMsgID, Status: status,
		})
	}
	reason := ""
	if sendErr != nil {
		reason = sendErr.Error()
	}
	if result != nil && result.Sent && sendErr != nil {
		partial = true
	}
	extra := model.JSONMap{
		"retryable": status == OutboundStatusPending,
	}
	if status == OutboundStatusSendFailed || status == OutboundStatusUnknown {
		extra["delivery_state"] = status
	}
	var sentAt *time.Time
	if status == OutboundStatusSent {
		now := time.Now()
		sentAt = &now
	}
	settled, err := l.store.SettleOutbound(ctx, OutboundSettlement{
		RowID:         row.ID,
		TenantID:      row.TenantID,
		CorrelationID: correlationID,
		Status:        status,
		PlatformMsgID: platformMsgID,
		Reason:        reason,
		Receipts:      receipts,
		Partial:       partial,
		Unknown:       unknown,
		SentAt:        sentAt,
		Extra:         extra,
	})
	if err != nil {
		return nil, fmt.Errorf("settle canonical outbound: %w", err)
	}
	return settled, nil
}

func payloadEventID(payload *ParsedPayload) string {
	if payload == nil {
		return ""
	}
	return payload.EventID
}

func cloneJSONMap(source model.JSONMap) model.JSONMap {
	if source == nil {
		return nil
	}
	cloned := make(model.JSONMap, len(source))
	for key, value := range source {
		cloned[key] = value
	}
	return cloned
}

func (s *WebhookService) ensureOutboundLifecycleStore() OutboundLifecycleStore {
	if s == nil {
		return nil
	}
	if s.outboundLifecycleStore != nil {
		return s.outboundLifecycleStore
	}
	s.ensureReposFromDB(context.Background())
	if s.messageHubRepo == nil {
		return nil
	}
	return s.messageHubRepo
}

func outboundTenantID(ctx context.Context, source *model.MessageHub) uint {
	if source != nil && source.TenantID != 0 {
		return source.TenantID
	}
	if tid, ok := tenant.FromContext(ctx); ok && tid != 0 {
		return tid
	}
	if !tenant.IsMultiTenant() {
		return tenant.DefaultTenantID
	}
	return 0
}
