package migrations

import (
	"context"
	"fmt"

	"hivemtk-user/internal/migration"

	"gorm.io/gorm"
)

type CustomerIDStandardizeMigration struct {
	db *gorm.DB
}

var _ migration.Migration = (*CustomerIDStandardizeMigration)(nil)

func NewCustomerIDStandardizeMigration(db *gorm.DB) *CustomerIDStandardizeMigration {
	return &CustomerIDStandardizeMigration{db: db}
}

func (m *CustomerIDStandardizeMigration) Version() string { return "v3.22.0" }

func (m *CustomerIDStandardizeMigration) Name() string {
	return "收敛 customer_id 字段类型为 varchar(64)"
}

func (m *CustomerIDStandardizeMigration) Description() string {
	return "将所有 customer_id 列统一为 varchar(64)，消除 JOIN 隐式类型转换"
}

// Up 执行升级
// PostgreSQL: 扫描 information_schema.columns 找出所有名为 customer_id 的列，
// 对 data_type 不是 'character varying' 或 character_maximum_length ≠ 64 的列执行 ALTER。
func (m *CustomerIDStandardizeMigration) Up(ctx context.Context) error {
	if m.db == nil {
		return fmt.Errorf("db is nil")
	}

	rows, err := m.db.WithContext(ctx).Raw(`
		SELECT table_schema, table_name, data_type, character_maximum_length
		FROM information_schema.columns
		WHERE column_name = 'customer_id'
		  AND table_schema NOT IN ('pg_catalog', 'information_schema')
		ORDER BY table_schema, table_name
	`).Rows()
	if err != nil {
		return fmt.Errorf("查询 customer_id 列失败: %w", err)
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

	var altered int
	for _, c := range cols {

		if c.Type == "character varying" && c.MaxLen == 64 {
			commentSQL := fmt.Sprintf(
				`COMMENT ON COLUMN %q.%q.customer_id IS '统一 varchar(64)：客户 ID，JOIN 键'`,
				c.Schema, c.Table)
			_ = m.db.WithContext(ctx).Exec(commentSQL).Error
			continue
		}

		alterSQL := fmt.Sprintf(
			`ALTER TABLE %q.%q ALTER COLUMN customer_id TYPE varchar(64)`,
			c.Schema, c.Table)
		if err := m.db.WithContext(ctx).Exec(alterSQL).Error; err != nil {
			return fmt.Errorf("ALTER %q.%q.customer_id 失败: %w", c.Schema, c.Table, err)
		}
		altered++
	}

	if altered > 0 {
		m.db.WithContext(ctx).Exec(
			`COMMENT ON COLUMN information_schema.columns.column_name IS '统一 varchar(64) 迁移完成'`)
	}

	return nil
}

func (m *CustomerIDStandardizeMigration) Down(ctx context.Context) error {
	return nil
}
