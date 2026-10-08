package migrations

import (
	"context"
	"strings"
	"testing"

	browsermodel "hivemtk-user/internal/browser_automation/model"
	"hivemtk-user/internal/migration"
	"hivemtk-user/internal/pkg/testutil"
)

// v3.50.0 新建 browser_outreach_dedupe 表（四元组唯一键 + 已触达归属行）。
// 判据三件：元信息齐、nil 库报错、Up 幂等且唯一键真的拦住第二条同键插入 + 注册进链。

func TestOutreachDedupeMigration_Meta(t *testing.T) {
	m := NewBrowserOutreachDedupeMigration(nil)
	if m.Version() != "v3.50.0" {
		t.Errorf("Version()=%q want=v3.50.0", m.Version())
	}
	if m.Name() != "browser_outreach_dedupe" {
		t.Errorf("Name()=%q want=browser_outreach_dedupe", m.Name())
	}
	if m.Description() == "" {
		t.Error("Description should not be empty")
	}
}

func TestOutreachDedupeMigration_NilDB(t *testing.T) {
	m := NewBrowserOutreachDedupeMigration(nil)
	if err := m.Up(context.Background()); err == nil {
		t.Error("nil db Up() 应返回错误")
	}
	if err := m.Down(context.Background()); err == nil {
		t.Error("nil db Down() 应返回错误")
	}
}

func TestOutreachDedupeMigration_UpRoundtripAndUniqueKey(t *testing.T) {
	db := testutil.NewTestDB(t, &browsermodel.BrowserOutreachDedupe{})
	ctx := context.Background()
	m := NewBrowserOutreachDedupeMigration(db)

	if err := m.Up(ctx); err != nil {
		t.Fatalf("Up() failed: %v", err)
	}
	if err := m.Up(ctx); err != nil {
		t.Fatalf("二次 Up() 应幂等: %v", err)
	}

	seed := &browsermodel.BrowserOutreachDedupe{
		Platform: "xiaohongshu", TargetURL: "https://x.test/p/1",
		Action: "post_comment", CopyHash: "abc123", TaskID: 7, SessionID: 9,
	}
	if err := db.Create(seed).Error; err != nil {
		t.Fatalf("Up 后写入去重行失败: %v", err)
	}
	var got browsermodel.BrowserOutreachDedupe
	if err := db.Where("platform = ? AND copy_hash = ?", "xiaohongshu", "abc123").First(&got).Error; err != nil {
		t.Fatalf("复读失败: %v", err)
	}
	if got.TargetURL != "https://x.test/p/1" || got.Action != "post_comment" {
		t.Errorf("去重行复读不符：%+v", got)
	}

	// 跨任务双发闸的核心：同四元组、不同 task_id，第二次插入必须被唯一键拒。
	dup := &browsermodel.BrowserOutreachDedupe{
		Platform: "xiaohongshu", TargetURL: "https://x.test/p/1",
		Action: "post_comment", CopyHash: "abc123", TaskID: 8, SessionID: 10,
	}
	if err := db.Create(dup).Error; err == nil {
		t.Error("同四元组不同任务的第二条去重行应被唯一键拒绝（否则跨任务双发拦不住）")
	} else if !isUniqueViolation(err) {
		t.Errorf("第二次插入的错因应是唯一键冲突，实得：%v", err)
	}

	// 文案不同 ⇒ 是另一个触达对象，允许并存
	other := &browsermodel.BrowserOutreachDedupe{
		Platform: "xiaohongshu", TargetURL: "https://x.test/p/1",
		Action: "post_comment", CopyHash: "def456", TaskID: 8, SessionID: 10,
	}
	if err := db.Create(other).Error; err != nil {
		t.Errorf("文案不同的触达应允许并存：%v", err)
	}

	if err := m.Down(ctx); err != nil {
		t.Fatalf("Down() failed: %v", err)
	}
	// 降级不销毁在用表：已触达证据还在
	var after int64
	if err := db.Model(&browsermodel.BrowserOutreachDedupe{}).Count(&after).Error; err != nil {
		t.Fatalf("统计失败: %v", err)
	}
	if after != 2 {
		t.Errorf("Down 后去重行数=%d want 2（降级不得销毁已触达证据）", after)
	}

	db.Unscoped().Delete(&browsermodel.BrowserOutreachDedupe{}, "platform = ?", "xiaohongshu")
}

func TestOutreachDedupeMigration_IsRegistered(t *testing.T) {
	reg := migration.NewMigrationRegistry()
	RegisterMigrations(reg, nil)
	m, ok := reg.Get("v3.50.0")
	if !ok {
		t.Fatal("v3.50.0 未注册进迁移链")
	}
	if m.Name() != "browser_outreach_dedupe" {
		t.Errorf("注册到的迁移名不符：%s", m.Name())
	}
}

// isUniqueViolation 判错因是否为唯一键冲突（跨驱动口径差，不硬编码错误码串）。
func isUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	for _, kw := range []string{"UNIQUE", "unique", "duplicate key", "Duplicate entry"} {
		if strings.Contains(msg, kw) {
			return true
		}
	}
	return false
}
