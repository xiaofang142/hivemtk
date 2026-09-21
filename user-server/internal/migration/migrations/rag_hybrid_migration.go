package migrations

import (
	"context"
	"fmt"

	"hivemtk-user/internal/migration"
	"hivemtk-user/internal/pkg/utils/logger"

	"gorm.io/gorm"
)

// RagHybridMigration RAG 混合检索迁移 v2.7.0
type RagHybridMigration struct {
	db *gorm.DB
}

// NewRagHybridMigration 创建迁移实例
func NewRagHybridMigration(db *gorm.DB) *RagHybridMigration {
	return &RagHybridMigration{db: db}
}

// Version 返回版本号
func (m *RagHybridMigration) Version() string { return "v2.7.0" }

// Name 返回迁移名称
func (m *RagHybridMigration) Name() string { return "RAG 混合检索（tsvector + 缓存表）" }

// Description 返回迁移描述
func (m *RagHybridMigration) Description() string {
	return "为 knowledge_chunks 增加 tsvector 列/触发器，新建 query_rewrite_cache / embedding_cache 表，增强 knowledge_search_logs 监控字段"
}

// Up 执行升级
func (m *RagHybridMigration) Up(ctx context.Context) error {
	if m.db == nil {
		return fmt.Errorf("db is nil")
	}

	if err := m.installPgcrypto(ctx); err != nil {
		logger.Infof("[RagHybridMigration] pgcrypto 扩展安装提示（content_hash 自动维护将不可用）: %v", err)
	}

	if err := m.installZhparser(ctx); err != nil {
		logger.Infof("[RagHybridMigration] zhparser 扩展安装提示（可忽略，将用 simple 兜底）: %v", err)
	}

	if err := m.createZhRagConfig(ctx); err != nil {
		logger.Infof("[RagHybridMigration] zh_rag 配置创建提示: %v", err)
	}

	if err := m.enhanceKnowledgeChunks(ctx); err != nil {
		return fmt.Errorf("enhance knowledge_chunks 失败: %w", err)
	}

	if err := m.createQueryRewriteCache(ctx); err != nil {
		return fmt.Errorf("create query_rewrite_cache 失败: %w", err)
	}

	if err := m.createEmbeddingCache(ctx); err != nil {
		return fmt.Errorf("create embedding_cache 失败: %w", err)
	}

	if err := m.enhanceKnowledgeSearchLogs(ctx); err != nil {
		return fmt.Errorf("enhance knowledge_search_logs 失败: %w", err)
	}

	return nil
}

func (m *RagHybridMigration) installPgcrypto(ctx context.Context) error {
	var installed bool
	if err := m.db.WithContext(ctx).Raw(
		`SELECT EXISTS (SELECT 1 FROM pg_extension WHERE extname = 'pgcrypto')`,
	).Scan(&installed).Error; err != nil {
		return err
	}
	if installed {
		return nil
	}
	return m.db.WithContext(ctx).Exec(`CREATE EXTENSION IF NOT EXISTS pgcrypto`).Error
}

func (m *RagHybridMigration) installZhparser(ctx context.Context) error {

	var installed bool
	if err := m.db.WithContext(ctx).Raw(
		`SELECT EXISTS (SELECT 1 FROM pg_extension WHERE extname = 'zhparser')`,
	).Scan(&installed).Error; err != nil {
		return err
	}
	if installed {
		return nil
	}
	return m.db.WithContext(ctx).Exec(`CREATE EXTENSION IF NOT EXISTS zhparser`).Error
}

func (m *RagHybridMigration) createZhRagConfig(ctx context.Context) error {

	var installed bool
	if err := m.db.WithContext(ctx).Raw(
		`SELECT EXISTS (SELECT 1 FROM pg_extension WHERE extname = 'zhparser')`,
	).Scan(&installed).Error; err != nil || !installed {
		return nil
	}

	stmt := `
		DO $$
		BEGIN
			IF NOT EXISTS (
				SELECT 1 FROM pg_ts_config WHERE cfgname = 'zh_rag'
			) THEN
				CREATE TEXT SEARCH CONFIGURATION zh_rag (PARSER = zhparser);
				ALTER TEXT SEARCH CONFIGURATION zh_rag ADD MAPPING FOR n,v,a,i,e,l WITH simple;
			END IF;
		END $$;
	`
	return m.db.WithContext(ctx).Exec(stmt).Error
}

