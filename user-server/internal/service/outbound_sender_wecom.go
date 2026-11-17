package service

import "context"

// WeComOutboundSender 是渠道 wecom 的出站 sender（specs/024 D1/FR002）。
type WeComOutboundSender struct{ svc *WebhookService }

func (x *WeComOutboundSender) Channel() WebhookChannel { return ChannelWeCom }

func (x *WeComOutboundSender) Send(ctx context.Context, req *OutboundRequest) *OutboundResult {
	return &OutboundResult{Sent: x.svc.outboundWeCom(ctx, req.Channel, req.AccountID, req.Payload, req.Content, req.Hub, req.Cards, req.Fail)}
}

func init() {
	RegisterChannelSender(ChannelWeCom, func(s *WebhookService) channelSender { return &WeComOutboundSender{svc: s} })
}
