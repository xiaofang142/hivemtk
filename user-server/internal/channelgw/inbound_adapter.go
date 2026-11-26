package channelgw

import (
	"context"
	"fmt"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/pkg/utils/logger"
	"hivemtk-user/internal/service"
)

// AttachInboundDispatcher 给具体 pipeline 装配归一化投递器。
//
// 刻意不把 setter 加进 IngressPipeline 接口：仓内多个测试 fake 实现该接口，
// 扩接口会无端破坏它们；装配点用类型断言只作用于真实实现。
func AttachInboundDispatcher(p IngressPipeline, d service.InboundDispatcher) bool {
	impl, ok := p.(*ingressPipeline)
	if !ok || d == nil {
		return false
	}
	impl.inboundDispatcher = d
	return true
}

// deliverInbound channelgw 入口适配器：MessageEvent → InboundMessage 统一投递。
func (p *ingressPipeline) deliverInbound(ctx context.Context, events []*model.MessageEvent) {
	if p == nil || p.inboundDispatcher == nil || len(events) == 0 {
		return
	}
	tenantID, _ := tenant.FromContext(ctx)
	for _, ev := range events {
		if ev == nil {
			continue
		}
		var accountRef string
		if v, ok := ev.Extra["account_id"]; ok && v != nil {
			accountRef = fmt.Sprint(v)
		}
		in := &model.InboundMessage{
			EventID:        ev.EventID,
			Source:         "channelgw_ws",
			Channel:        ev.Channel,
			AccountRef:     accountRef,
			TenantID:       tenantID,
			ConversationID: ev.ConversationID,
			SenderID:       ev.SenderID,
			SenderType:     ev.SenderType,
			SenderName:     ev.SenderName,
			MsgType:        ev.MsgType,
			Content:        ev.Content,
			IsGroup:        ev.IsGroup,
			GroupID:        ev.GroupID,
			Timestamp:      ev.Timestamp.UnixMilli(),
			Extra:          ev.Extra,
		}
		if err := p.inboundDispatcher.Deliver(ctx, in); err != nil {
			logger.Ctx(ctx).Warn().Err(err).Str("module", "channelgw").
				Str("event", "inbound_dispatch_failed").
				Str("event_id", ev.EventID).
				Msg("[channelgw] 归一化投递失败（骨架阶段不影响既有链路）")
		}
	}
}
