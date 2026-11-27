package monitor

import (
	"testing"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tracing"
)

func TestCheckLifecycleStagesProductionFixtures(t *testing.T) {
	wechat := []model.MessageTrace{
		{Node: tracing.NodeIngest},
		{Node: tracing.NodeAITriggerDecision, Output: `{"outcome":"queued"}`},
		{Node: tracing.NodeInboxSync},
		{Node: tracing.NodeAIDispatch},
		{Node: tracing.NodeAIResult, Abnormal: "capability_submit_denied"},
	}
	got := CheckLifecycleStages(wechat)
	if !got.Complete {
		t.Errorf("微信 trace 必需阶段应齐全，missing=%v", got.Missing)
	}
	if !stageOf(got, tracing.NodeAIResult).Abnormal {
		t.Error("微信 ai_result 异常应被识别")
	}

	douyin := []model.MessageTrace{
		{Node: tracing.NodeIngest},
		{Node: tracing.NodeInboxSync},
	}
	got = CheckLifecycleStages(douyin)
	if got.Complete {
		t.Error("抖音卡住 trace 缺 AI 触发决策，不应判定完整")
	}
	if len(got.Missing) != 1 || got.Missing[0] != tracing.NodeAITriggerDecision {
		t.Errorf("missing 应只有 ai_trigger_decision，实际=%v", got.Missing)
	}
}

func TestCheckLifecycleStagesDecisionOKNotAbnormal(t *testing.T) {
	// 数据驱动回归：queued/ok 的决策节点 Abnormal 必须为空，否则 checklist 会把
	// 正常触发误报成节点异常（生产曾实测 ai_trigger_decision.abnormal=true）。
	spans := []model.MessageTrace{
		{Node: tracing.NodeIngest, Status: tracing.StatusOk},
		{Node: tracing.NodeAITriggerDecision, Status: tracing.StatusOk, Abnormal: "", Output: `{"outcome":"queued","reason":"ok"}`},
		{Node: tracing.NodeInboxSync, Status: tracing.StatusOk},
	}
	got := CheckLifecycleStages(spans)
	if !got.Complete {
		t.Errorf("必需阶段应齐全，missing=%v", got.Missing)
	}
	if stageOf(got, tracing.NodeAITriggerDecision).Abnormal {
		t.Error("queued/ok 决策节点不应被判为异常")
	}
}

func stageOf(c LifecycleChecklist, node string) LifecycleCheckStage {
	for _, stage := range c.Stages {
		if stage.Node == node {
			return stage
		}
	}
	return LifecycleCheckStage{}
}
