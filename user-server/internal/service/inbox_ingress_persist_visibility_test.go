package service

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tenant"
)

func TestHubIngressVisibilityFromEvent(t *testing.T) {
	tests := []struct {
		name       string
		event      *model.MessageEvent
		wantVis    string
		wantAud    []string
		wantSource string
	}{
		{
			name: "group_uses_group_id_audience",
			event: &model.MessageEvent{
				IsGroup: true, GroupID: "g-1", ConversationID: "thread-9",
				EventID: "evt-1", BusLLMSessionID: "llmsess_abc",
			},
			wantVis:    model.VisibilityMember,
			wantAud:    []string{"g-1"},
			wantSource: "channel_ingress",
		},
		{
			name: "group_without_group_id_fails_closed",
			event: &model.MessageEvent{
				IsGroup: true, GroupID: "", ConversationID: "thread-9", EventID: "evt-2",
			},
			wantVis:    model.VisibilityUnknown,
			wantAud:    []string{},
			wantSource: "",
		},
		{
			name: "dm_uses_public_with_stable_sender_audience",
			event: &model.MessageEvent{
				IsGroup: false, ConversationID: "conv-1", SenderID: "u-1", EventID: "evt-3",
			},
			wantVis:    model.VisibilityPublic,
			wantAud:    []string{"u-1"},
			wantSource: "channel_ingress",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := hubIngressVisibilityFromEvent(tt.event)
			if got.Visibility != tt.wantVis {
				t.Fatalf("可见性错误: got %q want %q", got.Visibility, tt.wantVis)
			}
			if !reflect.DeepEqual(got.Audience, tt.wantAud) {
				t.Fatalf("audience 错误: got %v want %v", got.Audience, tt.wantAud)
			}
			if tt.wantSource == "" {
				if len(got.Evidence) != 0 {
					t.Fatalf("不可证明时不得写 evidence: %v", got.Evidence)
				}
			} else if got.Evidence == nil || got.Evidence["source"] != tt.wantSource {
				t.Fatalf("evidence 错误: got %v want source %q", got.Evidence, tt.wantSource)
			}
		})
	}
}

func TestHubIngressAuthorityExtraFromEvent(t *testing.T) {
	ctx := tenant.WithTenantID(context.Background(), 7)
	extra := hubIngressAuthorityExtraFromEvent(ctx, "10", &model.MessageEvent{
		EventID: "evt-9", BusLLMSessionID: "llmsess_9",
	})
	if got := extra["llm_session_id"]; got != "llmsess_9" {
		t.Fatalf("总线 session 未写入权威 extra: %v", got)
	}
	if got := extra["correlation_id"]; got != "evt-9" {
		t.Fatalf("真实消息 ID 未写入 correlation: %v", got)
	}

	derived := hubIngressAuthorityExtraFromEvent(ctx, "10", &model.MessageEvent{
		EventID: "evt-10", Channel: "personal_wx", IsGroup: true, GroupID: "g-1", ConversationID: "thread-9",
	})
	if got, ok := derived["llm_session_id"].(string); !ok || !strings.HasPrefix(got, "llmsess_") {
		t.Fatalf("总线缺失时服务端必须确定性派生 session: %v", derived["llm_session_id"])
	}

	empty := hubIngressAuthorityExtraFromEvent(ctx, "", &model.MessageEvent{})
	if len(empty) != 0 {
		t.Fatalf("缺少权威值时应返回空 extra: %v", empty)
	}
}

func TestHubIngressBusLLMSessionIDDerivationIsStable(t *testing.T) {
	ctx := tenant.WithTenantID(context.Background(), 7)
	base := &model.MessageEvent{Channel: "personal_wx", IsGroup: true, GroupID: "g-1", ConversationID: "thread-1", SenderID: "u-1"}
	a := hubIngressBusLLMSessionID(ctx, "10", base)
	b := hubIngressBusLLMSessionID(ctx, "10", &model.MessageEvent{Channel: "personal_wx", IsGroup: true, GroupID: "g-1", ConversationID: "thread-2", SenderID: "u-2"})
	if a == "" || a != b {
		t.Fatalf("同群不同线程/发言人必须复用同一派生 session: %q vs %q", a, b)
	}
	c := hubIngressBusLLMSessionID(ctx, "10", &model.MessageEvent{Channel: "personal_wx", IsGroup: true, GroupID: "g-2", ConversationID: "thread-1"})
	if a == c {
		t.Fatalf("跨群不得复用 session: %q", a)
	}
	if got := hubIngressBusLLMSessionID(ctx, "10", &model.MessageEvent{BusLLMSessionID: "bus-provided", IsGroup: true, GroupID: "g-1"}); got != "bus-provided" {
		t.Fatalf("总线权威值必须原样优先: %q", got)
	}
}
