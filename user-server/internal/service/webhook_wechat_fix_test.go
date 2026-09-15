package service

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"hivemtk-user/internal/cache"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/testutil"
)

func newFakeWechatAPIServer(t *testing.T) (*httptest.Server, func() []map[string]any) {
	t.Helper()
	var mu sync.Mutex
	var sent []map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/cgi-bin/token":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"access_token":"TEST_TOKEN","expires_in":7200}`))
		case "/cgi-bin/message/custom/send":
			body, _ := io.ReadAll(r.Body)
			var m map[string]any
			_ = json.Unmarshal(body, &m)
			mu.Lock()
			sent = append(sent, m)
			mu.Unlock()
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"errcode":0,"errmsg":"ok"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv, func() []map[string]any {
		mu.Lock()
		defer mu.Unlock()
		out := make([]map[string]any, len(sent))
		copy(out, sent)
		return out
	}
}

// TestSendOutbound_WechatChannel_DeliversCustomerServiceMessage 验证修复 A：
// channel="wechat" 的 AI 回复必须经微信客服消息 API 真实发出，且记录出站消息。
func TestSendOutbound_WechatChannel_DeliversCustomerServiceMessage(t *testing.T) {
	db := testutil.NewTestDBOrSkip(t, &model.MessageHub{}, &model.WechatAccount{}, &model.WechatMessage{})
	srv, getSent := newFakeWechatAPIServer(t)

	origBase := wechatAPIBase
	wechatAPIBase = srv.URL
	t.Cleanup(func() { wechatAPIBase = origBase })

	const (
		accountID = "3"
		openID    = "openid-fix-a"
		conv      = "wechat:3:" + openID
	)
	// -count=N 隔离：释放上一轮固定 EventID 的出站认领
	releaseReplyClaimForTest(t, "evt-wx-fix-a")
	acc := &model.WechatAccount{
		ID:        3,
		AppID:     "wx-test-appid",
		AppSecret: "wx-test-secret",
		Token:     "tok",
		Status:    "active",
	}
	if err := db.Create(acc).Error; err != nil {
		t.Fatalf("create wechat account failed: %v", err)
	}

	hub := &model.MessageHub{
		MsgID:          "1000000000000001",
		Platform:       "wechat",
		AccountID:      accountID,
		Direction:      "inbound",
		MsgType:        "text",
		SenderID:       openID,
		Content:        "你好",
		ConversationID: conv,
		SentAt:         time.Now(),
		Extra:          model.JSONMap{"account_id": accountID},
	}
	if err := db.Create(hub).Error; err != nil {
		t.Fatalf("pre-create inbound failed: %v", err)
	}

	svc := NewWebhookService(db)
	defer svc.Stop(context.Background())

	p := &ParsedPayload{EventID: "evt-wx-fix-a", Sender: openID, Content: "你好"}
	replyText := "您好，我是 AI 客服"

	svc.sendOutbound(context.Background(), ChannelWechat, accountID, p, replyText, hub, nil)

	sentMsgs := getSent()
	if len(sentMsgs) == 0 {
		t.Fatal("修复目标未达成：channel=wechat 的出站仍被丢弃，客服消息 API 未被调用")
	}
	first := sentMsgs[0]
	if first["touser"] != openID {
		t.Errorf("touser expected %q, got %v", openID, first["touser"])
	}
	if first["msgtype"] != "text" {
		t.Errorf("msgtype expected text, got %v", first["msgtype"])
	}
	textMap, _ := first["text"].(map[string]any)
	if textMap == nil || textMap["content"] != replyText {
		t.Errorf("text.content expected %q, got %v", replyText, first["text"])
	}

	var outMsg model.WechatMessage
	if err := db.Where("is_outgoing = ?", true).Order("id DESC").First(&outMsg).Error; err != nil {
		t.Fatalf("outbound wechat_message 未落库: %v", err)
	}
	if outMsg.ToUser != openID || outMsg.Content != replyText {
		t.Errorf("出站记录字段不符: to=%q content=%q", outMsg.ToUser, outMsg.Content)
	}
}

