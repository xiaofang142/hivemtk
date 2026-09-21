package migrations

import (
	"context"
	"strings"
	"testing"

	"hivemtk-user/internal/pkg/testutil"

	"gorm.io/gorm"
)

func setupMigrationTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	return testutil.NewTestDB(t)
}

func setupKnowledgeChunksBase(t *testing.T, db *gorm.DB) {
	t.Helper()
	stmts := []string{
		`CREATE EXTENSION IF NOT EXISTS vector`,
		`DROP TABLE IF EXISTS knowledge_chunks CASCADE`,
		`CREATE TABLE knowledge_chunks (
			id BIGSERIAL PRIMARY KEY,
			document_id BIGINT NOT NULL DEFAULT 0,
			product_id BIGINT NOT NULL DEFAULT 0,
			content TEXT NOT NULL DEFAULT '',
			chunk_index INT DEFAULT 0,
			embedding vector(1024),
			embedding_id VARCHAR(64),
			created_at TIMESTAMP DEFAULT NOW(),
			updated_at TIMESTAMP DEFAULT NOW()
		)`,
		`CREATE INDEX idx_knowledge_chunks_embedding_hnsw ON knowledge_chunks USING hnsw (embedding vector_cosine_ops)`,
	}
	for _, sql := range stmts {
		if err := db.Exec(sql).Error; err != nil {
			t.Fatalf("setup base schema failed (%s): %v", sql, err)
		}
	}
}

// TestRagHybridMigration_Version 验证版本号
func TestRagHybridMigration_Version(t *testing.T) {
	m := NewRagHybridMigration(nil)
	if m.Version() != "v2.7.0" {
		t.Errorf("Version()=%q want=v2.7.0", m.Version())
	}
	if m.Name() == "" {
		t.Error("Name() should not be empty")
	}
	if m.Description() == "" {
		t.Error("Description() should not be empty")
	}
}

// TestRagHybridMigration_UpCreatesAllTables 集成测试：Up 创建所有目标表
func TestRagHybridMigration_UpCreatesAllTables(t *testing.T) {
	db := setupMigrationTestDB(t)
	setupKnowledgeChunksBase(t, db)

	m := NewRagHybridMigration(db)
	if err := m.Up(context.Background()); err != nil {
		t.Fatalf("Up() failed: %v", err)
	}

	var exists bool
	if err := db.Raw(`SELECT EXISTS (SELECT 1 FROM information_schema.tables WHERE table_name = 'query_rewrite_cache')`).Scan(&exists).Error; err != nil || !exists {
		t.Errorf("query_rewrite_cache table should exist after Up(): exists=%v err=%v", exists, err)
	}

	if err := db.Raw(`SELECT EXISTS (SELECT 1 FROM information_schema.tables WHERE table_name = 'embedding_cache')`).Scan(&exists).Error; err != nil || !exists {
		t.Errorf("embedding_cache table should exist after Up(): exists=%v err=%v", exists, err)
	}

	var hasCol bool
	if err := db.Raw(`SELECT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name = 'knowledge_chunks' AND column_name = 'content_tsv')`).Scan(&hasCol).Error; err != nil || !hasCol {
		t.Errorf("knowledge_chunks.content_tsv column should exist after Up(): hasCol=%v err=%v", hasCol, err)
	}
	if err := db.Raw(`SELECT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name = 'knowledge_chunks' AND column_name = 'contextual_context')`).Scan(&hasCol).Error; err != nil || !hasCol {
		t.Errorf("knowledge_chunks.contextual_context column should exist after Up(): hasCol=%v err=%v", hasCol, err)
	}
	if err := db.Raw(`SELECT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name = 'knowledge_chunks' AND column_name = 'embed_status')`).Scan(&hasCol).Error; err != nil || !hasCol {
		t.Errorf("knowledge_chunks.embed_status column should exist after Up(): hasCol=%v err=%v", hasCol, err)
	}
}

