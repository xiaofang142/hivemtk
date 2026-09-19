package ragretrieval

import (
	"context"
	"fmt"
	"testing"
	"time"

	"hivemtk-user/internal/aiagent/llm"
	"hivemtk-user/internal/pkg/testutil"

	"gorm.io/gorm"
)

// HybridSearcherTestModels AutoMigrate 模型列表
// 注意：knowledge_chunks 表用 raw SQL 创建（含 pgvector 列），不能用 AutoMigrate
type HybridSearcherTestModels struct{}

// setupHybridTestDB 引导隔离测试库并建好 knowledge_chunks / knowledge_search_logs。
//
// ⚠️ 2026-09-16 审计（TEST-06）：本文件原先在每个用例开头都插了一段
// `if os.Getenv("POSTGRES_TEST_DSN")=="" && os.Getenv("POSTGRES_TEST_HOST")=="" { t.Skip }`
// 前置门（共 7 处）。那段门是**多余且有害**的：
//   - testutil.NewTestDB 本身已实现"不可达则跳过"的语义，且带端口候选探测
//     （8232 宿主机 / 8202 容器内）与 POSTGRES_PASSWORD 回落；
//   - 而这段门只看两个环境变量，二者都没设时**直接跳过** ——
//     于是本地即便 PG 就在 8232 上跑着，这 7 个集成用例也一律 SKIP（exit 0），
//     属 RISK-01 同源的假绿；CI 现在会注入 POSTGRES_TEST_HOST，行为又与本机不一致。
//
// 已删除该前置门，统一由 testutil 决定跳过还是执行。
func setupHybridTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	if testing.Short() {
		t.Skip("skipping PG integration test in short mode")
	}
	db := testutil.NewTestDB(t)

	stmts := []string{
		`CREATE EXTENSION IF NOT EXISTS vector`,
		`DROP TABLE IF EXISTS knowledge_chunks CASCADE`,
		// ⚠️ 本 DDL 必须与**生产** knowledge_chunks 的列集保持一致。
		//
		// 2026-09-16 审计（TEST-07）：原 DDL 只有 14 列，是较早的快照，缺少
		// embedding_source（v3.31.0 迁移 `v3_31_0_embedding_source_migration.go` 新增）
		// 等 10 列。而检索 SQL 会写
		//   WHERE embedding IS NOT NULL AND embedding_source = 'tei'
		// （见 hybrid_searcher.go:295 / vector_retriever.go:63,125），
		// 于是向量检索恒报 column "embedding_source" does not exist (SQLSTATE 42703)，
		// 被上层降级成"空结果 + WARN"，用例表现为 `expected at least 1 result`。
		//
		// 该缺陷之所以长期不可见，正是因为本文件每个用例都插了一段环境变量前置门
		// 让它们一律 SKIP（见 setupHybridTestDB 注释）——门一拆，缺陷立刻现形。
		//
		// 现按实时库 user_db.knowledge_chunks 的 24 列补齐（含 product_id 由 BIGINT 改
		// 回 TEXT：实时库就是 text）。**改动生产 schema 时请同步本 DDL。**
		`CREATE TABLE knowledge_chunks (
			id BIGSERIAL PRIMARY KEY,
			document_id BIGINT NOT NULL DEFAULT 0,
			product_id TEXT,
			chunk_index INT DEFAULT 0,
			content TEXT NOT NULL DEFAULT '',
			content_hash VARCHAR(64),
			token_count BIGINT DEFAULT 0,
			char_count BIGINT DEFAULT 0,
			embedding_id VARCHAR(64),
			similarity_score NUMERIC,
			hit_count BIGINT DEFAULT 0,
			weight DOUBLE PRECISION DEFAULT 1.0,
			metadata JSONB,
			source_language VARCHAR(16),
			translated_versions JSONB,
			created_at TIMESTAMPTZ DEFAULT NOW(),
			embedding vector(1024),
			content_tsv tsvector,
			contextual_context TEXT,
			contextual_tsv tsvector,
			embed_status VARCHAR(20) DEFAULT 'pending',
			updated_at TIMESTAMPTZ DEFAULT NOW(),
			content_tsv_jieba tsvector,
			embedding_source VARCHAR(16) NOT NULL DEFAULT 'tei'
		)`,
		`CREATE INDEX idx_knowledge_chunks_embedding_hnsw ON knowledge_chunks USING hnsw (embedding vector_cosine_ops) WITH (m = 16, ef_construction = 64)`,
		`CREATE INDEX idx_knowledge_chunks_content_tsv ON knowledge_chunks USING GIN (content_tsv)`,
		`CREATE OR REPLACE FUNCTION knowledge_chunks_tsv_trigger() RETURNS trigger AS $$
		BEGIN
			NEW.content_tsv := to_tsvector('simple', coalesce(NEW.content, ''));
			NEW.contextual_tsv := to_tsvector('simple', coalesce(NEW.contextual_context, '') || ' ' || coalesce(NEW.content, ''));
			RETURN NEW;
		END;
		$$ LANGUAGE plpgsql`,
		`CREATE TRIGGER knowledge_chunks_tsv_update
			BEFORE INSERT OR UPDATE ON knowledge_chunks
			FOR EACH ROW EXECUTE FUNCTION knowledge_chunks_tsv_trigger()`,
		`DROP TABLE IF EXISTS knowledge_search_logs`,
		`CREATE TABLE knowledge_search_logs (
			id BIGSERIAL PRIMARY KEY,
			query TEXT,
			product_id BIGINT,
			top_k INT,
			vector_count INT DEFAULT 0,
			bm25_count INT DEFAULT 0,
			fused_count INT DEFAULT 0,
			rerank_count INT DEFAULT 0,
			vector_latency_ms BIGINT DEFAULT 0,
			bm25_latency_ms BIGINT DEFAULT 0,
			rewrite_latency_ms BIGINT DEFAULT 0,
			rerank_latency_ms BIGINT DEFAULT 0,
			rewrite_used VARCHAR(50),
			cache_hit BOOLEAN DEFAULT false,
			created_at TIMESTAMP DEFAULT NOW()
		)`,
	}
	for _, sql := range stmts {
		if err := db.Exec(sql).Error; err != nil {
			t.Fatalf("setup SQL failed (%s): %v", sql, err)
		}
	}
	return db
}

