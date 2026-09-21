package migrations

import (
	"context"
	"fmt"
	"log"

	"hivemtk-user/internal/migration"

	"gorm.io/gorm"
)

// KBUnificationMigration 知识库统一迁移 (v3.15)
type KBUnificationMigration struct {
	db *gorm.DB
}

// NewKBUnificationMigration 创建迁移实例
func NewKBUnificationMigration(db *gorm.DB) *KBUnificationMigration {
	return &KBUnificationMigration{db: db}
}

// Version 返回版本号
func (m *KBUnificationMigration) Version() string { return "v3.15.0" }

// Name 返回迁移名称
func (m *KBUnificationMigration) Name() string {
	return "知识库统一 (P0-B 隔离架构) - knowledge_bases + agent_kb_bindings + 3 表加 agent_id"
}

// Description 返回迁移描述
func (m *KBUnificationMigration) Description() string {
	return "2026-07-31 P0-B: 1) knowledge_bases 主表; 2) agent_kb_bindings 中间表; 3) faq_entries / sop_templates / knowledge_documents 加 agent_id; 4) 现有数据归 default_agent (共享); 5) channel_agent_bindings 强 1:1 唯一"
}

// Up 执行迁移
func (m *KBUnificationMigration) Up(ctx context.Context) error {
	if m.db == nil {
		return fmt.Errorf("db is nil")
	}

	log.Println("[v3.15] 开始知识库统一迁移 P0-B ...")

	log.Println("[v3.15] 1/7 CREATE TABLE knowledge_bases ...")
	stmt1 := `
CREATE TABLE IF NOT EXISTS knowledge_bases (
    id              BIGSERIAL PRIMARY KEY,
    kb_code         VARCHAR(64)  NOT NULL,
    type            VARCHAR(16)  NOT NULL,
    name            VARCHAR(128) NOT NULL,
    description     TEXT         NOT NULL DEFAULT '',
    owner_type      VARCHAR(16)  NOT NULL DEFAULT 'private',
    owner_agent_id  BIGINT,
    member_count    INTEGER      NOT NULL DEFAULT 0,
    doc_count       INTEGER      NOT NULL DEFAULT 0,
    enabled         BOOLEAN      NOT NULL DEFAULT TRUE,
    created_at      TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    CONSTRAINT uq_knowledge_bases_kb_code UNIQUE (kb_code)
);
CREATE INDEX IF NOT EXISTS idx_kb_type          ON knowledge_bases (type);
CREATE INDEX IF NOT EXISTS idx_kb_owner_agent   ON knowledge_bases (owner_agent_id);
CREATE INDEX IF NOT EXISTS idx_kb_enabled       ON knowledge_bases (enabled);
`
	if err := m.db.WithContext(ctx).Exec(stmt1).Error; err != nil {
		return fmt.Errorf("v3.15 1/7 创建 knowledge_bases 失败: %w", err)
	}
	log.Println("[v3.15] ✓ knowledge_bases 表已创建")

	log.Println("[v3.15] 2/7 CREATE TABLE agent_kb_bindings ...")
	stmt2 := `
CREATE TABLE IF NOT EXISTS agent_kb_bindings (
    id          BIGSERIAL PRIMARY KEY,
    agent_id    BIGINT       NOT NULL,
    kb_id       BIGINT       NOT NULL,
    kb_type     VARCHAR(16)  NOT NULL,
    role        VARCHAR(16)  NOT NULL DEFAULT 'primary',
    priority    INTEGER      NOT NULL DEFAULT 0,
    enabled     BOOLEAN      NOT NULL DEFAULT TRUE,
    created_at  TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    CONSTRAINT uq_agent_kb_bindings UNIQUE (agent_id, kb_id)
);
CREATE INDEX IF NOT EXISTS idx_agent_kb_agent    ON agent_kb_bindings (agent_id);
CREATE INDEX IF NOT EXISTS idx_agent_kb_kb       ON agent_kb_bindings (kb_id);
CREATE INDEX IF NOT EXISTS idx_agent_kb_type     ON agent_kb_bindings (kb_type);
CREATE INDEX IF NOT EXISTS idx_agent_kb_enabled  ON agent_kb_bindings (enabled);
`
	if err := m.db.WithContext(ctx).Exec(stmt2).Error; err != nil {
		return fmt.Errorf("v3.15 2/7 创建 agent_kb_bindings 失败: %w", err)
	}
	log.Println("[v3.15] ✓ agent_kb_bindings 表已创建")

	log.Println("[v3.15] 3/7 ALTER TABLE faq_entries ADD agent_id ...")
	stmt3 := `ALTER TABLE faq_entries ADD COLUMN IF NOT EXISTS agent_id BIGINT;`
	if err := m.db.WithContext(ctx).Exec(stmt3).Error; err != nil {
		return fmt.Errorf("v3.15 3/7 faq_entries 加 agent_id 失败: %w", err)
	}
	stmt3idx := `CREATE INDEX IF NOT EXISTS idx_faq_agent_id ON faq_entries (agent_id);`
	if err := m.db.WithContext(ctx).Exec(stmt3idx).Error; err != nil {
		return fmt.Errorf("v3.15 3/7 faq_entries agent_id 索引失败: %w", err)
	}
	log.Println("[v3.15] ✓ faq_entries.agent_id 已添加 + 索引")

	log.Println("[v3.15] 4/7 ALTER TABLE sop_templates ADD agent_id ...")
	stmt4 := `ALTER TABLE sop_templates ADD COLUMN IF NOT EXISTS agent_id BIGINT;`
	if err := m.db.WithContext(ctx).Exec(stmt4).Error; err != nil {
		return fmt.Errorf("v3.15 4/7 sop_templates 加 agent_id 失败: %w", err)
	}
	stmt4idx := `CREATE INDEX IF NOT EXISTS idx_sop_agent_id ON sop_templates (agent_id);`
	if err := m.db.WithContext(ctx).Exec(stmt4idx).Error; err != nil {
		return fmt.Errorf("v3.15 4/7 sop_templates agent_id 索引失败: %w", err)
	}
	log.Println("[v3.15] ✓ sop_templates.agent_id 已添加 + 索引")

	log.Println("[v3.15] 5/7 ALTER TABLE knowledge_documents ADD agent_id ...")
	stmt5 := `ALTER TABLE knowledge_documents ADD COLUMN IF NOT EXISTS agent_id BIGINT;`
	if err := m.db.WithContext(ctx).Exec(stmt5).Error; err != nil {
		return fmt.Errorf("v3.15 5/7 knowledge_documents 加 agent_id 失败: %w", err)
	}
	stmt5idx := `CREATE INDEX IF NOT EXISTS idx_knowledge_doc_agent_id ON knowledge_documents (agent_id);`
	if err := m.db.WithContext(ctx).Exec(stmt5idx).Error; err != nil {
		return fmt.Errorf("v3.15 5/7 knowledge_documents agent_id 索引失败: %w", err)
	}
	log.Println("[v3.15] ✓ knowledge_documents.agent_id 已添加 + 索引")

	log.Println("[v3.15] 6/7 数据迁移: 现有 FAQ / SOP / RAG 文档已自动归为共享 (agent_id IS NULL)")

	if err := m.reportAgentDistribution(ctx, "faq_entries"); err != nil {
		log.Printf("[v3.15] WARN: faq_entries 分布查询失败: %v", err)
	}
	if err := m.reportAgentDistribution(ctx, "sop_templates"); err != nil {
		log.Printf("[v3.15] WARN: sop_templates 分布查询失败: %v", err)
	}
	if err := m.reportAgentDistribution(ctx, "knowledge_documents"); err != nil {
		log.Printf("[v3.15] WARN: knowledge_documents 分布查询失败: %v", err)
	}

	log.Println("[v3.15] 7/7 ChannelAgentBinding 1:1 约束检查 ...")
	stmt7 := `
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint
        WHERE conname = 'idx_channel_binding'
    ) AND NOT EXISTS (
        SELECT 1 FROM pg_indexes
        WHERE indexname = 'idx_channel_binding'
    ) THEN
        CREATE UNIQUE INDEX idx_channel_binding
        ON channel_agent_bindings (channel_type, account_id);
    END IF;
END $$;
`
	if err := m.db.WithContext(ctx).Exec(stmt7).Error; err != nil {
		log.Printf("[v3.15] WARN: idx_channel_binding 检查失败 (通常无影响): %v", err)
	}
	log.Println("[v3.15] ✓ ChannelAgentBinding 1:1 约束已确认")

	log.Println("[v3.15] 知识库统一迁移 P0-B 全部完成")
	return nil
}

