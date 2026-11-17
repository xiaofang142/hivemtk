package service

import "context"

// QQOutboundSender 是渠道 qq 的出站 sender（specs/024 D1/FR002）。
type QQOutboundSender struct{ svc *WebhookService }

func (x *QQOutboundSender) Channel() WebhookChannel { return ChannelQQ }

func (x *QQOutboundSender) Send(ctx context.Context, req *OutboundRequest) *OutboundResult {
	return &OutboundResult{Sent: x.svc.outboundQQ(ctx, req.Channel, req.AccountID, req.Payload, req.Content, req.Hub, req.Cards, req.Fail)}
}

func init() {
	RegisterChannelSender(ChannelQQ, func(s *WebhookService) channelSender { return &QQOutboundSender{svc: s} })
}
