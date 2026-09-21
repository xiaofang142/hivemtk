package migrations

import (
	"context"
	"fmt"

	"hivemtk-user/internal/migration"
	"hivemtk-user/internal/model"

	"gorm.io/gorm"
)

// BridgeAccountMigration 创建 bridge_accounts 表
type BridgeAccountMigration struct {
	db *gorm.DB
}

func NewBridgeAccountMigration(db *gorm.DB) *BridgeAccountMigration {
	return &BridgeAccountMigration{db: db}
}

func (m *BridgeAccountMigration) Version() string { return "v3.11.0" }

func (m *BridgeAccountMigration) Name() string { return "创建 bridge_accounts 表" }

func (m *BridgeAccountMigration) Description() string {
	return "创建 bridge_accounts（网页私信桥接账号）表，支撑抖音/小红书/TikTok 私信桥接的账号注册、归属校验与管理"
}

func (m *BridgeAccountMigration) Up(ctx context.Context) error {
	if m.db == nil {
		return fmt.Errorf("db is nil")
	}
	if err := m.db.WithContext(ctx).AutoMigrate(&model.BridgeAccount{}); err != nil {
		return fmt.Errorf("bridge_accounts 建表失败: %w", err)
	}
	return nil
}

func (m *BridgeAccountMigration) Down(ctx context.Context) error {
	declineTableDrop(m.Version(), "bridge_accounts")
	return nil
}

var _ migration.Migration = (*BridgeAccountMigration)(nil)
