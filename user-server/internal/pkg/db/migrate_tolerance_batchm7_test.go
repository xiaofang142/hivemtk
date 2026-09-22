// migrate_tolerance_batchm7_test.go 补 §23.8 第 8 条剩下的最后半场：`AutoMigrate()` 本体的
// **容忍/兜底/终止**三条分支今天一条腿都没有 —— 批M 到批M-6 把三条 post-migrate 守卫逐个接到了
// 真启动路径上，但那条路径自己"遇到迁移漂移怎么办"这一步始终只有读码。
//
// 三条分支各有一种坏法，代价完全不同，所以三腿各钉一种（全按真启动跑，不直调）：
//
//   - S20 容忍 + 兜底（`isTolerableMigrateError` 的 already-exists 支 → `createTableFallback`）：
//     现场是"表被删了、同名复合类型还留在 pg_class 里"。PG 的 relation 名字空间不分表与类型
//     ⇒ `CREATE TABLE` 当场 42P07；而 GORM 的 `HasTable` 读的是 information_schema，**诚实报 false**
//     （探针实测）。这条分支的全部价值就是"把缺失的表建回来、服务照旧起得来"；
//     摘掉它 ⇒ 一次历史残留让整个服务起不来。
//   - S21 容忍但**不许触发兜底**（`AutoMigrate()` 里 `createTableFallback` 前面那道 `!HasTable` 门）：现场是"列上挂着一条手写名字的单列
//     UNIQUE 约束"。配方不是想出来的，是从 gorm@v1.30.0 `migrator/migrator.go:580` 的
//     `MigrateColumnUnique` 反推并实测出来的：库里 `Unique()` 只由**单列唯一约束**供给，
//     模型字段不带 unique ⇒ 它去 `DROP CONSTRAINT uni_<表>_<列>`，而那个名字从来没存在过
//     ⇒ 42704 ⇒ 落进容忍表。这时候表**在**、数据**在**，兜底一步都不该走。
//     探针另量了一件事，决定了这一格的判据怎么写：对已存在的表跑 `DROP TYPE … CASCADE`
//     报的是 2BP01、表与行**都不会被带走**（那句 Exec 的错误在产码里是丢弃的），
//     所以"摘掉那道门"的后果不是数据销毁，而是**日志谎报『已重建缺失表』** ⇒ 本腿红在文案面上。
//   - S22 不容忍 ⇒ 必 panic：现场是"给已有存量的表新增一列 not null"（23502，探针实测
//     不在容忍表里）。这一格反过来钉 `panic(err)` 那一支：把它改成 Warn+continue，
//     服务会在一张缺列的表上正常启动，第一次读改写那一列才炸 —— 而启动日志是干净的。
//
// S23/S25/S26 是**分类器与两个纯函数**的直调腿，判据面与上面三条分开：
// `isTolerableMigrateError` 自己会落一条 Warn（它的返回值与日志是绑在一起的），
// 所以真值表绝不能和启动日志断言同腿，否则"日志里有那句"就成了我自己写进去的。
// S24 再补装配面的一条：扩展是被 `AutoMigrate()` 第一句装的，卸掉之后跑一次真启动它就回来了。
//
// 两条分支在**基线代码**下构造不出触发现场，本批不给它们立启动腿，只把牙挂在变异格上：
//   - `ensureExtensions` 的白名单拒绝那句（`allowedExts` 与 `exts` 是同一函数里的两份字面量、
//     今天完全同集 ⇒ 没有第三个名字能把它点亮）。它不是"没判据"：格 M93 从 `allowedExts` 里
//     摘掉一个名字，这句 Warn 当场响，S24/S25 两条腿都断言它**不许响** ⇒ 正常代码下不响、
//     一改就红，这正是这一支该有的形状。
//   - `AutoMigrate()` 末尾那句 `missingTables` 终校验 panic（产码字面：`if missing := missingTables(DB, all...); len(missing) > 0 { panic(...) }`）：
//     模型循环的四条出口已全部有腿 —— S20 容忍且兜底建回来了、S21 容忍但表在场所以不进兜底、
//     S22 不容忍当场 panic、S30 兜底自己建不出来也当场 panic（产码字面：`兜底 CreateTable(%T) 后表仍缺失`）。
//     没有一条出口把"缺表"留到循环之后
//     ⇒ 那一句在基线下构造不出触发现场。本批判它的**判定半径**（S26 直调 `missingTables`），
//     把那句 panic 本身留在账上。
//
// 本文件指认产码位置一律用**符号 + 代码字面**、不写行号：`allModels()` 每登记一个模型就把
// 下面的行号整体推位（本轮实测：并行泳道往 `allModels()` 里加的 Quote 登记，把本文件原先写的
// `:401` 推成了 `:407` —— 一个记错位置的注释比不记更坏，它会让下一个人去核对一个不该存在的等式）。
//
// 合成模型一律挂 `RegisterExtraModels` 的同一条口子（`ExtraModels()` 与 `allModels()` 并列迁移），
// 表名前缀 `zz_batchm7_`、腿名前缀仍是 `TestBatchM_`（电池的 `-run TestBatchM_` 是子串匹配）。
package db

import (
	"database/sql"
	"errors"
	"strings"
	"testing"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/testutil"

	"gorm.io/gorm"
)

const (
	batchM7LogToleratedDrift = "AutoMigrate 完成，但存在可容忍的迁移漂移"
	batchM7LogNoDrift        = "AutoMigrate 完成，无迁移漂移"
	batchM7LogAlreadyExists  = "AutoMigrate 命中幂等重跑提示"
	batchM7LogConstraintDrft = "AutoMigrate 命中历史约束命名漂移"
	batchM7LogRebuilt        = "AutoMigrate 兜底 CreateTable 已重建缺失表"
	batchM7LogUnknownExt     = "跳过未知 PG 扩展"
	batchM7LogExtHint        = "启用 PG 扩展提示"
)

// S20 的现场：表不在、同名复合类型在。
type batchM7Orphan struct {
	ID   string `gorm:"column:id;primaryKey;size:64"`
	Note string `gorm:"column:note;size:64"`
}

func (batchM7Orphan) TableName() string { return "zz_batchm7_orphan" }

