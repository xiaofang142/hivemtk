package humanize

import (
	"strings"
	"testing"
	"time"

	agentcontext "hivemtk-user/internal/aiagent/context"
	"hivemtk-user/internal/dto"
)

func humanizeContextTestView() *agentcontext.AgentConversationContext {
	now := time.Date(2026, 11, 27, 10, 0, 0, 0, time.UTC)
	return &agentcontext.AgentConversationContext{
		SnapshotID: "snap-1",
		Scope: agentcontext.ConversationScope{
			TenantID:         7,
			Platform:         "wecom",
			ChannelAccountID: 11,
			IsGroup:          true,
			GroupID:          "group-1",
			BusLLMSessionID:  "llmsess_abc",
		},
		Current: agentcontext.ContextMessage{
			MessageID:  "hub:3",
			Role:       "user",
			SenderID:   "u-1",
			Content:    "这个还来得及吗",
			SentAt:     now,
			Visibility: agentcontext.VisibilityPublic,
		},
		Recent: []agentcontext.ContextMessage{
			{
				MessageID:  "hub:1",
				Role:       "user",
				SenderID:   "u-1",
				Content:    "之前说预算是 5000",
				SentAt:     now.Add(-2 * time.Minute),
				Visibility: agentcontext.VisibilityPublic,
			},
			{
				MessageID:  "hub:2",
				Role:       "user",
				SenderID:   "u-2",
				Content:    "他的私人手机号是 13800138000",
				SentAt:     now.Add(-time.Minute),
				Visibility: agentcontext.VisibilityPrivate,
				Audience:   []string{"u-2"},
			},
		},
		SharedBusiness: []agentcontext.BusinessFact{
			{Key: "用途", Value: "客户已确认用途是送礼", Grade: agentcontext.FactConfirmed, SubjectType: "dm_person", SubjectID: "u-1"},
			{Key: "报价", Value: "建议报低价", Grade: agentcontext.FactSuggestion, SubjectType: "dm_person", SubjectID: "u-1"},
		},
	}
}

func TestBuildHumanizeContextBlockUsesOnlyVisibleConfirmedFacts(t *testing.T) {
	block := BuildHumanizeContextBlock(humanizeContextTestView())
	if !strings.Contains(block, "群聊") {
		t.Fatalf("群聊来源策略应进入上下文块: %q", block)
	}
	if !strings.Contains(block, "之前说预算是 5000") {
		t.Fatalf("可见历史应进入上下文块: %q", block)
	}
	if !strings.Contains(block, "客户已确认用途是送礼") {
		t.Fatalf("confirmed 事实应进入上下文块: %q", block)
	}
	for _, forbidden := range []string{"建议报低价", "他的私人手机号是 13800138000", "13800138000"} {
		if strings.Contains(block, forbidden) {
			t.Fatalf("不可证明/未确认内容不得进入上下文块: %s, block=%q", forbidden, block)
		}
	}
}

func TestBuildHumanizeLLMPromptIncludesContextBlock(t *testing.T) {
	input := &dto.HumanizeEvalInput{
		CustomerMessage: "这个还来得及吗",
		AIReply:         "可以的",
		ContextView:     humanizeContextTestView(),
	}
	prompt := buildHumanizeLLMPrompt(input, nil)
	if !strings.Contains(prompt, "之前说预算是 5000") || !strings.Contains(prompt, "客户已确认用途是送礼") {
		t.Fatalf("评估 prompt 必须带多轮上下文: %q", prompt)
	}
	if strings.Contains(prompt, "建议报低价") {
		t.Fatalf("评估 prompt 不得复述建议为事实: %q", prompt)
	}
}
