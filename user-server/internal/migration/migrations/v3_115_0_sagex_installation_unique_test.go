package migrations

import (
	"context"
	"strings"
	"testing"

	"hivemtk-user/internal/migration"
	"hivemtk-user/internal/model"
	pdb "hivemtk-user/internal/pkg/db"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/pkg/testutil"

	"gorm.io/gorm"
)

func sageXInstallationUniqueMigration(t *testing.T, db *gorm.DB) migration.Migration {
	t.Helper()
	reg := migration.NewMigrationRegistry()
	RegisterMigrations(reg, db)
	m, ok := reg.Get("v3.115.0")
	if !ok || m == nil {
		t.Fatal("v3.115.0 SageX installation unique migration is not registered")
	}
	return m
}

func sageXInstallationSystemCtx() context.Context {
	return tenant.WithSystemScope(context.Background())
}

func TestSageXInstallationUniqueMigrationCreatesPartialIndex(t *testing.T) {
	tenant.SetMode(tenant.ModeOn)
	t.Cleanup(func() { tenant.SetMode(tenant.ModeOff) })

	db := testutil.NewTestDB(t, &model.SageXInstallation{})
	if err := db.Exec(`DROP INDEX IF EXISTS uq_sagex_installation_tenant_id`).Error; err != nil {
		t.Fatalf("drop model-created index before migration test: %v", err)
	}
	ctx := sageXInstallationSystemCtx()
	if err := db.WithContext(ctx).Create(&model.SageXInstallation{
		TenantID: 11, InstallationID: "migration-id", AccountID: "101", Token: "migration-token-a", Status: 1,
	}).Error; err != nil {
		t.Fatalf("seed installation: %v", err)
	}

	m := sageXInstallationUniqueMigration(t, db)
	if err := m.Up(ctx); err != nil {
		t.Fatalf("Up failed: %v", err)
	}
	if err := m.Up(ctx); err != nil {
		t.Fatalf("second Up must be idempotent: %v", err)
	}

	var indexDef string
	if err := db.Raw(`SELECT indexdef FROM pg_indexes WHERE tablename = 'sagex_installations'
		AND indexname = 'uq_sagex_installation_tenant_id'`).Scan(&indexDef).Error; err != nil {
		t.Fatalf("read index definition: %v", err)
	}
	for _, want := range []string{"UNIQUE", "tenant_id", "installation_id", "WHERE"} {
		if !strings.Contains(indexDef, want) {
			t.Fatalf("index definition %q missing %q", indexDef, want)
		}
	}

	if err := db.WithContext(ctx).Create(&model.SageXInstallation{
		TenantID: 11, InstallationID: "migration-id", AccountID: "101", Token: "migration-token-duplicate", Status: 1,
	}).Error; err == nil {
		t.Fatal("same tenant duplicate installation ID was accepted after migration")
	}
	if err := db.WithContext(ctx).Create(&model.SageXInstallation{
		TenantID: 22, InstallationID: "migration-id", AccountID: "202", Token: "migration-token-b", Status: 1,
	}).Error; err != nil {
		t.Fatalf("cross-tenant same installation ID must remain allowed: %v", err)
	}
	for i := 0; i < 2; i++ {
		if err := db.WithContext(ctx).Create(&model.SageXInstallation{
			TenantID: 11, AccountID: "101", Token: "migration-empty-" + string(rune('a'+i)), Status: 1,
		}).Error; err != nil {
			t.Fatalf("empty installation ID must remain outside partial index: %v", err)
		}
	}
}

