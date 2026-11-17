package service

import (
	"errors"

	"hivemtk-user/internal/pkg/metrics"
)

// 出站可观测三指标（specs/024 T1205）：只加计数，不改投递路径。
// 挂点：markSendFailed / markSendFailedLogged（sendOutbound 的两个失败闭包）、
// sendOutbound 的渲染溢出分支、ChannelRouteEngine.Resolve 的规则命中点。
var (
	// outboundSendFailedTotal 出站投递失败次数，按 {channel, category} 分维。
	// category 取 ChannelError 的类别；非 ChannelError 的裸错误一律归 unknown。
	outboundSendFailedTotal = metrics.NewCounter(
		"outbound_send_failed_total",
		"outbound delivery failures by channel and error category",
		[]string{"channel", "category"},
	)
	// outboundTextOverflowTotal 出站正文超出平台单条上限的次数（只计数，不切分；
	// 切分归各渠道自己，见 outbound_render.go Overflow 口径）。
	outboundTextOverflowTotal = metrics.NewCounter(
		"outbound_text_overflow_total",
		"outbound texts exceeding platform per-message limit, by channel",
		[]string{"channel"},
	)
	// routeRuleHitTotal 入站事件命中渠道路由规则的次数，按 {tenant, target} 分维
	//（target = discard / agent / human，T305 决策口径）。
	routeRuleHitTotal = metrics.NewCounter(
		"route_rule_hit_total",
		"inbound events matched by channel route rules, by tenant and target",
		[]string{"tenant", "target"},
	)
)

// channelErrorCategoryOf 从失败错误中取 ChannelError 类别；
// 非 ChannelError（各 client 的裸 fmt.Errorf）统计为 unknown，
// 保证「哪个渠道、哪类错误在涨」永远有维度可查，而不是丢进 no-label 聚合。
func channelErrorCategoryOf(err error) ChannelErrorCategory {
	if err == nil {
		return CategoryUnknown
	}
	var ce *ChannelError
	if errors.As(err, &ce) && ce.Category != "" {
		return ce.Category
	}
	return CategoryUnknown
}

// recordOutboundSendFailed 出站失败计数（sendOutbound 两个失败闭包共用）。
// 计数失败不影响主流程：指标只是旁路观测，不能反过来弄丢投递错误。
func recordOutboundSendFailed(channel WebhookChannel, err error) {
	if err == nil {
		return
	}
	outboundSendFailedTotal.WithLabel(string(channel), string(channelErrorCategoryOf(err))).Inc()
}
