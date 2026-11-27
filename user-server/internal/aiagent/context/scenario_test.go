package context

import "testing"

func TestExecutionScenarioIDValidation(t *testing.T) {
	tests := []struct {
		name  string
		value string
		want  bool
	}{
		{name: "intent", value: "intent", want: true},
		{name: "fine intent", value: "fine_intent", want: true},
		{name: "emotion", value: "emotion", want: true},
		{name: "objection", value: "objection", want: true},
		{name: "humanize eval", value: "humanize_eval", want: true},
		{name: "humanize rewrite", value: "humanize_rewrite", want: true},
		{name: "sales", value: "sales_generate", want: true},
		{name: "lead detect", value: "lead_detect", want: true},
		{name: "lead judge", value: "lead_judge", want: true},
		{name: "lead react", value: "lead_react", want: true},
		{name: "runtime", value: "agent_runtime", want: true},
		{name: "summary", value: "summary", want: true},
		{name: "route scenario is not execution scenario", value: "low_cost", want: false},
		{name: "unknown", value: "unknown", want: false},
		{name: "empty", value: "", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ExecutionScenarioID(tt.value).Valid(); got != tt.want {
				t.Fatalf("ExecutionScenarioID(%q).Valid() = %v, want %v", tt.value, got, tt.want)
			}
		})
	}
}

func TestParseExecutionScenarioID(t *testing.T) {
	got, err := ParseExecutionScenarioID("  lead_react  ")
	if err != nil {
		t.Fatalf("ParseExecutionScenarioID() error = %v", err)
	}
	if got != ExecutionScenarioLeadReact {
		t.Fatalf("ParseExecutionScenarioID() = %q, want %q", got, ExecutionScenarioLeadReact)
	}

	if _, err := ParseExecutionScenarioID("low_cost"); err == nil {
		t.Fatal("ParseExecutionScenarioID(low_cost) 应拒绝路由档位")
	}
}
