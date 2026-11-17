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
	"hivemtk-user/internal/pkg/testutil"
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
	// DB-backed：delayedRepo 必须真实在位，「不入重试队」才有牙——
	// 负对照（非重放同款失败）证明同款失败会入队 1 行，重放版必须 0 行。
	database := testutil.NewTestDB(t, &model.MessageHub{}, &model.DelayedOutboundReply{})
	svc := NewWebhookService()
	t.Cleanup(func() { svc.Stop(context.Background()) })

	releaseReplyClaimForTest(t, "evt-t110-replay-neg")
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

	countRetries := func() int64 {
		var n int64
		if err := database.Model(&DelayedOutboundReply{}).Where("kind = ?", model.DelayedKindSendRetry).Count(&n).Error; err != nil {
			t.Fatalf("数重试行失败: %v", err)
		}
		return n
	}

	hub := dingtalkRetryHub(t, srv.URL)
	// 负对照：非重放路径的同一款可重试失败 → 入队 1 行（证明断言有牙）。
	sent0, err0 := svc.sendOutbound(context.Background(), ChannelDingTalk, "107",
		&ParsedPayload{EventID: "evt-t110-replay-neg", Sender: "staff", Content: "在吗"},
		"首发正文", hub, nil)
	if sent0 || err0 == nil {
		t.Fatalf("负对照必须失败: sent=%v err=%v", sent0, err0)
	}
	if n := countRetries(); n != 1 {
		t.Fatalf("负对照应入重试队 1 行，实际 %d（断言无牙，夹具失效）", n)
	}

	// 正题：重放路径同款失败 → 不再入队（收尾 defer 的 isDelayedReplay 分支跳过）。
	releaseReplyClaimForTest(t, "evt-t110-replay")
	sent, sendErr := svc.sendOutbound(DelayedReplayToContext(context.Background()), ChannelDingTalk, "107",
		&ParsedPayload{EventID: "evt-t110-replay", Sender: "staff", Content: "在吗"},
		"重放正文", hub, nil)
	mu.Lock()
	defer mu.Unlock()
	if sent || sendErr == nil {
		t.Fatalf("重放路径的平台失败必须原样失败: sent=%v err=%v", sent, sendErr)
	}
	if calls != 2 { // 负对照 1 次 + 重放 1 次（内部退避重试归客户端）
		t.Fatalf("HTTP 总次数应 2（负对照+重放各 1），实际 %d", calls)
	}
	if n := countRetries(); n != 1 {
		t.Fatalf("重放失败不得再入重试队列：重试行应仍为 1（只有负对照那条），实际 %d", n)
	}
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
