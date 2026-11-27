package llm

import "testing"

func TestApplyProviderCapabilitiesToConfigClampsOutput(t *testing.T) {
	provider := &ProviderConfig{Model: "claude-opus-5.5", APIType: "openai"}
	config := &LLMConfig{Model: provider.Model, MaxTokens: 4096}
	if err := applyProviderCapabilitiesToConfig(config, provider); err != nil {
		t.Fatalf("能力适配失败: %v", err)
	}
	caps := ResolveProviderCapabilities(*provider)
	if config.MaxTokens > caps.MaxOutputTokens {
		t.Fatalf("输出预算未按模型上限夹取: got %d, max %d", config.MaxTokens, caps.MaxOutputTokens)
	}
}

func TestApplyProviderCapabilitiesToConfigKeepsOpaqueSessionOnStatelessProvider(t *testing.T) {
	provider := &ProviderConfig{Model: "deepseek-v4-pro", APIType: "openai"}
	config := &LLMConfig{Model: provider.Model, MaxTokens: 512, LLMSessionRef: "llmsess_bus_1"}
	if err := applyProviderCapabilitiesToConfig(config, provider); err != nil {
		t.Fatalf("stateless 协议下 canonical session 只作本地关联，不应硬失败: %v", err)
	}
	shape, err := BuildProviderRequestShapeForDispatch(*config, ResolveProviderCapabilities(*provider))
	if err != nil {
		t.Fatalf("请求形状构造失败: %v", err)
	}
	if !shape.HasSession {
		t.Fatal("canonical session 关联丢失")
	}
	reported := false
	for _, field := range shape.UnsupportedFields {
		if field == "native_session" {
			reported = true
		}
	}
	if !reported {
		t.Fatalf("未如实报告原生 session 不受支持: %+v", shape.UnsupportedFields)
	}
}
