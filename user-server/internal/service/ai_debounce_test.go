package service

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"hivemtk-user/internal/cache"
	"hivemtk-user/internal/model"
)

type stubAITrigger struct {
	mu    sync.Mutex
	calls []stubAICall
}

type stubAICall struct {
	conversationID string
	content        string
	eventID        string
}

func (s *stubAITrigger) TriggerInboundAI(ctx context.Context, channel, accountID, conversationID, customerID, content, eventID string, opts ...TriggerInboundOption) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls = append(s.calls, stubAICall{conversationID: conversationID, content: content, eventID: eventID})
}

func (s *stubAITrigger) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.calls)
}

func (s *stubAITrigger) last() stubAICall {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.calls) == 0 {
		return stubAICall{}
	}
	return s.calls[len(s.calls)-1]
}

// debounceFixtureSeq 单调递增的 fixture 序号。
// sessionID 不能取 time.Now().UnixNano():Windows 墙钟粒度粗(亚毫秒级),
// 全量套件中相邻的快速测试极易取到相同时间戳,进而得到相同 conversationID。
var debounceFixtureSeq atomic.Int64

func newDebounceFixture(t *testing.T, seconds int) (*InboxIngressService, *stubAITrigger, string) {
	t.Helper()
	// 每个 fixture 使用独立内存缓存,而非 cache.GetGlobalCache() 的进程级单例:
	// triggerAIForEvent 触发前会对 hivemtk:ai_processing:<conv> 做 SetNX(TTL 2 分钟),
	// 防抖链路自身不释放该排他锁(生产中由 AI 完成回调释放)。若与其他测试共享
	// 全局缓存,前序测试遗留的排他锁会静默跳过本次触发,导致偶发失败。
	memCache := cache.NewMemoryCache()
	t.Cleanup(memCache.Close)
	svc := NewInboxIngressServiceWithDB(nil, memCache)
	stub := &stubAITrigger{}
	svc.aiTrigger = stub
	seq := debounceFixtureSeq.Add(1)
	return svc, stub, fmt.Sprintf("debounce-test-%s-%d-%d", t.Name(), seq, seconds)
}

func debounceEvent(sessionID, content string, seconds int) *model.MessageEvent {
	conv := sessionID + "-conv"
	return &model.MessageEvent{
		EventID:        sessionID + "-" + fmt.Sprint(time.Now().UnixNano()),
		SessionID:      sessionID,
		Channel:        "xiaohongshu",
		SenderID:       conv,
		Content:        content,
		ConversationID: conv,
		Extra: map[string]any{
			"account_id":          "acc-1",
			"ai_debounce_seconds": seconds,
		},
	}
}

func TestAIDebounce_CoalesceWindow(t *testing.T) {
	svc, stub, session := newDebounceFixture(t, 1)
	ctx := context.Background()

	svc.triggerAIWithDebounce(ctx, debounceEvent(session, "在吗", 1))
	svc.triggerAIWithDebounce(ctx, debounceEvent(session, "有个问题", 1))
	svc.triggerAIWithDebounce(ctx, debounceEvent(session, "就是那个订单", 1))

	if got := stub.count(); got != 0 {
		t.Fatalf("窗口内不应触发 AI, got %d calls", got)
	}

	deadline := time.Now().Add(4 * time.Second)
	for stub.count() == 0 && time.Now().Before(deadline) {
		time.Sleep(100 * time.Millisecond)
	}
	if stub.count() != 1 {
		t.Fatalf("窗口关闭后应恰好触发 1 次 AI, got %d", stub.count())
	}
	last := stub.last()
	for _, want := range []string{"在吗", "有个问题", "就是那个订单"} {
		if !strings.Contains(last.content, want) {
			t.Fatalf("合并内容应包含 %q, got %q", want, last.content)
		}
	}

	if strings.Index(last.content, "在吗") > strings.Index(last.content, "就是那个订单") {
		t.Fatalf("合并内容应为时间序, got %q", last.content)
	}
}

func TestAIDebounce_TransferKeywordExempt(t *testing.T) {
	svc, stub, session := newDebounceFixture(t, 3)
	svc.triggerAIWithDebounce(context.Background(), debounceEvent(session, "我要转人工客服", 3))
	if stub.count() != 1 {
		t.Fatalf("转人工关键词应立即触发 1 次, got %d", stub.count())
	}
	if stub.last().content != "我要转人工客服" {
		t.Fatalf("豁免路径内容不应被合并改写, got %q", stub.last().content)
	}
}

func TestAIDebounce_MediaExempt(t *testing.T) {
	svc, stub, session := newDebounceFixture(t, 3)
	svc.triggerAIWithDebounce(context.Background(), debounceEvent(session, "", 3))
	if stub.count() != 1 {
		t.Fatalf("媒体消息应立即触发 1 次, got %d", stub.count())
	}
}

func TestAIDebounce_DisabledByEnv(t *testing.T) {
	t.Setenv("AI_DEBOUNCE_SECONDS", "0")
	svc, stub, session := newDebounceFixture(t, 0)
	ctx := context.Background()
	svc.triggerAIWithDebounce(ctx, debounceEvent(session, "第一条", 0))
	if stub.count() != 1 {
		t.Fatalf("禁用防抖应立即触发第 1 次, got %d", stub.count())
	}
	svc.ReleaseAIProcessingFlag(ctx, session+"-conv")
	svc.triggerAIWithDebounce(ctx, debounceEvent(session, "第二条", 0))
	if stub.count() != 2 {
		t.Fatalf("释放锁后第二条应立即触发, got %d", stub.count())
	}
}

func TestCoalescePending(t *testing.T) {
	got := coalescePending([]string{"第三条", "第二条", "第一条"})
	if got[0] != "第一条" || got[2] != "第三条" {
		t.Fatalf("应反转为时间序, got %v", got)
	}
	many := make([]string, aiDebounceMaxMessages+5)
	for i := range many {
		many[i] = fmt.Sprintf("m%d", i)
	}
	out := coalescePending(many)
	if len(out) != aiDebounceMaxMessages {
		t.Fatalf("应截断到 %d 条, got %d", aiDebounceMaxMessages, len(out))
	}
}

func TestAIDebounce_NewWindowAfterClose(t *testing.T) {
	svc, stub, session := newDebounceFixture(t, 1)
	ctx := context.Background()
	svc.triggerAIWithDebounce(ctx, debounceEvent(session, "第一窗口", 1))
	deadline := time.Now().Add(4 * time.Second)
	for stub.count() == 0 && time.Now().Before(deadline) {
		time.Sleep(50 * time.Millisecond)
	}
	if stub.count() != 1 {
		t.Fatalf("第一窗口应触发, got %d", stub.count())
	}

	svc.ReleaseAIProcessingFlag(context.Background(), session+"-conv")

	svc.triggerAIWithDebounce(ctx, debounceEvent(session, "第二窗口", 1))
	deadline = time.Now().Add(4 * time.Second)
	for stub.count() < 2 && time.Now().Before(deadline) {
		time.Sleep(50 * time.Millisecond)
	}
	if stub.count() != 2 {
		t.Fatalf("第二窗口应独立触发, got %d", stub.count())
	}
	if !strings.Contains(stub.last().content, "第二窗口") {
		t.Fatalf("第二窗口内容错误: %q", stub.last().content)
	}
}
