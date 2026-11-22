package migrations

import (
	"hivemtk-user/internal/migration"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/pkg/testutil"
	"testing"
)

// 升级注册可重复执行，Down不删除历史操作/资金防重事实。
func TestCommerceMigration_RegisteredAndIdempotent(t *testing.T) {
	db := testutil.NewTestDB(t, &model.CommerceOperation{})
	ctx := tenant.WithSystemScope(t.Context())
	registry := migration.NewMigrationRegistry()
	RegisterMigrations(registry, db)
	m, ok := registry.Get("v3.107.0")
	if !ok {
		t.Fatal("缺少commerce迁移注册")
	}
	for i := 0; i < 2; i++ {
		if e := m.Up(ctx); e != nil {
			t.Fatal(e)
		}
	}
	for _, table := range []string{"commerce_operations", "commerce_approvals", "commerce_attempts", "commerce_resources", "commerce_outbox", "commerce_policies", "commerce_members", "commerce_evidences"} {
		if !db.Migrator().HasTable(table) {
			t.Fatalf("缺少表%s", table)
		}
	}
	if e := m.Down(ctx); e != nil {
		t.Fatal(e)
	}
	if !db.Migrator().HasTable(&model.CommerceOperation{}) {
		t.Fatal("回滚删除了历史账本")
	}
}
