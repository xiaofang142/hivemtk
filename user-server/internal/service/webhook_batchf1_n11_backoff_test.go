package service

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"hivemtk-user/internal/channelbot/core"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/testutil"
)

// 批F-1 / N-11：渠道给出的限流信号在归一层被逐段丢掉（四处环节）。
//
// 本文件的夹具全部是本仓各渠道客户端**当前真实产出**的错误串
// （产出点 file:line 见审计 §5 N-11），不是假想形态：
//   - channelbot/telegram/telegram.go:174  "tg send 429 (rate limited, retry_after=%ds): %s"
//   - channelbot/telegram/telegram.go:453  "tg %s 429 (retry_after=%ds): %s"
//   - service/whatsapp_tier.go:223         "whatsapp pacing rejected: … retry_after=%s"（%s 是 Go Duration）
//   - service/feishu.go:270                "feishu api code %d: %s"
//   - service/wechat.go:302                "wechat send error: %d %s"
//   - channelbot/qq/qq.go:242              "qq send status %d code=%d msg=%s body=%s"
//   - service/wecom.go:526                 errors.New(result.ErrMsg) —— 码根本没进串

// tgRateLimitedRaw Telegram 官方在 429 响应体里给 parameters.retry_after（秒），客户端已读到
// 并拼进错误串，归一层必须把它取回来当等待值。
const tgRateLimitedRaw = `tg send 429 (rate limited, retry_after=30s): {"ok":false,"error_code":429,"description":"Too Many Requests","parameters":{"retry_after":30}}`

// ---------- ① 退避秒数取不到 ----------

func TestN11_TGRetryAfterEqualsFormKeepsDelay(t *testing.T) {
	// (a) 本仓真实产出的整串：修复前它能过，靠的是渠道响应体里原样回显的 `"retry_after":30`
	//     那一段（旧字符类只认 `"` 和 `:`），不是客户端自己拼的 `retry_after=30s`。
	// (b) 只含 `=` 形态的同渠道串：修复前恒解不出，正是 N-11① 记的那处。
	for _, raw := range []string{tgRateLimitedRaw, `tg send 429 (rate limited, retry_after=30s)`} {
		ce := AsChannelError(errors.New(raw))
		if ce.Category != CategoryRateLimited {
			t.Fatalf("夹具 %q：类别 = %s, want rate_limited（串里没有 status 字样，只有裸 429）", raw, ce.Category)
		}
		if ce.RetryAfter != 30*time.Second {
			t.Fatalf("夹具 %q：retry_after=30s 必须解出 30s，got %s", raw, ce.RetryAfter)
		}
	}
}

// TestN11_CallMethod429FormKeepsDelay callMethod（setWebhook 等）形态同上，只是方法名在前。
func TestN11_CallMethod429FormKeepsDelay(t *testing.T) {
	ce := AsChannelError(errors.New(`tg setWebhook 429 (retry_after=7s): {"ok":false,"parameters":{"retry_after":7}}`))
	if ce.RetryAfter != 7*time.Second {
		t.Fatalf("got %s, want 7s", ce.RetryAfter)
	}
}

// TestN11_JSONColonFormStillKeepsDelay 反向守卫：既有的 JSON 冒号形态不得被改坏。
func TestN11_JSONColonFormStillKeepsDelay(t *testing.T) {
	ce := AsChannelError(errors.New(`tg send status 429: {"parameters":{"retry_after":31}}`))
	if ce.RetryAfter != 31*time.Second {
		t.Fatalf("JSON 冒号形态 = %s, want 31s", ce.RetryAfter)
	}
}

// TestN11_DurationFormKeepsFullDelay 本仓自己的 pacing 串把 Go Duration 拼进错误串
// （retry_after=1m0s）：等待值必须是完整一分钟，既不能是 0，也不能被截成 1 秒。
func TestN11_DurationFormKeepsFullDelay(t *testing.T) {
	ce := AsChannelError(errors.New("whatsapp pacing rejected: peer=86138 tier=utility retry_after=1m0s"))
	if ce.RetryAfter != time.Minute {
		t.Fatalf("Duration 形态 = %s, want 1m0s（不能被截成 1s）", ce.RetryAfter)
	}
	if ce.Category != CategoryRateLimited {
		t.Fatalf("pacing 拒绝本质是限流，类别 = %s", ce.Category)
	}
}