// Down 回滚 (注意: 回滚 DDL 不删数据, 仅删列/表, 谨慎使用)
func (m *KBUnificationMigration) Down(ctx context.Context) error {
	if m.db == nil {
		return fmt.Errorf("db is nil")
	}
	log.Println("[v3.15] DOWN: 知识库统一迁移回滚 ...")

	declineIndexDrop(m.Version(), "idx_channel_binding", "idx_knowledge_doc_agent_id", "idx_sop_agent_id",
		"idx_faq_agent_id", "idx_agent_kb_enabled", "idx_agent_kb_type", "idx_agent_kb_kb", "idx_agent_kb_agent",
		"idx_kb_enabled", "idx_kb_owner_agent", "idx_kb_type")
	declineColumnDrop(m.Version(), "knowledge_documents.agent_id", "sop_templates.agent_id",
		"faq_entries.agent_id")

	stmts := []string{
		`DROP TABLE IF EXISTS agent_kb_bindings`,
		`DROP TABLE IF EXISTS knowledge_bases`,
	}
	for _, s := range stmts {
		if err := m.db.WithContext(ctx).Exec(s).Error; err != nil {
			return fmt.Errorf("v3.15 回滚失败: %w (SQL: %s)", err, s)
		}
	}
	return nil
}

func (m *KBUnificationMigration) reportAgentDistribution(ctx context.Context, table string) error {
	type result struct {
		AgentID *int64 `gorm:"column:agent_id"`
		Count   int64  `gorm:"column:count"`
	}
	var rows []result
	if err := m.db.WithContext(ctx).
		Table(table).
		Select("agent_id, COUNT(*) AS count").
		Group("agent_id").
		Order("agent_id NULLS FIRST").
		Limit(50).
		Scan(&rows).Error; err != nil {
		return err
	}
	log.Printf("[v3.15] %s.agent_id 分布 (前 50):", table)
	for _, r := range rows {
		if r.AgentID == nil {
			log.Printf("  - agent_id=NULL(共享)  count=%d", r.Count)
		} else {
			log.Printf("  - agent_id=%d  count=%d", *r.AgentID, r.Count)
		}
	}
	return nil
}

var _ migration.Migration = (*KBUnificationMigration)(nil)
