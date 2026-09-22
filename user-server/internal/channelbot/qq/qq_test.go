package qq

import (
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"hivemtk-user/internal/channelbot/core"
	"hivemtk-user/internal/model"
)

// ---------------------------------------------------------------------------
// Ed25519 验签
// ---------------------------------------------------------------------------

func TestVerifySignature_Valid(t *testing.T) {
	secret := "test-bot-secret"
	priv := DerivePrivateKey(secret)
	pub := priv.Public().(ed25519.PublicKey)

	ts := "1728000000"
	msg := ts + `{"op":0,"t":"GROUP_AT_MESSAGE_CREATE"}`
	sig := ed25519.Sign(priv, []byte(msg))
	sigHex := hex.EncodeToString(sig)

	if !VerifySignature(secret, sigHex, ts, []byte(msg[len(ts):])) {
		t.Fatal("expected valid signature to pass")
	}
	_ = pub
}

func TestVerifySignature_Tampered(t *testing.T) {
	secret := "test-bot-secret"
	priv := DerivePrivateKey(secret)
	ts := "1728000000"
	body := []byte(`{"op":0}`)
	sig := hex.EncodeToString(ed25519.Sign(priv, append([]byte(ts), body...)))

	cases := []struct {
		name string
		sig  string
		ts   string
		body []byte
	}{
		{"wrong body", sig, ts, []byte(`{"op":1}`)},
		{"wrong ts", sig, "111", body},
		{"wrong secret (different key)", func() string {
			other := DerivePrivateKey("other-secret")
			return hex.EncodeToString(ed25519.Sign(other, append([]byte(ts), body...)))
		}(), ts, body},
		{"not hex", "zzzz", ts, body},
		{"short sig", "abcd", ts, body},
		{"empty secret", sig, ts, body},
		{"empty sig", "", ts, body},
		{"empty ts", sig, "", body},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			sec := secret
			if c.name == "empty secret" {
				sec = ""
			}
			if VerifySignature(sec, c.sig, c.ts, c.body) {
				t.Errorf("expected %s to fail", c.name)
			}
		})
	}
}

func TestVerifySignature_HighBitsConstraint(t *testing.T) {
	// sig[63]&224 != 0 必须拒绝（官方约束）
	secret := "s"
	ts := "1"
	body := []byte("x")
	priv := DerivePrivateKey(secret)
	sig := ed25519.Sign(priv, []byte(ts+string(body)))
	sig[63] |= 0xE0
	if VerifySignature(secret, hex.EncodeToString(sig), ts, body) {
		t.Fatal("expected high-bits signature to be rejected")
	}
}

func TestDerivePrivateKey_RepeatRule(t *testing.T) {
	// 短 secret repeat 补齐 32 字节
	priv := DerivePrivateKey("ab")
	expected := ed25519.NewKeyFromSeed([]byte("abababababababababababababababab"))
	if !ed2559PrivEqual(priv, expected) {
		t.Fatal("derived seed mismatch for short secret")
	}
	// 长 secret 截断 32
	long := strings.Repeat("xyz", 20) // 60 chars
	priv2 := DerivePrivateKey(long)
	expected2 := ed25519.NewKeyFromSeed([]byte(long[:32]))
	if !ed2559PrivEqual(priv2, expected2) {
		t.Fatal("derived seed mismatch for long secret")
	}
	if DerivePrivateKey("") != nil {
		t.Fatal("empty secret should return nil")
	}
}

func ed2559PrivEqual(a, b ed25519.PrivateKey) bool {
	return hex.EncodeToString(a.Seed()) == hex.EncodeToString(b.Seed())
}

// ---------------------------------------------------------------------------
// Op13 回调地址验证
// ---------------------------------------------------------------------------

func TestGenerateCallbackTestSignature(t *testing.T) {
	secret := "op13-secret"
	sig, err := GenerateCallbackTestSignature(secret, "1728000000", "plain-token-xyz")
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	if len(sig) != 128 { // 64 bytes hex
		t.Fatalf("sig hex length = %d, want 128", len(sig))
	}
	// 可验签回来
	pub := DerivePrivateKey(secret).Public().(ed25519.PublicKey)
	raw, _ := hex.DecodeString(sig)
	if !ed25519.Verify(pub, []byte("1728000000plain-token-xyz"), raw) {
		t.Fatal("signature does not verify against derived public key")
	}
	if _, err := GenerateCallbackTestSignature("", "1", "p"); err == nil {
		t.Fatal("empty secret should error")
	}
}

