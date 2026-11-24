package migrations

import (
	"context"
	"fmt"

	"hivemtk-user/internal/migration"

	"gorm.io/gorm"
)

// CustomerIDWidenVarchar64Migration v3.113.0（FIX-5 Phase7 回归修复）：
// 顶层 migrations/096_customer_id_widen_varchar64.sql 的 Go 注册入口。
// 把存量库里仍为 VARCHAR(36) 的 customer_id 列幂等加宽到 VARCHAR(64)——
// customer_id 是 OneID JOIN 键，093/094 在 045 之后新建的列仍是 36，
// 本迁移在其后执行，保证与 GORM tag type:varchar(64) 全链路一致、不截断。
type CustomerIDWidenVarchar64Migration struct {
	db *gorm.DB
}

var _ migration.Migration = (*CustomerIDWidenVarchar64Migration)(nil)

// NewCustomerIDWidenVarchar64Migration 构造。
func NewCustomerIDWidenVarchar64Migration(db *gorm.DB) *CustomerIDWidenVarchar64Migration {
	return &CustomerIDWidenVarchar64Migration{db: db}
}

func (m *CustomerIDWidenVarchar64Migration) Version() string { return "v3.113.0" }

func (m *CustomerIDWidenVarchar64Migration) Name() string {
	return "customer_id 全链路收敛 VARCHAR(64)"
}

func (m *CustomerIDWidenVarchar64Migration) Description() string {
	return "幂等加宽存量 VARCHAR(36) 的 customer_id 列为 VARCHAR(64)（OneID JOIN 键，USING 安全转换防截断）"
}

// Up 幂等：仅处理 character varying 且长度 < 64 的 customer_id 列；已是 64 的不动。
func (m *CustomerIDWidenVarchar64Migration) Up(ctx context.Context) error {
	if m.db == nil {
		return fmt.Errorf("customer_id widen migration: db 未初始化")
	}
	stmt := `
		DO $$
		DECLARE
		  tab RECORD;
		BEGIN
		  FOR tab IN
		    SELECT c.table_name
		    FROM information_schema.columns c
		    WHERE c.table_schema = 'public'
		      AND c.column_name = 'customer_id'
		      AND c.data_type = 'character varying'
		      AND c.character_maximum_length < 64
		  LOOP
		    EXECUTE format(
		      'ALTER TABLE %I ALTER COLUMN customer_id TYPE VARCHAR(64) USING customer_id::varchar(64)',
		      tab.table_name
		    );
		    RAISE NOTICE 'Widened customer_id to VARCHAR(64) in %', tab.table_name;
		  END LOOP;
		END $$;
	`
	if err := m.db.WithContext(ctx).Exec(stmt).Error; err != nil {
		return fmt.Errorf("customer_id 加宽迁移失败: %w", err)
	}
	return nil
}

// Down 不可回退：列宽加宽不可逆，收窄会截断 OneID JOIN 键数据。
func (m *CustomerIDWidenVarchar64Migration) Down(_ context.Context) error { return nil }
