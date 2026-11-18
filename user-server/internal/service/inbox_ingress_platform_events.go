package service

import (
	"context"
	"strconv"
	"time"

	"hivemtk-user/internal/event"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tenant"
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
		// 证据链事务（specs/024 三审 T209 契约）：交互登记 + 审计同提交/同回滚；
		// 任一故障必须明确拒绝（平台重投由幂等键兜底，不会风暴）。
		var evErr error
		result.Accepted = true
		result.Reason, evErr = s.recordInteractionCallback(ctx, event, accountID, result)
		if evErr != nil {
			result.Accepted = false
			return result, evErr
		}
	default:
		// reaction / group.member_* / bot.*：只留证据。
		result.Accepted = true
		result.Reason = event.EventType
	}

	// 审计是证据链的一部分（specs/024 三审 T209）：写失败必须拒绝，不再静默放行
	//（旧口径「审计失败不阻断」与证据完整性矛盾——审计写不进去 = 事件没留证）。
	if err := s.recordChannelEventAudit(ctx, event, accountID, result.RouteRuleID); err != nil {
		result.Accepted = false
		return result, err
	}
	publishPlatformEvent(ctx, event, accountID)
	return result, nil
}

// recordInteractionCallback 登记一次卡片点击（幂等），返回对外 reason。
//
// 只登记、不改消息表：点击不是消息，进 message_hub 会污染会话与未读数。
// 答复（answerCallbackQuery）由 webhook 渠道层在拿到本结果后执行，成功后 MarkAnswered。
func (s *InboxIngressService) recordInteractionCallback(ctx context.Context, event *model.MessageEvent, accountID string, result *InboxIngressResult) (string, error) {
	interactionID := extraString(event, "interaction_id")
	if interactionID == "" {
		interactionID = event.EventID
	}
	result.InteractionID = interactionID
	if s.interactionRepo == nil {
		return "interaction.callback_no_repo", nil
	}
	row := &model.ChannelInteraction{
		TenantID:       tenantEventKey(ctx, event),
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
	// 交互登记 + 审计**同事务**（specs/024 三审 T209 契约）：交互及其审计必须
	// 同提交或同回滚，否则证据链出现「有点击没审计」的半证据。
	created, err := s.interactionRepo.RecordCallbackWithAudit(ctx, row, buildChannelEventAuditRow(ctx, event, accountID, result.RouteRuleID))
	if err != nil {
		logger.Ctx(ctx).Warn().Err(err).Str("channel", event.Channel).
			Str("interaction_id", interactionID).Msg("[Inbox] 交互回调登记失败")
		result.Accepted = false
		return "interaction.callback_persist_failed", err
	}
	if !created {
		return "interaction.callback.replayed", nil
	}
	return "interaction.callback", nil
}

// tenantEventKey 平台事件租户键：ctx 绑定的租户是唯一权威（ModeOn fail-closed 口径）；
// Extra["tenant_id"] 是不可信的平台自报值，只在无租户上下文（off 模式）时兜底。
//
// 返回 uint 与渠道事件三张业务表（channel_event_audits / channel_interactions /
// channel_route_rules）的 bigint 列口径一致——租户 id 本身是 uint，早先按字符串
// 落库导致同库两种形态，迁移链的列形态校验直接红。
func tenantEventKey(ctx context.Context, event *model.MessageEvent) uint {
	if tid, ok := tenant.FromContext(ctx); ok && tid != 0 {
		return tid
	}
	if raw := extraString(event, "tenant_id"); raw != "" {
		if v, err := strconv.ParseUint(raw, 10, 64); err == nil {
			return uint(v)
		}
	}
	return 0
}

// buildChannelEventAuditRow 平台事件审计行的单一构造点（recordChannelEventAudit 与
// 交互事务共用，保证两处审计口径逐字段一致）。
func buildChannelEventAuditRow(ctx context.Context, event *model.MessageEvent, accountID string, routeRuleID uint) *model.ChannelEventAudit {
	return &model.ChannelEventAudit{
		TenantID:       tenantEventKey(ctx, event),
		Channel:        event.Channel,
		AccountID:      accountID,
		ChannelMsgID:   event.EventID,
		EventType:      event.EventType,
		SenderID:       event.SenderID,
		SenderName:     event.SenderName,
		ConversationID: event.ConversationID,
		RouteRuleID:    routeRuleID,
	}
}

// MarkInteractionAnswered 在 ack 成功后把交互行置为 answered（specs/024 T210）。
//
// 失败/未命中都返回 false 且不报错：MarkAnswered 本身是幂等条件更新
// （WHERE status='pending'），重复 ack 命中 0 行是正常现象，不是错误。
func (s *InboxIngressService) MarkInteractionAnswered(ctx context.Context, channel, interactionID string) bool {
	if s == nil || s.interactionRepo == nil || interactionID == "" {
		return false
	}
	ok, err := s.interactionRepo.MarkAnswered(ctx, channel, interactionID, time.Now())
	if err != nil {
		logger.Ctx(ctx).Warn().Err(err).Str("channel", channel).
			Str("interaction_id", interactionID).Msg("[Inbox] 交互答复状态回写失败")
		return false
	}
	return ok
}

// recordChannelEventAudit 幂等写一条平台事件审计；失败只记日志——
// 审计写不进去不能让平台回调整体失败（否则平台会无限重投）。
// recordChannelEventAudit 幂等写一条平台事件审计。
//
// 口径升级（specs/024 三审 T209 契约）：审计是证据链的一部分，写失败必须拒绝
// （返回 error，由 handlePlatformEvent 置 Accepted=false）——「审计失败静默放行」
// 会让平台事件无证可查；平台重投由幂等键兜底，不会风暴。
func (s *InboxIngressService) recordChannelEventAudit(ctx context.Context, event *model.MessageEvent, accountID string, routeRuleID uint) error {
	if s == nil || s.auditRepo == nil || event == nil {
		return nil
	}
	row := buildChannelEventAuditRow(ctx, event, accountID, routeRuleID)
	if peerID, ok := event.Extra["peer_id"]; ok {
		row.Payload["peer_id"] = peerID
	}
	if _, err := s.auditRepo.Record(ctx, row); err != nil {
		logger.Ctx(ctx).Warn().Err(err).Str("channel", event.Channel).
			Str("event_type", event.EventType).Str("channel_msg_id", event.EventID).
			Msg("[Inbox] 平台事件审计写入失败")
		return err
	}
	return nil
}

// publishPlatformEvent 把平台事件投到事件总线（specs/024 T204）。
//
// best-effort：Bus 队列满即丢，这里不重试、不阻塞入站——丢了还有 channel_event_audits
// 这一行证据，而阻塞入站会让平台回调超时重投，反而放大问题。
func publishPlatformEvent(ctx context.Context, ev *model.MessageEvent, accountID string) {
	if ev == nil {
		return
	}
	var topic string
	switch ev.EventType {
	case model.EventTypeMessageEdited:
		topic = event.TopicCustomerMessageEdited
	case model.EventTypeMessageDeleted:
		topic = event.TopicCustomerMessageDeleted
	case model.EventTypeMessageReaction:
		topic = event.TopicCustomerMessageReaction
	case model.EventTypeInteractionCallback:
		topic = event.TopicCustomerInteractionCallback
	default:
		topic = event.TopicCustomerGroupMemberChanged // group.member_* / bot.*
	}
	occurredAt := ev.Timestamp
	if occurredAt.IsZero() {
		occurredAt = time.Now()
	}
	payload := event.MessageLifecycleEvent{
		EventID:    ev.EventID,
		Channel:    ev.Channel,
		AccountID:  accountID,
		MsgID:      ev.EventID,
		EventType:  ev.EventType,
		SenderID:   ev.SenderID,
		SessionID:  ev.SessionID,
		OccurredAt: occurredAt,
		Extra:      ev.Extra,
	}
	event.PublishCtx(ctx, topic, payload)
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
