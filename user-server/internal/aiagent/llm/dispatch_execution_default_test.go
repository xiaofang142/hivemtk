package llm

import "testing"

func TestDefaultExecutionScenarioForRoute(t *testing.T) {
	cases := map[DispatchScenario]string{
		ScenarioIntentRecognize: "intent",
		ScenarioSOPReply:        "sales_generate",
		ScenarioObjection:       "objection",
		ScenarioFriendlyChat:    "sales_generate",
		ScenarioLongSummary:     "summary",
		ScenarioHighQuality:     "sales_generate",
		ScenarioLowCost:         "lead_detect",
		ScenarioJevChoice:       "sales_generate",
	}
	for route, want := range cases {
		if got := defaultExecutionScenarioForRoute(route); got != want {
			t.Fatalf("路由档位 %s 的兼容执行场景应为 %s, got %s", route, want, got)
		}
	}
}

func TestDispatchRequestEmptyExecutionScenarioIsLegacyCompatible(t *testing.T) {
	req := DispatchRequest{Scenario: ScenarioIntentRecognize}
	fillLegacyExecutionScenario(&req)
	if req.ExecutionScenarioID != "intent" {
		t.Fatalf("空执行场景应按路由档位补默认, got %q", req.ExecutionScenarioID)
	}
	explicit := DispatchRequest{Scenario: ScenarioIntentRecognize, ExecutionScenarioID: "fine_intent"}
	fillLegacyExecutionScenario(&explicit)
	if explicit.ExecutionScenarioID != "fine_intent" {
		t.Fatalf("显式执行场景不得被覆盖, got %q", explicit.ExecutionScenarioID)
	}
}
