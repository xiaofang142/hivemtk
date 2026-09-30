package migrations

import (
	"context"
	"fmt"

	"hivemtk-user/internal/migration"

	"gorm.io/gorm"
)

// BrowserTaskCopyTextMigration v3.48.0：browser_tasks 增加 copy_text 列（触达 P0 Chunk 1）。
//
// 修的事实：JEV TYPE_TEXT 需要任务级文案作为唯一文本来源（JEV 绝不编造文本），
// 而 BrowserTask 只有 brain_goal（目标描述）没有文案字段——文案只能以「目标描述」
// 的身份混进 goal，既污染规划输入，又无法单独清空/更新。
//
// 列形状沿用 v3.46 的「列缺省＝代码缺省」：`NOT NULL DEFAULT ”`。空串＝无文案
// （显式语义），planRound 空文案直接回退 Brain，不消费 JEV。
type BrowserTaskCopyTextMigration struct {
	db *gorm.DB
}

var _ migration.Migration = (*BrowserTaskCopyTextMigration)(nil)

func NewBrowserTaskCopyTextMigration(db *gorm.DB) *BrowserTaskCopyTextMigration {
	return &BrowserTaskCopyTextMigration{db: db}
}

func (m *BrowserTaskCopyTextMigration) Version() string { return "v3.48.0" }

func (m *BrowserTaskCopyTextMigration) Name() string {
	return "browser_tasks_copy_text"
}

func (m *BrowserTaskCopyTextMigration) Description() string {
	return "浏览器自动化 v3.48.0：browser_tasks 增加 copy_text 列（触达文案，JEV TYPE_TEXT 唯一文本来源）"
}

func (m *BrowserTaskCopyTextMigration) Up(ctx context.Context) error {
	if m.db == nil {
		return fmt.Errorf("db is nil")
	}
	if !m.db.Migrator().HasTable("browser_tasks") {
		return nil // 表还没建（全新库由模型标签负责），此处无物可改
	}
	return m.db.WithContext(ctx).Exec(`ALTER TABLE browser_tasks
		ADD COLUMN IF NOT EXISTS copy_text TEXT NOT NULL DEFAULT ''`).Error
}

// Down 不丢列：任务文案是触达回执的输入证据之一（口径同 v3.39/v3.42–v3.46 的
// 「降级不销毁在用列」）。
func (m *BrowserTaskCopyTextMigration) Down(ctx context.Context) error {
	if m.db == nil {
		return fmt.Errorf("db is nil")
	}
	declineColumnDrop(m.Version(), "browser_tasks.copy_text")
	return nil
}
