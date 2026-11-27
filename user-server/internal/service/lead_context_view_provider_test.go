package service

import (
	"context"
	"strings"
	"testing"
	"time"

	agentcontext "hivemtk-user/internal/aiagent/context"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/repository"
)

// H6：线索链必须消费与回复链同一份不可变 ContextView。
// 这里用假 provider / 假 reader，不依赖 POSTGRES_TEST_PASSWORD。

func leadContextViewTestHub() *model.MessageHub {
	return &model.MessageHub{
		TenantID: 7, Platform: "douyin", AccountID: "acc-1",
		ConversationID: "conv-1", GroupID: "group-1", SenderID: "u1",
		MsgID: "hub:100", Content: "那个多少钱？", IsGroup: true, Direction: "inbound",
	}
}

// 预置冻结作用域，让 buildLeadJudgeContext 不触库（channelAccountID=0 ⇒ 共享上下文读取短路）。
func leadContextViewTestCtx(hub *model.MessageHub) context.Context {
	ctx := tenant.WithTenantID(context.Background(), hub.TenantID)
	return context.WithValue(ctx, leadHistoryScopeKey{}, &leadHistoryScope{
		MessageHubHistoryScope: repository.MessageHubHistoryScope{
			Platform: hub.Platform, AccountIDs: []string{hub.AccountID}, IsGroup: hub.IsGroup,
		},
		tenantID: hub.TenantID, sourceRef: hub.AccountID, accountKey: "raw:" + hub.AccountID,
	})
}

func leadContextViewTestHistory() []DetectionMessage {
	return []DetectionMessage{
		{MessageID: "hub:99", Role: "assistant", SenderID: "bot", Content: "聊的是 A 产品的报价", Visibility: agentcontext.VisibilityPublic},
		{MessageID: "hub:98", Role: "user", SenderID: "u2", Content: "我的手机号是 13800138000", Visibility: agentcontext.VisibilityPrivate},
	}
}

func leadContextViewTestService(capture func(DetectionInput)) *Service {
	return &Service{
		ruleService: NewLeadRuleServiceWithRepo(nil),
		hitsProvider: func(_ context.Context, in DetectionInput) ([]model.LeadMiningHit, error) {
			capture(in)
			return nil, nil
		},
	}
}

func TestBuildLeadJudgeContext_ContextViewProviderWiresJudgeAndDetection(t *testing.T) {
	hub := leadContextViewTestHub()
	view := testLeadDetectionView()
	scope := view.Scope
	var args []string
	isGroupSeen := false
	var got DetectionInput
	svc := leadContextViewTestService(func(in DetectionInput) { got = in })
	svc.SetContextViewProvider(func(_ context.Context, platform, accountID, conversationID, groupID, senderID, messageID string, isGroup bool) (*agentcontext.AgentConversationContext, *agentcontext.ConversationScope, error) {
		args = []string{platform, accountID, conversationID, groupID, senderID, messageID}
		isGroupSeen = isGroup
		return view, &scope, nil
	})

	jctx := svc.buildLeadJudgeContext(leadContextViewTestCtx(hub), hub, leadContextViewTestHistory(), "lmj_h6")

	want := []string{hub.Platform, hub.AccountID, hub.ConversationID, hub.GroupID, hub.SenderID, hub.MsgID}
	if strings.Join(args, "|") != strings.Join(want, "|") || !isGroupSeen {
		t.Fatalf("provider 必须收到权威 MessageHub 字段: got=%v isGroup=%t want=%v", args, isGroupSeen, want)
	}
	if jctx.ContextView != view {
		t.Fatalf("jctx.ContextView 未注入 provider 快照: %+v", jctx.ContextView)
	}
	if jctx.Scope != &scope {
		t.Fatalf("jctx.Scope 未注入 provider 作用域: %+v", jctx.Scope)
	}
	if got.ContextView != view || got.Scope != &scope {
		t.Fatalf("三路检测输入未消费同一份 ContextView/Scope: view=%+v scope=%+v", got.ContextView, got.Scope)
	}
}

func TestBuildLeadJudgeContext_ContextViewFiltersGroupAudience(t *testing.T) {
	hub := leadContextViewTestHub()
	view := testLeadDetectionView()
	scope := view.Scope
	svc := leadContextViewTestService(func(DetectionInput) {})
	svc.SetContextViewProvider(func(context.Context, string, string, string, string, string, string, bool) (*agentcontext.AgentConversationContext, *agentcontext.ConversationScope, error) {
		return view, &scope, nil
	})
	jctx := svc.buildLeadJudgeContext(leadContextViewTestCtx(hub), hub, leadContextViewTestHistory(), "lmj_h6_audience")

	joined := ""
	for _, m := range BuildLeadDetectionMessages(DetectionInput{Messages: jctx.Messages, ContextView: jctx.ContextView}) {
		joined += m.Content
	}
	for _, m := range leadJudgeMessagesWithEvidence(jctx) {
		joined += m.Content
	}
	if strings.Contains(joined, "13800138000") || strings.Contains(joined, "hub:98") {
		t.Fatalf("其他成员的私有消息不得进入线索模型输入: %q", joined)
	}
	if !strings.Contains(joined, "hub:99") || !strings.Contains(joined, "hub:100") {
		t.Fatalf("可证明可见的消息必须保留真实证据锚点: %q", joined)
	}
}

