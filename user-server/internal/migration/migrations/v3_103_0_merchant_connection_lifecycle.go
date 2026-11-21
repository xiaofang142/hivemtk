package migrations

import (
	"context"
	"errors"
	"fmt"

	"hivemtk-user/internal/migration"
	"hivemtk-user/internal/model"

	"gorm.io/gorm"
)

type MerchantConnectionLifecycleMigration struct{ db *gorm.DB }

func NewMerchantConnectionLifecycleMigration(db *gorm.DB) *MerchantConnectionLifecycleMigration {
	return &MerchantConnectionLifecycleMigration{db: db}
}

func (*MerchantConnectionLifecycleMigration) Version() string { return "v3.103.0" }
func (*MerchantConnectionLifecycleMigration) Name() string {
	return "商家凭证续期和本地吊销"
}

func (*MerchantConnectionLifecycleMigration) Description() string {
	return "扩展商家连接表以持久化续期领取截止时间、失败和本地吊销状态"
}

func (m *MerchantConnectionLifecycleMigration) Up(ctx context.Context) error {
	if m.db == nil {
		return errors.New("merchant connection lifecycle migration requires a database")
	}
	if err := m.db.WithContext(ctx).AutoMigrate(&model.MerchantConnection{}); err != nil {
		return fmt.Errorf("migrate merchant connection lifecycle: %w", err)
	}
	return nil
}

// Keep the revoke marker and refresh fence across rollbacks.
func (*MerchantConnectionLifecycleMigration) Down(context.Context) error { return nil }

var _ migration.Migration = (*MerchantConnectionLifecycleMigration)(nil)
