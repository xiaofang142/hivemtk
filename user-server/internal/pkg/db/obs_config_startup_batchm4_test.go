// obs_config_startup_batchm4_test.go 把"下一次启动真的会建出那道索引"从**读装配点**
// 换成**跑通一次启动**（批M-4 / §23.8 第 3、6 条）。
//
// 批M 那五条 I 腿全都是**直接调钩子**：`postMigrateObsDefaultUniqueIndex(gdb)` 拿到句柄就跑。
// 于是它们共同测不到两件只有真启动路径才回答的事：
//
//  1. **有没有人调它**。I5 用一句静态锁钉住"migrate.go 里那句调用出现 1 次、位置在终校验之后"，
//     但静态锁只读文本：`AutoMigrate()` 中途 panic、表压根没建出来，静态锁照样绿。
//     批M-4 的 S11/S13 跑的是 `AutoMigrate()` 本体 —— 全新库里表从 0 建出来，索引跟着在。
//  2. **重跑时它是否只留一行日志**。钩子里 `CREATE UNIQUE INDEX IF NOT EXISTS` 报错只会被
//     吞进 `logger.Warn` 后 return（M21 因此在批M 记成"等价、无腿"）。
//     S12 把 stdout 拉进断言面，这条差异第一次变成可红判据。
//
// S14 是另一头：`model/obs_config.go` 上那些 `default:'active'`、`default:false` 是**库侧**默认值。
// §23.8 第 6 条当年写的是"走 `Create` 前服务层已显式赋值，所以库侧默认值今天没有承重" ——
// 本批把这句话量开了，结论是**它对一半**：走 GORM 时这一列确实被显式写进 INSERT、列默认管不到
// （S14 第三段实测），但任何绕过 GORM 的写入只由列默认决定（第一段实测），
// 而且标签值≠零值时 GORM 会拿标签去覆盖调用方传的零值（格 M51 实测）。
// 三种形状一起钉住：裸 SQL 不给这一列、走 GORM 给 false、以及"谁说了算"这个机制本身。
//
// S15 再往上挪一层：`AutoMigrate()` 本体的**调用方**（`cmd/api/main.go` 的 `db.InitDB()` +
// `db.AutoMigrate()` 两句）今天也零判据。批M 的 I5 只钉到"migrate.go 内部那句钩子调用"，
// 而 `main.go` 里那两句一旦少一行，钩子内外的证据全都不成立 —— 本批把它钉成一条静态锁。
//
// 腿名前缀刻意仍是 `TestBatchM_`（不是 `TestBatchM4_`）：变异电池的 `-run TestBatchM_` 是子串匹配，
// 换前缀等于新腿不进电池。批次由**文件名**区分，与 §23.10 的 obs_config_batchm3_test.go 同一口径。
package db

import (
	"database/sql"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/testutil"
	"hivemtk-user/internal/pkg/utils/logger"

	"gorm.io/gorm"
)

// 钩子里那几句日志的**字面**文本。测试里抄字面是有意的：改文案必须让腿红一次，
// 否则"日志面"这条判据就是空的 —— 把失败分支的文案改掉，S12 就再也看不见它了。
const (
	batchM4LogReady     = "post-migrate: obs_config 单默认偏唯一索引已就绪"
	batchM4LogDDLFailed = "CREATE idx_obs_config_single_default 失败"
	batchM4LogDeduped   = "条默认存储，已保留 GetDefault 会选中的最早一条、降级"
)

// batchM4StartupDB 把包级 DB 交给本进程测试库，并把**真启动迁移**的句柄准备好。
//
// 为什么要动这个全局：`AutoMigrate()` 读的是包级 `DB`（生产里由 `InitDB()` 赋值），
// 这是它唯一的句柄来源。批M 的腿刻意把句柄作参数传给钩子、绕开了全局，
// 代价就是"启动路径本身跑不跑得通"始终没有腿。
func batchM4StartupDB(t *testing.T) *gorm.DB {
	t.Helper()
	gdb := testutil.NewTestDB(t)
	if gdb == nil {
		t.Fatal("测试库不可达")
	}
	saved := DB
	DB = gdb
	t.Cleanup(func() { DB = saved })
	return gdb
}

