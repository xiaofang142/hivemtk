package service

import (
	"context"
	"testing"
	"time"

	"hivemtk-user/internal/cache"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/testutil"
)

// TestRecheck_NoHubRepo_SafelySkips 验证 hubRepo==nil 时 RecheckUnrepliedAndTrigger 安全跳过不 panic。
func TestRecheck_NoHubRepo_SafelySkips(t *testing.T) {
	svc := NewInboxIngressServiceWithDB(nil, newIsolatedCacheForTest(t))
	svc.RecheckUnrepliedAndTrigger(context.Background(), "conv1", "sess1")
}

// TestRecheck_EmptyConvID_SafelySkips 验证 conversationID 为空时安全跳过。
func TestRecheck_EmptyConvID_SafelySkips(t *testing.T) {
	svc := NewInboxIngressServiceWithDB(nil, newIsolatedCacheForTest(t))
	svc.RecheckUnrepliedAndTrigger(context.Background(), "", "sess1")
}

// TestRecheck_LastIsOutbound_NoTrigger 验证最后一条是 outbound（AI已回复）时不补触发 AI。
func TestRecheck_LastIsOutbound_NoTrigger(t *testing.T) {
	db := testutil.NewTestDBOrSkip(t, &model.MessageHub{})
	now := time.Now()
	db.Create(&model.MessageHub{
		MsgID: "in1", Platform: "douyin", AccountID: "acc1",
		Direction: "inbound", MsgType: "text", SenderID: "cust1",
		Content: "你好", ConversationID: "conv-out", SentAt: now.Add(-10 * time.Second),
	})
	db.Create(&model.MessageHub{
		MsgID: "out1", Platform: "douyin", AccountID: "acc1",
		Direction: "outbound", MsgType: "text", SenderID: "acc1", ReceiverID: "cust1",
		Content: "AI回复", ConversationID: "conv-out",
		IsAIReply: true, AIAgent: "sales_engine", SentAt: now.Add(-1 * time.Second),
	})

	mc := cache.NewMemoryCache()

	defer mc.Close()
	defer mc.Close()
	svc := NewInboxIngressServiceWithDB(db, mc)
	tr := &fakeAITrigger{}
	svc.SetAITrigger(tr)

	svc.RecheckUnrepliedAndTrigger(context.Background(), "conv-out", "")

	if tr.called != 0 {
		t.Fatalf("最后一条是 outbound 时不应补触发 AI，实际调用 %d 次", tr.called)
	}
}

// TestRecheck_LastInboundWithinWindow_TriggersAI 验证最后一条是 inbound 且在 5min 窗口内时补触发 AI。
func TestRecheck_LastInboundWithinWindow_TriggersAI(t *testing.T) {
	db := testutil.NewTestDBOrSkip(t, &model.MessageHub{})
	now := time.Now()
	expectedContent := "我要咨询订单"
	db.Create(&model.MessageHub{
		MsgID: "in1", Platform: "douyin", AccountID: "acc1",
		Direction: "inbound", MsgType: "text", SenderID: "cust1",
		SenderName: "客户A", Content: expectedContent,
		ConversationID: "conv-recheck-1", SentAt: now.Add(-1 * time.Second),
	})

	mc := cache.NewMemoryCache()

	defer mc.Close()
	defer mc.Close()
	svc := NewInboxIngressServiceWithDB(db, mc)
	tr := &fakeAITrigger{}
	svc.SetAITrigger(tr)

	svc.RecheckUnrepliedAndTrigger(context.Background(), "conv-recheck-1", "")

	if tr.called != 1 {
		t.Fatalf("最后一条是 inbound 且在窗口内时应补触发 AI 1 次，实际 %d 次", tr.called)
	}
	if tr.lastContent != expectedContent {
		t.Fatalf("补触发内容应为 %q，实际 %q", expectedContent, tr.lastContent)
	}
	if tr.lastConv != "conv-recheck-1" {
		t.Fatalf("补触发 conversationID 应为 conv-recheck-1，实际 %q", tr.lastConv)
	}
	if tr.lastChannel != "douyin" {
		t.Fatalf("补触发 channel 应为 douyin，实际 %q", tr.lastChannel)
	}
}

