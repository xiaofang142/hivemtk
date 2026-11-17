package service

import "context"

// DingTalkOutboundSender 是渠道 dingtalk 的出站 sender（specs/024 D1/FR002）。
type DingTalkOutboundSender struct{ svc *WebhookService }

func (x *DingTalkOutboundSender) Channel() WebhookChannel { return ChannelDingTalk }

func (x *DingTalkOutboundSender) Send(ctx context.Context, req *OutboundRequest) *OutboundResult {
	return &OutboundResult{Sent: x.svc.outboundDingTalk(ctx, req.Channel, req.AccountID, req.Payload, req.Content, req.Hub, req.Cards, req.Fail)}
}

func init() {
	RegisterChannelSender(ChannelDingTalk, func(s *WebhookService) channelSender { return &DingTalkOutboundSender{svc: s} })
}