// ---------------------------------------------------------------------------
// 事件解析 / ToInbound
// ---------------------------------------------------------------------------

const groupEventJSON = `{
  "id": "evt-001",
  "op": 0,
  "t": "GROUP_AT_MESSAGE_CREATE",
  "s": 42,
  "d": {
    "id": "ROBOT1.0_msg123",
    "group_openid": "G0ABC",
    "content": "  你好，介绍下产品",
    "author": {"member_openid": "M1"},
    "timestamp": "2026-09-07T10:00:00+08:00"
  }
}`

func TestParseEvent_Group(t *testing.T) {
	e, err := ParseEvent([]byte(groupEventJSON))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if e.T != EventGroupAtMessage || e.ID != "evt-001" || e.Op != 0 {
		t.Fatalf("event header mismatch: %+v", e)
	}
	in := e.ToInbound("7")
	if in == nil {
		t.Fatal("expected inbound")
	}
	if in.Platform != "qq" || !in.IsGroup || in.GroupID != "G0ABC" {
		t.Errorf("group fields mismatch: %+v", in)
	}
	if in.SenderID != "M1" || in.ConversationID != "G0ABC" {
		t.Errorf("sender/conv mismatch: %+v", in)
	}
	if !strings.Contains(in.Content, "介绍下产品") {
		t.Errorf("content mismatch: %q", in.Content)
	}
	if in.Timestamp == 0 {
		t.Error("timestamp should parse from ISO8601")
	}
}

func TestParseEvent_C2C(t *testing.T) {
	raw := `{"id":"e2","op":0,"t":"C2C_MESSAGE_CREATE","d":{"id":"m2","user_openid":"U1","content":"在吗","timestamp":""}}`
	e, err := ParseEvent([]byte(raw))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	in := e.ToInbound("3")
	if in == nil {
		t.Fatal("expected inbound")
	}
	if in.IsGroup || in.GroupID != "" || in.SenderID != "U1" || in.ConversationID != "U1" {
		t.Errorf("c2c fields mismatch: %+v", in)
	}
}

// TestEventNames_AreOfficialDocNames 事件名逐字对齐官方文档：
//
//	单聊 https://bot.q.qq.com/wiki/develop/api-v2/autogen/event/c2c_message_create.html
//	群@  https://bot.q.qq.com/wiki/develop/api-v2/autogen/event/group_at_message_create.html
//	群全量推送同端点另有 GROUP_MESSAGE_CREATE（与 GROUP_AT_MESSAGE_CREATE 载荷同构）
//
// 曾经写成 C2C_AT_MESSAGE_CREATE —— 官方没有这个事件名，单聊消息会被整批静默丢弃。
func TestEventNames_AreOfficialDocNames(t *testing.T) {
	cases := map[string]string{
		"EventC2CMessage":     EventC2CMessage,
		"EventGroupAtMessage": EventGroupAtMessage,
		"EventGroupMessage":   EventGroupMessage,
	}
	want := map[string]string{
		"EventC2CMessage":     "C2C_MESSAGE_CREATE",
		"EventGroupAtMessage": "GROUP_AT_MESSAGE_CREATE",
		"EventGroupMessage":   "GROUP_MESSAGE_CREATE",
	}
	for k, got := range cases {
		if got != want[k] {
			t.Errorf("%s = %q，官方为 %q", k, got, want[k])
		}
	}
}

// TestToInbound_GroupFullPushMode 群全量推送模式必须与 @机器人 模式同样落进站。
func TestToInbound_GroupFullPushMode(t *testing.T) {
	raw := `{"id":"e5","op":0,"t":"GROUP_MESSAGE_CREATE","d":{"id":"m5","group_openid":"G9","content":"没@机器人也问了","author":{"member_openid":"M9"}}}`
	e, err := ParseEvent([]byte(raw))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	in := e.ToInbound("7")
	if in == nil {
		t.Fatal("GROUP_MESSAGE_CREATE 被忽略：全量推送群的客户消息进不了站")
	}
	if !in.IsGroup || in.GroupID != "G9" || in.SenderID != "M9" {
		t.Errorf("group push mode fields mismatch: %+v", in)
	}
}

