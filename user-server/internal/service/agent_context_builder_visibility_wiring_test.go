package service

import (
	"context"
	"testing"
	"time"

	agentcontext "hivemtk-user/internal/aiagent/context"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tenant"
)

func TestConversationContextBuilderUsesGroupVisibilityFilter(t *testing.T) {
	base := time.Date(2026, 11, 27, 10, 0, 0, 0, time.UTC)
	scope := agentcontext.ConversationScope{
		TenantID: 7, Platform: "telegram", ChannelAccountID: 3,
		IsGroup: true, GroupID: "group-1", ConversationID: "thread-9",
	}
	rows := []UnifiedMessage{
		builderTestMessage("hub:1", "u2", "公开群消息", base.Add(-4*time.Minute), agentcontext.VisibilityPublic),
		builderTestMessage("hub:2", "u2", "成员可见但无显式 audience", base.Add(-3*time.Minute), agentcontext.VisibilityMember),
		builderTestMessage("hub:3", "u2", "仅对另一成员可见", base.Add(-2*time.Minute), agentcontext.VisibilityMember),
		builderTestMessage("hub:4", "u2", "群 audience 可见", base.Add(-time.Minute), agentcontext.VisibilityMember),
		builderTestMessage("hub:5", "u2", "未知可见性", base.Add(-30*time.Second), agentcontext.VisibilityUnknown),
	}
	rows[2].Audience = []string{"u2"}
	rows[3].Audience = []string{"group-1"}

	before := base.Add(-time.Hour)
	membership := model.ConversationMembership{
		TenantID: scope.TenantID, Platform: scope.Platform, ChannelAccountID: scope.ChannelAccountID,
		GroupID: scope.GroupID, MemberID: "u1", JoinedAt: &before,
		EvidenceSource: "channel_event", EvidenceRef: "join-1",
	}
	input := AgentContextBuilderInput{
		Scope: scope,
		Current: agentcontext.ContextMessage{
			MessageID: "hub:9", SenderID: "u1", Content: "在吗", SentAt: base,
			Visibility: agentcontext.VisibilityPublic,
		},
		Memberships: []model.ConversationMembership{membership},
		Now:         base,
	}

	withMembership, err := NewConversationContextBuilder(&fakeAgentContextReader{messages: rows}).Build(
		tenant.WithTenantID(context.Background(), 7), input)
	if err != nil {
		t.Fatalf("带成员关系的上下文构建失败: %v", err)
	}
	wantIDs := []string{"hub:1", "hub:2", "hub:4"}
	if len(withMembership.Recent) != len(wantIDs) {
		t.Fatalf("群历史过滤结果错误: %+v", withMembership.Recent)
	}
	for i, id := range wantIDs {
		if withMembership.Recent[i].MessageID != id {
			t.Fatalf("群历史顺序/内容错误: got %q want %q (all=%+v)", withMembership.Recent[i].MessageID, id, withMembership.Recent)
		}
	}

	input.Memberships = nil
	withoutMembership, err := NewConversationContextBuilder(&fakeAgentContextReader{messages: rows}).Build(
		tenant.WithTenantID(context.Background(), 7), input)
	if err != nil {
		t.Fatalf("无成员关系的上下文构建失败: %v", err)
	}
	wantIDs = []string{"hub:1", "hub:4"}
	if len(withoutMembership.Recent) != len(wantIDs) {
		t.Fatalf("缺 memberships 时 member 可见性只能靠显式 audience: %+v", withoutMembership.Recent)
	}
	for i, id := range wantIDs {
		if withoutMembership.Recent[i].MessageID != id {
			t.Fatalf("群 audience 可见性错误: got %q want %q (all=%+v)", withoutMembership.Recent[i].MessageID, id, withoutMembership.Recent)
		}
	}
}

func TestFilterVisibleGroupMessagesGroupAudienceWithoutMemberships(t *testing.T) {
	scope := agentcontext.ConversationScope{
		TenantID: 7, Platform: "telegram", ChannelAccountID: 3,
		IsGroup: true, GroupID: "group-1",
	}
	messages := []agentcontext.ContextMessage{
		{MessageID: "g-1", Visibility: agentcontext.VisibilityMember, Audience: []string{"group-1"}},
		{MessageID: "g-2", Visibility: agentcontext.VisibilityPrivate, Audience: []string{"group-1"}},
		{MessageID: "g-3", Visibility: agentcontext.VisibilityMember, Audience: []string{"other-member"}},
	}
	result := FilterVisibleGroupMessages(messages, nil, scope, "u1")
	if len(result.Visible) != 2 || result.Visible[0].MessageID != "g-1" || result.Visible[1].MessageID != "g-2" {
		t.Fatalf("群 audience 应对当前成员可见: %+v omitted=%+v", result.Visible, result.Omitted)
	}
	if len(result.Omitted) != 1 || result.Omitted[0].MessageID != "g-3" {
		t.Fatalf("缺 memberships 且无显式授权时必须 fail-closed: %+v", result.Omitted)
	}
}
