package migrations

import (
	"context"
	"fmt"

	"hivemtk-user/internal/migration"

	"gorm.io/gorm"
)

// LP1Migration L 域 缺口修复迁移
type LP1Migration struct {
	db *gorm.DB
}

// NewLP1Migration 创建迁移实例
func NewLP1Migration(db *gorm.DB) *LP1Migration {
	return &LP1Migration{db: db}
}

// Version 返回版本号
func (m *LP1Migration) Version() string { return "v3.3.0" }

// Name 返回迁移名称
func (m *LP1Migration) Name() string { return "L 域 P1 缺口修复 - 第三方对接模板" }

// Description 返回迁移描述
func (m *LP1Migration) Description() string {
	return "创建 integration_templates 表，支撑 ERP/CRM 预置对接模板（钉钉/企微/飞书/用友/金蝶/管家婆/SAP）的 JSON 导入导出与字段映射"
}

// Up 执行升级
func (m *LP1Migration) Up(ctx context.Context) error {
	if m.db == nil {
		return fmt.Errorf("db is nil")
	}
	stmts := []string{
		// 列名以模型为准：BuiltIn ⇒ built_in，is_built_in 只是 JSON tag。
		// 生产建表走 AutoMigrate，这里写错列名会让下面的索引必红。
		`CREATE TABLE IF NOT EXISTS integration_templates (
			id          BIGSERIAL PRIMARY KEY,
			code        VARCHAR(64) NOT NULL UNIQUE,
			platform    VARCHAR(32) NOT NULL,
			category    VARCHAR(32) NOT NULL DEFAULT 'erp',
			name        VARCHAR(128) NOT NULL,
			version     VARCHAR(16) NOT NULL DEFAULT '1.0.0',
			api_base    VARCHAR(255) NOT NULL DEFAULT '',
			auth_type   VARCHAR(32) NOT NULL DEFAULT 'none',
			auth_config TEXT NOT NULL DEFAULT '{}',
			doc_url     VARCHAR(255) NOT NULL DEFAULT '',
			field_maps  TEXT NOT NULL DEFAULT '[]',
			endpoints   TEXT NOT NULL DEFAULT '[]',
			built_in    BOOLEAN NOT NULL DEFAULT FALSE,
			enabled     BOOLEAN NOT NULL DEFAULT TRUE,
			remark      VARCHAR(500) NOT NULL DEFAULT '',
			created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)`,
		`CREATE INDEX IF NOT EXISTS idx_integration_templates_platform ON integration_templates(platform)`,
		`CREATE INDEX IF NOT EXISTS idx_integration_templates_category ON integration_templates(category)`,
		`CREATE INDEX IF NOT EXISTS idx_integration_templates_builtin ON integration_templates(built_in)`,
		`CREATE INDEX IF NOT EXISTS idx_integration_templates_enabled ON integration_templates(enabled)`,
	}
	return execAllMP1(ctx, m.db, stmts)
}

// Down 回滚
func (m *LP1Migration) Down(ctx context.Context) error {
	stmts := []string{
		`DROP TABLE IF EXISTS integration_templates`,
	}
	return execAllMP1(ctx, m.db, stmts)
}

var _ migration.Migration = (*LP1Migration)(nil)
