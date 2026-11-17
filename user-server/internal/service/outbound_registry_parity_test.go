package service

import (
	"testing"

	"hivemtk-user/internal/channelcaps"
)

// specs/024 T111 渠道覆盖门禁（outbound_registry_parity_test.go）：
// 把三方集合钉死——直发 sender 注册表 ↔ webhookInboundCapable 入站分支 ↔
// 桥接出站能力集合。三方互为前提：能收不能发=回复静默丢失，
// 能力位认了出站不认=漂移。registry ↔ service 双向恒等在
// internal/channelgw/registry_parity_test.go（外部测试包，避开 import 环）。
// 全部为正向断言，不依赖测试登记时序。

// allDeclaredWebhookChannels 全部 WebhookChannel 渠道常量（webhook_channel_telegram.go + qq）。
// 新增渠道常量必须同步扩这份清单，漏扩会让本文件所有门禁对它失明。
func allDeclaredWebhookChannels() []WebhookChannel {
	return []WebhookChannel{
		ChannelDouyin, ChannelKuaishou, ChannelXiaohongshu, ChannelXianyu, ChannelTiktok,
		ChannelJD, ChannelBilibili, ChannelWeibo, ChannelPersonalWX,
		ChannelWechat, ChannelWeCom, ChannelDingTalk, ChannelWhatsapp, ChannelTelegram,
		ChannelFeishu, ChannelCustom, ChannelQQ,
	}
}

// TestEveryInboundChannelHasSender webhookInboundCapable 的每个渠道必须可出站：
// 非桥接能力的入站渠道必须有直发工厂；桥接能力的走 bridgeOutboxSender 回退。
func TestEveryInboundChannelHasSender(t *testing.T) {
	svc := &WebhookService{}
	checked := 0
	for _, ch := range allDeclaredWebhookChannels() {
		if !webhookInboundCapable(ch) {
			continue
		}
		checked++
		_, hasFactory := outboundSenderFactoryOf(ch)
		bridge := bridgeOutboxCapable(ch)
		if !hasFactory && !bridge {
			t.Errorf("入站渠道 %s 既无直发 sender 工厂也无桥接出站能力位——能收不能发=回复静默丢失", ch)
			continue
		}
		if _, ok := svc.outboundSenderFor(ch); !ok {
			t.Errorf("入站渠道 %s 取不到可用 sender（工厂与回退都失败）", ch)
		}
	}
	if checked < 5 {
		t.Fatalf("入站能力渠道覆盖不足（仅 %d 个）：webhookInboundCapable 分支或渠道常量漂移", checked)
	}
}

// TestEveryBridgeChannelHasSender 桥接出站能力集合的每个渠道必须
// 由 bridgeOutboxSender 承接（集合本体来自 channelgw init 装配，这里钉承接端）。
func TestEveryBridgeChannelHasSender(t *testing.T) {
	svc := &WebhookService{}
	names := BridgeOutboxChannels()
	if len(names) == 0 {
		t.Fatal("桥接出站能力集合为空：channelgw init 装配未执行？")
	}
	for _, name := range names {
		ch := WebhookChannel(name)
		if !bridgeOutboxCapable(ch) {
			t.Errorf("桥接渠道 %s 在 BridgeOutboxChannels 集合内但 bridgeOutboxCapable 判否（判定与集合漂移）", name)
			continue
		}
		snd, ok := svc.outboundSenderFor(ch)
		if !ok {
			t.Errorf("桥接渠道 %s 取不到 sender", name)
			continue
		}
		if _, isBridge := snd.(*bridgeOutboxSender); !isBridge {
			t.Errorf("桥接渠道 %s 应由 bridgeOutboxSender 承接，got %T", name, snd)
		}
	}
}

// TestBridgeOutboxParityWithWebhookOutbound 服务侧恒等（channelgw 侧的另一半在
// internal/channelgw/registry_parity_test.go）：出站桥接分支认定集合必须与
// 能力位判定一致，且桥接族内的 webhookInboundCapable 渠道（douyin/tiktok）
// 不得冒出直发工厂（单一 sender 门）。
func TestBridgeOutboxParityWithWebhookOutbound(t *testing.T) {
	outboundNames := BridgeOutboxChannels()
	if len(outboundNames) == 0 {
		t.Fatal("桥接能力集合为空")
	}
	for _, name := range outboundNames {
		ch := WebhookChannel(name)
		if !bridgeOutboxCapable(ch) {
			t.Errorf("出站认定的桥接渠道 %s 能力位判否（registry 有能力但出站不认的反向漂移）", name)
		}
	}
	svc := &WebhookService{}
	for _, name := range outboundNames {
		ch := WebhookChannel(name)
		if webhookInboundCapable(ch) {
			if _, hasFactory := outboundSenderFactoryOf(ch); hasFactory {
				t.Errorf("桥接渠道 %s 同时登记了直发 sender 工厂——单一 sender 门被破坏", name)
			}
			snd, ok := svc.outboundSenderFor(ch)
			if !ok {
				t.Fatalf("桥接入站渠道 %s 取不到 sender", name)
			}
			if _, isBridge := snd.(*bridgeOutboxSender); !isBridge {
				t.Errorf("桥接入站渠道 %s 应由 bridgeOutboxSender 承接，got %T", name, snd)
			}
		}
	}
}

// TestRegisteredSendersHaveCapabilityManifests 直发 sender 与入站分支的每个渠道
// 都必须在 channelcaps 登记过（出站渲染按 manifest 定格式，未登记渠道渲染直接报错）。
func TestRegisteredSendersHaveCapabilityManifests(t *testing.T) {
	for _, ch := range RegisteredSenderChannels() {
		if !channelcaps.Known(string(ch)) {
			t.Errorf("直发 sender 渠道 %s 未在 channelcaps 登记——渲染层会拒绝它的出站", ch)
		}
	}
	for _, ch := range allDeclaredWebhookChannels() {
		if webhookInboundCapable(ch) && !channelcaps.Known(string(ch)) {
			t.Errorf("入站渠道 %s 未在 channelcaps 登记（入站分流/事件类型声明缺位）", ch)
		}
	}
}
