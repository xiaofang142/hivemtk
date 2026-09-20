package migrations

import (
	"context"
	"database/sql"
	"fmt"
	"log"

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
		// text / uuid 型列的 character_maximum_length 是 NULL，非空 int 承接会在
		// 第一条 ALTER 之前就把整个迁移打停（NULL ⇒ 恰恰是「还没收敛」，必须继续处理）
		MaxLen sql.NullInt64
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

		if c.Type != "character varying" || c.MaxLen.Int64 != 64 {
			alterSQL := fmt.Sprintf(
				`ALTER TABLE %q.%q ALTER COLUMN customer_id TYPE varchar(64)`,
				c.Schema, c.Table)
			if err := m.db.WithContext(ctx).Exec(alterSQL).Error; err != nil {
				return fmt.Errorf("ALTER %q.%q.customer_id 失败: %w", c.Schema, c.Table, err)
			}
			altered++
		}

		commentSQL := fmt.Sprintf(
			`COMMENT ON COLUMN %q.%q.customer_id IS '统一 varchar(64)：客户 ID，JOIN 键'`,
			c.Schema, c.Table)
		if err := m.db.WithContext(ctx).Exec(commentSQL).Error; err != nil {
			log.Printf("[v3.22.0] %s.%s customer_id 注释写入失败（不影响类型收敛）: %v", c.Schema, c.Table, err)
		}
	}

	log.Printf("[v3.22.0] customer_id 列共 %d 个，本次收敛 %d 个为 varchar(64)", len(cols), altered)

	return nil
}

func (m *CustomerIDStandardizeMigration) Down(ctx context.Context) error {
	return nil
}
