package migrations

import (
	"context"
	"errors"
	"fmt"

	"hivemtk-user/internal/migration"
	"hivemtk-user/internal/model"

	"gorm.io/gorm"
)

type MerchantCallbackMigration struct{ db *gorm.DB }

func NewMerchantCallbackMigration(db *gorm.DB) *MerchantCallbackMigration {
	return &MerchantCallbackMigration{db: db}
}

func (*MerchantCallbackMigration) Version() string { return "v3.104.0" }
func (*MerchantCallbackMigration) Name() string    { return "商家可信回调" }
func (*MerchantCallbackMigration) Description() string {
	return "创建商家订阅和持久回调收件箱，并区分连接同意与凭证续期代次"
}

func (m *MerchantCallbackMigration) Up(ctx context.Context) error {
	if m.db == nil {
		return errors.New("merchant callback migration requires a database")
	}
	if err := m.db.WithContext(ctx).AutoMigrate(&model.MerchantConnection{}, &model.MerchantCallbackSubscription{}, &model.MerchantCallbackEvent{}); err != nil {
		return fmt.Errorf("migrate merchant callbacks: %w", err)
	}
	return nil
}

// Preserve acknowledged inbox rows and consent fences across rollbacks.
func (*MerchantCallbackMigration) Down(context.Context) error { return nil }

var _ migration.Migration = (*MerchantCallbackMigration)(nil)
