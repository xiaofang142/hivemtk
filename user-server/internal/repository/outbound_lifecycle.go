package repository

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tenant"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// OutboundArtifactReceipt 是一条逻辑回复中单个文本分块或卡片的平台回执。
type OutboundArtifactReceipt struct {
	ArtifactID     string `json:"artifact_id"`
	PlatformMsgID  string `json:"platform_msg_id,omitempty"`
	Status         string `json:"status"`
	Error          string `json:"error,omitempty"`
	DeliveryKind   string `json:"delivery_kind,omitempty"`
	IdempotencyKey string `json:"idempotency_key,omitempty"`
}

// OutboundSettlement 是 service 已裁决出的同一 canonical 行结算事实。
// repository 只负责按租户与行 ID 落库，不判断发送业务语义。
type OutboundSettlement struct {
	RowID         uint
	TenantID      uint
	CorrelationID string
	Status        string
	PlatformMsgID string
	Reason        string
	Receipts      []OutboundArtifactReceipt
	Partial       bool
	Unknown       bool
	SentAt        *time.Time
	Extra         model.JSONMap
}

// PrepareOutbound 幂等创建或读取一条 canonical pending 出站行。
// msg_id 由 service 按 correlation 生成；同 correlation 同 scope 永远返回同一行。
func (r *MessageHubRepository) PrepareOutbound(ctx context.Context, msg *model.MessageHub) (*model.MessageHub, error) {
	if r == nil || r.db == nil {
		return nil, errors.New("outbound lifecycle repository not initialized")
	}
	if msg == nil {
		return nil, errors.New("outbound lifecycle: nil message")
	}
	if err := prepareOutboundLifecycleTenant(ctx, msg); err != nil {
		return nil, err
	}
	if msg.Direction != "outbound" || msg.MsgID == "" || msg.Platform == "" || msg.AccountID == "" {
		return nil, errors.New("outbound lifecycle: canonical identity incomplete")
	}

	var existing model.MessageHub
	err := r.db.WithContext(ctx).
		Where("tenant_id = ? AND platform = ? AND msg_id = ? AND conversation_id = ? AND direction = ?",
			msg.TenantID, msg.Platform, msg.MsgID, msg.ConversationID, "outbound").
		First(&existing).Error
	if err == nil {
		if hubOutboundCorrelationID(existing.Extra) != hubOutboundCorrelationID(msg.Extra) {
			return nil, errors.New("outbound lifecycle: canonical row correlation mismatch")
		}
		if existing.AccountID != msg.AccountID || existing.IsGroup != msg.IsGroup || existing.GroupID != msg.GroupID {
			return nil, errors.New("outbound lifecycle: canonical row scope mismatch")
		}
		return &existing, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, fmt.Errorf("find canonical outbound: %w", err)
	}

	conflict := clause.OnConflict{
		Columns: []clause.Column{
			{Name: "tenant_id"}, {Name: "platform"}, {Name: "msg_id"},
			{Name: "conversation_id"}, {Name: "direction"},
		},
		DoNothing: true,
	}
	if err := r.db.WithContext(ctx).Clauses(conflict).Create(msg).Error; err != nil {
		return nil, fmt.Errorf("create canonical outbound: %w", err)
	}
	if msg.ID == 0 {
		if err := r.db.WithContext(ctx).
			Where("tenant_id = ? AND platform = ? AND msg_id = ? AND conversation_id = ? AND direction = ?",
				msg.TenantID, msg.Platform, msg.MsgID, msg.ConversationID, "outbound").
			First(&existing).Error; err != nil {
			return nil, fmt.Errorf("reload canonical outbound: %w", err)
		}
		return &existing, nil
	}
	return msg, nil
}

