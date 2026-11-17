package service

import "context"

// WechatOutboundSender 是渠道 wechat 的出站 sender（specs/024 D1/FR002）。
type WechatOutboundSender struct{ svc *WebhookService }

func (x *WechatOutboundSender) Channel() WebhookChannel { return ChannelWechat }

func (x *WechatOutboundSender) Send(ctx context.Context, req *OutboundRequest) *OutboundResult {
	return &OutboundResult{Sent: x.svc.outboundWechat(ctx, req.Channel, req.AccountID, req.Payload, req.Content, req.Hub, req.Cards, req.Fail)}
}

func init() {
	RegisterChannelSender(ChannelWechat, func(s *WebhookService) channelSender { return &WechatOutboundSender{svc: s} })
}
