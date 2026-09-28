package migrations

import (
	"context"
	"testing"

	browsermodel "hivemtk-user/internal/browser_automation/model"
	"hivemtk-user/internal/migration"
	"hivemtk-user/internal/pkg/testutil"

	"gorm.io/gorm"
)

// v3.44.0 只动一列的可空性与默认值，外加一次存量回填。判据手法沿用 v3.42/v3.43：
// AutoMigrate 按模型标签建表（*bool ⇒ 可空），所以必须先把列**手动改回**旧形状，
// 否则 Up 什么都没做也能全绿——尤其证明不了「NOT NULL 被放开」这一件，而那正是新代码
// 第一次写 nil 会不会当场报错的分界。

func TestCommandLogOkTriStateMigration_Meta(t *testing.T) {
	m := NewBrowserCommandLogOkTriStateMigration(nil)
	if m.Version() != "v3.44.0" {
		t.Errorf("Version()=%q want=v3.44.0", m.Version())
	}
	if m.Name() == "" || m.Description() == "" {
		t.Error("Name/Description should not be empty")
	}
}

func TestCommandLogOkTriStateMigration_NilDB(t *testing.T) {
	m := NewBrowserCommandLogOkTriStateMigration(nil)
	if err := m.Up(context.Background()); err == nil {
		t.Error("nil db Up() 应返回错误")
	}
	if err := m.Down(context.Background()); err == nil {
		t.Error("nil db Down() 应返回错误")
	}
}

func columnNullable(t *testing.T, db *gorm.DB, table, column string) (nullable bool, hasDefault bool) {
	t.Helper()
	var isNullable string
	var def *string
	if err := db.Raw(`SELECT is_nullable, column_default FROM information_schema.columns
		WHERE table_name=? AND column_name=?`, table, column).Row().Scan(&isNullable, &def); err != nil {
		t.Fatalf("列元数据查询失败 %s.%s: %v", table, column, err)
	}
	return isNullable == "YES", def != nil
}

