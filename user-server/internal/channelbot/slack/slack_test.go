package slack

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"hivemtk-user/internal/channelbot/core"
)

func TestVerifySignatureWindow(t *testing.T) {
	secret := "s3cret"
	body := []byte(`{"type":"event_callback"}`)
	now := time.Unix(1700000000, 0)
	ts := "1700000000"
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte("v0:" + ts + ":" + string(body)))
	sig := "v0=" + hex.EncodeToString(mac.Sum(nil))
	if !VerifySignature(secret, ts, body, sig, now) {
		t.Fatal("正确签名应通过")
	}
	if VerifySignature(secret, ts, body, sig, now.Add(6*time.Minute)) {
		t.Fatal("超过 5 分钟必须拒绝")
	}
	if VerifySignature(secret, ts, []byte("tampered"), sig, now) {
		t.Fatal("body 被篡改必须拒绝")
	}
}

func TestPostMessageThread(t *testing.T) {
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &got)
		_, _ = w.Write([]byte(`{"ok":true,"ts":"111.222"}`))
	}))
	defer srv.Close()
	c := NewClient("xoxb-test", core.WithBaseURL(srv.URL))
	ts, err := c.PostMessage(context.Background(), "C1", "hi", "100.1")
	if err != nil || ts != "111.222" {
		t.Fatalf("PostMessage = %q, %v", ts, err)
	}
	if got["thread_ts"] != "100.1" || got["channel"] != "C1" {
		t.Fatalf("payload = %v", got)
	}
}

func TestEnvelopeBotDetection(t *testing.T) {
	env, err := ParseEnvelope([]byte(`{"type":"event_callback","event":{"type":"message","subtype":"bot_message","bot_id":"B1","text":"x"}}`))
	if err != nil || !env.IsBotMessage() {
		t.Fatalf("bot 消息未识别: %+v %v", env, err)
	}
}