func insertChunk(t *testing.T, db *gorm.DB, docID uint, productID string, content string, embedding []float32) {
	t.Helper()
	vecLiteral := vecToPGString(embedding)
	sql := `
		INSERT INTO knowledge_chunks (document_id, product_id, content, embedding, embed_status, content_tsv)
		VALUES ($1, $2, $3, $4::vector, 'indexed', to_tsvector('simple', $3))
	`
	if err := db.Exec(sql, docID, productID, content, vecLiteral).Error; err != nil {
		t.Fatalf("insert chunk failed: %v", err)
	}
}

func makePrefixVector(dim int, prefixLen int) []float32 {
	v := make([]float32, dim)
	for i := 0; i < prefixLen && i < dim; i++ {
		v[i] = 1.0
	}
	return v
}

func waitUntil(cond func() bool, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for !cond() {
		if !time.Now().Before(deadline) {
			return false
		}
		time.Sleep(20 * time.Millisecond)
	}
	return true
}

// TestHybridSearcher_VectorRetrieve_EndToEnd 集成测试：向量召回端到端
//
// 场景：3 个 chunk 有 embedding，1 个无 embedding；查询向量(全1) 与 chunk100(cos=1.0) 最相似，
// chunk101(cos=0.707) 次之，chunk102(cos=0.25) 最差
// 期望：返回 chunk100 排第一
func TestHybridSearcher_VectorRetrieve_EndToEnd(t *testing.T) {
	db := setupHybridTestDB(t)

	mockEmbed := &mockEmbeddingService{
		vectors: [][]float32{makeFixedVector(1024, 1.0)},
	}
	searcher := NewHybridSearcher(db, mockEmbed, nil, nil, nil, &HybridSearcherConfig{
		DefaultTopK:      5,
		CandidatePool:    50,
		FusedTopN:        20,
		RRFK:             60,
		EfSearch:         80,
		EnableHyDE:       false,
		EnableMultiQuery: false,
		EnableRerank:     false,
	})

	insertChunk(t, db, 100, "1", "如何申请退货退款流程", makeFixedVector(1024, 1.0))
	insertChunk(t, db, 101, "1", "商品保修政策说明", makePrefixVector(1024, 512))
	insertChunk(t, db, 102, "1", "联系方式与客服电话", makePrefixVector(1024, 256))

	out, err := searcher.Search(context.Background(), "如何退货", 5)
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}
	if len(out) == 0 {
		t.Fatal("expected at least 1 result")
	}
	if out[0].DocumentID != "100" {
		t.Errorf("first DocumentID=%s want=100", out[0].DocumentID)
	}
}

func TestHybridSearcher_BM25Retrieve_Fallback(t *testing.T) {
	db := setupHybridTestDB(t)

	mockEmbed := &mockEmbeddingService{err: fmt.Errorf("TEI down")}
	searcher := NewHybridSearcher(db, mockEmbed, nil, nil, nil, &HybridSearcherConfig{
		DefaultTopK:      5,
		CandidatePool:    50,
		FusedTopN:        20,
		RRFK:             60,
		EfSearch:         80,
		EnableHyDE:       false,
		EnableMultiQuery: false,
		EnableRerank:     false,
	})

	insertChunkNoEmbed(t, db, 100, "1", "如何申请退货退款流程")
	insertChunkNoEmbed(t, db, 101, "1", "商品保修政策说明")

	out, err := searcher.Search(context.Background(), "退货", 5)
	if err != nil {
		t.Fatalf("Search should succeed via BM25 fallback: %v", err)
	}
	if len(out) == 0 {
		t.Fatal("expected BM25 to return results")
	}
}

