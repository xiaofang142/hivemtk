package service

// outbound_sender_im_platforms.go 025 平台批 sender 登记（每个渠道一个工厂）。
//
// 登记单独成文件是为了让 scripts/check_im_touchpoints.py 能静态提取
// 「渠道 → 出站 sender」集合（脚本只扫 outbound_sender_*.go）；实现放在
// outbound_impl_im_platforms.go。

func init() {
	RegisterChannelSender(ChannelSlack, func(s *WebhookService) channelSender { return &SlackOutboundSender{svc: s} })
	RegisterChannelSender(ChannelOneBot, func(s *WebhookService) channelSender { return &OneBotOutboundSender{svc: s} })
	RegisterChannelSender(ChannelWeComCS, func(s *WebhookService) channelSender { return &WeComCSOutboundSender{svc: s} })
	RegisterChannelSender(ChannelWeCombBot, func(s *WebhookService) channelSender { return &WeCombBotOutboundSender{svc: s} })
	RegisterChannelSender(ChannelCustom, func(s *WebhookService) channelSender { return &CustomOutboundSender{svc: s} })
}
