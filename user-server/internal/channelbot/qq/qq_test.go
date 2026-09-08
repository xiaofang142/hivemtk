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
	raw := `{"id":"e2","op":0,"t":"C2C_AT_MESSAGE_CREATE","d":{"id":"m2","user_openid":"U1","content":"在吗","timestamp":""}}`
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

func TestClient_TokenErrorSurfaces(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]string{"code": "100014", "message": "invalid appid"})
	}))
	defer srv.Close()

	c := NewClient("bad", "bad", core.WithBaseURL(srv.URL))
	if _, err := c.GetAccessToken(context.Background()); err == nil {
		t.Fatal("expected token error")
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
