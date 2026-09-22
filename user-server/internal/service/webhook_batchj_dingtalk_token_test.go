package service

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestBatchJ_DingTalkTokenLegShapes 钉钉媒体链第一条腿（POST /v1.0/oauth2/accessToken）的
// 四格形状。此前全仓零用例：既有的钉钉下载用例是整条换掉 dtMediaFetchFn（见
// webhook_batchf4_m01_dingtalk_test.go:70 的说明），所以这两个写死主机名的 URL 上
// 没有任何一行解析代码被执行过 —— "HTTP 200 但 accessToken 为空"这一格尤其危险，
// 因为空凭证会被当成合法值送进下一条腿的 x-acs-dingtalk-access-token 头。
// 与抖音批G-2c 的 TestG2B_EmptyTokenResponseIsTerminalAndUncached 同形。
//
// 判据不依赖钉钉官方错误体：open.dingtalk.com 是 SPA，示例正文取不到可引用原文
// （审计 §6 已记），所以这里钉的是"取不到凭证必须报错、且把响应体带出来"这一
// 形状无关的口径。
func TestBatchJ_DingTalkTokenLegShapes(t *testing.T) {
	prevBase := dingtalkOpenAPIBase
	t.Cleanup(func() { dingtalkOpenAPIBase = prevBase })

	cases := []struct {
		name            string
		tokenStatus     int
		tokenBody       string
		wantErrHas      []string
		wantDownloadOn  int
		wantTokenHeader string
	}{
		{
			// 1) 200 + 空 accessToken：必须判失败，且把响应体片段带出来（否则现场只剩
			//    "empty" 一个字，看不出是 appKey 错还是权限没开）。
			name: "200-空accessToken", tokenStatus: http.StatusOK,
			tokenBody:      `{"accessToken":"","expireIn":7200}`,
			wantErrHas:     []string{"token empty status 200", `accessToken":""`},
			wantDownloadOn: 0,
		},
		{
			// 2) 200 + 根本不是 JSON（网关/登录页改写）：报根因要靠响应体片段，
			//    只报 json 语法错等于把"谁在应答"丢了。
			name: "200-非JSON应答", tokenStatus: http.StatusOK,
			tokenBody:      `<html><head><title>502 Bad Gateway</title></head></html>`,
			wantErrHas:     []string{"token parse", "502 Bad Gateway"},
			wantDownloadOn: 0,
		},
		{
			// 3) 非 200：状态码必须原样带出（判档要靠它）。
			name: "403-空accessToken", tokenStatus: http.StatusForbidden,
			tokenBody:      `{"accessToken":""}`,
			wantErrHas:     []string{"token empty status 403"},
			wantDownloadOn: 0,
		},
		{
			// 4) 对照组：拿到真凭证时后一条腿**必须**被打到，且带的就是这把凭证。
			//    缺了这条，前三条可以靠"整条链永远直接报错"绿过去。
			name: "200-拿到凭证", tokenStatus: http.StatusOK,
			tokenBody:       `{"accessToken":"TK-DT-1","expireIn":7200}`,
			wantErrHas:      []string{"download empty url"},
			wantDownloadOn:  1,
			wantTokenHeader: "TK-DT-1",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var tokenCalls, downloadCalls int
			var gotTokenHeader string
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/v1.0/oauth2/accessToken":
					tokenCalls++
					w.WriteHeader(tc.tokenStatus)
					_, _ = w.Write([]byte(tc.tokenBody))
				case "/v1.0/robot/messageFiles/download":
					downloadCalls++
					gotTokenHeader = r.Header.Get("x-acs-dingtalk-access-token")
					_, _ = w.Write([]byte(`{"downloadUrl":""}`))
				default:
					t.Errorf(" unexpected path %s", r.URL.Path)
				}
			}))
			defer srv.Close()
			dingtalkOpenAPIBase = srv.URL

			_, _, err := FetchDingTalkRobotMedia(context.Background(), "appkey-j", "secret-j", "robot-j", "code-j")
			if err == nil {
				t.Fatalf("必须报错：四格都不该有媒体返回")
			}
			for _, want := range tc.wantErrHas {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("错误里缺 %q，got %q", want, err)
				}
			}
			if tokenCalls != 1 {
				t.Errorf("token 腿调用 %d 次, want 1", tokenCalls)
			}
			if downloadCalls != tc.wantDownloadOn {
				t.Errorf("download 腿调用 %d 次, want %d（换不到凭证就不该再敲下一条腿）", downloadCalls, tc.wantDownloadOn)
			}
			if gotTokenHeader != tc.wantTokenHeader {
				t.Errorf("download 腿收到的凭证头 = %q, want %q", gotTokenHeader, tc.wantTokenHeader)
			}
		})
	}
}