// TestRagHybridMigration_UpIdempotent 集成测试：Up 幂等
func TestRagHybridMigration_UpIdempotent(t *testing.T) {
	db := setupMigrationTestDB(t)
	setupKnowledgeChunksBase(t, db)

	m := NewRagHybridMigration(db)
	if err := m.Up(context.Background()); err != nil {
		t.Fatalf("first Up() failed: %v", err)
	}
	if err := m.Up(context.Background()); err != nil {
		t.Fatalf("second Up() should be idempotent: %v", err)
	}
	if err := m.Up(context.Background()); err != nil {
		t.Fatalf("third Up() should be idempotent: %v", err)
	}
}

// TestRagHybridMigration_TriggerMaintainsTSV 集成测试：触发器自动维护 content_tsv
func TestRagHybridMigration_TriggerMaintainsTSV(t *testing.T) {
	db := setupMigrationTestDB(t)
	setupKnowledgeChunksBase(t, db)

	m := NewRagHybridMigration(db)
	if err := m.Up(context.Background()); err != nil {
		t.Fatalf("Up() failed: %v", err)
	}

	if sqlDB, err := db.DB(); err == nil {
		if _, err := sqlDB.Exec("SET session_replication_role = 'origin'"); err != nil {
			t.Logf("重置 session_replication_role 提示: %v", err)
		}
	}

	if err := db.Exec(`INSERT INTO knowledge_chunks (document_id, content) VALUES ($1, $2)`, 100, "如何申请退货退款流程").Error; err != nil {
		t.Fatalf("insert chunk failed: %v", err)
	}

	var notNullRow struct {
		TsvOK  bool `gorm:"column:tsv_ok"`
		HashOK bool `gorm:"column:hash_ok"`
	}
	if err := db.Raw(`SELECT content_tsv IS NOT NULL AS tsv_ok, content_hash IS NOT NULL AS hash_ok FROM knowledge_chunks WHERE document_id = 100`).Scan(&notNullRow).Error; err != nil {
		t.Fatalf("query content_tsv/content_hash failed: %v", err)
	}
	if !notNullRow.TsvOK {
		t.Error("content_tsv should be auto-maintained by trigger")
	}
	if !notNullRow.HashOK {
		t.Error("content_hash should be auto-maintained by trigger")
	}
}

