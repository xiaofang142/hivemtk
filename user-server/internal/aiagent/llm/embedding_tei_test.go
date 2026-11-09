package llm

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestEmbeddingTEI_RequestAndResponse TEI 原生 embedding：
// 端点 {base}/embed、请求 {inputs}、响应 [[float]]，并校验维度。
func TestEmbeddingTEI_RequestAndResponse(t *testing.T) {
	var gotPath, gotMethod, gotCT string
	var gotBody map[string]any

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotMethod = r.Method
		gotCT = r.Header.Get("Content-Type")
		raw, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read body: %v", err)
			return
		}
		if err := json.Unmarshal(raw, &gotBody); err != nil {
			t.Errorf("unmarshal request body %s: %v", raw, err)
			return
		}
		vec := make([]float32, 1024)
		vec[0] = 1
		vec[1023] = 1
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode([][]float32{vec, vec})
	}))
	defer srv.Close()

	cfg := &EmbeddingConfig{
		BaseURL:        srv.URL,
		Protocol:       "tei",
		Model:          "BAAI/bge-m3",
		Dimension:      1024,
		RequestTimeout: 5,
		MaxRetries:     1,
	}
	svc := NewEmbeddingService()
	vectors, err := svc.Embed(context.Background(), cfg, []string{"hello", "world"})
	if err != nil {
		t.Fatalf("embed: %v", err)
	}

	if gotPath != "/embed" {
		t.Errorf("path=%s want=/embed", gotPath)
	}
	if gotMethod != http.MethodPost {
		t.Errorf("method=%s want=POST", gotMethod)
	}
	if gotCT != "application/json" {
		t.Errorf("Content-Type=%s want=application/json", gotCT)
	}
	inputs, ok := gotBody["inputs"].([]any)
	if !ok || len(inputs) != 2 || inputs[0] != "hello" || inputs[1] != "world" {
		t.Errorf("inputs=%v want=[hello world]", gotBody["inputs"])
	}
	if _, exists := gotBody["model"]; exists {
		t.Errorf("TEI 请求体不应包含 model 字段: %v", gotBody)
	}
	if len(vectors) != 2 || len(vectors[0]) != 1024 {
		t.Fatalf("vectors shape=%dx%d want 2x1024", len(vectors), len(vectors[0]))
	}
}

// TestEmbeddingTEI_EndpointResolution 端点解析：裸 host → /embed；已带 /embed 原样。
func TestEmbeddingTEI_EndpointResolution(t *testing.T) {
	cases := []struct {
		name string
		base string
		tei  bool
		want string
	}{
		{"openai bare", "https://api.example.com", false, "https://api.example.com/v1/embeddings"},
		{"openai v1", "https://api.example.com/v1", false, "https://api.example.com/v1/embeddings"},
		{"tei bare", "https://tei.example.com", true, "https://tei.example.com/embed"},
		{"tei trailing slash", "https://tei.example.com/", true, "https://tei.example.com/embed"},
		{"tei full", "https://tei.example.com/embed", true, "https://tei.example.com/embed"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := resolveEmbeddingEndpoint(c.base, c.tei); got != c.want {
				t.Errorf("resolveEmbeddingEndpoint(%q,%v)=%q want %q", c.base, c.tei, got, c.want)
			}
		})
	}
}

// TestEmbeddingTEI_DimensionMismatch 防止把 384 维模型接进 vector(1024) 表。
func TestEmbeddingTEI_DimensionMismatch(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode([][]float32{make([]float32, 384)})
	}))
	defer srv.Close()

	cfg := &EmbeddingConfig{
		BaseURL:        srv.URL,
		Protocol:       "tei",
		Dimension:      1024,
		RequestTimeout: 5,
		MaxRetries:     1,
	}
	svc := NewEmbeddingService()
	_, err := svc.Embed(context.Background(), cfg, []string{"hello"})
	if err == nil || !strings.Contains(err.Error(), "维度不匹配") {
		t.Fatalf("384 维响应应报维度不匹配，实际 err=%v", err)
	}
}