// S21 的现场：表在、列上挂着一条**手写名字**的单列 UNIQUE 约束，而字段不带 unique 标签。
type batchM7Drift struct {
	ID   string `gorm:"column:id;primaryKey;size:64"`
	Code string `gorm:"column:code;size:64"`
}

func (batchM7Drift) TableName() string { return "zz_batchm7_drift" }

// S22 的现场：表在、有存量行，模型新增一列 not null。
type batchM7NotNull struct {
	ID     string `gorm:"column:id;primaryKey;size:64"`
	Legacy string `gorm:"column:legacy;size:64"`
	Added  string `gorm:"column:added;size:64;not null"`
}

func (batchM7NotNull) TableName() string { return "zz_batchm7_notnull" }

// S30 的现场：模型要的那个名字被一枚**视图**占住 ⇒ 表怎么都建不出来（视图不是表，见 S30 注释）。
type batchM7ViewBlock struct {
	ID   string `gorm:"column:id;primaryKey;size:64"`
	Note string `gorm:"column:note;size:64"`
}

func (batchM7ViewBlock) TableName() string { return "zz_batchm7_viewblock" }

// batchM7RegisterExtra 把合成模型挂上**真启动路径**，收尾原样退回。
//
// 为什么不直接改 `allModels()`：那是产码，本批除 N-36 那一处之外不动产码；
// `extraModels` 本来就是"别的包也想走启动期迁移"的口子（`RegisterExtraModels` 的注释原文），
// 走它等于用生产同一条装配链，而不是另搭一条测试专用的迁移。
// 加锁读改、并按值备份：这个切片是全包共享的包级状态，直接 append 会改到别人背上的底层数组。
func batchM7RegisterExtra(t *testing.T, models ...any) {
	t.Helper()
	extraModelsMu.Lock()
	saved := extraModels
	extraModels = append(append([]any{}, saved...), models...)
	extraModelsMu.Unlock()
	t.Cleanup(func() {
		extraModelsMu.Lock()
		extraModels = saved
		extraModelsMu.Unlock()
	})
}

// batchM7DropEverything 收尾把本批留下的表与同名类型都抹掉。
// 顺序是**先表后类型**：反过来的话 `DROP TYPE … CASCADE` 会连带把表摘掉，
// 于是"我这趟到底清了什么"变成一次级联的副产品。
func batchM7DropEverything(t *testing.T, gdb *gorm.DB, table string) {
	t.Helper()
	if err := gdb.Exec(`DROP TABLE IF EXISTS ` + table).Error; err != nil {
		t.Errorf("收尾删表 %s 失败: %v", table, err)
	}
	if err := gdb.Exec(`DROP TYPE IF EXISTS ` + table).Error; err != nil {
		t.Errorf("收尾删类型 %s 失败: %v", table, err)
	}
}

// batchM7Relkind 读 pg_class 里这个名字的**对象种类**（r=表 c=复合类型；不存在时空串）。
// 为什么不能只看 `HasTable`：S20 的判据正是"残留的那枚类型换成了真表"，
// 而 information_schema 那一层看不到类型，读错对象就会把"什么都没建"读成"建好了"。
func batchM7Relkind(t *testing.T, gdb *gorm.DB, name string) string {
	t.Helper()
	var kind *string
	err := gdb.Raw(`SELECT relkind FROM pg_class c
		JOIN pg_namespace n ON n.oid = c.relnamespace
		WHERE c.relname = ? AND n.nspname = current_schema()`, name).Row().Scan(&kind)
	if errors.Is(err, sql.ErrNoRows) {
		return ""
	}
	if err != nil {
		t.Fatalf("读取 %s 的 relkind 失败: %v", name, err)
	}
	if kind == nil {
		t.Fatalf("pg_class 里 %s 的 relkind 是 NULL，这一档不是本批认得的形状", name)
	}
	return *kind
}

// batchM7LinesWith 数日志里**同时**含若干字面的行数。
//
// 为什么启动日志面的断言要经这个口子而不是裸 `strings.Contains`：一趟启动读的是全库两百多张表
// 的日志，只按文案找会把别家模型的一次正常提示读成本腿的判据 —— 红因与本腿无关（假红），
// 或者反过来本腿那一支根本没跑、却被别人替它响了一声（假绿）。
func batchM7LinesWith(log string, subs ...string) int {
	n := 0
	for _, line := range strings.Split(strings.TrimSpace(log), "\n") {
		hit := true
		for _, sub := range subs {
			if !strings.Contains(line, sub) {
				hit = false
				break
			}
		}
		if hit {
			n++
		}
	}
	return n
}

// batchM7MissingTablesSafely 把 `missingTables` 的 nil 句柄那一支收在 recover 里跑。
//
// 为什么要包：格 M94 摘掉的正是产码里那三行 `if db == nil { 全报缺失 }` 守卫，直调就是把一次
// nil 解引用甩给**整个测试二进制** —— 实测那趟 25 条顶层腿只跑到 12 条就带走，剩下 13 条状态
// 未知 ⇒ 这一格的红集合只有下界、上界判不了，"这条刀只打到指定腿"那句压根没被检验过。
// 包一层之后崩溃落回本腿自己的红因，其余腿照跑，判据一个字不用改。
func batchM7MissingTablesSafely(db *gorm.DB, models ...any) (missing []string, recovered interface{}) {
	defer func() { recovered = recover() }()
	return missingTables(db, models...), nil
}

