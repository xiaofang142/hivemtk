package migrations

import (
	"context"
	"testing"

	browsermodel "hivemtk-user/internal/browser_automation/model"
	"hivemtk-user/internal/migration"
	"hivemtk-user/internal/pkg/testutil"
)

// v3.51.0 加活动级预算四列（token_budget / campaign_key / campaign_act_budget / campaign_act_used），
// 全部「0/空 = 不限」。判据：元信息齐、nil 库报错、Up 幂等 + 写读往返 + 缺省语义
// （未配置任务读回来必须是 0/空，而不是 NULL——执行器靠它判「不限」）、注册进链。

func TestTaskCampaignBudgetMigration_Meta(t *testing.T) {
	m := NewBrowserTaskCampaignBudgetMigration(nil)
	if m.Version() != "v3.51.0" {
		t.Errorf("Version()=%q want=v3.51.0", m.Version())
	}
	if m.Name() != "browser_tasks_campaign_budget" {
		t.Errorf("Name()=%q want=browser_tasks_campaign_budget", m.Name())
	}
	if m.Description() == "" {
		t.Error("Description should not be empty")
	}
}

func TestTaskCampaignBudgetMigration_NilDB(t *testing.T) {
	m := NewBrowserTaskCampaignBudgetMigration(nil)
	if err := m.Up(context.Background()); err == nil {
		t.Error("nil db Up() 应返回错误")
	}
	if err := m.Down(context.Background()); err == nil {
		t.Error("nil db Down() 应返回错误")
	}
}

func TestTaskCampaignBudgetMigration_UpRoundtrip(t *testing.T) {
	db := testutil.NewTestDB(t, &browsermodel.BrowserTask{})
	ctx := context.Background()
	m := NewBrowserTaskCampaignBudgetMigration(db)

	if err := m.Up(ctx); err != nil {
		t.Fatalf("Up() failed: %v", err)
	}
	if err := m.Up(ctx); err != nil {
		t.Fatalf("二次 Up() 应幂等: %v", err)
	}

	budgeted := &browsermodel.BrowserTask{Name: "campaign-budgeted", TaskType: "cron", Status: "draft",
		Url: "https://example.com", UserID: 5101,
		TokenBudget: 120000, CampaignKey: "2026-10-autumn", CampaignActBudget: 50, CampaignActUsed: 7}
	if err := db.Create(budgeted).Error; err != nil {
		t.Fatalf("Up 后写入预算字段失败: %v", err)
	}
	var got browsermodel.BrowserTask
	if err := db.First(&got, budgeted.ID).Error; err != nil {
		t.Fatalf("复读失败: %v", err)
	}
	if got.TokenBudget != 120000 || got.CampaignKey != "2026-10-autumn" ||
		got.CampaignActBudget != 50 || got.CampaignActUsed != 7 {
		t.Errorf("预算字段往返不符：%+v", got)
	}

	// 缺省语义：未配置预算的任务读回来必须是 0/空（不限），不能是 NULL
	plain := &browsermodel.BrowserTask{Name: "campaign-blank", TaskType: "one_shot", Status: "draft",
		Url: "https://example.com", UserID: 5101}
	if err := db.Create(plain).Error; err != nil {
		t.Fatalf("缺省任务写入失败: %v", err)
	}
	var gotPlain browsermodel.BrowserTask
	if err := db.First(&gotPlain, plain.ID).Error; err != nil {
		t.Fatalf("复读失败: %v", err)
	}
	if gotPlain.TokenBudget != 0 || gotPlain.CampaignKey != "" ||
		gotPlain.CampaignActBudget != 0 || gotPlain.CampaignActUsed != 0 {
		t.Errorf("缺省预算=%+v want 全 0/空（0=不限）", gotPlain)
	}

	// 活动键可索引（看板按活动归组）
	var count int64
	if err := db.Model(&browsermodel.BrowserTask{}).Where("campaign_key = ?", "2026-10-autumn").Count(&count).Error; err != nil {
		t.Fatalf("按活动键查询失败: %v", err)
	}
	if count != 1 {
		t.Errorf("按 campaign_key 查到 %d 行 want 1", count)
	}

	if err := m.Down(ctx); err != nil {
		t.Fatalf("Down() failed: %v", err)
	}
	var after int64
	if err := db.Model(&browsermodel.BrowserTask{}).Where("id IN ?", []uint{budgeted.ID, plain.ID}).Count(&after).Error; err != nil {
		t.Fatalf("统计失败: %v", err)
	}
	if after != 2 {
		t.Errorf("Down 后存量行数=%d want 2（降级不得销毁任务行）", after)
	}

	db.Unscoped().Delete(&browsermodel.BrowserTask{}, "id IN ?", []uint{budgeted.ID, plain.ID})
}

func TestTaskCampaignBudgetMigration_IsRegistered(t *testing.T) {
	reg := migration.NewMigrationRegistry()
	RegisterMigrations(reg, nil)
	m, ok := reg.Get("v3.51.0")
	if !ok {
		t.Fatal("v3.51.0 未注册进迁移链")
	}
	if m.Name() != "browser_tasks_campaign_budget" {
		t.Errorf("注册到的迁移名不符：%s", m.Name())
	}
}