// TestN11_BareNumberFormIsSeconds 裸数字按秒，不能被时长解析吃掉。
func TestN11_BareNumberFormIsSeconds(t *testing.T) {
	ce := AsChannelError(errors.New(`status 429: {"ok":false,"parameters":{"retry_after":45}}`))
	if ce.RetryAfter != 45*time.Second {
		t.Fatalf("裸数字必须按秒，got %s", ce.RetryAfter)
	}
}

// TestN11_MalformedRetryAfterDoesNotPanicOrInventDelay 畸形/越界值一律折成"渠道没说"。
func TestN11_MalformedRetryAfterDoesNotPanicOrInventDelay(t *testing.T) {
	for _, raw := range []string{
		`status 429: retry_after=`,
		`status 429: retry_after=abc`,
		`status 429: retry_after=99999999999999999999s`,
		`status 429: 未限流`,
	} {
		if got := AsChannelError(errors.New(raw)).RetryAfter; got != 0 {
			t.Errorf("夹具 %q 不该解出等待值，got %s", raw, got)
		}
	}
}

// ---------- ② 业务码不在判据里（按码不按文案） ----------

// TestN11_FeishuBusinessCodeIsRateLimited 飞书 99991400 出现在 HTTP 200 的响应体里，
// 状态码判据结构上看不见。
func TestN11_FeishuBusinessCodeIsRateLimited(t *testing.T) {
	ce := AsChannelError(errors.New(`feishu api code 99991400: request trigger frequency limit`))
	if ce.Category != CategoryRateLimited {
		t.Fatalf("99991400 应判限流，got %s（当前只认 HTTP status 与文案）", ce.Category)
	}
	if !ce.Retryable {
		t.Fatal("飞书限流是可恢复的，不该落终态")
	}
	if ce.Code != "99991400" {
		t.Fatalf("业务码应留在结构里，got %q", ce.Code)
	}
}

// TestN11_WeChatDailyQuotaIsNotRetryable 微信《返回码说明》原文（本轮 curl 直连取到）：
// "45009 reach max api daily quota limit 接口调用超过限制" ⇒ 当天不会再成功，退避无收益。
func TestN11_WeChatDailyQuotaIsNotRetryable(t *testing.T) {
	ce := AsChannelError(errors.New(`wechat send error: 45009 reach max api daily quota limit`))
	if ce.Category != CategoryQuotaExhausted {
		t.Fatalf("日配额型应单独成类，got %s", ce.Category)
	}
	if ce.Retryable {
		t.Fatal("日配额重试必然再失败，不该进持久化队列")
	}
}

// TestN11_WeChatMinuteQuotaIsRetryable 同渠道不同码：
// "45011 api minute-quota reach limit, must slower, retry next minute" ⇒ 可重试限流。
func TestN11_WeChatMinuteQuotaIsRetryable(t *testing.T) {
	ce := AsChannelError(errors.New(`wechat send error: 45011 api minute-quota reach limit, must slower, retry next minute`))
	if ce.Category != CategoryRateLimited {
		t.Fatalf("分钟级限流应判限流，got %s", ce.Category)
	}
	if !ce.Retryable {
		t.Fatal("分钟级限流下一分钟即可重投")
	}
}

