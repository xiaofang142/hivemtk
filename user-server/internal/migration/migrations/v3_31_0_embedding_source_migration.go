package migrations

import (
	"context"
	"fmt"

	"hivemtk-user/internal/migration"

	"gorm.io/gorm"
)

type EmbeddingSourceMigration struct {
	db *gorm.DB
}

var _ migration.Migration = (*EmbeddingSourceMigration)(nil)

func NewEmbeddingSourceMigration(db *gorm.DB) *EmbeddingSourceMigration {
	return &EmbeddingSourceMigration{db: db}
}

func (m *EmbeddingSourceMigration) Version() string { return "v3.31.0" }

func (m *EmbeddingSourceMigration) Name() string {
	return "knowledge_chunks 补 embedding_source 列（向量来源隔离）"
}

func (m *EmbeddingSourceMigration) Description() string {
	return "D16: knowledge_chunks 增加 embedding_source varchar(16) NOT NULL DEFAULT 'tei'，隔离 HashEmbedding 兜底向量"
}

func (m *EmbeddingSourceMigration) Up(ctx context.Context) error {
	if m.db == nil {
		return fmt.Errorf("db is nil")
	}
	if err := m.db.WithContext(ctx).Exec(
		`ALTER TABLE knowledge_chunks ADD COLUMN IF NOT EXISTS embedding_source VARCHAR(16) NOT NULL DEFAULT 'tei'`,
	).Error; err != nil {
		return fmt.Errorf("add embedding_source column failed: %w", err)
	}
	return nil
}

func (m *EmbeddingSourceMigration) Down(ctx context.Context) error {
	if m.db == nil {
		return fmt.Errorf("db is nil")
	}
	declineColumnDrop(m.Version(), "knowledge_chunks.embedding_source")
	return nil
}