// S20：表被删了、同名复合类型还留在库里 ⇒ 真启动一次就把它**自愈**成表，
// 并且第二次启动不再重复走兜底（同一句 Warn 天天刷 = 运维再也看不见别的东西）。
func TestBatchM_StartupHealsOrphanCompositeType(t *testing.T) {
	gdb := batchM4StartupDB(t)
	const tn = "zz_batchm7_orphan"
	t.Cleanup(func() { batchM7DropEverything(t, gdb, tn) })
	batchM7DropEverything(t, gdb, tn)
	if err := gdb.Exec(`CREATE TYPE ` + tn + ` AS (id varchar(64), note varchar(64))`).Error; err != nil {
		t.Fatalf("铺孤立复合类型失败: %v", err)
	}
	batchM7RegisterExtra(t, &batchM7Orphan{})

	// 两句前置钉夹具：残留没铺上，"自愈"就是别人的功劳（表本来就在）；
	// HasTable 必须是 false —— 它是那道 `!HasTable` 门的开关，前提反了整条腿判的就不是这条分支。
	if kind := batchM7Relkind(t, gdb, tn); kind != "c" {
		t.Fatalf("前置不成立：%s 的 relkind=%q，期望 c（复合类型残留没铺出来）", tn, kind)
	}
	if gdb.Migrator().HasTable(&batchM7Orphan{}) {
		t.Fatalf("前置不成立：HasTable 报 true ⇒ 表已在场，本腿走不到 createTableFallback 那一支")
	}

	log1, panicked := batchM4CaptureStartup(t)
	if panicked != nil {
		t.Errorf("一次历史残留把启动弄成 panic ⇒ 容忍+兜底这条链没接上（残留没被清掉时出口是兜底自己那句 :614 panic，先读 panic 文本再归因）: %v\n%s", panicked, log1)
	}
	// 那句 Warn 带 relation 名 ⇒ 按"同一行里既有文案又有本腿表名"数，别家模型的幂等提示不算本腿的账。
	if batchM7LinesWith(log1, batchM7LogAlreadyExists, tn) == 0 {
		t.Errorf("启动日志里没有一条同时含『%s』与 %s ⇒ 42P07 没落进容忍表（分类器那一支被人改坏 ⇒ 这种库的服务再也起不来）:\n%s",
			batchM7LogAlreadyExists, tn, log1)
	}
	if !strings.Contains(log1, batchM7LogRebuilt+": "+tn) {
		t.Errorf("兜底没有把缺失的表 %s 建回来（没有这句 Warn，运维无从知道有一张表是靠兜底重建的、而不是按常规迁移建出来的）:\n%s", tn, log1)
	}
	if !strings.Contains(log1, batchM7LogToleratedDrift) {
		t.Errorf("收尾汇总没报『存在可容忍的迁移漂移』⇒ 漂移被当成干净启动吞掉了:\n%s", log1)
	}
	// 上面那句只判"漂移那一支报了"，这一句判**汇总只报一条**：
	// 产码里那两句是一个 if/else 的两支，所以"报了漂移却没报无漂移"是算术推论、不设断言；
	// 真正没被邻句覆盖的坏法是有人把 else 拆成并列的第二个 if ⇒ 两句互相打脸的汇总一起印出来。
	if both := batchM7LinesWith(log1, batchM7LogToleratedDrift) + batchM7LinesWith(log1, batchM7LogNoDrift); both != 1 {
		t.Errorf("收尾汇总两句一共读了 %d 条，期望恰好 1 条（0 条=两支都没报，多于 1 条=两句打脸）:\n%s", both, log1)
	}

	// 状态面：类型换成表、表可读、行数 0。这里不检查"类型还在不在"——
	// 兜底的第一句就是 `DROP TYPE IF EXISTS … CASCADE`，它**必须**把类型让位给表。
	if kind := batchM7Relkind(t, gdb, tn); kind != "r" {
		t.Errorf("启动跑完之后 %s 的 relkind=%q，期望 r ⇒ 兜底没把表建出来（还是残留类型，或者什么都没建）", tn, kind)
	}
	if !gdb.Migrator().HasTable(&batchM7Orphan{}) {
		t.Error("启动跑完之后 HasTable 仍报 false")
	}
	var rows int64
	if err := gdb.Raw(`SELECT COUNT(*) FROM ` + tn).Row().Scan(&rows); err != nil {
		t.Errorf("自愈出来的表读不动: %v", err)
	} else if rows != 0 {
		t.Errorf("自愈出来的表里有 %d 行，期望 0（新表该是空的）", rows)
	}

	// 第二次启动：表已在 ⇒ 这一档整个不再触发。
	// 摘掉 `IF NOT EXISTS`、或者让兜底无条件跑一遍，都会红在这一段（而不是红在状态面：
	// 表照样在、照样读得动 —— 只有日志面看得见）。
	log2, panicked2 := batchM4CaptureStartup(t)
	if panicked2 != nil {
		t.Errorf("第二次启动 panic: %v", panicked2)
	}
	if strings.Contains(log2, batchM7LogRebuilt+": "+tn) {
		t.Errorf("表已经在场却还报『已重建缺失表』⇒ 兜底前面那道 `!HasTable` 门没了，启动日志会天天刷一句谎话:\n%s", log2)
	}
	// 这里钉的是**本腿那张表的名字一次都不许再出现**，而不是汇总那句『无迁移漂移』：
	// 那句读的是全库两百多张表，同一槽位库里别家留下的任何漂移都会把它顶红，红因与本腿无关。
	// （那句汇总文案本身的正反两面由 S21 那对 assertions 判，它判的正是"这一档确实算进漂移"。）
	if strings.Contains(log2, tn) {
		t.Errorf("干净重跑的日志里仍出现 %s ⇒ 表已经在场却还被迁移链提起（兜底又跑了一遍，或一次普通成功被读成了漂移）:\n%s", tn, log2)
	}
	if kind := batchM7Relkind(t, gdb, tn); kind != "r" {
		t.Errorf("第二次启动把表弄没了（relkind=%q）", kind)
	}
}

