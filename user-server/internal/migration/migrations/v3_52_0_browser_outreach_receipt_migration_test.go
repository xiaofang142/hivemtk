package migrations

import (
	"context"
	"testing"

	browsermodel "hivemtk-user/internal/browser_automation/model"
	"hivemtk-user/internal/migration"
	"hivemtk-user/internal/pkg/testutil"
)

// v3.52.0 建触达回执表。判据：元信息齐、nil 库报错、Up 幂等 + 三样交付物写读往返 +
// verified=false 也是合法回执（归因不到也要留痕）+ 同四元组重复行必须都能留下
// （回执不是闸门，唯一键会静默吞插入 → 「已发出但没回执」不可区分）+ 注册进链。

func TestOutreachReceiptMigration_Meta(t *testing.T) {
	m := NewBrowserOutreachReceiptMigration(nil)
	if m.Version() != "v3.52.0" {
		t.Errorf("Version()=%q want=v3.52.0", m.Version())
	}
	if m.Name() != "browser_outreach_receipts" {
		t.Errorf("Name()=%q want=browser_outreach_receipts", m.Name())
	}
	if m.Description() == "" {
		t.Error("Description should not be empty")
	}
}

func TestOutreachReceiptMigration_NilDB(t *testing.T) {
	m := NewBrowserOutreachReceiptMigration(nil)
	if err := m.Up(context.Background()); err == nil {
		t.Error("nil db Up() 应返回错误")
	}
	if err := m.Down(context.Background()); err == nil {
		t.Error("nil db Down() 应返回错误")
	}
}

func TestOutreachReceiptMigration_UpRoundtrip(t *testing.T) {
	db := testutil.NewTestDB(t, &browsermodel.BrowserOutreachReceipt{})
	ctx := context.Background()
	m := NewBrowserOutreachReceiptMigration(db)

	if err := m.Up(ctx); err != nil {
		t.Fatalf("Up() failed: %v", err)
	}
	if err := m.Up(ctx); err != nil {
		t.Fatalf("二次 Up() 应幂等: %v", err)
	}

	verified := &browsermodel.BrowserOutreachReceipt{
		TaskID: 9101, SessionID: 9201, Platform: "xiaohongshu", Action: "post_comment",
		TargetURL: "https://www.xiaohongshu.com/explore/abc", CopyHash: "hash-1",
		CopySnapshot: "测试评论正文", ScreenshotURL: "/files/browser_automation/session_9201.png",
		Evidence: []byte(`{"verified":true,"hit_count":2}`), Verified: true,
	}
	if err := db.Create(verified).Error; err != nil {
		t.Fatalf("Up 后写回执失败: %v", err)
	}
	var got browsermodel.BrowserOutreachReceipt
	if err := db.First(&got, verified.ID).Error; err != nil {
		t.Fatalf("复读失败: %v", err)
	}
	if got.TargetURL != verified.TargetURL || got.CopySnapshot != "测试评论正文" ||
		got.ScreenshotURL != verified.ScreenshotURL || !got.Verified {
		t.Errorf("回执往返不符：%+v", got)
	}
	if len(got.Evidence) == 0 {
		t.Errorf("evidence JSONB 往返丢失：%+v", got)
	}

	// verified=false 也是合法回执：归因不到同样是事实（双发闸据此叫人裁决）
	unknown := &browsermodel.BrowserOutreachReceipt{
		TaskID: 9101, SessionID: 9201, Platform: "xiaohongshu", Action: "post_comment",
		TargetURL: verified.TargetURL, CopyHash: verified.CopyHash, CopySnapshot: "测试评论正文",
	}
	if err := db.Create(unknown).Error; err != nil {
		t.Fatalf("verified=false 回执写入失败: %v", err)
	}

	// 同四元组重复行必须都留下：回执表没有唯一键约束
	var dup int64
	if err := db.Model(&browsermodel.BrowserOutreachReceipt{}).
		Where("task_id = ? AND copy_hash = ?", 9101, "hash-1").Count(&dup).Error; err != nil {
		t.Fatalf("统计失败: %v", err)
	}
	if dup != 2 {
		t.Errorf("同四元组回执行数=%d want 2（回执不是闸门，重复写入必须都留）", dup)
	}

	if err := m.Down(ctx); err != nil {
		t.Fatalf("Down() failed: %v", err)
	}
	var after int64
	if err := db.Model(&browsermodel.BrowserOutreachReceipt{}).
		Where("id IN ?", []uint{verified.ID, unknown.ID}).Count(&after).Error; err != nil {
		t.Fatalf("统计失败: %v", err)
	}
	if after != 2 {
		t.Errorf("Down 后存量行数=%d want 2（降级不得销毁验收证据）", after)
	}

	db.Unscoped().Delete(&browsermodel.BrowserOutreachReceipt{}, "id IN ?", []uint{verified.ID, unknown.ID})
}

func TestOutreachReceiptMigration_IsRegistered(t *testing.T) {
	reg := migration.NewMigrationRegistry()
	RegisterMigrations(reg, nil)
	m, ok := reg.Get("v3.52.0")
	if !ok {
		t.Fatal("v3.52.0 未注册进迁移链")
	}
	if m.Name() != "browser_outreach_receipts" {
		t.Errorf("注册到的迁移名不符：%s", m.Name())
	}
}