func TestToInbound_OtherEventsIgnored(t *testing.T) {
	raws := []string{
		`{"id":"e3","op":0,"t":"DIRECT_MESSAGE_CREATE","d":{}}`,
		`{"id":"e4","op":0,"t":"FRIEND_ADD","d":{}}`,
	}
	for _, raw := range raws {
		e, _ := ParseEvent([]byte(raw))
		if in := e.ToInbound("1"); in != nil {
			t.Errorf("event %s should be ignored", e.T)
		}
	}
}

func TestIngress_EventIDAndChannel(t *testing.T) {
	e, _ := ParseEvent([]byte(groupEventJSON))
	h := &fakeIngressHandler{}
	if err := e.Ingress(context.Background(), h, "9"); err != nil {
		t.Fatalf("ingress: %v", err)
	}
	if h.event == nil {
		t.Fatal("expected event")
	}
	if h.event.EventID != "qq_evt_evt-001" {
		t.Errorf("EventID = %q, want qq_evt_evt-001", h.event.EventID)
	}
	if h.event.Channel != "qq" || h.event.SessionID != "qq:G0ABC" {
		t.Errorf("channel/session mismatch: %s %s", h.event.Channel, h.event.SessionID)
	}
}

func TestIngress_NilHandlerOrNonMessage(t *testing.T) {
	e, _ := ParseEvent([]byte(`{"id":"x","op":0,"t":"FRIEND_ADD","d":{}}`))
	if err := e.Ingress(context.Background(), nil, "1"); err != nil {
		t.Error("nil handler should no-op")
	}
	if err := e.Ingress(context.Background(), &fakeIngressHandler{}, "1"); err != nil {
		t.Error("non-message event should no-op")
	}
}

type fakeIngressHandler struct{ event *model.MessageEvent }

func (f *fakeIngressHandler) HandleIngressMessage(_ context.Context, ev *model.MessageEvent) error {
	f.event = ev
	return nil
}

// ---------------------------------------------------------------------------
// Client：access token + 发送（httptest mock）
// ---------------------------------------------------------------------------

func TestClient_SendMessage_Group(t *testing.T) {
	var gotAuth string
	tokenCalls := 0
	sendCalls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/app/getAppAccessToken"):
			tokenCalls++
			_ = json.NewEncoder(w).Encode(map[string]string{"access_token": "TK-1", "expires_in": "7200"})
		case strings.HasPrefix(r.URL.Path, "/v2/groups/"):
			gotAuth = r.Header.Get("Authorization")
			if gotAuth != "QQBot TK-1" {
				t.Errorf("auth header = %q", gotAuth)
			}
			sendCalls++
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			if body["msg_type"].(float64) != 0 {
				t.Error("msg_type should be 0 for text")
			}
			if body["msg_id"] != "msg123" {
				t.Errorf("msg_id = %v", body["msg_id"])
			}
			if sendCalls == 1 && body["msg_seq"].(float64) != 1 {
				t.Errorf("msg_seq = %v", body["msg_seq"])
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": map[string]any{"msg_id": "sent-1"}})
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
		}
	}))
	defer srv.Close()

	c := NewClient("app-1", "sec-1", core.WithBaseURL(srv.URL))
	msgID, err := c.SendMessage(context.Background(), SendTarget{GroupOpenID: "G1", MsgID: "msg123", MsgSeq: 1}, "你好")
	if err != nil {
		t.Fatalf("send: %v", err)
	}
	if msgID != "sent-1" || tokenCalls != 1 {
		t.Fatalf("msgID=%s tokenCalls=%d", msgID, tokenCalls)
	}

	// 第二次发送命中 token 缓存，不再请求 token
	if _, err := c.SendMessage(context.Background(), SendTarget{GroupOpenID: "G1", MsgID: "msg123", MsgSeq: 2}, "第二条"); err != nil {
		t.Fatalf("send2: %v", err)
	}
	if tokenCalls != 1 {
		t.Errorf("token should be cached, calls=%d", tokenCalls)
	}
}

func TestClient_SendMessage_C2C_LongTextSplits(t *testing.T) {
	sends := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/app/getAppAccessToken") {
			_ = json.NewEncoder(w).Encode(map[string]string{"access_token": "TK", "expires_in": "7200"})
			return
		}
		if strings.HasPrefix(r.URL.Path, "/v2/users/") {
			sends++
			_ = json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": map[string]any{"msg_id": "m"}})
			return
		}
		t.Errorf("unexpected path %s", r.URL.Path)
	}))
	defer srv.Close()

	c := NewClient("a", "b", core.WithBaseURL(srv.URL))
	long := strings.Repeat("字", 2500)
	if _, err := c.SendMessage(context.Background(), SendTarget{UserOpenID: "U1"}, long); err != nil {
		t.Fatalf("send: %v", err)
	}
	if sends < 2 {
		t.Errorf("long text should split, sends=%d", sends)
	}
}

