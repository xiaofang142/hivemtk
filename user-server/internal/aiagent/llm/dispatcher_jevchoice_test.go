package llm

import (
	"testing"

	"hivemtk-user/internal/config"
)

func TestJevChoiceRouteRegistered(t *testing.T) {
	d := NewDispatcher(NewLLMService())
	r := d.GetRoute(ScenarioJevChoice)
	if r == nil {
		t.Fatal("jev_choice 路由缺失")
	}
	if r.MaxLatency != 5000 {
		t.Fatalf("MaxLatency=%d want 5000", r.MaxLatency)
	}
	if r.MinQuality != 0.7 {
		t.Fatalf("MinQuality=%v want 0.7", r.MinQuality)
	}
}

func TestJevChoiceRouteLocalFirst(t *testing.T) {
	cfg := config.AppConfig{
		Inference: config.InferenceConfig{
			LLM: config.InferenceLLMConfig{
				Mode: config.InferenceModeLocal, BaseURL: config.DefaultLLMBaseURLDev,
				Model: "Qwen2.5-1.5B-Instruct",
			},
		},
	}
	d := NewDispatcherFromConfig(cfg)
	r := d.GetRoute(ScenarioJevChoice)
	if r == nil {
		t.Fatal("jev_choice 路由缺失（localFirst）")
	}
	if r.Provider == "" {
		t.Fatal("jev_choice Provider 为空（localFirst）")
	}
	if r.MaxLatency != 5000 {
		t.Fatalf("MaxLatency=%d want 5000", r.MaxLatency)
	}
}