// batchM4RunStartup 跑一次真启动迁移。
//
// 必须在这儿接住 panic：`AutoMigrate()` 有两个 panic 出口（模型迁移不可容忍错误、
// `missingTables` 终校验）。不接住的话一条腿会把整个测试二进制带走，
// 电池里看到的就是"红的不是指定腿"，而不是这条腿的判据。
func batchM4RunStartup(t *testing.T) (panicked any) {
	t.Helper()
	defer func() { panicked = recover() }()
	AutoMigrate()
	return nil
}

// batchM4CaptureStartup 跑一次真启动并把期间落到 stdout 的日志抓成字符串。
//
// 为什么真抓日志而不是给钩子塞一个假回调：本批要钉的就是"这一句有没有落到运维看得见的那条流里"。
// 走真 logger、真 encoder 才算数 —— 级别被过滤掉、文案里少一个字、Warn 写成 Debug，假回调全看不见。
// 接缝是 `logger.stdout` **函数**（不是 io.Writer 变量）：换 os.Stdout 之后必须再 InitLogger 一次才生效。
func batchM4CaptureStartup(t *testing.T) (log string, panicked any) {
	t.Helper()
	oldOut := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("管道创建失败: %v", err)
	}
	os.Stdout = w
	logger.InitLogger(logger.LoggingConfig{Level: "info", Format: "json", Output: "stdout"})

	// 读端必须并发跑：AutoMigrate 一趟会写若干行，管道缓冲区写满而无人读会直接把这条腿卡死。
	done := make(chan string, 1)
	go func() {
		b, _ := io.ReadAll(r)
		done <- string(b)
	}()

	panicked = func() any {
		defer func() {
			os.Stdout = oldOut
			logger.InitLogger(logger.DefaultConfig())
			_ = w.Close()
		}()
		return batchM4RunStartup(t)
	}()

	log = <-done
	_ = r.Close()
	return log, panicked
}

// batchM4ObsIndex 直读系统目录，拿到索引的**形状**而不只是存在性。
//
// 为什么不能只查 pg_indexes 有没有这一行（批M 的 batchMObsIndexExists 就只查存在性）：
// 本批要钉的是"启动建出来的那道是 partial 唯一索引"。名字对了但 `UNIQUE` 丢了、
// 或 `WHERE` 谓词丢了，都存在性查询看不见 —— 而后者恰恰是把第二台非默认存储拦在门外的形状。
func batchM4ObsIndex(t *testing.T, gdb *gorm.DB) (unique, partial bool, def string, exists bool) {
	t.Helper()
	row := gdb.Raw(`SELECT i.indisunique, (i.indpred IS NOT NULL), pg_get_indexdef(i.indexrelid)
		FROM pg_index i
		JOIN pg_class ic ON ic.oid = i.indexrelid
		JOIN pg_class tc ON tc.oid = i.indrelid
		JOIN pg_namespace n ON n.oid = tc.relnamespace
		WHERE tc.relname = 'obs_config' AND ic.relname = 'idx_obs_config_single_default'
			AND n.nspname = current_schema()`).Row()
	err := row.Scan(&unique, &partial, &def)
	if err == sql.ErrNoRows {
		return false, false, "", false
	}
	if err != nil {
		t.Fatalf("读取 idx_obs_config_single_default 形状失败: %v", err)
	}
	return unique, partial, def, true
}

// batchM4DropObsTable 把 obs_config 抹回"这张表还不存在"的状态（= 全新部署前的库）。
//
// 前置必须钉成 Fatal 而不是"顺手删一下"：如果表还在（例如上一轮没清干净），
// S11 断的"启动路径把表建出来"就根本没发生，整条腿退化成批M 那几条的重复。
func batchM4DropObsTable(t *testing.T, gdb *gorm.DB) {
	t.Helper()
	if err := gdb.Migrator().DropTable(&model.ObsConfig{}); err != nil {
		t.Fatalf("清表失败: %v", err)
	}
	if gdb.Migrator().HasTable(&model.ObsConfig{}) {
		t.Fatal("前置不成立：obs_config 清不掉，本腿的『全新部署』前提没满足")
	}
	if _, _, _, ok := batchM4ObsIndex(t, gdb); ok {
		t.Fatal("前置不成立：表已删但索引还在，本腿会把自己的残留当启动路径的成果")
	}
}