func insertChunkNoEmbed(t *testing.T, db *gorm.DB, docID uint, productID string, content string) {
	t.Helper()
	sql := `
		INSERT INTO knowledge_chunks (document_id, product_id, content, embed_status, content_tsv)
		VALUES ($1, $2, $3, 'pending', to_tsvector('simple', $3))
	`
	if err := db.Exec(sql, docID, productID, content).Error; err != nil {
		t.Fatalf("insert chunk (no embed) failed: %v", err)
	}
}

// TestHybridSearcher_BothFail_ReturnsError 集成测试：两路均失败时返回 error
func TestHybridSearcher_BothFail_ReturnsError(t *testing.T) {
	db := setupHybridTestDB(t)

	mockEmbed := &mockEmbeddingService{err: fmt.Errorf("TEI down")}
	searcher := NewHybridSearcher(db, mockEmbed, nil, nil, nil, &HybridSearcherConfig{
		EnableHyDE:       false,
		EnableMultiQuery: false,
		EnableRerank:     false,
	})

	insertChunkNoEmbed(t, db, 100, "1", "")

	out, err := searcher.Search(context.Background(), "", 5)
	if err != nil {
		t.Logf("Search returned err (acceptable): %v", err)
	}
	if len(out) != 0 {
		t.Errorf("empty query should return empty, got=%d", len(out))
	}
}

// TestHybridSearcher_SearchIndex_WithProductFilter 集成测试：按 product_id 过滤检索
func TestHybridSearcher_SearchIndex_WithProductFilter(t *testing.T) {
	db := setupHybridTestDB(t)

	mockEmbed := &mockEmbeddingService{
		vectors: [][]float32{makeFixedVector(1024, 1.0)},
	}
	searcher := NewHybridSearcher(db, mockEmbed, nil, nil, nil, &HybridSearcherConfig{
		EnableHyDE:       false,
		EnableMultiQuery: false,
		EnableRerank:     false,
	})

	insertChunk(t, db, 100, "1", "产品A的退货流程", makeFixedVector(1024, 1.0))
	insertChunk(t, db, 200, "2", "产品B的退货流程", makeFixedVector(1024, 1.0))

	out, err := searcher.SearchIndex(context.Background(), "1", "退货", 5)
	if err != nil {
		t.Fatalf("SearchIndex failed: %v", err)
	}
	if len(out) == 0 {
		t.Fatal("expected results for product_id=1")
	}
	for _, c := range out {
		if c.DocumentID != "100" {
			t.Errorf("DocumentID=%s want=100 (product_id=1 filter)", c.DocumentID)
		}
	}
}

// TestHybridSearcher_TopKTruncation 集成测试：topK 截断
func TestHybridSearcher_TopKTruncation(t *testing.T) {
	db := setupHybridTestDB(t)

	mockEmbed := &mockEmbeddingService{
		vectors: [][]float32{makeFixedVector(1024, 1.0)},
	}
	searcher := NewHybridSearcher(db, mockEmbed, nil, nil, nil, &HybridSearcherConfig{
		EnableHyDE:       false,
		EnableMultiQuery: false,
		EnableRerank:     false,
	})

	for i := 0; i < 10; i++ {
		insertChunk(t, db, uint(100+i), "1", fmt.Sprintf("chunk-%d", i), makeFixedVector(1024, 1.0))
	}

	out, err := searcher.Search(context.Background(), "test", 3)
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}
	if len(out) > 3 {
		t.Errorf("topK=3 should truncate, got=%d", len(out))
	}

	// 上面那台 searcher 的 FinalTopK 是零值，测不到默认配置的形状。
	// 默认 FinalTopK=5 曾经**覆盖**调用方的 topK（而不是给它设上限），
	// 于是 topK=3 照样回 5 条 —— 线上所有走 DefaultHybridSearcherConfig 的调用都在超发。
	defaultCfgSearcher := NewHybridSearcher(db, mockEmbed, nil, nil, nil, DefaultHybridSearcherConfig())
	outSmall, err := defaultCfgSearcher.Search(context.Background(), "test", 3)
	if err != nil {
		t.Fatalf("默认配置 Search 失败: %v", err)
	}
	if len(outSmall) > 3 {
		t.Errorf("默认配置下 topK=3 应截到 3 条，实得 %d 条（FinalTopK 又去覆盖调用方了）", len(outSmall))
	}

	// 反向一侧也要钉住：FinalTopK 作为**上限**仍然生效，不能退化成完全不设界。
	outLarge, err := defaultCfgSearcher.Search(context.Background(), "test", 50)
	if err != nil {
		t.Fatalf("默认配置大 topK Search 失败: %v", err)
	}
	if len(outLarge) > 5 {
		t.Errorf("FinalTopK=5 的上限失效：topK=50 实得 %d 条", len(outLarge))
	}
}