func (m *RagHybridMigration) enhanceKnowledgeChunks(ctx context.Context) error {
	stmts := []string{
		`ALTER TABLE knowledge_chunks ADD COLUMN IF NOT EXISTS content_tsv tsvector`,
		`ALTER TABLE knowledge_chunks ADD COLUMN IF NOT EXISTS contextual_context text`,
		`ALTER TABLE knowledge_chunks ADD COLUMN IF NOT EXISTS contextual_tsv tsvector`,
		`ALTER TABLE knowledge_chunks ADD COLUMN IF NOT EXISTS content_hash varchar(64)`,
		`ALTER TABLE knowledge_chunks ADD COLUMN IF NOT EXISTS embed_status varchar(20) DEFAULT 'pending'`,
	}
	for _, sql := range stmts {
		if err := m.db.WithContext(ctx).Exec(sql).Error; err != nil {
			return fmt.Errorf("add column failed (%s): %w", sql, err)
		}
	}

	indexes := []string{
		`CREATE INDEX IF NOT EXISTS idx_knowledge_chunks_content_hash ON knowledge_chunks(content_hash)`,
		`CREATE INDEX IF NOT EXISTS idx_knowledge_chunks_embedding_id ON knowledge_chunks(embedding_id)`,
		`CREATE INDEX IF NOT EXISTS idx_knowledge_chunks_embed_status ON knowledge_chunks(embed_status)`,
		`CREATE INDEX IF NOT EXISTS idx_knowledge_chunks_content_tsv ON knowledge_chunks USING GIN (content_tsv)`,
		`CREATE INDEX IF NOT EXISTS idx_knowledge_chunks_contextual_tsv ON knowledge_chunks USING GIN (contextual_tsv)`,
	}
	for _, sql := range indexes {
		if err := m.db.WithContext(ctx).Exec(sql).Error; err != nil {
			return fmt.Errorf("create index failed (%s): %w", sql, err)
		}
	}

	triggerStmt := `
		DROP TRIGGER IF EXISTS knowledge_chunks_tsv_update ON knowledge_chunks;

		CREATE OR REPLACE FUNCTION knowledge_chunks_tsv_trigger() RETURNS trigger AS $$
		BEGIN
			-- 优先 zh_rag，不存在则 simple
			IF EXISTS (SELECT 1 FROM pg_ts_config WHERE cfgname = 'zh_rag') THEN
				NEW.content_tsv := to_tsvector('zh_rag', coalesce(NEW.content, ''));
				NEW.contextual_tsv := to_tsvector('zh_rag', coalesce(NEW.contextual_context, '') || ' ' || coalesce(NEW.content, ''));
			ELSE
				NEW.content_tsv := to_tsvector('simple', coalesce(NEW.content, ''));
				NEW.contextual_tsv := to_tsvector('simple', coalesce(NEW.contextual_context, '') || ' ' || coalesce(NEW.content, ''));
			END IF;
			-- 自动维护 content_hash（若未设置）
			IF NEW.content_hash IS NULL AND NEW.content IS NOT NULL THEN
				NEW.content_hash := encode(digest(coalesce(NEW.content, ''), 'sha256'), 'hex');
			END IF;
			RETURN NEW;
		END;
		$$ LANGUAGE plpgsql;

		CREATE TRIGGER knowledge_chunks_tsv_update
			BEFORE INSERT OR UPDATE ON knowledge_chunks
			FOR EACH ROW EXECUTE FUNCTION knowledge_chunks_tsv_trigger();
	`
	if err := m.db.WithContext(ctx).Exec(triggerStmt).Error; err != nil {
		return fmt.Errorf("create trigger failed: %w", err)
	}

	backfill := `
		UPDATE knowledge_chunks
		SET content_tsv = CASE
			WHEN EXISTS (SELECT 1 FROM pg_ts_config WHERE cfgname = 'zh_rag')
			THEN to_tsvector('zh_rag', coalesce(content, ''))
			ELSE to_tsvector('simple', coalesce(content, ''))
		END,
		contextual_tsv = CASE
			WHEN EXISTS (SELECT 1 FROM pg_ts_config WHERE cfgname = 'zh_rag')
			THEN to_tsvector('zh_rag', coalesce(contextual_context, '') || ' ' || coalesce(content, ''))
			ELSE to_tsvector('simple', coalesce(contextual_context, '') || ' ' || coalesce(content, ''))
		END
		WHERE content_tsv IS NULL
	`
	_ = m.db.WithContext(ctx).Exec(backfill).Error

	return nil
}