func TestClient_SendMessage_PlatformError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/app/getAppAccessToken") {
			_ = json.NewEncoder(w).Encode(map[string]string{"access_token": "TK", "expires_in": "7200"})
			return
		}
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]any{"code": 11253, "message": "turn off the active message"})
	}))
	defer srv.Close()

	c := NewClient("a", "b", core.WithBaseURL(srv.URL))
	if _, err := c.SendMessage(context.Background(), SendTarget{GroupOpenID: "G1"}, "hi"); err == nil {
		t.Fatal("expected platform error to surface")
	}
}

// TestClient_TokenErrorSurfaces 官方把 QQ 机器人的业务错误写在 **HTTP 200 的响应体**里：
// 《获取 access_token》原文「该接口的业务错误通过响应体的 code 返回，即使调用失败，HTTP 返回码仍为
// 200。请优先依据 code 判断请求是否成功，不要只依赖 HTTP 返回码」，失败返回示例
// `{"code": 100007, "message": "appid invalid"}`，且返回参数表把 code 的类型钉为 **number**
// （https://bot.q.qq.com/wiki/develop/api-v2/dev-prepare/access-token.html，批J 复验 2026-09-20，
// curl 直连 http=200 / 24,598 B / md5 f1acdd7dc218e41635259d0fa7fafea0）。
//
// 原夹具写成 `"code": "100014"`（字符串），与官方类型不符 ⇒ json.Unmarshal 在 int 字段上直接失败，
// 用例其实走的是"响应体解析失败"分支，"200 + 业务错误码"这条真分支从未被执行（假覆盖）。
// 现在按官方形状铺数据，并要求错误文本带上根因码与官方原话。
func TestClient_TokenErrorSurfaces(t *testing.T) {
	var statusSeen int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		statusSeen = http.StatusOK
		_ = json.NewEncoder(w).Encode(map[string]any{"code": 100007, "message": "appid invalid"})
	}))
	defer srv.Close()

	c := NewClient("bad", "bad", core.WithBaseURL(srv.URL))
	_, err := c.GetAccessToken(context.Background())
	if err == nil {
		t.Fatal("200 + 业务错误码必须报错（当成功返回 nil 等于把 AppID 无效伪装成凭证可用）")
	}
	if statusSeen != http.StatusOK {
		t.Fatalf("夹具应回 HTTP 200，实际 %d（官方明确失败也是 200）", statusSeen)
	}
	for _, want := range []string{"100007", "appid invalid"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("错误文本缺根因 %q：%v", want, err)
		}
	}
}

// TestClient_TokenEmptyAtHTTP200 「200 + 空 access_token」这条腿此前无任何用例：
// 三条断言各自钉住一个失效面。
//  1. 报根因：错误文本含官方 code 与 message（只报"empty"会让运维看不出是 AppID 错还是限流）；
//  2. 空值不进缓存：失败后 tokenExpAt/accessToken 必须保持零值，且第二次调用**重新发请求**
//     （若把空串当有效值缓存，整个进程生命周期内的出站都会带着 "QQBot " 空凭证打平台，
//     且再也不去换新凭证 —— 与抖音批G-2c 同一格缺陷）；
//  3. 不打后一条腿：SendMessage 必须在取凭证处就失败，一条 /v2/groups/ 请求都不发出去。
func TestClient_TokenEmptyAtHTTP200(t *testing.T) {
	tokenCalls, sendCalls := 0, 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/app/getAppAccessToken"):
			tokenCalls++
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(map[string]any{"code": 100016, "message": "invalid appid or secret"})
		case strings.HasPrefix(r.URL.Path, "/v2/groups/"):
			sendCalls++
			_ = json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": map[string]any{"msg_id": "x"}})
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
		}
	}))
	defer srv.Close()

	c := NewClient("bad", "bad", core.WithBaseURL(srv.URL))
	_, err := c.GetAccessToken(context.Background())
	if err == nil {
		t.Fatal("空 access_token 必须报错")
	}
	for _, want := range []string{"100016", "invalid appid or secret"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("错误文本缺根因 %q：%v", want, err)
		}
	}

	// 2) 空值不进缓存
	if c.accessToken != "" || !c.tokenExpAt.IsZero() {
		t.Errorf("失败后缓存被写入：accessToken=%q tokenExpAt=%v", c.accessToken, c.tokenExpAt)
	}
	if _, err := c.GetAccessToken(context.Background()); err == nil {
		t.Error("第二次调用仍应失败（第一次的空值被判成了有效凭证）")
	}
	if tokenCalls != 2 {
		t.Errorf("tokenCalls = %d, want 2（一次失败不该省掉下一次的取凭证）", tokenCalls)
	}

	// 3) 取不到凭证就不该碰发送腿
	if _, err := c.SendMessage(context.Background(), SendTarget{GroupOpenID: "G1"}, "hi"); err == nil {
		t.Error("凭证获取失败时 SendMessage 应返回错误")
	}
	if sendCalls != 0 {
		t.Errorf("sendCalls = %d, want 0（空 token 被拿去发 QQBot <空> 了）", sendCalls)
	}
}

