package migrations

import (
	"context"
	"fmt"

	"hivemtk-user/internal/migration"
	"hivemtk-user/internal/model"

	"gorm.io/gorm"
)

type KnowledgeReindexOwnerMigration struct {
	db *gorm.DB
}

var _ migration.Migration = (*KnowledgeReindexOwnerMigration)(nil)

func NewKnowledgeReindexOwnerMigration(db *gorm.DB) *KnowledgeReindexOwnerMigration {
	return &KnowledgeReindexOwnerMigration{db: db}
}

func (m *KnowledgeReindexOwnerMigration) Version() string { return "v3.101.0" }

func (m *KnowledgeReindexOwnerMigration) Name() string {
	return "knowledge_documents 索引 owner token"
}

func (m *KnowledgeReindexOwnerMigration) Description() string {
	return "knowledge_documents 新增独立 owner token，隔离过期索引任务的写入"
}

func (m *KnowledgeReindexOwnerMigration) Up(ctx context.Context) error {
	if m.db == nil {
		return fmt.Errorf("db is nil")
	}
	db := m.db.WithContext(ctx)
	if !db.Migrator().HasTable(&model.KnowledgeDocument{}) || db.Migrator().HasColumn(&model.KnowledgeDocument{}, "ReindexOwnerToken") {
		return nil
	}
	if err := db.Migrator().AddColumn(&model.KnowledgeDocument{}, "ReindexOwnerToken"); err != nil {
		return fmt.Errorf("添加 knowledge_documents.reindex_owner_token 失败: %w", err)
	}
	return nil
}

func (m *KnowledgeReindexOwnerMigration) Down(ctx context.Context) error {
	if m.db == nil {
		return fmt.Errorf("db is nil")
	}
	declineColumnDrop(m.Version(), "knowledge_documents.reindex_owner_token")
	return nil
}
