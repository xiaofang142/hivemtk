package migrations

import (
	"context"
	"fmt"
	"gorm.io/gorm"
	"hivemtk-user/internal/migration"
	"hivemtk-user/internal/model"
)

type KnowledgeSearchObservationMigration struct{ db *gorm.DB }

func NewKnowledgeSearchObservationMigration(db *gorm.DB) *KnowledgeSearchObservationMigration {
	return &KnowledgeSearchObservationMigration{db: db}
}

func (m *KnowledgeSearchObservationMigration) Version() string { return "v3.97.0" }
func (m *KnowledgeSearchObservationMigration) Name() string {
	return "检索字符串身份与分步观测"
}
func (m *KnowledgeSearchObservationMigration) Description() string { return m.Name() }
func (m *KnowledgeSearchObservationMigration) Up(ctx context.Context) error {
	if m.db == nil || ctx == nil {
		return fmt.Errorf("migration database or context unavailable")
	}
	return m.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if tx.Migrator().HasTable(&model.KnowledgeSearchLog{}) && tx.Migrator().HasColumn(&model.KnowledgeSearchLog{}, "product_id") {
			for _, statement := range []string{
				`ALTER TABLE knowledge_search_logs ALTER COLUMN product_id DROP DEFAULT`,
				`ALTER TABLE knowledge_search_logs ALTER COLUMN product_id DROP NOT NULL`,
				`ALTER TABLE knowledge_search_logs ALTER COLUMN product_id TYPE text USING product_id::text`,
			} {
				if err := tx.Exec(statement).Error; err != nil {
					return err
				}
			}
		}
		if err := tx.AutoMigrate(&model.KnowledgeSearchLog{}); err != nil {
			return err
		}
		for _, column := range []string{"embedding_latency_ms", "vector_latency_ms", "bm25_latency_ms", "rrf_latency_ms", "rerank_latency_ms", "parent_latency_ms", "rewrite_latency_ms"} {
			if err := tx.Exec("ALTER TABLE knowledge_search_logs ALTER COLUMN " + column + " DROP NOT NULL, ALTER COLUMN " + column + " DROP DEFAULT").Error; err != nil {
				return err
			}
		}
		return nil
	})
}
func (m *KnowledgeSearchObservationMigration) Down(context.Context) error { return nil }

var _ migration.Migration = (*KnowledgeSearchObservationMigration)(nil)
