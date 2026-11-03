package repository

// web_embed 出站直投的事务写侧（2026-11-04，BUG-3 配套）。
//
// service 层的架构守卫（arch_gorm_guard_test）要求事务走 Repository 接缝，
// 这里把「落访客消息 + 刷会话尾帧 + hub 行置 delivered」三步收进一个事务方法。

import (
	"context"
	"time"

	"hivemtk-user/internal/model"

	"gorm.io/gorm"
)

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
