package llm

import (
	"testing"

	"hivemtk-user/internal/config"
)

// TestNewDispatcherFromConfig_LocalFirst 验证优化三：本地优先、云端 opt-in
//
// 端口字面量与 ports.go 单一源对齐（DEVELOPMENT.md §2.4）：
//   - 8207 LLM（DefaultLLMBaseURLDev）
//   - 8208 Embedding（DefaultEmbeddingBaseURLDev）
//   - 8209 Rerank（DefaultRerankBaseURLDev）
func TestNewDispatcherFromConfig_LocalFirst(t *testing.T) {
	cfg := config.AppConfig{
		Inference: config.InferenceConfig{
			Profile: "dev",
			Embedding: config.InferenceEmbeddingConfig{
				Mode: config.InferenceModeLocal, BaseURL: config.DefaultEmbeddingBaseURLDev,
				Model: "bge-m3", Dimension: 1024,
			},
			Rerank: config.InferenceRerankConfig{
				Mode: config.InferenceModeLocal, BaseURL: config.DefaultRerankBaseURLDev,
				Model: "bge-reranker-v2-m3", Enabled: true,
			},
			LLM: config.InferenceLLMConfig{
				Mode: config.InferenceModeLocal, BaseURL: config.DefaultLLMBaseURLDev,
				Model: "Qwen2.5-1.5B-Instruct",
			},
		},
	}

	d := NewDispatcherFromConfig(cfg)

	def, ok := d.providers["default"]
	if !ok {
		t.Fatal("default provider 未注册")
	}
	if !def.Enabled {
		t.Fatal("default provider 应为启用（本地优先）")
	}
	if def.BaseURL != config.DefaultLLMBaseURLDev {
		t.Fatalf("default base_url 错误: %s", def.BaseURL)
	}

	for _, name := range []string{"deepseek"} {
		p, ok := d.providers[name]
		if !ok {
			t.Fatalf("云端 provider %s 未注册", name)
		}
		if p.Enabled {
			t.Fatalf("云端 provider %s 在无 api_key 时不应启用", name)
		}
	}

	if len(d.routes) == 0 {
		t.Fatal("未注册任何场景路由")
	}
	for sc, r := range d.routes {
		if r.Provider != "default" {
			t.Fatalf("场景 %s 主路由应为 default，实际 %s", sc, r.Provider)
		}
	}
}

// TestNewDispatcherFromConfig_CloudOptIn 验证：配置 api_key 后云端启用为 fallback
func TestNewDispatcherFromConfig_CloudOptIn(t *testing.T) {
	cfg := config.AppConfig{
		Inference: config.InferenceConfig{
			LLM: config.InferenceLLMConfig{
				BaseURL: config.DefaultLLMBaseURLDev,
				Model:   "Qwen2.5-1.5B-Instruct",
				CloudProviders: []config.InferenceCloudProviderConfig{
					{Name: "deepseek", BaseURL: "https://api.deepseek.com", APIType: "openai",
						Model: "deepseek-chat", Enabled: true, APIKey: "sk-" + "test"},
				},
			},
		},
	}
	d := NewDispatcherFromConfig(cfg)
	if !d.providers["deepseek"].Enabled {
		t.Fatal("配置 api_key+enabled 后 deepseek 应启用为 fallback")
	}
	if !d.providers["default"].Enabled {
		t.Fatal("default 仍应启用")
	}
}
