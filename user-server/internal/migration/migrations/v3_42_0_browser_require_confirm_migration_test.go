package migrations

import (
	"context"
	"testing"

	browsermodel "hivemtk-user/internal/browser_automation/model"
	"hivemtk-user/internal/pkg/testutil"

	"gorm.io/gorm"
)

// TestBrowserRequireConfirmMigration_Version 元信息（D7）
func TestBrowserRequireConfirmMigration_Version(t *testing.T) {
	m := NewBrowserRequireConfirmMigration(nil)
	if m.Version() != "v3.42.0" {
		t.Errorf("Version()=%q want=v3.42.0", m.Version())
	}
	if m.Name() == "" || m.Description() == "" {
		t.Error("Name/Description should not be empty")
	}
}

func TestBrowserRequireConfirmMigration_NilDB(t *testing.T) {
	m := NewBrowserRequireConfirmMigration(nil)
	if err := m.Up(context.Background()); err == nil {
		t.Error("nil db Up() 应返回错误")
	}
	if err := m.Down(context.Background()); err == nil {
		t.Error("nil db Down() 应返回错误")
	}
}

func hasRequireConfirm(t *testing.T, db *gorm.DB) bool {
	t.Helper()
	var exists bool
	if err := db.Raw(`SELECT EXISTS (SELECT 1 FROM information_schema.columns
		WHERE table_name='browser_tasks' AND column_name='require_confirm')`).Scan(&exists).Error; err != nil {
		t.Fatalf("列存在性查询失败: %v", err)
	}
	return exists
}

// TestBrowserRequireConfirmMigration_UpAndIdempotent 真 PG。
// 关键手法：先用 AutoMigrate 建表后**手动摘掉该列**，还原「迁移前 schema」——
// 否则模型标签自己就把列建出来了，Up 的有效性无从证明（假绿）。
// 之后验：列出现 + 类型 boolean + DEFAULT false（铁律 4 全自动的落点）+ 幂等 + gorm 往返 + Down 幂等。
func TestBrowserRequireConfirmMigration_UpAndIdempotent(t *testing.T) {
	db := testutil.NewTestDB(t, &browsermodel.BrowserTask{})
	ctx := context.Background()
	m := NewBrowserRequireConfirmMigration(db)

	if err := db.Exec(`ALTER TABLE browser_tasks DROP COLUMN IF EXISTS require_confirm`).Error; err != nil {
		t.Fatalf("预置迁移前 schema 失败: %v", err)
	}
	if hasRequireConfirm(t, db) {
		t.Fatal("预置条件不成立：列应已被摘除")
	}

	if err := m.Up(ctx); err != nil {
		t.Fatalf("Up() failed: %v", err)
	}
	if err := m.Up(ctx); err != nil {
		t.Fatalf("二次 Up() 应幂等: %v", err)
	}
	if !hasRequireConfirm(t, db) {
		t.Fatal("Up 后 require_confirm 列应存在")
	}
	var colType, colDefault string
	if err := db.Raw(`SELECT data_type, column_default FROM information_schema.columns
		WHERE table_name='browser_tasks' AND column_name='require_confirm'`).
		Row().Scan(&colType, &colDefault); err != nil {
		t.Fatalf("列元数据查询失败: %v", err)
	}
	if colType != "boolean" {
		t.Errorf("列类型应为 boolean，got %s", colType)
	}
	if colDefault != "false" {
		t.Errorf("默认值应为 false（默认全自动不变），got %q", colDefault)
	}

	task := browsermodel.BrowserTask{Name: "d7-default", Url: "https://example.com", UserID: 9001}
	if err := db.Create(&task).Error; err != nil {
		t.Fatalf("建任务失败: %v", err)
	}
	var got browsermodel.BrowserTask
	if err := db.First(&got, task.ID).Error; err != nil {
		t.Fatalf("读任务失败: %v", err)
	}
	if got.RequireConfirm {
		t.Error("新建任务默认应免确认")
	}
	if err := db.Model(&got).Update("require_confirm", true).Error; err != nil {
		t.Fatalf("写 require_confirm=true 失败: %v", err)
	}
	var again browsermodel.BrowserTask
	if err := db.First(&again, got.ID).Error; err != nil {
		t.Fatalf("复读失败: %v", err)
	}
	if !again.RequireConfirm {
		t.Error("require_confirm=true 未持久化（Executor 闸门拿不到确认语义）")
	}
	db.Unscoped().Delete(&browsermodel.BrowserTask{}, got.ID)

	if err := m.Down(ctx); err != nil {
		t.Fatalf("Down() failed: %v", err)
	}
	if err := m.Down(ctx); err != nil {
		t.Fatalf("二次 Down() 应幂等: %v", err)
	}
	if !hasRequireConfirm(t, db) {
		// browser_tasks 由模型/AutoMigrate 持有：降级删它的列等于销毁在用数据（本包口径，
		// 判据见 a_full_chain_migration_test.go）。
		t.Error("Down 后 require_confirm 必须仍在（降级不销毁在用列）")
	}
}
