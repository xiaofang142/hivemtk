package service

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"hivemtk-user/internal/channelbot/core"
	"hivemtk-user/internal/channelbot/qq"
	"hivemtk-user/internal/channelbot/telegram"
	"hivemtk-user/internal/channelbot/whatsapp"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/testutil"
)

// 审计 N-11 的第③条联动：判据要按渠道稳定字段（HTTP 状态码 / 业务码 / 等待值）判，
// 而不是从本仓自己拼的错误串里反解。这些用例从**真实客户端**（httptest 起服务）出发，
// 走一遍"客户端收到响应 → 构造错误 → 归一层判读 → 出站重试通道"整条链，
// 因此同时钉住两端的接缝：客户端不把事实带出来、或归一层不读结构化字段，都会红。

func apiErrIn(t *testing.T, err error) *core.APIError {
	t.Helper()
	var api *core.APIError
	if !errors.As(err, &api) {
		t.Fatalf("错误链里必须带 *core.APIError（渠道事实），got %T: %v", err, err)
	}
	return api
}

// TestN11_ClientChain_Telegram429 Telegram 官方 429：状态码与 parameters.retry_after
// 由客户端当场取出，上层不再靠文案反解。
func TestN11_ClientChain_Telegram429(t *testing.T) {
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"ok":false,"error_code":429,"description":"Too Many Requests: retry after 1","parameters":{"retry_after":1}}`))
	}))
	defer srv.Close()

	c := telegram.NewTelegramClient("tok", core.WithBaseURL(srv.URL))
	_, err := c.SendMessage(context.Background(), 123, "hi")
	if err == nil {
		t.Fatal("持续 429 必须报错")
	}
	api := apiErrIn(t, err)
	if api.Channel != "telegram" {
		t.Errorf("Channel = %q, want telegram", api.Channel)
	}
	if api.StatusCode != 429 {
		t.Errorf("StatusCode = %d, want 429（耗尽后必须留下最后一次的状态码）", api.StatusCode)
	}
	if api.RetryAfter != time.Second {
		t.Errorf("RetryAfter = %s, want 1s（官方 retry_after 单位是秒）", api.RetryAfter)
	}

	ce := AsChannelError(err)
	if ce.Category != CategoryRateLimited || !ce.Retryable {
		t.Errorf("429 应判 rate_limited+可重试，got %s/%v", ce.Category, ce.Retryable)
	}
	if ce.RetryAfter != time.Second {
		t.Errorf("归一层丢了渠道等待值：RetryAfter = %s", ce.RetryAfter)
	}
	if ce.Channel != "telegram" {
		t.Errorf("Channel = %q, want telegram", ce.Channel)
	}
}

// TestN11_ClientChain_Telegram400NotRetryable 4xx 业务错（chat 不存在）当场返回，
// 归一层必须判成不可重试，出站重试通道因此不收这条 —— 重投一条"发给不存在的会话"的消息永远不会成功。
func TestN11_ClientChain_Telegram400NotRetryable(t *testing.T) {
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"ok":false,"error_code":400,"description":"Bad Request: chat not found"}`))
	}))
	defer srv.Close()

	c := telegram.NewTelegramClient("tok", core.WithBaseURL(srv.URL))
	_, err := c.SendMessage(context.Background(), 123, "hi")
	if err == nil {
		t.Fatal("400 必须报错")
	}
	if got := atomic.LoadInt32(&hits); got != 1 {
		t.Errorf("4xx 不该在客户端重试，got %d 次", got)
	}
	api := apiErrIn(t, err)
	if api.StatusCode != 400 {
		t.Errorf("StatusCode = %d, want 400", api.StatusCode)
	}

	db := testutil.NewTestDBOrSkip(t, &model.MessageHub{}, &DelayedOutboundReply{})
	svc := NewWebhookService(db)
	defer svc.Stop(context.Background())
	p := &ParsedPayload{EventID: "evt-n11-chain-400", Sender: "sender-n11-chain-400"}
	hub := &model.MessageHub{ConversationID: "8608489012"}
	svc.enqueueSendRetry(context.Background(), ChannelTelegram, "5", p, "发给不存在会话的回复", hub, nil, err)

	var cnt int64
	if err := db.Model(&DelayedOutboundReply{}).Where("conversation_id = ?", "8608489012").Count(&cnt).Error; err != nil {
		t.Fatalf("count: %v", err)
	}
	if cnt != 0 {
		t.Fatalf("真实 400 不得进重试队列，实际 %d 条", cnt)
	}
}

