package rag_service

import (
	"context"
	"os"
	"testing"

	"hivemtk-user/internal/aiagent/llm"
	rag_core "hivemtk-user/internal/aiagent/rag/core"
)

type mockThreeTier struct {
	results []*ThreeTierResult
	err     error
	calls   int
}

func (m *mockThreeTier) Search(ctx context.Context, kbID, query string, topK int) (*ThreeTierResult, error) {
	m.calls++
	if m.err != nil {
		return nil, m.err
	}
	if len(m.results) == 0 {
		return &ThreeTierResult{Chunks: []rag_core.Chunk{}, Source: "mock"}, nil
	}
	r := m.results[0]
	if len(m.results) > 1 {
		r = m.results[m.calls%len(m.results)]
	}
	return r, nil
}

func (m *mockThreeTier) Stats() ThreeTierStats {
	return ThreeTierStats{Total: int64(m.calls), L2Hits: int64(m.calls)}
}

func requireRealAPIKey(t *testing.T) {
	t.Helper()
	if os.Getenv("OPENAI_API_KEY") == "" {
		t.Skip("需要 OPENAI_API_KEY 环境变量，跳过此集成测试")
	}
}

func TestNewRAGService(t *testing.T) {
	llmSvc := llm.NewLLMService()
	ragEngine := rag_core.NewRAGEngine(nil)
	svc := NewRAGService(llmSvc, ragEngine)
	if svc == nil {
		t.Error("Expected non-nil RAGService")
	}
}

func TestRAGService_Query_WithContext(t *testing.T) {
	requireRealAPIKey(t)
	llmSvc := llm.NewLLMService()
	ragEngine := rag_core.NewRAGEngine(nil)
	svc := NewRAGService(llmSvc, ragEngine)

	req := QueryRequest{
		Query: "What is marketing?",
		LLMConfig: &llm.LLMConfig{
			APIKey:    os.Getenv("OPENAI_API_KEY"),
			MaxTokens: 100,
			Model:     "gpt-4",
		},
		Context: map[string]any{
			"user_id": "test-merchant",
		},
	}

	resp, err := svc.Query(context.Background(), &req)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if resp == nil {
		t.Fatal("Expected non-nil response")
	}
	if resp.Answer == "" {
		t.Error("Expected non-empty answer")
	}
}

func TestRAGService_Query_EmptyQuery(t *testing.T) {
	requireRealAPIKey(t)
	llmSvc := llm.NewLLMService()
	ragEngine := rag_core.NewRAGEngine(nil)
	svc := NewRAGService(llmSvc, ragEngine)

	req := QueryRequest{
		Query: "",
		LLMConfig: &llm.LLMConfig{
			APIKey:    os.Getenv("OPENAI_API_KEY"),
			MaxTokens: 100,
			Model:     "gpt-4",
		},
	}

	resp, err := svc.Query(context.Background(), &req)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if resp == nil {
		t.Error("Expected non-nil response even for empty query")
	}
}

func TestRAGService_Query_WithLLMConfig(t *testing.T) {
	requireRealAPIKey(t)
	llmSvc := llm.NewLLMService()
	ragEngine := rag_core.NewRAGEngine(nil)
	svc := NewRAGService(llmSvc, ragEngine)

	req := QueryRequest{
		Query: "Test query",
		LLMConfig: &llm.LLMConfig{
			APIKey:    os.Getenv("OPENAI_API_KEY"),
			MaxTokens: 100,
			Model:     "gpt-4",
		},
	}

	resp, err := svc.Query(context.Background(), &req)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if resp == nil {
		t.Error("Expected non-nil response")
	}
}

func TestQueryRequest_Fields(t *testing.T) {
	req := QueryRequest{
		Query: "test",
		Context: map[string]any{
			"key": "value",
		},
	}
	if req.Query != "test" {
		t.Errorf("Expected Query 'test', got '%s'", req.Query)
	}
	if req.Context["key"] != "value" {
		t.Error("Expected Context[key] = 'value'")
	}
}

func TestQueryResponse_Fields(t *testing.T) {
	resp := QueryResponse{
		Answer:     "test answer",
		References: []rag_core.Chunk{},
		Metadata:   map[string]any{"key": "value"},
	}
	if resp.Answer != "test answer" {
		t.Errorf("Expected Answer 'test answer', got '%s'", resp.Answer)
	}
	if len(resp.References) != 0 {
		t.Errorf("Expected empty References, got %d", len(resp.References))
	}
}