func (m *RagHybridMigration) createQueryRewriteCache(ctx context.Context) error {
	stmt := `
		CREATE TABLE IF NOT EXISTS query_rewrite_cache (
			id              BIGSERIAL PRIMARY KEY,
			query_hash      VARCHAR(64)  NOT NULL UNIQUE,
			original_query  TEXT         NOT NULL,
			hyde_doc        TEXT,
			multi_queries   JSONB,
			rewrite_model   VARCHAR(100),
			rewrite_type    VARCHAR(50),
			hit_count       BIGINT       DEFAULT 0,
			last_used_at    TIMESTAMP,
			expires_at      TIMESTAMP,
			created_at      TIMESTAMP    DEFAULT NOW(),
			updated_at      TIMESTAMP    DEFAULT NOW()
		);
		CREATE INDEX IF NOT EXISTS idx_query_rewrite_cache_expires ON query_rewrite_cache(expires_at);
		CREATE INDEX IF NOT EXISTS idx_query_rewrite_cache_last_used ON query_rewrite_cache(last_used_at);
	`
	return m.db.WithContext(ctx).Exec(stmt).Error
}

func (m *RagHybridMigration) createEmbeddingCache(ctx context.Context) error {

	var extCount int64
	if err := m.db.WithContext(ctx).Raw(
		`SELECT COUNT(*) FROM pg_extension WHERE extname = 'vector'`,
	).Scan(&extCount).Error; err != nil {
		return fmt.Errorf("查询 pgvector 扩展失败: %w", err)
	}
	if extCount == 0 {
		if err := m.db.WithContext(ctx).Exec(`CREATE EXTENSION IF NOT EXISTS vector`).Error; err != nil {
			return fmt.Errorf("pgvector 扩展未安装且创建失败: %w", err)
		}
	}

	stmt := `
		CREATE TABLE IF NOT EXISTS embedding_cache (
			id              BIGSERIAL PRIMARY KEY,
			text_hash       VARCHAR(64)  NOT NULL,
			text_content    TEXT         NOT NULL,
			model           VARCHAR(100) NOT NULL,
			dimension       INT          NOT NULL DEFAULT 1024,
			embedding       vector(1024) NOT NULL,
			hit_count       BIGINT       DEFAULT 0,
			last_used_at    TIMESTAMP,
			expires_at      TIMESTAMP,
			created_at      TIMESTAMP    DEFAULT NOW(),
			updated_at      TIMESTAMP    DEFAULT NOW(),
			CONSTRAINT uk_embedding_cache_hash_model UNIQUE (text_hash, model)
		);
		CREATE INDEX IF NOT EXISTS idx_embedding_cache_expires ON embedding_cache(expires_at);
		CREATE INDEX IF NOT EXISTS idx_embedding_cache_last_used ON embedding_cache(last_used_at);
	`
	return m.db.WithContext(ctx).Exec(stmt).Error
}