func TestSplitQQMessage(t *testing.T) {
	if got := splitQQMessage("", 10); len(got) != 0 {
		t.Errorf("empty text: %v", got)
	}
	if got := splitQQMessage("短文本", 10); len(got) != 1 {
		t.Errorf("short text: %v", got)
	}
	long := strings.Repeat("a", 35) + "\n" + strings.Repeat("b", 35)
	got := splitQQMessage(long, 40)
	if len(got) != 2 || !strings.HasSuffix(got[0], "a") {
		t.Errorf("newline split expected: %q", got)
	}
}

// ---------------------------------------------------------------------------
// M-01（批F-4c）：入站富媒体 attachments[] 承载 + 被动回复 msg_id 口径
// 官方事件表：bot.q.qq.com/wiki/develop/api-v2/autogen/event/group_message_create.html
// 官方发送表：msg_id「从 GROUP_AT_MESSAGE_CREATE 等事件的 d.id 获取，5 分钟内有效」
// ---------------------------------------------------------------------------

// qqEvent 拼一条群 @ 事件（d 为事件体 JSON）。
func qqEvent(d string) *Event {
	e, err := ParseEvent([]byte(`{"id":"evt-1","op":0,"t":"GROUP_AT_MESSAGE_CREATE","d":` + d + `}`))
	if err != nil {
		panic(err)
	}
	return e
}

