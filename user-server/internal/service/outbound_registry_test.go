package service

import (
	"context"
	"testing"
)

// specs/024 T101 验收：出站发送器注册表。
//
// 测试登记唯一使用 ChannelCustom —— 生产上还没有 custom sender（025 平台批接线，
// BF6 门明确允许 custom 单独允许），登记动作全部放在 parity 子测试之后
//（同文件源码顺序执行），保证「工厂表键集 == 实例键集」断言不被测试自身污染。

// stubOutcomeSender 空实现 sender：既不成功也不调 Fail（R5 不变量的被测对象）。
type stubOutcomeSender struct{ svc *WebhookService }

func (x *stubOutcomeSender) Channel() WebhookChannel { return ChannelCustom }

func (x *stubOutcomeSender) Send(_ context.Context, _ *OutboundRequest) *OutboundResult {
	return &OutboundResult{}
}

// registerCustomSenderForTest 登记测试用 custom sender；全量跑时可能已被其他用例
// 登记过（注册表无注销口），这里吞掉「重复登记」panic，让各用例独立可跑。
func registerCustomSenderForTest(f OutboundSenderFactory) {
	func() {
		defer func() { _ = recover() }()
		RegisterChannelSender(ChannelCustom, f)
	}()
}

func TestOutboundRegistry(t *testing.T) {
	svc := &WebhookService{}

	t.Run("factory_keys_match_instance_keys", func(t *testing.T) {
		// 静态工厂表键集 == 实例注册表键集：每个工厂键在新 service 上都能取到
		// 同一惰性单例实例（R6 两张表由同一测试校验键集相等）。
		channels := RegisteredSenderChannels()
		if len(channels) == 0 {
			t.Fatal("出站发送器注册表为空：outbound_sender_*.go 的 init 未执行？")
		}
		for _, ch := range channels {
			s1, ok1 := svc.outboundSenderFor(ch)
			if !ok1 || s1 == nil {
				t.Fatalf("工厂表渠道 %s 无法构造实例 sender", ch)
			}
			s2, ok2 := svc.outboundSenderFor(ch)
			if !ok2 || s2 != s1 {
				t.Fatalf("渠道 %s 的实例 sender 未按惰性单例缓存（两次取值不同）", ch)
			}
			if s1.Channel() != ch {
				t.Fatalf("渠道 %s 的 sender 自报渠道为 %s（注册键与实现漂移）", ch, s1.Channel())
			}
		}
	})

	t.Run("registered_channels_sorted_stable", func(t *testing.T) {
		// RegisteredSenderChannels 按 channel 字典序稳定升序（parity/文档生成依赖）。
		a := RegisteredSenderChannels()
		b := RegisteredSenderChannels()
		if len(a) != len(b) {
			t.Fatalf("两次读取注册表长度不稳定: %d vs %d", len(a), len(b))
		}
		for i := range a {
			if a[i] != b[i] {
				t.Fatalf("注册表两次读取不一致: %v vs %v", a, b)
			}
			if i > 0 && a[i] <= a[i-1] {
				t.Fatalf("注册表未按字典序稳定升序: %v", a)
			}
		}
	})

	t.Run("direct_vocabulary_frozen", func(t *testing.T) {
		// 直发渠道词表冻结（specs/024 B1 + specs/025 平台批）：12 个直发渠道。
		// 新增渠道必须在对应批次里有意识扩表（tests+T1201 触点矩阵），
		// 不允许悄悄注册进来；bridge 族不在此表（走能力位回退，由 parity 测试钉住）。
		want := []WebhookChannel{ChannelCustom, ChannelDingTalk, ChannelFeishu, ChannelOneBot,
			ChannelQQ, ChannelSlack, ChannelTelegram, ChannelWechat, ChannelWeCom,
			ChannelWeCombBot, ChannelWeComCS, ChannelWhatsapp}
		got := RegisteredSenderChannels()
		if len(got) != len(want) {
			t.Fatalf("直发 sender 渠道数漂移：want %v got %v（多出的渠道必须在 T111/词表/触点矩阵同步扩表）", want, got)
		}
		for i := range want {
			if got[i] != want[i] {
				t.Fatalf("直发 sender 渠道集漂移：want %v got %v", want, got)
			}
		}
	})

	t.Run("unknown_channel_not_deliverable", func(t *testing.T) {
		// 未注册且非桥接能力的渠道必须取不到 sender（sendOutbound 的明确拒绝分支）。
		if snd, ok := svc.outboundSenderFor(WebhookChannel("zz-unknown-channel")); ok || snd != nil {
			t.Fatalf("未注册渠道被取到 sender: ok=%v sender=%v", ok, snd)
		}
	})

	t.Run("empty_channel_panics", func(t *testing.T) {
		defer func() {
			if r := recover(); r == nil {
				t.Fatal("空渠道注册未 panic（注册表是契约，空渠道必须炸在登记时）")
			}
		}()
		RegisterChannelSender("", func(*WebhookService) channelSender { return nil })
	})

	t.Run("nil_factory_panics", func(t *testing.T) {
		defer func() {
			if r := recover(); r == nil {
				t.Fatal("nil 工厂注册未 panic（静默 nil 会让出站取到空实现）")
			}
		}()
		RegisterChannelSender(ChannelCustom, nil)
	})
}

// TestSenderOutcomeMustBeExplicit R5 不变量（specs/024 T101 二次审核补）：
// sender「既没发成功也没调 Fail」返回时，sendOutbound 必须兜成
// unknown + 可重试的失败——否则重试不入队、审计不落行，回复静默丢失。
func TestSenderOutcomeMustBeExplicit(t *testing.T) {
	releaseReplyClaimForTest(t, "evt-r5-explicit")

	// 直接注入实例缓存：生产已登记 custom sender，注册表无注销口，
	// 这里用缓存覆盖走同一 R5 判定路径，且不依赖登记时序。
	svc := &WebhookService{senders: map[WebhookChannel]channelSender{
		ChannelCustom: &stubOutcomeSender{},
	}}
	p := &ParsedPayload{EventID: "evt-r5-explicit", Sender: "cust-r5", Content: "在吗"}
	sent, sendErr := svc.sendOutbound(context.Background(), ChannelCustom, "acct-r5", p, "你好", nil, nil)
	if sent {
		t.Fatal("空实现 sender（既不成功也不报错）不得判 sent=true")
	}
	if sendErr == nil {
		t.Fatal("sender 未表态时必须被 R5 不变量兜成失败，sendErr 不得为空")
	}
	ce := AsChannelError(sendErr)
	if ce == nil {
		t.Fatalf("兜底失败必须是 *ChannelError，got %T: %v", sendErr, sendErr)
	}
	if ce.Category != CategoryUnknown || !ce.Retryable {
		t.Errorf("兜底口径应为 unknown+可重试，got category=%s retryable=%v", ce.Category, ce.Retryable)
	}
}

// TestOutboundRegistry_DuplicateRegistrationPanics 重复登记必须 panic：
// 两个实现争同一渠道时静默覆盖会让线上行为取决于 init 顺序。
func TestOutboundRegistry_DuplicateRegistrationPanics(t *testing.T) {
	registerCustomSenderForTest(func(*WebhookService) channelSender { return &stubOutcomeSender{} })
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("重复登记 ChannelCustom 未 panic")
		}
	}()
	RegisterChannelSender(ChannelCustom, func(*WebhookService) channelSender { return &stubOutcomeSender{} })
}
