package migrations

import (
	"context"
	"errors"
	"fmt"

	"hivemtk-user/internal/migration"
	"hivemtk-user/internal/model"

	"gorm.io/gorm"
)

type MerchantConnectionMigration struct{ db *gorm.DB }

func NewMerchantConnectionMigration(db *gorm.DB) *MerchantConnectionMigration {
	return &MerchantConnectionMigration{db: db}
}

func (*MerchantConnectionMigration) Version() string { return "v3.102.0" }
func (*MerchantConnectionMigration) Name() string    { return "商家授权连接" }

func (*MerchantConnectionMigration) Description() string {
	return "创建按租户、提供方与店铺隔离的授权状态及加密凭证绑定表"
}

func (m *MerchantConnectionMigration) Up(ctx context.Context) error {
	if m.db == nil {
		return errors.New("merchant connection migration requires a database")
	}
	if err := m.db.WithContext(ctx).AutoMigrate(&model.MerchantConnection{}); err != nil {
		return fmt.Errorf("migrate merchant connections: %w", err)
	}
	return nil
}

func (m *MerchantConnectionMigration) Down(context.Context) error {
	declineTableDrop(m.Version(), "merchant_connections")
	return nil
}

var _ migration.Migration = (*MerchantConnectionMigration)(nil)