// TestN11_ClientChain_TelegramMarkdownFallbackKeepsFacts 去掉 Markdown 重试的那一条
// 同样会撞 429（频控按账号计，连着两条必撞）。这条分支曾把错误退化成裸 fmt.Errorf，
// 状态码与 retry_after 全丢 ⇒ 上层只能按文案猜（N-11③）。
func TestN11_ClientChain_TelegramMarkdownFallbackKeepsFacts(t *testing.T) {
	var fallbackHits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body := make([]byte, r.ContentLength)
		_, _ = r.Body.Read(body)
		if strings.Contains(string(body), "parse_mode") {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"ok":false,"error_code":400,"description":"Bad Request: can't parse entities"}`))
			return
		}
		atomic.AddInt32(&fallbackHits, 1)
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"ok":false,"error_code":429,"parameters":{"retry_after":1}}`))
	}))
	defer srv.Close()

	c := telegram.NewTelegramClient("tok", core.WithBaseURL(srv.URL))
	_, err := c.SendMessage(context.Background(), 123, "**hi**", telegram.SendMessageOptions{ParseMode: "Markdown"})
	if err == nil {
		t.Fatal("fallback 撞上 429 必须报错")
	}
	if atomic.LoadInt32(&fallbackHits) == 0 {
		t.Fatal("未走到 fallback 分支，用例失效")
	}
	api := apiErrIn(t, err)
	if api.StatusCode != 429 || api.RetryAfter != time.Second {
		t.Errorf("fallback 分支必须带上 429 与 retry_after，got status=%d retry_after=%s", api.StatusCode, api.RetryAfter)
	}
	if ce := AsChannelError(err); ce.Category != CategoryRateLimited || !ce.Retryable {
		t.Errorf("fallback 429 应判限流，got %s/%v", ce.Category, ce.Retryable)
	}
}

// TestN11_ClientChain_QQCodeDecidesCategory QQ 两条码在 HTTP 层都是 400：
// 40034100（主动消息超频控）退避后可恢复，40034128（被动回复窗口/次数超限）重投必然再失败。
// 只看状态码会把两类混成一档，所以码值必须在客户端当场取出来。
func TestN11_ClientChain_QQCodeDecidesCategory(t *testing.T) {
	cases := []struct {
		code      int
		wantCat   ChannelErrorCategory
		wantRetry bool
	}{
		{40034100, CategoryRateLimited, true},
		{40034128, CategoryWindowExpired, false},
	}
	for _, tc := range cases {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if strings.HasSuffix(r.URL.Path, "/app/getAppAccessToken") {
				_, _ = w.Write([]byte(`{"access_token":"tok","expires_in":"7200"}`))
				return
			}
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]any{"code": tc.code, "message": "api frequency limit", "data": nil})
		}))
		c := qq.NewClient("app-1", "secret-1", core.WithBaseURL(srv.URL))
		_, err := c.SendMessage(context.Background(), qq.SendTarget{GroupOpenID: "GROUP_1", MsgID: "MSG_1", MsgSeq: 1}, "hi")
		srv.Close()
		if err == nil {
			t.Fatalf("code=%d 应报错", tc.code)
		}
		api := apiErrIn(t, err)
		if api.Channel != "qq" || api.Code != strconv.Itoa(tc.code) {
			t.Errorf("APIError 事实不符: channel=%q code=%q", api.Channel, api.Code)
		}
		ce := AsChannelError(err)
		if ce.Category != tc.wantCat || ce.Retryable != tc.wantRetry {
			t.Errorf("code=%d 应判 %s/%v，got %s/%v", tc.code, tc.wantCat, tc.wantRetry, ce.Category, ce.Retryable)
		}
	}
}

