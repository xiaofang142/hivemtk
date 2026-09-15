package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"hivemtk-user/internal/aiagent/embedding"
)

// newTestServer 构造带本地 embedding 引擎的测试服务（不依赖外部容器）
func newTestServer(dim int, model string) *Server {
	return &Server{
		engine:     embedding.NewLocalEmbedding(dim, 42),
		defaultDim: dim,
		defaultMod: model,
		startTime:  time.Now(),
	}
}

// postEmbed 发送 /v1/embeddings 请求并返回响应
func postEmbed(t *testing.T, s *Server, body []byte) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/v1/embeddings", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	s.handleEmbed(rec, req)
	return rec
}

// TestEmbeddingServer_Health 健康检查
func TestEmbeddingServer_Health(t *testing.T) {
	s := newTestServer(768, "BAAI/bge-base-zh-v1.5")
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()
	s.handleHealth(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	var resp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if resp["status"] != "ok" {
		t.Errorf("expected status ok, got %v", resp["status"])
	}
	if int(resp["dim"].(float64)) != 768 {
		t.Errorf("expected dim 768, got %v", resp["dim"])
	}
	if resp["engine"] != "local-ngram-tfidf-randproj" {
		t.Errorf("expected local engine, got %v", resp["engine"])
	}
}

// TestEmbeddingServer_Root 根路径信息
func TestEmbeddingServer_Root(t *testing.T) {
	s := newTestServer(768, "BAAI/bge-base-zh-v1.5")
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	s.handleRoot(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if !bytes.Contains(rec.Body.Bytes(), []byte("embedding-server")) {
		t.Errorf("root response missing banner: %s", rec.Body.String())
	}
}

// TestEmbeddingServer_Embed_Success 正常向量化
func TestEmbeddingServer_Embed_Success(t *testing.T) {
	s := newTestServer(768, "BAAI/bge-base-zh-v1.5")
	body, _ := json.Marshal(EmbeddingRequest{
		Model: "BAAI/bge-base-zh-v1.5",
		Input: []string{"你好世界", "人工智能客服"},
	})
	rec := postEmbed(t, s, body)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp EmbeddingResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if len(resp.Data) != 2 {
		t.Fatalf("expected 2 items, got %d", len(resp.Data))
	}
	for i, item := range resp.Data {
		if len(item.Embedding) != 768 {
			t.Errorf("item %d: expected dim 768, got %d", i, len(item.Embedding))
		}
		if item.Index != i {
			t.Errorf("item %d: expected index %d, got %d", i, i, item.Index)
		}
	}
	if resp.Usage.PromptTokens != len([]rune("你好世界"))+len([]rune("人工智能客服")) {
		t.Errorf("unexpected usage: %+v", resp.Usage)
	}
}

// TestEmbeddingServer_Embed_EmptyInput 空 input 校验
func TestEmbeddingServer_Embed_EmptyInput(t *testing.T) {
	s := newTestServer(768, "BAAI/bge-base-zh-v1.5")
	body, _ := json.Marshal(EmbeddingRequest{Input: []string{}})
	rec := postEmbed(t, s, body)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

// TestEmbeddingServer_Embed_TooMany 超过 256 条校验
func TestEmbeddingServer_Embed_TooMany(t *testing.T) {
	s := newTestServer(768, "BAAI/bge-base-zh-v1.5")
	inputs := make([]string, 257)
	for i := range inputs {
		inputs[i] = "x"
	}
	body, _ := json.Marshal(EmbeddingRequest{Input: inputs})
	rec := postEmbed(t, s, body)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

// TestEmbeddingServer_Embed_WrongMethod 方法校验
func TestEmbeddingServer_Embed_WrongMethod(t *testing.T) {
	s := newTestServer(768, "BAAI/bge-base-zh-v1.5")
	req := httptest.NewRequest(http.MethodGet, "/v1/embeddings", nil)
	rec := httptest.NewRecorder()
	s.handleEmbed(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", rec.Code)
	}
}

// TestEmbeddingServer_Embed_InvalidJSON 非法 JSON
func TestEmbeddingServer_Embed_InvalidJSON(t *testing.T) {
	s := newTestServer(768, "BAAI/bge-base-zh-v1.5")
	rec := postEmbed(t, s, []byte("{not-json"))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

// TestEmbeddingServer_Embed_Deterministic 同输入同向量（本地构建决定性）
func TestEmbeddingServer_Embed_Deterministic(t *testing.T) {
	s := newTestServer(768, "BAAI/bge-base-zh-v1.5")
	body, _ := json.Marshal(EmbeddingRequest{Input: []string{"决定性测试文本"}})

	rec1 := postEmbed(t, s, body)
	rec2 := postEmbed(t, s, body)
	if rec1.Code != 200 || rec2.Code != 200 {
		t.Fatalf("unexpected status: %d / %d", rec1.Code, rec2.Code)
	}
	if rec1.Body.String() != rec2.Body.String() {
		t.Error("expected identical response for identical input")
	}
}

// TestEmbeddingServer_Embed_DefaultModel model 缺省回退到 defaultMod
func TestEmbeddingServer_Embed_DefaultModel(t *testing.T) {
	s := newTestServer(768, "BAAI/bge-base-zh-v1.5")
	body, _ := json.Marshal(map[string]any{"input": []string{"模型回退测试"}})
	rec := postEmbed(t, s, body)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	var resp EmbeddingResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp.Model != "BAAI/bge-base-zh-v1.5" {
		t.Errorf("expected default model echoed, got %s", resp.Model)
	}
}
