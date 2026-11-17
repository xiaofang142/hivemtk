package service

import "context"

// bridgeOutboxSender 承接全部 bridge_outbox 渠道（channelgw registry 能力投影）。
// 不按渠道常量逐个登记：registry 是唯一渠道清单，出站侧用能力位判定（specs/024 D1）。
type bridgeOutboxSender struct{ svc *WebhookService }

// Channel 返回空串：桥接 sender 由 outboundSenderFor 按调用渠道分派，不是一个具体渠道。
func (x *bridgeOutboxSender) Channel() WebhookChannel { return "" }

func (x *bridgeOutboxSender) Send(ctx context.Context, req *OutboundRequest) *OutboundResult {
	return &OutboundResult{Sent: x.svc.outboundBridge(ctx, req.Channel, req.AccountID, req.Payload, req.Content, req.Hub, req.Cards, req.CardsFallbackCount, req.Fail, req.FailUntracked)}
}