// TestRecheck_LastInboundOutsideWindow_NoTrigger 验证最后一条 inbound 超 5min 窗口时不补触发。
func TestRecheck_LastInboundOutsideWindow_NoTrigger(t *testing.T) {
	db := testutil.NewTestDBOrSkip(t, &model.MessageHub{})
	db.Create(&model.MessageHub{
		MsgID: "in-old", Platform: "douyin", AccountID: "acc1",
		Direction: "inbound", MsgType: "text", SenderID: "cust1",
		Content: "历史消息", ConversationID: "conv-old",
		SentAt: time.Now().Add(-6 * time.Minute),
	})

	mc := cache.NewMemoryCache()

	defer mc.Close()
	defer mc.Close()
	svc := NewInboxIngressServiceWithDB(db, mc)
	tr := &fakeAITrigger{}
	svc.SetAITrigger(tr)

	svc.RecheckUnrepliedAndTrigger(context.Background(), "conv-old", "")

	if tr.called != 0 {
		t.Fatalf("最后一条 inbound 超 5min 窗口时不应补触发，实际调用 %d 次", tr.called)
	}
}

// TestRecheck_AIProcessingFlagExists_NoTrigger 验证 ai_processing 标记存在（新一轮 AI 已在处理）时不补触发。
func TestRecheck_AIProcessingFlagExists_NoTrigger(t *testing.T) {
	db := testutil.NewTestDBOrSkip(t, &model.MessageHub{})
	now := time.Now()
	db.Create(&model.MessageHub{
		MsgID: "in-flag", Platform: "douyin", AccountID: "acc1",
		Direction: "inbound", MsgType: "text", SenderID: "cust1",
		Content: "新消息", ConversationID: "conv-flag", SentAt: now,
	})

	mc := cache.NewMemoryCache()

	defer mc.Close()
	defer mc.Close()
	svc := NewInboxIngressServiceWithDB(db, mc)
	tr := &fakeAITrigger{}
	svc.SetAITrigger(tr)

	aiKey := InboxAIProcessingKey + "conv-flag"
	if err := mc.Set(context.Background(), aiKey, "1", InboxAIProcessingTTL); err != nil {
		t.Fatalf("设置 ai_processing 标记失败: %v", err)
	}

	svc.RecheckUnrepliedAndTrigger(context.Background(), "conv-flag", "")

	if tr.called != 0 {
		t.Fatalf("ai_processing 标记存在时不应补触发（新一轮 AI 已在处理），实际调用 %d 次", tr.called)
	}
}

// TestRecheck_HumanLocked_NoTrigger 验证会话被人工接管时不补触发 AI。
func TestRecheck_HumanLocked_NoTrigger(t *testing.T) {
	db := testutil.NewTestDBOrSkip(t, &model.MessageHub{})
	now := time.Now()
	db.Create(&model.MessageHub{
		MsgID: "in-human", Platform: "douyin", AccountID: "acc1",
		Direction: "inbound", MsgType: "text", SenderID: "cust1",
		Content: "新消息", ConversationID: "conv-human", SentAt: now,
	})

	mc := cache.NewMemoryCache()

	defer mc.Close()
	defer mc.Close()
	svc := NewInboxIngressServiceWithDB(db, mc)
	tr := &fakeAITrigger{}
	svc.SetAITrigger(tr)

	sessionID := "sess-human"
	if err := svc.LockSessionForHuman(context.Background(), sessionID, "test"); err != nil {
		t.Fatalf("设置人工接管锁失败: %v", err)
	}

	svc.RecheckUnrepliedAndTrigger(context.Background(), "conv-human", sessionID)

	if tr.called != 0 {
		t.Fatalf("会话被人工接管时不应补触发 AI，实际调用 %d 次", tr.called)
	}
}