// SettleOutbound 按 service 给出的状态、回执与原因结算同一行，并同步会话补充行。
func (r *MessageHubRepository) SettleOutbound(ctx context.Context, settlement OutboundSettlement) (*model.MessageHub, error) {
	if r == nil || r.db == nil {
		return nil, errors.New("outbound lifecycle repository not initialized")
	}
	if settlement.RowID == 0 || settlement.TenantID == 0 || settlement.CorrelationID == "" {
		return nil, errors.New("outbound lifecycle: settlement identity incomplete")
	}
	switch settlement.Status {
	case "pending", "sent", "send_failed", "unknown":
	default:
		return nil, fmt.Errorf("outbound lifecycle: unsupported settlement status %q", settlement.Status)
	}

	var row model.MessageHub
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.
			Where("tenant_id = ? AND id = ? AND direction = ?", settlement.TenantID, settlement.RowID, "outbound").
			First(&row).Error; err != nil {
			return fmt.Errorf("lock canonical outbound: %w", err)
		}
		if hubOutboundCorrelationID(row.Extra) != settlement.CorrelationID {
			return errors.New("outbound lifecycle: settlement correlation mismatch")
		}
		extra := make(model.JSONMap, len(row.Extra)+len(settlement.Extra)+5)
		for key, value := range row.Extra {
			extra[key] = value
		}
		for key, value := range settlement.Extra {
			extra[key] = value
		}
		extra["receipts"] = settlement.Receipts
		extra["partial"] = settlement.Partial
		extra["unknown"] = settlement.Unknown
		if settlement.Reason != "" {
			extra["settlement_reason"] = settlement.Reason
		}

		updates := map[string]any{
			"status":          settlement.Status,
			"extra":           extra,
			"push_error":      truncateForColumn(settlement.Reason, 200),
			"sent_at":         row.SentAt,
			"platform_msg_id": settlement.PlatformMsgID,
		}
		if settlement.SentAt != nil {
			updates["sent_at"] = *settlement.SentAt
		}
		if err := tx.Model(&model.MessageHub{}).
			Where("tenant_id = ? AND id = ? AND direction = ?", settlement.TenantID, settlement.RowID, "outbound").
			Updates(updates).Error; err != nil {
			return fmt.Errorf("update canonical outbound: %w", err)
		}
		if settlement.Status == "sent" && settlement.SentAt != nil {
			if err := alignOutboundSessionTx(tx, settlement.TenantID, settlement.CorrelationID, row.Content, row.IsAIReply, row.SenderID, settlement.SentAt); err != nil {
				return fmt.Errorf("align outbound session message: %w", err)
			}
		}
		row.Status = settlement.Status
		row.PlatformMsgID = settlement.PlatformMsgID
		row.Extra = extra
		if settlement.SentAt != nil {
			row.SentAt = *settlement.SentAt
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &row, nil
}

// AlignOutboundSession 把同 correlation 的 SessionMessage 对齐到 canonical 渲染正文，
// 避免 raw Markdown 与 Hub 渲染正文被 UnifiedMessageReader 判为 ambiguous duplicate。
func (r *MessageHubRepository) AlignOutboundSession(ctx context.Context, row *model.MessageHub, deliveredAt *time.Time) error {
	if r == nil || r.db == nil {
		return errors.New("outbound lifecycle repository not initialized")
	}
	if row == nil || row.TenantID == 0 {
		return errors.New("outbound lifecycle: session alignment scope incomplete")
	}
	correlationID := hubOutboundCorrelationID(row.Extra)
	if correlationID == "" {
		return errors.New("outbound lifecycle: session alignment correlation missing")
	}
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		return alignOutboundSessionTx(tx, row.TenantID, correlationID, row.Content, row.IsAIReply, row.SenderID, deliveredAt)
	})
}

func alignOutboundSessionTx(tx *gorm.DB, tenantID uint, correlationID, content string, aiReply bool, senderID string, deliveredAt *time.Time) error {
	senderType := "agent"
	if aiReply {
		senderType = "ai"
	}
	updates := map[string]any{
		"content":     content,
		"sender_type": senderType,
		"sender_id":   senderID,
	}
	if deliveredAt != nil {
		updates["delivered_at"] = *deliveredAt
	}
	return tx.Model(&model.SessionMessage{}).
		Where("tenant_id = ? AND correlation_id = ?", tenantID, correlationID).
		Updates(updates).Error
}

func prepareOutboundLifecycleTenant(ctx context.Context, msg *model.MessageHub) error {
	if ctx == nil || tenant.IsSystemScope(ctx) {
		return errors.New("outbound lifecycle: tenant scope unavailable")
	}
	tid, ok := tenant.FromContext(ctx)
	if !ok || tid == 0 {
		if !tenant.IsMultiTenant() {
			tid = tenant.DefaultTenantID
		} else {
			return errors.New("outbound lifecycle: tenant scope unavailable")
		}
	}
	if msg.TenantID == 0 {
		msg.TenantID = tid
	}
	if msg.TenantID != tid {
		return errors.New("outbound lifecycle: tenant binding mismatch")
	}
	return nil
}

func hubOutboundCorrelationID(extra model.JSONMap) string {
	for _, key := range []string{"correlation_id", "llm_correlation_id"} {
		if value, ok := extra[key].(string); ok {
			if value = strings.TrimSpace(value); value != "" {
				return value
			}
		}
	}
	return ""
}
