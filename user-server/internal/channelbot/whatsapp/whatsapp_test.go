package whatsapp

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"strings"
	"testing"

	"hivemtk-user/internal/channelbot/core"
)

type fakeTransport struct {
	resp *http.Response
	got  *http.Request
}

func (f *fakeTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	f.got = req
	return f.resp, nil
}

func newFakeClient(respBody string, status int) (*CloudClient, *fakeTransport) {
	tr := &fakeTransport{resp: &http.Response{
		StatusCode: status,
		Body:       io.NopCloser(strings.NewReader(respBody)),
		Header:     make(http.Header),
	}}
	c := NewCloudClient("phone-id", "access-token", core.WithHTTPClient(&http.Client{Transport: tr}))
	return c, tr
}

func TestSendText(t *testing.T) {
	c, f := newFakeClient(`{"messages":[{"id":"wamid.abc"}]}`, 200)
	id, err := c.SendText(context.Background(), "5511999", "hi")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if id != "wamid.abc" {
		t.Fatalf("expected wa id wamid.abc, got %s", id)
	}
	if f.got.Method != http.MethodPost {
		t.Fatalf("expected POST, got %s", f.got.Method)
	}
	if !strings.Contains(f.got.URL.Path, "/phone-id/messages") {
		t.Fatalf("unexpected url path: %s", f.got.URL.Path)
	}
}

func TestSendTemplate(t *testing.T) {
	c, f := newFakeClient(`{"messages":[{"id":"wamid.tpl"}]}`, 201)
	id, err := c.SendTemplate(context.Background(), "5511999", "welcome", "zh_CN", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if id != "wamid.tpl" {
		t.Fatalf("expected wamid.tpl, got %s", id)
	}
	if !strings.Contains(f.got.URL.Path, "/phone-id/messages") {
		t.Fatalf("unexpected url path: %s", f.got.URL.Path)
	}
}

func TestVerifyWebhook(t *testing.T) {
	body := []byte("payload")
	mac := hmac.New(sha256.New, []byte("appsecret"))
	mac.Write(body)
	sig := "sha256=" + hex.EncodeToString(mac.Sum(nil))
	if !VerifyWebhook("appsecret", body, sig) {
		t.Fatal("valid HMAC should pass")
	}
	if VerifyWebhook("appsecret", body, "sha256=deadbeef") {
		t.Fatal("invalid HMAC should fail")
	}
	// 空 appSecret 必须拒签。这条断言的方向 2026-10-10 被倒转过：旧实现 return true
	// （fail-open），旧断言钉的是「empty secret should skip (pass)」。fail-open 是
	// 本仓验签链路唯一一处不带开关、不打日志的旁路，任何漏掉前置守卫的调用方都会
	// 静默放行伪造回调，因此把不变式改成 fail-closed。
	if VerifyWebhook("", body, "sha256=anything") {
		t.Fatal("empty secret must fail closed")
	}
}

func TestVerifySubscribe(t *testing.T) {
	if chal, ok := VerifySubscribe("subscribe", "vtoken", "challenge", "vtoken"); !ok || chal != "challenge" {
		t.Fatalf("valid subscribe should return challenge, got %q %v", chal, ok)
	}
	if _, ok := VerifySubscribe("subscribe", "vtoken", "challenge", "wrong"); ok {
		t.Fatal("wrong verify token should fail")
	}
}

func TestParseWebhook_FirstMessage(t *testing.T) {
	body := []byte(`{
		"object":"whatsapp_business_account",
		"entry":[{"id":"WABA","changes":[{"field":"messages","value":{
			"metadata":{"phone_number_id":"P","display_phone_number":"+1"},
			"contacts":[{"profile":{"name":"Bob"},"wa_id":"5511"}],
			"messages":[{"from":"5511","id":"wamid.x","timestamp":"1700000000","type":"text","text":{"body":"hello"}}]
		}}]}]
	}`)
	ev, err := ParseWebhook(body)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	from, msgID, typ, content, name, _, ok := ev.FirstMessage()
	if !ok || from != "5511" || msgID != "wamid.x" || typ != "text" || content != "hello" || name != "Bob" {
		t.Fatalf("unexpected first message: %s %s %s %s %s %v", from, msgID, typ, content, name, ok)
	}
	in := ev.ToInbound("acc1")
	if in.Platform != "whatsapp" || in.ConversationID != "5511" || in.SenderName != "Bob" {
		t.Fatalf("unexpected inbound: %+v", in)
	}
}
