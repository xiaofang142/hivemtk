package wecombot

import (
	"context"
	"crypto/sha1"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"testing"

	"hivemtk-user/internal/channelbot/core"
)

func testAESKey() string {
	raw := make([]byte, 32)
	for i := range raw {
		raw[i] = byte(i + 1)
	}
	return strings.TrimRight(base64.StdEncoding.EncodeToString(raw), "=")
}

func TestEncryptDecryptRoundTrip(t *testing.T) {
	key := testAESKey()
	plain := []byte(`{"msgtype":"text","msgid":"m1","text":{"content":"你好"}}`)
	enc, err := Encrypt(key, "corp1", plain)
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	got, err := Decrypt(key, enc)
	if err != nil {
		t.Fatalf("Decrypt: %v", err)
	}
	if string(got) != string(plain) {
		t.Fatalf("roundtrip = %s", got)
	}
	m, err := ParseMessage(got)
	if err != nil || m.MsgID != "m1" || m.Text == nil || m.Text.Content != "你好" {
		t.Fatalf("ParseMessage = %+v, %v", m, err)
	}
}

func TestVerifySignature(t *testing.T) {
	token, ts, nonce, encrypt := "tok", "1700000000", "9", "cipher"
	parts := []string{token, ts, nonce, encrypt}
	sort.Strings(parts)
	sum := sha1.Sum([]byte(strings.Join(parts, "")))
	if !VerifySignature(token, hex.EncodeToString(sum[:]), ts, nonce, encrypt) {
		t.Fatal("正确签名应通过")
	}
	if VerifySignature(token, "bad", ts, nonce, encrypt) {
		t.Fatal("错误签名必须拒绝")
	}
}

func TestPushMessageMarkdown(t *testing.T) {
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = decode(r, &body)
		got = body
		_, _ = w.Write([]byte(`{"errcode":0,"errmsg":"ok"}`))
	}))
	defer srv.Close()
	c := NewClient(core.WithBaseURL(srv.URL))
	if err := c.PushMessage(context.Background(), srv.URL+"/push", "markdown", "**hi**"); err != nil {
		t.Fatalf("PushMessage: %v", err)
	}
	if got["msgtype"] != "markdown" {
		t.Fatalf("payload = %v", got)
	}
	if _, ok := got["markdown"]; !ok {
		t.Fatalf("缺 markdown 载荷: %v", got)
	}
}

func decode(r *http.Request, v any) error {
	raw, err := io.ReadAll(r.Body)
	if err != nil {
		return err
	}
	return json.Unmarshal(raw, v)
}
