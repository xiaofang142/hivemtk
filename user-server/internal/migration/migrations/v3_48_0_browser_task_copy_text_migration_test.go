package migrations

import (
	"context"
	"testing"

	browsermodel "hivemtk-user/internal/browser_automation/model"
	"hivemtk-user/internal/migration"
	"hivemtk-user/internal/pkg/testutil"
)

// v3.48.0 只加一列 copy_text（TEXT NOT NULL DEFAULT ''），无回填、无可空性博弈。
// 判据三件：元信息齐、nil 库报错、Up 幂等且写读往返 + 注册进链。

func TestTaskCopyTextMigration_Meta(t *testing.T) {
	m := NewBrowserTaskCopyTextMigration(nil)
	if m.Version() != "v3.48.0" {
		t.Errorf("Version()=%q want=v3.48.0", m.Version())
	}
	if m.Name() != "browser_tasks_copy_text" {
		t.Errorf("Name()=%q want=browser_tasks_copy_text", m.Name())
	}
	if m.Description() == "" {
		t.Error("Description should not be empty")
	}
}

func TestTaskCopyTextMigration_NilDB(t *testing.T) {
	m := NewBrowserTaskCopyTextMigration(nil)
	if err := m.Up(context.Background()); err == nil {
		t.Error("nil db Up() 应返回错误")
	}
	if err := m.Down(context.Background()); err == nil {
		t.Error("nil db Down() 应返回错误")
	}
}

func TestTaskCopyTextMigration_UpRoundtrip(t *testing.T) {
	db := testutil.NewTestDB(t, &browsermodel.BrowserTask{})
	ctx := context.Background()
	m := NewBrowserTaskCopyTextMigration(db)

	if err := m.Up(ctx); err != nil {
		t.Fatalf("Up() failed: %v", err)
	}
	if err := m.Up(ctx); err != nil {
		t.Fatalf("二次 Up() 应幂等: %v", err)
	}

	seed := &browsermodel.BrowserTask{Name: "copytext-rt", TaskType: "one_shot", Status: "draft",
		Url: "https://example.com", UserID: 4801, CopyText: "好评，已关注"}
	if err := db.Create(seed).Error; err != nil {
		t.Fatalf("Up 后写入文案失败: %v", err)
	}
	var got browsermodel.BrowserTask
	if err := db.First(&got, seed.ID).Error; err != nil {
		t.Fatalf("复读失败: %v", err)
	}
	if got.CopyText != "好评，已关注" {
		t.Errorf("文案复读=%q want 好评，已关注", got.CopyText)
	}
	// 空文案是显式语义（无文案），不是 NULL：planRound 靠它判回退
	var blank browsermodel.BrowserTask
	if err := db.Where("id = ?", seed.ID).First(&blank).Error; err != nil {
		t.Fatalf("复读失败: %v", err)
	}
	plain := &browsermodel.BrowserTask{Name: "copytext-blank", TaskType: "one_shot", Status: "draft",
		Url: "https://example.com", UserID: 4801}
	if err := db.Create(plain).Error; err != nil {
		t.Fatalf("空文案写入失败: %v", err)
	}
	var gotBlank browsermodel.BrowserTask
	if err := db.First(&gotBlank, plain.ID).Error; err != nil {
		t.Fatalf("复读失败: %v", err)
	}
	if gotBlank.CopyText != "" {
		t.Errorf("缺省文案=%q want 空串", gotBlank.CopyText)
	}

	if err := m.Down(ctx); err != nil {
		t.Fatalf("Down() failed: %v", err)
	}
	// 降级不销毁在用列：两行都在
	var after int64
	if err := db.Model(&browsermodel.BrowserTask{}).Where("id IN ?", []uint{seed.ID, plain.ID}).Count(&after).Error; err != nil {
		t.Fatalf("统计失败: %v", err)
	}
	if after != 2 {
		t.Errorf("Down 后存量行数=%d want 2（降级不得销毁任务行）", after)
	}

	db.Unscoped().Delete(&browsermodel.BrowserTask{}, "id IN ?", []uint{seed.ID, plain.ID})
}

func TestTaskCopyTextMigration_IsRegistered(t *testing.T) {
	reg := migration.NewMigrationRegistry()
	RegisterMigrations(reg, nil)
	m, ok := reg.Get("v3.48.0")
	if !ok {
		t.Fatal("v3.48.0 未注册进迁移链")
	}
	if m.Name() != "browser_tasks_copy_text" {
		t.Errorf("注册到的迁移名不符：%s", m.Name())
	}
}
