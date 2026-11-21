package service

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"testing"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/httpclient"
	"hivemtk-user/internal/pkg/testutil"
)

// specs/024 T112 行为冻结对照测试。
//
// 口径：8 个出站渠道（7 个 webhook 直发 + 桥接族 douyin 代表）× 3 组样例，
// 逐样例捕获「平台可见的 (method, path, body) 调用序列 + 投递事实（sent / 失败类别）」，
// 与 testdata/outbound_golden.json 逐字段比对；差异必须人工确认后 UPDATE_GOLDEN=1 重写。
// 平台假服务器只回固定应答，所有请求体内容全部来自用例常量——同样的输入永远得到同样的序列。
//
// 平台端点经 goldenRouter 劫持（httpclient.Client 与 http.DefaultClient 换 Transport），
// 渠道凭据行种在测试库——真实 HTTP 拼装/鉴权/重试/失败分类全部走生产代码，冻结才冻结得住。

type goldenCall struct {
	Method string         `json:"method"`
	Path   string         `json:"path"`
	Body   map[string]any `json:"body,omitempty"`
}

type goldenCase struct {
	Channel       string       `json:"channel"`
	Sample        string       `json:"sample"`
	Calls         []goldenCall `json:"calls"`
	Sent          bool         `json:"sent"`
	FailCategory  string       `json:"fail_category,omitempty"`
	FailRetryable *bool        `json:"fail_retryable,omitempty"`
}

// goldenRouter 按域名劫持平台 HTTP：请求改写到按域名注册的假服务器。
type goldenRouter struct {
	mu     sync.Mutex
	routes map[string]http.Handler
	prevHC http.RoundTripper
	prevDT http.RoundTripper
}

func newGoldenRouter(t *testing.T) *goldenRouter {
	t.Helper()
	r := &goldenRouter{routes: map[string]http.Handler{}}
	r.prevHC = httpclient.Client.Transport
	r.prevDT = http.DefaultClient.Transport
	httpclient.Client.Transport = r
	http.DefaultClient.Transport = r
	t.Cleanup(func() {
		httpclient.Client.Transport = r.prevHC
		http.DefaultClient.Transport = r.prevDT
	})
	return r
}

// serve 注册 host → 假平台 handler（同 host 重复调用即替换，子测试顺序使用）。
// 请求在 Transport 层就地应答（不落网络）：URL/方法/请求体与生产完全一致，
// 只有字节不再出门——金样冻结的就是这层拼装结果。
func (r *goldenRouter) serve(t *testing.T, host string, h http.Handler) {
	t.Helper()
	r.mu.Lock()
	r.routes[host] = h
	r.mu.Unlock()
}

func (r *goldenRouter) RoundTrip(req *http.Request) (*http.Response, error) {
	r.mu.Lock()
	h := r.routes[req.URL.Host]
	r.mu.Unlock()
	if h == nil {
		return http.DefaultTransport.RoundTrip(req)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req.Clone(context.Background()))
	return w.Result(), nil
}

// goldenRecorder 平台假服务器通用处理：按路径取固定应答，顺手记录调用序列。
type goldenRecorder struct {
	mu       sync.Mutex
	calls    *[]goldenCall
	handlers map[string]func(r *http.Request) (int, string)
}