func TestSageXInstallationUniqueMigrationFailsClosedOnDuplicates(t *testing.T) {
	tenant.SetMode(tenant.ModeOn)
	t.Cleanup(func() { tenant.SetMode(tenant.ModeOff) })

	db := testutil.NewTestDB(t, &model.SageXInstallation{})
	if err := db.Exec(`DROP INDEX IF EXISTS uq_sagex_installation_tenant_id`).Error; err != nil {
		t.Fatalf("drop index to seed legacy duplicates: %v", err)
	}
	ctx := sageXInstallationSystemCtx()
	for _, token := range []string{"duplicate-token-a", "duplicate-token-b"} {
		if err := db.WithContext(ctx).Create(&model.SageXInstallation{
			TenantID: 11, InstallationID: "duplicate-id", AccountID: "101", Token: token, Status: 1,
		}).Error; err != nil {
			t.Fatalf("seed duplicate row: %v", err)
		}
	}

	m := sageXInstallationUniqueMigration(t, db)
	err := m.Up(ctx)
	if err == nil || !strings.Contains(strings.ToLower(err.Error()), "duplicate") {
		t.Fatalf("Up must fail closed on duplicate data, got %v", err)
	}
	var count int64
	if err := db.WithContext(ctx).Model(&model.SageXInstallation{}).
		Where("tenant_id = ? AND installation_id = ?", 11, "duplicate-id").Count(&count).Error; err != nil {
		t.Fatalf("count duplicate rows: %v", err)
	}
	if count != 2 {
		t.Fatalf("migration silently deduplicated rows: count=%d want=2", count)
	}
	var indexCount int64
	if err := db.Raw(`SELECT count(*) FROM pg_indexes WHERE tablename = 'sagex_installations'
		AND indexname = 'uq_sagex_installation_tenant_id'`).Scan(&indexCount).Error; err != nil {
		t.Fatalf("count index after failed migration: %v", err)
	}
	if indexCount != 0 {
		t.Fatal("failed migration left partial unique index behind")
	}
}

func TestSageXInstallationUniqueMigrationDownDropsOnlyLaneIndex(t *testing.T) {
	tenant.SetMode(tenant.ModeOn)
	t.Cleanup(func() { tenant.SetMode(tenant.ModeOff) })

	db := testutil.NewTestDB(t, &model.SageXInstallation{})
	ctx := sageXInstallationSystemCtx()
	t.Cleanup(func() {
		_ = db.Exec(`
			CREATE UNIQUE INDEX IF NOT EXISTS uq_sagex_installation_tenant_id
			ON sagex_installations (tenant_id, installation_id)
			WHERE installation_id <> ''
		`).Error
	})
	if err := db.Exec(`DROP INDEX IF EXISTS uq_sagex_installation_tenant_id`).Error; err != nil {
		t.Fatalf("drop model-created index to simulate legacy schema: %v", err)
	}
	if err := db.WithContext(ctx).Create(&model.SageXInstallation{
		TenantID: 11, InstallationID: "down-id", AccountID: "101", Token: "down-token", Status: 1,
	}).Error; err != nil {
		t.Fatalf("seed installation: %v", err)
	}
	m := sageXInstallationUniqueMigration(t, db)
	if err := m.Up(ctx); err != nil {
		t.Fatalf("Up failed: %v", err)
	}
	if err := m.Down(ctx); err != nil {
		t.Fatalf("Down failed: %v", err)
	}
	var indexCount int64
	if err := db.Raw(`SELECT count(*) FROM pg_indexes WHERE tablename = 'sagex_installations'
		AND indexname = 'uq_sagex_installation_tenant_id'`).Scan(&indexCount).Error; err != nil {
		t.Fatalf("count index after Down: %v", err)
	}
	if indexCount != 0 {
		t.Fatal("Down did not drop index created by this migration")
	}
	var rowCount int64
	if err := db.WithContext(ctx).Model(&model.SageXInstallation{}).Count(&rowCount).Error; err != nil {
		t.Fatalf("count rows after Down: %v", err)
	}
	if rowCount != 1 {
		t.Fatalf("Down removed installation rows: count=%d", rowCount)
	}
}

