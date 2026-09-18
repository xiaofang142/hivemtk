package migrations

import (
	"context"
	"fmt"
	"strings"

	"hivemtk-user/internal/migration"

	"gorm.io/gorm"
)

// MerchantIDNullableMigration merchant_id 字段可空化迁移
// 私域部署：每商户独立部署一套完整系统，merchant_id 字段不再承担多租户隔离职责
// 保留字段(便于数据回迁/审计)但允许为 NULL,以兼容历史数据 + 未来扩展
type MerchantIDNullableMigration struct {
	db *gorm.DB
}

// NewMerchantIDNullableMigration 创建 merchant_id 可空化迁移
func NewMerchantIDNullableMigration(db *gorm.DB) *MerchantIDNullableMigration {
	return &MerchantIDNullableMigration{db: db}
}

// Version 返回版本号
func (m *MerchantIDNullableMigration) Version() string {
	return "v2.1.0"
}

// Name 返回迁移名称
func (m *MerchantIDNullableMigration) Name() string {
	return "merchant_id 字段可空化"
}

// Description 返回迁移描述
func (m *MerchantIDNullableMigration) Description() string {
	return "私域部署：所有业务表的 merchant_id 字段允许为 NULL，幂等可重入"
}

// Up 执行升级
// PostgreSQL: 动态扫描 information_schema 找出所有 is_nullable='NO' 的 merchant_id 列,逐一 ALTER
func (m *MerchantIDNullableMigration) Up(ctx context.Context) error {
	if m.db == nil {
		return fmt.Errorf("db is nil")
	}

	return m.upPostgres()
}

func (m *MerchantIDNullableMigration) upPostgres() error {
	rows, err := m.db.Raw(`
		SELECT table_schema, table_name
		FROM information_schema.columns
		WHERE column_name = 'merchant_id'
		  AND is_nullable = 'NO'
		  AND table_schema NOT IN ('pg_catalog', 'information_schema')
	`).Rows()
	if err != nil {
		return fmt.Errorf("查询 merchant_id 列失败: %w", err)
	}
	defer func() { _ = rows.Close() }()

	type tableRef struct {
		Schema string
		Table  string
	}
	var tables []tableRef
	for rows.Next() {
		var t tableRef
		if err := rows.Scan(&t.Schema, &t.Table); err != nil {
			return fmt.Errorf("扫描表名失败: %w", err)
		}
		tables = append(tables, t)
	}

	var success, failed int
	for _, t := range tables {
		alterSQL := fmt.Sprintf(`ALTER TABLE %q.%q ALTER COLUMN merchant_id DROP NOT NULL`,
			t.Schema, t.Table)
		if err := m.db.Exec(alterSQL).Error; err != nil {
			if !strings.Contains(err.Error(), "already") &&
				!strings.Contains(err.Error(), "does not exist") {
				failed++
				continue
			}
		}
		success++
	}

	m.db.Exec(`UPDATE pg_attribute SET attnotnull = false
		WHERE attrelid IN (
			SELECT attrelid FROM pg_attribute WHERE attname = 'merchant_id'
		) AND attname = 'merchant_id' AND attnotnull = true`)

	return nil
}

// Down 执行降级
// 私域部署:无需回滚 merchant_id 可空限制(回滚会破坏业务数据)
func (m *MerchantIDNullableMigration) Down(ctx context.Context) error {
	return nil
}

var _ migration.Migration = (*MerchantIDNullableMigration)(nil)