// TestRunAIGeneration_ReleasesAILock_OnErrorExit 验证修复 B（错误退出路径）：
// 编排器持续返回错误时，runAIGeneration 退出后必须释放 AI 处理锁，
// 否则会话被静音 2 分钟、后续消息全部不回复。
func TestRunAIGeneration_ReleasesAILock_OnErrorExit(t *testing.T) {
	ctx := context.Background()
	_mc1 := cache.NewMemoryCache()
	defer _mc1.Close()
	ingress := NewInboxIngressServiceWithDB(nil, _mc1)

	svc := &WebhookService{
		replySem:          make(chan struct{}, 4),
		ingressSvc:        ingress,
		smartOrchestrator: nil,
	}

	const conv = "conv-wx-lock-err"
	key := InboxAIProcessingKey + conv
	ok, err := ingress.cache.SetNX(ctx, key, "1", time.Minute)
	if err != nil || !ok {
		t.Fatalf("preset AI lock failed: ok=%v err=%v", ok, err)
	}

	hubMsg := &model.MessageHub{
		MsgID:          "evt-wx-lock-err",
		Platform:       "wechat",
		ConversationID: conv,
		Direction:      "inbound",
		SentAt:         time.Now(),
	}
	p := &ParsedPayload{EventID: "evt-wx-lock-err", Sender: "openid-x", Content: "在吗"}
	in := &IncomingContext{Content: "在吗"}

	done := make(chan struct{})
	go func() {
		defer close(done)
		svc.runAIGeneration(ctx, ChannelWechat, "3", p, hubMsg, in, nil)
	}()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("runAIGeneration 未在预期时间内返回")
	}

	v, err := ingress.cache.Get(ctx, key)
	if err == nil && v == "1" {
		t.Fatal("修复目标未达成：错误退出后 AI 处理锁未释放（会话将被静音 2 分钟）")
	}
}

// TestRunAIGeneration_ReleasesAILock_OnSemaphoreTimeout 验证修复 B（sem 超时路径）：
// replySem 满导致任务跳过时同样必须释放锁。
func TestRunAIGeneration_ReleasesAILock_OnSemaphoreTimeout(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	_mc2 := cache.NewMemoryCache()
	defer _mc2.Close()
	ingress := NewInboxIngressServiceWithDB(nil, _mc2)

	sem := make(chan struct{}, 1)
	sem <- struct{}{}
	svc := &WebhookService{
		replySem:   sem,
		ingressSvc: ingress,
	}

	const conv = "conv-wx-lock-sem"
	key := InboxAIProcessingKey + conv
	ok, err := ingress.cache.SetNX(context.Background(), key, "1", time.Minute)
	if err != nil || !ok {
		t.Fatalf("preset AI lock failed: ok=%v err=%v", ok, err)
	}

	hubMsg := &model.MessageHub{
		MsgID:          "evt-wx-lock-sem",
		Platform:       "wechat",
		ConversationID: conv,
		Direction:      "inbound",
		SentAt:         time.Now(),
	}
	p := &ParsedPayload{EventID: "evt-wx-lock-sem", Sender: "openid-x", Content: "在吗"}
	in := &IncomingContext{Content: "在吗"}

	svc.runAIGeneration(ctx, ChannelWechat, "3", p, hubMsg, in, nil)

	v, err := ingress.cache.Get(context.Background(), key)
	if err == nil && v == "1" {
		t.Fatal("修复目标未达成：sem 超时跳过后 AI 处理锁未释放")
	}
}

// TestInboxIngress_WechatChannel_TriggersAI 验证修复 C：
// wechat 渠道入站事件必须能通过去重/钩子3 全链路并触发 AI（渠道连通性回归护栏）。
func TestInboxIngress_WechatChannel_TriggersAI(t *testing.T) {
	db := testutil.NewTestDBOrSkip(t, &model.MessageHub{})
	_mc3 := cache.NewMemoryCache()
	defer _mc3.Close()
	ingress := NewInboxIngressServiceWithDB(db, _mc3)
	tr := &fakeAITrigger{}
	ingress.SetAITrigger(tr)

	ev := &model.MessageEvent{
		EventID:        "1000000000000009",
		Channel:        "wechat",
		ConversationID: "wechat:3:openid-fix-c",
		SessionID:      "wechat:3:openid-fix-c",
		SenderType:     model.SenderTypeCustomer,
		SenderID:       "openid-fix-c",
		MsgType:        model.MsgTypeText,
		Content:        "你们的价格是多少",
		Timestamp:      time.Now(),
		Extra:          map[string]any{"account_id": "3"},
	}
	res, err := ingress.HandleIngressMessage(context.Background(), ev)
	if err != nil {
		t.Fatalf("HandleIngressMessage error: %v", err)
	}
	if !res.Accepted {
		t.Fatalf("消息应被接受: %+v", res)
	}
	if !res.QueuedForAI {
		t.Fatalf("wechat 入站应触发 AI: reason=%q", res.Reason)
	}
	if tr.called != 1 {
		t.Fatalf("期望 AI 触发 1 次，实际 %d", tr.called)
	}
	if tr.lastChannel != "wechat" || tr.lastAccount != "3" {
		t.Errorf("触发参数不符: channel=%q account=%q", tr.lastChannel, tr.lastAccount)
	}
}