// TestN11_SameCodeDifferentChannel 45009 在企微是"1 分钟后自动解除"（可重试），
// 在公众号是 daily quota（不可重试）：同号不同义，必须按渠道分档。
func TestN11_SameCodeDifferentChannel(t *testing.T) {
	wecom := AsChannelError(errors.New(`wecom send errcode=45009 errmsg=api freq out of limit, rid: 123`))
	if wecom.Category != CategoryRateLimited || !wecom.Retryable {
		t.Fatalf("企微 45009 应为可重试限流，got %s retryable=%v", wecom.Category, wecom.Retryable)
	}
	if wecom.Code != "45009" {
		t.Fatalf("企微 errcode 必须留在错误串与结构里（当前被 errors.New(ErrMsg) 丢掉），got %q", wecom.Code)
	}
	wechat := AsChannelError(errors.New(`wechat send error: 45009 reach max api daily quota limit`))
	if wechat.Category != CategoryQuotaExhausted {
		t.Fatalf("公众号 45009 应为配额型，got %s", wechat.Category)
	}
}

// TestN11_QQPassiveWindowExpiredIsNotRetryable QQ 官方：40034128 被动回复时间或次数超限
// ⇒ 窗口已关，重试必然再失败；40034100 主动消息超频控 ⇒ 退避后可恢复。两类必须分档。
func TestN11_QQPassiveWindowExpiredIsNotRetryable(t *testing.T) {
	expired := AsChannelError(errors.New(`qq send status 400 code=40034128 msg=被动回复时间或次数超限 body={"code":40034128}`))
	if expired.Category != CategoryWindowExpired || expired.Retryable {
		t.Fatalf("被动窗口超限应为不可重试的独立类别，got %s retryable=%v", expired.Category, expired.Retryable)
	}
	throttled := AsChannelError(errors.New(`qq send status 400 code=40034100 msg=主动消息发送超过频控限制 body={"code":40034100}`))
	if throttled.Category != CategoryRateLimited || !throttled.Retryable {
		t.Fatalf("主动频控应为可重试限流，got %s retryable=%v", throttled.Category, throttled.Retryable)
	}
}

// TestN11_AuthFailureIsNotRetryable 取 token 失败被折成常量对外返回（N-11③），
// 于是落进 default 变成 unknown+retryable：该 fail-fast 的被无限重试。
func TestN11_AuthFailureIsNotRetryable(t *testing.T) {
	ce := AsChannelError(errors.New(`get feishu access token failed`))
	if ce.Category != CategoryAuth || ce.Retryable {
		t.Fatalf("取 token 失败属授权类、不该重试，got %s retryable=%v", ce.Category, ce.Retryable)
	}
}

// TestN11_UnknownChannelCodeKeepsTextFallback 未登记的码不得被"看起来像数字"的东西误判：
// 只有裸 3 位数字（无 status 字样）不构成 HTTP 状态码判据。
func TestN11_UnknownChannelCodeKeepsTextFallback(t *testing.T) {
	ce := AsChannelError(errors.New(`wecom send errcode=90303 errmsg=unknown to us`))
	if ce.Category == CategoryQuotaExhausted || ce.Category == CategoryWindowExpired {
		t.Fatalf("未登记的码不得凭空落进新类别，got %s", ce.Category)
	}
	if ce.Code != "90303" {
		t.Fatalf("码值仍要留档，got %q", ce.Code)
	}
}

// ---------- ③ 结构化结果跨层存活 ----------

// TestN11_ClientAPIErrorSurvivesWrapping 客户端用 fmt.Errorf("%w") 逐层包装后，
// 状态码/业务码/等待值仍必须完整到达归一层（这是"判据按码"的落地前提）。
func TestN11_ClientAPIErrorSurvivesWrapping(t *testing.T) {
	inner := &core.APIError{
		Channel:    "telegram",
		StatusCode: 429,
		Code:       "429",
		RetryAfter: 12 * time.Second,
		Raw:        `tg send 429 (rate limited, retry_after=12s): {"ok":false}`,
	}
	wrapped := errors.Join(errors.New("send tg msg:"), inner)

	ce := AsChannelError(wrapped)
	if ce.StatusCode != 429 {
		t.Fatalf("StatusCode = %d, want 429", ce.StatusCode)
	}
	if ce.RetryAfter != 12*time.Second {
		t.Fatalf("RetryAfter = %s, want 12s", ce.RetryAfter)
	}
	if ce.Code != "429" {
		t.Fatalf("Code = %q, want 429", ce.Code)
	}
	if ce.Category != CategoryRateLimited || !ce.Retryable {
		t.Fatalf("类别 = %s retryable=%v", ce.Category, ce.Retryable)
	}
}

