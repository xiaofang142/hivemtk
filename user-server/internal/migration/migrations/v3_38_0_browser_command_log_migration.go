package migrations

import (
	"context"
	"fmt"

	"hivemtk-user/internal/migration"

	"gorm.io/gorm"
)

// BrowserCommandLogMigration v3.38.0：浏览器自动化 append-only 命令-事件日志表
// （设计稿 P8 durable execution：断点续跑=重放；审计依赖不可变性，无 Update/Delete 路径）
type BrowserCommandLogMigration struct {
	db *gorm.DB
}

var _ migration.Migration = (*BrowserCommandLogMigration)(nil)

func NewBrowserCommandLogMigration(db *gorm.DB) *BrowserCommandLogMigration {
	return &BrowserCommandLogMigration{db: db}
}

func (m *BrowserCommandLogMigration) Version() string { return "v3.38.0" }

func (m *BrowserCommandLogMigration) Name() string {
	return "browser_command_log"
}

func (m *BrowserCommandLogMigration) Description() string {
	return "浏览器自动化命令-事件日志（append-only）：Executor 下发的每个命令帧与回包/错误全文落库"
}

func (m *BrowserCommandLogMigration) Up(ctx context.Context) error {
	if m.db == nil {
		return fmt.Errorf("db is nil")
	}
	if err := m.db.WithContext(ctx).Exec(`CREATE TABLE IF NOT EXISTS browser_command_log (
		id BIGSERIAL PRIMARY KEY,
		session_id BIGINT NOT NULL,
		task_id BIGINT NOT NULL,
		step_id BIGINT DEFAULT 0,
		seq INT NOT NULL,
		direction VARCHAR(16) NOT NULL DEFAULT 'command',
		action VARCHAR(32) NOT NULL,
		payload JSONB,
		duration_ms BIGINT DEFAULT 0,
		ok BOOLEAN NOT NULL DEFAULT FALSE,
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
	)`).Error; err != nil {
		return err
	}
	indexes := []string{
		`CREATE INDEX IF NOT EXISTS idx_browser_cmdlog_session ON browser_command_log(session_id)`,
		`CREATE INDEX IF NOT EXISTS idx_browser_cmdlog_task ON browser_command_log(task_id)`,
		`CREATE INDEX IF NOT EXISTS idx_browser_cmdlog_step ON browser_command_log(step_id)`,
		`CREATE INDEX IF NOT EXISTS idx_browser_cmdlog_created ON browser_command_log(created_at)`,
	}
	for _, stmt := range indexes {
		if err := m.db.WithContext(ctx).Exec(stmt).Error; err != nil {
			return err
		}
	}
	return nil
}

func (m *BrowserCommandLogMigration) Down(ctx context.Context) error {
	if m.db == nil {
		return fmt.Errorf("db is nil")
	}
	return m.db.WithContext(ctx).Exec(`DROP TABLE IF EXISTS browser_command_log`).Error
}
