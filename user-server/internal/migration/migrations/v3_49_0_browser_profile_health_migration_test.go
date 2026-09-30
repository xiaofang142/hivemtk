package migrations

import (
	"context"
	"testing"

	browsermodel "hivemtk-user/internal/browser_automation/model"
	"hivemtk-user/internal/migration"
	"hivemtk-user/internal/pkg/testutil"
)

// v3.49.0 新建 browser_profile_health 表（平台唯一键 + 熔断标记 + 熔断原因）。
// 判据三件：元信息齐、nil 库报错、Up 幂等且写读往返 + 注册进链。

func TestProfileHealthMigration_Meta(t *testing.T) {
	m := NewBrowserProfileHealthMigration(nil)
	if m.Version() != "v3.49.0" {
		t.Errorf("Version()=%q want=v3.49.0", m.Version())
	}
	if m.Name() != "browser_profile_health" {
		t.Errorf("Name()=%q want=browser_profile_health", m.Name())
	}
	if m.Description() == "" {
		t.Error("Description should not be empty")
	}
}

func TestProfileHealthMigration_NilDB(t *testing.T) {
	m := NewBrowserProfileHealthMigration(nil)
	if err := m.Up(context.Background()); err == nil {
		t.Error("nil db Up() 应返回错误")
	}
	if err := m.Down(context.Background()); err == nil {
		t.Error("nil db Down() 应返回错误")
	}
}

func TestProfileHealthMigration_UpRoundtrip(t *testing.T) {
	db := testutil.NewTestDB(t, &browsermodel.BrowserProfileHealth{})
	ctx := context.Background()
	m := NewBrowserProfileHealthMigration(db)

	if err := m.Up(ctx); err != nil {
		t.Fatalf("Up() failed: %v", err)
	}
	if err := m.Up(ctx); err != nil {
		t.Fatalf("二次 Up() 应幂等: %v", err)
	}

	seed := &browsermodel.BrowserProfileHealth{Platform: "xiaohongshu", Blocked: true, Reason: "rt-封锁"}
	if err := db.Create(seed).Error; err != nil {
		t.Fatalf("Up 后写入熔断行失败: %v", err)
	}
	var got browsermodel.BrowserProfileHealth
	if err := db.Where("platform = ?", "xiaohongshu").First(&got).Error; err != nil {
		t.Fatalf("复读失败: %v", err)
	}
	if !got.Blocked || got.Reason != "rt-封锁" {
		t.Errorf("熔断行复读 blocked=%v reason=%q want true/rt-封锁", got.Blocked, got.Reason)
	}

	if err := m.Down(ctx); err != nil {
		t.Fatalf("Down() failed: %v", err)
	}
	// 降级不销毁在用表：熔断行还在
	var after int64
	if err := db.Model(&browsermodel.BrowserProfileHealth{}).Where("platform = ?", "xiaohongshu").Count(&after).Error; err != nil {
		t.Fatalf("统计失败: %v", err)
	}
	if after != 1 {
		t.Errorf("Down 后熔断行数=%d want 1（降级不得销毁审计行）", after)
	}

	db.Unscoped().Delete(&browsermodel.BrowserProfileHealth{}, "platform = ?", "xiaohongshu")
}

func TestProfileHealthMigration_IsRegistered(t *testing.T) {
	reg := migration.NewMigrationRegistry()
	RegisterMigrations(reg, nil)
	m, ok := reg.Get("v3.49.0")
	if !ok {
		t.Fatal("v3.49.0 未注册进迁移链")
	}
	if m.Name() != "browser_profile_health" {
		t.Errorf("注册到的迁移名不符：%s", m.Name())
	}
}