func TestRAGService_SetThreeTier(t *testing.T) {
	llmSvc := llm.NewLLMService()
	ragEngine := rag_core.NewRAGEngine(nil)
	svc := NewRAGService(llmSvc, ragEngine)
	if svc.HasThreeTier() {
		t.Error("expected no three tier initially")
	}
	mock := &mockThreeTier{}
	svc.SetThreeTier(mock)
	if !svc.HasThreeTier() {
		t.Error("expected three tier after set")
	}
}

func TestRAGService_ThreeTierStats(t *testing.T) {
	llmSvc := llm.NewLLMService()
	ragEngine := rag_core.NewRAGEngine(nil)
	svc := NewRAGService(llmSvc, ragEngine)

	_, ok := svc.ThreeTierStats()
	if ok {
		t.Error("expected false when no three tier")
	}

	mock := &mockThreeTier{}
	svc.SetThreeTier(mock)
	stats, ok := svc.ThreeTierStats()
	if !ok {
		t.Error("expected true after set")
	}
	if stats.Total == 0 {
		_ = stats
	}
}

func TestRAGService_Retrieve_ThreeTier(t *testing.T) {
	llmSvc := llm.NewLLMService()
	ragEngine := rag_core.NewRAGEngine(nil)
	svc := NewRAGService(llmSvc, ragEngine)

	mock := &mockThreeTier{
		results: []*ThreeTierResult{{
			Chunks: []rag_core.Chunk{{ID: "c1", Content: "hello"}},
			Source: "L2_warm_index",
		}},
	}
	svc.SetThreeTier(mock)

	chunks, source, err := svc.retrieve(context.Background(), "kb1", "hello", 5)
	if err != nil {
		t.Fatalf("retrieve: %v", err)
	}
	if len(chunks) != 1 {
		t.Errorf("expected 1 chunk, got %d", len(chunks))
	}
	if source != "L2_warm_index" {
		t.Errorf("expected L2 source, got %s", source)
	}
	if mock.calls != 1 {
		t.Errorf("expected 1 call, got %d", mock.calls)
	}
}

func TestRAGService_Retrieve_NoKBID_NoThreeTier(t *testing.T) {
	llmSvc := llm.NewLLMService()
	ragEngine := rag_core.NewRAGEngineWithEmbedder(nil, rag_core.NewMockEmbedder(128))
	svc := NewRAGService(llmSvc, ragEngine)

	mock := &mockThreeTier{}
	svc.SetThreeTier(mock)
	_, source, err := svc.retrieve(context.Background(), "", "hello", 5)
	if err != nil {
		t.Fatalf("retrieve: %v", err)
	}
	if source != "engine" {
		t.Errorf("expected engine source, got %s", source)
	}
	if mock.calls != 0 {
		t.Errorf("mock should not be called without KBID, got %d calls", mock.calls)
	}
}

func TestRAGService_Retrieve_ThreeTierError_Fallback(t *testing.T) {
	llmSvc := llm.NewLLMService()
	ragEngine := rag_core.NewRAGEngineWithEmbedder(nil, rag_core.NewMockEmbedder(128))
	svc := NewRAGService(llmSvc, ragEngine)

	mock := &mockThreeTier{err: &dummyError{}}
	svc.SetThreeTier(mock)
	_, source, _ := svc.retrieve(context.Background(), "kb1", "hello", 5)
	if source != "engine" {
		t.Errorf("expected engine source (fallback), got %s", source)
	}
}

type dummyError struct{}

func (d *dummyError) Error() string { return "mock error" }

func TestRAGService_QueryRequest_KBIDField(t *testing.T) {
	req := QueryRequest{Query: "hi", KBID: "kb1", TopK: 10}
	if req.KBID != "kb1" {
		t.Errorf("expected kb1, got %s", req.KBID)
	}
	if req.TopK != 10 {
		t.Errorf("expected 10, got %d", req.TopK)
	}
}

func TestThreeTierResult_Fields(t *testing.T) {
	r := &ThreeTierResult{
		Query:     "test",
		Chunks:    []rag_core.Chunk{{ID: "c1"}},
		Source:    "L1_hot_cache",
		Score:     0.95,
		LatencyMs: 5,
		FromCache: true,
	}
	if r.Query != "test" || r.Source != "L1_hot_cache" {
		t.Errorf("field mismatch: %+v", r)
	}
	if !r.FromCache {
		t.Error("expected FromCache true")
	}
}
