package migrations

import (
	"testing"
	"time"

	"hivemtk-user/internal/migration"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/pkg/testutil"

	"github.com/stretchr/testify/require"
)

// This fixture keeps the v3.102.0 schema independent of the current AutoMigrate model.
type merchantConnectionBeforeLifecycle struct {
	ID                   uint   `gorm:"primaryKey"`
	TenantID             uint   `gorm:"not null;uniqueIndex:uq_merchant_connection_target,priority:1"`
	Provider             string `gorm:"not null;size:64;uniqueIndex:uq_merchant_connection_target,priority:2"`
	ShopID               string `gorm:"not null;size:160;uniqueIndex:uq_merchant_connection_target,priority:3"`
	Version              int64  `gorm:"not null"`
	AttemptActor         uint   `gorm:"not null"`
	AttemptContract      string `gorm:"not null;size:64"`
	AttemptExpiresAt     *time.Time
	StateDigest          string   `gorm:"not null;size:64"`
	VerifierCiphertext   string   `gorm:"not null;type:text"`
	AttemptFailed        bool     `gorm:"not null"`
	CredentialVersion    int64    `gorm:"not null"`
	CredentialContract   string   `gorm:"not null;size:64"`
	CredentialCiphertext string   `gorm:"not null;type:text"`
	Scopes               []string `gorm:"serializer:json;type:text"`
	ExpiresAt            *time.Time
	CreatedAt            time.Time
	UpdatedAt            time.Time
}

func (merchantConnectionBeforeLifecycle) TableName() string { return "merchant_connections" }

// 原库中的凭证保留，新列默认未吊销、无刷新领取，重复升级和降级均保留新状态。
func TestMerchantConnectionLifecycleMigration_ExistingRowDefaults(t *testing.T) {
	db := testutil.NewTestDB(t, &merchantConnectionBeforeLifecycle{})
	ctx := tenant.WithSystemScope(t.Context())
	row := merchantConnectionBeforeLifecycle{TenantID: 71, Provider: "fixture", ShopID: "shop", Version: 3, CredentialVersion: 3, CredentialCiphertext: "migration-fixture"}
	require.NoError(t, db.WithContext(ctx).Create(&row).Error)
	registry := migration.NewMigrationRegistry()
	RegisterMigrations(registry, db)
	m, ok := registry.Get("v3.103.0")
	require.True(t, ok)
	require.NoError(t, m.Up(ctx))
	require.NoError(t, m.Up(ctx))
	var upgraded model.MerchantConnection
	require.NoError(t, db.WithContext(ctx).First(&upgraded, row.ID).Error)
	require.Equal(t, row.CredentialCiphertext, upgraded.CredentialCiphertext)
	require.Equal(t, row.CredentialVersion, upgraded.CredentialVersion)
	require.False(t, upgraded.Revoked)
	require.False(t, upgraded.RefreshFailed)
	require.Nil(t, upgraded.RefreshUntil)
	require.NoError(t, m.Down(ctx))
	require.True(t, db.Migrator().HasColumn(&model.MerchantConnection{}, "revoked"))
	require.True(t, db.Migrator().HasColumn(&model.MerchantConnection{}, "refresh_until"))
}
