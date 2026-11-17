package service

import "context"

// WhatsappOutboundSender 是渠道 whatsapp 的出站 sender（specs/024 D1/FR002）。
type WhatsappOutboundSender struct{ svc *WebhookService }

func (x *WhatsappOutboundSender) Channel() WebhookChannel { return ChannelWhatsapp }

func (x *WhatsappOutboundSender) Send(ctx context.Context, req *OutboundRequest) *OutboundResult {
	return &OutboundResult{Sent: x.svc.outboundWhatsapp(ctx, req.Channel, req.AccountID, req.Payload, req.Content, req.Hub, req.Cards, req.Fail)}
}

func init() {
	RegisterChannelSender(ChannelWhatsapp, func(s *WebhookService) channelSender { return &WhatsappOutboundSender{svc: s} })
}
