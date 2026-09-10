package migrations

import (
	"context"
	"fmt"

	"hivemtk-user/internal/migration"

	"gorm.io/gorm"
)

// BrowserTaskPlatformMigration v3.39.0：browser_tasks 增加 platform 列（多平台基座 M2）
// 存量任务缺省 xiaohongshu；douyin/xianyu 由 L3 适配器注册表校验
type BrowserTaskPlatformMigration struct {
	db *gorm.DB
}

var _ migration.Migration = (*BrowserTaskPlatformMigration)(nil)

func NewBrowserTaskPlatformMigration(db *gorm.DB) *BrowserTaskPlatformMigration {
	return &BrowserTaskPlatformMigration{db: db}
}

func (m *BrowserTaskPlatformMigration) Version() string { return "v3.39.0" }

func (m *BrowserTaskPlatformMigration) Name() string { return "browser_tasks_platform" }

func (m *BrowserTaskPlatformMigration) Description() string {
	return "浏览器自动化多平台基座 M2：browser_tasks.platform 列（xiaohongshu/douyin/xianyu），存量缺省 xiaohongshu"
}

func (m *BrowserTaskPlatformMigration) Up(ctx context.Context) error {
	if m.db == nil {
		return fmt.Errorf("db is nil")
	}
	if err := m.db.WithContext(ctx).Exec(`ALTER TABLE browser_tasks
		ADD COLUMN IF NOT EXISTS platform VARCHAR(32) NOT NULL DEFAULT 'xiaohongshu'`).Error; err != nil {
		return err
	}
	return m.db.WithContext(ctx).Exec(
		`CREATE INDEX IF NOT EXISTS idx_browser_tasks_platform ON browser_tasks(platform)`).Error
}

func (m *BrowserTaskPlatformMigration) Down(ctx context.Context) error {
	if m.db == nil {
		return fmt.Errorf("db is nil")
	}
	return m.db.WithContext(ctx).Exec(`ALTER TABLE browser_tasks DROP COLUMN IF EXISTS platform`).Error
}
