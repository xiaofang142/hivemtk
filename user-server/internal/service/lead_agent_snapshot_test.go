package service

import (
	"context"
	"strings"
	"testing"

	"hivemtk-user/internal/model"
)

// FR-026 回归：账号级线索发现智能体的配置必须真正生效，
// 而不是停在 ai_agents 表里成为死配置。

type lmAgentReaderStub struct {
	agent *model.AIAgent
	err   error
}

func (s *lmAgentReaderStub) Get(context.Context, uint) (*model.AIAgent, error) {
	return s.agent, s.err
}

func TestLeadMiningConfigResolver_AgentConfigIsLive(t *testing.T) {
	ctx := context.Background()
	r := NewLeadMiningConfigResolverWithDeps(&lmRuleRepoStub{rows: map[string]*model.LeadMiningRule{
		"account": {Scope: "account", Enabled: true, SystemDescription: "rule"},
	}}, &lmConfigRepoStub{cfg: &model.LeadMiningConfig{Enabled: true, MinIntentScore: 50, Model: "legacy-model"}})
	r.SetAgentReader(&lmAgentReaderStub{agent: &model.AIAgent{
		ID: 51, AgentType: string(model.AgentTypeLeadMining), AgentMode: "active", Status: 1,
		Persona: "四川同咨微信线索挖掘助手", SystemPrompt: "只识别成都本地高意向客户并归档",
		LLMModel: "qwen-plus",
	}})

	snap, err := r.Resolve(ctx, LeadRuleTarget{ChannelAccountID: 10}, 51)
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if snap.LeadAgentID != 51 || snap.LeadAgentMode != "active" {
		t.Fatalf("智能体绑定未进入快照: %+v", snap)
	}
	if snap.LeadAgentPersona != "四川同咨微信线索挖掘助手" || snap.LeadAgentPrompt != "只识别成都本地高意向客户并归档" {
		t.Fatalf("智能体人设/系统提示未进入快照: %+v", snap)
	}
	if snap.Model != "qwen-plus" {
		t.Fatalf("智能体模型未覆盖 legacy 模型: %+v", snap)
	}

	// 不传 agentID 时不读取智能体，存量租户行为不变。
	plain, err := r.Resolve(ctx, LeadRuleTarget{ChannelAccountID: 10})
	if err != nil || plain.LeadAgentID != 0 || plain.Model != "legacy-model" {
		t.Fatalf("未绑定智能体时应保持 legacy: %+v err=%v", plain, err)
	}

	// 停用或类型不符必须 fail-closed，不能让线索腿静默放行。
	r.SetAgentReader(&lmAgentReaderStub{agent: &model.AIAgent{ID: 51, AgentType: string(model.AgentTypeLeadMining), Status: 0}})
	disabled, err := r.Resolve(ctx, LeadRuleTarget{ChannelAccountID: 10}, 51)
	if err != nil || disabled.Enabled {
		t.Fatalf("停用智能体必须禁用发掘: %+v err=%v", disabled, err)
	}
}

func TestLeadJudgePromptIncludesAgentConfig(t *testing.T) {
	jctx := &LeadJudgeContext{
		AgentSnapshot: &LeadMiningConfigSnapshot{
			LeadAgentID:      51,
			LeadAgentPersona: "四川同咨微信线索挖掘助手",
			LeadAgentPrompt:  "只识别成都本地高意向客户并归档",
		},
	}
	prompt := buildLeadJudgeSystemPrompt(jctx)
	for _, want := range []string{"四川同咨微信线索挖掘助手", "只识别成都本地高意向客户并归档"} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("判定提示缺失智能体配置 %q:\n%s", want, prompt)
		}
	}

	// 无智能体快照时不得输出该段。
	plain := buildLeadJudgeSystemPrompt(&LeadJudgeContext{})
	if strings.Contains(plain, "线索发现智能体人设") || strings.Contains(plain, "线索发现智能体系统提示") {
		t.Fatal("未绑定智能体时提示不应出现智能体段")
	}
}