func TestToInbound_AttachmentsDeriveTypeAndContent(t *testing.T) {
	cases := []struct {
		name    string
		d       string
		wantTyp string
		wantCtt string
		wantN   int
	}{
		{
			name:    "纯文本不受影响",
			d:       `{"id":"m1","group_openid":"G1","content":"多少钱","author":{"member_openid":"M1"}}`,
			wantTyp: "text", wantCtt: "多少钱",
		},
		{
			// 官方纯图片消息 content 为空串——类型只能由 attachments 决定。
			name:    "纯图片",
			d:       `{"id":"m2","group_openid":"G1","content":"","author":{"member_openid":"M1"},"attachments":[{"url":"http://gchat.qpic.cn/a.png","filename":"a.png","content_type":"image/png","width":10,"height":8,"size":100}]}`,
			wantTyp: "image", wantCtt: "[图片]", wantN: 1,
		},
		{
			name:    "文本带图",
			d:       `{"id":"m3","group_openid":"G1","content":"看下这个","author":{"member_openid":"M1"},"attachments":[{"url":"http://gchat.qpic.cn/b.jpg","content_type":"image/jpeg"}]}`,
			wantTyp: "image", wantCtt: "看下这个[图片]", wantN: 1,
		},
		{
			// 语音正文取官方 ASR 结果：那段话就是客户真正说的，只留占位符等于漏答。
			name:    "语音带ASR",
			d:       `{"id":"m4","group_openid":"G1","content":"","author":{"member_openid":"M1"},"attachments":[{"url":"http://gchat.qpic.cn/silk","content_type":"voice","voice_wav_url":"http://gchat.qpic.cn/wav","asr_refer_text":"想问下报价"}]}`,
			wantTyp: "audio", wantCtt: "想问下报价", wantN: 1,
		},
		{
			name:    "语音无ASR",
			d:       `{"id":"m5","group_openid":"G1","content":"","author":{"member_openid":"M1"},"attachments":[{"url":"http://gchat.qpic.cn/silk","content_type":"voice"}]}`,
			wantTyp: "audio", wantCtt: "[语音]", wantN: 1,
		},
		{
			name:    "视频",
			d:       `{"id":"m6","group_openid":"G1","content":"","author":{"member_openid":"M1"},"attachments":[{"url":"http://gchat.qpic.cn/v.mp4","content_type":"video/mp4"}]}`,
			wantTyp: "video", wantCtt: "[视频]", wantN: 1,
		},
		{
			name:    "文件带原名",
			d:       `{"id":"m7","group_openid":"G1","content":"","author":{"member_openid":"M1"},"attachments":[{"url":"http://gchat.qpic.cn/f","filename":"报价单.pdf","content_type":"file"}]}`,
			wantTyp: "file", wantCtt: "[文件] 报价单.pdf", wantN: 1,
		},
		{
			// 官方新增/缺失 content_type 时按 URL 后缀兜底（去掉 ?query），未知归 file。
			name:    "content_type缺失按后缀兜底",
			d:       `{"id":"m8","group_openid":"G1","content":"","author":{"member_openid":"M1"},"attachments":[{"url":"http://gchat.qpic.cn/photo.gif?w=1","filename":"photo.gif"}]}`,
			wantTyp: "image", wantCtt: "[图片]", wantN: 1,
		},
		{
			name:    "未知类型按文件",
			d:       `{"id":"m9","group_openid":"G1","content":"","author":{"member_openid":"M1"},"attachments":[{"url":"http://gchat.qpic.cn/x.bin","content_type":"application/whatever","filename":"x.bin"}]}`,
			wantTyp: "file", wantCtt: "[文件] x.bin", wantN: 1,
		},
		{
			// 一条消息两张图：类型取首个、占位符按顺序全拼（只留首张是 N-10 在 WA 修过的错）。
			name:    "两张图全覆盖",
			d:       `{"id":"m10","group_openid":"G1","content":"对比下","author":{"member_openid":"M1"},"attachments":[{"url":"http://gchat.qpic.cn/1.png","content_type":"image/png"},{"url":"http://gchat.qpic.cn/2.png","content_type":"image/png"}]}`,
			wantTyp: "image", wantCtt: "对比下[图片][图片]", wantN: 2,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := qqEvent(tc.d)
			in := e.ToInbound("7")
			if in == nil {
				t.Fatal("消息事件被忽略")
			}
			if in.MsgType != tc.wantTyp {
				t.Errorf("MsgType = %q, want %q", in.MsgType, tc.wantTyp)
			}
			if in.Content != tc.wantCtt {
				t.Errorf("Content = %q, want %q", in.Content, tc.wantCtt)
			}
			if n := len(e.Attachments()); tc.wantN != n {
				t.Errorf("Attachments() 条数 = %d, want %d", n, tc.wantN)
			}
		})
	}
}

// TestOfficialMsgID_IsRawDID 被动回复关联 ID 必须是官方 d.id 原值：
// 内部命名空间前缀 qq_ 一旦进出站报文，平台按「msg_id 有误」拒掉，每条 AI 回复都发不出去。
func TestOfficialMsgID_IsRawDID(t *testing.T) {
	e := qqEvent(`{"id":"ROBOT1.0_msg123","group_openid":"G1","content":"hi","author":{"member_openid":"M1"}}`)
	if got := e.OfficialMsgID(); got != "ROBOT1.0_msg123" {
		t.Errorf("OfficialMsgID = %q, want 官方 d.id 原值（不带 qq_ 前缀）", got)
	}
	h := &fakeIngressHandler{}
	if err := e.Ingress(context.Background(), h, "9"); err != nil {
		t.Fatalf("ingress: %v", err)
	}
	if got, _ := h.event.Extra["channel_msg_id"].(string); got != "ROBOT1.0_msg123" {
		t.Errorf("落库 Extra.channel_msg_id = %q, want %q（出站被动回复按它取 msg_id）",
			got, "ROBOT1.0_msg123")
	}
	if h.event.EventID != "qq_evt_evt-1" {
		t.Errorf("EventID = %q, want qq_evt_evt-1（幂等键仍用事件级 id）", h.event.EventID)
	}
}