// TestCommandLogOkTriStateMigration_UpAndBackfill 真 PG 往返：旧形状 → Up → 三态可用 + 存量被纠正。
func TestCommandLogOkTriStateMigration_UpAndBackfill(t *testing.T) {
	db := testutil.NewTestDB(t, &browsermodel.BrowserCommandLog{})
	ctx := context.Background()
	m := NewBrowserCommandLogOkTriStateMigration(db)

	// 预置迁移前 schema（v3.38.0 建表时的形状）
	if err := db.Exec(`ALTER TABLE browser_command_log ALTER COLUMN ok SET DEFAULT false`).Error; err != nil {
		t.Fatalf("预置迁移前 schema 失败（default）: %v", err)
	}
	if err := db.Exec(`ALTER TABLE browser_command_log ALTER COLUMN ok SET NOT NULL`).Error; err != nil {
		t.Fatalf("预置迁移前 schema 失败（not null）: %v", err)
	}
	if nullable, _ := columnNullable(t, db, "browser_command_log", "ok"); nullable {
		t.Fatal("预置条件不成立：ok 应已被改回 NOT NULL")
	}

	// 旧形状下写 nil 必须失败——这条是「放开 NOT NULL 到底救了什么」的反证，
	// 不是给 Up 记功：先证明旧库会炸，Up 之后同一条写入才算被它修好。
	if err := db.Exec(`INSERT INTO browser_command_log (session_id, task_id, seq, direction, action, ok)
		VALUES (4400, 4400, 1, 'command', 'click', NULL)`).Error; err == nil {
		t.Fatal("旧形状（NOT NULL）下写入 NULL 竟然成功：预置没生效，本批判据全部失焦")
	}

	// 存量：历史 command 帧的 ok 全是写入方传的字面量 true，另有 event 帧的真观测 true/false
	legacy := []*browsermodel.BrowserCommandLog{
		{SessionID: 4401, TaskID: 4401, Seq: 1, Direction: "command", Action: "click", Ok: ptrBool(true)},
		{SessionID: 4401, TaskID: 4401, Seq: 2, Direction: "event", Action: "click", Ok: ptrBool(true)},
		{SessionID: 4401, TaskID: 4401, Seq: 3, Direction: "event", Action: "click", Ok: ptrBool(false)},
		{SessionID: 4401, TaskID: 4401, Seq: 4, Direction: "judge", Action: "write_confirm", Ok: ptrBool(true)},
	}
	if err := db.Create(&legacy).Error; err != nil {
		t.Fatalf("存量种子失败: %v", err)
	}

	if err := m.Up(ctx); err != nil {
		t.Fatalf("Up() failed: %v", err)
	}
	if err := m.Up(ctx); err != nil {
		t.Fatalf("二次 Up() 应幂等: %v", err)
	}

	if nullable, hasDef := columnNullable(t, db, "browser_command_log", "ok"); !nullable || hasDef {
		t.Errorf("Up 后 ok 应为可空且无默认值，got nullable=%v hasDefault=%v（带默认值时 gorm 会把 nil 折成 false）",
			nullable, hasDef)
	}

	// 新代码的下发帧（nil 结论）现在必须写得进、读得出
	fresh := &browsermodel.BrowserCommandLog{
		SessionID: 4401, TaskID: 4401, Seq: 5, Direction: "command", Action: "comment_send",
	}
	if err := db.Create(fresh).Error; err != nil {
		t.Fatalf("Up 后写入无结论的 command 帧失败: %v", err)
	}
	var got browsermodel.BrowserCommandLog
	if err := db.First(&got, fresh.ID).Error; err != nil {
		t.Fatalf("复读失败: %v", err)
	}
	if got.Ok != nil {
		t.Errorf("无结论被折成了 %v（三态退化成两态）", *got.Ok)
	}

	// 回填只碰 command 帧：event/judge 那三行的真观测一个都不许动
	var nulled int
	if err := db.Raw(`SELECT count(*) FROM browser_command_log
		WHERE session_id=4401 AND direction='command' AND ok IS NULL`).Scan(&nulled).Error; err != nil {
		t.Fatalf("统计失败: %v", err)
	}
	if nulled != 2 {
		t.Errorf("command 帧无结论行数=%d want 2（1 条历史被回填 + 1 条新写入）", nulled)
	}
	var kept []struct {
		Seq int
		Ok  *bool
	}
	if err := db.Raw(`SELECT seq, ok FROM browser_command_log
		WHERE session_id=4401 AND direction<>'command' ORDER BY seq`).Scan(&kept).Error; err != nil {
		t.Fatalf("统计失败: %v", err)
	}
	if len(kept) != 3 {
		t.Fatalf("非 command 帧回读 %d 行 want 3", len(kept))
	}
	want := map[int]bool{2: true, 3: false, 4: true}
	for _, k := range kept {
		if k.Ok == nil || *k.Ok != want[k.Seq] {
			t.Errorf("seq=%d 的真观测被改动/被抹掉，got %v", k.Seq, k.Ok)
		}
	}

	if err := m.Down(ctx); err != nil {
		t.Fatalf("Down() failed: %v", err)
	}
	// 降级不销毁在用列，也不许把 NULL 折回某个布尔值：列形状必须与 Up 后一致
	if nullable, hasDef := columnNullable(t, db, "browser_command_log", "ok"); !nullable || hasDef {
		t.Errorf("Down 后 ok 必须保持可空无默认（降级不伪造审计结论），got nullable=%v hasDefault=%v", nullable, hasDef)
	}
	var after int
	if err := db.Raw(`SELECT count(*) FROM browser_command_log WHERE session_id=4401`).Scan(&after).Error; err != nil {
		t.Fatalf("统计失败: %v", err)
	}
	if after != 5 {
		t.Errorf("Down 后存量行数=%d want 5（降级不得销毁审计行）", after)
	}

	db.Unscoped().Delete(&browsermodel.BrowserCommandLog{}, "session_id IN (?)", []uint{4400, 4401})
}

func ptrBool(v bool) *bool { return &v }

// 迁移必须真的挂进启动注册链——只写文件不注册等于没写（老库升上来拿不到可空列，
// 新代码第一次写 nil 就报错）。走 registry 而非读源码，判据同 v3.43.0。
func TestCommandLogOkTriStateMigration_IsRegistered(t *testing.T) {
	reg := migration.NewMigrationRegistry()
	RegisterMigrations(reg, nil)
	m, ok := reg.Get("v3.44.0")
	if !ok {
		t.Fatal("v3.44.0 未注册进迁移链")
	}
	if m.Name() != "browser_command_log_ok_tristate" {
		t.Errorf("注册到的迁移名不符：%s", m.Name())
	}
}
