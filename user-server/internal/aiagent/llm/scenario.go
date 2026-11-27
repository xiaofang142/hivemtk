package llm

import (
	"fmt"

	agentcontext "hivemtk-user/internal/aiagent/context"
)

// ResolveDispatchScenario maps an isolated execution scenario to the existing
// provider routing/billing scenario. The result must never be used as the
// session isolation key.
func ResolveDispatchScenario(execution agentcontext.ExecutionScenarioID) (DispatchScenario, error) {
	switch execution {
	case agentcontext.ExecutionScenarioIntent,
		agentcontext.ExecutionScenarioFineIntent,
		agentcontext.ExecutionScenarioEmotion:
		return ScenarioIntentRecognize, nil
	case agentcontext.ExecutionScenarioObjection,
		agentcontext.ExecutionScenarioHumanizeEval,
		agentcontext.ExecutionScenarioHumanizeRewrite:
		return ScenarioObjection, nil
	case agentcontext.ExecutionScenarioSalesGenerate:
		return ScenarioSOPReply, nil
	case agentcontext.ExecutionScenarioLeadDetect,
		agentcontext.ExecutionScenarioLeadJudge,
		agentcontext.ExecutionScenarioLeadReact,
		agentcontext.ExecutionScenarioAgentRuntime:
		return ScenarioLowCost, nil
	case agentcontext.ExecutionScenarioSummary:
		return ScenarioLongSummary, nil
	default:
		return "", fmt.Errorf("no dispatch scenario for execution scenario %q", execution)
	}
}

func (s DispatchScenario) Valid() bool {
	switch s {
	case ScenarioIntentRecognize,
		ScenarioSOPReply,
		ScenarioObjection,
		ScenarioFriendlyChat,
		ScenarioLongSummary,
		ScenarioHighQuality,
		ScenarioLowCost,
		ScenarioJevChoice:
		return true
	default:
		return false
	}
}