// S21：历史约束命名漂移被容忍 ⇒ **不 panic、不动数据、不谎报重建**，
// 而那条手写约束仍然在库里挡重（容忍不等于把约束也一并容忍掉）。
func TestBatchM_StartupToleratesLegacyConstraintNameDriftWithoutRewriting(t *testing.T) {
	gdb := batchM4StartupDB(t)
	const (
		tn            = "zz_batchm7_drift"
		legacyConst   = "uq_legacy_zz_batchm7"
		gormWouldWant = "uni_zz_batchm7_drift_code"
	)
	t.Cleanup(func() { batchM7DropEverything(t, gdb, tn) })
	batchM7DropEverything(t, gdb, tn)
	if err := gdb.Exec(`CREATE TABLE ` + tn + ` (id varchar(64) primary key, code varchar(64))`).Error; err != nil {
		t.Fatalf("铺存量表失败: %v", err)
	}
	if err := gdb.Exec(`INSERT INTO ` + tn + ` (id, code) VALUES ('a','x'),('b','y')`).Error; err != nil {
		t.Fatalf("铺存量行失败: %v", err)
	}
	if err := gdb.Exec(`ALTER TABLE ` + tn + ` ADD CONSTRAINT ` + legacyConst + ` UNIQUE (code)`).Error; err != nil {
		t.Fatalf("铺手写唯一约束失败: %v", err)
	}
	batchM7RegisterExtra(t, &batchM7Drift{})

	// 夹具三句前置：这三件凑齐才会触发 42704 那一支（单列 UNIQUE **约束** + 字段不带 unique 标签）。
	// 少任何一句，本腿后面读到的就是一次普通成功迁移，而不是容忍分支。
	var n int64
	if err := gdb.Raw(`SELECT COUNT(*) FROM pg_constraint WHERE conname = ?`, legacyConst).
		Row().Scan(&n); err != nil {
		t.Fatalf("读约束失败: %v", err)
	}
	if n != 1 {
		t.Fatalf("前置不成立：手写唯一约束在场 %d 条，期望 1", n)
	}
	if gdb.Migrator().HasIndex(&batchM7Drift{}, gormWouldWant) {
		t.Fatalf("前置不成立：%s 已在场 ⇒ GORM 不会再对它做列级对账，本腿走不到 42704 那一支", gormWouldWant)
	}
	if !gdb.Migrator().HasTable(&batchM7Drift{}) {
		t.Fatal("前置不成立：表不在场 ⇒ 本腿会走 S20 那条兜底分支，判的就不是这道门")
	}

	log, panicked := batchM4CaptureStartup(t)
	if panicked != nil {
		t.Errorf("一次历史命名漂移让整个服务起不来（容忍分支没接上，或分类器不再认这一档）: %v", panicked)
	}
	if !strings.Contains(log, batchM7LogConstraintDrft) {
		t.Errorf("启动日志里没有『%s』那一句 ⇒ 这一档漂移没人报出来:\n%s", batchM7LogConstraintDrft, log)
	}
	// 点名 GORM 找的那个名字，等于把**机理**钉在判据里：读到一个别的 42704 也算红。
	if !strings.Contains(log, gormWouldWant) {
		t.Errorf("漂移那一句日志里没点名 %s（=GORM 命名约定算出来的那个名字）⇒ 命中的可能不是同一件事:\n%s", gormWouldWant, log)
	}
	if strings.Contains(log, batchM7LogRebuilt) {
		t.Errorf("表明明在场却报了『%s』⇒ 兜底前面那道 `!HasTable` 门被摘掉，兜底对一张有数据的表空跑了一遍还谎报重建:\n%s",
			batchM7LogRebuilt, log)
	}
	if batchM7LinesWith(log, batchM7LogAlreadyExists, tn) != 0 {
		t.Errorf("这一档只有约束名漂移，本腿那张表上却也报了幂等重跑提示 ⇒ 分类器把两支混在了一起（两支的文案与后果都不同）:\n%s", log)
	}
	if !strings.Contains(log, batchM7LogToleratedDrift) {
		t.Errorf("收尾汇总没把它算进漂移 ⇒ 一次历史命名漂移被当成干净启动吞掉了（运维读到的是『%s』）:\n%s", batchM7LogNoDrift, log)
	}
	// 这里**不**再配"也不许出现『无迁移漂移』"：产码末尾那两句是同一个 if/else 的两支，
	// 报了漂移就必然没报无漂移。"两句合计恰好一条"那一格由 S20 判（同一处产码，不必三处重复）。

	// 状态面：容忍**只到"这次不建"为止**，不许顺手改结构、也不许丢数据。
	if err := gdb.Raw(`SELECT COUNT(*) FROM pg_constraint WHERE conname = ?`, legacyConst).
		Row().Scan(&n); err != nil {
		t.Fatalf("收尾前读约束失败: %v", err)
	}
	if n != 1 {
		t.Errorf("启动跑完之后手写唯一约束只剩 %d 条 ⇒ 兜底那句 `DROP TYPE … CASCADE` 或别的路径把它带走了", n)
	}
	var codes []string
	if err := gdb.Raw(`SELECT code FROM ` + tn + ` ORDER BY code`).Scan(&codes).Error; err != nil {
		t.Fatalf("读回存量行失败: %v", err)
	}
	if strings.Join(codes, ",") != "x,y" {
		t.Errorf("存量行 = %v，期望 x,y 原样都在（本批的容忍分支没有任何改写数据的权限）", codes)
	}
	if gdb.Migrator().HasIndex(&batchM7Drift{}, gormWouldWant) {
		t.Errorf("GORM 想要的那枚索引被建出来了 ⇒ 漂移被『顺手补齐』了，而那正是要人判断命名的历史包袱")
	}
	// 约束还挡得住重（它才是这张表上唯一的唯一性来源）：重复 code 必撞、换值不受牵连。
	if err := gdb.Exec(`INSERT INTO ` + tn + ` (id, code) VALUES ('c','x')`).Error; err == nil {
		t.Error("重复 code 落进去了 ⇒ 那条手写唯一约束已经不生效，本腿读到的形状自相矛盾")
	}
	if err := gdb.Exec(`INSERT INTO ` + tn + ` (id, code) VALUES ('d','z')`).Error; err != nil {
		t.Errorf("换一个新值反而插不进: %v", err)
	}
	if err := gdb.Exec(`DELETE FROM ` + tn + ` WHERE id IN ('c','d')`).Error; err != nil {
		t.Errorf("收尾删探针行失败: %v", err)
	}
}

