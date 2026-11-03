package service

import (
	"context"

	"errors"

	"time"

	"hivemtk-user/internal/model"

	"hivemtk-user/internal/repository"
)

func (s *WebhookService) upsertInboxFromHub(ctx context.Context, hub *model.MessageHub, customerName string) {
	if s.inboxConvRepo == nil || hub == nil {
		return
	}
	// BUG-11（2026-11-04）：出站 hub 行只有真正投递成功才该占预览位。桥接/custom
	// 渠道的 pending 行可能永远没人认领（T1 实测 id=866 就是这么挂住的），把
	// 它写成"最后一条消息"会让坐席看到一条客户根本没收到的话。入站照旧，
	// 出站只在 delivered/sent 时刷新，且不增加客户侧未读数。
	outboundPending := hub.Direction == "outbound" &&
		hub.Status != "delivered" && hub.Status != "sent"
	if outboundPending {
		return
	}
	unreadInc := 1
	if hub.Direction == "outbound" {
		unreadInc = 0
	}
	conv, err := s.inboxConvRepo.FindByPlatformAccountCustomer(ctx, hub.Platform, hub.AccountID, hub.SenderID)
	if err == nil && conv != nil {

		_ = s.inboxConvRepo.UpdateLastMessage(ctx, conv.ID, hub.Content, hub.SentAt, unreadInc)
		return
	}
	newConv := &model.InboxConversation{
		Platform:           hub.Platform,
		AccountID:          hub.AccountID,
		CustomerID:         hub.SenderID,
		CustomerName:       customerName,
		LastMessagePreview: hub.Content,
		LastMessageAt:      &hub.SentAt,
		UnreadCount:        unreadInc,
		Status:             InboxStatusUnread,
		CreatedAt:          time.Now(),
		UpdatedAt:          time.Now(),
	}
	// CreateOrUpdateByChannel：并发 webhook 重发撞 uk_inbox_conv_channel 唯一键时
	// 退化为更新既有会话，不再静默丢消息
	_ = s.inboxConvRepo.CreateOrUpdateByChannel(ctx, newConv)
}

func (s *WebhookService) dispatchToUnified(ctx context.Context, um *model.UnifiedMessage) error {
	s.ensureReposFromDB(ctx)
	if s.unifiedMsgRepo == nil {
		return errors.New("unified message repo nil")
	}

	if err := s.unifiedMsgRepo.Create(ctx, um); err != nil {

		if repository.IsDuplicateKeyErr(err) {
			return nil
		}
		return err
	}
	return nil
}