// TestN11_ClientAPIErrorCarriesChannelSpecificSemantics 同一码值经客户端带上渠道名，
// 归一层必须按渠道解释（企微 45009 可重试 / 公众号 45009 不可重试）。
func TestN11_ClientAPIErrorCarriesChannelSpecificSemantics(t *testing.T) {
	byWeCom := AsChannelError(&core.APIError{Channel: "wecom", Code: "45009", Raw: "wecom send errcode=45009"})
	if byWeCom.Category != CategoryRateLimited || !byWeCom.Retryable {
		t.Fatalf("企微 45009 = %s retryable=%v", byWeCom.Category, byWeCom.Retryable)
	}
	byWeChat := AsChannelError(&core.APIError{Channel: "wechat", Code: "45009", Raw: "wechat send error: 45009"})
	if byWeChat.Category != CategoryQuotaExhausted || byWeChat.Retryable {
		t.Fatalf("公众号 45009 = %s retryable=%v", byWeChat.Category, byWeChat.Retryable)
	}
}

// TestN11_APIErrorWithoutKnownCodeFallsBackToText 客户端只给状态码、码未登记时，
// 文案判据仍要兜住（HTTP 429 ⇒ 限流）。
func TestN11_APIErrorWithoutKnownCodeFallsBackToText(t *testing.T) {
	ce := AsChannelError(&core.APIError{Channel: "dingtalk", StatusCode: 429, Raw: "dingtalk sessionWebhook status 429: slow down"})
	if ce.Category != CategoryRateLimited || !ce.Retryable {
		t.Fatalf("got %s retryable=%v", ce.Category, ce.Retryable)
	}
}

// ---------- ④ retryDelaysFor / 持久化重试时序接入 ----------

func TestN11_QuotaAndWindowHaveNoInProcessBackoff(t *testing.T) {
	if d := retryDelaysFor(&ChannelError{Category: CategoryQuotaExhausted}); d != nil {
		t.Fatalf("配额型不该有进程内退避，got %v", d)
	}
	if d := retryDelaysFor(&ChannelError{Category: CategoryWindowExpired}); d != nil {
		t.Fatalf("窗口过期型不该有进程内退避，got %v", d)
	}
	if d := retryDelaysFor(&ChannelError{Category: CategoryRateLimited, RetryAfter: 3 * time.Minute}); len(d) != 1 || d[0] != 3*time.Minute {
		t.Fatalf("限流退避应取渠道等待值，got %v", d)
	}
}

// TestN11_EnqueueHonoursChannelDelay N-11④：渠道明确给了等待值，持久化重试队列必须按它排，
// 而不是无条件用 {60s,2m,4m}。
func TestN11_EnqueueHonoursChannelDelay(t *testing.T) {
	db := testutil.NewTestDBOrSkip(t, &model.MessageHub{}, &DelayedOutboundReply{})
	svc := NewWebhookService(db)
	defer svc.Stop(context.Background())

	p := &ParsedPayload{EventID: "evt-n11-1", Sender: "sender-n11-1"}
	hub := &model.MessageHub{ConversationID: "8608489001"}

	svc.enqueueSendRetry(context.Background(), ChannelTelegram, "5", p, "被 flood control 挡住的回复", hub, nil,
		errors.New(`tg send 429 (rate limited, retry_after=300s): {"ok":false,"parameters":{"retry_after":300}}`))

	var rec DelayedOutboundReply
	if err := db.Where("conversation_id = ?", "8608489001").First(&rec).Error; err != nil {
		t.Fatalf("限流失败应入持久化重试队列: %v", err)
	}
	if d := time.Until(rec.SendAt); d < 290*time.Second || d > 310*time.Second {
		t.Fatalf("渠道给了 retry_after=300s，重投时刻就该是 ~300s 后，实际间隔 %s", d)
	}
}

