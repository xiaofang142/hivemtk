package service

import (
	"context"
	"testing"

	"hivemtk-user/internal/dto"
	"hivemtk-user/internal/model"
)

func TestFAQPolicy_KeywordScoreCannotApproveWholeQuestion(t *testing.T) {
	enabled, agentID := true, uint(1)
	entries := []model.FAQEntry{{ID: 1, AgentID: &agentID, Enabled: &enabled,
		Question: "HiveMTK是什么？", Answer: "标准产品介绍。", Confidence: 0.99,
		Keywords: []string{"HiveMTK", "是什么", "介绍", "项目", "硬件", "部署", "模型", "本地", "需要", "人工", "投诉", "费用"},
	}}
	for _, tc := range []struct {
		name, question string
		hit            bool
	}{
		{"标准整问", "HiveMTK是什么？", true},
		{"完整介绍等价", "介绍下项目", true},
		{"硬件不是泛介绍", "HiveMTK本地部署模型需要什么硬件？", false},
		{"组合要求不能漏答", "介绍下项目和部署费用", false},
		{"人工诉求优先", "介绍项目，转人工客服", false},
		{"投诉不免检", "我要投诉这个项目，请介绍部署", false},
		{"上下文追问不能猜", "这个项目具体怎么部署？", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			engine := &SalesEngine{faqSvc: newFAQServiceForFastPath(entries)}
			resp, hit := engine.tryFAQFastPath(context.Background(), &SalesRequest{
				UserMessage: tc.question, AgentContext: &dto.AgentContext{AgentID: agentID},
			})
			if hit != tc.hit {
				t.Fatalf("只能批准完整已覆盖问题：期望=%v 实际=%v", tc.hit, hit)
			}
			if hit && (resp == nil || resp.Reply != entries[0].Answer) {
				t.Fatal("标准答不得丢失")
			}
		})
	}
}

func TestFAQPolicy_IntroductionDoesNotBorrowUnrelatedDefinition(t *testing.T) {
	for _, standard := range []string{"内存是什么？", "退款是什么？", "大模型是什么？"} {
		t.Run(standard, func(t *testing.T) {
			if faqQuestionCovered("介绍下项目", standard) {
				t.Fatal("产品介绍不能借任意‘是什么’定义冒充覆盖")
			}
			if !faqQuestionCovered(standard, standard) {
				t.Fatal("完全同问的安全候选仍应保留资格")
			}
		})
	}
}
