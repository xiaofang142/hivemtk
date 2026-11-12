package llm

import (
	"context"
	"testing"
)

func TestNewLLMService(t *testing.T) {
	svc := NewLLMService()
	if svc == nil {
		t.Error("Expected non-nil LLMService")
	}
}

// TestLLMService_Generate 集成测试：需要真实 LLM API，跳过若缺少 API Key
func TestLLMService_Generate(t *testing.T) {
	// 2026-11-12 §3.13：LLM 连接配置已迁移至参数中心 llm.global_override，
	// 集成测试不再从 env 取密钥；默认跳过，需要时由开发者配置 DB 后手工运行。
	t.Skip("集成测试默认跳过：LLM 连接配置已迁移至参数中心 llm.global_override")
	svc := NewLLMService()
	config := &LLMConfig{
		APIKey:  "",
		BaseURL: "https://api.openai.com/v1",
		Model:   "gpt-4",
	}
	result, err := svc.Generate(context.Background(), config, "hello")
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if result == "" {
		t.Error("Expected non-empty result")
	}
}

func TestLLMService_GenerateStructured(t *testing.T) {
	// 2026-11-12 §3.13：LLM 连接配置已迁移至参数中心 llm.global_override，
	// 集成测试不再从 env 取密钥；默认跳过，需要时由开发者配置 DB 后手工运行。
	t.Skip("集成测试默认跳过：LLM 连接配置已迁移至参数中心 llm.global_override")
	svc := NewLLMService()
	config := &LLMConfig{
		APIKey:  "",
		BaseURL: "https://api.openai.com/v1",
		Model:   "gpt-4",
	}
	schema := map[string]string{"type": "object"}
	result, err := svc.GenerateStructured(context.Background(), config, "hello", schema)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if result == nil {
		t.Error("Expected non-nil result")
	}
}

func TestLLMService_ValidateConfig(t *testing.T) {
	svc := NewLLMService()

	validConfig := &LLMConfig{
		APIKey:    "test-key",
		BaseURL:   "https://api.example.com",
		Model:     "gpt-4",
		MaxTokens: 100,
	}
	err := svc.ValidateConfig(validConfig)
	if err != nil {
		t.Errorf("Expected valid config, got error: %v", err)
	}

	emptyConfig := &LLMConfig{}
	err = svc.ValidateConfig(emptyConfig)
	if err == nil {
		t.Error("Expected error for empty config")
	}
}
