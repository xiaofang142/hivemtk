package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func resetEmbeddingDimensionOverrideSeams(t *testing.T) {
	t.Helper()
	t.Cleanup(func() {
		SetEmbeddingOverrideProvider(nil)
		SetEmbeddingOverrideWriter(nil)
	})
}

func TestEmbeddingOverrideDimensionsSentToOpenAIProvider(t *testing.T) {
	resetEmbeddingDimensionOverrideSeams(t)

	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/embeddings" {
			t.Errorf("path=%s, want /v1/embeddings", r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Errorf("decode request: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"object":"list","data":[{"object":"embedding","index":0,"embedding":[0.1,0.2]}],"model":"gemini-embedding-2","usage":{"prompt_tokens":1,"total_tokens":1}}`))
	}))
	defer srv.Close()

	raw := fmt.Sprintf(`{"enabled":true,"base_url":%q,"api_key":"k","model":"gemini-embedding-2","protocol":"openai","dimensions":1024}`, srv.URL)
	SetEmbeddingOverrideProvider(func(context.Context) string { return raw })

	svc := NewEmbeddingService()
	cfg := svc.DefaultConfig()
	if cfg.RequestDimensions != 1024 {
		t.Fatalf("RequestDimensions=%d, want 1024", cfg.RequestDimensions)
	}
	if cfg.Dimension != 1024 {
		t.Fatalf("Dimension=%d, want 1024 (pgvector hard constraint)", cfg.Dimension)
	}
	if _, err := svc.Embed(context.Background(), cfg, []string{"hello"}); err != nil {
		t.Fatalf("Embed 失败：%v", err)
	}
	if gotBody["dimensions"] != float64(1024) {
		t.Fatalf("请求体 dimensions=%v, want 1024; body=%v", gotBody["dimensions"], gotBody)
	}
}

func TestEmbeddingRequestOmitsDimensionsWhenNotConfigured(t *testing.T) {
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Errorf("decode request: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"object":"list","data":[{"object":"embedding","index":0,"embedding":[0.1]}],"model":"bge-m3","usage":{"prompt_tokens":1,"total_tokens":1}}`))
	}))
	defer srv.Close()

	cfg := &EmbeddingConfig{
		BaseURL:        srv.URL,
		Protocol:       "openai",
		Model:          "bge-m3",
		Dimension:      1024,
		RequestTimeout: 5,
	}
	svc := NewEmbeddingService()
	if _, err := svc.Embed(context.Background(), cfg, []string{"hello"}); err != nil {
		t.Fatalf("Embed 失败：%v", err)
	}
	if _, ok := gotBody["dimensions"]; ok {
		t.Fatalf("未配置 dimensions 时请求体不应带该字段：%v", gotBody)
	}
}

func TestResolveEmbeddingEndpointKeepsVersionedOpenAICompatibleBase(t *testing.T) {
	tests := []struct {
		name string
		base string
		want string
	}{
		{"plain", "https://api.example.com", "https://api.example.com/v1/embeddings"},
		{"v1", "https://api.example.com/v1", "https://api.example.com/v1/embeddings"},
		{"gemini", "https://generativelanguage.googleapis.com/v1beta/openai/", "https://generativelanguage.googleapis.com/v1beta/openai/embeddings"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := resolveEmbeddingEndpoint(tt.base, false); got != tt.want {
				t.Fatalf("endpoint=%q, want %q", got, tt.want)
			}
		})
	}
}

func TestEmbeddingOverrideRejectsNon1024RequestedDimensions(t *testing.T) {
	resetEmbeddingDimensionOverrideSeams(t)
	SetEmbeddingOverrideProvider(func(context.Context) string {
		return `{"enabled":true,"base_url":"http://example.invalid","model":"m","dimensions":3072}`
	})
	cfg := NewEmbeddingService().DefaultConfig()
	if cfg.RequestDimensions != 3072 {
		t.Fatalf("RequestDimensions=%d, want 3072", cfg.RequestDimensions)
	}
	if cfg.Dimension != 1024 {
		t.Fatalf("存储维度被改写为 %d，want 1024", cfg.Dimension)
	}
}
