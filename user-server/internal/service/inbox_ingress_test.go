package service

import (
	"context"
	"testing"
	"time"

	"hivemtk-user/internal/cache"
	"hivemtk-user/internal/model"
)

func TestInboxIngress_NormalizeEvent_Defaults(t *testing.T) {
	_mc1 := cache.NewMemoryCache()
	defer _mc1.Close()
	svc := NewInboxIngressService()
	svc.cache = _mc1

	event := &model.MessageEvent{
		Channel:  model.ChannelWeb,
		SenderID: "user-001",
		Content:  "hi",
	}
	if err := svc.NormalizeEvent(context.Background(), event); err != nil {
		t.Fatalf("normalize: %v", err)
	}
	if event.EventID == "" {
		t.Fatal("EventID should be auto-assigned")
	}
	if event.SessionID == "" {
		t.Fatal("SessionID should be derived")
	}
	if event.MsgType != model.MsgTypeText {
		t.Fatalf("MsgType default = text, got %q", event.MsgType)
	}
	if event.Timestamp.IsZero() {
		t.Fatal("Timestamp should be set")
	}
}

func TestInboxIngress_NormalizeEvent_RejectsEmptyChannel(t *testing.T) {
	_mc2 := cache.NewMemoryCache()
	defer _mc2.Close()
	svc := NewInboxIngressService()
	svc.cache = _mc2
	err := svc.NormalizeEvent(context.Background(), &model.MessageEvent{SenderID: "x"})
	if err == nil {
		t.Fatal("expected error for empty channel")
	}
}

func TestInboxIngress_NormalizeEvent_EmptySenderFallsBack(t *testing.T) {
	_mc3 := cache.NewMemoryCache()
	defer _mc3.Close()
	svc := NewInboxIngressService()
	svc.cache = _mc3
	ev := &model.MessageEvent{Channel: model.ChannelWeb}
	if err := svc.NormalizeEvent(context.Background(), ev); err != nil {
		t.Fatalf("空 sender_id 应走兜底而非报错，实际: %v", err)
	}
	if ev.SenderID == "" {
		t.Fatal("空 sender_id 兜底后不应为空")
	}
}

func TestInboxIngress_HumanLockCycle(t *testing.T) {
	ctx := context.Background()
	c := cache.NewMemoryCache()
	defer c.Close()
	svc := NewInboxIngressService()
	svc.cache = c

	sessionID := "sess-lock-test"
	locked, err := svc.IsSessionHumanLocked(ctx, sessionID)
	if err != nil {
		t.Fatalf("check lock: %v", err)
	}
	if locked {
		t.Fatal("session should not be locked initially")
	}

	if err := svc.LockSessionForHuman(ctx, sessionID, "test"); err != nil {
		t.Fatalf("lock: %v", err)
	}
	locked, _ = svc.IsSessionHumanLocked(ctx, sessionID)
	if !locked {
		t.Fatal("session should be locked after LockSessionForHuman")
	}

	if err := svc.UnlockSessionForHuman(ctx, sessionID); err != nil {
		t.Fatalf("unlock: %v", err)
	}
	locked, _ = svc.IsSessionHumanLocked(ctx, sessionID)
	if locked {
		t.Fatal("session should be unlocked after UnlockSessionForHuman")
	}
}

func TestInboxIngress_AIProcessingLockSerializes(t *testing.T) {
	ctx := context.Background()
	c := cache.NewMemoryCache()
	defer c.Close()
	svc := NewInboxIngressService()
	svc.cache = c

	sessionID := "sess-ai-lock"

	ok1, err := svc.tryAcquireAILock(ctx, sessionID)
	if err != nil {
		t.Fatalf("acquire1: %v", err)
	}
	if !ok1 {
		t.Fatal("first acquire should succeed")
	}

	ok2, _ := svc.tryAcquireAILock(ctx, sessionID)
	if ok2 {
		t.Fatal("second acquire should fail (key exists)")
	}

	svc.ReleaseAILock(ctx, sessionID)
	ok3, _ := svc.tryAcquireAILock(ctx, sessionID)
	if !ok3 {
		t.Fatal("acquire after release should succeed")
	}
	svc.ReleaseAILock(ctx, sessionID)
}

