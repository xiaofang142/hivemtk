package migrations

import (
	"context"
	"errors"

	"hivemtk-user/internal/migration"
	"hivemtk-user/internal/model"

	"gorm.io/gorm"
)

// CapabilityEvidenceMigration creates the two empty EC-01 tables for explicit upgrade runs.
// The standard application startup also registers the models with AutoMigrate.
type CapabilityEvidenceMigration struct{ db *gorm.DB }

func NewCapabilityEvidenceMigration(db *gorm.DB) *CapabilityEvidenceMigration {
	return &CapabilityEvidenceMigration{db: db}
}

var _ migration.Migration = (*CapabilityEvidenceMigration)(nil)

func (*CapabilityEvidenceMigration) Version() string { return "v3.106.0" }
func (*CapabilityEvidenceMigration) Name() string    { return "EC-01 持久能力证据" }

func (*CapabilityEvidenceMigration) Description() string {
	return "Create EC-01 capability evidence and source bindings"
}

func (m *CapabilityEvidenceMigration) Up(ctx context.Context) error {
	if m.db == nil {
		return errors.New("capability evidence migration requires a database")
	}
	return m.db.WithContext(ctx).AutoMigrate(&model.CapabilitySourceBinding{}, &model.CapabilityEvidence{})
}

// Down preserves permission evidence and binding history.
func (*CapabilityEvidenceMigration) Down(context.Context) error { return nil }