// TestBatchJ_DingTalkTokenErrorIsNotRetryable 凭证取不到这一格在出站重试里的档位。
// 官方 v1.0 的失败体里没有 errcode= 数字（reChannelCode["dingtalk"] 认的是 `errcode=`），
// 所以错误串里没有可比对的码值 ⇒ 现状必然落 CategoryUnknown。
// 这里钉的是**当前可证的边界**：它绝不能被读成"网络抖一下"以外的人手口径，
// 也不能因为 Unknown 而漏掉轨迹。真正的"该判 auth、不重试"需要钉钉错误码 A 档原文，
// 已按批J 登记为未证项（见审计 §17.7），拿到原文前不凭猜改判据。
func TestBatchJ_DingTalkTokenErrorIsNotRetryable(t *testing.T) {
	prevBase := dingtalkOpenAPIBase
	t.Cleanup(func() { dingtalkOpenAPIBase = prevBase })

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"accessToken":"","expireIn":7200}`))
	}))
	defer srv.Close()
	dingtalkOpenAPIBase = srv.URL

	_, _, err := FetchDingTalkRobotMedia(context.Background(), "appkey-j", "secret-j", "robot-j", "code-j")
	if err == nil {
		t.Fatal("空凭证必须报错")
	}
	ce := AsChannelError(err)
	if ce.Channel != "dingtalk" {
		t.Errorf("渠道识别 = %q, want dingtalk（错误串前缀是判档的第一依据）", ce.Channel)
	}
	if ce.Code != "" {
		t.Errorf("Code = %q, want 空（现状拿不到 errcode，登记在案的未证项）", ce.Code)
	}
	if ce.StatusCode != http.StatusOK {
		t.Errorf("StatusCode = %d, want 200（钉钉这一格是「200 带空值」，状态码必须落进结构里）", ce.StatusCode)
	}
}

// TestBatchJ_DingTalkPresignedURLQueryNeverReachesError 预签名链接等价于一把临时凭证：
// 它的查询串（Signature / security-token / Expires）不能出现在错误串里——这条错误会被
// 异步转存原样打进日志（dingtalk_media.go 的「媒体下载失败」），日志是长期留存的、
// 而链接在有效期内任何人拿到都能直接下载客户的原始文件。
// 拒绝非 https 的形态本身要保留（域名可判因），要抹掉的只有查询部分。
func TestBatchJ_DingTalkPresignedURLQueryNeverReachesError(t *testing.T) {
	prevBase := dingtalkOpenAPIBase
	t.Cleanup(func() { dingtalkOpenAPIBase = prevBase })

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1.0/oauth2/accessToken":
			_, _ = w.Write([]byte(`{"accessToken":"TK-J-2","expireIn":7200}`))
		case "/v1.0/robot/messageFiles/download":
			_, _ = w.Write([]byte(`{"downloadUrl":"http://presigned.example/pic.png?Signature=SIG-LEAK-J&security-token=ST-LEAK-J"}`))
		default:
			t.Errorf("不该请求 %s", r.URL.Path)
		}
	}))
	defer srv.Close()
	dingtalkOpenAPIBase = srv.URL

	_, _, err := FetchDingTalkRobotMedia(context.Background(), "appkey-j", "secret-j", "robot-j", "code-j")
	if err == nil {
		t.Fatal("非 https 的预签名链接必须拒绝")
	}
	msg := err.Error()
	if !strings.Contains(msg, "download url rejected") {
		t.Errorf("错误里缺「rejected」判因，got %q", msg)
	}
	for _, leak := range []string{"SIG-LEAK-J", "ST-LEAK-J"} {
		if strings.Contains(msg, leak) {
			t.Errorf("预签名凭证参数 %s 落进了错误串（会随日志长期留存）：%q", leak, msg)
		}
	}
}

// TestBatchJ_DingTalkAccessTokenNeverReachesParseError 凭证响应被中间层改写到一半时（截断的 JSON、
// 网关插进来的 HTML），响应体片段是要带进错误的（否则分不出"钉钉答了"还是"没打到钉钉"），
// 但那把 accessToken 本身不能跟着进去：它有效期 7200s，落进日志等于泄露一把可用凭证。
// 空值必须照常带出（批J 的 200-空accessToken 那格靠它判因），所以抹的是"非空凭证值"。
func TestBatchJ_DingTalkAccessTokenNeverReachesParseError(t *testing.T) {
	prevBase := dingtalkOpenAPIBase
	t.Cleanup(func() { dingtalkOpenAPIBase = prevBase })

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"accessToken":"TK-LEAK-J","expireIn":`)) // 被截断 ⇒ JSON 解析失败
	}))
	defer srv.Close()
	dingtalkOpenAPIBase = srv.URL

	_, _, err := FetchDingTalkRobotMedia(context.Background(), "appkey-j", "secret-j", "robot-j", "code-j")
	if err == nil {
		t.Fatal("截断的凭证响应必须报错")
	}
	msg := err.Error()
	if !strings.Contains(msg, "token parse") {
		t.Errorf("错误里缺「token parse」判因，got %q", msg)
	}
	if strings.Contains(msg, "TK-LEAK-J") {
		t.Errorf("accessToken 明文落进错误串（会随日志长期留存）：%q", msg)
	}
}
