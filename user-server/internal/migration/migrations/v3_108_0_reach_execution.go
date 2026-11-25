package migrations

import (
	"context"
	"errors"

	contentmodel "hivemtk-user/internal/content/model"
	"hivemtk-user/internal/migration"
	"hivemtk-user/internal/model"

	"gorm.io/gorm"
)

type ReachExecutionMigration struct{ db *gorm.DB }

func NewReachExecutionMigration(db *gorm.DB) *ReachExecutionMigration {
	return &ReachExecutionMigration{db: db}
}

var _ migration.Migration = (*ReachExecutionMigration)(nil)

func (*ReachExecutionMigration) Version() string { return "v3.108.0" }
func (*ReachExecutionMigration) Name() string    { return "Durable marketing execution" }
func (*ReachExecutionMigration) Description() string {
	return "Preserve graph versions, trigger identity, deadlines, leases and node dispatch evidence"
}
func (m *ReachExecutionMigration) Up(ctx context.Context) error {
	if m.db == nil {
		return errors.New("reach execution migration requires database")
	}
	return m.db.WithContext(ctx).AutoMigrate(&contentmodel.FlowExecution{}, &contentmodel.FlowNodeAttempt{}, &model.ContactConsentEvent{})
}

// 恢复旧应用不能删除已发生的派发及幂等证据。
func (*ReachExecutionMigration) Down(context.Context) error { return nil }
