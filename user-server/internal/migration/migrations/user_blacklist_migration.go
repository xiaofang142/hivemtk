package migrations

import (
	"context"
	"fmt"

	"hivemtk-user/internal/migration"
	"hivemtk-user/internal/model"

	"gorm.io/gorm"
)

// UserBlacklistMigration 创建 user_blacklist 表
type UserBlacklistMigration struct {
	db *gorm.DB
}

// NewUserBlacklistMigration 创建迁移实例
func NewUserBlacklistMigration(db *gorm.DB) *UserBlacklistMigration {
	return &UserBlacklistMigration{db: db}
}

// Version 返回版本号
//
// 注意：v3.7.0 已被「llm_routing_logs 字段扩展」迁移占用并记录为 completed，
// 若复用会触发 GetPending 的版本去重而被判定为已执行、跳过本迁移。
// 故使用独立的 v3.10.0 以避免版本冲突。
func (m *UserBlacklistMigration) Version() string { return "v3.10.0" }

// Name 返回迁移名称
func (m *UserBlacklistMigration) Name() string { return "创建 user_blacklist 表" }

// Description 返回迁移描述
func (m *UserBlacklistMigration) Description() string {
	return "创建 user_blacklist（用户黑名单）表，支撑坐席聊天看板的拉黑/取消拉黑与黑名单管理"
}

// Up 执行升级
func (m *UserBlacklistMigration) Up(ctx context.Context) error {
	if m.db == nil {
		return fmt.Errorf("db is nil")
	}
	if err := m.db.WithContext(ctx).AutoMigrate(&model.UserBlacklist{}); err != nil {
		return fmt.Errorf("user_blacklist 建表失败: %w", err)
	}
	return nil
}

// Down 回滚
func (m *UserBlacklistMigration) Down(ctx context.Context) error {
	declineTableDrop(m.Version(), "user_blacklist")
	return nil
}

var _ migration.Migration = (*UserBlacklistMigration)(nil)
