package migrations

import (
	"context"
	"fmt"

	"hivemtk-user/internal/migration"

	"gorm.io/gorm"
)

// BrowserRequireConfirmMigration v3.42.0：D7 写操作人工确认开关。
// browser_tasks 增加 require_confirm 列（默认 false）——铁律 4「默认全自动」不变；
// 置 true 的任务在 post_comment 不可逆提交点前挂起，等 POST /sessions/:id/confirm 放行。
type BrowserRequireConfirmMigration struct {
	db *gorm.DB
}

var _ migration.Migration = (*BrowserRequireConfirmMigration)(nil)

func NewBrowserRequireConfirmMigration(db *gorm.DB) *BrowserRequireConfirmMigration {
	return &BrowserRequireConfirmMigration{db: db}
}

func (m *BrowserRequireConfirmMigration) Version() string { return "v3.42.0" }

func (m *BrowserRequireConfirmMigration) Name() string { return "browser_require_confirm" }

func (m *BrowserRequireConfirmMigration) Description() string {
	return "浏览器自动化 v3.42.0：D7 写操作人工确认开关（browser_tasks.require_confirm，默认 false）"
}

func (m *BrowserRequireConfirmMigration) Up(ctx context.Context) error {
	if m.db == nil {
		return fmt.Errorf("db is nil")
	}
	stmts := []string{
		`ALTER TABLE browser_tasks
			ADD COLUMN IF NOT EXISTS require_confirm BOOLEAN NOT NULL DEFAULT false`,
	}
	for _, s := range stmts {
		if err := m.db.WithContext(ctx).Exec(s).Error; err != nil {
			return err
		}
	}
	return nil
}

func (m *BrowserRequireConfirmMigration) Down(ctx context.Context) error {
	if m.db == nil {
		return fmt.Errorf("db is nil")
	}
	declineColumnDrop(m.Version(), "browser_tasks.require_confirm")
	return nil
}
