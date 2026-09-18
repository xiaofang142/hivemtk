package migrations

import (
	"context"
	"fmt"

	"hivemtk-user/internal/migration"

	"gorm.io/gorm"
)

type UnifiedIDWidenMigration struct {
	db *gorm.DB
}

var _ migration.Migration = (*UnifiedIDWidenMigration)(nil)

func NewUnifiedIDWidenMigration(db *gorm.DB) *UnifiedIDWidenMigration {
	return &UnifiedIDWidenMigration{db: db}
}

func (m *UnifiedIDWidenMigration) Version() string { return "v3.24.0" }

func (m *UnifiedIDWidenMigration) Name() string { return "unified_id 列宽放宽为 varchar(128)" }

func (m *UnifiedIDWidenMigration) Description() string {
	return "修复盐化哈希 unified_id(70字符) 超出 varchar(64) 导致客户创建失败"
}

func (m *UnifiedIDWidenMigration) Up(ctx context.Context) error {
	if m.db == nil {
		return fmt.Errorf("db is nil")
	}

	rows, err := m.db.WithContext(ctx).Raw(`
		SELECT table_schema, table_name, data_type, character_maximum_length
		FROM information_schema.columns
		WHERE column_name = 'unified_id'
		  AND table_schema NOT IN ('pg_catalog', 'information_schema')
		ORDER BY table_schema, table_name
	`).Rows()
	if err != nil {
		return fmt.Errorf("查询 unified_id 列失败: %w", err)
	}
	defer func() { _ = rows.Close() }()

	type colInfo struct {
		Schema string
		Table  string
		Type   string
		MaxLen int
	}
	var cols []colInfo
	for rows.Next() {
		var c colInfo
		if err := rows.Scan(&c.Schema, &c.Table, &c.Type, &c.MaxLen); err != nil {
			return fmt.Errorf("扫描列信息失败: %w", err)
		}
		cols = append(cols, c)
	}

	for _, c := range cols {
		if c.Type == "character varying" && c.MaxLen >= 128 {
			continue
		}
		alterSQL := fmt.Sprintf(
			`ALTER TABLE %q.%q ALTER COLUMN unified_id TYPE varchar(128)`,
			c.Schema, c.Table)
		if err := m.db.WithContext(ctx).Exec(alterSQL).Error; err != nil {
			return fmt.Errorf("ALTER %q.%q.unified_id 失败: %w", c.Schema, c.Table, err)
		}
	}
	return nil
}

func (m *UnifiedIDWidenMigration) Down(ctx context.Context) error {
	return nil
}
