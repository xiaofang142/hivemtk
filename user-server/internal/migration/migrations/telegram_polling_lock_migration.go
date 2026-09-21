package migrations

import (
	"context"
	"hivemtk-user/internal/migration"

	"gorm.io/gorm"
)

// TelegramPollingLockMigration S3-6 修复：为 telegram_accounts 表添加分布式锁字段
//
// 必要性：
//   - Telegram Bot API 同一 token 全局只允许一个进程做 getUpdates long polling
//   - 多实例同时 polling 会立即触发 409 Conflict，本地端只能错误退避
//   - 通过 polling_owner + polling_heartbeat_at 字段，进程启动时原子抢占 + 心跳续约
//   - 心跳超时的死锁可被其他进程抢占，避免僵尸锁
//
// 字段：
//   - polling_owner VARCHAR(100) DEFAULT ”：当前持有 polling 的 worker 标识（hostname:pid）
//   - polling_heartbeat_at TIMESTAMP：worker 最近一次心跳时间
type TelegramPollingLockMigration struct {
	db *gorm.DB
}

// NewTelegramPollingLockMigration 创建迁移实例
func NewTelegramPollingLockMigration(db *gorm.DB) *TelegramPollingLockMigration {
	return &TelegramPollingLockMigration{db: db}
}

// Version 返回版本号
func (m *TelegramPollingLockMigration) Version() string {
	return "v2.1.2"
}

// Name 返回迁移名称
func (m *TelegramPollingLockMigration) Name() string {
	return "Telegram polling 分布式锁字段"
}

// Description 返回迁移描述
func (m *TelegramPollingLockMigration) Description() string {
	return "为 telegram_accounts 表添加 polling_owner + polling_heartbeat_at 字段，实现多实例 polling 互斥（修复 S3-6）"
}

// Up 执行迁移
func (m *TelegramPollingLockMigration) Up(ctx context.Context) error {
	if !m.db.Migrator().HasTable("telegram_accounts") {
		return nil
	}
	if !m.db.Migrator().HasColumn(&struct {
		PollingOwner string `gorm:"type:varchar(100)"`
	}{}, "PollingOwner") {
		if err := m.db.Exec("ALTER TABLE telegram_accounts ADD COLUMN IF NOT EXISTS polling_owner VARCHAR(100) DEFAULT ''").Error; err != nil {
			return err
		}
	}
	if !m.db.Migrator().HasColumn(&struct {
		PollingHeartbeatAt *string `gorm:"type:timestamp"`
	}{}, "PollingHeartbeatAt") {
		if err := m.db.Exec("ALTER TABLE telegram_accounts ADD COLUMN IF NOT EXISTS polling_heartbeat_at TIMESTAMP").Error; err != nil {
			return err
		}
	}
	if !m.db.Migrator().HasIndex(&struct {
		PollingOwner string `gorm:"type:varchar(100);index"`
	}{}, "PollingOwner") {
		_ = m.db.Exec("CREATE INDEX IF NOT EXISTS idx_telegram_accounts_polling_owner ON telegram_accounts (polling_owner)").Error
	}
	return nil
}

// Down 回滚
func (m *TelegramPollingLockMigration) Down(ctx context.Context) error {
	if !m.db.Migrator().HasTable("telegram_accounts") {
		return nil
	}
	declineIndexDrop(m.Version(), "idx_telegram_accounts_polling_owner")
	declineColumnDrop(m.Version(), "telegram_accounts.polling_heartbeat_at", "telegram_accounts.polling_owner")
	return nil
}

var _ migration.Migration = (*TelegramPollingLockMigration)(nil)
