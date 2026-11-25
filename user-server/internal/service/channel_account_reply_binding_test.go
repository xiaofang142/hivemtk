package service

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/repository"
)

// LEAD-02 RED/GREEN：账号侧回复绑定是执行配置的唯一来源，校验错类型/失效/跨租户；
// 旧绑定先预览映射再收口为从账号侧投影，不能产生两套权威。

type replyAgentRepoStub struct {
	agents map[uint]*model.AIAgent
}

func (s *replyAgentRepoStub) GetByID(ctx context.Context, id uint) (*model.AIAgent, error) {
	if a := s.agents[id]; a != nil {
		copy := *a
		return &copy, nil
	}
	return nil, errors.New("not found")
}

type replyAccountRepoStub struct {
	accounts map[uint]*model.ChannelAccount
}

func (s *replyAccountRepoStub) Available() bool { return true }

func (s *replyAccountRepoStub) List(_ context.Context, _ repository.ChannelAccountListOptions) ([]model.ChannelAccount, error) {
	return nil, nil
}

func (s *replyAccountRepoStub) GetByID(ctx context.Context, tenantID, id uint) (*model.ChannelAccount, error) {
	a := s.accounts[id]
	if a == nil || a.TenantID != tenantID {
		return nil, errors.New("not found")
	}
	copy := *a
	return &copy, nil
}

func ptrUint(v uint) *uint { return &v }

func TestReplyAgentBinding_ValidateRejectsWrongTypeDisabledAndCrossTenant(t *testing.T) {
	ctx := context.Background()
	svc := NewReplyAgentBindingService(&replyAccountRepoStub{accounts: map[uint]*model.ChannelAccount{
		1: {ID: 1, TenantID: 7, Status: 1},
	}}, &replyAgentRepoStub{agents: map[uint]*model.AIAgent{
		2: {ID: 2, TenantID: 7, Status: 1, AgentType: "sales", LLMModel: "qwen", SystemPrompt: "sales prompt"},
		3: {ID: 3, TenantID: 7, Status: 0, AgentType: "sales"},
		4: {ID: 4, TenantID: 8, Status: 1, AgentType: "sales"},
		5: {ID: 5, TenantID: 7, Status: 1, AgentType: string(model.AgentTypeLeadMining)},
	}})
	agentID := uint(2)
	if _, err := svc.Resolve(ctx, 7, 1, &agentID); err != nil {
		t.Fatalf("合法绑定应通过: %v", err)
	}
	for _, tc := range []struct {
		name string
		id   uint
		want error
	}{
		{"disabled", 3, ErrReplyAgentBindingInvalid},
		{"cross-tenant", 4, ErrReplyAgentBindingInvalid},
		{"wrong-type-lead-mining", 5, ErrReplyAgentBindingWrongType},
		{"missing", 99, ErrReplyAgentBindingInvalid},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := svc.Resolve(ctx, 7, 1, ptrUint(tc.id)); !errors.Is(err, tc.want) {
				t.Fatalf("%s 期望 err=%v，实际 %v", tc.name, tc.want, err)
			}
		})
	}
}

