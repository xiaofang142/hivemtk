package service

import (
	"context"
	"strings"
	"testing"

	agentcontext "hivemtk-user/internal/aiagent/context"
	"hivemtk-user/internal/model"
)

func groupGateView(proof string, privateFacts ...agentcontext.BusinessFact) *agentcontext.AgentConversationContext {
	view := &agentcontext.AgentConversationContext{
		Scope: agentcontext.ConversationScope{IsGroup: true, GroupID: "group-1"},
		Current: agentcontext.ContextMessage{
			MessageID: "m-cur", SenderID: "member-2", Visibility: agentcontext.VisibilityPublic,
		},
		Provenance: agentcontext.ContextProvenance{VisibilityProof: proof},
	}
	if len(privateFacts) > 0 {
		view.Customer = &agentcontext.CustomerContext{
			SubjectType: "group", SubjectID: "group-1", Facts: privateFacts,
		}
	}
	return view
}

func TestCheckGroupReplyGate(t *testing.T) {
	otherPrivate := agentcontext.BusinessFact{
		Key: "phone", Value: "13800138000", Grade: agentcontext.FactConfirmed,
		SubjectType: "group_member", SubjectID: "member-1",
	}
	cases := []struct {
		name       string
		isGroup    bool
		view       *agentcontext.AgentConversationContext
		reply      string
		allowed    bool
		wantReason string
	}{
		{
			name: "dm_is_not_gated", reply: "请联系 13800138000", allowed: true,
		},
		{
			name: "group_without_view_fails_closed", isGroup: true, reply: "公开回答",
			wantReason: GroupReplyReasonContextUnverified,
		},
		{
			name: "group_with_unproven_visibility_fails_closed", isGroup: true,
			view: groupGateView("visibility_unproven"), reply: "公开回答",
			wantReason: GroupReplyReasonContextUnverified,
		},
		{
			name: "group_verified_public_reply_allowed", isGroup: true,
			view: groupGateView("group_audienceVerified"), reply: "这款产品支持批量导出。", allowed: true,
		},
		{
			name: "group_verified_reply_with_other_member_pii_blocked", isGroup: true,
			view: groupGateView("group_audienceVerified", otherPrivate), reply: "请打 13800138000 咨询。",
			wantReason: PrivacyReasonPhone,
		},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			decision := CheckGroupReplyGate(tt.isGroup, tt.view, "member-2", tt.reply)
			if decision.Allowed != tt.allowed {
				t.Fatalf("群回复门禁结果错误: allowed=%v reasons=%v", decision.Allowed, decision.Reasons)
			}
			if tt.wantReason == "" {
				return
			}
			found := false
			for _, reason := range decision.Reasons {
				if reason == tt.wantReason {
					found = true
				}
			}
			if !found {
				t.Fatalf("缺少原因 %q: %+v", tt.wantReason, decision)
			}
			if strings.Contains(strings.Join(decision.Reasons, "|"), tt.reply) {
				t.Fatal("门禁原因不得回显回复原文")
			}
		})
	}
}

func TestDeliverAIResult_GroupPrivacyGateSkipsOutbound(t *testing.T) {
	ws := &WebhookService{}
	ctx := context.Background()
	hub := &model.MessageHub{ConversationID: "group-conv", IsGroup: true}
	in := &IncomingContext{
		IsGroup: true, SenderID: "member-2",
		ContextView: groupGateView("group_audienceVerified", agentcontext.BusinessFact{
			Key: "phone", Value: "13800138000", Grade: agentcontext.FactConfirmed,
			SubjectType: "group_member", SubjectID: "member-1",
		}),
	}
	result := &HandleResult{SessionID: "group-session", AIReplied: true, Reply: "请打 13800138000"}
	// ingressSvc 故意为 nil：若门禁未拦截，发送前锁复核会 panic，测试即失败。
	ws.deliverAIResult(ctx, webhookReplyRequest{
		channel: ChannelDouyin, accountID: "acc", payload: &ParsedPayload{EventID: "group-privacy-1"},
		hub: hub, incoming: in,
	}, result, nil)
	if !result.GroupReplyBlocked {
		t.Fatalf("群隐私门禁结果应回写到 HandleResult: %+v", result)
	}
	if len(result.GroupReplyReasons) == 0 {
		t.Fatal("群隐私门禁应记录机器可读原因")
	}
}

func TestDeliverAIResult_GroupReplyBlockedFlagSkipsOutbound(t *testing.T) {
	ws := &WebhookService{}
	hub := &model.MessageHub{ConversationID: "group-conv", IsGroup: true}
	result := &HandleResult{
		SessionID: "group-session", AIReplied: true, Reply: "任意回复",
		GroupReplyBlocked: true, GroupReplyReasons: []string{GroupReplyReasonContextUnverified},
	}
	ws.deliverAIResult(context.Background(), webhookReplyRequest{
		channel: ChannelDouyin, accountID: "acc", payload: &ParsedPayload{EventID: "group-privacy-2"}, hub: hub,
	}, result, nil)
}