func (m *RagHybridMigration) enhanceKnowledgeSearchLogs(ctx context.Context) error {

	var tableExists bool
	if err := m.db.WithContext(ctx).Raw(
		`SELECT EXISTS (SELECT 1 FROM information_schema.tables WHERE table_name = 'knowledge_search_logs')`,
	).Scan(&tableExists).Error; err != nil {
		return err
	}
	if !tableExists {
		stmt := `
			CREATE TABLE knowledge_search_logs (
				id                  BIGSERIAL PRIMARY KEY,
				query               TEXT,
				product_id          BIGINT,
				top_k               INT,
				vector_count        INT      DEFAULT 0,
				bm25_count          INT      DEFAULT 0,
				fused_count         INT      DEFAULT 0,
				rerank_count        INT      DEFAULT 0,
				vector_latency_ms   BIGINT   DEFAULT 0,
				bm25_latency_ms     BIGINT   DEFAULT 0,
				rewrite_latency_ms  BIGINT   DEFAULT 0,
				rerank_latency_ms   BIGINT   DEFAULT 0,
				rewrite_used        VARCHAR(50),
				cache_hit           BOOLEAN  DEFAULT false,
				created_at          TIMESTAMP DEFAULT NOW()
			);
			CREATE INDEX IF NOT EXISTS idx_knowledge_search_logs_created ON knowledge_search_logs(created_at);
			CREATE INDEX IF NOT EXISTS idx_knowledge_search_logs_product ON knowledge_search_logs(product_id);
		`
		return m.db.WithContext(ctx).Exec(stmt).Error
	}

	stmts := []string{
		`ALTER TABLE knowledge_search_logs ADD COLUMN IF NOT EXISTS vector_count INT DEFAULT 0`,
		`ALTER TABLE knowledge_search_logs ADD COLUMN IF NOT EXISTS bm25_count INT DEFAULT 0`,
		`ALTER TABLE knowledge_search_logs ADD COLUMN IF NOT EXISTS fused_count INT DEFAULT 0`,
		`ALTER TABLE knowledge_search_logs ADD COLUMN IF NOT EXISTS rerank_count INT DEFAULT 0`,
		`ALTER TABLE knowledge_search_logs ADD COLUMN IF NOT EXISTS vector_latency_ms BIGINT DEFAULT 0`,
		`ALTER TABLE knowledge_search_logs ADD COLUMN IF NOT EXISTS bm25_latency_ms BIGINT DEFAULT 0`,
		`ALTER TABLE knowledge_search_logs ADD COLUMN IF NOT EXISTS rewrite_latency_ms BIGINT DEFAULT 0`,
		`ALTER TABLE knowledge_search_logs ADD COLUMN IF NOT EXISTS rerank_latency_ms BIGINT DEFAULT 0`,
		`ALTER TABLE knowledge_search_logs ADD COLUMN IF NOT EXISTS rewrite_used VARCHAR(50)`,
		`ALTER TABLE knowledge_search_logs ADD COLUMN IF NOT EXISTS cache_hit BOOLEAN DEFAULT false`,
		`ALTER TABLE knowledge_search_logs ADD COLUMN IF NOT EXISTS product_id BIGINT`,
		`ALTER TABLE knowledge_search_logs ADD COLUMN IF NOT EXISTS top_k INT`,
	}
	for _, sql := range stmts {
		_ = m.db.WithContext(ctx).Exec(sql).Error
	}
	_ = m.db.WithContext(ctx).Exec(`CREATE INDEX IF NOT EXISTS idx_knowledge_search_logs_created ON knowledge_search_logs(created_at)`).Error
	_ = m.db.WithContext(ctx).Exec(`CREATE INDEX IF NOT EXISTS idx_knowledge_search_logs_product ON knowledge_search_logs(product_id)`).Error
	return nil
}

// Down 执行降级
//
// 注意:
//   - 不删除 knowledge_chunks 表（业务数据，仅删除新增列）
//   - 删除 query_rewrite_cache / embedding_cache 缓存表（仅缓存数据，可重建）
//   - 不删除 knowledge_search_logs 表（历史监控数据）
func (m *RagHybridMigration) Down(ctx context.Context) error {
	_ = m.db.WithContext(ctx).Exec(`DROP TRIGGER IF EXISTS knowledge_chunks_tsv_update ON knowledge_chunks`).Error
	_ = m.db.WithContext(ctx).Exec(`DROP FUNCTION IF EXISTS knowledge_chunks_tsv_trigger()`).Error

	declineIndexDrop(m.Version(), "idx_knowledge_chunks_content_hash", "idx_knowledge_chunks_embedding_id",
		"idx_knowledge_chunks_embed_status", "idx_knowledge_chunks_content_tsv", "idx_knowledge_chunks_contextual_tsv")
	declineColumnDrop(m.Version(), "knowledge_chunks.content_tsv", "knowledge_chunks.contextual_context",
		"knowledge_chunks.contextual_tsv", "knowledge_chunks.content_hash", "knowledge_chunks.embed_status")

	_ = m.db.WithContext(ctx).Exec(`DROP TABLE IF EXISTS query_rewrite_cache`).Error
	_ = m.db.WithContext(ctx).Exec(`DROP TABLE IF EXISTS embedding_cache`).Error

	return nil
}

var _ migration.Migration = (*RagHybridMigration)(nil)
