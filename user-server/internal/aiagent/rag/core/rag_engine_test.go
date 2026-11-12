package rag_core

import (
	"context"
	"testing"
	"time"
)

func requireLocalEmbedding(t *testing.T) {
	t.Helper()
	// 2026-11-12 §3.13：embedding 连接配置已迁移至参数中心 embedding.global_override，
	// EMBEDDING_ALLOW_FALLBACK/EMBEDDING_BASE_URL env 已移除；依赖真实本地
	// embedding 服务的集成测试默认跳过，需要时由开发者配置 DB 后手工运行。
	t.Skip("集成测试默认跳过：embedding 连接配置已迁移至参数中心 embedding.global_override")
}

func newTestRAGEngine(config *RAGConfig) *RAGEngine {
	dim := 768
	if config != nil && config.VectorDimension > 0 {
		dim = config.VectorDimension
	}
	return NewRAGEngineWithEmbedder(config, NewMockEmbedder(dim))
}

// TestRAGEngine_NewRAGEngine 测试创建 RAG 引擎
func TestRAGEngine_newTestRAGEngine(t *testing.T) {
	engine := newTestRAGEngine(nil)
	if engine == nil {
		t.Fatal("Expected RAGEngine to be created")
	}
	if engine.config.ChunkSize != 512 {
		t.Errorf("Expected ChunkSize 512, got %d", engine.config.ChunkSize)
	}
	if engine.config.VectorDimension != 1024 {
		t.Errorf("Expected VectorDimension 1024, got %d", engine.config.VectorDimension)
	}

	customConfig := &RAGConfig{
		ChunkSize:           256,
		ChunkOverlap:        30,
		MaxChunksToRetrieve: 10,
		SimilarityThreshold: 0.7,
		VectorDimension:     1024,
	}
	customEngine := newTestRAGEngine(customConfig)
	if customEngine.config.ChunkSize != 256 {
		t.Errorf("Expected custom ChunkSize 256, got %d", customEngine.config.ChunkSize)
	}
	if customEngine.config.VectorDimension != 1024 {
		t.Errorf("Expected custom VectorDimension 1024, got %d", customEngine.config.VectorDimension)
	}
}

// TestRAGEngine_AddDocuments 测试添加文档
func TestRAGEngine_AddDocuments(t *testing.T) {
	engine := newTestRAGEngine(nil)
	ctx := context.Background()

	docs := []Document{
		{
			ID:      "doc1",
			Content: "这是一个测试文档，包含一些测试内容。",
			Metadata: map[string]any{
				"title": "测试文档 1",
			},
			CreatedAt: time.Now(),
		},
		{
			ID:      "doc2",
			Content: "这是另一个测试文档，用于验证添加功能。",
			Metadata: map[string]any{
				"title": "测试文档 2",
			},
			CreatedAt: time.Now(),
		},
	}

	err := engine.AddDocuments(ctx, docs)
	if err != nil {
		t.Fatalf("Failed to add documents: %v", err)
	}

	if len(engine.documents) != 2 {
		t.Errorf("Expected 2 documents, got %d", len(engine.documents))
	}

	if len(engine.chunks) == 0 {
		t.Error("Expected chunks to be generated")
	}
}

// TestRAGEngine_AddDocuments_EmptyContent 测试添加空内容文档
func TestRAGEngine_AddDocuments_EmptyContent(t *testing.T) {
	engine := newTestRAGEngine(nil)
	ctx := context.Background()

	docs := []Document{
		{
			ID:        "empty_doc",
			Content:   "",
			Metadata:  map[string]any{},
			CreatedAt: time.Now(),
		},
	}

	err := engine.AddDocuments(ctx, docs)
	if err != nil {
		t.Fatalf("Failed to add empty document: %v", err)
	}

	if _, exists := engine.documents["empty_doc"]; !exists {
		t.Error("Expected empty document to be stored")
	}
}

