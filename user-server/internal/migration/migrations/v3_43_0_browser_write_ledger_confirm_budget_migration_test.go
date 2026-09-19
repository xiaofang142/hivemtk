package migrations

import (
	"context"
	"testing"

	browsermodel "hivemtk-user/internal/browser_automation/model"
	"hivemtk-user/internal/migration"
	"hivemtk-user/internal/pkg/testutil"

	"gorm.io/gorm"
)

// v3.43.0 覆盖批6–批8 的四列两索引。判据手法沿用 v3.42：先 AutoMigrate 建表再**手动摘列**，
// 还原「迁移前 schema」——否则模型标签自己就把列建好了，Up 到底做了什么无从证明。

func TestBrowserWriteLedgerConfirmBudgetMigration_Meta(t *testing.T) {
	m := NewBrowserWriteLedgerConfirmBudgetMigration(nil)
	if m.Version() != "v3.43.0" {
		t.Errorf("Version()=%q want=v3.43.0", m.Version())
	}
	if m.Name() == "" || m.Description() == "" {
		t.Error("Name/Description should not be empty")
	}
}

func TestBrowserWriteLedgerConfirmBudgetMigration_NilDB(t *testing.T) {
	m := NewBrowserWriteLedgerConfirmBudgetMigration(nil)
	if err := m.Up(context.Background()); err == nil {
		t.Error("nil db Up() 应返回错误")
	}
	if err := m.Down(context.Background()); err == nil {
		t.Error("nil db Down() 应返回错误")
	}
}

func hasColumn(t *testing.T, db *gorm.DB, table, column string) bool {
	t.Helper()
	var exists bool
	if err := db.Raw(`SELECT EXISTS (SELECT 1 FROM information_schema.columns
		WHERE table_name=? AND column_name=?)`, table, column).Scan(&exists).Error; err != nil {
		t.Fatalf("列存在性查询失败 %s.%s: %v", table, column, err)
	}
	return exists
}

func hasIndex(t *testing.T, db *gorm.DB, name string) bool {
	t.Helper()
	var exists bool
	if err := db.Raw(`SELECT EXISTS (SELECT 1 FROM pg_indexes WHERE indexname=?)`, name).
		Scan(&exists).Error; err != nil {
		t.Fatalf("索引存在性查询失败 %s: %v", name, err)
	}
	return exists
}

func columnDefault(t *testing.T, db *gorm.DB, table, column string) (string, string) {
	t.Helper()
	var dataType, colDefault string
	if err := db.Raw(`SELECT data_type, column_default FROM information_schema.columns
		WHERE table_name=? AND column_name=?`, table, column).Row().Scan(&dataType, &colDefault); err != nil {
		t.Fatalf("列元数据查询失败 %s.%s: %v", table, column, err)
	}
	return dataType, colDefault
}

var b43StepCols = []string{"submit_state", "text_hash", "is_write"}

