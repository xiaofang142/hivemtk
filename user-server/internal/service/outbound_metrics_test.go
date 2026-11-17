package service

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"hivemtk-user/internal/model"
)

// specs/024 T1205 验收：出站可观测打点。
// 用例全部走钉钉 sessionWebhook（DB-free）：缺 webhook=window_expired、
// 平台 errcode≠0=裸错误归 unknown、超长正文=溢出计数。
// 指标为进程级累计值，断言一律用「本用例前后差值」，与其他用例（含 golden）互不干扰。

func TestOutboundFailureMetricDimensions(t *testing.T) {
	allowAnyDingtalkHostForTest(t)
	svc := &WebhookService{}
	ctx := context.Background()

	// 样本 A：缺 sessionWebhook → preSendFailure(window_expired)，无 HTTP。
	hubNoWebhook := &model.MessageHub{Platform: "dingtalk", ConversationID: "cid-metric-a", Extra: model.JSONMap{}}
	releaseReplyClaimForTest(t, "evt-metric-a")
	beforeExpired := outboundSendFailedTotal.WithLabel("dingtalk", "window_expired").Value()
	sentA, errA := svc.sendOutbound(ctx, ChannelDingTalk, "7",
		&ParsedPayload{EventID: "evt-metric-a", Sender: "staff", Content: "在吗"},
		"回复", hubNoWebhook, nil)
	afterExpired := outboundSendFailedTotal.WithLabel("dingtalk", "window_expired").Value()
	if sentA || errA == nil {
		t.Fatalf("缺 sessionWebhook 必须失败: sent=%v err=%v", sentA, errA)
	}
	if got := AsChannelError(errA); got == nil || got.Category != CategoryWindowExpired {
		t.Fatalf("样本 A 应为 window_expired，got %+v", errA)
	}
	if afterExpired != beforeExpired+1 {
		t.Errorf("outbound_send_failed_total{dingtalk,window_expired} 差值=%d，期望 1", afterExpired-beforeExpired)
	}

	// 样本 B：平台 errcode≠0 → 裸 fmt.Errorf → 归 unknown（与 A 同渠道不同类别分维）。
	var mu sync.Mutex
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.ReadAll(r.Body)
		mu.Lock()
		calls++
		mu.Unlock()
		_, _ = w.Write([]byte(`{"errcode":310000,"errmsg":"session webhook has expired"}`))
	}))
	defer srv.Close()
	hubErr := &model.MessageHub{
		Platform:       "dingtalk",
		ConversationID: "cid-metric-b",
		Extra: model.JSONMap{
			"session_webhook":            srv.URL,
			"session_webhook_expired_at": time.Now().Add(time.Hour).Unix(),
		},
	}
	releaseReplyClaimForTest(t, "evt-metric-b")
	beforeUnknown := outboundSendFailedTotal.WithLabel("dingtalk", "unknown").Value()
	sentB, errB := svc.sendOutbound(ctx, ChannelDingTalk, "7",
		&ParsedPayload{EventID: "evt-metric-b", Sender: "staff", Content: "在吗"},
		"回复", hubErr, nil)
	afterUnknown := outboundSendFailedTotal.WithLabel("dingtalk", "unknown").Value()
	if sentB || errB == nil {
		t.Fatalf("errcode≠0 必须失败: sent=%v err=%v", sentB, errB)
	}
	mu.Lock()
	defer mu.Unlock()
	if calls != 1 {
		t.Fatalf("样本 B 应真实发起 1 次 HTTP，实际 %d", calls)
	}
	if afterUnknown != beforeUnknown+1 {
		t.Errorf("outbound_send_failed_total{dingtalk,unknown} 差值=%d，期望 1（裸错误必须归 unknown 维度）", afterUnknown-beforeUnknown)
	}
}

func TestRenderOverflowMetric(t *testing.T) {
	allowAnyDingtalkHostForTest(t)
	releaseReplyClaimForTest(t, "evt-metric-overflow")

	var mu sync.Mutex
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.ReadAll(r.Body)
		mu.Lock()
		calls++
		mu.Unlock()
		_, _ = w.Write([]byte(`{"errcode":0,"errmsg":"ok"}`))
	}))
	defer srv.Close()

	// 钉钉 MaxTextLen=20000（channelcaps）：20010 字正文只计数不切分，仍整条投出。
	content := strings.Repeat("好", 20010)
	hub := &model.MessageHub{
		Platform:       "dingtalk",
		ConversationID: "cid-metric-overflow",
		Extra: model.JSONMap{
			"session_webhook":            srv.URL,
			"session_webhook_expired_at": time.Now().Add(time.Hour).Unix(),
		},
	}
	before := outboundTextOverflowTotal.WithLabel("dingtalk").Value()
	sent, sendErr := (&WebhookService{}).sendOutbound(context.Background(), ChannelDingTalk, "7",
		&ParsedPayload{EventID: "evt-metric-overflow", Sender: "staff", Content: "在吗"},
		content, hub, nil)
	after := outboundTextOverflowTotal.WithLabel("dingtalk").Value()

	if !sent || sendErr != nil {
		t.Fatalf("溢出只计数不拦截，必须照常投递: sent=%v err=%v", sent, sendErr)
	}
	mu.Lock()
	defer mu.Unlock()
	if calls != 1 {
		t.Fatalf("溢出样本应真实发起 1 次 HTTP，实际 %d", calls)
	}
	if after != before+1 {
		t.Errorf("outbound_text_overflow_total{dingtalk} 差值=%d，期望 1", after-before)
	}
	if _, err := url.Parse(srv.URL); err != nil {
		t.Fatalf("夹具 URL 非法: %v", err)
	}
}
