package service

import (
	"testing"

	"hivemtk-user/internal/model"
)

// LM-08 正例：三轴分离，保留多值与说话人证据；供需可同时存在。
func TestClassifyLeadSemanticAxes_MultipleRolesWithEvidence(t *testing.T) {
	msgs := []DetectionMessage{
		{MessageID: "m1", SenderID: "甲", Content: "我这里有货，厂价直销", Role: "user"},
		{MessageID: "m2", SenderID: "乙", Content: "我需要货，帮找原料", Role: "user"},
		{MessageID: "m3", SenderID: "丙", Content: "我撮合上下游，赚佣金", Role: "user"},
	}
	axes := ClassifyLeadSemanticAxes(msgs)
	if len(axes.Intent["demand"]) == 0 || len(axes.Intent["supply"]) == 0 {
		t.Fatalf("买卖兼有应保留多值: %+v", axes.Intent)
	}
	if len(axes.Identity["broker"]) == 0 || len(axes.Chain["broker"]) == 0 {
		t.Fatalf("撮合/上下游应分轴表达: %+v", axes)
	}
	if len(axes.Evidence["m1"]) == 0 || axes.Evidence["m1"][0].Speaker != "甲" {
		t.Fatalf("证据须带说话人: %+v", axes.Evidence)
	}
}

// LM-08 反例：否定、引用、转发、跨行业同词不得误判为真实意图；无关键词真实需求仍可被召回为 unknown 候选。
func TestClassifyLeadSemanticAxes_NegationReferenceAndUnknown(t *testing.T) {
	msgs := []DetectionMessage{
		{MessageID: "n1", SenderID: "甲", Content: "我不需要货，已经买到了", Role: "user"},
		{MessageID: "n2", SenderID: "乙", Content: "转述：张三需要货", Role: "user"},
		{MessageID: "n3", SenderID: "丙", Content: "你们报价里的‘需要’是什么意思", Role: "user"},
	}
	axes := ClassifyLeadSemanticAxes(msgs)
	for _, id := range []string{"n1", "n2", "n3"} {
		if len(axes.Evidence[id]) != 0 {
			t.Errorf("%s 是否定/转述/引用，不得当作真实意图证据: %+v", id, axes.Evidence[id])
		}
	}
	// 明确否定/转述/引用属于无信号，不建候选；不得为此补造 unknown 意图证据。
	if len(axes.Intent) != 0 {
		t.Fatalf("明确否定/转述/引用不应产生意图轴值: %+v", axes.Intent)
	}
}

// LM-08 与现有语义检测器共存：Payload 里追加三轴结论，不破坏 matched/count。
func TestLeadSemanticDetector_AxesPayload(t *testing.T) {
	d := NewLeadSemanticDetector()
	hit := d.Detect(DetectionInput{
		Rule:     &model.LeadMiningRule{SemanticKeywords: model.JSONStrings{"需求"}},
		Messages: []DetectionMessage{{MessageID: "m1", SenderID: "甲", Content: "我需要货"}},
	})
	axes, ok := hit.Payload["semantic_axes"]
	if !ok {
		t.Fatal("语义检测 Payload 应携带分轴结论")
	}
	if _, ok := axes.(LeadSemanticAxes); !ok {
		t.Fatalf("分轴结论类型错误: %T", axes)
	}
}
