package migrations

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"testing"
	"time"

	"hivemtk-user/internal/migration"
	pdb "hivemtk-user/internal/pkg/db"
	"hivemtk-user/internal/pkg/testutil"

	"gorm.io/gorm"
)

// knownFailingVersions 逐条登记「Up 目前必然失败」的版本与已核实的根因（本排期只显式化、不修生产码）。
// 修好后这里必须删条：用例会在登记项不再失败时报错，防止豁免表长期滞留。
var knownFailingVersions = map[string]string{
	// l_p1_migration.go:60 在 integration_templates 上建 is_built_in 索引，
	// 但模型列名实为 built_in（model/integration_template.go:27 BuiltIn）。
	// 生产建表走 AutoMigrate，表已按模型名建好，CREATE TABLE IF NOT EXISTS 不补列 → 索引必红。
	"v3.3.0": "列名口径不一致：迁移建 is_built_in 索引，AutoMigrate 建的列叫 built_in",
	// v3_22_0:53-63 把 information_schema.columns.character_maximum_length 扫进非空 int，
	// 而 text/uuid 型 customer_id 列该字段为 NULL → 整个迁移在 ALTER 之前即中止。
	"v3.22.0": "Scan NULL→int：character_maximum_length 需用 sql.NullInt64 承接",
	// v3_36_0 stmts 顺序错：第 73 行 CREATE TRIGGER 引用 fn_guard_initial_admin_delete()，
	// 该函数却在第 77 行才 CREATE。首错即 return ⇒ 删除保护触发器与函数在任何库上都从未建立成功。
	"v3.36.0": "语句顺序缺陷：CREATE TRIGGER 早于其依赖的 CREATE FUNCTION",
}

// TestFullMigrationChainUpThenRollback 在测试库上按生产建表路径铺好基表，再按序执行全部迁移的 Up()，
// 最后逆序执行 Down()。这是唯一能在一次运行里驱动 70+ 个 Up 体的用例。
//
// 为什么要先跑一遍生产建表：本仓的建表事实源是 GORM AutoMigrate（internal/pkg/db/migrate.go），
// 版本化迁移只做其上的增量；缺了这一步，17 个依赖 knowledge_chunks / message_hub / system_users
// 等基表的迁移会以 "relation does not exist" 失败（既有设计，非本用例可放宽的假失败）。
//
// 文件名以 a_ 前缀开头：Go 按文件名字典序执行同包用例，本用例因此跑在最前，
// 此时 testutil 刚 DROP+CREATE 完进程槽位库，public schema 确为空。
// 用例结束后把 schema 重置回空并重建 pgvector，后续用例不受影响。
// 单跑（约 60s）：
//
//	go test -p 1 -count=1 -timeout 600s -run TestFullMigrationChainUpThenRollback ./internal/migration/migrations/ -v
func TestFullMigrationChainUpThenRollback(t *testing.T) {
	gdb := testutil.NewTestDB(t)
	if gdb == nil {
		t.Fatal("测试库不可达")
	}
	t.Cleanup(func() { resetChainSchema(t, gdb) })

	prev := pdb.GetDB()
	pdb.SetTestDB(gdb)
	t.Cleanup(func() { pdb.SetTestDB(prev) })

	if n := chainTableCount(gdb); n != 0 {
		t.Fatalf("全链路用例要求空库，实际 public schema 已有 %d 张表；"+
			"说明本文件不再是同包首个执行的测试文件（见函数头注释）", n)
	}

	// v3.28.0 要求字段加密主键可用；本库无任何 email_smtp 存量行，用测试专用假值通过其 fail-closed 校验
	t.Setenv("FIELD_ENCRYPTION_KEY", strings.Repeat("0", 32))

	seedStart := time.Now()
	if err := runProductionAutoMigrate(gdb); err != nil {
		t.Fatalf("生产建表路径（AutoMigrate）失败，迁移链前置条件不成立: %v", err)
	}
	baseTables := chainTableCount(gdb)
	t.Logf("基表铺设完成 用时=%v 表数=%d", time.Since(seedStart).Round(time.Millisecond), baseTables)

	reg := migration.NewMigrationRegistry()
	RegisterMigrations(reg, gdb)
	all := reg.GetAll()
	ctx := context.Background()

	upFailed := map[string]string{}
	for _, m := range all {
		if err := m.Up(ctx); err != nil {
			upFailed[m.Version()] = err.Error()
		}
	}
	unexpected := map[string]string{}
	for v, e := range upFailed {
		if _, known := knownFailingVersions[v]; !known {
			unexpected[v+"/"+nameOf(all, v)] = e
		}
	}
	if len(unexpected) > 0 {
		t.Errorf("全链路 Up 出现未登记失败 %d/%d：%s", len(unexpected), len(all), formatFailures(unexpected))
	}
	for v, reason := range knownFailingVersions {
		if _, failed := upFailed[v]; !failed {
			t.Errorf("已登记为必红的版本 %s 竟通过（登记原因：%s），请从 knownFailingVersions 移除", v, reason)
		}
	}
	if len(upFailed) > 0 {
		t.Logf("Up 失败合计 %d/%d，其中已登记 %d 条：%s",
			len(upFailed), len(all), len(knownFailingVersions), formatFailures(upFailed))
	}
	afterUp := chainTableCount(gdb)

	downFailed := map[string]string{}
	for i := len(all) - 1; i >= 0; i-- {
		m := all[i]
		if err := m.Down(ctx); err != nil {
			downFailed[fmt.Sprintf("%s/%s", m.Version(), m.Name())] = err.Error()
		}
	}
	// 回滚链路不完备属既有事实（见计划 Findings），本排期只把它显式化，不据此判红。
	if len(downFailed) > 0 {
		t.Logf("Down 失败 %d/%d：%s", len(downFailed), len(all), formatFailures(downFailed))
	}
	t.Logf("迁移总数=%d 基表=%d Up 后表数=%d Down 后表数=%d Down 失败数=%d",
		len(all), baseTables, afterUp, chainTableCount(gdb), len(downFailed))
}

// nameOf 取版本对应的迁移名，仅用于失败信息可读化。
func nameOf(all []migration.Migration, version string) string {
	for _, m := range all {
		if m.Version() == version {
			return m.Name()
		}
	}
	return "?"
}

// runProductionAutoMigrate 调生产建表入口（其内部 panic 转为 error，避免打挂整个测试二进制）。
func runProductionAutoMigrate(gdb *gorm.DB) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("panic: %v", r)
		}
	}()
	pdb.AutoMigrate()
	return nil
}

func chainTableCount(gdb *gorm.DB) int {
	var n int
	if err := gdb.Raw(
		`SELECT COUNT(*) FROM information_schema.tables WHERE table_schema = 'public'`,
	).Scan(&n).Error; err != nil {
		return -1
	}
	return n
}

func resetChainSchema(t *testing.T, gdb *gorm.DB) {
	t.Helper()
	if err := gdb.Exec(`DROP SCHEMA IF EXISTS public CASCADE`).Error; err != nil {
		t.Logf("清理全链路用例残留失败: %v", err)
		return
	}
	if err := gdb.Exec(`CREATE SCHEMA public`).Error; err != nil {
		t.Logf("重建 public schema 失败: %v", err)
		return
	}
	if err := gdb.Exec(`CREATE EXTENSION IF NOT EXISTS vector`).Error; err != nil {
		t.Logf("重建 pgvector 扩展失败: %v", err)
	}
}

func formatFailures(m map[string]string) string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := ""
	for _, k := range keys {
		out += "\n  " + k + ": " + m[k]
	}
	return out
}
