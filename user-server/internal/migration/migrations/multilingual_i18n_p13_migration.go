package migrations

import (
	"context"
	"fmt"

	"hivemtk-user/internal/migration"

	"gorm.io/gorm"
)

// MultilingualI18nP13Migration 多语言 i18n 迁移（v3.12.1）
type MultilingualI18nP13Migration struct {
	db *gorm.DB
}

// NewMultilingualI18nP13Migration 创建迁移实例
func NewMultilingualI18nP13Migration(db *gorm.DB) *MultilingualI18nP13Migration {
	return &MultilingualI18nP13Migration{db: db}
}

// Version 返回版本号
func (m *MultilingualI18nP13Migration) Version() string { return "v3.12.1" }

// Name 返回迁移名称
func (m *MultilingualI18nP13Migration) Name() string {
	return "多语言 i18n P1-3 - knowledge_chunks.translated_versions 字段"
}

// Description 返回迁移描述
func (m *MultilingualI18nP13Migration) Description() string {
	return "v1.2 出海多语言方案 P1-3：为 knowledge_chunks 新增 translated_versions JSONB 字段，存储高频条目预翻译版本"
}

// Up 执行升级
func (m *MultilingualI18nP13Migration) Up(ctx context.Context) error {
	if m.db == nil {
		return fmt.Errorf("db is nil")
	}
	stmts := []string{
		`ALTER TABLE knowledge_chunks ADD COLUMN IF NOT EXISTS translated_versions JSONB`,
		`CREATE INDEX IF NOT EXISTS idx_knowledge_chunks_translated_versions ON knowledge_chunks USING GIN (translated_versions)`,
	}
	for _, s := range stmts {
		if err := m.db.WithContext(ctx).Exec(s).Error; err != nil {
			return fmt.Errorf("multilingual_i18n_p13 执行失败 (%s): %w", truncate(s, 60), err)
		}
	}
	return nil
}

// Down 回滚
func (m *MultilingualI18nP13Migration) Down(ctx context.Context) error {
	if m.db == nil {
		return fmt.Errorf("db is nil")
	}
	declineIndexDrop(m.Version(), "idx_knowledge_chunks_translated_versions")
	declineColumnDrop(m.Version(), "knowledge_chunks.translated_versions")
	return nil
}

var _ migration.Migration = (*MultilingualI18nP13Migration)(nil)