// TestHybridSearcher_LogSearch_WritesToDB 集成测试：logSearch 写入 knowledge_search_logs
func TestHybridSearcher_LogSearch_WritesToDB(t *testing.T) {
	db := setupHybridTestDB(t)

	mockEmbed := &mockEmbeddingService{
		vectors: [][]float32{makeFixedVector(1024, 1.0)},
	}
	searcher := NewHybridSearcher(db, mockEmbed, nil, nil, nil, &HybridSearcherConfig{
		EnableHyDE:       false,
		EnableMultiQuery: false,
		EnableRerank:     false,
	})

	insertChunk(t, db, 100, "1", "测试内容", makeFixedVector(1024, 1.0))

	_, err := searcher.Search(context.Background(), "测试", 5)
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}

	logWritten := waitUntil(func() bool {
		var count int64
		if err := db.Raw(`SELECT COUNT(*) FROM knowledge_search_logs`).Scan(&count).Error; err != nil {
			t.Fatalf("query logs failed: %v", err)
		}
		return count > 0
	}, 5*time.Second)
	if !logWritten {
		t.Error("knowledge_search_logs should have at least 1 record")
	}
}

// TestHybridSearcher_RerankerFailed_FallbackToFused 集成测试：rerank 失败时回退到融合顺序
func TestHybridSearcher_RerankerFailed_FallbackToFused(t *testing.T) {
	db := setupHybridTestDB(t)

	mockEmbed := &mockEmbeddingService{
		vectors: [][]float32{makeFixedVector(1024, 1.0)},
	}
	failingReranker := &mockReranker{err: fmt.Errorf("rerank service down")}
	searcher := NewHybridSearcher(db, mockEmbed, failingReranker, nil, nil, &HybridSearcherConfig{
		EnableHyDE:       false,
		EnableMultiQuery: false,
		EnableRerank:     true,
	})

	insertChunk(t, db, 100, "1", "测试内容1", makeFixedVector(1024, 1.0))
	insertChunk(t, db, 101, "1", "测试内容2", makeFixedVector(1024, 0.9))

	out, err := searcher.Search(context.Background(), "测试", 5)
	if err != nil {
		t.Fatalf("Search should succeed even when rerank fails: %v", err)
	}
	if len(out) == 0 {
		t.Error("expected results even when rerank fails")
	}

	// 归一化是 min-max：最高分→1、**最低分→0**（见 normalizeRRFScores 与
	// normalize_test.go 的 TestD17b_NormalizeRRFScores「最低分应=0」）。
	// 因此这里该断言的是**值域 + 单调性**，而不是"所有分都必须 > 0"。
	//
	// ⚠️ 2026-09-16 审计（TEST-07）：原断言为 `c.Score <= 0 || c.Score > 1.0001`，
	// 即要求每个结果都 > 0 —— 这与同一提交（61ad8857「D17b：RRF 分数归一化」）
	// 引入的 min→0 设计**直接自相矛盾**：只要结果数 ≥2 且分数不全相同，
	// 末位必被归一化为 0，断言必失败。两者同批写就，却因本文件每个用例都被
	// 环境变量前置门挡住长期 SKIP 而从未对撞（门一拆即现形）。
	//
	// 结论：是**断言错**，不是生产错 —— 生产侧另有 normalize_test.go 明确钉住
	// "最低分应=0"。故此处改为断言真正的不变式，而非放宽以迁就旧断言。
	if len(out) > 0 && out[0].Score != 1.0 {
		t.Errorf("out[0] 归一化后应为 1（最高分），实际 %v", out[0].Score)
	}
	for i, c := range out {
		if c.Score < 0 || c.Score > 1.0001 {
			t.Errorf("out[%d] 归一化后应落在 [0,1]：%v", i, c.Score)
		}
		if i > 0 && c.Score > out[i-1].Score {
			t.Errorf("out[%d].Score=%v 高于前一位 %v，单调性被破坏", i, c.Score, out[i-1].Score)
		}
	}
}

type mockReranker struct {
	err error
}

func (m *mockReranker) Rerank(_ context.Context, _ string, docs []RerankDoc) ([]RerankResult, error) {
	if m.err != nil {
		return nil, m.err
	}
	out := make([]RerankResult, len(docs))
	for i, d := range docs {
		out[i] = RerankResult{ID: d.ID, Score: float64(len(docs) - i)}
	}
	return out, nil
}

var _ RerankerInterface = (*mockReranker)(nil)

var _ = llm.EmbeddingServiceInterface(nil)
