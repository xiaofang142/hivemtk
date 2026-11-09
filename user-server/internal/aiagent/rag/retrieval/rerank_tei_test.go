package ragretrieval

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestTEIRerank_RequestAndResponse TEI 原生协议（公开 TEI Space 等）：
// 校验端点不补 /v1、请求体形状 {query,texts,truncate}（不带 model）、
// 响应数组 [{index,score}] 向既有 rerankResponse 的转换与分数过滤。
func TestTEIRerank_RequestAndResponse(t *testing.T) {
	var gotPath, gotMethod, gotAuth, gotCT string
	var gotBody map[string]any

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotMethod = r.Method
		gotAuth = r.Header.Get("Authorization")
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
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[
			{"index": 1, "score": 0.05},
			{"index": 0, "score": 0.95}
		]`))
	}))
	defer srv.Close()

	cfg := &RerankConfig{
		APIKey:     "tei-test-key",
		BaseURL:    srv.URL,
		Model:      "BAAI/bge-reranker-base",
		Enabled:    true,
		Timeout:    5,
		MaxRetries: 1,
		Protocol:   "tei",
	}
	reranker := NewLocalRerankerWithConfig(cfg)

	docs := []RerankDoc{
		{ID: "d0", Content: "hello"},
		{ID: "d1", Content: "world"},
	}
	results, err := reranker.Rerank(context.Background(), "greeting", docs)
	if err != nil {
		t.Fatalf("rerank: %v", err)
	}

	// 端点 = base + /rerank（不补 /v1、不重复拼接）
	if gotPath != "/rerank" {
		t.Errorf("path=%s want=/rerank", gotPath)
	}
	if strings.Contains(gotPath, "/v1/") {
		t.Errorf("TEI 协议不应补 /v1: %s", gotPath)
	}
	if gotMethod != http.MethodPost {
		t.Errorf("method=%s want=POST", gotMethod)
	}
	if gotCT != "application/json" {
		t.Errorf("Content-Type=%s want=application/json", gotCT)
	}
	if gotAuth != "Bearer tei-test-key" {
		t.Errorf("Authorization=%q want=%q", gotAuth, "Bearer tei-test-key")
	}

	// 请求体形状：query + texts + truncate；TEI schema 不要求 model，故不发送
	if gotBody["query"] != "greeting" {
		t.Errorf("query=%v want=greeting", gotBody["query"])
	}
	texts, ok := gotBody["texts"].([]any)
	if !ok || len(texts) != 2 || texts[0] != "hello" || texts[1] != "world" {
		t.Errorf("texts=%v want=[hello world]", gotBody["texts"])
	}
	if gotBody["truncate"] != true {
		t.Errorf("truncate=%v want=true", gotBody["truncate"])
	}
	if _, exists := gotBody["model"]; exists {
		t.Errorf("TEI 请求体不应包含 model 字段: %v", gotBody)
	}

	// 响应转换：0.95 ≥ floor 保留、0.05 < floor(0.3) 被过滤；分数原样（0~1 概率域不过 sigmoid）
	if len(results) != 1 {
		t.Fatalf("len(results)=%d want=1（0.05 低于 rerankScoreFloor 应被过滤），results=%+v", len(results), results)
	}
	if results[0].ID != "d0" {
		t.Errorf("results[0].ID=%s want=d0", results[0].ID)
	}
	if results[0].Score != 0.95 {
		t.Errorf("results[0].Score=%v want=0.95（0~1 概率域原样通过）", results[0].Score)
	}
}

// TestTEIRerank_EndpointResolution TEI 端点解析：
// 裸 host → +/rerank；已带 /rerank 的完整端点 → 原样；尾部斜杠归一；Normalize 不改写协议。
func TestTEIRerank_EndpointResolution(t *testing.T) {
	cases := []struct {
		name string
		base string
		want string
	}{
		{"裸 host", "https://tei.example.com", "https://tei.example.com/rerank"},
		{"裸 host 尾斜杠", "https://tei.example.com/", "https://tei.example.com/rerank"},
		{"带路径前缀", "https://tei.example.com/api", "https://tei.example.com/api/rerank"},
		{"完整端点", "https://tei.example.com/rerank", "https://tei.example.com/rerank"},
		{"完整端点尾斜杠", "https://tei.example.com/rerank/", "https://tei.example.com/rerank"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			cfg := &RerankConfig{Protocol: "tei", BaseURL: c.base}
			NormalizeRerankConfig(cfg, false)
			if cfg.Protocol != teiProtocol {
				t.Errorf("Normalize 不应改写 TEI 协议: %s", cfg.Protocol)
			}
			if got := resolveRerankEndpoint(cfg); got != c.want {
				t.Errorf("resolveRerankEndpoint(%q)=%q want %q", c.base, got, c.want)
			}
		})
	}
}