// TestRagHybridMigration_QueryRewriteCacheWritable 集成测试：query_rewrite_cache 表可读写
func TestRagHybridMigration_QueryRewriteCacheWritable(t *testing.T) {
	db := setupMigrationTestDB(t)
	setupKnowledgeChunksBase(t, db)

	m := NewRagHybridMigration(db)
	if err := m.Up(context.Background()); err != nil {
		t.Fatalf("Up() failed: %v", err)
	}

	err := db.Exec(`
		INSERT INTO query_rewrite_cache (query_hash, original_query, hyde_doc, multi_queries, rewrite_model, rewrite_type, hit_count, expires_at)
		VALUES ($1, $2, $3, $4::jsonb, $5, $6, 0, NOW() + INTERVAL '30 days')
	`, "abc123", "如何退货", "假设性文档", `["v1","v2"]`, "local", "hyde").Error
	if err != nil {
		t.Fatalf("insert query_rewrite_cache failed: %v", err)
	}

	var row struct {
		HyDEDoc     string `gorm:"column:hyde_doc"`
		HitCount    int64  `gorm:"column:hit_count"`
		RewriteType string `gorm:"column:rewrite_type"`
	}
	err = db.Raw(`SELECT hyde_doc, hit_count, rewrite_type FROM query_rewrite_cache WHERE query_hash = $1`, "abc123").Scan(&row).Error
	if err != nil {
		t.Fatalf("query query_rewrite_cache failed: %v", err)
	}
	if row.HyDEDoc != "假设性文档" {
		t.Errorf("hyde_doc=%q want=假设性文档", row.HyDEDoc)
	}
	if row.RewriteType != "hyde" {
		t.Errorf("rewrite_type=%q want=hyde", row.RewriteType)
	}

	err = db.Exec(`
		INSERT INTO query_rewrite_cache (query_hash, original_query) VALUES ($1, $2)
	`, "abc123", "重复").Error
	if err == nil {
		t.Error("should fail on duplicate query_hash (UNIQUE constraint)")
	}

	err = db.Exec(`
		INSERT INTO query_rewrite_cache (query_hash, original_query, hyde_doc, rewrite_type)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (query_hash) DO UPDATE SET
			hyde_doc = EXCLUDED.hyde_doc,
			rewrite_type = EXCLUDED.rewrite_type
	`, "abc123", "如何退货", "新假设文档", "hyde_multiquery").Error
	if err != nil {
		t.Fatalf("ON CONFLICT update failed: %v", err)
	}
	var afterRow struct {
		HyDEDoc     string `gorm:"column:hyde_doc"`
		RewriteType string `gorm:"column:rewrite_type"`
	}
	err = db.Raw(`SELECT hyde_doc, rewrite_type FROM query_rewrite_cache WHERE query_hash = $1`, "abc123").Scan(&afterRow).Error
	if err != nil {
		t.Fatalf("query after ON CONFLICT failed: %v", err)
	}
	if afterRow.HyDEDoc != "新假设文档" || afterRow.RewriteType != "hyde_multiquery" {
		t.Errorf("after ON CONFLICT: hyde_doc=%q rewrite_type=%q", afterRow.HyDEDoc, afterRow.RewriteType)
	}
}

// TestRagHybridMigration_EmbeddingCacheWritable 集成测试：embedding_cache 表可读写
func TestRagHybridMigration_EmbeddingCacheWritable(t *testing.T) {
	db := setupMigrationTestDB(t)
	setupKnowledgeChunksBase(t, db)

	m := NewRagHybridMigration(db)
	if err := m.Up(context.Background()); err != nil {
		t.Fatalf("Up() failed: %v", err)
	}

	vecParts := make([]string, 1024)
	vecParts[0] = "0.1"
	vecParts[1] = "0.2"
	vecParts[2] = "0.3"
	for i := 3; i < 1024; i++ {
		vecParts[i] = "0.0"
	}
	vec := "[" + strings.Join(vecParts, ",") + "]"

	err := db.Exec(`
		INSERT INTO embedding_cache (text_hash, text_content, model, dimension, embedding, hit_count, expires_at)
		VALUES ($1, $2, $3, $4, $5::vector, 0, NOW() + INTERVAL '30 days')
	`, "hash123", "hello", "bge-m3", 1024, vec).Error
	if err != nil {
		t.Fatalf("insert embedding_cache failed: %v", err)
	}

	var dim int
	err = db.Raw(`SELECT dimension FROM embedding_cache WHERE text_hash = $1 AND model = $2`, "hash123", "bge-m3").Scan(&dim).Error
	if err != nil {
		t.Fatalf("query embedding_cache failed: %v", err)
	}
	if dim != 1024 {
		t.Errorf("dimension=%d want=1024", dim)
	}

	err = db.Exec(`
		INSERT INTO embedding_cache (text_hash, text_content, model, dimension, embedding)
		VALUES ($1, $2, $3, $4, $5::vector)
	`, "hash123", "dup", "bge-m3", 1024, vec).Error
	if err == nil {
		t.Error("should fail on duplicate (text_hash, model) UNIQUE constraint")
	}
}