// TestHubMsgID_KeyPrecedence 媒体回填按 HubMsgID 找 hub 行，该键必须与 Ingress 落库一致：
// 事件级 id 优先（官方重投不变），缺事件 id 才退回消息 id，两者都没有时留空
// （返回恒定 "qq_" 会把所有无 id 消息塌成一行，第二条起全被幂等丢弃）。
func TestHubMsgID_KeyPrecedence(t *testing.T) {
	if got := qqEvent(`{"id":"m-1","group_openid":"G1","content":"a"}`).HubMsgID(); got != "qq_evt_evt-1" {
		t.Errorf("带事件 id 时 HubMsgID = %q, want qq_evt_evt-1", got)
	}
	noEvt, _ := ParseEvent([]byte(`{"op":0,"t":"GROUP_AT_MESSAGE_CREATE","d":{"id":"m-2","group_openid":"G1","content":"a"}}`))
	if got := noEvt.HubMsgID(); got != "qq_m-2" {
		t.Errorf("无事件 id 时 HubMsgID = %q, want qq_m-2", got)
	}
	both, _ := ParseEvent([]byte(`{"op":0,"t":"GROUP_AT_MESSAGE_CREATE","d":{"group_openid":"G1","content":"a"}}`))
	if got := both.HubMsgID(); got != "" {
		t.Errorf("事件 id 与 d.id 都缺失时 HubMsgID = %q, want 空串（交给中台补）", got)
	}
	if in := both.ToInbound("7"); in == nil || in.MessageID != "" {
		t.Errorf("无 d.id 时 MessageID 必须留空，got %+v", in)
	}
	// Ingress 的 EventID 与 HubMsgID 必须同源，否则媒体回填找不到行。
	h := &fakeIngressHandler{}
	if err := noEvt.Ingress(context.Background(), h, "7"); err != nil {
		t.Fatalf("ingress: %v", err)
	}
	if h.event.EventID != noEvt.HubMsgID() {
		t.Errorf("Ingress EventID = %q 与 HubMsgID = %q 不同源", h.event.EventID, noEvt.HubMsgID())
	}
}

// TestToInbound_C2CNoGroupFields 单聊事件不能带上群字段（工作台按 GroupID 判会话结构）。
func TestToInbound_C2CNoGroupFields(t *testing.T) {
	e, _ := ParseEvent([]byte(`{"id":"evt-2","op":0,"t":"C2C_MESSAGE_CREATE","d":{"id":"ROBOT1.0_c2c","user_openid":"C9","content":"在吗","attachments":[{"url":"http://gchat.qpic.cn/a.png","content_type":"image/png"}]}}`))
	in := e.ToInbound("7")
	if in == nil {
		t.Fatal("C2C_MESSAGE_CREATE 被忽略")
	}
	if in.IsGroup || in.GroupID != "" {
		t.Errorf("单聊不应带群字段: is_group=%v group_id=%q", in.IsGroup, in.GroupID)
	}
	if in.ConversationID != "C9" || in.SenderID != "C9" {
		t.Errorf("单聊会话/发送者 = %q/%q", in.ConversationID, in.SenderID)
	}
	if in.MsgType != "image" {
		t.Errorf("单聊附件类型 = %q, want image", in.MsgType)
	}
}

// TestEvent_AttachmentFieldNamesAreOfficial 官方 attachments[] 字段名逐字校验：
// 拼错一个字母 json.Unmarshal 不报错、只是静默得到零值，媒体会以「无附件」的姿态被丢掉。
func TestEvent_AttachmentFieldNamesAreOfficial(t *testing.T) {
	e := qqEvent(`{"id":"m11","group_openid":"G1","content":"","author":{"member_openid":"M1"},"attachments":[{
		"url":"http://gchat.qpic.cn/x.png","filename":"x.png","width":123,"height":456,"size":789,
		"content_type":"image/png","voice_wav_url":"http://gchat.qpic.cn/w.wav","asr_refer_text":"识别文本"}]}`)
	atts := e.Attachments()
	if len(atts) != 1 {
		t.Fatalf("附件条数 = %d, want 1", len(atts))
	}
	a := atts[0]
	if a.URL == "" || a.Filename != "x.png" || a.Width != 123 || a.Height != 456 || a.Size != 789 {
		t.Errorf("基础字段解析不齐: %+v", a)
	}
	if a.ContentType != "image/png" || a.VoiceWavURL == "" || a.AsrReferText != "识别文本" {
		t.Errorf("媒体字段解析不齐: %+v", a)
	}
}
