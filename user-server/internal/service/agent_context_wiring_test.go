package service

import (
	"context"
	"strings"
	"testing"
	"time"

	agentcontext "hivemtk-user/internal/aiagent/context"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tenant"
)

func TestBuildIncomingContextViewUsesAuthoritativeMetadata(t *testing.T) {
	ctx := tenant.WithTenantID(context.Background(), 7)
	reader := &wiringContextReader{rows: []UnifiedMessage{{
		ContextMessage: agentcontext.ContextMessage{
			MessageID: "hub:11", Role: "assistant", Direction: "outbound",
			SenderID: "bot", Content: "上次说的是报价方案", SentAt: time.Unix(100, 0),
			Visibility: agentcontext.VisibilityPublic,
		},
	}}}
	builder := NewConversationContextBuilder(reader)
	in := &IncomingContext{
		Platform: model.PlatformDouyin, AccountID: "42", SenderID: " buyer-1",
		SenderName: "张三", Content: "那个方案再说说", MessageID: "hub:12",
		ConversationID: "conv-1", BusLLMSessionID: "llmsess_bus_1",
		IsGroup: false, OriginalSentAt: time.Unix(200, 0),
	}
	view, err := buildIncomingContextView(ctx, in, "session-1", builder, time.Unix(300, 0))
	if err != nil {
		t.Fatalf("构建上下文视图失败: %v", err)
	}
	if view == nil {
		t.Fatal("上下文视图不应为空")
	}
	if view.Scope.TenantID != 7 || view.Scope.ChannelAccountID != 42 || view.Scope.ConversationID != "conv-1" {
		t.Fatalf("scope 未采用权威元数据: %+v", view.Scope)
	}
	if view.Scope.BusLLMSessionID != "llmsess_bus_1" {
		t.Fatalf("总线 session key 丢失: %q", view.Scope.BusLLMSessionID)
	}
	if len(view.Recent) != 1 || view.Recent[0].MessageID != "hub:11" {
		t.Fatalf("历史未进入上下文: %+v", view.Recent)
	}
	if reader.lastQuery.AccountIDs[0] != "42" || reader.lastQuery.ConversationID != "conv-1" || reader.lastQuery.TenantID != 7 {
		t.Fatalf("历史查询范围错误: %+v", reader.lastQuery)
	}
}

func TestBuildIncomingContextViewGroupUsesGroupRootAndAudienceProof(t *testing.T) {
	ctx := tenant.WithTenantID(context.Background(), 7)
	reader := &wiringContextReader{}
	builder := NewConversationContextBuilder(reader)
	in := &IncomingContext{
		Platform: model.Platform("telegram"), AccountID: "42", SenderID: "member-1",
		Content: "在群里问一下", MessageID: "hub:21", IsGroup: true, GroupID: "group-1",
		ConversationID: "thread-9", OriginalSentAt: time.Unix(200, 0),
	}
	view, err := buildIncomingContextView(ctx, in, "session-2", builder, time.Unix(300, 0))
	if err != nil {
		t.Fatalf("构建群上下文失败: %v", err)
	}
	if view == nil {
		t.Fatal("群上下文视图不应为空")
	}
	if reader.lastQuery.GroupID != "group-1" || reader.lastQuery.ConversationID != "" {
		t.Fatalf("群历史必须以 group_id 为根: %+v", reader.lastQuery)
	}
	if !strings.Contains(view.Provenance.VisibilityProof, "group_audienceVerified") {
		t.Fatalf("群上下文缺少 audience 证明: %q", view.Provenance.VisibilityProof)
	}
	if decision := CheckGroupReplyGate(true, view, in.SenderID, "公开群回复"); !decision.Allowed {
		t.Fatalf("装配后的群上下文应通过输出硬门: %+v", decision)
	}
}

func TestBuildIncomingContextViewNilBuilderIsNoop(t *testing.T) {
	view, err := buildIncomingContextView(
		tenant.WithTenantID(context.Background(), 7),
		&IncomingContext{Platform: model.PlatformDouyin, AccountID: "42", SenderID: "u", MessageID: "m"},
		"s", nil, time.Now(),
	)
	if err != nil || view != nil {
		t.Fatalf("未装配构建器时应安全降级: view=%v err=%v", view, err)
	}
}

type wiringContextReader struct {
	rows      []UnifiedMessage
	lastQuery UnifiedMessageQuery
}

func (r *wiringContextReader) List(_ context.Context, query UnifiedMessageQuery) ([]UnifiedMessage, error) {
	r.lastQuery = query
	return r.rows, nil
}