func TestInboxIngress_PendingQueue(t *testing.T) {
	ctx := context.Background()
	c := cache.NewMemoryCache()
	defer c.Close()
	svc := NewInboxIngressService()
	svc.cache = c

	sessionID := "sess-pending"

	if err := svc.AppendPendingMessage(ctx, sessionID, "msg-1"); err != nil {
		t.Fatalf("append1: %v", err)
	}
	if err := svc.AppendPendingMessage(ctx, sessionID, "msg-2"); err != nil {
		t.Fatalf("append2: %v", err)
	}
	if err := svc.AppendPendingMessage(ctx, sessionID, "msg-3"); err != nil {
		t.Fatalf("append3: %v", err)
	}

	items, err := svc.PopPendingMessages(ctx, sessionID)
	if err != nil {
		t.Fatalf("pop: %v", err)
	}
	if len(items) != 3 {
		t.Fatalf("expected 3 pending, got %d", len(items))
	}

	items2, _ := svc.PopPendingMessages(ctx, sessionID)
	if len(items2) != 0 {
		t.Fatalf("expected empty after first pop, got %d", len(items2))
	}
}

func TestInboxIngress_HandleIngressMessage_HumanLockedBypassesAI(t *testing.T) {
	ctx := context.Background()
	c := cache.NewMemoryCache()
	defer c.Close()
	svc := NewInboxIngressService()
	svc.cache = c

	sessionID := "sess-h-lock-bypass"
	if err := svc.LockSessionForHuman(ctx, sessionID, "test"); err != nil {
		t.Fatalf("lock: %v", err)
	}

	event := &model.MessageEvent{
		SessionID: sessionID,
		Channel:   model.ChannelTelegram,
		SenderID:  "tg-user-1",
		Content:   "我被锁住了",
	}
	result, err := svc.HandleIngressMessage(ctx, event)
	if err != nil {
		t.Fatalf("handle: %v", err)
	}
	if !result.Accepted {
		t.Fatal("should be accepted")
	}
	if !result.HumanLocked {
		t.Fatal("should flag human_locked")
	}
	if result.QueuedForAI {
		t.Fatal("should not be queued for AI when human-locked")
	}
}

func TestInboxIngress_HandleIngressMessage_TriggersAITrigger(t *testing.T) {
	t.Setenv("AI_DEBOUNCE_SECONDS", "0")
	ctx := context.Background()
	c := cache.NewMemoryCache()
	defer c.Close()
	svc := NewInboxIngressService()
	svc.cache = c

	trigger := &fakeAITrigger{}
	svc.SetAITrigger(trigger)

	event := &model.MessageEvent{
		Channel:  model.ChannelWhatsApp,
		SenderID: "wa-1",
		Content:  "hi",
		EventID:  "evt-test-1",
		Extra:    map[string]interface{}{"account_id": "wa-acct-1"},
	}
	result, err := svc.HandleIngressMessage(ctx, event)
	if err != nil {
		t.Fatalf("handle: %v", err)
	}
	if !result.Accepted || !result.QueuedForAI {
		t.Fatalf("expected accepted+queued_for_ai, got %+v", result)
	}
	if result.HumanLocked {
		t.Fatal("should not be human_locked")
	}
	if trigger.called != 1 {
		t.Fatalf("aiTrigger should be invoked once, got %d", trigger.called)
	}
	if trigger.lastChannel != model.ChannelWhatsApp {
		t.Fatalf("channel mismatch: got %q", trigger.lastChannel)
	}
	if trigger.lastAccount != "wa-acct-1" {
		t.Fatalf("account mismatch: got %q", trigger.lastAccount)
	}
	if trigger.lastContent != "hi" {
		t.Fatalf("content mismatch: got %q", trigger.lastContent)
	}
	if trigger.lastEventID != "evt-test-1" {
		t.Fatalf("eventID mismatch: got %q", trigger.lastEventID)
	}
}