// TestBrowserWriteLedgerConfirmBudgetMigration_UpAndIdempotent 真 PG 全量往返。
func TestBrowserWriteLedgerConfirmBudgetMigration_UpAndIdempotent(t *testing.T) {
	db := testutil.NewTestDB(t, &browsermodel.BrowserStep{}, &browsermodel.BrowserTask{})
	ctx := context.Background()
	m := NewBrowserWriteLedgerConfirmBudgetMigration(db)

	for _, c := range b43StepCols {
		if err := db.Exec(`ALTER TABLE browser_steps DROP COLUMN IF EXISTS ` + c).Error; err != nil {
			t.Fatalf("预置迁移前 schema 失败 %s: %v", c, err)
		}
	}
	if err := db.Exec(`ALTER TABLE browser_tasks DROP COLUMN IF EXISTS confirm_wait_sec`).Error; err != nil {
		t.Fatalf("预置迁移前 schema 失败 confirm_wait_sec: %v", err)
	}
	for _, c := range b43StepCols {
		if hasColumn(t, db, "browser_steps", c) {
			t.Fatalf("预置条件不成立：browser_steps.%s 应已被摘除", c)
		}
	}
	if hasColumn(t, db, "browser_tasks", "confirm_wait_sec") {
		t.Fatal("预置条件不成立：confirm_wait_sec 应已被摘除")
	}

	if err := m.Up(ctx); err != nil {
		t.Fatalf("Up() failed: %v", err)
	}
	if err := m.Up(ctx); err != nil {
		t.Fatalf("二次 Up() 应幂等: %v", err)
	}

	for _, c := range b43StepCols {
		if !hasColumn(t, db, "browser_steps", c) {
			t.Fatalf("Up 后 browser_steps.%s 应存在", c)
		}
	}
	// 台账闸门靠这两条索引扫「同任务同正文是否已提交过」；缺索引=全表扫，闸门越用越慢
	for _, idx := range []string{"idx_browser_steps_submit_state", "idx_browser_steps_text_hash"} {
		if !hasIndex(t, db, idx) {
			t.Errorf("Up 后应有索引 %s（防双发闸门的查询路径）", idx)
		}
	}
	if dt, def := columnDefault(t, db, "browser_steps", "submit_state"); dt != "character varying" || def != "''::character varying" {
		t.Errorf("submit_state 应为 varchar 默认空串，got %s/%q", dt, def)
	}
	if dt, def := columnDefault(t, db, "browser_steps", "is_write"); dt != "boolean" || def != "false" {
		t.Errorf("is_write 应为 boolean 默认 false，got %s/%q", dt, def)
	}
	// 缺省值必须等于代码缺省值：列默认 0 会让「老行」读成「不等确认」，与代码的 600s 口径冲突
	if dt, def := columnDefault(t, db, "browser_tasks", "confirm_wait_sec"); dt != "integer" || def != "600" {
		t.Errorf("confirm_wait_sec 应为 integer 默认 600，got %s/%q", dt, def)
	}

	// gorm 往返：不显式赋值时读回默认，显式赋值后不得丢
	task := browsermodel.BrowserTask{Name: "b43-default", Url: "https://example.com", UserID: 9101}
	if err := db.Create(&task).Error; err != nil {
		t.Fatalf("建任务失败: %v", err)
	}
	var got browsermodel.BrowserTask
	if err := db.First(&got, task.ID).Error; err != nil {
		t.Fatalf("读任务失败: %v", err)
	}
	if got.ConfirmWaitSec != 600 {
		t.Errorf("未指定时应取默认 600，got %d", got.ConfirmWaitSec)
	}
	if err := db.Model(&got).Update("confirm_wait_sec", 900).Error; err != nil {
		t.Fatalf("写 confirm_wait_sec 失败: %v", err)
	}
	var again browsermodel.BrowserTask
	if err := db.First(&again, got.ID).Error; err != nil {
		t.Fatalf("复读失败: %v", err)
	}
	if again.ConfirmWaitSec != 900 {
		t.Errorf("confirm_wait_sec=900 未持久化，got %d（D7 预算解耦失效）", again.ConfirmWaitSec)
	}

	step := browsermodel.BrowserStep{SessionID: 1, TaskID: 1, StepIndex: 0, Action: "post_comment"}
	if err := db.Create(&step).Error; err != nil {
		t.Fatalf("建步失败: %v", err)
	}
	var gs browsermodel.BrowserStep
	if err := db.First(&gs, step.ID).Error; err != nil {
		t.Fatalf("读步失败: %v", err)
	}
	if gs.SubmitState != "" || gs.TextHash != "" || gs.IsWrite {
		t.Errorf("台账列默认应为空态（空=无提交记录，闸门据此放行首次执行），got %q/%q/%v",
			gs.SubmitState, gs.TextHash, gs.IsWrite)
	}
	if err := db.Model(&gs).Updates(map[string]any{"submit_state": "verified", "text_hash": "0a0b0c0d", "is_write": true}).Error; err != nil {
		t.Fatalf("写台账列失败: %v", err)
	}
	var gs2 browsermodel.BrowserStep
	if err := db.First(&gs2, step.ID).Error; err != nil {
		t.Fatalf("复读台账失败: %v", err)
	}
	if gs2.SubmitState != "verified" || gs2.TextHash != "0a0b0c0d" || !gs2.IsWrite {
		t.Errorf("台账回写未持久化：%q/%q/%v（防双发闸门重启后即失忆）",
			gs2.SubmitState, gs2.TextHash, gs2.IsWrite)
	}

	db.Unscoped().Delete(&browsermodel.BrowserTask{}, got.ID)
	db.Unscoped().Delete(&browsermodel.BrowserStep{}, step.ID)

	if err := m.Down(ctx); err != nil {
		t.Fatalf("Down() failed: %v", err)
	}
	if err := m.Down(ctx); err != nil {
		t.Fatalf("二次 Down() 应幂等: %v", err)
	}
	for _, c := range b43StepCols {
		if hasColumn(t, db, "browser_steps", c) {
			t.Errorf("Down 后 browser_steps.%s 应已删除", c)
		}
	}
	if hasColumn(t, db, "browser_tasks", "confirm_wait_sec") {
		t.Error("Down 后 confirm_wait_sec 应已删除")
	}
	for _, idx := range []string{"idx_browser_steps_submit_state", "idx_browser_steps_text_hash"} {
		if hasIndex(t, db, idx) {
			t.Errorf("Down 后索引 %s 应已删除", idx)
		}
	}
}

// 迁移必须真的挂进启动注册链——只写文件不注册等于没写（AutoMigrate 之外的来路拿不到列）。
// 走 registry 而非读源码：注册漏了/版本写错/与既有版本撞号，这里都会直接红。
func TestBrowserWriteLedgerConfirmBudgetMigration_IsRegistered(t *testing.T) {
	reg := migration.NewMigrationRegistry()
	RegisterMigrations(reg, nil)
	m, ok := reg.Get("v3.43.0")
	if !ok {
		t.Fatal("v3.43.0 未注册进迁移链")
	}
	if m.Name() != "browser_write_ledger_confirm_budget" {
		t.Errorf("注册到的迁移名不符：%s", m.Name())
	}
}