func (g *goldenRecorder) handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		var b []byte
		if req.Body != nil { // GET（如 token 请求）无 body，直接 ReadAll(nil) 会 panic
			b, _ = io.ReadAll(req.Body)
		}
		var body map[string]any
		if len(b) > 0 {
			_ = json.Unmarshal(b, &body)
		}
		path := req.URL.Path
		if req.URL.RawQuery != "" {
			path += "?" + req.URL.RawQuery
		}
		g.mu.Lock()
		*g.calls = append(*g.calls, goldenCall{Method: req.Method, Path: path, Body: body})
		g.mu.Unlock()
		h, ok := g.handlers[path]
		if !ok {
			h, ok = g.handlers[req.URL.Path] // 带查询串的请求按裸路径回退（注册键不带 query）
		}
		if !ok {
			// 未登记路径：探测类请求（如 token）默认成功应答，让主链路走通。
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{}`))
			return
		}
		code, out := h(req)
		w.WriteHeader(code)
		_, _ = w.Write([]byte(out))
	})
}

func goldenBool(b bool) *bool { return &b }

func goldenFailCategory(t *testing.T, sendErr error) (string, *bool) {
	t.Helper()
	if sendErr == nil {
		return "", nil
	}
	ce := AsChannelError(sendErr)
	if ce == nil {
		return "non_channel_error", goldenBool(false)
	}
	return string(ce.Category), goldenBool(ce.Retryable)
}

func goldenLoad(t *testing.T) []goldenCase {
	t.Helper()
	raw, err := os.ReadFile("testdata/outbound_golden.json")
	if err != nil {
		t.Fatalf("读取 golden 失败（先 UPDATE_GOLDEN=1 生成）: %v", err)
	}
	var cases []goldenCase
	if err := json.Unmarshal(raw, &cases); err != nil {
		t.Fatalf("golden JSON 解析失败: %v", err)
	}
	if len(cases) < 24 {
		t.Fatalf("golden 覆盖不足（%d 条 < 8 渠道 × 3 样例），冻结面残缺", len(cases))
	}
	return cases
}

func goldenSave(t *testing.T, cases []goldenCase) {
	t.Helper()
	out, err := json.MarshalIndent(cases, "", "  ")
	if err != nil {
		t.Fatalf("golden 序列化失败: %v", err)
	}
	if err := os.WriteFile("testdata/outbound_golden.json", append(out, '\n'), 0o644); err != nil {
		t.Fatalf("golden 写回失败: %v", err)
	}
}

func TestOutboundBehaviorFreezeGolden(t *testing.T) {
	resetQuiet := DisableAIQuietHoursForTest(true)
	t.Cleanup(resetQuiet)

	database := testutil.NewTestDB(t,
		&model.MessageHub{}, &model.InboxConversation{}, &model.DelayedOutboundReply{},
		&model.WebhookEvent{}, &model.IntegrationAccount{}, &model.UnifiedMessage{},
		&model.CustomerSession{}, &model.SessionMessage{}, &model.AISuggestion{},
		&model.AgentStatus{}, &model.Customer{}, &model.MessageTrace{},
		&model.FeishuAccount{}, &model.FeishuMessage{}, &model.TelegramAccount{},
		&model.WechatAccount{}, &model.WeComAccount{}, &model.QQAccount{},
		&model.WhatsAppCloudAccount{},
	)

	// 渠道凭据行（固定主键，golden 里的账号 id 全部由此而来）。
	seed := []any{
		&model.FeishuAccount{ID: 101, TenantID: 1, AccountName: "golden-feishu", AppID: "cli_golden", AppSecret: "sec_golden", Status: 1},
		&model.TelegramAccount{ID: 102, TenantID: 1, AccountName: "golden-tg", BotToken: "tok-golden-tg", Status: 1},
		&model.WechatAccount{ID: 103, TenantID: 1, AppID: "wx_golden", AppSecret: "wx-sec-golden", Status: "active"},
		&model.WeComAccount{ID: 104, TenantID: 1, CorpID: "corp-golden", CorpSecret: "corp-sec-golden", Status: 1},
		&model.QQAccount{ID: 105, TenantID: 1, AppID: "qq-golden", AppSecret: "qq-sec-golden"},
		&model.WhatsAppCloudAccount{ID: 106, TenantID: 1, AccountName: "golden-wa", PhoneNumberID: "phone-golden", AccessToken: "wa-tok-golden", Status: 1},
	}
	for _, s := range seed {
		if err := database.Create(s).Error; err != nil {
			t.Fatalf("seed 渠道账号失败: %v", err)
		}
	}

	router := newGoldenRouter(t)
	allowAnyDingtalkHostForTest(t)

	newCalls := func() *[]goldenCall {
		c := make([]goldenCall, 0, 4)
		return &c
	}
	// fake(host, handlers)：为子测试起假平台并路由（host 域名=生产代码里写死的平台域名）。
	fake := func(t *testing.T, host string, calls *[]goldenCall, handlers map[string]func(*http.Request) (int, string)) {
		t.Helper()
		router.serve(t, host, (&goldenRecorder{calls: calls, handlers: handlers}).handler())
	}

	cases := goldenCasesTable()
	run := make([]goldenCase, 0, len(cases))
	for _, tc := range cases {
		t.Run(tc.Channel+"/"+tc.Sample, func(t *testing.T) {
			// 每样例独立 service：隔离 wechat tokenClients / qq msgSeq 等内存缓存，
			// 保证「单独 -run 任一样例」与全量跑产出同一金样序列（R1 顺序依赖修复）。
			svc := NewWebhookService()
			t.Cleanup(func() { svc.Stop(context.Background()) })
			calls := newCalls()
			ctx := context.Background()

			// ── 各渠道假平台按样例装配 ──────────────────────────────
			switch tc.Channel {
			case "dingtalk":
				if tc.Sample != "missing_webhook" { // 该样例不发生任何 HTTP
					fake(t, "goldendt.example.com", calls, map[string]func(*http.Request) (int, string){
						"/goldendt": func(*http.Request) (int, string) {
							if tc.Sample == "platform_err" {
								return 200, `{"errcode":310000,"errmsg":"session webhook has expired"}`
							}
							return 200, `{"errcode":0,"errmsg":"ok"}`
						},
					})
				}
			case "feishu":
				// 清 DB 行 token 缓存：feishu token 命中 acc.AccessToken+TokenExpires，
				// 不清则 api_err 样例漏记 token 调用（顺序依赖）。wecom 同理。
				if err := database.Model(&model.FeishuAccount{}).Where("id = ?", 101).
					Updates(map[string]any{"access_token": "", "token_expires": nil}).Error; err != nil {
					t.Fatalf("清 feishu token 缓存失败: %v", err)
				}
				msgBody := func(*http.Request) (int, string) {
					if tc.Sample == "api_err" {
						return 200, `{"code":230013,"msg":"target not in contact range"}`
					}
					return 200, `{"code":0,"data":{"message_id":"om_golden_feishu"}}`
				}
				fake(t, "open.feishu.cn", calls, map[string]func(*http.Request) (int, string){
					"/open-apis/auth/v3/tenant_access_token/internal": func(*http.Request) (int, string) {
						return 200, `{"code":0,"tenant_access_token":"t-golden","expire":7200}`
					},
					"/open-apis/im/v1/messages": msgBody,
				})
			case "telegram":
				fake(t, "api.telegram.org", calls, map[string]func(*http.Request) (int, string){
					"/bottok-golden-tg/sendMessage": func(*http.Request) (int, string) {
						if tc.Sample == "api_err" {
							// 真实 telegram 的 429 是 HTTP 429 + ok:false（retry_after 在 body）；
							// 客户端按 429 退避重试 tgSendMaxRetries 次后放弃。
							return 429, `{"ok":false,"error_code":429,"description":"Too Many Requests: retry after 1","parameters":{"retry_after":1}}`
						}
						return 200, `{"ok":true,"result":{"message_id":77001}}`
					},
				})
			case "qq":
				send := func(*http.Request) (int, string) {
					if tc.Sample == "api_err" {
						// QQ v2 错误回非 2xx（HTTP 200 只给成功）；客户端按状态码判败。
						return 400, `{"code":11244,"message":"invalid access token"}`
					}
					return 200, `{"code":0,"data":{"msg_id":"qq-golden-out"}}`
				}
				fake(t, "api.bot.qq.com", calls, map[string]func(*http.Request) (int, string){
					"/app/getAppAccessToken": func(*http.Request) (int, string) {
						return 200, `{"code":0,"message":"ok","access_token":"qq-token-golden","expires_in":7200}`
					},
					"/v2/users/cs-golden-qq/messages":  send,
					"/v2/groups/cs-golden-qq/messages": send,
				})
			case "wechat":
				fake(t, "api.weixin.qq.com", calls, map[string]func(*http.Request) (int, string){
					"/cgi-bin/token": func(*http.Request) (int, string) {
						return 200, `{"access_token":"wx-token-golden","expires_in":7200}`
					},
					"/cgi-bin/message/custom/send": func(*http.Request) (int, string) {
						if tc.Sample == "api_err" {
							return 200, `{"errcode":45009,"errmsg":"reach daily api quota limit"}`
						}
						return 200, `{"errcode":0,"errmsg":"ok"}`
					},
				})
			case "wecom":
				if tc.Sample == "quota_exhausted" {
					// SelectHealthyAccount 按健康度选号（不看 req.AccountID），
					// 「账号不存在」测不出失败门；把唯一账号配额清零触发真实配额闸。
					if err := database.Model(&model.WeComAccount{}).Where("id = ?", 104).
						Update("daily_msg_quota", 0).Error; err != nil {
						t.Fatalf("清零企微配额失败: %v", err)
					}
				} else {
					// 清 DB 行 token 缓存（GetAccessToken 命中 acc.AccessToken+TokenExpires 就不 fetch），
					// 不清则 api_err 样例漏记 gettoken 调用（顺序依赖，R1 修复）。
					if err := database.Model(&model.WeComAccount{}).Where("id = ?", 104).
						Updates(map[string]any{"access_token": "", "token_expires": time.Time{}}).Error; err != nil {
						t.Fatalf("清 wecom token 缓存失败: %v", err)
					}
					t.Setenv("WECOM_ALLOW_OUTBOUND", "1")   // IS_TEST_MODE=1 时企微出站被哨兵跳过
					t.Setenv("WECOM_DISABLE_OUTBOUND", "0") // main_test.go 默认置 1，金样要钉真实 HTTP 形状
					fake(t, "qyapi.weixin.qq.com", calls, map[string]func(*http.Request) (int, string){
						"/cgi-bin/gettoken": func(*http.Request) (int, string) {
							return 200, `{"errcode":0,"access_token":"wecom-token-golden","expires_in":7200}`
						},
						"/cgi-bin/externalcontact/message/send": func(*http.Request) (int, string) {
							if tc.Sample == "api_err" {
								return 200, `{"errcode":81013,"errmsg":"invalid external userid"}`
							}
							return 200, `{"errcode":0,"errmsg":"ok","msgid":"wecom-golden-msg"}`
						},
					})
				}
			case "whatsapp":
				if tc.Sample != "account_missing" {
					fake(t, "graph.facebook.com", calls, map[string]func(*http.Request) (int, string){
						"/v21.0/phone-golden/messages": func(*http.Request) (int, string) {
							if tc.Sample == "api_err" {
								return 400, `{"error":{"code":131047,"message":"Re-engagement message required"}}`
							}
							return 200, `{"messages":[{"id":"wamid.golden001"}]}`
						},
					})
				}
			case "bridge":
				// 桥接族出站不产生平台 HTTP：行为体现在 message_hub 行状态。
			}

			// ── 执行 ───────────────────────────────────────────────
			var sent bool
			var sendErr error
			switch tc.Channel {
			case "dingtalk":
				hub := &model.MessageHub{Platform: "dingtalk", ConversationID: "cs-golden-dt", Extra: model.JSONMap{}}
				if tc.Sample != "missing_webhook" {
					hub.Extra["session_webhook"] = "https://goldendt.example.com/goldendt"
					hub.Extra["session_webhook_expired_at"] = time.Now().Add(time.Hour).Unix()
				}
				releaseReplyClaimForTest(t, "evt-golden-dt-"+tc.Sample)
				sent, sendErr = svc.sendOutbound(ctx, ChannelDingTalk, "107",
					&ParsedPayload{EventID: "evt-golden-dt-" + tc.Sample, Sender: "staff-golden", Content: "在吗"},
					"golden 您好，这是冻结样例回复", hub, nil)
			case "feishu":
				var cards []model.RichCard
				if tc.Sample == "ok_card" {
					cards = []model.RichCard{{
						Title: "报价单", Description: "golden 卡片正文",
						Fields:  map[string]string{"总额": "¥12,000"},
						Buttons: []model.CardButton{{Text: "查看详情", URL: "https://example.com/golden"}},
					}}
				}
				releaseReplyClaimForTest(t, "evt-golden-fs-"+tc.Sample)
				sent, sendErr = svc.sendOutbound(ctx, ChannelFeishu, "101",
					&ParsedPayload{EventID: "evt-golden-fs-" + tc.Sample, Sender: "ou_golden", Content: "在吗"},
					"golden 飞书回复正文", nil, cards)
			case "telegram":
				var tgCards []model.RichCard
				if tc.Sample == "ok_card" {
					tgCards = []model.RichCard{{
						Title: "工单进度", Description: "golden telegram 卡片",
						Buttons: []model.CardButton{{Text: "查看详情", URL: "https://example.com/tg"}},
					}}
				}
				hub := &model.MessageHub{Platform: "telegram", ConversationID: "555001", SenderID: "555001"}
				releaseReplyClaimForTest(t, "evt-golden-tg-"+tc.Sample)
				sent, sendErr = svc.sendOutbound(ctx, ChannelTelegram, "102",
					&ParsedPayload{EventID: "evt-golden-tg-" + tc.Sample, Sender: "555001", Content: "在吗"},
					"golden telegram 回复正文", hub, tgCards)
			case "qq":
				// 生产口径：QQ 入站把官方消息 id 写进 hub.Extra["channel_msg_id"]
				//（channelbot/qq qq.go 入站覆盖），出站 QQOutboundMsgID 从这里取被动
				// 回复关联的原消息 id。夹具必须同款——只设 MsgID 时出站拿到空 msg_id
				// 会走主动消息分支（seq 随机），golden 永远对不上（2026-11-19 收口：
				// ed8eeb76 引入 Extra 口径时本夹具没跟上，golden 一直红）。
				// msgID 每样例唯一：msg_seq 按 msg_id 独立从 1 起，样例间不递增。
				hub := &model.MessageHub{
					Platform: "qq", ConversationID: "cs-golden-qq", SenderID: "cust-golden-qq",
					MsgID: "qq-in-golden-" + tc.Sample,
					Extra: model.JSONMap{"channel_msg_id": "qq-in-golden-" + tc.Sample},
				}
				if tc.Sample == "missing_conv" {
					hub = nil
				}
				releaseReplyClaimForTest(t, "evt-golden-qq-"+tc.Sample)
				sent, sendErr = svc.sendOutbound(ctx, ChannelQQ, "105",
					&ParsedPayload{EventID: "evt-golden-qq-" + tc.Sample, Sender: "cust-golden-qq", Content: "在吗"},
					"golden QQ 回复正文", hub, nil)
			case "wechat":
				var cards []model.RichCard
				if tc.Sample == "ok_card_folded" {
					cards = []model.RichCard{{
						Title: "工单进度", Description: "golden 折叠卡片",
						Buttons: []model.CardButton{{Text: "详情", URL: "https://example.com/wx"}},
					}}
				}
				releaseReplyClaimForTest(t, "evt-golden-wx-"+tc.Sample)
				sent, sendErr = svc.sendOutbound(ctx, ChannelWechat, "103",
					&ParsedPayload{EventID: "evt-golden-wx-" + tc.Sample, Sender: "openid-golden", Content: "在吗"},
					"golden 公众号回复正文", nil, cards)
			case "wecom":
				releaseReplyClaimForTest(t, "evt-golden-wecom-"+tc.Sample)
				sent, sendErr = svc.sendOutbound(ctx, ChannelWeCom, "104",
					&ParsedPayload{EventID: "evt-golden-wecom-" + tc.Sample, Sender: "wo-golden", Content: "在吗"},
					"golden 企微回复正文", nil, nil)
			case "whatsapp":
				accountID := "106"
				if tc.Sample == "account_missing" {
					accountID = "999999"
				}
				hub := &model.MessageHub{Platform: "whatsapp", ConversationID: "cs-golden-wa"}
				releaseReplyClaimForTest(t, "evt-golden-wa-"+tc.Sample)
				sent, sendErr = svc.sendOutbound(ctx, ChannelWhatsapp, accountID,
					&ParsedPayload{EventID: "evt-golden-wa-" + tc.Sample, Sender: "8613800000000", Content: "在吗"},
					"golden whatsapp 回复正文", hub, nil)
			case "bridge":
				accountID := "acct-golden-q"
				if tc.Sample == "undeliverable" {
					accountID = "acct-golden-unknown"
				}
				hub := bridgeInboundHub("douyin", accountID, "cs-golden-bridge-"+tc.Sample, "cust-golden-bridge")
				if err := database.Create(hub).Error; err != nil {
					t.Fatalf("seed inbound hub: %v", err)
				}
				content := "golden 桥接回复正文"
				if tc.Sample == "duplicate_occupied" {
					// 预置同键出站行（同会话同文案已 delivered）→ CreateMessageHubIdempotent 占位冲突。
					dup := &model.MessageHub{
						MsgID:    ContentHashMsgID("douyin", hub.ConversationID, content),
						Platform: "douyin", AccountID: accountID, Direction: "outbound",
						Status: "delivered", MsgType: "text", SenderID: accountID,
						ReceiverID: hub.SenderID, Content: content,
						ConversationID: hub.ConversationID, IsAIReply: true, SentAt: time.Now(),
					}
					if err := database.Create(dup).Error; err != nil {
						t.Fatalf("seed duplicate outbound: %v", err)
					}
				}
				releaseReplyClaimForTest(t, "evt-golden-bridge-"+tc.Sample)
				useLegacyBridgeFixture(t)
				bridgeCtx := localBridgeSubmitContext(t, ctx, "douyin", accountID, "text")
				sent, sendErr = svc.sendOutbound(bridgeCtx, ChannelDouyin, accountID,
					&ParsedPayload{EventID: "evt-golden-bridge-" + tc.Sample, Sender: hub.SenderID, Content: "在吗", ChatID: hub.ConversationID},
					content, hub, nil)
			default:
				t.Fatalf("未知渠道样例 %s/%s", tc.Channel, tc.Sample)
			}

			// ── 捕获并与 golden 比对 ────────────────────────────────
			got := goldenCase{
				Channel: tc.Channel,
				Sample:  tc.Sample,
				Calls:   *calls,
				Sent:    sent,
			}
			got.FailCategory, got.FailRetryable = goldenFailCategory(t, sendErr)
			if sent && sendErr != nil {
				t.Fatalf("sent=true 与 sendErr=%v 并存——失败口径泄漏", sendErr)
			}
			run = append(run, got)

			if tc.Sample != got.Sample || tc.Channel != got.Channel {
				t.Fatalf("样例标签错位: want %s/%s got %s/%s", tc.Channel, tc.Sample, got.Channel, got.Sample)
			}
		})
	}

	if os.Getenv("UPDATE_GOLDEN") == "1" {
		if len(run) != len(cases) {
			t.Fatalf("UPDATE_GOLDEN=1 但样例集不全（%d/%d）：-run 过滤下重写会截断冻结面，必须全量子测试下重写",
				len(run), len(cases))
		}
		goldenSave(t, run)
		t.Logf("UPDATE_GOLDEN=1：已重写 testdata/outbound_golden.json（%d 条）。差异必须经人工确认后才能使用。", len(run))
		return
	}

	// 按标签比对（非按序全量对齐）：-run 过滤下只跑子集也能对子集做冻结校验
	//（R1 自包含 + 子集可比，二者合起来才叫「单样例可独立验收」）。
	want := goldenLoad(t)
	wantByLabel := make(map[string]goldenCase, len(want))
	for _, w := range want {
		wantByLabel[w.Channel+"/"+w.Sample] = w
	}
	for _, g := range run {
		w, ok := wantByLabel[g.Channel+"/"+g.Sample]
		if !ok {
			t.Fatalf("golden 里没有 %s/%s：样例表与 golden 漂移，人工确认后重写", g.Channel, g.Sample)
			continue
		}
		if w.Sent != g.Sent {
			t.Errorf("%s/%s: sent want %v got %v（fail=%v）", w.Channel, w.Sample, w.Sent, g.Sent, g.FailCategory)
			continue
		}
		if w.FailCategory != g.FailCategory {
			t.Errorf("%s/%s: fail_category want %q got %q", w.Channel, w.Sample, w.FailCategory, g.FailCategory)
		}
		if (w.FailRetryable == nil) != (g.FailRetryable == nil) ||
			(w.FailRetryable != nil && g.FailRetryable != nil && *w.FailRetryable != *g.FailRetryable) {
			t.Errorf("%s/%s: fail_retryable want %v got %v", w.Channel, w.Sample, w.FailRetryable, g.FailRetryable)
		}
		if len(w.Calls) != len(g.Calls) {
			t.Errorf("%s/%s: 调用序列长度 want %d got %d\n  want=%+v\n  got =%+v", w.Channel, w.Sample, len(w.Calls), len(g.Calls), w.Calls, g.Calls)
			continue
		}
		for j := range w.Calls {
			wc, gc := w.Calls[j], g.Calls[j]
			if wc.Method != gc.Method || wc.Path != gc.Path {
				t.Errorf("%s/%s call[%d]: want %s %s got %s %s", w.Channel, w.Sample, j, wc.Method, wc.Path, gc.Method, gc.Path)
				continue
			}
			if !goldenBodyEqual(wc.Body, gc.Body) {
				t.Errorf("%s/%s call[%d] %s %s 请求体漂移:\n  want=%s\n  got =%s", w.Channel, w.Sample, j, wc.Method, wc.Path, goldenJSON(wc.Body), goldenJSON(gc.Body))
			}
		}
	}
}

func goldenJSON(v map[string]any) string {
	if v == nil {
		return "(no body)"
	}
	b, _ := json.Marshal(v)
	return string(b)
}

// goldenBodyEqual 请求体逐键比对（键集与取值都必须相等；map 递归）。
func goldenBodyEqual(want, got map[string]any) bool {
	if want == nil && got == nil {
		return true
	}
	if len(want) != len(got) {
		return false
	}
	for k, wv := range want {
		gv, ok := got[k]
		if !ok {
			return false
		}
		switch w := wv.(type) {
		case map[string]any:
			gm, ok := gv.(map[string]any)
			if !ok || !goldenBodyEqual(w, gm) {
				return false
			}
		case []any:
			ga, ok := gv.([]any)
			if !ok || len(w) != len(ga) {
				return false
			}
			for i := range w {
				wm, wok := w[i].(map[string]any)
				gm, gok := ga[i].(map[string]any)
				if wok && gok {
					if !goldenBodyEqual(wm, gm) {
						return false
					}
				} else if goldenJSON(map[string]any{"v": w[i]}) != goldenJSON(map[string]any{"v": ga[i]}) {
					return false
				}
			}
		default:
			if goldenJSON(map[string]any{"v": wv}) != goldenJSON(map[string]any{"v": gv}) {
				return false
			}
		}
	}
	return true
}

// goldenCasesTable 8 渠道 × 3 样例 = 24 条冻结样例（顺序即契约，比对按序对齐）。
func goldenCasesTable() []goldenCase {
	return []goldenCase{
		{Channel: "dingtalk", Sample: "ok_text"},
		{Channel: "dingtalk", Sample: "missing_webhook"},
		{Channel: "dingtalk", Sample: "platform_err"},
		{Channel: "feishu", Sample: "ok_text"},
		{Channel: "feishu", Sample: "api_err"},
		{Channel: "feishu", Sample: "ok_card"},
		{Channel: "telegram", Sample: "ok_text"},
		{Channel: "telegram", Sample: "api_err"},
		{Channel: "telegram", Sample: "ok_card"},
		{Channel: "qq", Sample: "ok_text"},
		{Channel: "qq", Sample: "missing_conv"},
		{Channel: "qq", Sample: "api_err"},
		{Channel: "wechat", Sample: "ok_text"},
		{Channel: "wechat", Sample: "api_err"},
		{Channel: "wechat", Sample: "ok_card_folded"},
		{Channel: "wecom", Sample: "ok_text"},
		{Channel: "wecom", Sample: "api_err"},
		{Channel: "wecom", Sample: "quota_exhausted"},
		{Channel: "whatsapp", Sample: "ok_text"},
		{Channel: "whatsapp", Sample: "api_err"},
		{Channel: "whatsapp", Sample: "account_missing"},
		{Channel: "bridge", Sample: "queued"},
		{Channel: "bridge", Sample: "duplicate_occupied"},
		{Channel: "bridge", Sample: "undeliverable"},
	}
}
