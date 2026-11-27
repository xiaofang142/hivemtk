package tracing_test

import (
	"testing"

	"hivemtk-user/internal/pkg/tracing"
)

func TestAITriggerDecisionNodeRegistered(t *testing.T) {
	if tracing.NodeAITriggerDecision != "ai_trigger_decision" {
		t.Fatalf("节点名漂移: %q", tracing.NodeAITriggerDecision)
	}
	if tracing.NodeOrder(tracing.NodeAITriggerDecision) != 2 {
		t.Errorf("节点顺序应为 2，实际=%d", tracing.NodeOrder(tracing.NodeAITriggerDecision))
	}
	if tracing.NodeLabel(tracing.NodeAITriggerDecision) != "AI 触发决策" {
		t.Errorf("节点中文名漂移: %q", tracing.NodeLabel(tracing.NodeAITriggerDecision))
	}
}
