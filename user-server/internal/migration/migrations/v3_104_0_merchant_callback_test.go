package migrations

import (
	"testing"

	"hivemtk-user/internal/migration"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tenancyidx"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/pkg/testutil"

	"github.com/stretchr/testify/require"
)

func TestMerchantCallbackMigration_UpgradePreservesUnprovisionedConnections(t *testing.T) {
	db := testutil.NewTestDB(t, &merchantConnectionBeforeLifecycle{})
	ctx := tenant.WithSystemScope(t.Context())
	db = db.WithContext(ctx)
	conn := merchantConnectionBeforeLifecycle{TenantID: 71, Provider: "fixture", ShopID: "fixture-shop", Version: 3,
		CredentialVersion: 3, CredentialCiphertext: "migration-fixture"}
	require.NoError(t, db.Create(&conn).Error)
	registry := migration.NewMigrationRegistry()
	RegisterMigrations(registry, db)
	m, ok := registry.Get("v3.104.0")
	require.True(t, ok)
	require.NoError(t, m.Up(ctx))
	require.NoError(t, m.Up(ctx))
	_, err := tenancyidx.Normalize(db)
	require.NoError(t, err)
	var upgraded model.MerchantConnection
	require.NoError(t, db.First(&upgraded, conn.ID).Error)
	require.Equal(t, conn.CredentialVersion, upgraded.CredentialVersion)
	require.Equal(t, conn.CredentialCiphertext, upgraded.CredentialCiphertext)
	require.Zero(t, upgraded.ConsentVersion)
	var count int64
	require.NoError(t, db.Model(&model.MerchantCallbackSubscription{}).Count(&count).Error)
	require.Zero(t, count)
	require.NoError(t, m.Down(ctx))
	require.True(t, db.Migrator().HasTable(&model.MerchantCallbackEvent{}))
	require.True(t, db.Migrator().HasColumn(&model.MerchantConnection{}, "consent_version"))
}
