// obs_config_default_index_batchm_test.go 钉"全站最多一条默认存储"这道库级守卫（批M / N-26）。
//
// 放在 internal/pkg/db：建索引这件事的事实源在这里（post-migrate 钩子），
// 而 repository 包的用例跑不到钩子（testutil 只做 AutoMigrate，不跑本文件这段裸 DDL）。
//
// 判据分三段，缺一段都可能"索引建了但防不住"或"防住了但服务起不来"：
//  1. 钩子可重跑（第二次不报错、约束仍在）；
//  2. 建成之后，第二行 is_default=true 必须被库拒绝；
//  3. is_default=false 的行**不受约束**（否则第二台非默认存储连创建都创建不了，
//     那就是"把合法形状拦在门外的约束"，比没有约束更坏）。
//
// 外加清重那段单独一条：存量双默认时保留的是 GetDefault 会选中的最早一条。
package db

import (
	"bytes"
	"fmt"
	"os"
	"testing"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/testutil"

	"gorm.io/gorm"
)

func batchMObsConfigTable(t *testing.T, gdb *gorm.DB) {
	t.Helper()
	if err := gdb.Migrator().DropTable(&model.ObsConfig{}); err != nil {
		t.Fatalf("清表失败: %v", err)
	}
	if err := gdb.AutoMigrate(&model.ObsConfig{}); err != nil {
		t.Fatalf("建表失败: %v", err)
	}
}

func batchMInsertObs(t *testing.T, gdb *gorm.DB, name string, isDefault bool, createdAt time.Time) {
	t.Helper()
	row := model.ObsConfig{
		Name: name, Provider: model.ObsProviderLocal,
		AccessKey: "ak", SecretKey: "sk", Bucket: "b",
		Status: model.ObsStatusActive, MaxSize: 1 << 20, MaxCount: 10,
		IsDefault: isDefault, CreatedAt: createdAt,
	}
	if err := gdb.Create(&row).Error; err != nil {
		t.Fatalf("插入 %s 失败: %v", name, err)
	}
}

// batchMDefaultNames 按列直读，不走 GetDefault。
func batchMDefaults(t *testing.T, gdb *gorm.DB) []string {
	t.Helper()
	var names []string
	if err := gdb.Model(&model.ObsConfig{}).Where("is_default = ?", true).
		Order("name").Pluck("name", &names).Error; err != nil {
		t.Fatalf("读取默认行失败: %v", err)
	}
	return names
}

func batchMObsIndexExists(t *testing.T, gdb *gorm.DB) bool {
	t.Helper()
	var n int64
	if err := gdb.Raw(`SELECT count(*) FROM pg_indexes WHERE tablename = 'obs_config'
		AND indexname = 'idx_obs_config_single_default'`).Scan(&n).Error; err != nil {
		t.Fatalf("查索引存在性失败: %v", err)
	}
	return n > 0
}

func TestBatchM_ObsDefaultIndex_CreatedAndIdempotent(t *testing.T) {
	gdb := testutil.NewTestDB(t)
	if gdb == nil {
		t.Fatal("测试库不可达")
	}
	batchMObsConfigTable(t, gdb)

	postMigrateObsDefaultUniqueIndex(gdb)
	if !batchMObsIndexExists(t, gdb) {
		t.Fatal("钩子跑完后 idx_obs_config_single_default 不存在")
	}
	// 可重跑：启动路径每次都过这段，第二次报错会让启动日志长期带噪音。
	postMigrateObsDefaultUniqueIndex(gdb)
	if !batchMObsIndexExists(t, gdb) {
		t.Fatal("重跑之后索引不见了（幂等性不成立）")
	}
}

func TestBatchM_ObsDefaultIndex_RejectsSecondDefault(t *testing.T) {
	gdb := testutil.NewTestDB(t)
	if gdb == nil {
		t.Fatal("测试库不可达")
	}
	batchMObsConfigTable(t, gdb)
	postMigrateObsDefaultUniqueIndex(gdb)

	now := time.Now()
	batchMInsertObs(t, gdb, "batchm-idx-first", true, now)

	// 插入侧：第二行默认必须插不进去。
	err := gdb.Create(&model.ObsConfig{
		Name: "batchm-idx-second", Provider: model.ObsProviderLocal,
		AccessKey: "ak", SecretKey: "sk", Bucket: "b",
		Status: model.ObsStatusActive, MaxSize: 1 << 20,
		IsDefault: true, CreatedAt: now,
	}).Error
	if err == nil {
		t.Error("第二行 is_default=true 插入成功 —— 唯一索引没起作用")
	}

	// 更新侧：把一台非默认行提升为默认也要被拒（这才是本批真正防的那条旁路：
	// 别处拿陈旧快照整行 Save，把 is_default 写回 true）。
	batchMInsertObs(t, gdb, "batchm-idx-third", false, now)
	if err := gdb.Model(&model.ObsConfig{}).Where("name = ?", "batchm-idx-third").
		Update("is_default", true).Error; err == nil {
		t.Error("把非默认行改成默认成功 —— 唯一索引对 UPDATE 没起作用")
	}

	if names := batchMDefaults(t, gdb); len(names) != 1 || names[0] != "batchm-idx-first" {
		t.Errorf("默认行 = %v，期望 [batchm-idx-first]", names)
	}
}

