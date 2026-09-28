package migrations

import (
	"context"
	"testing"

	"hivemtk-user/internal/migration"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/testutil"

	"gorm.io/gorm"
)

// v3.45.0 给 message_hub 加两列。判据手法沿用 v3.42/v3.43/v3.44：AutoMigrate 会按模型标签
// 把这两列直接建出来，所以必须先把它们**手动摘掉**还原迁移前 schema——否则 Up 什么都没做
// 也能全绿，而真正要证的是「老库升上来拿得到这两列」：拿不到的话新代码三条取行路径
// 第一条 SQL 就报错，整条出站链路当场断。

func TestOutboundPushBudgetMigration_Meta(t *testing.T) {
	m := NewMessageHubOutboundPushBudgetMigration(nil)
	if m.Version() != "v3.45.0" {
		t.Errorf("Version()=%q want=v3.45.0", m.Version())
	}
	if m.Name() == "" || m.Description() == "" {
		t.Error("Name/Description should not be empty")
	}
}

func TestOutboundPushBudgetMigration_NilDB(t *testing.T) {
	m := NewMessageHubOutboundPushBudgetMigration(nil)
	if err := m.Up(context.Background()); err == nil {
		t.Error("nil db Up() 应返回错误")
	}
	if err := m.Down(context.Background()); err == nil {
		t.Error("nil db Down() 应返回错误")
	}
}

func columnShape(t *testing.T, db *gorm.DB, table, column string) (dataType string, notNull bool, def *string) {
	t.Helper()
	var nn string
	var d *string
	if err := db.Raw(`SELECT data_type, is_nullable, column_default FROM information_schema.columns
		WHERE table_name=? AND column_name=?`, table, column).Row().Scan(&dataType, &nn, &d); err != nil {
		t.Fatalf("列元数据查询失败 %s.%s: %v", table, column, err)
	}
	return dataType, nn == "NO", d
}