func TestReplyAgentBinding_ResolveReflectsRealModelAndPrompt(t *testing.T) {
	ctx := context.Background()
	agents := &replyAgentRepoStub{agents: map[uint]*model.AIAgent{
		2: {ID: 2, TenantID: 7, Status: 1, AgentType: "sales", LLMModel: "model-A", SystemPrompt: "prompt-A"},
		3: {ID: 3, TenantID: 7, Status: 1, AgentType: "customer_service", LLMModel: "model-B", SystemPrompt: "prompt-B"},
	}}
	svc := NewReplyAgentBindingService(&replyAccountRepoStub{accounts: map[uint]*model.ChannelAccount{
		1: {ID: 1, TenantID: 7, Status: 1},
	}}, agents)
	first, err := svc.Resolve(ctx, 7, 1, ptrUint(2))
	if err != nil {
		t.Fatalf("首次解析失败: %v", err)
	}
	if first.AgentID != 2 || first.LLMModel != "model-A" || first.SystemPrompt != "prompt-A" {
		t.Fatalf("首次执行配置错误: %+v", first)
	}
	second, err := svc.Resolve(ctx, 7, 1, ptrUint(3))
	if err != nil {
		t.Fatalf("换绑解析失败: %v", err)
	}
	if second.AgentID != 3 || second.LLMModel != "model-B" || second.SystemPrompt != "prompt-B" {
		t.Fatalf("换绑后执行配置未变化: %+v", second)
	}
	if reflect.DeepEqual(first, second) {
		t.Fatal("换绑后必须捕获真实 Agent/模型/提示词变化")
	}
}

func TestLegacyReplyBinding_PreviewMapsAccountBindingAndConflicts(t *testing.T) {
	legacy := []model.ChannelAgentBinding{
		{ID: 10, ChannelType: "telegram", AccountID: "11", AgentID: 2, IsPrimary: replyBoolPtr(true), Enabled: replyBoolPtr(true)},
		{ID: 11, ChannelType: "telegram", AccountID: "11", ChatID: "chat-1", AgentID: 3, IsPrimary: replyBoolPtr(false), Enabled: replyBoolPtr(true)},
		{ID: 12, ChannelType: "telegram", AccountID: "11", ChatID: "chat-2", AgentID: 4, IsPrimary: replyBoolPtr(false), Enabled: replyBoolPtr(false)},
	}
	preview := PreviewLegacyReplyBindings(legacy, ptrUint(99))
	if len(preview) != 1 {
		t.Fatalf("预览只应列需迁移的启用主绑定，实际 %d 条: %+v", len(preview), preview)
	}
	if preview[0].LegacyBindingID != 10 || preview[0].AccountID != "11" || preview[0].AgentID != 2 {
		t.Fatalf("预览映射错误: %+v", preview[0])
	}
	if !preview[0].ConflictsWithAccountBinding {
		t.Fatal("账号侧已绑定不同 Agent 时必须标记冲突，不能产生两套权威")
	}
	redirect := PreviewLegacyReplyBindings(legacy, ptrUint(2))
	if len(redirect) != 1 || !redirect[0].RedirectToAccountBinding || redirect[0].ConflictsWithAccountBinding {
		t.Fatalf("账号侧绑定同 Agent 应转为转向而非冲突: %+v", redirect)
	}
	// 关闭/辅绑定不迁移；冲突需人工确认后才转向账号侧。
	none := PreviewLegacyReplyBindings([]model.ChannelAgentBinding{
		{ID: 12, ChannelType: "telegram", AccountID: "11", AgentID: 4, IsPrimary: replyBoolPtr(true), Enabled: replyBoolPtr(false)},
	}, nil)
	if len(none) != 0 {
		t.Fatalf("停用旧绑定不应进入迁移预览，实际 %+v", none)
	}
	if !strings.Contains(LegacyReplyBindingNotice(), "账号") {
		t.Fatal("旧入口提示必须明确账号侧为唯一权威")
	}
}

func replyBoolPtr(v bool) *bool { return &v }

// ReplyBindingAgentContext 捕获一次真实执行所选 Agent/模型/提示词，供模型请求消费。
func TestReplyBindingAgentContext_ExecutionProof(t *testing.T) {
	ac := NewReplyAgentBindingService(nil, nil).ExecutionContext(&model.AIAgent{
		ID: 9, AgentType: "sales", LLMModel: "proof-model", SystemPrompt: "proof-prompt",
	})
	if ac.AgentID != 9 || ac.LLMModel != "proof-model" || ac.SystemPrompt != "proof-prompt" {
		t.Fatalf("执行证明未捕获真实模型/提示词: %+v", ac)
	}
}
