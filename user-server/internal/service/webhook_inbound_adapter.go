package service

import (
	"context"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/pkg/utils/logger"
)

// deliverInbound webhook 入口适配器：解析后的回调载荷归一化为 InboundMessage
// 投递到统一收口。T015 只做骨架；既有事件落库/派发/验签链完全不变。
func (s *WebhookService) deliverInbound(ctx context.Context, req *ReceiveRequest, payload *ParsedPayload) {
	if s == nil || s.inboundDispatcher == nil || req == nil || payload == nil {
		return
	}
	tenantID, _ := tenant.FromContext(ctx)
	in := &model.InboundMessage{
		EventID:        payload.EventID,
		Source:         "webhook",
		Channel:        string(req.Channel),
		AccountRef:     req.AccountID,
		TenantID:       tenantID,
		ConversationID: payload.ChatID,
		SenderID:       payload.Sender,
		Content:        payload.Content,
		Raw:            req.Body,
		Extra:          payload.Extra,
	}
	if err := s.inboundDispatcher.Deliver(ctx, in); err != nil {
		logger.Ctx(ctx).Warn().Err(err).Str("module", "webhook").
			Str("event", "inbound_dispatch_failed").
			Str("event_id", payload.EventID).
			Msg("[Webhook] 归一化投递失败（骨架阶段不影响既有链路）")
	}
}
