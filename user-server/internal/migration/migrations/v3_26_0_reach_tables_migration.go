package migrations

import (
	"context"
	"fmt"

	"hivemtk-user/internal/migration"

	"gorm.io/gorm"
)

// ReachTablesMigration R-4/R-8/H-3 触达域两张表迁移 v3.26.0
//
// reach_compliance_log（R-8 合规审计）与 reach_delayed_outbound（H-3 AI 回复延迟出站）。
// 两张表的 model 在 internal/model，由 internal/service 的 init() 经
// db.RegisterExtraModels 挂进启动 AutoMigrate——所以现网是模型建表，本迁移负责
// 纯迁移链恢复路径。两边列名必须同步：模型加字段而这里漏了，链恢复出来的库
// 第一次读写就报「列不存在」，v3_26_0_reach_tables_migration_test.go 会当场红。
type ReachTablesMigration struct {
	db *gorm.DB
}

// NewReachTablesMigration 创建迁移实例
func NewReachTablesMigration(db *gorm.DB) *ReachTablesMigration {
	return &ReachTablesMigration{db: db}
}

// Version 返回版本号
func (m *ReachTablesMigration) Version() string { return "v3.26.0" }

// Name 返回迁移名称
func (m *ReachTablesMigration) Name() string {
	return "触达域表（合规审计日志 + AI 延迟出站队列）"
}

// Description 返回迁移描述
func (m *ReachTablesMigration) Description() string {
	return "创建 reach_compliance_log / reach_delayed_outbound 2 张表"
}

// Up 执行升级
func (m *ReachTablesMigration) Up(ctx context.Context) error {
	if m.db == nil {
		return fmt.Errorf("db is nil")
	}
	stmts := []string{

		`CREATE TABLE IF NOT EXISTS reach_compliance_log (
			id BIGSERIAL PRIMARY KEY,
			channel VARCHAR(30) DEFAULT '',
			recipient_id VARCHAR(128) DEFAULT '',
			created_at TIMESTAMPTZ
		)`,
		`CREATE INDEX IF NOT EXISTS idx_reach_compliance_log_channel ON reach_compliance_log(channel)`,
		`CREATE INDEX IF NOT EXISTS idx_reach_compliance_log_created ON reach_compliance_log(created_at)`,

		`CREATE TABLE IF NOT EXISTS reach_delayed_outbound (
			id BIGSERIAL PRIMARY KEY,
			platform VARCHAR(30) DEFAULT '',
			account_id VARCHAR(64) DEFAULT '',
			conversation_id VARCHAR(128) DEFAULT '',
			sender_id VARCHAR(128) DEFAULT '',
			content TEXT DEFAULT '',
			cards JSONB,
			send_at TIMESTAMPTZ,
			status VARCHAR(20) DEFAULT 'pending',
			sent_at TIMESTAMPTZ,
			kind VARCHAR(20) NOT NULL DEFAULT 'quiet_hours',
			attempts INTEGER NOT NULL DEFAULT 0,
			last_error TEXT NOT NULL DEFAULT '',
			created_at TIMESTAMPTZ
		)`,
		`CREATE INDEX IF NOT EXISTS idx_reach_delayed_outbound_platform ON reach_delayed_outbound(platform)`,
		`CREATE INDEX IF NOT EXISTS idx_reach_delayed_outbound_conversation ON reach_delayed_outbound(conversation_id)`,
		`CREATE INDEX IF NOT EXISTS idx_reach_delayed_outbound_send_at ON reach_delayed_outbound(send_at)`,
		`CREATE INDEX IF NOT EXISTS idx_reach_delayed_outbound_status ON reach_delayed_outbound(status)`,
	}
	for _, sql := range stmts {
		if err := m.db.WithContext(ctx).Exec(sql).Error; err != nil {
			return fmt.Errorf("exec failed (%s): %w", sql, err)
		}
	}
	return nil
}

// Down 执行降级（删除 2 张表）
func (m *ReachTablesMigration) Down(ctx context.Context) error {
	if m.db == nil {
		return fmt.Errorf("db is nil")
	}
	stmts := []string{
		`DROP TABLE IF EXISTS reach_delayed_outbound`,
		`DROP TABLE IF EXISTS reach_compliance_log`,
	}
	for _, sql := range stmts {
		if err := m.db.WithContext(ctx).Exec(sql).Error; err != nil {
			return fmt.Errorf("exec failed (%s): %w", sql, err)
		}
	}
	return nil
}

var _ migration.Migration = (*ReachTablesMigration)(nil)
