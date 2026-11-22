package migrations

import (
	"context"
	"errors"
	"gorm.io/gorm"
	"hivemtk-user/internal/migration"
	"hivemtk-user/internal/model"
)

type CommerceExecutionMigration struct{ db *gorm.DB }

func NewCommerceExecutionMigration(db *gorm.DB) *CommerceExecutionMigration {
	return &CommerceExecutionMigration{db: db}
}

var _ migration.Migration = (*CommerceExecutionMigration)(nil)

func (*CommerceExecutionMigration) Version() string { return "v3.107.0" }
func (*CommerceExecutionMigration) Name() string    { return "Commerce durable execution ledger" }
func (*CommerceExecutionMigration) Description() string {
	return "Add tenant scoped commerce operations approvals attempts guards policies and transactional outbox"
}
func (m *CommerceExecutionMigration) Up(ctx context.Context) error {
	if m.db == nil {
		return errors.New("commerce migration requires database")
	}
	return m.db.WithContext(ctx).AutoMigrate(&model.CommerceOperation{}, &model.CommerceApproval{}, &model.CommerceAttempt{}, &model.CommerceResource{}, &model.CommerceOutbox{}, &model.CommercePolicy{}, &model.CommerceMember{}, &model.CommerceEvidence{})
}

// Down retains financial/approval/idempotency facts; rollback stops new writes without deleting history.
func (*CommerceExecutionMigration) Down(context.Context) error { return nil }
