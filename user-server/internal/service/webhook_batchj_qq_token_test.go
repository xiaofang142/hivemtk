package service

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"hivemtk-user/internal/channelbot/core"
	"hivemtk-user/internal/channelbot/qq"
)

// TestBatchJ_QQTokenFailureRetryTier QQ 取凭证腿失败时在出站重试里的档位，端到端跑真客户端：
// 假平台按官方形状回 **HTTP 200 + 响应体 code**，错误串由 qq.go 自己拼，判档走 AsChannelError。
//
// 官方两条硬事实决定这张表（A 档：https://bot.q.qq.com/wiki/develop/api-v2/dev-prepare/access-token.html，
// 批J 2026-09-20 curl 直连 http=200 / 24,598 B / md5 f1acdd7dc218e41635259d0fa7fafea0）：
//   - 「即使调用失败，HTTP 返回码仍为 200。请优先依据 code 判断请求是否成功，不要只依赖 HTTP 返回码」
//     ⇒ 状态码这一路看不见失败；
//   - message「仅用于人工排查，内容可能随时调整」⇒ 文案判据不可依赖。
//     「100001 官方换了文案」那一格就是钉这一条的：只靠 "too many requests" 文本命中的实现，
//     文案一改就静默退化成 Unknown（仍可重试，但限流档的读数与运维口径全丢）。
//
// 补表前这五格全落 CategoryUnknown + Retryable=true：AppID/Secret 错这类"重投不会自己变好"
// 的失败要白撞三轮退避，且不触发 CategoryAuth 那条运维事件（凭证坏了没人知道）。
func TestBatchJ_QQTokenFailureRetryTier(t *testing.T) {
	cases := []struct {
		name          string
		body          string
		wantCode      string
		wantMsg       string
		wantCategory  ChannelErrorCategory
		wantRetryable bool
	}{
		{"100001 限流", `{"code":100001,"message":"Too many requests"}`, "100001",
			"Too many requests", CategoryRateLimited, true},
		{"100001 官方换了文案", `{"code":100001,"message":"please slow down"}`, "100001",
			"please slow down", CategoryRateLimited, true},
		{"100007 AppID 无效", `{"code":100007,"message":"appid invalid"}`, "100007",
			"appid invalid", CategoryAuth, false},
		{"100016 Secret 错", `{"code":100016,"message":"invalid appid or secret"}`, "100016",
			"invalid appid or secret", CategoryAuth, false},
		{"10004 机器人不存在", `{"code":10004,"message":"机器人不存在"}`, "10004",
			"机器人不存在", CategoryAuth, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer srv.Close()

			c := qq.NewClient("app-j", "secret-j", core.WithBaseURL(srv.URL))
			_, err := c.GetAccessToken(context.Background())
			if err == nil {
				t.Fatal("200 + 业务错误码必须报错")
			}
			// 根因（码 + 官方原话）必须先落在错误串里，判档才有东西可判。
			for _, frag := range []string{"code=" + tc.wantCode, tc.wantMsg} {
				if !strings.Contains(err.Error(), frag) {
					t.Errorf("错误串缺根因片段 %q：%v", frag, err)
				}
			}

			ce := AsChannelError(err)
			if ce.Channel != "qq" {
				t.Errorf("渠道 = %q, want qq", ce.Channel)
			}
			if ce.StatusCode != http.StatusOK {
				t.Errorf("StatusCode = %d, want 200（官方明写失败也回 200）", ce.StatusCode)
			}
			if ce.Code != tc.wantCode {
				t.Errorf("业务码 = %q, want %q（提不出码值 ⇒ 判档只能靠文案）", ce.Code, tc.wantCode)
			}
			if ce.Category != tc.wantCategory {
				t.Errorf("档位 = %q, want %q", ce.Category, tc.wantCategory)
			}
			if ce.Retryable != tc.wantRetryable {
				t.Errorf("Retryable = %v, want %v", ce.Retryable, tc.wantRetryable)
			}
			// 落到可观测后果：auth 档不进出站重投预算，限流档才给退避表。
			delays := retryDelaysFor(ce)
			if !tc.wantRetryable && len(delays) != 0 {
				t.Errorf("不可重试却拿到 %v 退避预算（坏凭证白撞三轮）", delays)
			}
			if tc.wantRetryable && len(delays) == 0 {
				t.Error("限流档必须留出退避，否则这条消息当场判死")
			}
			if ce.RetryAfter != 0 {
				t.Errorf("官方未给解除时长，RetryAfter 应为 0 交给退避表，got %v", ce.RetryAfter)
			}
		})
	}
}
