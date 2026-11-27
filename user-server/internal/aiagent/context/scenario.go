package context

import (
	"fmt"
	"strings"
)

// ExecutionScenarioID identifies an isolated agent execution chain. It is not
// a provider routing or billing scenario.
type ExecutionScenarioID string

const (
	ExecutionScenarioIntent          ExecutionScenarioID = "intent"
	ExecutionScenarioFineIntent      ExecutionScenarioID = "fine_intent"
	ExecutionScenarioEmotion         ExecutionScenarioID = "emotion"
	ExecutionScenarioObjection       ExecutionScenarioID = "objection"
	ExecutionScenarioHumanizeEval    ExecutionScenarioID = "humanize_eval"
	ExecutionScenarioHumanizeRewrite ExecutionScenarioID = "humanize_rewrite"
	ExecutionScenarioSalesGenerate   ExecutionScenarioID = "sales_generate"
	ExecutionScenarioLeadDetect      ExecutionScenarioID = "lead_detect"
	ExecutionScenarioLeadJudge       ExecutionScenarioID = "lead_judge"
	ExecutionScenarioLeadReact       ExecutionScenarioID = "lead_react"
	ExecutionScenarioAgentRuntime    ExecutionScenarioID = "agent_runtime"
	ExecutionScenarioSummary         ExecutionScenarioID = "summary"
)

var executionScenarios = map[ExecutionScenarioID]struct{}{
	ExecutionScenarioIntent:          {},
	ExecutionScenarioFineIntent:      {},
	ExecutionScenarioEmotion:         {},
	ExecutionScenarioObjection:       {},
	ExecutionScenarioHumanizeEval:    {},
	ExecutionScenarioHumanizeRewrite: {},
	ExecutionScenarioSalesGenerate:   {},
	ExecutionScenarioLeadDetect:      {},
	ExecutionScenarioLeadJudge:       {},
	ExecutionScenarioLeadReact:       {},
	ExecutionScenarioAgentRuntime:    {},
	ExecutionScenarioSummary:         {},
}

func (s ExecutionScenarioID) Valid() bool {
	_, ok := executionScenarios[s]
	return ok
}

func ParseExecutionScenarioID(value string) (ExecutionScenarioID, error) {
	scenario := ExecutionScenarioID(strings.TrimSpace(value))
	if !scenario.Valid() {
		return "", fmt.Errorf("invalid execution scenario %q", value)
	}
	return scenario, nil
}
