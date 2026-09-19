package migrations

import (
	"context"
	"fmt"

	"hivemtk-user/internal/migration"

	"gorm.io/gorm"
)

// BrowserWriteLedgerConfirmBudgetMigration v3.43.0：批6–批8 的浏览器自动化列补齐。
//
// 一条迁移管两件事，因为它们同属「写链路上不可逆那一段」的可追溯性：
//   - browser_steps 台账三列（批6 submit_state/text_hash + 批7 is_write）：
//     步状态回答「跑成什么样」，台账三列回答「有没有真的提交过、提交的是什么、这一步算不算写」。
//     缺任何一个，防双发闸门就只能靠内存态，进程一重启就把「已提交」忘干净。
//   - browser_tasks.confirm_wait_sec（批8）：D7 确认等待的执行预算解耦列。
//     默认 600s 而非 0——列的缺省值必须等于代码里的缺省值，否则老行读出来是 0，
//     「不等待」和「按默认等待」在库里就无法区分。
//
// Down 丢弃这四列即丢弃台账本身（重放历史不可追），与 v3.42.0 的 require_confirm 同口径。
type BrowserWriteLedgerConfirmBudgetMigration struct {
	db *gorm.DB
}

var _ migration.Migration = (*BrowserWriteLedgerConfirmBudgetMigration)(nil)

func NewBrowserWriteLedgerConfirmBudgetMigration(db *gorm.DB) *BrowserWriteLedgerConfirmBudgetMigration {
	return &BrowserWriteLedgerConfirmBudgetMigration{db: db}
}

func (m *BrowserWriteLedgerConfirmBudgetMigration) Version() string { return "v3.43.0" }

func (m *BrowserWriteLedgerConfirmBudgetMigration) Name() string {
	return "browser_write_ledger_confirm_budget"
}

func (m *BrowserWriteLedgerConfirmBudgetMigration) Description() string {
	return "浏览器自动化 v3.43.0：写台账列（browser_steps.submit_state/text_hash/is_write）+ D7 确认预算列（browser_tasks.confirm_wait_sec，默认 600）"
}

func (m *BrowserWriteLedgerConfirmBudgetMigration) Up(ctx context.Context) error {
	if m.db == nil {
		return fmt.Errorf("db is nil")
	}
	stmts := []string{
		`ALTER TABLE browser_steps
			ADD COLUMN IF NOT EXISTS submit_state VARCHAR(16) NOT NULL DEFAULT ''`,
		`ALTER TABLE browser_steps
			ADD COLUMN IF NOT EXISTS text_hash VARCHAR(16) NOT NULL DEFAULT ''`,
		`ALTER TABLE browser_steps
			ADD COLUMN IF NOT EXISTS is_write BOOLEAN NOT NULL DEFAULT false`,
		`CREATE INDEX IF NOT EXISTS idx_browser_steps_submit_state ON browser_steps (submit_state)`,
		`CREATE INDEX IF NOT EXISTS idx_browser_steps_text_hash ON browser_steps (text_hash)`,
		`ALTER TABLE browser_tasks
			ADD COLUMN IF NOT EXISTS confirm_wait_sec INTEGER NOT NULL DEFAULT 600`,
	}
	for _, s := range stmts {
		if err := m.db.WithContext(ctx).Exec(s).Error; err != nil {
			return err
		}
	}
	return nil
}

func (m *BrowserWriteLedgerConfirmBudgetMigration) Down(ctx context.Context) error {
	if m.db == nil {
		return fmt.Errorf("db is nil")
	}
	stmts := []string{
		`DROP INDEX IF EXISTS idx_browser_steps_text_hash`,
		`DROP INDEX IF EXISTS idx_browser_steps_submit_state`,
		`ALTER TABLE browser_tasks DROP COLUMN IF EXISTS confirm_wait_sec`,
		`ALTER TABLE browser_steps DROP COLUMN IF EXISTS is_write`,
		`ALTER TABLE browser_steps DROP COLUMN IF EXISTS text_hash`,
		`ALTER TABLE browser_steps DROP COLUMN IF EXISTS submit_state`,
	}
	for _, s := range stmts {
		if err := m.db.WithContext(ctx).Exec(s).Error; err != nil {
			return err
		}
	}
	return nil
}
