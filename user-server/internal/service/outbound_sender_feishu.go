package service

import "context"

// FeishuOutboundSender 是渠道 feishu 的出站 sender（specs/024 D1/FR002）。
type FeishuOutboundSender struct{ svc *WebhookService }

func (x *FeishuOutboundSender) Channel() WebhookChannel { return ChannelFeishu }

func (x *FeishuOutboundSender) Send(ctx context.Context, req *OutboundRequest) *OutboundResult {
	return &OutboundResult{Sent: x.svc.outboundFeishu(ctx, req.Channel, req.AccountID, req.Payload, req.Content, req.Hub, req.Cards, req.Fail)}
}

func init() {
	RegisterChannelSender(ChannelFeishu, func(s *WebhookService) channelSender { return &FeishuOutboundSender{svc: s} })
}