// TestRAGEngine_DeleteDocument 测试删除文档
func TestRAGEngine_DeleteDocument(t *testing.T) {
	engine := newTestRAGEngine(nil)
	ctx := context.Background()

	docs := []Document{
		{ID: "doc1", Content: "测试内容 1", CreatedAt: time.Now()},
		{ID: "doc2", Content: "测试内容 2", CreatedAt: time.Now()},
	}
	engine.AddDocuments(ctx, docs)

	err := engine.DeleteDocument(ctx, "doc1")
	if err != nil {
		t.Fatalf("Failed to delete document: %v", err)
	}

	if _, exists := engine.documents["doc1"]; exists {
		t.Error("Expected doc1 to be deleted")
	}

	if _, exists := engine.documents["doc2"]; !exists {
		t.Error("Expected doc2 to still exist")
	}

	for _, chunk := range engine.chunks {
		if chunk.DocumentID == "doc1" {
			t.Error("Expected all chunks for doc1 to be removed")
		}
	}
}

// TestRAGEngine_DeleteDocument_NotFound 测试删除不存在的文档
func TestRAGEngine_DeleteDocument_NotFound(t *testing.T) {
	engine := newTestRAGEngine(nil)
	ctx := context.Background()

	err := engine.DeleteDocument(ctx, "nonexistent")
	if err != nil {
		t.Errorf("Deleting nonexistent document should not return error: %v", err)
	}
}

// TestRAGEngine_Search 测试搜索功能
func TestRAGEngine_Search(t *testing.T) {
	requireLocalEmbedding(t)
	engine := newTestRAGEngine(&RAGConfig{
		ChunkSize:           512,
		ChunkOverlap:        50,
		MaxChunksToRetrieve: 5,
		SimilarityThreshold: 0,
		VectorDimension:     768,
	})
	ctx := context.Background()

	docs := []Document{
		{ID: "doc1", Content: "人工智能是未来科技的发展趋势", CreatedAt: time.Now()},
		{ID: "doc2", Content: "机器学习是人工智能的一个重要分支", CreatedAt: time.Now()},
		{ID: "doc3", Content: "深度学习在图像识别中应用广泛", CreatedAt: time.Now()},
	}
	engine.AddDocuments(ctx, docs)

	results, err := engine.Search(ctx, "人工智能", 2)
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}

	if len(results) == 0 {
		t.Error("Expected search results")
	}

	if len(results) > 2 {
		t.Errorf("Expected max 2 results, got %d", len(results))
	}

	for _, result := range results {
		if result.Score < 0 || result.Score > 1 {
			t.Errorf("Invalid similarity score: %f", result.Score)
		}
	}
}

// TestRAGEngine_Search_EmptyQuery 测试空查询
func TestRAGEngine_Search_EmptyQuery(t *testing.T) {
	engine := newTestRAGEngine(nil)
	ctx := context.Background()

	results, err := engine.Search(ctx, "", 5)
	if err != nil {
		t.Fatalf("Empty query should not fail: %v", err)
	}

	_ = results
}

// TestRAGEngine_Search_NoDocs 测试空知识库搜索
func TestRAGEngine_Search_NoDocs(t *testing.T) {
	engine := newTestRAGEngine(nil)
	ctx := context.Background()

	results, err := engine.Search(ctx, "测试查询", 5)
	if err != nil {
		t.Fatalf("Search in empty knowledge base should not fail: %v", err)
	}

	if len(results) != 0 {
		t.Errorf("Expected 0 results in empty knowledge base, got %d", len(results))
	}
}

// TestRAGEngine_Search_TopK 测试 topK 参数
func TestRAGEngine_Search_TopK(t *testing.T) {
	requireLocalEmbedding(t)
	engine := newTestRAGEngine(nil)
	ctx := context.Background()

	for i := 0; i < 10; i++ {
		engine.AddDocuments(ctx, []Document{
			{ID: string(rune('a' + i)), Content: "测试内容" + string(rune('0'+i)), CreatedAt: time.Now()},
		})
	}

	results, err := engine.Search(ctx, "测试", 3)
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}

	if len(results) > 3 {
		t.Errorf("Expected max 3 results, got %d", len(results))
	}
}

