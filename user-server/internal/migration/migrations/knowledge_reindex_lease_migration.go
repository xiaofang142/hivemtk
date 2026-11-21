package migrations

import (
	"context"
	"fmt"

	"hivemtk-user/internal/migration"
	"hivemtk-user/internal/model"

	"gorm.io/gorm"
)

// KnowledgeReindexLeaseMigration adds an additive recovery lease for processing documents.
type KnowledgeReindexLeaseMigration struct {
	db *gorm.DB
}

var _ migration.Migration = (*KnowledgeReindexLeaseMigration)(nil)

func NewKnowledgeReindexLeaseMigration(db *gorm.DB) *KnowledgeReindexLeaseMigration {
	return &KnowledgeReindexLeaseMigration{db: db}
}

func (m *KnowledgeReindexLeaseMigration) Version() string { return "v3.100.0" }

func (m *KnowledgeReindexLeaseMigration) Name() string {
	return "knowledge_documents 重建租约"
}

func (m *KnowledgeReindexLeaseMigration) Description() string {
	return "knowledge_documents 新增 reindex_lease_until，支持回收超时 processing 状态"
}

func (m *KnowledgeReindexLeaseMigration) Up(ctx context.Context) error {
	if m.db == nil {
		return fmt.Errorf("db is nil")
	}
	db := m.db.WithContext(ctx)
	if !db.Migrator().HasTable(&model.KnowledgeDocument{}) || db.Migrator().HasColumn(&model.KnowledgeDocument{}, "ReindexLeaseUntil") {
		return nil
	}
	if err := db.Migrator().AddColumn(&model.KnowledgeDocument{}, "ReindexLeaseUntil"); err != nil {
		return fmt.Errorf("添加 knowledge_documents.reindex_lease_until 失败: %w", err)
	}
	return nil
}

func (m *KnowledgeReindexLeaseMigration) Down(ctx context.Context) error {
	if m.db == nil {
		return fmt.Errorf("db is nil")
	}
	declineColumnDrop(m.Version(), "knowledge_documents.reindex_lease_until")
	return nil
}
