package migrations

import (
	"context"
	"testing"

	"hivemtk-user/internal/migration"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/pkg/testutil"
)

// TestLeadNotifyOutboxMigrationRegistered v3.117.0 必须进入生产迁移注册表且版本不冲突。
func TestLeadNotifyOutboxMigrationRegistered(t *testing.T) {
	registry := migration.NewMigrationRegistry()
	RegisterMigrations(registry, nil)
	got, ok := registry.Get("v3.117.0")
	if !ok {
		t.Fatal("v3.117.0 未注册到生产迁移链")
	}
	if _, ok := got.(*LeadNotifyOutboxMigration); !ok {
		t.Fatalf("v3.117.0 类型不正确：%T", got)
	}
}

// TestLeadNotifyOutboxMigrationIdempotentAndLaneOnlyDown Up 可重入，Down 只删本 lane 两张表。
func TestLeadNotifyOutboxMigrationIdempotentAndLaneOnlyDown(t *testing.T) {
	db := testutil.NewTestDB(t, &model.LeadMiningNotifyRoute{})
	ctx := tenant.WithSystemScope(context.Background())
	route := &model.LeadMiningNotifyRoute{TenantID: 11, RouteID: "migration-sentinel", Scope: "tenant"}
	if err := db.WithContext(ctx).Create(route).Error; err != nil {
		t.Fatalf("创建非本 lane 哨兵失败：%v", err)
	}
	m := NewLeadNotifyOutboxMigration(db)
	if err := m.Up(ctx); err != nil {
		t.Fatalf("首次 Up 失败：%v", err)
	}
	if err := m.Up(ctx); err != nil {
		t.Fatalf("重复 Up 应幂等：%v", err)
	}
	for _, table := range []string{"lead_notify_outbox", "lead_notify_deliveries"} {
		if !db.Migrator().HasTable(table) {
			t.Fatalf("Up 后缺表 %s", table)
		}
	}
	if !db.Migrator().HasIndex("lead_notify_outbox", "uq_lead_notify_outbox_tenant_key") {
		t.Fatal("outbox 缺租户内幂等唯一索引")
	}
	if !db.Migrator().HasIndex("lead_notify_deliveries", "uq_lead_notify_delivery_key") {
		t.Fatal("delivery 缺租户内幂等唯一索引")
	}
	if err := m.Down(ctx); err != nil {
		t.Fatalf("首次 Down 失败：%v", err)
	}
	if err := m.Down(ctx); err != nil {
		t.Fatalf("重复 Down 应幂等：%v", err)
	}
	if db.Migrator().HasTable("lead_notify_outbox") || db.Migrator().HasTable("lead_notify_deliveries") {
		t.Fatal("Down 后本 lane 表仍存在")
	}
	var count int64
	if err := db.WithContext(ctx).Model(&model.LeadMiningNotifyRoute{}).Where("route_id = ?", route.RouteID).Count(&count).Error; err != nil || count != 1 {
		t.Fatalf("Down 误删非本 lane 数据：count=%d err=%v", count, err)
	}
}
