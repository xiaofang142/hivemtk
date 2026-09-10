package migrations

import (
	"context"
	"fmt"

	"hivemtk-user/internal/migration"

	"gorm.io/gorm"
)

// BrowserAutomationMigration v3.37.0：浏览器自动化（寄生式 Chrome Native Messaging）
type BrowserAutomationMigration struct {
	db *gorm.DB
}

var _ migration.Migration = (*BrowserAutomationMigration)(nil)

func NewBrowserAutomationMigration(db *gorm.DB) *BrowserAutomationMigration {
	return &BrowserAutomationMigration{db: db}
}

func (m *BrowserAutomationMigration) Version() string { return "v3.37.0" }

func (m *BrowserAutomationMigration) Name() string {
	return "browser_tasks / browser_sessions / browser_steps / browser_cron_triggers / browser_llm_plans"
}

func (m *BrowserAutomationMigration) Description() string {
	return "浏览器自动化：寄生式 Chrome Native Messaging（WS 单长连接 + per-user Host 路由），任务/会话/步骤/定时触发/LLM 计划五表"
}

func (m *BrowserAutomationMigration) Up(ctx context.Context) error {
	if m.db == nil {
		return fmt.Errorf("db is nil")
	}
	// 1. 任务主体
	if err := m.db.WithContext(ctx).Exec(`CREATE TABLE IF NOT EXISTS browser_tasks (
		id BIGSERIAL PRIMARY KEY,
		name VARCHAR(256) NOT NULL,
		description TEXT DEFAULT '',
		task_type VARCHAR(32) NOT NULL DEFAULT 'one_shot',
		status VARCHAR(32) NOT NULL DEFAULT 'draft',
		url VARCHAR(2048) NOT NULL,
		steps JSONB,
		brain_mode BOOLEAN NOT NULL DEFAULT FALSE,
		brain_goal TEXT DEFAULT '',
		llm_plan_id BIGINT,
		loop_count INT NOT NULL DEFAULT 1,
		delay_ms INT NOT NULL DEFAULT 1000,
		timeout_sec INT NOT NULL DEFAULT 120,
		depends_on_task_id BIGINT,
		depends_on_mode VARCHAR(32) DEFAULT 'all_done',
		retry_on_fail BOOLEAN NOT NULL DEFAULT FALSE,
		retry_delay_sec INT NOT NULL DEFAULT 300,
		max_retry_times INT NOT NULL DEFAULT 3,
		retry_count INT NOT NULL DEFAULT 0,
		user_id BIGINT NOT NULL,
		account_id BIGINT DEFAULT 0,
		last_run_at TIMESTAMP,
		last_result TEXT,
		error_msg TEXT,
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		deleted_at TIMESTAMP
	)`).Error; err != nil {
		return err
	}
	// 2. Chrome tab 会话
	if err := m.db.WithContext(ctx).Exec(`CREATE TABLE IF NOT EXISTS browser_sessions (
		id BIGSERIAL PRIMARY KEY,
		task_id BIGINT NOT NULL,
		user_id BIGINT NOT NULL,
		chrome_tab_id INT DEFAULT 0,
		url VARCHAR(2048) DEFAULT '',
		title VARCHAR(512) DEFAULT '',
		status VARCHAR(32) NOT NULL DEFAULT 'created',
		snapshot TEXT,
		llm_plan JSONB,
		started_at TIMESTAMP,
		completed_at TIMESTAMP,
		duration_ms BIGINT DEFAULT 0,
		error_msg TEXT,
		total_steps INT NOT NULL DEFAULT 0,
		success_steps INT NOT NULL DEFAULT 0,
		failed_steps INT NOT NULL DEFAULT 0,
		hand_latency_ms BIGINT DEFAULT 0,
		console_errors TEXT,
		extracted_data JSONB,
		final_screenshot_url VARCHAR(1024) DEFAULT '',
		llm_summary TEXT,
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		deleted_at TIMESTAMP
	)`).Error; err != nil {
		return err
	}
	// 3. 执行步骤
	if err := m.db.WithContext(ctx).Exec(`CREATE TABLE IF NOT EXISTS browser_steps (
		id BIGSERIAL PRIMARY KEY,
		session_id BIGINT NOT NULL,
		task_id BIGINT NOT NULL,
		step_index INT NOT NULL,
		action VARCHAR(32) NOT NULL,
		target VARCHAR(1024) DEFAULT '',
		value TEXT,
		params JSONB,
		status VARCHAR(32) NOT NULL DEFAULT 'pending',
		result JSONB,
		duration_ms BIGINT DEFAULT 0,
		error_msg TEXT,
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		deleted_at TIMESTAMP
	)`).Error; err != nil {
		return err
	}
	// 4. 定时触发器
	if err := m.db.WithContext(ctx).Exec(`CREATE TABLE IF NOT EXISTS browser_cron_triggers (
		id BIGSERIAL PRIMARY KEY,
		task_id BIGINT NOT NULL,
		cron_expr VARCHAR(128) NOT NULL,
		enabled BOOLEAN NOT NULL DEFAULT TRUE,
		next_run_at TIMESTAMP,
		last_run_at TIMESTAMP,
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		deleted_at TIMESTAMP,
		CONSTRAINT uk_browser_cron_task UNIQUE (task_id)
	)`).Error; err != nil {
		return err
	}
	// 5. LLM 执行计划
	if err := m.db.WithContext(ctx).Exec(`CREATE TABLE IF NOT EXISTS browser_llm_plans (
		id BIGSERIAL PRIMARY KEY,
		task_id BIGINT NOT NULL,
		goal TEXT NOT NULL,
		snapshot TEXT,
		steps JSONB,
		reasoning TEXT,
		model VARCHAR(64) DEFAULT '',
		token_in INT DEFAULT 0,
		token_out INT DEFAULT 0,
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		deleted_at TIMESTAMP
	)`).Error; err != nil {
		return err
	}

	indexes := []string{
		`CREATE INDEX IF NOT EXISTS idx_browser_tasks_user ON browser_tasks(user_id)`,
		`CREATE INDEX IF NOT EXISTS idx_browser_tasks_status ON browser_tasks(status)`,
		`CREATE INDEX IF NOT EXISTS idx_browser_tasks_type ON browser_tasks(task_type)`,
		`CREATE INDEX IF NOT EXISTS idx_browser_tasks_dep ON browser_tasks(depends_on_task_id)`,
		`CREATE INDEX IF NOT EXISTS idx_browser_sessions_task ON browser_sessions(task_id)`,
		`CREATE INDEX IF NOT EXISTS idx_browser_sessions_user ON browser_sessions(user_id)`,
		`CREATE INDEX IF NOT EXISTS idx_browser_sessions_status ON browser_sessions(status)`,
		`CREATE INDEX IF NOT EXISTS idx_browser_steps_session ON browser_steps(session_id)`,
		`CREATE INDEX IF NOT EXISTS idx_browser_steps_task ON browser_steps(task_id)`,
		`CREATE INDEX IF NOT EXISTS idx_browser_steps_status ON browser_steps(status)`,
		`CREATE INDEX IF NOT EXISTS idx_browser_cron_enabled ON browser_cron_triggers(enabled)`,
		`CREATE INDEX IF NOT EXISTS idx_browser_llm_plans_task ON browser_llm_plans(task_id)`,
	}
	for _, stmt := range indexes {
		if err := m.db.WithContext(ctx).Exec(stmt).Error; err != nil {
			return err
		}
	}
	return nil
}

func (m *BrowserAutomationMigration) Down(ctx context.Context) error {
	if m.db == nil {
		return fmt.Errorf("db is nil")
	}
	// 子表先删，父表后删
	drops := []string{
		`DROP TABLE IF EXISTS browser_steps`,
		`DROP TABLE IF EXISTS browser_llm_plans`,
		`DROP TABLE IF EXISTS browser_cron_triggers`,
		`DROP TABLE IF EXISTS browser_sessions`,
		`DROP TABLE IF EXISTS browser_tasks`,
	}
	for _, stmt := range drops {
		if err := m.db.WithContext(ctx).Exec(stmt).Error; err != nil {
			return err
		}
	}
	return nil
}
