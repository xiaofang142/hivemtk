package service

import (
	"testing"

	"hivemtk-user/internal/channelcaps"
)

// shopChannelsFixture 与 channelgw Default 注册表 2026-11-18 五渠道一致。
var shopChannelsFixture = []string{"taobao", "douyin_shop", "kuaishou_shop", "shopee", "walmart_us"}

// TestShopChannelsOutboundRenderProof 电商店铺五渠道（CH-15/18/19/28/35）出站渲染实证
// （2026-11-19 二次审核：channelcaps 缺 manifest 时 RenderForChannel 直接报
// 「出站不支持该渠道」——AI 回复静默失败，入站/parity 门拦不住，本测试锁死该面）。
//
// 注：service 包测试二进制不链 channelgw（依赖方向 channelgw→service），outbox 能力集
// 在本包测试里由 webhook_outbound_b5_bridge_test.go 的夹具注入——本测试显式注入五渠道
// （镜像生产装配 channelgw/registry.go init：SetBridgeOutboxChannels(Default.OutboundChannels(...))），
// 防止夹具过期导致断言空转。
func TestShopChannelsOutboundRenderProof(t *testing.T) {
	prev := BridgeOutboxChannels()
	SetBridgeOutboxChannels(append(prev, shopChannelsFixture...))
	t.Cleanup(func() { SetBridgeOutboxChannels(prev) })

	for _, ch := range shopChannelsFixture {
		if !channelcaps.Known(ch) {
			t.Errorf("渠道 %s 未在 channelcaps 登记 manifest", ch)
			continue
		}
		svc := &WebhookService{}
		snd, ok := svc.outboundSenderFor(WebhookChannel(ch))
		if !ok {
			t.Errorf("渠道 %s 取不到出站 sender", ch)
			continue
		}
		if _, isBridge := snd.(*bridgeOutboxSender); !isBridge {
			t.Errorf("渠道 %s 应由 bridgeOutboxSender 承接，got %T", ch, snd)
		}
		rendered, err := RenderForChannel(ch, "text", "模拟信号：出站渲染实证（二次审核）", nil)
		if err != nil {
			t.Errorf("渠道 %s 出站渲染失败（AI 回复会静默丢失）: %v", ch, err)
			continue
		}
		if rendered.Text == "" {
			t.Errorf("渠道 %s 渲染正文为空", ch)
		}
	}
}
