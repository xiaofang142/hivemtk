package service

import (
	"testing"
	"time"

	agentcontext "hivemtk-user/internal/aiagent/context"
	"hivemtk-user/internal/model"
)

func TestFilterVisibleGroupMessagesByVisibility(t *testing.T) {
	sentAt := time.Date(2026, 11, 27, 10, 0, 0, 0, time.UTC)
	scope := agentcontext.ConversationScope{
		TenantID:         7,
		Platform:         "telegram",
		ChannelAccountID: 9,
		IsGroup:          true,
		GroupID:          "group-1",
	}
	memberships := []model.ConversationMembership{
		{
			TenantID:         scope.TenantID,
			Platform:         scope.Platform,
			ChannelAccountID: scope.ChannelAccountID,
			GroupID:          scope.GroupID,
			MemberID:         "member-1",
			EvidenceSource:   "channel_event",
			EvidenceRef:      "join-1",
		},
	}

	tests := []struct {
		name       string
		visibility agentcontext.Visibility
		audience   []string
		memberID   string
		want       bool
	}{
		{name: "public", visibility: agentcontext.VisibilityPublic, want: true},
		{name: "member_with_membership", visibility: agentcontext.VisibilityMember, want: true},
		{name: "member_without_membership", visibility: agentcontext.VisibilityMember, memberID: "member-2", want: false},
		{name: "member_with_explicit_audience", visibility: agentcontext.VisibilityMember, audience: []string{"member-2"}, memberID: "member-2", want: true},
		{name: "private_with_audience", visibility: agentcontext.VisibilityPrivate, audience: []string{"member-2"}, memberID: "member-2", want: true},
		{name: "private_without_audience", visibility: agentcontext.VisibilityPrivate, memberID: "member-1", want: false},
		{name: "unknown_never_grants_visibility", visibility: agentcontext.VisibilityUnknown, audience: []string{"member-1"}, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			memberID := tt.memberID
			if memberID == "" {
				memberID = "member-1"
			}
			messages := []agentcontext.ContextMessage{
				{
					MessageID:  "msg-1",
					SenderID:   "sender-1",
					SentAt:     sentAt,
					Visibility: tt.visibility,
					Audience:   append([]string{}, tt.audience...),
				},
			}

			result := FilterVisibleGroupMessages(messages, memberships, scope, memberID)
			if got := len(result.Visible); (got == 1) != tt.want {
				t.Fatalf("可见性结果不符合预期: want visible=%v, got visible=%d omitted=%+v", tt.want, got, result.Omitted)
			}
			if !tt.want && len(result.Omitted) != 1 {
				t.Fatalf("被过滤消息必须记录原因, got %+v", result.Omitted)
			}
		})
	}
}

func TestFilterVisibleGroupMessagesMembershipBoundaries(t *testing.T) {
	sentAt := time.Date(2026, 11, 27, 10, 0, 0, 0, time.UTC)
	scope := agentcontext.ConversationScope{
		TenantID:         7,
		Platform:         "telegram",
		ChannelAccountID: 9,
		IsGroup:          true,
		GroupID:          "group-1",
	}
	before := sentAt.Add(-time.Hour)
	after := sentAt.Add(time.Hour)

	tests := []struct {
		name        string
		joinedAt    *time.Time
		leftAt      *time.Time
		evidence    bool
		wantVisible bool
	}{
		{name: "joined_at_message_time", joinedAt: &sentAt, evidence: true, wantVisible: true},
		{name: "joined_after_message", joinedAt: &after, evidence: true, wantVisible: false},
		{name: "left_at_message_time", joinedAt: &before, leftAt: &sentAt, evidence: true, wantVisible: false},
		{name: "left_after_message", joinedAt: &before, leftAt: &after, evidence: true, wantVisible: true},
		{name: "unknown_join_is_proven_before_history", evidence: true, wantVisible: true},
		{name: "active_membership", joinedAt: &before, evidence: true, wantVisible: true},
		{name: "missing_evidence_is_not_proof", joinedAt: &before, evidence: false, wantVisible: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			membership := model.ConversationMembership{
				TenantID:         scope.TenantID,
				Platform:         scope.Platform,
				ChannelAccountID: scope.ChannelAccountID,
				GroupID:          scope.GroupID,
				MemberID:         "member-1",
				JoinedAt:         tt.joinedAt,
				LeftAt:           tt.leftAt,
			}
			if tt.evidence {
				membership.EvidenceSource = "channel_event"
				membership.EvidenceRef = "membership-event-1"
			}
			messages := []agentcontext.ContextMessage{
				{MessageID: "msg-1", SentAt: sentAt, Visibility: agentcontext.VisibilityMember},
			}

			result := FilterVisibleGroupMessages(messages, []model.ConversationMembership{membership}, scope, "member-1")
			if got := len(result.Visible) == 1; got != tt.wantVisible {
				t.Fatalf("成员区间可见性不符合预期: want=%v got=%v omitted=%+v", tt.wantVisible, got, result.Omitted)
			}
		})
	}
}