// TestRecheck_FullScenario_OrphanMessage 补触发完整极限场景测试：
//
// 时序：用户消息1(inbound) → 触发AI（设置标记）→ AI推理中
//
//	→ 用户消息2(inbound)入库 → 查最后一条=inbound 但标记存在 → 跳过触发
//	→ AI回复消息1(outbound) → 释放标记 → 消息2成为"孤儿"
//	→ RecheckUnrepliedAndTrigger 补触发消息2
//
// 验证：补触发后 AI 被调用，且触发内容是消息2（最后一条 inbound）。
func TestRecheck_FullScenario_OrphanMessage(t *testing.T) {
	db := testutil.NewTestDBOrSkip(t, &model.MessageHub{})
	now := time.Now()
	convID := "conv-orphan"

	db.Create(&model.MessageHub{
		MsgID: "in1", Platform: "douyin", AccountID: "acc1",
		Direction: "inbound", MsgType: "text", SenderID: "cust1",
		Content: "第一条消息", ConversationID: convID,
		SentAt: now.Add(-10 * time.Second),
	})

	mc := cache.NewMemoryCache()

	defer mc.Close()
	defer mc.Close()
	svc := NewInboxIngressServiceWithDB(db, mc)
	tr := &fakeAITrigger{}
	svc.SetAITrigger(tr)

	aiKey := InboxAIProcessingKey + convID
	if err := mc.Set(context.Background(), aiKey, "1", InboxAIProcessingTTL); err != nil {
		t.Fatalf("设置 ai_processing 标记失败: %v", err)
	}

	msg2Content := "第二条消息（遗漏）"
	db.Create(&model.MessageHub{
		MsgID: "in2", Platform: "douyin", AccountID: "acc1",
		Direction: "inbound", MsgType: "text", SenderID: "cust1",
		Content: msg2Content, ConversationID: convID,
		SentAt: now.Add(-1 * time.Second),
	})

	unreplied, withinWindow, err := svc.hubRepo.HasUnrepliedCustomerMessage(
		context.Background(), convID, InboxReplyWindow)
	if err != nil {
		t.Fatalf("HasUnrepliedCustomerMessage 失败: %v", err)
	}
	if !unreplied || !withinWindow {
		t.Fatalf("期望 unreplied=true withinWindow=true，实际 unreplied=%v withinWindow=%v", unreplied, withinWindow)
	}
	flagExists, _ := mc.Exists(context.Background(), aiKey)
	if !flagExists {
		t.Fatal("ai_processing 标记应存在")
	}
	t.Logf("✓ 模拟 HandleIngressBatch：消息2被 ai_processing 标记跳过（孤儿消息形成）")

	db.Create(&model.MessageHub{
		MsgID: "out1", Platform: "douyin", AccountID: "acc1",
		Direction: "outbound", MsgType: "text", SenderID: "acc1", ReceiverID: "cust1",
		Content: "AI回复消息1", ConversationID: convID,
		IsAIReply: true, AIAgent: "sales_engine", SentAt: now,
	})
	svc.ReleaseAIProcessingFlag(context.Background(), convID)

	db.Where("conversation_id = ?", convID).Delete(&model.MessageHub{})

	db.Create(&model.MessageHub{
		MsgID: "in1-b", Platform: "douyin", AccountID: "acc1",
		Direction: "inbound", MsgType: "text", SenderID: "cust1",
		Content: "第一条消息", ConversationID: convID,
		SentAt: now.Add(-10 * time.Second),
	})
	db.Create(&model.MessageHub{
		MsgID: "out1-b", Platform: "douyin", AccountID: "acc1",
		Direction: "outbound", MsgType: "text", SenderID: "acc1", ReceiverID: "cust1",
		Content: "AI回复", ConversationID: convID,
		IsAIReply: true, AIAgent: "sales_engine", SentAt: now.Add(-5 * time.Second),
	})
	db.Create(&model.MessageHub{
		MsgID: "in2-b", Platform: "douyin", AccountID: "acc1",
		Direction: "inbound", MsgType: "text", SenderID: "cust1",
		Content: msg2Content, ConversationID: convID,
		SentAt: now.Add(-1 * time.Second),
	})

	tr.called = 0
	svc.RecheckUnrepliedAndTrigger(context.Background(), convID, "")

	if tr.called != 1 {
		t.Fatalf("AI 回复后用户新发消息应补触发 AI 1 次，实际 %d 次", tr.called)
	}
	if tr.lastContent != msg2Content {
		t.Fatalf("补触发内容应为最后一条 inbound %q，实际 %q", msg2Content, tr.lastContent)
	}
	t.Logf("✓ 极限场景修复验证通过：AI 回复后用户新发消息被正确补触发")
}

