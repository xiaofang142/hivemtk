package service

import (
	"context"
	"strings"
	"testing"
	"time"

	"hivemtk-user/internal/model"
)

// LEAD-04：业务备注带来源/时间/事实或建议属性；双方模型输入必须真的带上对方刚写的信息，
// 且建议不得被当成已发生事实。日志不算消费证据。

func TestBusinessNoteEncodingRoundTrip(t *testing.T) {
	note := BusinessNote{Source: ContextNoteNamespaceAutoReply, At: 1700000000, Attribute: BusinessNoteSuggestion, Text: "建议转人工"}
	tag := EncodeBusinessNote(note)
	got, ok := DecodeBusinessNote(tag)
	if !ok {
		t.Fatalf("备注编码无法解码: %q", tag)
	}
	if got.Source != note.Source || got.At != note.At || got.Attribute != note.Attribute || got.Text != note.Text {
		t.Fatalf("备注属性丢失: got=%+v want=%+v", got, note)
	}
	// 存量裸备注必须兼容为事实属性。
	legacy, ok := DecodeBusinessNote("note:lead_mining:线索已介入")
	if !ok || legacy.Attribute != BusinessNoteFact || legacy.Text != "线索已介入" {
		t.Fatalf("存量备注兼容失败: got=%+v ok=%t", legacy, ok)
	}
	// 对方刚写的信息必须进入提示，且建议标注为建议。
	input := SharedBusinessInput{Notes: []BusinessNote{
		{Source: ContextNoteNamespaceAutoReply, At: 2, Attribute: BusinessNoteSuggestion, Text: "建议转人工"},
		{Source: ContextNoteNamespaceLeadMining, At: 3, Attribute: BusinessNoteFact, Text: "判定为需求线索"},
	}, Tags: []string{"大客户"}, Summary: "早期讨论预算"}
	block := input.PromptBlock()
	for _, want := range []string{"建议转人工", "判定为需求线索", "大客户", "早期讨论预算", "非已发生事实"} {
		if !strings.Contains(block, want) {
			t.Fatalf("共享业务提示缺少 %q: %s", want, block)
		}
	}
}

func TestSharedBusinessInput_BudgetAndOwnNamespace(t *testing.T) {
	conv := &model.ConversationContext{
		Tags: model.JSONStrings{
			"note:auto_reply:suggestion|10|自动回复刚建议转人工",
			"note:auto_reply:fact|11|对方刚写的事实备注",
			"note:lead_mining:fact|12|自己写的备注",
			"高意向",
		},
		Summary: "双方已讨论价格",
	}
	got := SharedBusinessInputFromContext(conv, ContextNoteNamespaceLeadMining, 200)
	if len(got.Notes) != 2 {
		t.Fatalf("只读对方备注，期望 2 条，实际 %+v", got.Notes)
	}
	if len(got.Tags) != 1 || got.Tags[0] != "高意向" {
		t.Fatalf("业务标签应保留、备注标签应剔除，实际 %v", got.Tags)
	}
	if !strings.Contains(got.PromptBlock(), "对方刚写的事实备注") {
		t.Fatal("对方刚写的信息未进入提示")
	}
	if strings.Contains(got.PromptBlock(), "自己写的备注") {
		t.Fatal("自己的备注不应被当作对方信息消费")
	}
	// 预算必须截断长文本，不能无限放大模型输入。
	long := &model.ConversationContext{Tags: model.JSONStrings{"note:auto_reply:fact|1|" + strings.Repeat("长", 1000)}}
	short := SharedBusinessInputFromContext(long, ContextNoteNamespaceLeadMining, 40)
	if len(short.PromptBlock()) > 200 {
		t.Fatalf("长度预算未生效: %d", len(short.PromptBlock()))
	}
}

// 线索侧：JudgeWithContext 构造 DispatchRequest.SystemPrompt 时消费共享备注，
// 因此这里直接断言提示文本（真实模型请求内容），不是日志。
func TestLeadJudgePrompt_ConsumesOtherSideNote(t *testing.T) {
	jctx := &LeadJudgeContext{
		SharedBusiness: SharedBusinessInput{
			Notes: []BusinessNote{{Source: ContextNoteNamespaceAutoReply, At: time.Now().Unix(), Attribute: BusinessNoteFact, Text: "自动回复侧刚确认客户预算 50 万"}},
			Tags:  []string{"预算明确"},
		},
	}
	prompt := buildLeadJudgeSystemPrompt(jctx)
	if !strings.Contains(prompt, "自动回复侧刚确认客户预算 50 万") || !strings.Contains(prompt, "预算明确") {
		t.Fatalf("线索侧模型提示未消费对方刚写信息: %s", prompt)
	}
}

type sharedContextRepoStub struct {
	conv *model.ConversationContext
}

func (r *sharedContextRepoStub) Get(_ context.Context, _ uint, _ string) (*model.ConversationContext, error) {
	return r.conv, nil
}
func (r *sharedContextRepoStub) Upsert(_ context.Context, _ *model.ConversationContext) error {
	return nil
}
func (r *sharedContextRepoStub) List(_ context.Context, _ uint, _ int) ([]model.ConversationContext, error) {
	return nil, nil
}

// 回复侧：入站编排把共享备注/标签写进实际 TriggerInboundAI 的 content（用户消息输入），
// 用假 trigger 捕获真实模型请求入参作为消费证据。
func TestInboxTrigger_CarriesOtherSideNoteIntoAIContent(t *testing.T) {
	t.Setenv("AI_DEBOUNCE_SECONDS", "0")
	svc := NewInboxIngressService()
	tr := &fakeAITrigger{}
	svc.SetAITrigger(tr)
	sharedConv := &model.ConversationContext{
		Tags: model.JSONStrings{
			"note:lead_mining:fact|1|线索侧刚判定为采购需求",
			"note:auto_reply:suggestion|2|自己刚写的建议",
			"重点客户",
		},
		Summary: "双方已交换名片",
	}
	svc.SetSharedBusinessProvider(func(_ context.Context, _ *model.MessageEvent) SharedBusinessInput {
		return SharedBusinessInputFromContext(sharedConv, ContextNoteNamespaceAutoReply, sharedBusinessInputBudget)
	})
	ev := &model.MessageEvent{
		EventID: "e-shared", Channel: "douyin", ConversationID: "conv-shared", SenderID: "c1",
		Content: "你好", Extra: map[string]any{"account_id": "acc-shared"}, Timestamp: time.Now(),
	}
	if _, err := svc.HandleIngressMessage(context.Background(), ev); err != nil {
		t.Fatalf("入站处理失败: %v", err)
	}
	if tr.called != 1 {
		t.Fatalf("期望触发 AI 一次，实际 %d", tr.called)
	}
	for _, want := range []string{"线索侧刚判定为采购需求", "重点客户", "双方已交换名片"} {
		if !strings.Contains(tr.lastContent, want) {
			t.Fatalf("回复侧模型输入未消费对方信息 %q: %s", want, tr.lastContent)
		}
	}
	if strings.Contains(tr.lastContent, "自己刚写的建议") {
		t.Fatal("回复侧不得把自己写的备注当对方信息再消费")
	}
}