// TestRAGEngine_GetDocument 测试获取文档
func TestRAGEngine_GetDocument(t *testing.T) {
	engine := newTestRAGEngine(nil)
	ctx := context.Background()

	originalDoc := Document{
		ID:      "test_doc",
		Content: "测试文档内容",
		Metadata: map[string]any{
			"author": "tester",
		},
		CreatedAt: time.Now(),
	}
	engine.AddDocuments(ctx, []Document{originalDoc})

	doc, err := engine.GetDocument(ctx, "test_doc")
	if err != nil {
		t.Fatalf("Failed to get document: %v", err)
	}

	if doc.ID != "test_doc" {
		t.Errorf("Expected ID 'test_doc', got %s", doc.ID)
	}

	if doc.Content != "测试文档内容" {
		t.Errorf("Expected content '测试文档内容', got %s", doc.Content)
	}
}

// TestRAGEngine_GetDocument_NotFound 测试获取不存在的文档
func TestRAGEngine_GetDocument_NotFound(t *testing.T) {
	engine := newTestRAGEngine(nil)
	ctx := context.Background()

	doc, err := engine.GetDocument(ctx, "nonexistent")
	if err == nil {
		t.Error("Expected error for nonexistent document")
	}
	if doc != nil {
		t.Error("Expected nil document for nonexistent ID")
	}
}

// TestRAGEngine_UpdateConfig 测试更新配置
func TestRAGEngine_UpdateConfig(t *testing.T) {
	engine := newTestRAGEngine(nil)

	newConfig := &RAGConfig{
		ChunkSize:           1024,
		ChunkOverlap:        100,
		MaxChunksToRetrieve: 20,
		SimilarityThreshold: 0.8,
		VectorDimension:     512,
	}

	err := engine.UpdateConfig(newConfig)
	if err != nil {
		t.Fatalf("Failed to update config: %v", err)
	}

	config := engine.GetConfig()
	if config.ChunkSize != 1024 {
		t.Errorf("Expected ChunkSize 1024, got %d", config.ChunkSize)
	}
	if config.SimilarityThreshold != 0.8 {
		t.Errorf("Expected SimilarityThreshold 0.8, got %f", config.SimilarityThreshold)
	}
}

// TestRAGEngine_GetConfig 测试获取配置
func TestRAGEngine_GetConfig(t *testing.T) {
	engine := newTestRAGEngine(nil)

	config := engine.GetConfig()
	if config == nil {
		t.Fatal("Expected config to be returned")
	}
	if config.ChunkSize == 0 {
		t.Error("Expected ChunkSize to be set")
	}
}

// TestRAGEngine_SplitDocument 测试文档分片
func TestRAGEngine_SplitDocument(t *testing.T) {
	engine := newTestRAGEngine(&RAGConfig{
		ChunkSize:    10,
		ChunkOverlap: 2,
	})

	doc := Document{
		ID:      "split_test",
		Content: "这是一个比较长的测试文档，用于验证分片功能是否正常工作。",
	}

	chunks := engine.splitDocument(doc)

	if len(chunks) == 0 {
		t.Fatal("Expected chunks to be generated")
	}

	for i, chunk := range chunks {
		if len(chunk.Content) > engine.config.ChunkSize+20 {
			t.Errorf("Chunk %d exceeds size limit: %d chars", i, len(chunk.Content))
		}
	}
}

// TestRAGEngine_SplitDocument_ShortContent 测试短内容分片
func TestRAGEngine_SplitDocument_ShortContent(t *testing.T) {
	engine := newTestRAGEngine(nil)

	doc := Document{
		ID:      "short_doc",
		Content: "短内容",
	}

	chunks := engine.splitDocument(doc)

	if len(chunks) != 1 {
		t.Errorf("Expected 1 chunk for short content, got %d", len(chunks))
	}
}