// TestRecheck_PersistentSendFailure_BudgetCapped 复现 2026-09 TG 全链路测试发现的补触发风暴：
//
//	AI 回复已生成但出站发送持续失败(retryable 网络错误) → message_hub 无任何出站痕迹 →
//	sendOutbound 尾部每轮都 ReleaseAIProcessingFlag + go RecheckUnrepliedAndTrigger →
//	recheck 以全新 uuid event 重触发 AI（绕过 eventID 去重），5min 窗口内单条入站消息烧掉 11 次 LLM 调用。
//
// 修复语义：recheck 只负责「补 AI 推理期间遗漏的新消息」，同一条入站消息至多补触发 1 次；
// 投递失败的重试不是 recheck 的职责。
func TestRecheck_PersistentSendFailure_BudgetCapped(t *testing.T) {
	db := testutil.NewTestDBOrSkip(t, &model.MessageHub{})
	db.Create(&model.MessageHub{
		MsgID: "in-stuck", Platform: "telegram", AccountID: "5",
		Direction: "inbound", MsgType: "text", SenderID: "cust1",
		Content: "这条消息的回复永远投递不出去", ConversationID: "conv-stuck",
		SentAt: time.Now().Add(-2 * time.Second),
	})

	mc := cache.NewMemoryCache()
	defer mc.Close()
	svc := NewInboxIngressServiceWithDB(db, mc)
	tr := &fakeAITrigger{}
	svc.SetAITrigger(tr)

	for i := 0; i < 3; i++ {
		svc.RecheckUnrepliedAndTrigger(context.Background(), "conv-stuck", "")
		// 模拟真实链路：runAIGeneration 结束后 defer 释放 ai_processing 标记，
		// 使下一轮 recheck 能通过「处理中」守卫、抵达预算判定。
		svc.ReleaseAIProcessingFlag(context.Background(), "conv-stuck")
	}

	if tr.called > 1 {
		t.Fatalf("同一条入站消息的补触发应被预算封顶(≤1 次)，实际触发 %d 次——发送失败风暴未被阻断", tr.called)
	}
}

// TestRecheck_NewMessageGetsFreshBudget 验证预算按消息粒度计：
// 前一条消息耗尽预算后，新到达的入站消息仍可正常补触发。
func TestRecheck_NewMessageGetsFreshBudget(t *testing.T) {
	db := testutil.NewTestDBOrSkip(t, &model.MessageHub{})
	now := time.Now()
	convID := "conv-budget"
	db.Create(&model.MessageHub{
		MsgID: "in-old", Platform: "telegram", AccountID: "5",
		Direction: "inbound", MsgType: "text", SenderID: "cust1",
		Content: "旧消息已耗尽预算", ConversationID: convID,
		SentAt: now.Add(-3 * time.Second),
	})

	mc := cache.NewMemoryCache()
	defer mc.Close()
	svc := NewInboxIngressServiceWithDB(db, mc)
	tr := &fakeAITrigger{}
	svc.SetAITrigger(tr)

	// 两轮 recheck（中间释放标记模拟真实链路）：第一轮触发（消耗旧消息预算），第二轮被封顶
	svc.RecheckUnrepliedAndTrigger(context.Background(), convID, "")
	svc.ReleaseAIProcessingFlag(context.Background(), convID)
	svc.RecheckUnrepliedAndTrigger(context.Background(), convID, "")
	svc.ReleaseAIProcessingFlag(context.Background(), convID)
	first := tr.called
	if first != 1 {
		t.Fatalf("旧消息应只补触发 1 次，实际 %d 次", first)
	}

	// 模拟：旧消息已回复(outbound 落库)，新消息到达成为最后一条 inbound
	db.Create(&model.MessageHub{
		MsgID: "out-old", Platform: "telegram", AccountID: "5",
		Direction: "outbound", MsgType: "text", SenderID: "acc5", ReceiverID: "cust1",
		Content: "已回复旧消息", ConversationID: convID,
		IsAIReply: true, AIAgent: "sales_engine", SentAt: now,
	})
	db.Create(&model.MessageHub{
		MsgID: "in-new", Platform: "telegram", AccountID: "5",
		Direction: "inbound", MsgType: "text", SenderID: "cust1",
		Content: "推理期间到达的新消息", ConversationID: convID,
		SentAt: now.Add(time.Second),
	})

	svc.RecheckUnrepliedAndTrigger(context.Background(), convID, "")
	if tr.called != first+1 {
		t.Fatalf("新消息应获得独立预算并补触发，实际总触发 %d 次", tr.called)
	}
	if tr.lastContent != "推理期间到达的新消息" {
		t.Fatalf("补触发内容应为新消息，实际 %q", tr.lastContent)
	}
}

// TestRecheck_ContextCanceled_NoBlock 验证 context 取消时 RecheckUnrepliedAndTrigger 不阻塞。
func TestRecheck_ContextCanceled_NoBlock(t *testing.T) {
	svc := NewInboxIngressServiceWithDB(nil, newIsolatedCacheForTest(t))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	done := make(chan struct{})
	go func() {
		svc.RecheckUnrepliedAndTrigger(ctx, "conv1", "")
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("context 取消后 RecheckUnrepliedAndTrigger 应立即返回，但阻塞了 2s")
	}
}
