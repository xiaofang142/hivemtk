package service

import (
	"context"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/utils/logger"
)

// inbox_ingress_platform_events.go 平台事件分流（specs/024 D2 / T203）。
//
// 三类事件三种处置，唯一共同点是「都不产生一条新消息」：
//   - edited   → 用平台内容覆盖原消息（只认 inbound：编辑的是客户发来的那条）；
//   - deleted  → 标记平台撤回（专用方法，不复用只认 outbound 的 MarkRecalled）；
//   - 其余（reaction / 成员 / 机器人 / 交互回调）→ 只写审计，不改消息表、不计未读。
// 审计是同步写：总线可能丢，但「谁入群了、谁点了哪个按钮」必须留证据。

func (s *InboxIngressService) handlePlatformEvent(ctx context.Context, event *model.MessageEvent, result *InboxIngressResult) (*InboxIngressResult, error) {
	accountID := extraString(event, "account_id")

	switch event.EventType {
	case model.EventTypeMessageEdited:
		if s.hubRepo == nil {
			result.Reason = "edited_no_repo"
			return result, nil
		}
		ok, err := s.hubRepo.UpdateContentByChannelMsgID(ctx, event.Channel, accountID, event.EventID, event.Content)
		if err != nil {
			return result, err
		}
		result.Accepted = ok
		result.Reason = "edited"
	case model.EventTypeMessageDeleted:
		if s.hubRepo == nil {
			result.Reason = "deleted_no_repo"
			return result, nil
		}
		ok, err := s.hubRepo.MarkInboundRecalledByChannelMsgID(ctx, event.Channel, accountID, event.EventID, recallTime(event), extraString(event, "recall_reason"))
		if err != nil {
			return result, err
		}
		result.Accepted = ok
		result.Reason = "deleted"
	case model.EventTypeInteractionCallback:
		result.Accepted = true
		result.Reason = s.recordInteractionCallback(ctx, event, accountID, result)
	default:
		// reaction / group.member_* / bot.*：只留证据。
		result.Accepted = true
		result.Reason = event.EventType
	}

	s.recordChannelEventAudit(ctx, event, accountID, result.RouteRuleID)
	return result, nil
}

// recordInteractionCallback 登记一次卡片点击（幂等），返回对外 reason。
//
// 只登记、不改消息表：点击不是消息，进 message_hub 会污染会话与未读数。
// 答复（answerCallbackQuery）由 webhook 渠道层在拿到本结果后执行，成功后 MarkAnswered。
func (s *InboxIngressService) recordInteractionCallback(ctx context.Context, event *model.MessageEvent, accountID string, result *InboxIngressResult) string {
	interactionID := extraString(event, "interaction_id")
	if interactionID == "" {
		interactionID = event.EventID
	}
	result.InteractionID = interactionID
	if s.interactionRepo == nil {
		return "interaction.callback_no_repo"
	}
	row := &model.ChannelInteraction{
		TenantID:       extraString(event, "tenant_id"),
		Channel:        event.Channel,
		AccountID:      accountID,
		InteractionID:  interactionID,
		ConversationID: event.ConversationID,
		PlatformMsgID:  extraString(event, "platform_msg_id"),
		SenderID:       event.SenderID,
		Action:         extraString(event, "action"),
		Payload:        model.JSONMap{},
	}
	for k, v := range event.Extra {
		switch k {
		case "account_id", "tenant_id":
			continue
		}
		row.Payload[k] = v
	}
	created, err := s.interactionRepo.CreateIdempotent(ctx, row)
	if err != nil {
		logger.Ctx(ctx).Warn().Err(err).Str("channel", event.Channel).
			Str("interaction_id", interactionID).Msg("[Inbox] 交互回调登记失败")
		return "interaction.callback_persist_failed"
	}
	if !created {
		return "interaction.callback.replayed"
	}
	return "interaction.callback"
}

// recordChannelEventAudit 幂等写一条平台事件审计；失败只记日志——
// 审计写不进去不能让平台回调整体失败（否则平台会无限重投）。
func (s *InboxIngressService) recordChannelEventAudit(ctx context.Context, event *model.MessageEvent, accountID string, routeRuleID uint) {
	if s == nil || s.auditRepo == nil || event == nil {
		return
	}
	row := &model.ChannelEventAudit{
		TenantID:       extraString(event, "tenant_id"),
		Channel:        event.Channel,
		AccountID:      accountID,
		ChannelMsgID:   event.EventID,
		EventType:      event.EventType,
		SenderID:       event.SenderID,
		SenderName:     event.SenderName,
		ConversationID: event.ConversationID,
		RouteRuleID:    routeRuleID,
		Payload:        model.JSONMap{},
	}
	if event.Content != "" {
		row.Payload["content"] = event.Content
	}
	for k, v := range event.Extra {
		switch k {
		case "account_id", "tenant_id":
			continue
		}
		row.Payload[k] = v
	}
	if _, err := s.auditRepo.Record(ctx, row); err != nil {
		logger.Ctx(ctx).Warn().Err(err).Str("channel", event.Channel).
			Str("event_type", event.EventType).Str("channel_msg_id", event.EventID).
			Msg("[Inbox] 平台事件审计写入失败")
	}
}

func extraString(event *model.MessageEvent, key string) string {
	if event == nil || event.Extra == nil {
		return ""
	}
	v, _ := event.Extra[key].(string)
	return v
}

func recallTime(event *model.MessageEvent) time.Time {
	if event != nil && !event.Timestamp.IsZero() {
		return event.Timestamp
	}
	return time.Now()
}
