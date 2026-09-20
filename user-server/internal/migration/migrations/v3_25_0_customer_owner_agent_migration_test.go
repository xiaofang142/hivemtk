package migrations

import (
	"context"
	"testing"

	"hivemtk-user/internal/migration"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/testutil"
)

// v3.25.0 实现了 customers.owner_agent_id（S-4 专属坐席定向路由），却从未进
// RegisterMigrations 清单 ⇒ 迁移链里根本没有它。当前生产库靠模型字段
// （model/customer.go:62）由 AutoMigrate 建出该列，所以注册本身是安全的；
// 但「旧库缺列」这条 Up 真正会走的分支此前无人执行过，一并补上用例。

func TestCustomerOwnerAgentMigration_Meta(t *testing.T) {
	m := NewCustomerOwnerAgentMigration(nil)
	if m.Version() != "v3.25.0" {
		t.Errorf("Version()=%q want=v3.25.0", m.Version())
	}
	if m.Name() == "" || m.Description() == "" {
		t.Error("Name/Description should not be empty")
	}
}

func TestCustomerOwnerAgentMigration_NilDB(t *testing.T) {
	m := NewCustomerOwnerAgentMigration(nil)
	if err := m.Up(context.Background()); err == nil {
		t.Error("nil db Up() 应返回错误")
	}
}

func TestCustomerOwnerAgentMigration_IsRegistered(t *testing.T) {
	reg := migration.NewMigrationRegistry()
	RegisterMigrations(reg, nil)
	m, ok := reg.Get("v3.25.0")
	if !ok {
		t.Fatal("v3.25.0 未注册进迁移链")
	}
	if m.Name() != NewCustomerOwnerAgentMigration(nil).Name() {
		t.Errorf("注册到的迁移名不符：%s", m.Name())
	}
}

// TestCustomerOwnerAgentMigration_UpAddsMissingColumn 覆盖旧库分支：列不存在时必须补列 + 建索引。
func TestCustomerOwnerAgentMigration_UpAddsMissingColumn(t *testing.T) {
	db := testutil.NewTestDB(t, &model.Customer{})
	ctx := context.Background()

	if err := db.Exec(`ALTER TABLE customers DROP COLUMN IF EXISTS owner_agent_id`).Error; err != nil {
		t.Fatalf("预置旧库形状失败: %v", err)
	}
	if db.Migrator().HasColumn("customers", "owner_agent_id") {
		t.Fatal("预置条件不成立：owner_agent_id 应已被摘除")
	}

	m := NewCustomerOwnerAgentMigration(db)
	if err := m.Up(ctx); err != nil {
		t.Fatalf("Up() failed: %v", err)
	}
	if err := m.Up(ctx); err != nil {
		t.Fatalf("二次 Up() 应幂等: %v", err)
	}
	if !db.Migrator().HasColumn("customers", "owner_agent_id") {
		t.Fatal("Up 后 customers.owner_agent_id 应存在")
	}
	if indexDefinition(t, db, "idx_customers_owner_agent") == "" {
		t.Error("Up 后应有索引 idx_customers_owner_agent（定向路由按它查在线坐席）")
	}
}

// TestCustomerOwnerAgentMigration_UpOnCurrentSchema 现网形状（列已由模型建好）下 Up 必须
// 什么都不做，尤其不得再叠一个同列索引。
func TestCustomerOwnerAgentMigration_UpOnCurrentSchema(t *testing.T) {
	db := testutil.NewTestDB(t, &model.Customer{})
	ctx := context.Background()

	if err := NewCustomerOwnerAgentMigration(db).Up(ctx); err != nil {
		t.Fatalf("Up() failed: %v", err)
	}
	var n int
	if err := db.Raw(`SELECT COUNT(*) FROM pg_indexes WHERE tablename='customers'
		AND indexdef ILIKE '%owner_agent_id%'`).Scan(&n).Error; err != nil {
		t.Fatalf("索引计数失败: %v", err)
	}
	if n != 1 {
		t.Errorf("customers.owner_agent_id 上应只有 1 个索引（模型自带），got %d", n)
	}
}