// 约束不许把合法形状拦在门外：obs_config 的日常形态就是"一条默认 + N 条非默认"。
func TestBatchM_ObsDefaultIndex_AllowsManyNonDefaults(t *testing.T) {
	gdb := testutil.NewTestDB(t)
	if gdb == nil {
		t.Fatal("测试库不可达")
	}
	batchMObsConfigTable(t, gdb)
	postMigrateObsDefaultUniqueIndex(gdb)

	now := time.Now()
	batchMInsertObs(t, gdb, "batchm-free-0", true, now)
	for i := 1; i <= 3; i++ {
		batchMInsertObs(t, gdb, "batchm-free-"+string(rune('a'+i)), false, now)
	}
	// 清掉默认之后再来一条默认也要成功（"唯一"不等于"必须存在"）。
	if err := gdb.Model(&model.ObsConfig{}).Where("is_default = ?", true).
		Update("is_default", false).Error; err != nil {
		t.Fatalf("清默认失败: %v", err)
	}
	batchMInsertObs(t, gdb, "batchm-free-again", true, now)

	if names := batchMDefaults(t, gdb); len(names) != 1 || names[0] != "batchm-free-again" {
		t.Errorf("默认行 = %v，期望 [batchm-free-again]", names)
	}
}

// 存量双默认：钩子先清重、再建索引，留下的必须是 GetDefault 会选中的那台
// （最早 created_at，同值按 id 升序）—— 存量文件实际就落在那台上。
//
// 存量条数跑 2 和 3 两档：清重的门槛写的是 `dups > 1`，只测 3 条的话，
// 把它改成 `dups > 2` 这种"差一个数"的写法就没人拦 —— 而生产里最常见的脏状态恰恰是 2 条。
func TestBatchM_ObsDefaultIndex_RepairKeepsOldestDefault(t *testing.T) {
	for _, dups := range []int{2, 3} {
		t.Run(fmt.Sprintf("存量%d条默认", dups), func(t *testing.T) {
			gdb := testutil.NewTestDB(t)
			if gdb == nil {
				t.Fatal("测试库不可达")
			}
			batchMObsConfigTable(t, gdb)

			base := time.Now().Add(-time.Hour)
			batchMInsertObs(t, gdb, "batchm-rep-newest", true, base.Add(30*time.Minute))
			batchMInsertObs(t, gdb, "batchm-rep-oldest", true, base)
			if dups == 3 {
				batchMInsertObs(t, gdb, "batchm-rep-middle", true, base.Add(10*time.Minute))
			}
			if n := len(batchMDefaults(t, gdb)); n != dups {
				t.Fatalf("前置不成立：默认行有 %d 条，期望 %d", n, dups)
			}

			postMigrateObsDefaultUniqueIndex(gdb)

			names := batchMDefaults(t, gdb)
			if len(names) != 1 || names[0] != "batchm-rep-oldest" {
				t.Errorf("清重后默认行 = %v，期望 [batchm-rep-oldest]（GetDefault 按 created_at 升序选它）", names)
			}
			if !batchMObsIndexExists(t, gdb) {
				t.Error("清重之后索引没建成：双默认仍可随时复现")
			}
			// 存量行一条都不许丢：降级是改列，不是删行。
			var total int64
			if err := gdb.Model(&model.ObsConfig{}).Count(&total).Error; err != nil {
				t.Fatalf("统计总行数失败: %v", err)
			}
			if int(total) != dups {
				t.Errorf("清重后总行数 = %d，期望 %d（只降级、不删行）", total, dups)
			}
		})
	}
}

// 钩子必须真的挂在启动迁移路径上：上面几条腿都是直接调函数，测不到"有没有人调它"。
// 这一条是静态锁（行为不变，只钉装配）：migrate.go 里那句调用恰好出现一次，
// 且它排在 AutoMigrate 终校验之后 —— 早于建表就是给不存在的表建索引。
func TestBatchM_ObsDefaultIndex_HookIsWiredIntoMigrate(t *testing.T) {
	src, err := os.ReadFile("migrate.go")
	if err != nil {
		t.Fatalf("读 migrate.go 失败: %v", err)
	}
	const call = "\tpostMigrateObsDefaultUniqueIndex(DB)\n"
	if n := bytes.Count(src, []byte(call)); n != 1 {
		t.Fatalf("migrate.go 里 postMigrateObsDefaultUniqueIndex(DB) 的调用出现 %d 次，期望恰好 1 次"+
			"（0 次=索引只在测试里存在，生产库永远裸奔；>1 次=同一 DDL 被跑多遍）", n)
	}
	const after = "if missing := missingTables(DB, all...)"
	iCall, iAfter := bytes.Index(src, []byte(call)), bytes.Index(src, []byte(after))
	if iAfter < 0 {
		t.Fatal("没找到 AutoMigrate 终校验那一段锚点，本条静态锁的口径需要跟着改")
	}
	if iCall < iAfter {
		t.Errorf("钩子调用（偏移 %d）排在终校验（偏移 %d）之前：表还没建齐就建索引", iCall, iAfter)
	}
}