// S11：全新部署跑一遍真启动 ⇒ obs_config 从 0 建出来，且那道**partial 唯一**索引随之就位，
// 并且就位之后真的挡得住第二条默认。
//
// 这条腿替 §23.8 第 3 条还了一句债：`db_test.go:TestAutoMigrate` 今天**也**走真启动路径
// （它调的就是 `AutoMigrate()` 本体，钩子照样跑），但它只断"返回值非 nil" ——
// 而那函数成功路径恒 `return DB`，所以它对启动结果一个字都没钉：
// 表没建出来、索引没建、清重没跑，它都绿。本腿钉的是这三件事本身。
func TestBatchM_StartupFreshDatabaseBuildsObsGuard(t *testing.T) {
	gdb := batchM4StartupDB(t)
	batchM4DropObsTable(t, gdb)

	if p := batchM4RunStartup(t); p != nil {
		t.Fatalf("启动期 AutoMigrate panic: %v", p)
	}

	if !gdb.Migrator().HasTable(&model.ObsConfig{}) {
		t.Fatal("启动路径没建出 obs_config：全新部署连存储配置都存不下")
	}
	unique, partial, def, exists := batchM4ObsIndex(t, gdb)
	if !exists {
		t.Fatal("启动路径跑完之后 idx_obs_config_single_default 不存在：钩子没挂在真路径上")
	}
	if !unique {
		t.Errorf("启动建出的索引不是唯一索引：indisunique=false，def=%s", def)
	}
	if !partial {
		t.Errorf("启动建出的索引没有 WHERE 谓词（indpred 为空）：def=%s —— 全列唯一会把第二台非默认存储拦在创建门外", def)
	}
	if !strings.Contains(def, "is_default") {
		t.Errorf("索引定义里读不到 is_default：def=%s", def)
	}

	// 建出来的那道必须**当场就能挡**：只查系统目录的话，"索引在但挡不住"这类形状错看不见。
	now := time.Now()
	batchMInsertObs(t, gdb, "batchm4-fresh-first", true, now)
	err := gdb.Create(&model.ObsConfig{
		Name: "batchm4-fresh-second", Provider: model.ObsProviderLocal,
		AccessKey: "ak", SecretKey: "sk", Bucket: "b",
		Status: model.ObsStatusActive, MaxSize: 1 << 20, MaxCount: 10,
		IsDefault: true, CreatedAt: now,
	}).Error
	if err == nil {
		t.Error("启动建出的索引挡不住第二条默认 —— 形状对了但没生效")
	}
	if names := batchMDefaults(t, gdb); len(names) != 1 || names[0] != "batchm4-fresh-first" {
		t.Errorf("默认行 = %v，期望 [batchm4-fresh-first]", names)
	}
}

