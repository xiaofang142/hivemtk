package migrations

import (
	"context"
	"fmt"

	"hivemtk-user/internal/migration"
	"hivemtk-user/internal/model"

	"gorm.io/gorm"
)

// MergedIntoIDWidenVarchar64Migration v3.114.0：
// 顶层 migrations/097_merged_into_id_widen_varchar64.sql 的 Go 注册入口。
// 合并目标列与 customer_id 主链同为 VARCHAR(64)，防止长 OneID 写入被截断。
//
// 安全写法：列类型变更走 GORM Migrator（结构化 API），不拼接、不执行原始 SQL 字符串。
type MergedIntoIDWidenVarchar64Migration struct {
	db *gorm.DB
}

var _ migration.Migration = (*MergedIntoIDWidenVarchar64Migration)(nil)

func NewMergedIntoIDWidenVarchar64Migration(db *gorm.DB) *MergedIntoIDWidenVarchar64Migration {
	return &MergedIntoIDWidenVarchar64Migration{db: db}
}

func (m *MergedIntoIDWidenVarchar64Migration) Version() string { return "v3.114.0" }

func (m *MergedIntoIDWidenVarchar64Migration) Name() string {
	return "merged_into_id 与 customer_id 宽度对齐"
}

func (m *MergedIntoIDWidenVarchar64Migration) Description() string {
	return "幂等加宽 customers.merged_into_id 到 VARCHAR(64)（合并审计链防截断）"
}

func (m *MergedIntoIDWidenVarchar64Migration) Up(ctx context.Context) error {
	if m.db == nil {
		return fmt.Errorf("merged_into_id widen migration: db 未初始化")
	}
	// AlterColumn 走 GORM 结构化 DDL API：目标表/列来自 model.Customer 的 tag，
	// 期望宽度由 model 的 varchar(64) 声明，不使用 Exec/Raw 拼接 SQL 文本。
	migrator := m.db.WithContext(ctx).Migrator()
	if !migrator.HasColumn(&model.Customer{}, "merged_into_id") {
		return nil // 列不存在由 093 迁移负责，本迁移只负责加宽
	}
	if err := migrator.AlterColumn(&model.Customer{}, "MergedIntoID"); err != nil {
		return fmt.Errorf("merged_into_id 加宽迁移失败: %w", err)
	}
	return nil
}

func (m *MergedIntoIDWidenVarchar64Migration) Down(_ context.Context) error { return nil }