// TestN11_ClientChain_WhatsAppErrorCodeDecidesCategory Graph API 的 HTTP 状态码几乎恒为 400，
// 定性靠 error.code：131047（re-engagement，窗口外）该 fail-fast，130429（吞吐到顶）该退避重投。
func TestN11_ClientChain_WhatsAppErrorCodeDecidesCategory(t *testing.T) {
	cases := []struct {
		code      int
		wantCat   ChannelErrorCategory
		wantRetry bool
	}{
		{131047, CategoryAuth, false},
		{130429, CategoryRateLimited, true},
	}
	for _, tc := range cases {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"error": map[string]any{
					"message": "Re-engagement message",
					"type":    "OAuthException",
					"code":    tc.code,
				},
			})
		}))
		c := whatsapp.NewCloudClient("phone-1", "tok", core.WithBaseURL(srv.URL))
		_, err := c.SendText(context.Background(), "8612345678", "hi")
		srv.Close()
		if err == nil {
			t.Fatalf("code=%d 应报错", tc.code)
		}
		api := apiErrIn(t, err)
		if api.Channel != "whatsapp" || api.Code != strconv.Itoa(tc.code) || api.StatusCode != 400 {
			t.Errorf("APIError 事实不符: %+v", api)
		}
		ce := AsChannelError(err)
		if ce.Category != tc.wantCat || ce.Retryable != tc.wantRetry {
			t.Errorf("code=%d 应判 %s/%v，got %s/%v（HTTP 400 不能压过业务码）", tc.code, tc.wantCat, tc.wantRetry, ce.Category, ce.Retryable)
		}
	}
}

// TestN11_ServiceErrorHelpers_WeComAndWeChat 企微/公众号的客户端就在 service 包内，
// 构造点即 helper；两家的 45009 同号不同义（企微"接口调用超过限制…1分钟后自动解除"、
// 公众号"reach max api daily quota limit"），只有构造点把 Channel 与 Code 一起带出来才分得开。
// errmsg 一律取官方《全局错误码》页的说明列原文（见审计 §10 取证），
// 判读靠码值不靠文案 —— 把 errmsg 换成中文描述也不该改变结论。
func TestN11_ServiceErrorHelpers_WeComAndWeChat(t *testing.T) {
	cases := []struct {
		name      string
		err       error
		wantCat   ChannelErrorCategory
		wantRetry bool
		wantAfter time.Duration
	}{
		{"wecom 45009 接口调用超过限制", wecomAPIError("send", 45009, "reach max api daily quota limit"), CategoryRateLimited, true, time.Minute},
		{"wecom 45033 接口并发调用超过限制", wecomAPIError("send", 45033, "concurrent call limit"), CategoryRateLimited, true, time.Minute},
		{"wecom 40001 不合法的secret参数", wecomAPIError("send", 40001, "invalid credential"), CategoryAuth, false, 0},
		{"wecom 40014 不合法的access_token", wecomAPIError("send", 40014, "不合法的access_token"), CategoryAuth, false, 0},
		{"wecom 42001 access_token已过期", wecomAPIError("token", 42001, "access_token有时效性，需要重新获取一次"), CategoryAuth, false, 0},
		{"wechat 45009 日配额到顶", wechatAPIError("send error", 45009, "reach max api daily quota limit"), CategoryQuotaExhausted, false, 0},
		{"wechat 45011 分钟配额", wechatAPIError("send error", 45011, "api minute-quota reach limit, must slower, retry next minute"), CategoryRateLimited, true, time.Minute},
		{"wechat -1 系统繁忙", wechatAPIError("send error", -1, "system error 系统繁忙，此时请开发者稍候再试"), CategoryNetwork, true, 0},
		{"wechat 40001 凭证不合法", wechatAPIError("send error", 40001, "invalid credential, access_token is invalid or not latest"), CategoryAuth, false, 0},
		{"wechat 42001 token 超时", wechatAPIError("send error", 42001, "access_token expired"), CategoryAuth, false, 0},
		{"wechat 41001 缺少 token 参数", wechatAPIError("send error", 41001, "access_token missing"), CategoryAuth, false, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ce := AsChannelError(tc.err)
			if ce.Category != tc.wantCat || ce.Retryable != tc.wantRetry {
				t.Fatalf("got %s/%v，want %s/%v（raw=%s）", ce.Category, ce.Retryable, tc.wantCat, tc.wantRetry, ce.Raw)
			}
			if ce.RetryAfter != tc.wantAfter {
				t.Errorf("RetryAfter = %s, want %s", ce.RetryAfter, tc.wantAfter)
			}
		})
	}
}

