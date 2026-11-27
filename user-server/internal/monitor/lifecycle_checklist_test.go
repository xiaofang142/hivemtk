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

func stageOf(c LifecycleChecklist, node string) LifecycleCheckStage {
	for _, stage := range c.Stages {
		if stage.Node == node {
			return stage
		}
	}
	return LifecycleCheckStage{}
}