// TestN11_EnqueueHonoursClientStructuredDelay 客户端结构化路径同口径：等待值来自 APIError。
func TestN11_EnqueueHonoursClientStructuredDelay(t *testing.T) {
	db := testutil.NewTestDBOrSkip(t, &model.MessageHub{}, &DelayedOutboundReply{})
	svc := NewWebhookService(db)
	defer svc.Stop(context.Background())

	p := &ParsedPayload{EventID: "evt-n11-4", Sender: "sender-n11-4"}
	hub := &model.MessageHub{ConversationID: "8608489004"}

	err := fmt.Errorf("send tg msg: %w", &core.APIError{
		Channel: "telegram", StatusCode: 429, Code: "429", RetryAfter: 6 * time.Minute,
		Raw: `tg send 429 (rate limited, retry_after=360s): {"ok":false}`,
	})
	svc.enqueueSendRetry(context.Background(), ChannelTelegram, "5", p, "长退避", hub, nil, err)

	var rec DelayedOutboundReply
	if err := db.Where("conversation_id = ?", "8608489004").First(&rec).Error; err != nil {
		t.Fatalf("入队失败: %v", err)
	}
	if d := time.Until(rec.SendAt); d < 355*time.Second || d > 375*time.Second {
		t.Fatalf("结构化 RetryAfter 必须接入出站时序，实际间隔 %s", d)
	}
}

// TestN11_ChannelDelayNeverShortensTableBackoff 反方向：渠道只说等 30s 时不得比表更早
// （"只顺延不提前"）。
func TestN11_ChannelDelayNeverShortensTableBackoff(t *testing.T) {
	db := testutil.NewTestDBOrSkip(t, &model.MessageHub{}, &DelayedOutboundReply{})
	svc := NewWebhookService(db)
	defer svc.Stop(context.Background())

	p := &ParsedPayload{EventID: "evt-n11-2", Sender: "sender-n11-2"}
	hub := &model.MessageHub{ConversationID: "8608489002"}

	svc.enqueueSendRetry(context.Background(), ChannelTelegram, "5", p, "只等 30s 的回复", hub, nil,
		&ChannelError{Category: CategoryRateLimited, Retryable: true, RetryAfter: 30 * time.Second, Raw: "rate limited retry_after=30s"})

	var rec DelayedOutboundReply
	if err := db.Where("conversation_id = ?", "8608489002").First(&rec).Error; err != nil {
		t.Fatalf("入队失败: %v", err)
	}
	if d := time.Until(rec.SendAt); d < 55*time.Second || d > 70*time.Second {
		t.Fatalf("渠道等待值短于表档时不得提前重投，实际间隔 %s", d)
	}
}

// TestN11_QuotaCodeDoesNotEnterRetryLane 配额型失败不得进队列（把队列灌满无效请求）。
func TestN11_QuotaCodeDoesNotEnterRetryLane(t *testing.T) {
	db := testutil.NewTestDBOrSkip(t, &model.MessageHub{}, &DelayedOutboundReply{})
	svc := NewWebhookService(db)
	defer svc.Stop(context.Background())

	p := &ParsedPayload{EventID: "evt-n11-3", Sender: "sender-n11-3"}
	hub := &model.MessageHub{ConversationID: "8608489003"}

	svc.enqueueSendRetry(context.Background(), ChannelWechat, "5", p, "日配额已用完", hub, nil,
		errors.New(`wechat send error: 45009 reach max api daily quota limit`))

	var cnt int64
	if err := db.Model(&DelayedOutboundReply{}).Where("conversation_id = ?", "8608489003").Count(&cnt).Error; err != nil {
		t.Fatalf("count: %v", err)
	}
	if cnt != 0 {
		t.Fatalf("日配额型失败不得入重试队列，实际 %d 条", cnt)
	}
}
