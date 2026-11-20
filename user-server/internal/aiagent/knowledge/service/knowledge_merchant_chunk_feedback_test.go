package service

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"hivemtk-user/internal/aiagent/knowledge/model"
	"hivemtk-user/internal/aiagent/llm"
)

func TestReindexChunksAfterWriteMarksFailureAndHidesProviderError(t *testing.T) {
	providerErr := errors.New("provider secret detail")
	chunks := []model.KnowledgeChunk{
		{ID: 21, ProductID: "p-1"},
		{ID: 22, ProductID: "p-1"},
	}
	var marked []uint64

	err := reindexChunksAfterWrite(
		context.Background(),
		"p-1",
		chunks,
		func(context.Context, string, []model.KnowledgeChunk) error { return providerErr },
		func(_ context.Context, ids []uint64) error {
			marked = append(marked, ids...)
			return nil
		},
	)
	if !errors.Is(err, ErrChunkIndexingFailed) {
		t.Fatalf("expected safe indexing failure sentinel, got %v", err)
	}
	if err == nil || strings.Contains(err.Error(), providerErr.Error()) {
		t.Fatalf("provider detail must not escape in returned error, got %v", err)
	}
	if len(marked) != 2 || marked[0] != 21 || marked[1] != 22 {
		t.Fatalf("failed chunks = %v, want [21 22]", marked)
	}
}

func TestReindexChunksAfterWriteDoesNotMarkSuccessFailed(t *testing.T) {
	marked := false
	err := reindexChunksAfterWrite(
		context.Background(),
		"p-1",
		[]model.KnowledgeChunk{{ID: 21, ProductID: "p-1"}},
		func(context.Context, string, []model.KnowledgeChunk) error { return nil },
		func(context.Context, []uint64) error { marked = true; return nil },
	)
	if err != nil {
		t.Fatalf("successful reindex returned error: %v", err)
	}
	if marked {
		t.Fatal("successful reindex must not mark chunks failed")
	}
}

func TestChunkReembedRejectsProviderVectorsFilteredByModelDimension(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		vector := make([]float32, 1024)
		if err := json.NewEncoder(w).Encode([][]float32{vector}); err != nil {
			t.Errorf("encode embedding response: %v", err)
		}
	}))
	defer server.Close()

	cfg := &llm.EmbeddingConfig{
		BaseURL:        server.URL,
		Protocol:       "tei",
		Model:          "text-embedding-3-small",
		Dimension:      1024,
		RequestTimeout: 2,
		MaxRetries:     1,
	}
	embeddingService := llm.NewEmbeddingServiceWithConfig(cfg)
	vectors, _, err := embeddingService.EmbedWithSource(context.Background(), cfg, []string{"answer"})
	if err != nil {
		t.Fatalf("local TEI embedding request failed: %v", err)
	}

	persisted := false
	err = persistValidatedChunkEmbeddings(
		embeddingService,
		cfg,
		[]model.KnowledgeChunk{{Content: "answer"}},
		vectors,
		func(_ []model.KnowledgeChunk, _ [][]float32) error {
			persisted = true
			return nil
		},
	)
	if err == nil {
		t.Fatal("a provider vector rejected by the model-dimension guard must not be reported as ready to persist")
	}
	if !strings.Contains(err.Error(), "维度") {
		t.Fatalf("expected dimension validation error, got %v", err)
	}
	if persisted {
		t.Fatal("dimension-invalid vectors must not be persisted as a partial batch")
	}
}
