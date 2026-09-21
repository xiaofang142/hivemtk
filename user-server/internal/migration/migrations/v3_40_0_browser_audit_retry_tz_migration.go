package migrations

import (
	"context"
	"fmt"

	"hivemtk-user/internal/migration"

	"gorm.io/gorm"
)

// BrowserAuditRetryTzMigration v3.40.0：浏览器自动化三项收口
//  1. G3/D3：browser_llm_plans 增加 session_id + kind 列——plan/judge/summary 三类 token 全落库，
//     session 级成本账可审计（BRAIN_PRODUCT_SPEC P1-1 欠账）；
//  2. G5/D4b：browser_tasks 增加 next_retry_at 列——失败重试从内存定时器升级为 DB 持久化
//     （进程重启不丢，扫描器认领后触发）；
//  3. G20：browser_cron_triggers 增加 time_zone 列——注册时前缀 CRON_TZ=（robfig v3 原生支持），
//     不再依赖服务器本地时区。
type BrowserAuditRetryTzMigration struct {
	db *gorm.DB
}

var _ migration.Migration = (*BrowserAuditRetryTzMigration)(nil)

func NewBrowserAuditRetryTzMigration(db *gorm.DB) *BrowserAuditRetryTzMigration {
	return &BrowserAuditRetryTzMigration{db: db}
}

func (m *BrowserAuditRetryTzMigration) Version() string { return "v3.40.0" }

func (m *BrowserAuditRetryTzMigration) Name() string { return "browser_audit_retry_tz" }

func (m *BrowserAuditRetryTzMigration) Description() string {
	return "浏览器自动化 v3.40.0：llm_plans 成本账（session_id/kind）+ 重试持久化（next_retry_at）+ cron 时区（time_zone）"
}

func (m *BrowserAuditRetryTzMigration) Up(ctx context.Context) error {
	if m.db == nil {
		return fmt.Errorf("db is nil")
	}
	stmts := []string{
		`ALTER TABLE browser_llm_plans
			ADD COLUMN IF NOT EXISTS session_id BIGINT NOT NULL DEFAULT 0`,
		`ALTER TABLE browser_llm_plans
			ADD COLUMN IF NOT EXISTS kind VARCHAR(16) NOT NULL DEFAULT 'plan'`,
		`CREATE INDEX IF NOT EXISTS idx_browser_llm_plans_session ON browser_llm_plans(session_id)`,
		`ALTER TABLE browser_tasks
			ADD COLUMN IF NOT EXISTS next_retry_at TIMESTAMPTZ NULL`,
		`CREATE INDEX IF NOT EXISTS idx_browser_tasks_next_retry ON browser_tasks(next_retry_at) WHERE next_retry_at IS NOT NULL`,
		`ALTER TABLE browser_cron_triggers
			ADD COLUMN IF NOT EXISTS time_zone VARCHAR(64) NOT NULL DEFAULT ''`,
	}
	for _, s := range stmts {
		if err := m.db.WithContext(ctx).Exec(s).Error; err != nil {
			return err
		}
	}
	return nil
}

func (m *BrowserAuditRetryTzMigration) Down(ctx context.Context) error {
	if m.db == nil {
		return fmt.Errorf("db is nil")
	}
	declineIndexDrop(m.Version(), "idx_browser_tasks_next_retry", "idx_browser_llm_plans_session")
	declineColumnDrop(m.Version(), "browser_cron_triggers.time_zone", "browser_tasks.next_retry_at",
		"browser_llm_plans.kind", "browser_llm_plans.session_id")
	return nil
}