func TestBuildLeadJudgeContext_ContextViewBindsCanonicalSession(t *testing.T) {
	hub := leadContextViewTestHub()
	view := testLeadDetectionView()
	scope := view.Scope
	var got DetectionInput
	svc := leadContextViewTestService(func(in DetectionInput) { got = in })
	svc.SetContextViewProvider(func(context.Context, string, string, string, string, string, string, bool) (*agentcontext.AgentConversationContext, *agentcontext.ConversationScope, error) {
		return view, &scope, nil
	})
	jctx := svc.buildLeadJudgeContext(leadContextViewTestCtx(hub), hub, leadContextViewTestHistory(), "lmj_h6_session")

	detectReq := leadDetectionDispatchRequest(got, "system", nil)
	judgeReq := leadJudgeDispatchRequest(jctx, nil)
	if detectReq.LLMSessionRef != "llmsess-canonical" || judgeReq.LLMSessionRef != "llmsess-canonical" {
		t.Fatalf("线索检测/判定必须绑定 canonical session: detect=%q judge=%q", detectReq.LLMSessionRef, judgeReq.LLMSessionRef)
	}
	if detectReq.ExecutionScenarioID != string(agentcontext.ExecutionScenarioLeadDetect) ||
		judgeReq.ExecutionScenarioID != string(agentcontext.ExecutionScenarioLeadJudge) {
		t.Fatalf("必须使用各自执行场景: detect=%q judge=%q", detectReq.ExecutionScenarioID, judgeReq.ExecutionScenarioID)
	}
}

func TestBuildLeadJudgeContext_WithoutProviderKeepsLegacyNilView(t *testing.T) {
	hub := leadContextViewTestHub()
	var got DetectionInput
	svc := leadContextViewTestService(func(in DetectionInput) { got = in })

	jctx := svc.buildLeadJudgeContext(leadContextViewTestCtx(hub), hub, leadContextViewTestHistory(), "lmj_legacy")

	if jctx.ContextView != nil || jctx.Scope != nil || got.ContextView != nil || got.Scope != nil {
		t.Fatalf("未装配 provider 时保持既有 nil 降级行为: jctx=%+v detection=%+v", jctx, got)
	}
}

func TestNewLeadContextViewProvider_BuildsGroupViewWithCanonicalSession(t *testing.T) {
	base := time.Date(2026, 11, 27, 10, 0, 0, 0, time.UTC)
	reader := &fakeAgentContextReader{messages: []UnifiedMessage{
		builderTestMessage("hub:11", "u2", "群里公开问题", base.Add(-time.Minute), agentcontext.VisibilityPublic),
		builderTestMessage("hub:12", "u2", "私下报价秘密", base.Add(-30*time.Second), agentcontext.VisibilityMember),
	}}
	reader.messages[1].Audience = []string{"u2"}
	provider := NewLeadContextViewProvider(NewConversationContextBuilder(reader))
	if provider == nil {
		t.Fatal("builder 已装配时必须返回 provider")
	}
	ctx := tenant.WithTenantID(context.Background(), 7)
	load := func() (*agentcontext.AgentConversationContext, *agentcontext.ConversationScope) {
		view, scope, err := provider(ctx, "telegram", "3", "conv-1", "group-1", "u1", "hub:13", true)
		if err != nil {
			t.Fatalf("构建线索 ContextView 失败: %v", err)
		}
		return view, scope
	}
	view, scope := load()
	view2, scope2 := load()
	if view == nil || scope == nil || view2 == nil || scope2 == nil {
		t.Fatal("provider 必须同时返回快照与作用域")
	}
	if scope.Platform != "telegram" || scope.ChannelAccountID != 3 || scope.GroupID != "group-1" ||
		scope.ConversationID != "conv-1" || scope.StablePeerID != "u1" || !scope.IsGroup {
		t.Fatalf("作用域必须来自权威字段: %+v", scope)
	}
	if view.Current.MessageID != "hub:13" || view.Current.SenderID != "u1" {
		t.Fatalf("当前消息必须绑定真实 hub 消息: %+v", view.Current)
	}
	for _, msg := range view.Recent {
		if msg.MessageID == "hub:12" {
			t.Fatalf("其他成员的私有历史不得进入线索上下文: %+v", view.Recent)
		}
	}
	identity, err := agentcontext.DeriveScopeIdentity(*scope)
	if err != nil {
		t.Fatal(err)
	}
	if scope.BusLLMSessionID != identity.BusLLMSessionID {
		t.Fatalf("必须绑定 canonical session: got=%q want=%q", scope.BusLLMSessionID, identity.BusLLMSessionID)
	}
	ref1, _ := intentDispatchCallContext(view, string(agentcontext.ExecutionScenarioLeadDetect), "detect")
	ref2, _ := intentDispatchCallContext(view2, string(agentcontext.ExecutionScenarioLeadDetect), "detect")
	if ref1 == "" || ref1 != ref2 || ref1 != identity.BusLLMSessionID {
		t.Fatalf("同一 scope 必须复用 canonical session: %q / %q want %q", ref1, ref2, identity.BusLLMSessionID)
	}
	_ = scope2
}
