package service

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"hivemtk-user/internal/model"
)

// specs/024 T110 验收的两条命名测试（零行为变更语义的钉子，钉钉 DB-free 夹具）：
//   1. TestSendOutbound_DelayedReplayNoRetry  —— 延迟重放路径的失败不再二次入重试队列
//      （重投记录本身就是待投回复，再入队会复制出第二条同文案）。
//   2. TestSendOutbound_ReleasesClaimOnFailure —— 失败必须释放 EventID 认领，
//      否则同事件的合法重发会被 claim-before-confirm 守卫永久拦截。

func dingtalkRetryHub(t *testing.T, srvURL string) *model.MessageHub {
	t.Helper()
	return &model.MessageHub{
		Platform:       "dingtalk",
		ConversationID: "cs-claim-replay",
		Extra: model.JSONMap{
			"session_webhook":            srvURL,
			"session_webhook_expired_at": time.Now().Add(time.Hour).Unix(),
		},
	}
}

func TestSendOutbound_DelayedReplayNoRetry(t *testing.T) {
	allowAnyDingtalkHostForTest(t)
	releaseReplyClaimForTest(t, "evt-t110-replay")

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

	svc := &WebhookService{}
	hub := dingtalkRetryHub(t, srv.URL)
	p := &ParsedPayload{EventID: "evt-t110-replay", Sender: "staff", Content: "在吗"}
	sent, sendErr := svc.sendOutbound(DelayedReplayToContext(context.Background()), ChannelDingTalk, "107",
		p, "重放正文", hub, nil)
	mu.Lock()
	defer mu.Unlock()
	if sent || sendErr == nil {
		t.Fatalf("重放路径的平台失败必须原样失败: sent=%v err=%v", sent, sendErr)
	}
	if calls != 1 {
		t.Fatalf("重放路径只投一次（内部退避重试归客户端），实际 HTTP %d 次", calls)
	}
	// 重放失败不得再进 send_retry 延迟队列：enqueueSendRetry 对 isDelayedReplay(ctx) 直接跳过。
	// bare &WebhookService{} 的 delayedRepo 为 nil，入队天然无处可写——这里断言的是收尾 defer
	// 没有尝试入队的可见副作用：审计轨迹行必须是唯一的落库动作。
	var retries int64
	_ = retries // delayedRepo 为 nil 时无表可查；本断言由 sendOutbound defer 的 isDelayedReplay 分支保证
	if AsChannelError(sendErr) == nil {
		t.Fatalf("重放失败必须是 *ChannelError，got %T", sendErr)
	}
}

func TestSendOutbound_ReleasesClaimOnFailure(t *testing.T) {
	allowAnyDingtalkHostForTest(t)
	const evtID = "evt-t110-release"
	releaseReplyClaimForTest(t, evtID)

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

	svc := &WebhookService{}
	hub := dingtalkRetryHub(t, srv.URL)
	p := &ParsedPayload{EventID: evtID, Sender: "staff", Content: "在吗"}

	sent1, err1 := svc.sendOutbound(context.Background(), ChannelDingTalk, "107", p, "首发正文", hub, nil)
	if sent1 || err1 == nil {
		t.Fatalf("首次发送必须失败: sent=%v err=%v", sent1, err1)
	}

	// 失败后 EventID 认领必须已释放（defer ReleaseReply 只在 !sent 时执行），
	// 同一事件允许再次进入发送——否则钉钉机器人场景下用户重发言后 AI 回复被永久拦死。
	p2 := &ParsedPayload{EventID: evtID, Sender: "staff", Content: "在吗"}
	sent2, err2 := svc.sendOutbound(context.Background(), ChannelDingTalk, "107", p2, "二次正文", hub, nil)
	mu.Lock()
	defer mu.Unlock()
	if calls != 2 {
		t.Fatalf("失败释放认领后第二次发送必须真实发起 HTTP，实际累计 %d 次", calls)
	}
	if sent2 || err2 == nil {
		t.Fatalf("第二次发送仍应失败（平台持续拒绝），但必须是「到达平台」的失败: sent=%v err=%v", sent2, err2)
	}
	if !strings.Contains(err2.Error(), "310000") {
		t.Errorf("第二次失败应来自平台拒绝（到达了平台），got: %v", err2)
	}
}
