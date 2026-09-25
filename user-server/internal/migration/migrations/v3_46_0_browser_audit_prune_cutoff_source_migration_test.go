package migrations

import (
	"context"
	"strings"
	"testing"
	"time"

	browsermodel "hivemtk-user/internal/browser_automation/model"
	"hivemtk-user/internal/migration"
	"hivemtk-user/internal/pkg/testutil"
)

// v3.46.0 给 browser_audit_prune_runs 补一列 cutoff_source。手法沿用 v3.42–v3.45：
// AutoMigrate 会按模型标签直接建出这列，所以必须先手动 DROP 还原迁移前 schema，
// 否则 Up 什么都没做也能全绿——而这里要证的正是「老库（批22 那版建的表）升上来拿得到这列」：
// 拿不到时新代码第一次写留痕行就报错，而留痕行是 fail-close 的那一侧，等于裁剪功能全废。

func TestAuditPruneCutoffSourceMigration_Meta(t *testing.T) {
	m := NewBrowserAuditPruneCutoffSourceMigration(nil)
	if m.Version() != "v3.46.0" {
		t.Errorf("Version()=%q want=v3.46.0", m.Version())
	}
	if m.Name() == "" || m.Description() == "" {
		t.Error("Name/Description should not be empty")
	}
}

func TestAuditPruneCutoffSourceMigration_NilDB(t *testing.T) {
	m := NewBrowserAuditPruneCutoffSourceMigration(nil)
	if err := m.Up(context.Background()); err == nil {
		t.Error("nil db Up() 应返回错误")
	}
	if err := m.Down(context.Background()); err == nil {
		t.Error("nil db Down() 应返回错误")
	}
}

// TestAuditPruneCutoffSourceMigration_UpAndShape 真 PG 往返：摘列 → Up → 列形状与代码缺省一致。
func TestAuditPruneCutoffSourceMigration_UpAndShape(t *testing.T) {
	db := testutil.NewTestDB(t, &browsermodel.BrowserAuditPruneRun{})
	if db == nil {
		t.Fatal("测试库不可达：迁移往返无法判定（不 Skip，跳过等于没锁）")
	}
	ctx := context.Background()
	m := NewBrowserAuditPruneCutoffSourceMigration(db)

	// 预置迁移前 schema：先摘列，再以**裸 SQL 插一行没有该列的老数据**。
	// 顺序与手法都有讲究：用模型 Create 会带着 cutoff_source 去 INSERT（列还没加回来就是
	// 「列不存在」），而先 Create 再 DROP 的话这一行是「新代码写的空串」，证不了
	// 「老行升上来读到的是 DEFAULT 给的空串」——那正是本迁移唯一需要的语义。
	if err := db.Exec(`ALTER TABLE browser_audit_prune_runs
		DROP COLUMN IF EXISTS cutoff_source`).Error; err != nil {
		t.Fatalf("预置迁移前 schema 失败: %v", err)
	}
	legacyCutoff := time.Now().AddDate(0, 0, -90).UTC().Truncate(time.Microsecond)
	if err := db.Exec(`INSERT INTO browser_audit_prune_runs
		(cutoff, batches, digests, rows_pruned, rows_before, created_at)
		VALUES (?, 1, 0, 0, 0, ?)`, legacyCutoff, legacyCutoff).Error; err != nil {
		t.Fatalf("存量种子失败: %v", err)
	}
	var legacyID uint
	if err := db.Raw(`SELECT max(id) FROM browser_audit_prune_runs`).Row().Scan(&legacyID); err != nil {
		t.Fatalf("取存量行 id 失败: %v", err)
	}

	if err := m.Up(ctx); err != nil {
		t.Fatalf("Up() failed: %v", err)
	}
	if err := m.Up(ctx); err != nil {
		t.Fatalf("二次 Up() 应幂等: %v", err)
	}

	dataType, notNull, def := columnShape(t, db, "browser_audit_prune_runs", "cutoff_source")
	if !notNull {
		t.Error("cutoff_source 应 NOT NULL：可空的来源列等于允许「不知道从哪来」重新变成一个合法状态")
	}
	// PG 把默认值渲染成 `''::character varying`（带类型标注），按前缀判、把原文印出来：
	// 只判 `*def == "''"` 会红在一次成功的迁移上（本用例第一版就是这么红的）。
	if def == nil || !strings.HasPrefix(*def, "''") {
		got := "<nil>"
		if def != nil {
			got = *def
		}
		t.Errorf(`cutoff_source 默认值应是空串（列缺省＝代码缺省），got=%s`, got)
	}
	if dataType != "character varying" {
		t.Errorf("cutoff_source 类型 got=%s want=character varying", dataType)
	}

	// 存量行的来源必须读成空串而不是被伪造成某个配置值。
	var got string
	if err := db.Raw(`SELECT cutoff_source FROM browser_audit_prune_runs WHERE id=?`, legacyID).
		Row().Scan(&got); err != nil {
		t.Fatalf("读存量行失败: %v", err)
	}
	if got != "" {
		t.Errorf("存量行 cutoff_source got=%q want=\"\"（空串=本列之前的时代，回填任何字符串都是伪造历史）", got)
	}

	if err := m.Down(ctx); err != nil {
		t.Fatalf("Down() failed: %v", err)
	}
	var count int64
	if err := db.Raw(`SELECT count(*) FROM information_schema.columns
		WHERE table_name='browser_audit_prune_runs' AND column_name='cutoff_source'`).Scan(&count).Error; err != nil {
		t.Fatalf("Down 后查列失败: %v", err)
	}
	if count != 1 {
		t.Error("Down 不许销毁在用列（口径同 v3.39–v3.45）")
	}

	var _ migration.Migration = m
}
