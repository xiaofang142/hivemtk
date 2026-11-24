package bridge

import (
	"context"

	channelgw "hivemtk-user/internal/channelgw"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/pkg/utils/logger"
	"hivemtk-user/internal/service"
)

// SetInboundDispatcher 装配三路消息归一化投递器（router 装配点调用）。
func (h *BridgeIngestHandler) SetInboundDispatcher(d service.InboundDispatcher) {
	if h != nil && d != nil {
		h.inboundDispatcher = d
	}
}

// deliverInbound bridge HTTP 入口适配器：把 HTTPIngestMessage 归一化为
// InboundMessage 投递到统一收口。不改既有入库/AI 链路，失败只留痕。
func (h *BridgeIngestHandler) deliverInbound(ctx context.Context, channel, accountID, conversationID string, msgs []*channelgw.IngestMessage) {
	if h == nil || h.inboundDispatcher == nil || len(msgs) == 0 {
		return
	}
	tenantID, _ := tenant.FromContext(ctx)
	for _, m := range msgs {
		if m == nil {
			continue
		}
		conv := m.ConversationID
		if conv == "" {
			conv = conversationID
		}
		in := &model.InboundMessage{
			EventID:        m.EventID,
			Source:         "bridge_http",
			Channel:        channel,
			AccountRef:     accountID,
			TenantID:       tenantID,
			ConversationID: conv,
			SenderID:       m.SenderID,
			SenderType:     m.SenderType,
			MsgType:        m.MsgType,
			Content:        m.Content,
			IsGroup:        m.IsGroup,
			GroupID:        m.GroupID,
			Timestamp:      m.Timestamp,
			Extra:          m.Extra,
		}
		if err := h.inboundDispatcher.Deliver(ctx, in); err != nil {
			logger.Ctx(ctx).Warn().Err(err).Str("module", "bridge").
				Str("event", "inbound_dispatch_failed").
				Str("event_id", m.EventID).
				Msg("[Bridge HTTP] 归一化投递失败（骨架阶段不影响既有链路）")
		}
	}
}
