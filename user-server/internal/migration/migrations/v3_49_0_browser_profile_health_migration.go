package migrations

import (
	"context"
	"fmt"

	"hivemtk-user/internal/migration"

	"gorm.io/gorm"
)

// BrowserProfileHealthMigration v3.49.0：新建 browser_profile_health 表（触达 P0 Chunk 2）。
//
// 修的事实：单主 Profile 复用是本项目风控特色（不做多账号矩阵），封号熔断状态
// 必须落库（Executor 进程级单例禁内存计数）——recordProfileBlocked 的 MarkBlocked
// 与 RunTask 启动门都读写同一张表。
//
// 表形状与 model.BrowserProfileHealth 标签对齐；IF NOT EXISTS 幂等
// （建表事实源另有 AutoMigrate，此处只保证存量库可达）。
type BrowserProfileHealthMigration struct {
	db *gorm.DB
}

var _ migration.Migration = (*BrowserProfileHealthMigration)(nil)

func NewBrowserProfileHealthMigration(db *gorm.DB) *BrowserProfileHealthMigration {
	return &BrowserProfileHealthMigration{db: db}
}

func (m *BrowserProfileHealthMigration) Version() string { return "v3.49.0" }

func (m *BrowserProfileHealthMigration) Name() string {
	return "browser_profile_health"
}

func (m *BrowserProfileHealthMigration) Description() string {
	return "浏览器自动化 v3.49.0：新建 browser_profile_health 表（单主 Profile 平台级熔断行）"
}

func (m *BrowserProfileHealthMigration) Up(ctx context.Context) error {
	if m.db == nil {
		return fmt.Errorf("db is nil")
	}
	return m.db.WithContext(ctx).Exec(`CREATE TABLE IF NOT EXISTS browser_profile_health (
		id BIGSERIAL PRIMARY KEY,
		platform VARCHAR(64) NOT NULL UNIQUE,
		blocked BOOLEAN NOT NULL DEFAULT FALSE,
		reason TEXT NOT NULL DEFAULT '',
		blocked_at TIMESTAMPTZ NULL,
		updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
	)`).Error
}

// Down 不删表：熔断行是审计证据（口径同 v3.39/v3.42–v3.48 的「降级不销毁在用对象」）。
func (m *BrowserProfileHealthMigration) Down(ctx context.Context) error {
	if m.db == nil {
		return fmt.Errorf("db is nil")
	}
	declineTableDrop(m.Version(), "browser_profile_health")
	return nil
}
