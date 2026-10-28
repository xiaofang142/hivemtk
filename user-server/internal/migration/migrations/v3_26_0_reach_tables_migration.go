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
			tenant_id BIGINT NOT NULL DEFAULT 1,
			channel VARCHAR(30) DEFAULT '',
			recipient_id VARCHAR(128) DEFAULT '',
			decision VARCHAR(16) DEFAULT '',
			reason VARCHAR(48) DEFAULT '',
			region VARCHAR(32) DEFAULT '',
			industry VARCHAR(32) DEFAULT '',
			policy_scope VARCHAR(64) DEFAULT '',
			created_at TIMESTAMPTZ
		)`,
		// E1 策略判定的 5 列。上面的 CREATE 只对新库生效；存量库早就建好了表
		// （现网是 AutoMigrate 建、或旧版本条迁移建），必须显式补列，
		// 否则「纯迁移链恢复」出来的库仍缺列——加列一律带 IF NOT EXISTS 保证重入。
		`ALTER TABLE reach_compliance_log ADD COLUMN IF NOT EXISTS decision VARCHAR(16) DEFAULT ''`,
		`ALTER TABLE reach_compliance_log ADD COLUMN IF NOT EXISTS reason VARCHAR(48) DEFAULT ''`,
		`ALTER TABLE reach_compliance_log ADD COLUMN IF NOT EXISTS region VARCHAR(32) DEFAULT ''`,
		`ALTER TABLE reach_compliance_log ADD COLUMN IF NOT EXISTS industry VARCHAR(32) DEFAULT ''`,
		`ALTER TABLE reach_compliance_log ADD COLUMN IF NOT EXISTS policy_scope VARCHAR(64) DEFAULT ''`,
		`CREATE INDEX IF NOT EXISTS idx_reach_compliance_log_channel ON reach_compliance_log(channel)`,
		`CREATE INDEX IF NOT EXISTS idx_reach_compliance_log_created ON reach_compliance_log(created_at)`,
		`CREATE INDEX IF NOT EXISTS idx_reach_compliance_log_decision ON reach_compliance_log(decision)`,
		`CREATE INDEX IF NOT EXISTS idx_reach_compliance_log_reason ON reach_compliance_log(reason)`,
		`CREATE INDEX IF NOT EXISTS idx_reach_compliance_log_region ON reach_compliance_log(region)`,
		`CREATE INDEX IF NOT EXISTS idx_reach_compliance_log_industry ON reach_compliance_log(industry)`,

		`CREATE TABLE IF NOT EXISTS reach_delayed_outbound (
			id BIGSERIAL PRIMARY KEY,
			tenant_id BIGINT NOT NULL DEFAULT 1,
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
			-- D03 租约三列（eea1f2e2）：与模型列同步是本迁移的硬约定
			--（见文件头注释——漏列=纯迁移链库第一次读写即报错，v3_26_0 测试当场红）
			claim_token VARCHAR(64) DEFAULT '',
			claimed_at TIMESTAMPTZ,
			lease_until TIMESTAMPTZ,
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

// Down 走 declineTableDrop（降级不销毁在用对象）。
//
// 这两张表在本项目的每个真实部署里都是 AutoMigrate 建的（model 挂在
// internal/service 的 init() → db.RegisterExtraModels 上），本迁移只是"纯迁移链
// 恢复"路径的补建。Down 按名字 DROP 就等于：一次 `POST /api/migration/rollback`
// 把现网正在写的 reach_compliance_log（合规审计日志）连同 reach_delayed_outbound
// （AI 回复延迟出站队列，丢一行就是一条客户回复永远不发）一起抹掉，而 AutoMigrate
// 只在启动时跑，回滚后的进程里这两张表永不自动补回。
// 两张表的代价完全不对称：留着只是多占一行元数据，删掉是不可恢复的数据丢失。
func (m *ReachTablesMigration) Down(ctx context.Context) error {
	if m.db == nil {
		return fmt.Errorf("db is nil")
	}
	declineTableDrop(m.Version(), "reach_delayed_outbound", "reach_compliance_log")
	return nil
}

var _ migration.Migration = (*ReachTablesMigration)(nil)