// S12：第二次启动（索引已在）⇒ 一句 Warn 都不许有，且守卫仍然生效。
//
// 这是 M21（去掉 `IF NOT EXISTS`）在本批**从"等价"改成"有腿"**的原因：
// 少了 IF NOT EXISTS，第二次启动的 DDL 必报错，而钩子把错误吞进 logger.Warn 后 return，
// 索引还在、启动不炸 ⇒ 状态类断言全绿。唯一看得境它的门面是日志。
func TestBatchM_StartupSecondRunIsSilentAndGuardSurvives(t *testing.T) {
	gdb := batchM4StartupDB(t)
	batchMObsConfigTable(t, gdb)
	if p := batchM4RunStartup(t); p != nil {
		t.Fatalf("第一次启动 AutoMigrate panic: %v", p)
	}
	if _, _, _, ok := batchM4ObsIndex(t, gdb); !ok {
		t.Fatal("前置不成立：第一次启动没建出索引，『第二次启动不报错』就是空断言")
	}
	now := time.Now()
	batchMInsertObs(t, gdb, "batchm4-sil-first", true, now)

	log, panicked := batchM4CaptureStartup(t)
	if panicked != nil {
		t.Fatalf("第二次启动 AutoMigrate panic（索引已在时不该重跑 DDL）: %v", panicked)
	}
	if strings.Contains(log, batchM4LogDDLFailed) {
		t.Errorf("第二次启动报了建索引失败（启动日志长期带这条噪音，运维再也看不见别的 Warn）:\n%s", log)
	}
	if !strings.Contains(log, batchM4LogReady) {
		t.Errorf("第二次启动没报『已就绪』，说明钩子这一段没跑到（或文案改了）:\n%s", log)
	}
	// dups=0/1 时不许走清重分支：把门槛写成 `dups >= 0` 这类"每次都跑一遍 UPDATE"的改动会在这里红。
	if strings.Contains(log, batchM4LogDeduped) {
		t.Errorf("只有一条默认却打了清重日志:\n%s", log)
	}

	// 日志判据之外，状态判据一条都不能松：只断"没噪音"的话，把整段 DDL 删掉也能绿。
	if _, _, _, ok := batchM4ObsIndex(t, gdb); !ok {
		t.Fatal("第二次启动之后索引不见了")
	}
	err := gdb.Create(&model.ObsConfig{
		Name: "batchm4-sil-second", Provider: model.ObsProviderLocal,
		AccessKey: "ak", SecretKey: "sk", Bucket: "b",
		Status: model.ObsStatusActive, MaxSize: 1 << 20, MaxCount: 10,
		IsDefault: true, CreatedAt: now,
	}).Error
	if err == nil {
		t.Error("第二次启动之后第二条默认插进去了：守卫没活过重跑")
	}
}

// S13：存量双默认走**真启动路径**清重（批M 的 I4 是直接调钩子，这条是端到端）。
//
// 与 I4 的分工说清楚，别读成重复：I4 钉"清重留哪一条"的**选择判据**（留最早那条），
// 本腿钉的是"启动路径真的会把这段跑一遍"—— 把 migrate.go 里那句调用删掉（格 M20），
// I4 因为直接调函数而照绿，本腿必须红。这是 M20 从"只有静态锁"变成"静态锁 + 行为腿"的一格。
func TestBatchM_StartupDedupesExistingDefaultsOnRealPath(t *testing.T) {
	gdb := batchM4StartupDB(t)
	// 存量形状：表已在、索引还不在（= 钩子那次上线之前的库）。
	batchMObsConfigTable(t, gdb)
	if _, _, _, ok := batchM4ObsIndex(t, gdb); ok {
		t.Fatal("前置不成立：存量库里不该已经有那道索引，有的话清重分支根本不会被走到")
	}

	base := time.Now().Add(-time.Hour)
	// 插入顺序刻意与 created_at 顺序相反：留"最早"还是留"最后插的"，两判据必须能分开。
	batchMInsertObs(t, gdb, "batchm4-legacy-newest", true, base.Add(30*time.Minute))
	batchMInsertObs(t, gdb, "batchm4-legacy-oldest", true, base)
	if n := len(batchMDefaults(t, gdb)); n != 2 {
		t.Fatalf("前置不成立：默认行有 %d 条，期望 2", n)
	}

	log, panicked := batchM4CaptureStartup(t)
	if panicked != nil {
		t.Fatalf("启动期 AutoMigrate panic: %v", panicked)
	}
	if !strings.Contains(log, "obs_config 有 2 "+batchM4LogDeduped+" 1 条") {
		t.Errorf("启动路径没打出『2 条清成 1 条』这条 Warn（清重段没跑，或条数/降级数不对）:\n%s", log)
	}

	if names := batchMDefaults(t, gdb); len(names) != 1 || names[0] != "batchm4-legacy-oldest" {
		t.Errorf("真启动路径清重后默认行 = %v，期望 [batchm4-legacy-oldest]", names)
	}
	var total int64
	if err := gdb.Model(&model.ObsConfig{}).Count(&total).Error; err != nil {
		t.Fatalf("统计总行数失败: %v", err)
	}
	if total != 2 {
		t.Errorf("清重后总行数 = %d，期望 2（降级是改列，不是删行）", total)
	}
	if _, _, _, ok := batchM4ObsIndex(t, gdb); !ok {
		t.Error("清重之后索引没建成：双默认随时可复现")
	}
}