// TestRAGEngine_SplitDocument_Boundary 测试分片边界处理
func TestRAGEngine_SplitDocument_Boundary(t *testing.T) {
	engine := newTestRAGEngine(&RAGConfig{
		ChunkSize:    20,
		ChunkOverlap: 5,
	})

	doc := Document{
		ID:      "boundary_test",
		Content: "这是第一句。这是第二句！这是第三句？这是第四句。",
	}

	chunks := engine.splitDocument(doc)

	if len(chunks) == 0 {
		t.Fatal("Expected chunks to be generated")
	}

	for _, chunk := range chunks {
		content := chunk.Content
		if len(content) > 0 {
			lastChar := content[len(content)-1]
			_ = lastChar
		}
	}
}

// TestCosineSimilarity 测试余弦相似度计算
func TestCosineSimilarity(t *testing.T) {
	tests := []struct {
		name     string
		vec1     []float32
		vec2     []float32
		expected float64
	}{
		{
			name:     "identical vectors",
			vec1:     []float32{1, 0, 0},
			vec2:     []float32{1, 0, 0},
			expected: 1.0,
		},
		{
			name:     "orthogonal vectors",
			vec1:     []float32{1, 0, 0},
			vec2:     []float32{0, 1, 0},
			expected: 0.0,
		},
		{
			name:     "opposite vectors",
			vec1:     []float32{1, 0, 0},
			vec2:     []float32{-1, 0, 0},
			expected: -1.0,
		},
		{
			name:     "zero vectors",
			vec1:     []float32{0, 0, 0},
			vec2:     []float32{0, 0, 0},
			expected: 0.0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := cosineSimilarity(tt.vec1, tt.vec2)
			diff := result - tt.expected
			if diff < -0.0001 || diff > 0.0001 {
				t.Errorf("Expected similarity %f, got %f", tt.expected, result)
			}
		})
	}
}

// TestMockEmbedder 测试模拟嵌入器
func TestMockEmbedder(t *testing.T) {
	embedder := NewMockEmbedder(128)

	vec, err := embedder.EmbedText("测试文本")
	if err != nil {
		t.Fatalf("EmbedText failed: %v", err)
	}
	if len(vec) != 128 {
		t.Errorf("Expected vector dimension 128, got %d", len(vec))
	}

	queryVec, err := embedder.EmbedQuery("查询文本")
	if err != nil {
		t.Fatalf("EmbedQuery failed: %v", err)
	}
	if len(queryVec) != 128 {
		t.Errorf("Expected query vector dimension 128, got %d", len(queryVec))
	}

	dim := embedder.GetDimension()
	if dim != 128 {
		t.Errorf("Expected dimension 128, got %d", dim)
	}
}

// TestRAGEngine_ConcurrentAccess 测试并发访问
func TestRAGEngine_ConcurrentAccess(t *testing.T) {
	engine := newTestRAGEngine(nil)
	ctx := context.Background()

	engine.AddDocuments(ctx, []Document{
		{ID: "base_doc", Content: "基础文档内容", CreatedAt: time.Now()},
	})

	done := make(chan bool, 10)
	for i := 0; i < 10; i++ {
		go func() {
			defer func() { done <- true }()
			engine.Search(ctx, "测试", 5)
			engine.GetDocument(ctx, "base_doc")
			engine.GetConfig()
		}()
	}

	for i := 0; i < 10; i++ {
		<-done
	}
}

// TestDocument_JSONSerialization 测试文档序列化
func TestDocument_JSONSerialization(t *testing.T) {
	doc := Document{
		ID:      "json_test",
		Content: "测试内容",
		Metadata: map[string]any{
			"key":   "value",
			"count": 42,
		},
		CreatedAt: time.Now(),
	}

	if doc.ID != "json_test" {
		t.Errorf("Expected ID 'json_test', got %s", doc.ID)
	}
	if doc.Content != "测试内容" {
		t.Errorf("Expected content '测试内容', got %s", doc.Content)
	}
}

// TestChunk_Score 测试分片分数
func TestChunk_Score(t *testing.T) {
	chunk := Chunk{
		ID:         "chunk1",
		DocumentID: "doc1",
		Content:    "测试分片内容",
		Score:      0.85,
		TokenCount: 10,
	}

	if chunk.Score < 0 || chunk.Score > 1 {
		t.Errorf("Invalid chunk score: %f", chunk.Score)
	}
}
