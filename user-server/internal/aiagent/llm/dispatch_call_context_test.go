package llm

import (
	"crypto/sha256"
	"encoding/hex"
	"testing"
)

func TestDispatchCallContextPropagatesToLLMConfig(t *testing.T) {
	req := DispatchRequest{
		Scenario:              ScenarioSOPReply,
		ExecutionScenarioID:   "sales_generate",
		LLMSessionRef:         "llmsess_abc",
		RequestIdempotencyKey: "idem-1",
		PromptPrefixHash:      "prefix-1",
	}
	config := &LLMConfig{}
	if err := applyDispatchCallContext(config, req); err != nil {
		t.Fatal(err)
	}
	if config.ExecutionScenarioID != req.ExecutionScenarioID || config.LLMSessionRef != req.LLMSessionRef ||
		config.RequestIdempotencyKey != req.RequestIdempotencyKey || config.PromptPrefixHash != req.PromptPrefixHash {
		t.Fatalf("调用上下文未贯通：%+v", config)
	}
}

func TestDispatchCallContextRejectsInvalidExecutionScenario(t *testing.T) {
	for _, value := range []string{"", "low_cost", "unknown"} {
		req := DispatchRequest{Scenario: ScenarioLowCost, ExecutionScenarioID: value}
		if err := applyDispatchCallContext(&LLMConfig{}, req); err == nil {
			t.Fatalf("非法执行场景 %q 必须拒绝", value)
		}
	}
}

func TestBuildRequestIdempotencyKeyIsStableAndScoped(t *testing.T) {
	got := BuildRequestIdempotencyKey("hub:42", "sales_generate", "generate")
	if got != BuildRequestIdempotencyKey("hub:42", "sales_generate", "generate") {
		t.Fatal("相同输入必须产生稳定幂等键")
	}
	if got == BuildRequestIdempotencyKey("hub:42", "lead_judge", "generate") {
		t.Fatal("不同执行场景不得共用幂等键")
	}
	if got == BuildRequestIdempotencyKey("hub:43", "sales_generate", "generate") {
		t.Fatal("不同消息不得共用幂等键")
	}
	sum := sha256.Sum256([]byte("hub:42|sales_generate|generate"))
	if got != hex.EncodeToString(sum[:]) {
		t.Fatalf("幂等键应为确定性 SHA-256：%q", got)
	}
}