// TestOutboundPushBudgetMigration_UpAndShape 真 PG 往返：摘列 → Up → 列形状与代码缺省一致。
func TestOutboundPushBudgetMigration_UpAndShape(t *testing.T) {
	db := testutil.NewTestDB(t, &model.MessageHub{})
	ctx := context.Background()
	m := NewMessageHubOutboundPushBudgetMigration(db)

	// 先播种再摘列：顺序反了的话 gorm Create 会带着这两列去 INSERT，报的是「列不存在」，
	// 红因落在预置而不是 Up 上，读起来像是迁移坏了。
	legacy := &model.MessageHub{
		Platform: "b20d_mig", MsgID: "legacy-1", AccountID: "acc-1", Direction: "outbound",
		Status: "pending", MsgType: "text", Content: "升级前就存在的待推行",
	}
	if err := db.Create(legacy).Error; err != nil {
		t.Fatalf("存量种子失败: %v", err)
	}

	// 预置迁移前 schema
	if err := db.Exec(`ALTER TABLE message_hub DROP COLUMN IF EXISTS push_attempts`).Error; err != nil {
		t.Fatalf("预置迁移前 schema 失败（push_attempts）: %v", err)
	}
	if err := db.Exec(`ALTER TABLE message_hub DROP COLUMN IF EXISTS push_error`).Error; err != nil {
		t.Fatalf("预置迁移前 schema 失败（push_error）: %v", err)
	}

	if err := m.Up(ctx); err != nil {
		t.Fatalf("Up() failed: %v", err)
	}
	if err := m.Up(ctx); err != nil {
		t.Fatalf("二次 Up() 应幂等: %v", err)
	}

	// 列形状：INTEGER NOT NULL DEFAULT 0 / VARCHAR(200) NOT NULL DEFAULT ''
	if dt, nn, d := columnShape(t, db, "message_hub", "push_attempts"); dt != "integer" || !nn || d == nil {
		t.Errorf("push_attempts 应为 integer NOT NULL 带默认值，got type=%s notNull=%v default=%v", dt, nn, d)
	} else if *d != "0" {
		t.Errorf("push_attempts 默认值=%q want 0（列缺省必须等于代码缺省，否则老行读出来是「已到界」）", *d)
	}
	if dt, nn, d := columnShape(t, db, "message_hub", "push_error"); dt != "character varying" || !nn || d == nil {
		t.Errorf("push_error 应为 varchar NOT NULL 带默认值，got type=%s notNull=%v default=%v", dt, nn, d)
	}

	// 存量行没被回填成任何非零值：升级不得凭空吃掉真实待推行的预算
	var got model.MessageHub
	if err := db.First(&got, legacy.ID).Error; err != nil {
		t.Fatalf("复读存量行失败: %v", err)
	}
	if got.PushAttempts != 0 || got.PushError != "" {
		t.Errorf("存量行被回填成 attempts=%d error=%q（应为 0/空：错填会直接吞消息）", got.PushAttempts, got.PushError)
	}

	// 新代码的下发预算读写往返
	if err := db.Model(&got).Updates(map[string]any{"push_attempts": 3, "push_error": "x"}).Error; err != nil {
		t.Fatalf("写入预算失败: %v", err)
	}
	var again model.MessageHub
	if err := db.First(&again, legacy.ID).Error; err != nil {
		t.Fatalf("复读失败: %v", err)
	}
	if again.PushAttempts != 3 || again.PushError != "x" {
		t.Errorf("预算未持久化，got %d/%q", again.PushAttempts, again.PushError)
	}

	if err := m.Down(ctx); err != nil {
		t.Fatalf("Down() failed: %v", err)
	}
	// 降级不销毁在用列
	if _, _, d := columnShape(t, db, "message_hub", "push_attempts"); d == nil {
		t.Error("Down 后 push_attempts 必须仍在（降级不销毁在用列）")
	}
	var n int
	if err := db.Raw(`SELECT count(*) FROM message_hub WHERE platform='b20d_mig'`).Scan(&n).Error; err != nil {
		t.Fatalf("统计失败: %v", err)
	}
	if n != 1 {
		t.Errorf("Down 后存量行数=%d want 1（降级不得销毁数据）", n)
	}

	db.Unscoped().Delete(&model.MessageHub{}, legacy.ID)
}

// 表不存在时 Up 必须静默通过（全新库由模型标签负责），不能报「表不存在」卡住启动链。
//
// 前置必须**自己把表摘掉**：测试库是「每进程一个影子库」，同文件上一条用例已经建过
// message_hub，若只 NewTestDB(t) 不建表就当「表不存在」，判据其实走在 HasTable==true 的
// 分支上——去掉那句前置保护也照样绿（第一版 M5 就是这么活的）。
func TestOutboundPushBudgetMigration_NoTable(t *testing.T) {
	db := testutil.NewTestDB(t, &model.MessageHub{})
	if err := db.Migrator().DropTable(&model.MessageHub{}); err != nil {
		t.Fatalf("预置「表不存在」失败: %v", err)
	}
	t.Cleanup(func() {
		if err := db.AutoMigrate(&model.MessageHub{}); err != nil {
			t.Logf("还原 message_hub 失败（同进程后续用例可能受影响）: %v", err)
		}
	})
	if db.Migrator().HasTable("message_hub") {
		t.Fatal("预置条件不成立：message_hub 仍在，本批判据失焦")
	}
	if err := NewMessageHubOutboundPushBudgetMigration(db).Up(context.Background()); err != nil {
		t.Errorf("表不存在时 Up() 应无操作通过，got: %v", err)
	}
}

// 迁移必须真的挂进启动注册链——只写文件不注册等于没写（老库升上来拿不到这两列）。
func TestOutboundPushBudgetMigration_IsRegistered(t *testing.T) {
	reg := migration.NewMigrationRegistry()
	RegisterMigrations(reg, nil)
	m, ok := reg.Get("v3.45.0")
	if !ok {
		t.Fatal("v3.45.0 未注册进迁移链")
	}
	if m.Name() != "message_hub_outbound_push_budget" {
		t.Errorf("注册到的迁移名不符：%s", m.Name())
	}
}
