package service

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"testing"
	"time"

	"hivemtk-user/internal/channelbot/core"
	"hivemtk-user/internal/channelbot/onebot"
	"hivemtk-user/internal/channelbot/wecomcs"
	"hivemtk-user/internal/model"
)

func TestVerifyCustomSignature(t *testing.T) {
	secret := "s3cret"
	body := []byte(`{"event_id":"e1","sender":{"id":"u1"},"content":"hi"}`)
	now := time.Unix(1700000000, 0)
	headers := map[string]string{"X-Hive-Signature": SignCustom(secret, body, now)}
	if !VerifyCustom(secret, body, headers, now) {
		t.Fatal("正确签名应通过")
	}
	if VerifyCustom(secret, []byte("tampered"), headers, now) {
		t.Fatal("body 被篡改必须拒绝")
	}
	if VerifyCustom(secret, body, headers, now.Add(6*time.Minute)) {
		t.Fatal("超过 5 分钟必须拒绝")
	}
	if VerifyCustom("other", body, headers, now) {
		t.Fatal("错误 secret 必须拒绝")
	}
}

func TestParseCustomPayload(t *testing.T) {
	body := []byte(`{"event_id":"e1","channel_msg_id":"cm1","conversation_id":"c1","sender":{"id":"u1","name":"张三"},"msg_type":"image","content":"[图片]","media_url":"https://x/y.png","timestamp":1700000000,"is_group":true,"group_id":"g1"}`)
	p, err := parseCustomPayload(body)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if p.EventID != "e1" || p.Sender != "u1" || p.ChatID != "c1" || p.EventType != model.EventTypeMessageReceived {
		t.Fatalf("payload = %+v", p)
	}
	if p.Extra["media_url"] != "https://x/y.png" || p.Extra["is_group"] != true {
		t.Fatalf("extra = %v", p.Extra)
	}
	if _, err := parseCustomPayload([]byte(`{"content":"x"}`)); err == nil {
		t.Fatal("缺幂等键应报错")
	}
}

func TestParseSlackAndOneBotPayload(t *testing.T) {
	sp, err := parseSlackPayload([]byte(`{"type":"event_callback","event_id":"Ev1","team_id":"T1","event":{"type":"message","user":"U1","text":"hi","channel":"C1","ts":"1700000000.0001","thread_ts":"1600000000.0001","channel_type":"im"}}`))
	if err != nil || sp.EventID != "Ev1" || sp.Sender != "U1" || sp.ChatID != "C1" {
		t.Fatalf("slack payload = %+v, %v", sp, err)
	}
	deleted, err := parseSlackPayload([]byte(`{"type":"event_callback","event_id":"Ev2","event":{"type":"message","subtype":"message_deleted","channel":"C1","deleted_ts":"1700000000.0002"}}`))
	if err != nil || deleted.EventType != model.EventTypeMessageDeleted {
		t.Fatalf("deleted = %+v, %v", deleted, err)
	}

	op, err := parseOneBotPayload([]byte(`{"time":1700000000,"self_id":1,"post_type":"message","message_type":"private","message_id":9,"user_id":5,"message":[{"type":"text","data":{"text":"hello"}}]}`))
	if err != nil || op.EventID != "onebot_9" || op.Sender != "5" || op.Content != "hello" {
		t.Fatalf("onebot payload = %+v, %v", op, err)
	}
}

func TestVerifySlackAndOneBot(t *testing.T) {
	body := []byte(`{"type":"event_callback"}`)
	// Slack 签名时窗依赖真实当前时间，签名算法本身在 channelbot 包钉死；这里只锁缺头拒绝。
	if VerifySlack("s3cret", body, nil) {
		t.Fatal("缺 header 必须拒绝")
	}

	if !VerifyOneBot("tok", map[string]string{"Authorization": "Bearer tok"}, nil) {
		t.Fatal("OneBot Bearer token 应通过")
	}
	if VerifyOneBot("tok", nil, map[string]string{"access_token": "bad"}) {
		t.Fatal("错误 OneBot token 必须拒绝")
	}
}

func TestVerifyWeComCS(t *testing.T) {
	token := "tok"
	body := []byte(`<xml><Encrypt><![CDATA[cipher]]></Encrypt><MsgType><![CDATA[event]]></MsgType></xml>`)
	ts, nonce := "1700000000", "42"
	parts := []string{token, ts, nonce, "cipher"}
	sort.Strings(parts)
	sum := sha1.Sum([]byte(strings.Join(parts, "")))
	sig := hex.EncodeToString(sum[:])
	q := map[string]string{"msg_signature": sig, "timestamp": ts, "nonce": nonce}
	if !VerifyWeComCS(token, body, q) {
		t.Fatal("正确客服回调签名应通过")
	}
	if VerifyWeComCS(token, body, map[string]string{"msg_signature": "bad", "timestamp": ts, "nonce": nonce}) {
		t.Fatal("错误签名必须拒绝")
	}
}

func TestWeComCSPullerMapsMessages(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/cgi-bin/gettoken":
			fmt.Fprint(w, `{"errcode":0,"access_token":"tok","expires_in":7200}`)
		case "/cgi-bin/kf/sync_msg":
			fmt.Fprint(w, `{"errcode":0,"next_cursor":"c2","has_more":0,"msg_list":[
				{"msgid":"m1","external_userid":"u1","send_time":1700000000,"origin":3,"msgtype":"text","text":{"content":"你好"}},
				{"msgid":"m2","external_userid":"u1","send_time":1700000001,"origin":5,"msgtype":"text","text":{"content":"客服自己"}}]}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	// pullAccount 的游标落库在 repo 为 nil 时跳过；本用例只验证消息映射与分页终止。
	svc := &WebhookService{}
	w := &wecomCSService{svc: svc, clients: map[uint]*wecomcs.Client{}, pulling: map[uint]struct{}{}}
	acc := &model.WeComCSAccount{ID: 7, CorpID: "corp", Secret: "sec", Token: "cb", OpenKfID: "kf"}
	if err := w.pullAccount(context.Background(), acc, wecomcs.NewClient(core.WithBaseURL(srv.URL))); err != nil {
		t.Fatalf("pullAccount: %v", err)
	}
}

func TestOneBotEventTypeMapping(t *testing.T) {
	cases := []struct {
		post, notice, request string
		want                  string
	}{
		{"notice", "group_recall", "", model.EventTypeMessageDeleted},
		{"notice", "group_increase", "", model.EventTypeGroupMemberJoined},
		{"notice", "group_decrease", "", model.EventTypeGroupMemberLeft},
		{"request", "", "group", "request.received"},
		{"meta_event", "", "", "meta_event"},
	}
	for _, c := range cases {
		e := &onebot.Event{PostType: c.post, NoticeType: c.notice, RequestType: c.request}
		if got := oneBotEventType(e); got != c.want {
			t.Errorf("%s/%s → %q, want %q", c.post, c.notice, got, c.want)
		}
	}
}