// TestN11_AuthProseWithoutCodeStillFailsFast 码值取不到时（错误串只剩 errmsg）凭证类
// 原文仍必须落 auth+不可重试；否则这类失败会被无限重投（N-11③的兜底分支）。
func TestN11_AuthProseWithoutCodeStillFailsFast(t *testing.T) {
	for _, raw := range []string{
		"wecom token reply errmsg=invalid credential",
		"wechat api errmsg=access_token expired",
		"wecom send errmsg=invalid access_token hint",
	} {
		ce := AsChannelError(errors.New(raw))
		if ce.Category != CategoryAuth || ce.Retryable {
			t.Errorf("%q 应判 auth+不可重试，got %s/%v", raw, ce.Category, ce.Retryable)
		}
	}
}

// TestN11_ServiceErrorHelper_Feishu 飞书把等待值放在响应头 x-ogw-ratelimit-reset
// （官方："使用该响应头延迟请求是解除限频的最好方法"），只看状态码与响应体取不到。
func TestN11_ServiceErrorHelper_Feishu(t *testing.T) {
	ce := AsChannelError(feishuCallError(http.StatusOK, []byte(`{"code":99991400,"msg":"request trigger frequency limit"}`), "45"))
	if ce.Category != CategoryRateLimited || !ce.Retryable {
		t.Fatalf("99991400 应判限流，got %s/%v", ce.Category, ce.Retryable)
	}
	if ce.RetryAfter != 45*time.Second {
		t.Errorf("等待值应取响应头，got %s", ce.RetryAfter)
	}
	if ce.Code != "99991400" {
		t.Errorf("业务码应当场取出，got %q", ce.Code)
	}

	bad := AsChannelError(feishuCallError(http.StatusUnauthorized, []byte(`{"code":99991668,"msg":"Invalid token for self built apps"}`), ""))
	if bad.Category != CategoryAuth || bad.Retryable {
		t.Errorf("token 失效应 fail-fast，got %s/%v", bad.Category, bad.Retryable)
	}

	quota := AsChannelError(feishuCallError(http.StatusTooManyRequests, []byte(`{}`), ""))
	if quota.Category != CategoryRateLimited || quota.StatusCode != 429 {
		t.Errorf("裸 429 无业务码时应回落到状态码档，got %s/%d", quota.Category, quota.StatusCode)
	}
}

// TestN11_HeaderOnlyDelayCountsAsRateLimit 飞书把等待值放在响应头里，Raw 天然不带这句话；
// 构造点带来的 Duration 必须自己把结论抬出 unknown 档。
func TestN11_HeaderOnlyDelayCountsAsRateLimit(t *testing.T) {
	ce := AsChannelError(&ChannelError{
		Channel:    string(ChannelFeishu),
		StatusCode: 200,
		RetryAfter: 45 * time.Second,
		Raw:        "feishu api rejected the request",
	})
	if ce.Category != CategoryRateLimited || !ce.Retryable {
		t.Fatalf("只有等待值、文案无特征时应判限流，got %s/%v", ce.Category, ce.Retryable)
	}
	if ce.RetryAfter != 45*time.Second {
		t.Errorf("等待值不得被归一层改写，got %s", ce.RetryAfter)
	}
}

// TestN11_AuthStatusBeatsStrayRetryAfter 403 同时带 retry_after 属自相矛盾的响应：
// 凭证类一律 fail-fast，不能被等待值带进重试通道（否则一个失效 token 会攒出一整条队列）。
func TestN11_AuthStatusBeatsStrayRetryAfter(t *testing.T) {
	ce := AsChannelError(errors.New(`feishu api status 403: {"code":99991672,"msg":"BPT_APP_TENANT_ID_INVALID"} retry_after=30s`))
	if ce.Category != CategoryAuth || ce.Retryable {
		t.Fatalf("403 必须压过 retry_after 判 auth+不可重试，got %s/%v", ce.Category, ce.Retryable)
	}
}

