package migrations

import (
	"context"
	"fmt"

	"gorm.io/gorm"

	"hivemtk-user/internal/migration"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tenant"
)

// LeadNotifyOutboxMigration v3.117.0：只新增 item2 的耐久 outbox 与独立送达历史。
type LeadNotifyOutboxMigration struct {
	db *gorm.DB
}

var _ migration.Migration = (*LeadNotifyOutboxMigration)(nil)

func NewLeadNotifyOutboxMigration(db *gorm.DB) *LeadNotifyOutboxMigration {
	return &LeadNotifyOutboxMigration{db: db}
}

func (*LeadNotifyOutboxMigration) Version() string { return "v3.117.0" }

func (*LeadNotifyOutboxMigration) Name() string {
	return "线索通知耐久 outbox 与独立送达历史"
}

func (*LeadNotifyOutboxMigration) Description() string {
	return "幂等新增 lead_notify_outbox / lead_notify_deliveries 及本 lane 索引；Down 只回滚这两张表"
}

func (m *LeadNotifyOutboxMigration) Up(ctx context.Context) error {
	if m == nil || m.db == nil {
		return fmt.Errorf("lead notify outbox migration: db is nil")
	}
	return m.db.WithContext(tenant.WithSystemScope(ctx)).AutoMigrate(
		&model.LeadNotifyOutbox{},
		&model.LeadNotifyDelivery{},
	)
}

func (m *LeadNotifyOutboxMigration) Down(ctx context.Context) error {
	if m == nil || m.db == nil {
		return fmt.Errorf("lead notify outbox migration: db is nil")
	}
	db := m.db.WithContext(tenant.WithSystemScope(ctx))
	if db.Migrator().HasTable("lead_notify_deliveries") {
		if err := db.Migrator().DropTable("lead_notify_deliveries"); err != nil {
			return fmt.Errorf("drop lead_notify_deliveries: %w", err)
		}
	}
	if db.Migrator().HasTable("lead_notify_outbox") {
		if err := db.Migrator().DropTable("lead_notify_outbox"); err != nil {
			return fmt.Errorf("drop lead_notify_outbox: %w", err)
		}
	}
	return nil
}
