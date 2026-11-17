package service

import "context"

// TelegramOutboundSender 是渠道 telegram 的出站 sender（specs/024 D1/FR002）。
type TelegramOutboundSender struct{ svc *WebhookService }

func (x *TelegramOutboundSender) Channel() WebhookChannel { return ChannelTelegram }

func (x *TelegramOutboundSender) Send(ctx context.Context, req *OutboundRequest) *OutboundResult {
	return &OutboundResult{Sent: x.svc.outboundTelegram(ctx, req.Channel, req.AccountID, req.Payload, req.Content, req.Hub, req.Cards, req.Fail)}
}

func init() {
	RegisterChannelSender(ChannelTelegram, func(s *WebhookService) channelSender { return &TelegramOutboundSender{svc: s} })
}