// S14：库侧列默认值（`model/obs_config.go` 的 gorm `default:` 标签）**承哪一层重**，此前无人量过。
// 本腿三段，一段一个问句：
//
//  1. 裸 SQL 不给 `status`/`is_default`/用量四列时，库里落下的是什么？（→ 必须是 active + 非默认）
//  2. 走 GORM、明传 `IsDefault: false` 的行，读回来是不是 false？（→ 格 M50/M51 的杀手在这一句与下一段）
//  3. 决定第 2 段那个 false 的，到底是写入侧给的值，还是列上的 default？
//
// 第 3 段是本批唯一的新知识，它把"标签承不承重"这一个问题拆成了两条路：
// 把库侧默认改成 true 之后再走一次同样的 `Create`，行**仍落 false** ⇒ GORM 把这一列显式写进了
// INSERT，**库侧列默认对走 GORM 的写入不承重**（§23.8 第 6 条当年那句话在这条路上是对的）。
// 反过来，第一段那个 false 完全由列默认给出 ⇒ 对**绕过 GORM 的写入**（裸 SQL、迁移脚本、
// 手工补数据、别家服务直连库），那枚标签是唯一还在岗的东西。
// 还有第三种形状要记下：格 M51 只把标签翻成 `default:true`（不动任何调用方），
// 行就落成了 true —— 因为 GORM 会用标签值**替换**结构体里的零值再写进去，
// 于是"调用方显式赋值了"这件事，只在赋的不是零值时才说话算数。
func TestBatchM_ModelColumnDefaultsApplyAtDatabaseLevel(t *testing.T) {
	gdb := batchM4StartupDB(t)
	batchMObsConfigTable(t, gdb)

	const name = "batchm4-raw-defaults"
	// 裸 SQL：不走 BeforeCreate、不走 GORM 的字段默认值填充，只给 NOT NULL 那几列。
	if err := gdb.Exec(`INSERT INTO obs_config (id, name, provider, access_key, secret_key, bucket)
		VALUES (?, ?, ?, ?, ?, ?)`,
		"batchm4-raw-1", name, string(model.ObsProviderLocal), "ak", "sk", "b").Error; err != nil {
		t.Fatalf("裸 INSERT 失败（说明某列既无库侧默认又非空）: %v", err)
	}

	var status string
	var isDefault bool
	var maxSize int64
	var maxCount int
	var totalSize int64
	var fileCount int
	// 只读断言到的那几列：created_at/updated_at 在裸 INSERT 下是 NULL，
	// 扫进 model.ObsConfig（time.Time）会报"NULL 转 time.Time 不支持"—— 那是夹具的错，不是判据。
	err := gdb.Raw(`SELECT status, is_default, max_size, max_count, total_size, file_count
		FROM obs_config WHERE name = ?`, name).
		Row().Scan(&status, &isDefault, &maxSize, &maxCount, &totalSize, &fileCount)
	if err != nil {
		t.Fatalf("读回裸 INSERT 那一行失败: %v", err)
	}
	if isDefault {
		t.Error("旁路写入不给 is_default 落成了 true：它会凭空造出第二条默认")
	}
	if status != string(model.ObsStatusActive) {
		t.Errorf("旁路写入不给 status 落成 %q，期望 %q（落成 inactive 的行不会被 GetDefault 选中）",
			status, model.ObsStatusActive)
	}
	if maxSize != 104857600 || maxCount != 1000 {
		t.Errorf("用量闸门默认值 = (%d,%d)，期望 (104857600,1000) —— 默认值归零等于旁路行不可用",
			maxSize, maxCount)
	}
	if totalSize != 0 || fileCount != 0 {
		t.Errorf("用量计数默认值 = (%d,%d)，期望 (0,0)", totalSize, fileCount)
	}

	// 第二段：走 GORM 的写入形状必须与非默认同形。这一句是格 M51 的杀手。
	batchMInsertObs(t, gdb, "batchm4-via-gorm", false, time.Now())
	var gormDefault bool
	if err := gdb.Raw(`SELECT is_default FROM obs_config WHERE name = ?`, "batchm4-via-gorm").
		Row().Scan(&gormDefault); err != nil {
		t.Fatalf("读回 GORM 那一行失败: %v", err)
	}
	if gormDefault {
		t.Error("batchMInsertObs 传 isDefault=false 却落成 true")
	}

	// 第三段：把"到底是谁决定了第 2 段那个 false"量出来。
	//
	// "传 false 落成 false"本身有歧义：可能是 GORM 把 `is_default = false` 显式写进了 INSERT，
	// 也可能是 GORM 压根没写这一列、由库侧默认值兜住。分开两者的唯一办法是把**库侧**默认改成
	// true 再走一次同样的 `Create`（只改列，不改任何产码）：
	// 落成 true ⇒ 这一列没进 INSERT；仍落 false ⇒ 决定权在写入侧，库侧默认管不到这条路。
	// 实测是后者。因此第 2 段真正钉住的不是"那一枚标签兜住了"（那是段 1 的事），而是
	// `default:false` 与零值同值 ⇒ GORM 写进去的就是调用方给的那个 false。
	if err := gdb.Exec(`ALTER TABLE obs_config ALTER COLUMN is_default SET DEFAULT true`).Error; err != nil {
		t.Fatalf("把库侧默认改成 true 失败: %v", err)
	}
	// 顺序是判据的一部分：`Create` 必须发生在库侧默认已经是 true 的时候。
	batchMInsertObs(t, gdb, "batchm4-probe-column-default", false, time.Now())
	var landed bool
	if err := gdb.Raw(`SELECT is_default FROM obs_config WHERE name = ?`, "batchm4-probe-column-default").
		Row().Scan(&landed); err != nil {
		t.Fatalf("读回探针行失败: %v", err)
	}
	if err := gdb.Exec(`ALTER TABLE obs_config ALTER COLUMN is_default SET DEFAULT false`).Error; err != nil {
		t.Errorf("把库侧默认摆回 false 失败（会把 altered 形状留给后面的腿）: %v", err)
	}
	// 收尾：把表重建回"只有 model 标签说了算"的形状，本腿不留下任何 ALTER 过的列。
	batchMObsConfigTable(t, gdb)
	if landed {
		// 这一句的红有**两个**因，本腿分不开（要分开就得改产码或换列名，都不在本批范围内）：
		// 格 M51（只把标签翻成 `default:true`）实测就是走第二个因红的 —— 那一格"传 isDefault=false
		// 却落成 true"那句会一起红，所以按"红因配对"读日志即可，别把本句单独当成"跳过这一列"的证据。
		t.Error("库侧默认改成 true 之后行落成了 true ⇒ 调用方给的那个 false 没说了算。两种因都可能：" +
			"① GORM 没把这一列写进 INSERT、由列上的 DEFAULT 兜住（本段设计要抓的那条通道）；" +
			"② `default:` 标签的值 ≠ 零值、GORM 拿标签值替下了调用方的零值" +
			"（此时第二段『传 isDefault=false 却落成 true』必然同红，格 M51 即此形）。" +
			"任一因落地，第 2 段的含义都要跟着改，§23.11 里『标签在哪一层承重』那段结论要重读")
	}
}