// S22：不可容忍的迁移错误必须**当场 panic**，而不是 Warn 之后继续启动。
//
// 现场不是编的：给一张有存量的表新增一列 `not null`（没有默认值）是迁移里最常见的一类动作，
// PG 报 23502 contains null values（探针实测这一档不在容忍表里）。
// 少这一格 panic 的后果不是"起不来"，而是**服务在一片干净日志里起来、
// 等到第一次写那一列才炸** —— 那种炸法离根因远得多。
func TestBatchM_StartupPanicsOnNonTolerableNotNullDrift(t *testing.T) {
	gdb := batchM4StartupDB(t)
	const tn = "zz_batchm7_notnull"
	t.Cleanup(func() { batchM7DropEverything(t, gdb, tn) })
	batchM7DropEverything(t, gdb, tn)
	if err := gdb.Exec(`CREATE TABLE ` + tn + ` (id varchar(64) primary key, legacy varchar(64))`).Error; err != nil {
		t.Fatalf("铺存量表失败: %v", err)
	}
	if err := gdb.Exec(`INSERT INTO ` + tn + ` (id, legacy) VALUES ('a','x'),('b','y')`).Error; err != nil {
		t.Fatalf("铺存量行失败: %v", err)
	}
	batchM7RegisterExtra(t, &batchM7NotNull{})
	if gdb.Migrator().HasColumn(&batchM7NotNull{}, "Added") {
		t.Fatal("前置不成立：added 列已在场 ⇒ 不会触发 23502，本腿走不到不容忍那一支")
	}

	panicked := batchM4RunStartup(t)
	if panicked == nil {
		t.Fatal("给有存量的表新增 not null 列，启动却成功了 ⇒ 不容忍分支不再 panic" +
			"（把 `panic(err)` 那一支换成 Warn+continue 就是这一档）：" +
			"服务会在一列根本建不出来的表上正常起来，第一次写那一列才炸，而启动日志是干净的")
	}
	msg, ok := panicked.(error)
	if !ok {
		t.Fatalf("panic 的不是 error（%T）⇒ `panic(err)` 那一支被换成了别的出口，判据面要重看", panicked)
	}
	// 文案 + 本腿表名一起钉：全库两百多张表里任何一张撞出 23502 都会 panic，
	// 只查 "null values" 等于把归因交给运气。
	if m := msg.Error(); !strings.Contains(m, "null values") || !strings.Contains(m, tn) {
		t.Errorf("panic 文本里没有那句 23502、或没点名 %s（带着根因与本腿表名才谈得上『启动期就报出来』）: %v", tn, m)
	}
	// panic 不撤销已经跑过的迁移：这一档的坏法只可能出现在这张合成表上，
	// 存量两行必须原样在（不容忍 ≠ 回滚，但它也没有改数据的权限）。
	var rows int64
	if err := gdb.Raw(`SELECT COUNT(*) FROM ` + tn).Row().Scan(&rows); err != nil {
		t.Fatalf("panic 之后读不动存量表: %v", err)
	}
	if rows != 2 {
		t.Errorf("panic 之后存量行数 = %d，期望 2（原样）", rows)
	}
	// 终校验的出口也要读得到：panic 发生在模型循环里，那张表**没建成列**，
	// 于是这条腿同时是"不容忍错误不会被容忍表吞掉"的唯一反例。
	if gdb.Migrator().HasColumn(&batchM7NotNull{}, "Added") {
		t.Error("23502 之后 added 列却在场 ⇒ 这一档其实是成功的，本腿关于『不容忍』的结论不成立")
	}
}

// S23：分类器的真值表（**直调**，判据面与 S20–S22 的日志面刻意分开）。
//
// 为什么必须单独一条：S20/S21/S22 只能从"库里恰好造得出来的三种错误"各证一档，
// 而分类器是一句 `Contains && Contains` —— 与条件退化成或条件时，误判的是**只含一个关键词**的两种错误：
// 只含 "constraint" 的那一类（23514 违反检查约束 ⇒ 真故障被当成可容忍吞掉），
// 与只含 "does not exist" 的那一类（42P01 relation 不存在 ⇒ 真缺表被放行，正是 S20 那档会掩盖的形状）。
// 23502（S22 那一档）两个关键词都不含、与或之下同假 ⇒ 它**不是**这一格的杀手；本腿下面第 ⑥ 档的
// why 文本说的就是这件事（第一版这段注释写成"23502 会被误判"，与自己的表格打脸，改口）。
// 这两侧在真启动路径上都得**恰好错配**的现场才显形，直调一次就够。
//
// 直调的代价写在账上：`isTolerableMigrateError` 判定为可容忍时**自己会落一条 Warn**，
// 所以这一腿绝不许同时断言启动日志 —— 那句日志是我自己写进去的。
func TestBatchM_MigrateErrorClassifierTruthTable(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
		why  string
	}{
		{"nil 不是错误", nil, false, "调用点已经判过 err!=nil；这里钉的是分类器自身不把 nil 当可容忍"},
		{"约束名漂移(42704)", errors.New(`ERROR: constraint "uni_zz_batchm7_drift_code" of relation "zz_batchm7_drift" does not exist (SQLSTATE 42704)`), true,
			"S21 那一档的原文"},
		{"relation 已存在(42P07)", errors.New(`ERROR: relation "zz_batchm7_orphan" already exists (SQLSTATE 42P07)`), true,
			"S20 那一档的原文"},
		{"relation 不存在(42P01)", errors.New(`ERROR: relation "opportunities" does not exist (SQLSTATE 42P01)`), false,
			"只含 does not exist、不含 constraint ⇒ 真缺东西，绝不能容忍"},
		{"check 约束违反", errors.New(`ERROR: new row for relation "obs_config" violates check constraint "ck_obs_status" (SQLSTATE 23514)`), false,
			"只含 constraint、不含 does not exist ⇒ 与条件退化成或条件时这一条会被误判"},
		{"非空列含存量值(23502)", errors.New(`ERROR: column "added" of relation "zz_batchm7_notnull" contains null values (SQLSTATE 23502)`), false,
			"S22 那一档的原文：它含 constraint 字样吗不含，含 does not exist 也不含"},
		{"语法错误", errors.New(`ERROR: syntax error at or near "TABL" (SQLSTATE 42601)`), false, "与两张表都无关的错误"},
	}
	for _, c := range cases {
		if got := isTolerableMigrateError(c.err); got != c.want {
			t.Errorf("%s ⇒ isTolerableMigrateError=%v，期望 %v（%s）", c.name, got, c.want, c.why)
		}
	}
	// 这里**不**再加一句"两支都得各自认账"的收尾断言（写过，删了）：它是上面第 ②③ 两档的合取，
	// 表跑完之后恒为真 —— 一条只会跟着别的断言一起红的断言不判任何东西，留着等于把"这一腿还缺判据"
	// 这件事藏得更深。两支各自失守由 ②③ 两档分别红，合并成一支由 ④⑤ 两档（只含一个关键词）红。
}