func TestSageXInstallationUniqueMigrationDownPreservesPreexistingModelIndex(t *testing.T) {
	tenant.SetMode(tenant.ModeOn)
	t.Cleanup(func() { tenant.SetMode(tenant.ModeOff) })

	db := testutil.NewTestDB(t, &model.SageXInstallation{})
	ctx := sageXInstallationSystemCtx()
	if err := db.Exec(`
		CREATE UNIQUE INDEX IF NOT EXISTS uq_sagex_installation_tenant_id
		ON sagex_installations (tenant_id, installation_id)
		WHERE installation_id <> ''
	`).Error; err != nil {
		t.Fatalf("ensure model-owned baseline index: %v", err)
	}
	outsideIndex := db.Migrator().HasIndex("sagex_installations", "uq_sagex_installation_tenant_id")
	insideIndex := false
	if err := db.Transaction(func(tx *gorm.DB) error {
		insideIndex = tx.Migrator().HasIndex("sagex_installations", "uq_sagex_installation_tenant_id")
		return nil
	}); err != nil {
		t.Fatalf("transactional index probe failed: %v", err)
	}
	t.Logf("index ownership probe: outside=%v inside=%v", outsideIndex, insideIndex)
	if !outsideIndex {
		t.Fatal("model-owned index must exist before migration")
	}
	m := sageXInstallationUniqueMigration(t, db)
	if err := m.Up(ctx); err != nil {
		t.Fatalf("Up failed: %v", err)
	}
	concrete, ok := m.(*SageXInstallationUniqueMigration)
	if !ok {
		t.Fatalf("unexpected migration type %T", m)
	}
	if concrete.createdIndex {
		t.Fatalf("preexisting model index was marked as migration-created: outside=%v inside=%v", outsideIndex, insideIndex)
	}
	if err := m.Down(ctx); err != nil {
		t.Fatalf("Down failed: %v", err)
	}
	var indexCount int64
	if err := db.Raw(`SELECT count(*) FROM pg_indexes WHERE tablename = 'sagex_installations'
		AND indexname = 'uq_sagex_installation_tenant_id'`).Scan(&indexCount).Error; err != nil {
		t.Fatalf("count index after Down: %v", err)
	}
	if indexCount != 1 {
		t.Fatalf("Down removed model-owned baseline index: count=%d", indexCount)
	}
}

func TestSageXInstallationUniqueMigrationProductionBaselineOwnership(t *testing.T) {
	gdb := testutil.NewTestDB(t)
	if gdb == nil {
		t.Fatal("测试库不可达")
	}
	if n := chainTableCount(gdb); n != 0 {
		resetChainSchema(t, gdb)
	}
	t.Cleanup(func() { resetChainSchema(t, gdb) })
	prev := pdb.GetDB()
	pdb.SetTestDB(gdb)
	t.Cleanup(func() { pdb.SetTestDB(prev) })
	if err := runProductionAutoMigrate(gdb); err != nil {
		t.Fatalf("production AutoMigrate failed: %v", err)
	}

	outsideIndex := gdb.Migrator().HasIndex("sagex_installations", "uq_sagex_installation_tenant_id")
	insideIndex := false
	if err := gdb.Transaction(func(tx *gorm.DB) error {
		insideIndex = tx.Migrator().HasIndex("sagex_installations", "uq_sagex_installation_tenant_id")
		return nil
	}); err != nil {
		t.Fatalf("transactional index probe failed: %v", err)
	}
	t.Logf("production baseline index ownership probe: outside=%v inside=%v", outsideIndex, insideIndex)
	if !outsideIndex {
		t.Fatal("production AutoMigrate baseline must own the index")
	}
	m := NewSageXInstallationUniqueMigration(gdb)
	if err := m.Up(sageXInstallationSystemCtx()); err != nil {
		t.Fatalf("Up failed: %v", err)
	}
	if m.createdIndex {
		t.Fatalf("production baseline index was marked migration-created: outside=%v inside=%v", outsideIndex, insideIndex)
	}
}

func TestSageXInstallationUniqueMigrationNilDBFailsClosed(t *testing.T) {
	m := sageXInstallationUniqueMigration(t, nil)
	if err := m.Up(context.Background()); err == nil {
		t.Fatal("nil DB Up must fail closed")
	}
}
