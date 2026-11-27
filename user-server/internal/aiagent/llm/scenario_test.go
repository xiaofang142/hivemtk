package llm

import (
	"testing"

	agentcontext "hivemtk-user/internal/aiagent/context"
)

func TestResolveDispatchScenarioFromExecutionScenario(t *testing.T) {
	tests := []struct {
		name        string
		execution   agentcontext.ExecutionScenarioID
		want        DispatchScenario
		wantUnknown bool
	}{
		{name: "intent", execution: agentcontext.ExecutionScenarioIntent, want: ScenarioIntentRecognize},
		{name: "fine intent", execution: agentcontext.ExecutionScenarioFineIntent, want: ScenarioIntentRecognize},
		{name: "emotion", execution: agentcontext.ExecutionScenarioEmotion, want: ScenarioIntentRecognize},
		{name: "objection", execution: agentcontext.ExecutionScenarioObjection, want: ScenarioObjection},
		{name: "humanize eval", execution: agentcontext.ExecutionScenarioHumanizeEval, want: ScenarioObjection},
		{name: "humanize rewrite", execution: agentcontext.ExecutionScenarioHumanizeRewrite, want: ScenarioObjection},
		{name: "sales", execution: agentcontext.ExecutionScenarioSalesGenerate, want: ScenarioSOPReply},
		{name: "lead detect", execution: agentcontext.ExecutionScenarioLeadDetect, want: ScenarioLowCost},
		{name: "lead judge", execution: agentcontext.ExecutionScenarioLeadJudge, want: ScenarioLowCost},
		{name: "lead react", execution: agentcontext.ExecutionScenarioLeadReact, want: ScenarioLowCost},
		{name: "runtime", execution: agentcontext.ExecutionScenarioAgentRuntime, want: ScenarioLowCost},
		{name: "summary", execution: agentcontext.ExecutionScenarioSummary, want: ScenarioLongSummary},
		{name: "unknown", execution: agentcontext.ExecutionScenarioID("unknown"), wantUnknown: true},
		{name: "route only", execution: agentcontext.ExecutionScenarioID("low_cost"), wantUnknown: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ResolveDispatchScenario(tt.execution)
			if tt.wantUnknown {
				if err == nil {
					t.Fatalf("ResolveDispatchScenario(%q) 应返回错误", tt.execution)
				}
				return
			}
			if err != nil {
				t.Fatalf("ResolveDispatchScenario(%q) error = %v", tt.execution, err)
			}
			if got != tt.want {
				t.Fatalf("ResolveDispatchScenario(%q) = %q, want %q", tt.execution, got, tt.want)
			}
		})
	}
}

func TestDispatchScenarioCannotSelectExecutionSession(t *testing.T) {
	if DispatchScenario("lead_detect").Valid() {
		t.Fatal("DispatchScenario 不应接受执行场景值")
	}
	if !ScenarioLowCost.Valid() {
		t.Fatal("ScenarioLowCost 应保持为合法路由档位")
	}
}