// S24：PG 扩展是被 `AutoMigrate()` **第一句**装的 ⇒ 卸掉 uuid-ossp 再跑一次真启动，它自己回来。
//
// 为什么用 uuid-ossp 不用 vector：本镜像两家都装得上（探针实测），但 `vector` 在跑过全量迁移的
// 槽位库里有依赖对象（`internal/model` 里四张表带 `type:vector(1024)` 列）⇒ `DROP EXTENSION vector`
// 必被拒。这不是判据缺失，是这一档在**已建库**上构造不出来；它由"同一个循环里的 uuid-ossp 装回来了"
// 这条装配证据覆盖（两支同一次循环、同一句 Exec）。
// 而"从 `exts` 名单里摘掉 vector"那一格（M92）**不指定本腿当杀手**：testutil 建库时自己就先
// `CREATE EXTENSION IF NOT EXISTS vector`，所以真启动路径上这一改观察不到任何东西；
// 判它的是 S25 那条"必失败"的腿 —— 那里两句 Warn 各自点名一家，名单少一家就少一句。
func TestBatchM_StartupReinstallsMissingPGExtension(t *testing.T) {
	gdb := batchM4StartupDB(t)
	t.Cleanup(func() {
		if err := gdb.Exec(`CREATE EXTENSION IF NOT EXISTS "uuid-ossp"`).Error; err != nil {
			t.Errorf("收尾补回 uuid-ossp 失败（会把缺扩展留给同槽位的其它腿）: %v", err)
		}
	})
	if err := gdb.Exec(`DROP EXTENSION IF EXISTS "uuid-ossp"`).Error; err != nil {
		t.Fatalf("前置不成立：卸 uuid-ossp 失败(%v) ⇒ 有东西依赖它，本腿没法判『装回来』这件事", err)
	}
	var present int64
	if err := gdb.Raw(`SELECT COUNT(*) FROM pg_extension WHERE extname = 'uuid-ossp'`).Row().Scan(&present); err != nil {
		t.Fatalf("读扩展清单失败: %v", err)
	}
	if present != 0 {
		t.Fatalf("前置不成立：扩展还在场（%d 条）⇒ `IF NOT EXISTS` 会空转，本腿读到的回来不是启动的功劳", present)
	}

	log, panicked := batchM4CaptureStartup(t)
	if panicked != nil {
		t.Fatalf("少一个扩展把启动弄成 panic（ensureExtensions 的既有口径是装不上只 Warn）: %v", panicked)
	}
	if err := gdb.Raw(`SELECT COUNT(*) FROM pg_extension WHERE extname = 'uuid-ossp'`).Row().Scan(&present); err != nil {
		t.Fatalf("启动之后读扩展失败: %v", err)
	}
	if present != 1 {
		t.Errorf("真启动跑完之后 uuid-ossp 不在场（%d 条）⇒ `AutoMigrate()` 第一句的 ensureExtensions 没挂上或被摘了；"+
			"而这一句红的时候表可能照样建得出（模型不依赖 uuid-ossp），只有扩展目录看得见", present)
	}
	// 扩展真的活着：它的函数可用（不查 pg_extension 那一行就够了，装个空壳也算在场）。
	var got *string
	if err := gdb.Raw(`SELECT uuid_generate_v4()::text`).Row().Scan(&got); err != nil {
		t.Errorf("扩展回来之后 uuid_generate_v4() 仍不可用: %v", err)
	} else if got == nil || len(*got) < 30 {
		t.Errorf("uuid_generate_v4() 返回值可疑: %v", got)
	}
	if strings.Contains(log, batchM7LogExtHint) {
		t.Errorf("装扩展这句报了 Warn ⇒ 真库上这就是一次静默失败（今天 uuid 默认值不靠它，但报出来才是这条 Warn 的全部作用）:\n%s", log)
	}
	if strings.Contains(log, batchM7LogUnknownExt) {
		t.Errorf("白名单把该装的扩展拦了（这句一旦出现，说明 exts 与 allowedExts 两份字面量不再同源）:\n%s", log)
	}
}

// S25：装扩展失败的那一支 ⇒ 只 Warn、不 panic（直调，句柄是**已关掉**的独立连接）。
//
// 为什么要单开一条腿而不是在 S24 里顺手断：这一支要的是 `DB.Exec` **真的报错**，
// 而探针实测本镜像两家扩展都装得上 ⇒ 在好库上这一支永远走不到。
// 拿一个已关闭的句柄去调它是成本最低的"报错面"现场（`testutil.NewTestDB` 每次开**独立**句柄
// 并注册 Close 收尾，所以关掉它不会影响别的腿，也不会被 `batchM4StartupDB` 的还原波及）。
func TestBatchM_EnsureExtensionsWarnsButDoesNotPanicOnDeadHandle(t *testing.T) {
	fresh := testutil.NewTestDB(t)
	if fresh == nil {
		t.Fatal("测试库不可达")
	}
	sqlDB, err := fresh.DB()
	if err != nil {
		t.Fatalf("拿裸句柄失败: %v", err)
	}
	if err := sqlDB.Close(); err != nil {
		t.Fatalf("关掉探针句柄失败: %v", err)
	}
	saved := DB
	DB = fresh
	t.Cleanup(func() { DB = saved })

	log := batchM7CaptureLogs(t, ensureExtensions)
	if strings.Contains(log, batchM7LogUnknownExt) {
		t.Errorf("白名单拦下了该装的扩展（exts 与 allowedExts 两份字面量不再同源）:\n%s", log)
	}
	hints := batchM7LinesWith(log, batchM7LogExtHint)
	if hints != 2 {
		t.Errorf("句柄已关，两句 CREATE 都该失败 ⇒ 日志里应有 2 条 Warn，实读 %d 条 ⇒ 装不上扩展这件事没人报全（这一支的全部作用就是让它可见），或者该装的名单被摘短了（格 M92 打的就是这一句）:\n%s", hints, log)
	}
	// 逐家点名：`exts` 里有两个名字 ⇒ 那两句 Warn 必须各自点出 `vector` 与 `uuid-ossp`。
	// 为什么光数条数不够（踩过）：只断"两句都在"时，从名单里删掉 vector、却又因别的原因多出一次 Warn，
	// 这一格就绿了；而"名单里还有没有 vector"正是 S24 在**好库**上判不动的那一半（探针实测：
	// `testutil.NewTestDB` 自己就先 `CREATE EXTENSION IF NOT EXISTS vector`，所以真启动路径上
	// 摘掉 vector 观察不到任何东西 ⇒ 只能在这条"必失败"的腿上按名字钉）。
	// 按**裸名**找而不是按 `"uuid-ossp"` 找：日志是 JSON 编码的，名字外那对引号在原文里是 `\"`
	// （第一版把整串带引号的文案当子串去查 ⇒ 两条都"找不到"，红在取证上而不是判据上）。
	// 只数"这一家被点了几次"，不去拼 `err=` 尾巴：尾巴的形态由编码器决定，钉它等于把判据绑在日志编码上。
	for _, name := range []string{"vector", "uuid-ossp"} {
		named := batchM7LinesWith(log, batchM7LogExtHint, name)
		if named != 1 {
			t.Errorf("点名家 %s 的那句 Warn 读了 %d 条，期望 1 条 ⇒ 该装的扩展名单与文案不再一致"+
				"（少一条先看 %s 是不是被从 exts 里摘了；多一条则是名单里出现了重名）:\n%s", name, named, name, log)
		}
	}
	// 到这里没 panic 就是判据：batchM7CaptureLogs 的 recover 会把 panic 直接转成 Fatal。
	// 不另设"必须看到某句结尾"的断言 —— 那等于把"没炸"这件事再包一层文案依赖。
}