func TestFilterVisibleGroupMessagesCoversWholeVisibilityInterval(t *testing.T) {
	start := time.Date(2026, 11, 27, 10, 0, 0, 0, time.UTC)
	end := start.Add(time.Hour)
	joinedAfterStart := start.Add(time.Minute)
	leftBeforeEnd := end.Add(-time.Minute)
	scope := agentcontext.ConversationScope{
		TenantID:         7,
		Platform:         "telegram",
		ChannelAccountID: 9,
		IsGroup:          true,
		GroupID:          "group-1",
	}
	message := agentcontext.ContextMessage{
		MessageID:   "msg-interval",
		SentAt:      start,
		Visibility:  agentcontext.VisibilityMember,
		VisibleFrom: &start,
		VisibleTo:   &end,
	}

	tests := []struct {
		name     string
		joinedAt *time.Time
		leftAt   *time.Time
		want     bool
	}{
		{name: "full_interval_covered", joinedAt: &start, leftAt: &end, want: true},
		{name: "joined_during_interval", joinedAt: &joinedAfterStart, want: false},
		{name: "left_during_interval", leftAt: &leftBeforeEnd, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			membership := model.ConversationMembership{
				TenantID:         scope.TenantID,
				Platform:         scope.Platform,
				ChannelAccountID: scope.ChannelAccountID,
				GroupID:          scope.GroupID,
				MemberID:         "member-1",
				JoinedAt:         tt.joinedAt,
				LeftAt:           tt.leftAt,
				EvidenceSource:   "channel_event",
				EvidenceRef:      "membership-event-1",
			}
			result := FilterVisibleGroupMessages([]agentcontext.ContextMessage{message}, []model.ConversationMembership{membership}, scope, "member-1")
			if got := len(result.Visible) == 1; got != tt.want {
				t.Fatalf("整段可见性结果不符合预期: want=%v got=%v omitted=%+v", tt.want, got, result.Omitted)
			}
		})
	}
}

func TestFilterVisibleGroupMessagesFailsClosed(t *testing.T) {
	sentAt := time.Date(2026, 11, 27, 10, 0, 0, 0, time.UTC)
	validScope := agentcontext.ConversationScope{
		TenantID:         7,
		Platform:         "telegram",
		ChannelAccountID: 9,
		IsGroup:          true,
		GroupID:          "group-1",
	}
	messages := []agentcontext.ContextMessage{
		{MessageID: "msg-1", SentAt: sentAt, Visibility: agentcontext.VisibilityPublic},
	}

	tests := []struct {
		name     string
		scope    agentcontext.ConversationScope
		memberID string
		reason   string
	}{
		{name: "missing_tenant", scope: agentcontext.ConversationScope{Platform: "telegram", ChannelAccountID: 9, IsGroup: true, GroupID: "group-1"}, memberID: "member-1", reason: "invalid_scope"},
		{name: "missing_group", scope: agentcontext.ConversationScope{TenantID: 7, Platform: "telegram", ChannelAccountID: 9, IsGroup: true}, memberID: "member-1", reason: "invalid_scope"},
		{name: "dm_scope", scope: agentcontext.ConversationScope{TenantID: 7, Platform: "telegram", ChannelAccountID: 9}, memberID: "member-1", reason: "invalid_scope"},
		{name: "missing_member", scope: validScope, reason: "missing_member"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := FilterVisibleGroupMessages(messages, nil, tt.scope, tt.memberID)
			if len(result.Visible) != 0 || len(result.Omitted) != 1 {
				t.Fatalf("非法范围必须 fail-closed, got %+v", result)
			}
			if result.Omitted[0].Reason != tt.reason {
				t.Fatalf("过滤原因不符合预期: want=%q got=%q", tt.reason, result.Omitted[0].Reason)
			}
		})
	}
}

func TestFilterVisibleGroupMessagesIgnoresCrossScopeMembership(t *testing.T) {
	sentAt := time.Date(2026, 11, 27, 10, 0, 0, 0, time.UTC)
	scope := agentcontext.ConversationScope{
		TenantID:         7,
		Platform:         "telegram",
		ChannelAccountID: 9,
		IsGroup:          true,
		GroupID:          "group-1",
	}
	baseMembership := model.ConversationMembership{
		TenantID:         7,
		Platform:         "telegram",
		ChannelAccountID: 9,
		GroupID:          "group-1",
		MemberID:         "member-1",
		EvidenceSource:   "channel_event",
		EvidenceRef:      "membership-event-1",
	}
	tests := []struct {
		name   string
		mutate func(*model.ConversationMembership)
	}{
		{name: "tenant", mutate: func(m *model.ConversationMembership) { m.TenantID = 8 }},
		{name: "platform", mutate: func(m *model.ConversationMembership) { m.Platform = "wecom" }},
		{name: "account", mutate: func(m *model.ConversationMembership) { m.ChannelAccountID = 10 }},
		{name: "group", mutate: func(m *model.ConversationMembership) { m.GroupID = "group-2" }},
		{name: "member", mutate: func(m *model.ConversationMembership) { m.MemberID = "member-2" }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			membership := baseMembership
			tt.mutate(&membership)
			messages := []agentcontext.ContextMessage{
				{MessageID: "msg-1", SentAt: sentAt, Visibility: agentcontext.VisibilityMember},
			}
			result := FilterVisibleGroupMessages(messages, []model.ConversationMembership{membership}, scope, "member-1")
			if len(result.Visible) != 0 || len(result.Omitted) != 1 {
				t.Fatalf("跨范围成员证据不得授权, got %+v", result)
			}
		})
	}
}