// S15：装配点静态锁。上面四条腿跑的都是 `AutoMigrate()` **本体**，而它上面那两句
// （`cmd/api/main.go` 里的 `db.InitDB()` / `db.AutoMigrate()`）此前零判据：谁把它们删掉、
// 换个顺序、或者调两遍，本批与批M 的全部 db 腿照样绿（它们自己调本体），而生产库里
// 三条 post-migrate 守卫（obs 单默认、message_hub 三元组、opportunities clue_id）一起不存在。
//
// 三条断言各钉一种真实失效形状：出现次数（0 = 没人跑迁移；>1 = 每次启动重复跑 DDL）、
// 与 `InitDB` 的先后（换序 = 拿还没赋值的包级全局句柄去建表）、
// 与 `InitDefaultStorageIfEmpty` 的先后（seed 跑到迁移之前 ⇒ 全新库里那次 seed 面对的是
// 一张还不存在的 `obs_config`，"装完系统有没有一个能用的默认存储"当场落空 —— S10 在
// 服务层钉的正是这一句，但它俩之间今天没人管）。
//
// 口径与边界（照 `internal/browser_automation/controller/error_code_b10_test.go` 批8 的教训写死）：
// 计数式静态锁挡不住"语句还在、却被包进恒假分支"。这里比的是**整行 = 一个制表符 + 语句**，
// 即"`func main` 顶层、独占一行"这一种形状。它挡得住：删掉（0 行）、挪进多行块（顶层那行没了 ⇒ 0 行）、
// 缩进变化；挡不住的是单行包装（`if false { db.AutoMigrate() }`）—— 那要 go/ast 级校验才拦得住，
// 本批不为它引依赖，这一条残余写在 §23.11 的边界里。另记一句：将来若把它合法地包进条件块，
// 这条腿会以"0 次"红 —— 那是在要求**同步改这条腿**，不是让它静默放宽成"缩进几格都算"。
//
// 为什么不写成 `strings.Count(s, "\n\tdb.AutoMigrate()\n")`：`strings.Count` 是**不重叠**计数，
// 紧邻的两句 `db.AutoMigrate()` 中间那个换行会被前一次命中吃掉 ⇒ 调两遍数出来仍是 1。
// 这不是推演，是格 M57 的第一版实测：打了"调两遍"的变异、整包全绿。整行比较没有这个共享分隔符问题。
func TestBatchM_MigrateIsWiredIntoAPILifecycle(t *testing.T) {
	src, err := os.ReadFile("../../../cmd/api/main.go")
	if err != nil {
		t.Fatalf("读 cmd/api/main.go 失败: %v", err)
	}
	lines := strings.Split(string(src), "\n")
	// 行号一律 1 基（`hit[0]+1`），报出来要能直接 goto。
	stmtLines := func(stmt string) []int {
		want := "\t" + stmt
		var hit []int
		for i, l := range lines {
			if l == want {
				hit = append(hit, i+1)
			}
		}
		return hit
	}
	const (
		initStmt    = "db.InitDB()"
		migrateStmt = "db.AutoMigrate()"
		seedStmt    = "service.InitDefaultStorageIfEmpty(db.GetDB())"
	)
	iInit, iMigrate, iSeed := stmtLines(initStmt), stmtLines(migrateStmt), stmtLines(seedStmt)
	if n := len(iMigrate); n != 1 {
		t.Errorf("main.go 里顶层独占一行的 db.AutoMigrate() 出现 %d 次，期望恰好 1 次（0 次 = 三条 post-migrate 守卫在生产库里全都不存在；>1 次 = 每次启动重复跑 DDL）", n)
	}
	if n := len(iInit); n != 1 {
		t.Errorf("main.go 里顶层独占一行的 db.InitDB() 出现 %d 次，期望恰好 1 次（本批的腿都靠换全局 DB 句柄绕开它，它在不在只有这条腿看得见）", n)
	}
	if len(iMigrate) > 0 && len(iInit) > 0 && iMigrate[0] < iInit[0] {
		t.Errorf("db.AutoMigrate()（:%d）排在 db.InitDB()（:%d）之前：包级全局句柄还没赋值，迁移是拿 nil *gorm.DB 去建表",
			iMigrate[0], iInit[0])
	}
	if len(iSeed) == 0 {
		t.Fatal("main.go 里读不到默认存储 seed 那一行，本条静态锁的口径需要跟着改")
	}
	if len(iMigrate) > 0 && iMigrate[0] > iSeed[0] {
		t.Errorf("db.AutoMigrate()（:%d）排在 InitDefaultStorageIfEmpty（:%d）之后：全新库里 seed 面对的是还没建的 obs_config 表，默认存储当场 seed 不出来",
			iMigrate[0], iSeed[0])
	}
}