// S26：终校验的两个纯函数（**直调**）。
//
// 为什么不给 `AutoMigrate()` 里那句终校验 panic 编一条启动路径腿：构造不出来。
// 模型循环里的四条出口每一条都收在"表建好了"或"当场 panic"上（S20 兜底成功 / S21 容忍但表在场 /
// S22 不容忍 / S30 兜底自己失败），
// 而 `missingTables` 读的是循环**之后**的状态 —— 探针实测 GORM 的 HasTable 不撒谎
// （表没建出来时报的就是 false，见 S20 那句前置）。所以这一格登记为**构造性不可达**，
// 本腿只判纯函数的判定半径：倒装的条件（把 !HasTable 写成 HasTable）在这里红，
// 而在启动路径上没有任何一条腿能红 —— 这条差异本身就是本腿存在的理由。
func TestBatchM_MissingTablesAndTableNameHelpers(t *testing.T) {
	gdb := batchM4StartupDB(t)
	if err := gdb.AutoMigrate(&model.Opportunity{}); err != nil {
		t.Fatalf("铺一张在场的表失败: %v", err)
	}
	// ④ 会亲手把 `zz_batchm7_orphan` 建出来，而 ① 的期望条数**以它不在场为前提** ⇒ 前置要摆平、
	// 收尾要带走（两处都是踩过的：`-count=2`/`-shuffle` 下第二趟的 ① 会读到第一趟留下的表，
	// 报成"终校验漏判"那种根本没有发生的红）。
	const (
		tn       = "zz_batchm7_missing"
		orphanTn = "zz_batchm7_orphan"
	)
	t.Cleanup(func() {
		batchM7DropEverything(t, gdb, tn)
		batchM7DropEverything(t, gdb, orphanTn)
	})
	batchM7DropEverything(t, gdb, tn)
	batchM7DropEverything(t, gdb, orphanTn)
	if gdb.Migrator().HasTable(&batchM7Orphan{}) {
		t.Fatalf("前置不成立：%s 已在场 ⇒ ① 那句『恰好缺一张』无从判，本腿读到的可能是上一趟的残留", orphanTn)
	}

	// ① 只报缺的那一张：在场的不报、缺的报，且报的是模型类型名（运维据此能定位到 Go 类型）。
	missing := missingTables(gdb, &model.Opportunity{}, &batchM7Orphan{})
	if len(missing) != 1 || missing[0] != "*db.batchM7Orphan" {
		t.Errorf("missingTables=%v，期望恰好一条 %q ⇒ 条件倒装或漏判都在这句上显形", missing, "*db.batchM7Orphan")
	}
	// ② 没有句柄时不许"全都算齐"：那是终校验最坏的一种失效（绿灯说"表都在"，其实一张都没查）。
	// 走 batchM7MissingTablesSafely 而不是直调：理由与代价写在那条注释里（格 M94 那一刀会把整包带走）。
	noneDB, nonePanic := batchM7MissingTablesSafely(nil, &model.Opportunity{}, &batchM7Orphan{})
	if nonePanic != nil {
		t.Errorf("db==nil 时 missingTables 当场崩了: %v ⇒ 没有句柄时它既没报缺也没报齐，终校验那一步在启动早期不可判定", nonePanic)
	}
	if len(noneDB) != 2 {
		t.Errorf("db==nil 时 missingTables=%v，期望两条全报（宁可红在启动期，也不要绿着骗过终校验）", noneDB)
	}
	// ③ tableNameOf 读的是模型自己的表名（兜底那句 DROP TYPE 的拼串输入源）。
	if got := tableNameOf(gdb, &batchM7Orphan{}); got != "zz_batchm7_orphan" {
		t.Errorf("tableNameOf=%q，期望 zz_batchm7_orphan ⇒ 兜底会去 DROP 别的名字，或干脆空跑", got)
	}
	if got := tableNameOf(gdb, 42); got != "" {
		t.Errorf("解析不了的 dest 却拿到表名 %q ⇒ 那句 `DROP TYPE IF EXISTS %%s CASCADE` 会被拼进一个来历不明的名字", got)
	}
	// ④ 建完之后同一个 helper 必须翻口：这一句把 ① 从"永远报缺"那种坏法里分出来。
	if err := gdb.AutoMigrate(&batchM7Orphan{}); err != nil {
		t.Fatalf("直接迁移合成模型失败: %v", err)
	}
	if still := missingTables(gdb, &batchM7Orphan{}); len(still) != 0 {
		t.Errorf("建完之后仍报缺失: %v", still)
	}
}