// TestN11_PacingSiteRejectionEntersRetryLane 节奏器（本仓自算的等待值）拒绝的那条，
// 归一层要认成限流可重试，而不是"某条看不懂的失败"。
func TestN11_PacingSiteRejectionEntersRetryLane(t *testing.T) {
	now := time.Date(2026, 9, 20, 11, 0, 0, 0, time.UTC)
	peer := "pacing-n11-" + t.Name()
	var err error
	for i := 0; i < 400; i++ {
		if err = enforceWhatsAppTierPacing(peer, "utility", now); err != nil {
			break
		}
	}
	if err == nil {
		t.Fatal("同一时刻连发 400 次必然触发 pacing 拒绝，用例失效")
	}
	ce := AsChannelError(err)
	if ce.Category != CategoryRateLimited || !ce.Retryable {
		t.Fatalf("pacing 拒绝应判限流可重试，got %s/%v（raw=%s）", ce.Category, ce.Retryable, ce.Raw)
	}
	if ce.RetryAfter <= 0 {
		t.Errorf("节奏器算出的等待值必须以 Duration 带出，got %s", ce.RetryAfter)
	}
	if ce.Channel != string(ChannelWhatsapp) {
		t.Errorf("Channel = %q, want whatsapp", ce.Channel)
	}
}

// TestN11_HTTPStatusSpellingVariants 状态码在真实错误串里有三种写法（渠道体原样回显用
// `"error_code":429`、本仓拼接用 `status %d`、第三方代理用 `status=` / `status:`），
// 字符类少一种就会整条判读退化，所以三种写法都钉住。
func TestN11_HTTPStatusSpellingVariants(t *testing.T) {
	for _, raw := range []string{
		"tg send status 429: Too Many Requests",
		"proxy replied status=429 body=slow down",
		"upstream said status:429",
	} {
		ce := AsChannelError(errors.New(raw))
		if ce.StatusCode != 429 {
			t.Errorf("%q 应解出状态码 429，got %d", raw, ce.StatusCode)
		}
		if ce.Category != CategoryRateLimited || !ce.Retryable {
			t.Errorf("%q 应判限流可重试，got %s/%v", raw, ce.Category, ce.Retryable)
		}
	}
}

// TestN11_RetryAfterProseAloneIsRateLimited 串里既没有状态码也没有任何限流文案，
// 只有渠道给的等待值 —— 这一条本身就足以判限流。
func TestN11_RetryAfterProseAloneIsRateLimited(t *testing.T) {
	ce := AsChannelError(errors.New("wa send peer throttled, retry_after=25s"))
	if ce.Category != CategoryRateLimited || !ce.Retryable {
		t.Fatalf("只有 retry_after 时应判限流可重试，got %s/%v", ce.Category, ce.Retryable)
	}
	if ce.RetryAfter != 25*time.Second {
		t.Errorf("RetryAfter = %s, want 25s", ce.RetryAfter)
	}
}

// TestN11_ClientChain_WhatsAppProseOnlyErrorKeepsBodyEcho Graph 也有不带 error.code 的失败体
// （只有 message 一句 "(#130429) Rate limit hit"）。这时结构化 Code 是空的，
// 唯一的证据就是 Raw 里回显的响应体 ⇒ 构造点一旦不把 body 拼进 Raw，
// 限流会被 400 判成 bad_request（不可重试，直接丢），凭证类同理。
func TestN11_ClientChain_WhatsAppProseOnlyErrorKeepsBodyEcho(t *testing.T) {
	cases := []struct {
		message   string
		wantCat   ChannelErrorCategory
		wantRetry bool
	}{
		{"(#130429) Rate limit hit", CategoryRateLimited, true},
		{"(#131047) Re-engagement message", CategoryAuth, false},
	}
	for _, tc := range cases {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"error": map[string]any{"message": tc.message, "type": "OAuthException"},
			})
		}))
		c := whatsapp.NewCloudClient("phone-1", "tok", core.WithBaseURL(srv.URL))
		_, err := c.SendText(context.Background(), "8612345678", "hi")
		srv.Close()
		if err == nil {
			t.Fatalf("%q 应报错", tc.message)
		}
		api := apiErrIn(t, err)
		if api.Code != "" {
			t.Fatalf("夹具应造出「无 error.code」的响应，got Code=%q", api.Code)
		}
		if !strings.Contains(api.Raw, tc.message) {
			t.Errorf("Raw 未回显响应体：%q", api.Raw)
		}
		ce := AsChannelError(err)
		if ce.Category != tc.wantCat || ce.Retryable != tc.wantRetry {
			t.Errorf("%q 应判 %s/%v，got %s/%v（raw=%s）",
				tc.message, tc.wantCat, tc.wantRetry, ce.Category, ce.Retryable, ce.Raw)
		}
	}
}
