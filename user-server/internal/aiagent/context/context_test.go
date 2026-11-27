package context

import (
	"testing"
	"time"
)

func TestContextSchemaVersionStable(t *testing.T) {
	if ContextSchemaVersion != "agent-context-v1" {
		t.Fatalf("上下文 schema 版本不稳定：got %q", ContextSchemaVersion)
	}
}

func TestFactGradeCanStateAsFact(t *testing.T) {
	tests := []struct {
		name  string
		grade FactGrade
		want  bool
	}{
		{name: "confirmed", grade: FactConfirmed, want: true},
		{name: "inferred", grade: FactInferred, want: false},
		{name: "derived_summary", grade: FactDerivedSummary, want: false},
		{name: "suggestion", grade: FactSuggestion, want: false},
		{name: "external_untrusted", grade: FactExternalUntrusted, want: false},
		{name: "legacy_unverified", grade: FactLegacyUnverified, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.grade.CanStateAsFact(); got != tt.want {
				t.Fatalf("FactGrade(%q).CanStateAsFact() = %v, want %v", tt.grade, got, tt.want)
			}
		})
	}
}

func TestVisibilityIsProven(t *testing.T) {
	tests := []struct {
		name       string
		visibility Visibility
		want       bool
	}{
		{name: "public", visibility: VisibilityPublic, want: true},
		{name: "member", visibility: VisibilityMember, want: true},
		{name: "private", visibility: VisibilityPrivate, want: true},
		{name: "unknown", visibility: VisibilityUnknown, want: false},
		{name: "empty", visibility: Visibility(""), want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.visibility.IsProven(); got != tt.want {
				t.Fatalf("Visibility(%q).IsProven() = %v, want %v", tt.visibility, got, tt.want)
			}
		})
	}
}

func TestSourcePolicyValues(t *testing.T) {
	tests := []struct {
		name   string
		policy SourcePolicyKind
		want   string
	}{
		{name: "dm", policy: SourcePolicyDM, want: "dm"},
		{name: "group", policy: SourcePolicyGroup, want: "group"},
		{name: "unproven", policy: SourcePolicyUnproven, want: "unproven"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := string(tt.policy); got != tt.want {
				t.Fatalf("SourcePolicyKind = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestContextContractMessageFields(t *testing.T) {
	now := time.Date(2026, time.November, 27, 10, 0, 0, 0, time.UTC)
	visibleFrom := now.Add(-time.Hour)
	visibleTo := now.Add(time.Hour)
	msg := ContextMessage{
		MessageID:      "msg-1",
		Origin:         OriginMessageHub,
		CorrelationID:  "corr-1",
		HistorySource:  HistorySourceMessageHub,
		SourceMetadata: map[string]string{"platform_msg_id": "pm-1"},
		Role:           "user",
		Direction:      "inbound",
		SenderID:       "sender-1",
		SenderName:     "Alice",
		MemberRole:     "demand",
		Content:        "什么时候交付",
		MediaSummary:   "",
		SentAt:         now,
		ReplyTo:        "msg-0",
		Mentions:       []string{"agent-1"},
		Visibility:     VisibilityMember,
		Audience:       []string{"sender-1", "agent-1"},
		VisibleFrom:    &visibleFrom,
		VisibleTo:      &visibleTo,
	}

	if msg.MessageID != "msg-1" || msg.SenderID != "sender-1" {
		t.Fatalf("消息契约字段丢失：%+v", msg)
	}
	if len(msg.Audience) != 2 || len(msg.Mentions) != 1 {
		t.Fatalf("消息 audience/mentions 字段丢失：%+v", msg)
	}
	if msg.VisibleFrom == nil || msg.VisibleTo == nil {
		t.Fatalf("成员可见区间字段丢失：%+v", msg)
	}
}
