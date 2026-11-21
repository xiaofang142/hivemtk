package migrations

import (
	"testing"

	"hivemtk-user/internal/migration"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/pkg/testutil"

	"github.com/stretchr/testify/require"
)

func TestMerchantConnectionMigration_RegisteredAndIdempotent(t *testing.T) {
	db := testutil.NewTestDB(t)
	ctx := tenant.WithSystemScope(t.Context())
	registry := migration.NewMigrationRegistry()
	RegisterMigrations(registry, db)
	m, ok := registry.Get("v3.102.0")
	require.True(t, ok)
	require.NoError(t, m.Up(ctx))
	require.NoError(t, m.Up(ctx))
	require.NoError(t, m.Down(ctx))
	require.True(t, db.Migrator().HasTable(&model.MerchantConnection{}))
	require.True(t, db.Migrator().HasIndex(&model.MerchantConnection{}, "uq_merchant_connection_target"))
}
