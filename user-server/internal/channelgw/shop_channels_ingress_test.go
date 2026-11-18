// shop_channels_ingress_test.go 电商店铺五渠道（CH-15/18/19/28/35）全链路模拟信号贯通。
//
// 2026-11-18 服务端全链注册批（specs/024 接缝复用）：taobao / douyin_shop /
// kuaishou_shop / shopee / walmart_us 五渠道以 fail-closed 桥骨架 + 服务端全链注册
// 落地。本文件用**模拟信号**（合成 wire 帧，真实 PG 8217 受控变量）钉住五渠道的
// 服务端主链路三格：
//
//	① 入站贯通：合成帧经生产管道 IngestBatch 被注册表接受（accepted+queued_for_ai）
//	   → message_hub 落行 platform=canonical、direction=inbound；
//	② AI 触发身份元组：触发器收到的 channel 就是 canonical 渠道名（别名归一后）；
//	③ 出站能力位：五渠道全部在 BridgeOutboxChannels 能力集合内（bridge_outbox
//	   单一 sender 承接，AI 回复落 message_hub pending——该格由
//	   outbound_registry_parity_test.go TestEveryBridgeChannelHasSender 对全部
//	   桥接渠道统一承载，此处只对账集合成员防漂移）。
//
// 口径：模拟信号验收 ≠ 真机校准。桥侧适配器 fail-closed（SEL 零证据拒启），
// 真机校准（授权账号现场取 DOM）后按 pdd 模式补选择器——见
// user-web/bridge/test/channel-shop-channels-failclosed.test.js 与
// docs/未完成任务清单.md §3.9 各行。
package channelgw

import (
	"context"
	"testing"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/db"
	"hivemtk-user/internal/pkg/testutil"
	"hivemtk-user/internal/service"
)

// shopChannels 五渠道 canonical 名（与 registry.go/model 常量同源）。
var shopChannels = []string{"taobao", "douyin_shop", "kuaishou_shop", "shopee", "walmart_us"}

// shopPipeline 真实管道 + 真库 + 可计数触发器（夹具同 cg01Pipeline，白名单换五渠道）。
func shopPipeline(t *testing.T) (IngressPipeline, *cg01CountingTrigger) {
	t.Helper()
	database := testutil.NewTestDB(t, &model.MessageHub{})
	db.SetTestDB(database)
	t.Cleanup(func() { db.SetTestDB(nil) })
	ingress := service.NewInboxIngressService()
	tr := &cg01CountingTrigger{}
	ingress.SetAITrigger(tr)
	service.SetBridgeChannels(shopChannels)
	t.Cleanup(func() { service.SetBridgeChannels(nil) })
	service.SetGlobalInboxIngressService(ingress)
	t.Cleanup(func() { service.SetGlobalInboxIngressService(nil) })
	return NewPipeline(ingress), tr
}

// shopFrame 五渠道合成入站帧（wire 字段与 cs01 样本同形，仅渠道/ID 前缀换）。
func shopFrame(channel, eventID string) *model.MessageEvent {
	return (&IngestMessage{
		EventID:        eventID,
		Channel:        channel,
		AccountID:      "shop-acc-" + channel,
		ConversationID: "shop-conv-" + channel + "-001",
		SenderID:       "shop-cust-" + channel + "-001",
		ReceiverID:     "shop-acc-" + channel,
		SenderType:     "customer",
		MsgType:        "text",
		Content:        "模拟信号：店铺客服咨询样本（" + channel + "）",
		Timestamp:      time.Date(2026, 11, 18, 10, 0, 0, 0, time.UTC).UnixMilli(),
	}).ToEvent("http")
}

// TestShopChannels_FiveChannelIngressFullChain 五渠道逐个：入站接受→message_hub 落行
// →AI 触发身份元组=canonical 渠道。别名归一格一并核（taobao_web→taobao）。
func TestShopChannels_FiveChannelIngressFullChain(t *testing.T) {
	for _, ch := range shopChannels {
		t.Run(ch, func(t *testing.T) {
			p, tr := shopPipeline(t)
			ctx := context.Background()

			eventID := "shop-evt-" + ch + "-0001"
			res, err := p.IngestBatch(ctx, []*model.MessageEvent{shopFrame(ch, eventID)})
			if err != nil {
				t.Fatalf("%s 入站失败: %v", ch, err)
			}
			if !res.TriggeredAI {
				t.Errorf("%s triggered_ai 应为 true", ch)
			}
			if len(res.PerEvent) != 1 || res.PerEvent[0] == nil {
				t.Fatalf("%s per_event[0] 必须非空回读", ch)
			}
			if !res.PerEvent[0].Accepted {
				t.Errorf("%s accepted = false（注册表应接受已注册桥渠道）", ch)
			}
			if !res.PerEvent[0].QueuedForAI {
				t.Errorf("%s queued_for_ai = false，reason=%q", ch, res.PerEvent[0].Reason)
			}
			if snap := tr.snapshot(); snap["channel"] != ch {
				t.Errorf("%s AI 触发身份元组 channel=%q，期望 canonical %q（别名归一后）", ch, snap["channel"], ch)
			}

			// message_hub 落行：direction=inbound、platform=canonical。
			var hub model.MessageHub
			if err := db.GetDB().Where("direction = ? AND platform = ?", "inbound", ch).
				Order("id desc").First(&hub).Error; err != nil {
				t.Fatalf("%s message_hub 入站行缺失（入站能进但轨迹落不了库）: %v", ch, err)
			}
			if hub.ConversationID != "shop-conv-"+ch+"-001" {
				t.Errorf("%s 落行会话不符: %q", ch, hub.ConversationID)
			}
		})
	}
}

// TestShopChannels_BridgeOutboxCapability 出站能力位：五渠道全部登记 bridge_outbox
// （AI 回复经 bridgeOutboxSender 落 message_hub pending；承接端由 parity 测试统一钉）。
func TestShopChannels_BridgeOutboxCapability(t *testing.T) {
	set := map[string]bool{}
	for _, n := range service.BridgeOutboxChannels() {
		set[n] = true
	}
	for _, ch := range shopChannels {
		if !set[ch] {
			t.Errorf("渠道 %s 未登记 bridge_outbox 能力位（能收不能发=回复静默丢失）", ch)
		}
	}
}

// TestShopChannels_RegisteredAndExperimental 五渠道在注册表已注册且如实标实验
// （链路可跑但选择器未真机校准——不得静默转正）。
func TestShopChannels_RegisteredAndExperimental(t *testing.T) {
	for _, ch := range shopChannels {
		if !Default.IsChannel(ch) {
			t.Errorf("渠道 %s 未在 Default 注册表注册", ch)
		}
		if !Default.IsExperimental(ch) {
			t.Errorf("渠道 %s 应如实标 Experimental（fail-closed 骨架不得按生产口径宣传）", ch)
		}
	}
}