// TestRagHybridMigration_DownCleansUp 集成测试：Down 清理
func TestRagHybridMigration_DownCleansUp(t *testing.T) {
	db := setupMigrationTestDB(t)
	setupKnowledgeChunksBase(t, db)

	m := NewRagHybridMigration(db)
	if err := m.Up(context.Background()); err != nil {
		t.Fatalf("Up() failed: %v", err)
	}
	if err := m.Down(context.Background()); err != nil {
		t.Fatalf("Down() failed: %v", err)
	}

	var exists bool
	if err := db.Raw(`SELECT EXISTS (SELECT 1 FROM information_schema.tables WHERE table_name = 'query_rewrite_cache')`).Scan(&exists).Error; err != nil || exists {
		t.Errorf("query_rewrite_cache should be dropped after Down(): exists=%v", exists)
	}
	if err := db.Raw(`SELECT EXISTS (SELECT 1 FROM information_schema.tables WHERE table_name = 'embedding_cache')`).Scan(&exists).Error; err != nil || exists {
		t.Errorf("embedding_cache should be dropped after Down(): exists=%v", exists)
	}

	var hasCol bool
	if err := db.Raw(`SELECT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name = 'knowledge_chunks' AND column_name = 'content_tsv')`).Scan(&hasCol).Error; err != nil || !hasCol {
		// 本包口径：Down 不销毁 knowledge_chunks 上的列——该表由模型/AutoMigrate 持有，
		// 降级删列等于销毁在用数据（判据见 a_full_chain_migration_test.go）。
		t.Errorf("content_tsv column must survive Down(): hasCol=%v", hasCol)
	}

	if err := db.Raw(`SELECT EXISTS (SELECT 1 FROM information_schema.tables WHERE table_name = 'knowledge_chunks')`).Scan(&exists).Error; err != nil || !exists {
		t.Errorf("knowledge_chunks base table should still exist after Down(): exists=%v", exists)
	}
}

// TestRagHybridMigration_KnowledgeSearchLogsEnhanced 集成测试：knowledge_search_logs 表增强字段
func TestRagHybridMigration_KnowledgeSearchLogsEnhanced(t *testing.T) {
	db := setupMigrationTestDB(t)
	setupKnowledgeChunksBase(t, db)

	m := NewRagHybridMigration(db)
	if err := m.Up(context.Background()); err != nil {
		t.Fatalf("Up() failed: %v", err)
	}

	err := db.Exec(`
		INSERT INTO knowledge_search_logs
			(query, product_id, top_k, vector_count, bm25_count, fused_count, rerank_count,
			 vector_latency_ms, bm25_latency_ms, rewrite_latency_ms, rerank_latency_ms,
			 rewrite_used, cache_hit)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
	`, "如何退货", 1, 5, 10, 8, 18, 5, 120, 80, 200, 90, "hyde_multiquery", true).Error
	if err != nil {
		t.Fatalf("insert knowledge_search_logs failed: %v", err)
	}

	var logRow struct {
		VectorCount int    `gorm:"column:vector_count"`
		BM25Count   int    `gorm:"column:bm25_count"`
		FusedCount  int    `gorm:"column:fused_count"`
		RewriteUsed string `gorm:"column:rewrite_used"`
		CacheHit    bool   `gorm:"column:cache_hit"`
	}
	err = db.Raw(`SELECT vector_count, bm25_count, fused_count, rewrite_used, cache_hit FROM knowledge_search_logs ORDER BY id DESC LIMIT 1`).Scan(&logRow).Error
	if err != nil {
		t.Fatalf("query knowledge_search_logs failed: %v", err)
	}
	if logRow.VectorCount != 10 || logRow.BM25Count != 8 || logRow.FusedCount != 18 {
		t.Errorf("counts: vec=%d bm25=%d fused=%d", logRow.VectorCount, logRow.BM25Count, logRow.FusedCount)
	}
	if logRow.RewriteUsed != "hyde_multiquery" {
		t.Errorf("rewrite_used=%q want=hyde_multiquery", logRow.RewriteUsed)
	}
	if !logRow.CacheHit {
		t.Error("cache_hit should be true")
	}
}
