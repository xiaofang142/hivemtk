package service

// web_embed 出站直接投递适配（2026-11-04 T1 线上验收 BUG-3）。
//
// 现状断点：POST /api/message-hub/push 用 platform=web_embed 能落库成 pending 行，
// 但 pending 行只等「扩展认领」或「直接投递渠道在发送 API 成功后自报 delivered」——
// web_embed 是本系统内部的网页客服渠道，没有任何一方来认领，员工从中台发出的
// 消息永远 pending，访客永远收不到。
//
// 修复口径：web_embed 的 outbound 行在 Push 落库后由本服务**直接投递**——
// 定位访客会话 → 落 session_messages（sender_type='agent'，访客轮询/离线拉取都可见）
// → WebSocket 在线则实时推送并记 delivered_at → hub 行置 delivered（extra 记录投递元信息）。
// 与 CustomerSessionService.SendMessage 的 agent 通道同一份落点语义，不新建第二条消息面。
//
// 找不到可投递会话时返回错误但**不回滚 hub 行**：轨迹留着（extra.web_embed_error），
// 管理端能看到这条消息没送出去，好过静默吞掉。

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"hivemtk-user/internal/model"
	dbUtil "hivemtk-user/internal/pkg/db"
	"hivemtk-user/internal/pkg/utils/logger"
	"hivemtk-user/internal/repository"
	"hivemtk-user/internal/websocket"

	"gorm.io/gorm"
)

// DeliverWebEmbedOutbound 把一条 web_embed 出站 hub 行投递到访客会话。
//
// 会话定位两级：ConversationID 非空按 chat session_id 精确找；找不到（或为空）
// 回退到该渠道最近一条未结束会话（与访客离线续接同一套排序口径）。
func (s *MessageHubService) DeliverWebEmbedOutbound(ctx context.Context, msg *model.MessageHub) error {
	if msg == nil {
		return errors.New("nil hub message")
	}
	db := dbUtil.GetDB().WithContext(ctx)
	if db == nil {
		return errors.New("db not ready")
	}

	var session model.CustomerSession
	err := db.Where("platform = ? AND session_id = ?", model.PlatformWebEmbed, msg.ConversationID).
		First(&session).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		err = db.Where("platform = ? AND account_id = ?", model.PlatformWebEmbed, msg.AccountID).
			Where("status NOT IN ?", []model.SessionStatus{model.SessionStatusResolved, model.SessionStatusClosed}).
			Order("last_message_at DESC NULLS LAST, created_at DESC").
			First(&session).Error
	}
	if err != nil {
		return fmt.Errorf("未找到可投递的网页客服会话 (account=%s conv=%s): %w", msg.AccountID, msg.ConversationID, err)
	}

	contentType := model.MessageType(msg.MsgType)
	if contentType == "" {
		contentType = model.MessageTypeText
	}

	// 事务写侧在 Repository（arch 守卫要求：service 不直接开 gorm 事务）。
	if s.repo == nil {
		return errors.New("message hub repo not ready")
	}
	message, txErr := s.repo.DeliverWebEmbedOutboundTx(ctx, repository.WebEmbedDeliveryInput{
		MsgID:       msg.ID,
		Extra:       msg.Extra,
		SessionID:   session.ID,
		ChatSession: session.SessionID,
		Content:     msg.Content,
		ContentType: contentType,
		MediaURL:    msg.MediaURL,
		SenderID:    msg.SenderID,
		SenderName:  msg.SenderName,
	})
	if txErr != nil {
		return txErr
	}

	if websocket.IsVisitorOnline(session.SessionID) {
		payload := map[string]any{
			"session_id":   session.SessionID,
			"id":           message.ID,
			"content":      message.Content,
			"content_type": message.ContentType,
			"media_url":    message.MediaURL,
			"sender_type":  message.SenderType,
			"sender_name":  message.SenderName,
			"sender_id":    message.SenderID,
		}
		if err := websocket.SendToVisitor("message", payload, session.SessionID); err == nil {
			_ = repository.NewSessionMessageRepository().MarkDeliveredAt(ctx, message.ID, time.Now())
		}
	}
	return nil
}

// deliverWebEmbedOutboundLogged Push 旁路的投递入口：失败只 Warn 不阻断落库响应，
// 轨迹已随 hub 行 pending 状态留在待办里可重查。
func (s *MessageHubService) deliverWebEmbedOutboundLogged(ctx context.Context, msg *model.MessageHub) {
	if msg == nil || msg.Direction != "outbound" || msg.Platform != string(model.PlatformWebEmbed) {
		return
	}
	if err := s.DeliverWebEmbedOutbound(ctx, msg); err != nil {
		logger.Warnf("[MessageHub] web_embed 直投失败 msg_id=%s: %v", msg.MsgID, err)
		// 失败轨迹写回 hub 行，管理端可见（不覆盖 pending 状态，待办集合仍能捞起）。
		if db := dbUtil.GetDB().WithContext(ctx); db != nil {
			extra := msg.Extra
			if extra == nil {
				extra = model.JSONMap{}
			}
			extra["web_embed_error"] = err.Error()
			extraJSON, _ := json.Marshal(extra)
			_ = db.Model(&model.MessageHub{}).Where("id = ?", msg.ID).Update("extra", string(extraJSON)).Error
		}
	}
}
