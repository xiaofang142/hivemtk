package llm

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"hivemtk-user/internal/config"
)

// TestEmbeddingService_DefaultConfig_LocalBaseURL 验证私域基线：
// Embedding 默认指向本地推理服务（真实 bge-m3），禁止走 LLM 厂商 API。
//
// 文档源：DEVELOPMENT.md §2.4 + config.yaml inference.embedding
// dev 档契约：base_url=http://127.0.0.1:8208/v1（宿主机 llama.cpp，dev 部署位置）
//
//	docker 档契约：base_url=http://mtk-embedding:8208/v1（容器内服务名）
//	调整流程：先改 ports.go DefaultEmbeddingBaseURLDev/Docker，再同步 config.yaml
func TestEmbeddingService_DefaultConfig_LocalBaseURL(t *testing.T) {
	clearEmbeddingEnv(t)

	svc := NewEmbeddingService()
	cfg := svc.DefaultConfig()

	if !strings.HasPrefix(cfg.BaseURL, "http://127.0.0.1:8208/") &&
		!strings.HasPrefix(cfg.BaseURL, "http://mtk-embedding:8208/") {
		t.Errorf("私域基线违规：默认 BaseURL 必须指向本地 embedding 服务（127.0.0.1:8208 或 mtk-embedding:8208），实际: %s", cfg.BaseURL)
	}
	if cfg.Model != "bge-m3" {
		t.Errorf("默认 Model 必须是 bge-m3（bge-m3 模型名），实际: %s", cfg.Model)
	}
	if cfg.Dimension != 1024 {
		t.Errorf("默认 Dimension 必须是 1024，实际: %d", cfg.Dimension)
	}
	if cfg.AllowFallback {
		t.Error("私域基线违规：默认 AllowFallback 必须是 false（禁止静默降级到 hash）")
	}
}

// TestEmbeddingService_DefaultConfig_NotFallBackToLLMBaseURL 验证不再回退到 LLM_BASE_URL
// 即不允许把 LLM 对话 API 用于 Embedding（私域数据禁止出域）
func TestEmbeddingService_DefaultConfig_NotFallBackToLLMBaseURL(t *testing.T) {
	clearEmbeddingEnv(t)
	t.Setenv("LLM_BASE_URL", "https://api.openai.com/v1")

	svc := NewEmbeddingService()
	cfg := svc.DefaultConfig()

	if strings.Contains(cfg.BaseURL, "api.openai.com") {
		t.Errorf("私域基线违规：Embedding BaseURL 不得引用 LLM 厂商域名: %s", cfg.BaseURL)
	}
}

// 2026-11-12 §3.13：TestEmbeddingService_DefaultConfig_OverrideByEnv 与
// TestEmbeddingService_Embed_AllowFallback_Works 已删——EMBEDDING_* env 读取点随
// env→DB 迁移全部移除（连接配置改由参数中心 embedding.global_override 驱动，
// EMBEDDING_ALLOW_FALLBACK 生产恒 false），原测试验证的 env 覆盖语义不复存在。

// TestEmbeddingService_Embed_NoFallback_ReturnsError 验证默认配置下，
// 不可达时直接返回错误（不再静默降级到 hash）。
func TestEmbeddingService_Embed_NoFallback_ReturnsError(t *testing.T) {
	clearEmbeddingEnv(t)
	config.SetAppConfig(&config.AppConfig{
		Inference: config.InferenceConfig{
			Embedding: config.InferenceEmbeddingConfig{
				BaseURL:       "http://127.0.0.1:9",
				Model:         "bge-m3",
				Dimension:     1024,
				AllowFallback: false,
			},
		},
	})
	t.Cleanup(func() { config.SetAppConfig(nil) })

	svc := NewEmbeddingService()
	cfg := svc.DefaultConfig()
	cfg.RequestTimeout = 1
	cfg.MaxRetries = 1

	_, err := svc.Embed(context.Background(), cfg, []string{"hello"})
	if err == nil {
		t.Fatal("私域基线违规：本地 embedding 不可达时应返回错误，不应静默降级")
	}
	if !strings.Contains(err.Error(), "本地 embedding 服务不可达") {
		t.Errorf("错误消息应当明确指出本地 embedding 不可达，实际: %v", err)
	}
}

// 2026-11-12 §3.13：TestEmbeddingService_Embed_AllowFallback_Works 已删——
// EMBEDDING_ALLOW_FALLBACK 生产恒 false（私域红线），env 开关已移除。

// TestEmbeddingService_Embed_RealLocalServer 验证对接真实 TEI 兼容服务（mock 一个）
//
// 注：config.yaml 中 base_url 已固定为 127.0.0.1:8208（dev 档契约）。
// EMBEDDING_BASE_URL env 变量不覆盖已配置值（file 优先），故本测试直接构造 cfg 走 mock server。
func TestEmbeddingService_Embed_RealLocalServer(t *testing.T) {
	clearEmbeddingEnv(t)
	mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/embeddings" {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		vec := make([]float32, 768)
		for i := range vec {
			vec[i] = 0.01
		}
		w.Write([]byte(`{"object":"list","data":[{"object":"embedding","index":0,"embedding":[` +
			floatArrayString(vec) + `]}],"model":"BAAI/bge-base-zh-v1.5","usage":{"prompt_tokens":1,"total_tokens":1}}`))
	}))
	defer mock.Close()

	svc := NewEmbeddingService()
	cfg := &EmbeddingConfig{
		BaseURL:        mock.URL,
		Model:          "BAAI/bge-base-zh-v1.5",
		Dimension:      768,
		APIType:        "openai",
		RequestTimeout: 5,
		MaxRetries:     1,
		AllowFallback:  false,
	}

	vectors, err := svc.Embed(context.Background(), cfg, []string{"中文测试"})
	if err != nil {
		t.Fatalf("对接真实本地服务失败: %v", err)
	}
	if len(vectors) != 1 || len(vectors[0]) != 768 {
		t.Errorf("期望 1 个 768 维向量，实际 %d 个,维度 %d", len(vectors), len(vectors[0]))
	}
}

func floatArrayString(v []float32) string {
	parts := make([]string, len(v))
	for i, f := range v {
		parts[i] = strings.TrimRight(strings.TrimRight(
			formatFloat(f), "0"), ".")
	}
	return strings.Join(parts, ",")
}

func formatFloat(f float32) string {
	return "0.01"
}

func clearEmbeddingEnv(t *testing.T) {
	t.Helper()
	keys := []string{
		"EMBEDDING_BASE_URL", "EMBEDDING_MODEL", "EMBEDDING_DIM", "EMBEDDING_PROTOCOL",
		"EMBEDDING_API_KEY", "EMBEDDING_ALLOW_FALLBACK",
		"LLM_BASE_URL", "LLM_API_KEY", "OPENAI_BASE_URL", "OPENAI_API_KEY",
	}
	for _, k := range keys {
		_ = os.Unsetenv(k)
	}
}
