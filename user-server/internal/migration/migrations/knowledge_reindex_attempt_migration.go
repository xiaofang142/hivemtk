package migrations

import (
	"context"
	"fmt"

	"hivemtk-user/internal/migration"
	"hivemtk-user/internal/model"

	"gorm.io/gorm"
)

// KnowledgeReindexAttemptMigration adds the durable retry counter to knowledge documents.
type KnowledgeReindexAttemptMigration struct {
	db *gorm.DB
}

var _ migration.Migration = (*KnowledgeReindexAttemptMigration)(nil)

func NewKnowledgeReindexAttemptMigration(db *gorm.DB) *KnowledgeReindexAttemptMigration {
	return &KnowledgeReindexAttemptMigration{db: db}
}

func (m *KnowledgeReindexAttemptMigration) Version() string { return "v3.99.0" }

func (m *KnowledgeReindexAttemptMigration) Name() string {
	return "knowledge_documents 重建尝试计数"
}

func (m *KnowledgeReindexAttemptMigration) Description() string {
	return "knowledge_documents 新增 reindex_retry_count，持久化每篇文档的重建认领次数"
}

func (m *KnowledgeReindexAttemptMigration) Up(ctx context.Context) error {
	if m.db == nil {
		return fmt.Errorf("db is nil")
	}
	db := m.db.WithContext(ctx)
	if !db.Migrator().HasTable(&model.KnowledgeDocument{}) || db.Migrator().HasColumn(&model.KnowledgeDocument{}, "ReindexRetryCount") {
		return nil
	}
	if err := db.Migrator().AddColumn(&model.KnowledgeDocument{}, "ReindexRetryCount"); err != nil {
		return fmt.Errorf("添加 knowledge_documents.reindex_retry_count 失败: %w", err)
	}
	return nil
}

func (m *KnowledgeReindexAttemptMigration) Down(ctx context.Context) error {
	if m.db == nil {
		return fmt.Errorf("db is nil")
	}
	declineColumnDrop(m.Version(), "knowledge_documents.reindex_retry_count")
	return nil
}
