package wecomcs

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"sync/atomic"
	"testing"

	"hivemtk-user/internal/channelbot/core"
	"hivemtk-user/internal/model"
)

func TestClientTokenCacheAndSyncCursor(t *testing.T) {
	var tokenCalls int32
	var gotCursor atomic.Value
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/cgi-bin/gettoken":
			atomic.AddInt32(&tokenCalls, 1)
			fmt.Fprint(w, `{"errcode":0,"access_token":"tok-1","expires_in":7200}`)
		case "/cgi-bin/kf/sync_msg":
			body := make([]byte, r.ContentLength)
			_, _ = r.Body.Read(body)
			gotCursor.Store(string(body))
			fmt.Fprint(w, `{"errcode":0,"next_cursor":"c2","has_more":0,"msg_list":[{"msgid":"m1","open_kfid":"kf1","external_userid":"u1","send_time":1700000000,"origin":3,"msgtype":"text","text":{"content":"你好"}}]}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	c := NewClient(core.WithBaseURL(srv.URL))
	for i := 0; i < 2; i++ {
		if _, err := c.GetToken(context.Background(), "corp", "secret"); err != nil {
			t.Fatalf("GetToken: %v", err)
		}
	}
	if tokenCalls != 1 {
		t.Fatalf("token 未缓存，调用 %d 次", tokenCalls)
	}
	resp, err := c.SyncMsg(context.Background(), "tok-1", SyncMsgReq{Cursor: "c1", Token: "cb", OpenKfID: "kf1"})
	if err != nil {
		t.Fatalf("SyncMsg: %v", err)
	}
	if resp.NextCursor != "c2" || len(resp.MsgList) != 1 {
		t.Fatalf("resp = %+v", resp)
	}
	if !strings.Contains(gotCursor.Load().(string), `"cursor":"c1"`) {
		t.Fatalf("游标未透传: %s", gotCursor.Load())
	}
	in := resp.MsgList[0].ToInbound("7")
	if in == nil || in.Content != "你好" || in.SenderID != "u1" || in.MessageID != "m1" {
		t.Fatalf("ToInbound = %+v", in)
	}
	if resp.MsgList[0].EventID() != "wecomcs_m1" {
		t.Fatalf("EventID = %q", resp.MsgList[0].EventID())
	}
}

func TestSyncMsgWindowError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"errcode":95021,"errmsg":"session expired"}`)
	}))
	defer srv.Close()
	c := NewClient(core.WithBaseURL(srv.URL))
	_, err := c.SendMsg(context.Background(), "tok", &SendMsgReq{Touser: "u", MsgType: "text"})
	var ce *Error
	if !asError(err, &ce) || !ce.IsWindowExpired() {
		t.Fatalf("期望窗口过期错误, got %v", err)
	}
}

func TestToInboundSkipsNonCustomerAndMapsTypes(t *testing.T) {
	if (&SyncMsg{Origin: 5, ExternalUserID: "u"}).ToInbound("a") != nil {
		t.Fatal("origin=5（客服自己）不应入站")
	}
	cases := map[string]string{
		"image": model.MsgTypeImage, "voice": model.MsgTypeAudio,
		"video": model.MsgTypeVideo, "file": model.MsgTypeFile,
		"link": model.MsgTypeLink, "miniprogram": model.MsgTypeCard,
	}
	for platform, want := range cases {
		m := &SyncMsg{Origin: 3, ExternalUserID: "u", MsgType: platform}
		if got := m.ToInbound("a"); got == nil || got.MsgType != want {
			t.Errorf("%s → %v, want %s", platform, got, want)
		}
	}
}

func TestVerifySignature(t *testing.T) {
	token, ts, nonce, encrypt := "tok", "1700000000", "42", "cipher"
	parts := []string{token, ts, nonce, encrypt}
	sort.Strings(parts)
	sum := sha1.Sum([]byte(strings.Join(parts, "")))
	sig := hex.EncodeToString(sum[:])
	if !VerifySignature(token, sig, ts, nonce, encrypt) {
		t.Fatal("正确签名应通过")
	}
	if VerifySignature(token, sig, ts, nonce, "other") {
		t.Fatal("被篡改的 encrypt 必须拒绝")
	}
}

func asError(err error, target **Error) bool {
	e, ok := err.(*Error)
	if ok {
		*target = e
	}
	return ok
}
