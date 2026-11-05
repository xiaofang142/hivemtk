package repository

// web_embed 出站直投的事务写侧（2026-11-04，BUG-3 配套）。
//
// service 层的架构守卫（arch_gorm_guard_test）要求事务走 Repository 接缝，
// 这里把「落访客消息 + 刷会话尾帧 + hub 行置 delivered」三步收进一个事务方法。

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"hivemtk-user/internal/model"

	"gorm.io/gorm"
)

// errWebEmbedRepoUnavailable 句柄缺失＝装配漏线，不是「没有这条会话」。
var errWebEmbedRepoUnavailable = errors.New("message hub repository not ready")

// WebEmbedDeliveryInput 直投事务的全部写参。
type WebEmbedDeliveryInput struct {
	MsgID       uint
	Extra       model.JSONMap
	SessionID   uint // customer_sessions.id
	ChatSession string
	Content     string
	ContentType model.MessageType
	MediaURL    string
	SenderID    string
	SenderName  string
}

// DeliverWebEmbedOutboundTx 事务落库：session_messages → customer_sessions → message_hub。
// 返回落库的访客侧消息（调用方据此做 WebSocket 实时推送与 delivered_at 标记）。
func (r *MessageHubRepository) DeliverWebEmbedOutboundTx(ctx context.Context, in WebEmbedDeliveryInput) (*model.SessionMessage, error) {
	message := &model.SessionMessage{
		SessionID:   in.ChatSession,
		Content:     in.Content,
		ContentType: in.ContentType,
		MediaURL:    in.MediaURL,
		SenderType:  "agent",
		SenderID:    in.SenderID,
		SenderName:  in.SenderName,
	}
	now := time.Now()
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(message).Error; err != nil {
			return err
		}
		extra := in.Extra
		if extra == nil {
			extra = model.JSONMap{}
		}
		extra["web_embed_delivered_at"] = now.Format(time.RFC3339)
		extra["web_embed_session_id"] = in.ChatSession
		if err := tx.Model(&model.CustomerSession{}).Where("id = ?", in.SessionID).Updates(map[string]any{
			"last_message":      in.Content,
			"last_message_by":   "agent",
			"last_message_at":   now,
			"human_reply_count": gorm.Expr("human_reply_count + 1"),
		}).Error; err != nil {
			return err
		}
		return tx.Model(&model.MessageHub{}).Where("id = ?", in.MsgID).Updates(map[string]any{
			"status": "delivered",
			"extra":  extra,
		}).Error
	})
	if err != nil {
		return nil, err
	}
	return message, nil
}

// MarkDeliveredAt 幂等补写访客侧消息的 delivered_at（WS 推送成功后调用）。
func (r *SessionMessageRepository) MarkDeliveredAt(ctx context.Context, messageID uint, at time.Time) error {
	return r.db.WithContext(ctx).Model(&model.SessionMessage{}).
		Where("id = ?", messageID).Update("delivered_at", &at).Error
}

// FindDeliverableWebEmbedSession 定位可直投的网页客服会话：
// ConversationID 非空先按 chat session_id 精确找；未命中（或为空）回退到该渠道
// 最近一条未结束会话（与访客离线续接同一套排序口径）。
func (r *MessageHubRepository) FindDeliverableWebEmbedSession(ctx context.Context, conversationID, accountID string) (*model.CustomerSession, error) {
	if r == nil || r.db == nil {
		return nil, errWebEmbedRepoUnavailable
	}
	var session model.CustomerSession
	db := r.db.WithContext(ctx)
	err := db.Where("platform = ? AND session_id = ?", model.PlatformWebEmbed, conversationID).
		First(&session).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		err = db.Where("platform = ? AND account_id = ?", model.PlatformWebEmbed, accountID).
			Where("status NOT IN ?", []model.SessionStatus{model.SessionStatusResolved, model.SessionStatusClosed}).
			Order("last_message_at DESC NULLS LAST, created_at DESC").
			First(&session).Error
	}
	if err != nil {
		return nil, err
	}
	return &session, nil
}

// UpdateHubExtra 只写回 hub 行的 extra（web_embed 直投失败轨迹；不动 status，
// 待办集合仍能把 pending 行捞起）。
func (r *MessageHubRepository) UpdateHubExtra(ctx context.Context, msgID uint, extra model.JSONMap) error {
	if r == nil || r.db == nil {
		return errWebEmbedRepoUnavailable
	}
	extraJSON, err := json.Marshal(extra)
	if err != nil {
		return err
	}
	return r.db.WithContext(ctx).Model(&model.MessageHub{}).
		Where("id = ?", msgID).Update("extra", string(extraJSON)).Error
}

// MarkSessionMessageDeliveredAt 幂等补写访客侧消息的 delivered_at（WS 推送成功后调用）。
// 收在 hub 仓储上供直投链路复用，service 无需自行构造 SessionMessageRepository。
func (r *MessageHubRepository) MarkSessionMessageDeliveredAt(ctx context.Context, messageID uint, at time.Time) error {
	if r == nil || r.db == nil {
		return errWebEmbedRepoUnavailable
	}
	return r.db.WithContext(ctx).Model(&model.SessionMessage{}).
		Where("id = ?", messageID).Update("delivered_at", &at).Error
}