func TestInboxIngress_HandleIngressMessage_QueuesWhileAIBusy(t *testing.T) {
	ctx := context.Background()
	c := cache.NewMemoryCache()
	defer c.Close()
	svc := NewInboxIngressService()
	svc.cache = c

	first := &model.MessageEvent{
		Channel:  model.ChannelWeb,
		SenderID: "user-x",
		Content:  "first",
	}
	r1, err := svc.HandleIngressMessage(ctx, first)
	if err != nil {
		t.Fatalf("first handle: %v", err)
	}
	_ = r1.SessionID

	second := &model.MessageEvent{
		Channel:  model.ChannelWeb,
		SenderID: "user-x",
		Content:  "second",
	}
	r2, err := svc.HandleIngressMessage(ctx, second)
	if err != nil {
		t.Fatalf("second handle: %v", err)
	}
	if !r2.QueuedForAI {
		t.Fatalf("second message should be queued, got %+v", r2)
	}
	if r2.Reason == "" {
		t.Fatal("reason should be set for queued message")
	}
}

func TestInboxIngress_IsSessionAIBusy(t *testing.T) {
	ctx := context.Background()
	c := cache.NewMemoryCache()
	defer c.Close()
	svc := NewInboxIngressService()
	svc.cache = c

	sessionID := "sess-ai-busy-check"
	busy, _ := svc.IsSessionAIBusy(ctx, sessionID)
	if busy {
		t.Fatal("should not be busy initially")
	}

	svc.tryAcquireAILock(ctx, sessionID)
	busy, _ = svc.IsSessionAIBusy(ctx, sessionID)
	if !busy {
		t.Fatal("should be busy after lock")
	}
}

// 保证在不依赖具体 Redis 客户端的情况下能正常完成
// 整个消息中台的关键路径
func TestInboxIngress_EndToEndScenario(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	c := cache.NewMemoryCache()

	defer c.Close()
	svc := NewInboxIngressService()
	svc.cache = c

	r1, err := svc.HandleIngressMessage(ctx, &model.MessageEvent{
		Channel:  model.ChannelTelegram,
		SenderID: "tg-customer-1",
		Content:  "你好",
	})
	if err != nil {
		t.Fatalf("scenario step1: %v", err)
	}
	if !r1.QueuedForAI {
		t.Fatal("first message should trigger AI")
	}

	r2, err := svc.HandleIngressMessage(ctx, &model.MessageEvent{
		Channel:  model.ChannelTelegram,
		SenderID: "tg-customer-1",
		Content:  "在线吗",
	})
	if err != nil {
		t.Fatalf("scenario step2: %v", err)
	}
	if !r2.QueuedForAI {
		t.Fatal("second message should be queued (AI lock held)")
	}

	if err := svc.LockSessionForHuman(ctx, r1.SessionID, "用户要求转人工"); err != nil {
		t.Fatalf("scenario step3 lock: %v", err)
	}

	r4, err := svc.HandleIngressMessage(ctx, &model.MessageEvent{
		Channel:  model.ChannelTelegram,
		SenderID: "tg-customer-1",
		Content:  "我要找活人",
	})
	if err != nil {
		t.Fatalf("scenario step4: %v", err)
	}
	if !r4.HumanLocked {
		t.Fatal("after human lock, message should be human-locked")
	}

	if err := svc.UnlockSessionForHuman(ctx, r1.SessionID); err != nil {
		t.Fatalf("scenario step5 unlock: %v", err)
	}
	svc.ReleaseAILock(ctx, r1.SessionID)
	r5, err := svc.HandleIngressMessage(ctx, &model.MessageEvent{
		Channel:  model.ChannelTelegram,
		SenderID: "tg-customer-1",
		Content:  "我又来了",
	})
	if err != nil {
		t.Fatalf("scenario step5: %v", err)
	}
	if r5.HumanLocked {
		t.Fatal("after unlock, message should not be human-locked")
	}
	if !r5.QueuedForAI {
		t.Fatal("after unlock, AI should be triggered again")
	}
}