// S30：兜底建不出表 ⇒ 不许报『已重建缺失表』，必须当场 panic 点名是哪张表。
//
// 现场不是编的，两件事在视图上恰好错开（都是探针实测，不是推断）：
//   - GORM 建表走的是裸 `CREATE TABLE`（gorm@v1.30.0 `migrator/migrator.go:225`，不带 IF NOT EXISTS），
//     撞上同名视图报 42P07 relation "…" already exists —— 与 S20 那枚孤立复合类型**同一档**，
//     因此照样落进容忍表；
//   - GORM 的 HasTable 只数 `table_type = 'BASE TABLE'`（`driver/postgres@v1.6.0/migrator.go:225`），
//     视图在它眼里就是"这张表不存在"（探针另量了一句：`DROP TABLE` 对视图报 "not a table"，
//     所以本腿的收尾必须先 DROP VIEW，不能复用 batchM7DropEverything）。
//
// 于是链路一路走到兜底的**第二道出口**：容忍 → !HasTable → createTableFallback →
// 那句 `DROP TYPE IF EXISTS … CASCADE` 对视图是 no-op → CreateTable 再撞 42P07（同样容忍）→
// :611 那道"建出来了没有"的核验 ⇒ else 的 panic。
//
// 为什么单独立腿而不并给 S20：S20 那条现场的兜底是**成功**的，成功分支下 :611 写成什么都是同一句日志，
// 它判不到这一档。而 :611 是整条链上唯一能说清"兜底到底成没成"的地方 —— 它退化成无条件报成功之后，
// 同一次启动会先印"已重建 zz_batchm7_viewblock"、再印"以下模型对应的数据表缺失：batchM7ViewBlock"，
// 两句互相打脸，运维只能挑一句信；挑错的那一句，就是在一张根本不存在的表上跑业务。
func TestBatchM_FallbackPanicsInsteadOfClaimingRebuildWhenNameIsTakenByView(t *testing.T) {
	gdb := batchM4StartupDB(t)
	const (
		viewTn = "zz_batchm7_viewblock"
		baseTn = "zz_batchm7_viewbase"
	)
	// 收尾顺序是视图在前：视图依赖底表，反过来 DROP 底表会被依赖挡住。
	t.Cleanup(func() {
		if err := gdb.Exec(`DROP VIEW IF EXISTS ` + viewTn).Error; err != nil {
			t.Errorf("收尾删视图失败: %v", err)
		}
		batchM7DropEverything(t, gdb, baseTn)
	})
	if err := gdb.Exec(`DROP VIEW IF EXISTS ` + viewTn).Error; err != nil {
		t.Fatalf("清场删视图失败: %v", err)
	}
	batchM7DropEverything(t, gdb, baseTn)
	if err := gdb.Exec(`CREATE TABLE ` + baseTn + ` (id varchar(64), note varchar(64))`).Error; err != nil {
		t.Fatalf("铺视图底表失败: %v", err)
	}
	if err := gdb.Exec(`INSERT INTO ` + baseTn + ` (id, note) VALUES ('a','x'),('b','y')`).Error; err != nil {
		t.Fatalf("铺底表存量行失败: %v", err)
	}
	if err := gdb.Exec(`CREATE VIEW ` + viewTn + ` AS SELECT id, note FROM ` + baseTn).Error; err != nil {
		t.Fatalf("铺占位视图失败: %v", err)
	}
	batchM7RegisterExtra(t, &batchM7ViewBlock{})

	// 两句前置钉夹具：视图真的在那个名字上、GORM 真的不把它当表 ——
	// 任一不成立，本腿读到的就是一次普通建表（或一次别的原因的 panic），判据全数落空。
	if kind := batchM7Relkind(t, gdb, viewTn); kind != "v" {
		t.Fatalf("前置不成立：%s 的 relkind=%q，期望 v（占位视图没铺出来）", viewTn, kind)
	}
	if gdb.Migrator().HasTable(&batchM7ViewBlock{}) {
		t.Fatal("前置不成立：HasTable 把视图读成了表 ⇒ 本腿走不到 createTableFallback 那一支，判据要按当前 GORM 版本重推")
	}

	log, panicked := batchM4CaptureStartup(t)
	if panicked == nil {
		t.Fatal("名字被视图占住、表一张都没建出来，启动却成功了 ⇒ :611 那道核验不再承重")
	}
	// :614 用的是 `panic(fmt.Sprintf(...))`，所以 recover 到的是 string；
	// 两种形状都接住，是为了"哪天它改成 panic(err)"时这条腿仍读得到文本，而不是先红在类型上。
	var msg string
	switch p := panicked.(type) {
	case string:
		msg = p
	case error:
		msg = p.Error()
	default:
		t.Fatalf("panic 的值既不是 string 也不是 error（%T）⇒ 兜底那句出口换了形状，判据面要重看", panicked)
	}
	// 点名**是哪一句** panic：变异体里 :418 的终校验 panic 同样会响，但那是一句"别人替这张表说话"，
	// 与它自己刚报过的『已重建』打脸 ⇒ 本腿只认兜底这一句出口。
	if !strings.Contains(msg, "兜底 CreateTable") || !strings.Contains(msg, "后表仍缺失") ||
		!strings.Contains(msg, "batchM7ViewBlock") {
		t.Errorf("panic 文本不是 :614 那句兜底出口（要同时含『兜底 CreateTable』『后表仍缺失』和模型名）: %v", msg)
	}
	if strings.Contains(log, batchM7LogRebuilt+": "+viewTn) {
		t.Errorf("表根本没建出来，日志却报了『%s: %s』⇒ 运维据此以为兜底补上了，而它和后面的终校验自相矛盾:\n%s",
			batchM7LogRebuilt, viewTn, log)
	}
	// 撞视图这一档必须是**被容忍过的** 42P07，而不是别的错误被顺手吞掉（否则机理就不是我登记的这一条）。
	if !strings.Contains(log, batchM7LogAlreadyExists) {
		t.Errorf("启动日志里没有『%s』⇒ 视图占名报的 42P07 没落进容忍表，本腿读到的 panic 与登记的机理不是同一件事:\n%s",
			batchM7LogAlreadyExists, log)
	}

	// 状态面三件事：视图还在、读得动、底表两行原样（兜底那句 CASCADE 不许带走别人的东西）。
	if kind := batchM7Relkind(t, gdb, viewTn); kind != "v" {
		t.Errorf("启动跑完之后 %s 的 relkind=%q，期望 v ⇒ 兜底换掉/删掉了别人的视图", viewTn, kind)
	}
	var rows int64
	if err := gdb.Raw(`SELECT COUNT(*) FROM ` + viewTn).Row().Scan(&rows); err != nil {
		t.Fatalf("启动跑完之后视图读不动: %v", err)
	}
	if rows != 2 {
		t.Errorf("视图读回 %d 行，期望底表那 2 行原样 ⇒ CASCADE 一旦带走依赖就是数据可见性事故", rows)
	}
}
