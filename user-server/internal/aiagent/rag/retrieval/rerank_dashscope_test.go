package ragretrieval

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

// TestDashscopeRerank_RequestAndResponse DashScope 原生协议：
// 校验端点不做 /v1 补齐、请求体形状、Bearer 鉴权，以及原生响应向 rerankResponse 的转换
// （0~1 概率域分数原样通过 normalizeRerankScores，rerankScoreFloor 仍生效）。
func TestDashscopeRerank_RequestAndResponse(t *testing.T) {
	const dashscopePath = "/api/v1/services/rerank/text-rerank/text-rerank"

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
		_, _ = w.Write([]byte(`{
			"output": {"results": [
				{"index": 1, "relevance_score": 0.05},
				{"index": 0, "relevance_score": 0.95}
			]},
			"code": "",
			"message": ""
		}`))
	}))
	defer srv.Close()

	cfg := &RerankConfig{
		APIKey:     "sk-te" + "st-key",
		BaseURL:    srv.URL + dashscopePath,
		Model:      "qwen3-rerank",
		Enabled:    true,
		Timeout:    5,
		MaxRetries: 1,
		Protocol:   "dashscope",
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

	// 端点 = BaseURL 原样（不做 /v1 补齐、不拼 /rerank）
	if gotPath != dashscopePath {
		t.Errorf("path=%s want=%s", gotPath, dashscopePath)
	}
	if strings.HasSuffix(gotPath, "/rerank/rerank") {
		t.Errorf("不应重复拼接 /rerank: %s", gotPath)
	}
	if gotMethod != http.MethodPost {
		t.Errorf("method=%s want=POST", gotMethod)
	}
	if gotCT != "application/json" {
		t.Errorf("Content-Type=%s want=application/json", gotCT)
	}
	if gotAuth != "Bearer sk-test-key" {
		t.Errorf("Authorization=%q want=%q", gotAuth, "Bearer sk-test-key")
	}

	// 请求体形状：model + input{query,documents} + parameters{return_documents}
	if gotBody["model"] != "qwen3-rerank" {
		t.Errorf("model=%v want=qwen3-rerank", gotBody["model"])
	}
	input, ok := gotBody["input"].(map[string]any)
	if !ok {
		t.Fatalf("input 字段缺失或类型错误: %v", gotBody)
	}
	if input["query"] != "greeting" {
		t.Errorf("input.query=%v want=greeting", input["query"])
	}
	docsArr, ok := input["documents"].([]any)
	if !ok || len(docsArr) != 2 || docsArr[0] != "hello" || docsArr[1] != "world" {
		t.Errorf("input.documents=%v want=[hello world]", input["documents"])
	}
	params, ok := gotBody["parameters"].(map[string]any)
	if !ok {
		t.Fatalf("parameters 字段缺失或类型错误: %v", gotBody)
	}
	if params["return_documents"] != false {
		t.Errorf("parameters.return_documents=%v want=false", params["return_documents"])
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

// TestDashscopeRerank_ResponseCodeError DashScope 响应 code 非空视为错误。
// MaxRetries=1 避免重试退避拖慢用例。
func TestDashscopeRerank_ResponseCodeError(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"output": {"results": []},
			"code": "InvalidParameter.Error",
			"message": "invalid model name"
		}`))
	}))
	defer srv.Close()

	cfg := &RerankConfig{
		BaseURL:    srv.URL + "/api/v1/services/rerank/text-rerank/text-rerank",
		Model:      "qwen3-rerank",
		Enabled:    true,
		Timeout:    5,
		MaxRetries: 1,
		Protocol:   "dashscope",
	}
	reranker := NewLocalRerankerWithConfig(cfg)

	_, err := reranker.Rerank(context.Background(), "q", []RerankDoc{{ID: "d0", Content: "c"}})
	if err == nil {
		t.Fatalf("code 非空应报错，实际 calls=%d", atomic.LoadInt32(&calls))
	}
	if !strings.Contains(err.Error(), "InvalidParameter.Error") {
		t.Errorf("错误应包含 code，实际: %v", err)
	}
	if !strings.Contains(err.Error(), "invalid model name") {
		t.Errorf("错误应包含 message，实际: %v", err)
	}
}

// TestDashscopeProtocolDetection 协议判定：dashscope（大小写/空白容忍）走原生，
// 空与 openai 均走 OpenAI 兼容现状。
func TestDashscopeProtocolDetection(t *testing.T) {
	for _, p := range []string{"dashscope", " DASHSCOPE ", "DashScope"} {
		if !isDashscopeProtocol(p) {
			t.Errorf("isDashscopeProtocol(%q) 应为 true", p)
		}
	}
	for _, p := range []string{"", "openai", "OPENAI", "tei"} {
		if isDashscopeProtocol(p) {
			t.Errorf("isDashscopeProtocol(%q) 应为 false", p)
		}
	}
}
