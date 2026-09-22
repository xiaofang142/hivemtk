// opportunity_clue_startup_batchm6_test.go 把 §23.12 第 9 段第 ① 条从"这句话没人读"变成"这句话有腿"：
// `AutoMigrate()` 末尾三条 post-migrate 守卫里，obs 那条由批M/批M-2/批M-4 接到真实启动路径，
// message_hub 那条由批M-5 接上，只剩 `postMigrateOpportunityClueUniqueIndex` 今天
// **只有 `opportunity_migration_test.go` 里四处包内直调**（:204/:289/:343/:363）⇒
// "下一次启动真的会建出商机那道 partial 索引"这句话的证据强度还停在批M-4 之前的 obs 水平。
//
// 直调那四条已经测了不少（索引形状、重复撞键、空串/NULL 不受约束、钩子可重跑），
// 它们共同测不到的是**只有真启动路径才回答的两件事**，本批两条腿各钉一件：
//
//   - S18「有没有人在建完表之后调它」。四处直调全都自己拿句柄跑函数，
//     把 migrate.go 里那句调用删掉（格 M70）或者把它挪到模型迁移**之前**（格 M71，
//     那张表还不存在 ⇒ 撞 `relation does not exist`）它们全都照绿。
//     §23.8 第 8 条当年开的方子是"照 obs 的 I5 补一条静态锁"——本批量完之后**不补那条锁**：
//     静态锁只读文本，而这两格红的都是"位置不对"，跑本体一次就把 presence 与 position 一起钉住，
//     强度严格高于计数式锁（I5 那类锁挡不住"挪进恒假分支"，这里挡得住）。
//   - S19「存量真有重复时它会不会自己动手改数据」。`opportunity_migration_test.go:352-371`
//     那一段今天只断两件事：没 panic、表没被清空（`remaining != 0`）。而 §5 与钩子注释写的是
//     更强的承诺 —— **重复留哪一条要人判断**（同一条线索转出的两个商机，哪个才是真身只有业务知道），
//     所以钩子的失败分支必须"索引不建 + Warn 点名人工清重"。今天这两半一个都没断言：
//     把失败分支改成"DELETE 掉重复行再建"（格 M76），旧腿**全绿**，而那是启动路径上的静默数据销毁。
//
// 腿名前缀仍是 `TestBatchM_`（电池的 `-run TestBatchM_` 是子串匹配，换前缀等于新腿不进电池），
// 夹具复用批M-4 的 `batchM4StartupDB` / `batchM4RunStartup` / `batchM4CaptureStartup`，不另起一套。
package db

import (
	"database/sql"
	"errors"
	"strings"
	"testing"

	"hivemtk-user/internal/model"

	"gorm.io/gorm"
)

// 钩子里那几句日志的**字面**文本（同批M-4/M-5 的口径：改文案必须让腿红一次，
// 否则"运维从启动日志里看得见什么"这条判据就是空的）。
const (
	batchM6LogReady      = "post-migrate: opportunities 非空 clue_id 唯一索引已就绪（手工商机的空 clue_id 不受约束）"
	batchM6LogCreateFail = "CREATE idx_opportunities_clue_id 失败"
	batchM6LogManualFix  = "需人工清重"
	batchM6IndexName     = "idx_opportunities_clue_id"
	batchM6LegacyClue    = "clue_batchm6_legacy"
)

// batchM6OppIndex 直读系统目录拿那道索引的**形状**：唯一性 + 谓词原文。
//
// 为什么读 `pg_get_expr(indpred)` 而不是 `pg_indexes.indexdef` 整句：indexdef 带 schema 名与
// `USING btree`，换 schema/换访问路径方法都会红，而那些都不是判据。谓词原文只用来核"键管在哪一列"，
// 谓词**语义**上的两种坏法（全列唯一 / 换成 `IS NOT NULL`）由 S18 的行为段去挡 ——
// 那两种形状在 indexdef 上都能读出"WHERE 这一句还在"。
func batchM6OppIndex(t *testing.T, gdb *gorm.DB) (unique, partial bool, pred string, exists bool) {
	t.Helper()
	row := gdb.Raw(`SELECT i.indisunique, (i.indpred IS NOT NULL),
			COALESCE(pg_get_expr(i.indpred, i.indrelid), '')
		FROM pg_index i
		JOIN pg_class ic ON ic.oid = i.indexrelid
		JOIN pg_class tc ON tc.oid = i.indrelid
		JOIN pg_namespace n ON n.oid = tc.relnamespace
		WHERE tc.relname = 'opportunities' AND ic.relname = ? AND n.nspname = current_schema()`,
		batchM6IndexName).Row()
	err := row.Scan(&unique, &partial, &pred)
	if errors.Is(err, sql.ErrNoRows) {
		return false, false, "", false
	}
	if err != nil {
		t.Fatalf("读取 %s 形状失败: %v", batchM6IndexName, err)
	}
	return unique, partial, pred, true
}

// batchM6InsertOpp 走 GORM 的 `Create`（= 生产写口 `repository/opportunity.go:123 Insert` 的形状），
// 而不是裸 SQL。裸 SQL 省掉 clue_id 会落成 NULL，而按 struct 写会把**空串显式写进 INSERT**
// （本腿的 blankRows 那句 Fatal 就是量这一件事：两行必须真的都是 clue_id 空串）。
// 记一句可达性，别把这段读成"生产天天在写空串"：今天唯一的写口是线索转化，
// 而它把空 clueID 当场拒了（`service/opportunity_convert.go:161-162`），路由里也没有 POST /opportunity
// 手工建单口 ⇒ 空串这一档今天只由旁路写入（裸 SQL／别家服务直连）落出来，
// 而那恰恰是谓词存在的理由：库级契约不能只按"当前有没有人这么写"来设。
func batchM6InsertOpp(gdb *gorm.DB, id, code, clueID string) error {
	return gdb.Create(&model.Opportunity{
		ID: id, Code: code, ClueID: clueID,
		Stage:  model.OpportunityStageQualification,
		Status: model.OpportunityStatusOpen,
	}).Error
}

// batchM6ResetOppTable 把 opportunities 抹回"这张表还不存在"（= 全新部署前的库）。
// 前置钉成 Fatal 而不是"顺手删一下"：槽位库在同一台 PG 上复用，上一趟留下的那道索引
// 会让这一趟的钩子直接撞 `IF NOT EXISTS` 空转 ⇒ "启动建出了索引"就成了别人残留的功劳。
func batchM6ResetOppTable(t *testing.T, gdb *gorm.DB) {
	t.Helper()
	if err := gdb.Migrator().DropTable(&model.Opportunity{}); err != nil {
		t.Fatalf("清表失败: %v", err)
	}
	if gdb.Migrator().HasTable(&model.Opportunity{}) {
		t.Fatal("前置不成立：opportunities 清不掉，本腿会把上一趟的残留当本趟启动路径的成果")
	}
}

// batchM6Cleanup 把本批两腿留在槽位库里的痕迹清干净，并把索引摆回"启动路径正常收口"的形状。
//
// 顺序是判据的一部分：**先删行、再补索引**。反过来的话，留着重复 clue_id 去跑钩子只会又走一遍
// 失败分支，而 `opportunity_migration_test.go` 那几条腿（按文件名排在本案后面）随后建索引时
// 会撞在我留下的重复行上 ⇒ 我的残留变成它的红因。
//
// 钩子调用包了 recover：万一将来有人把失败分支改成 panic（正是格 M75 打的那一刀），
// 收尾不该把整个测试二进制带走 —— 那会让电池看到"红的不是指定腿"。
func batchM6Cleanup(t *testing.T, gdb *gorm.DB) {
	t.Helper()
	if err := gdb.Exec(`DELETE FROM opportunities WHERE id LIKE 'batchm6-%'`).Error; err != nil {
		t.Errorf("收尾清理本批行失败: %v", err)
	}
	if !gdb.Migrator().HasTable(&model.Opportunity{}) {
		if err := gdb.AutoMigrate(&model.Opportunity{}); err != nil {
			t.Errorf("收尾重建 opportunities 失败（会把『表不存在』留给后面的腿）: %v", err)
		}
	}
	if _, _, _, ok := batchM6OppIndex(t, gdb); !ok {
		func() {
			defer func() {
				if p := recover(); p != nil {
					t.Errorf("收尾补索引时 panic: %v", p)
				}
			}()
			postMigrateOpportunityClueUniqueIndex(gdb)
		}()
	}
}

// S18：全新部署跑两遍真启动 ⇒ 表从 0 建出来、那道 partial 唯一索引随启动就位、
// 就位之后真的"挡得住同线索第二条、放行两条手工商机"，而第二次启动一句失败分支都不许有。
func TestBatchM_StartupBuildsOpportunityClueGuard(t *testing.T) {
	gdb := batchM4StartupDB(t)
	batchM6ResetOppTable(t, gdb)
	t.Cleanup(func() { batchM6Cleanup(t, gdb) })

	log1, panicked := batchM4CaptureStartup(t)
	if panicked != nil {
		t.Fatalf("第一次启动 AutoMigrate panic: %v", panicked)
	}
	if !gdb.Migrator().HasTable(&model.Opportunity{}) {
		t.Fatal("启动路径没建出 opportunities 表")
	}
	// 这两句日志判据合起来才是"挂在能干活的位置上"：只读『已就绪』挡不住"挪到建表之前"
	//（那句会走失败分支 ⇒ 前一句红），只读"没有失败"挡不住"整段没跑"（后一句红）。
	if strings.Contains(log1, batchM6LogCreateFail) {
		t.Errorf("全新部署第一次启动就报建索引失败（这里没有存量重复，红因只可能是钩子建在了表还不存在的时候、或 DDL 本身变了）:\n%s", log1)
	}
	if !strings.Contains(log1, batchM6LogReady) {
		t.Errorf("第一次启动的日志里读不到那句『已就绪』⇒ 钩子没挂在真实启动路径上（migrate.go 里那句调用没了，或跑在它前面的哪一步把这条吞了）:\n%s", log1)
	}

	unique, partial, pred, exists := batchM6OppIndex(t, gdb)
	if !exists {
		t.Fatal("启动路径跑完之后 idx_opportunities_clue_id 不在场：这条钩子在启动路径上一次都没成功过")
	}
	if !unique {
		t.Errorf("启动建出的索引不是唯一索引（indisunique=false）：谓词 %q ⇒ 幂等只剩 service 那一层", pred)
	}
	if !partial {
		t.Errorf("启动建出的索引不带 WHERE 谓词（indpred 为空）⇒ 第二条手工商机（clue_id 为空串）会被拦在门外，那比没索引更坏")
	}
	if !strings.Contains(pred, "clue_id") {
		t.Errorf("谓词里读不到 clue_id（键管到了别的列上）: %q", pred)
	}

	// 行为面：谓词存在的理由 —— 两条没有来源线索的手工商机必须都落得进去。
	if err := batchM6InsertOpp(gdb, "batchm6-manual-1", "OPP-BM6-M1", ""); err != nil {
		t.Errorf("第一条手工商机落不进去: %v", err)
	}
	blankErr2 := batchM6InsertOpp(gdb, "batchm6-manual-2", "OPP-BM6-M2", "")
	if blankErr2 != nil {
		t.Errorf("第二条手工商机被挡: %v ⇒ 启动建出的是全列唯一索引（谓词没了或换成了 IS NOT NULL），手工建的商机第二条起再也建不出来", blankErr2)
	}
	// 这一句判的是**夹具**：GORM 若把空串省成 NULL，上面那句"没被挡"就只是在测 NULL 不参与索引，
	// 而不是在测谓词 —— 所以它必须红在"两行真的都落了空串"上，而不是留给读者推断。
	var blankRows int64
	if err := gdb.Raw(`SELECT COUNT(*) FROM opportunities WHERE id LIKE 'batchm6-manual-%' AND clue_id = ''`).
		Row().Scan(&blankRows); err != nil {
		t.Fatalf("统计手工商机行失败: %v", err)
	}
	if blankRows != 2 {
		// 红因分流（格 M73/M74 实测踩过）：少的那一行有两种成因，读法完全不同 ——
		// 一种是"INSERT 被索引挡了"（谓词坏了，本腿的判据正面命中），另一种才是"GORM 把空串省成 NULL"
		// （夹具不成立，本腿关于谓词的结论作废）。不分流的话，谓词被改掉的那一刀会报成"去查 GORM"。
		if blankErr2 != nil {
			t.Fatalf("两行手工商机里只落了 %d 行 clue_id = ''，而第二条的 INSERT 当场就失败了（%v）"+
				"⇒ 少的那一行是被唯一索引挡掉的（谓词坏了），不是夹具省列；本腿的判据命中，按上面那条 Errorf 归因", blankRows, blankErr2)
		}
		t.Fatalf("前置不成立：两条手工商机里只有 %d 行真的落了 clue_id = ''，而两条 INSERT 都没报错"+
			"⇒ 落库的键不是空串（被省成 NULL？），本腿关于谓词的那句结论不成立", blankRows)
	}

	// 键真的挡：同一线索第二条必须撞在这道索引上，而不同线索不受牵连。
	if err := batchM6InsertOpp(gdb, "batchm6-conv-1", "OPP-BM6-C1", "clue_batchm6_a"); err != nil {
		t.Fatalf("先插一条合法行失败: %v", err)
	}
	dup := batchM6InsertOpp(gdb, "batchm6-conv-2", "OPP-BM6-C2", "clue_batchm6_a")
	switch {
	case dup == nil:
		t.Error("同一线索的第二行商机插进去了 ⇒ 启动建出的那道索引不挡重")
	case !strings.Contains(dup.Error(), batchM6IndexName):
		t.Errorf("同线索第二条被挡了，但挡它的不是那道索引（换个约束名说明索引形状变了）: %v", dup)
	}
	if err := batchM6InsertOpp(gdb, "batchm6-conv-3", "OPP-BM6-C3", "clue_batchm6_b"); err != nil {
		t.Errorf("换一条线索就被误拦: %v", err)
	}

	// 第二次启动：索引已在 ⇒ `IF NOT EXISTS` 让整句空转，既不报错也仍报"已就绪"。
	// 这一句是格 M72 的落点 —— 摘掉 IF NOT EXISTS 之后本腿必红，而状态面（索引还在、还挡得住）全绿。
	log2, panicked2 := batchM4CaptureStartup(t)
	if panicked2 != nil {
		t.Fatalf("第二次启动 AutoMigrate panic: %v", panicked2)
	}
	if strings.Contains(log2, batchM6LogCreateFail) {
		t.Errorf("第二次启动报建索引失败 ⇒ 守卫不幂等；而钩子里失败只落 logger.Warn，长期没人看启动日志它就静默失效:\n%s", log2)
	}
	if !strings.Contains(log2, batchM6LogReady) {
		t.Errorf("第二次启动不再报『已就绪』：重跑时走到了失败分支:\n%s", log2)
	}
	if _, _, _, ok := batchM6OppIndex(t, gdb); !ok {
		t.Error("第二次启动之后索引不见了")
	}
	if err := batchM6InsertOpp(gdb, "batchm6-conv-4", "OPP-BM6-C4", "clue_batchm6_a"); err == nil {
		t.Error("重跑一次启动之后同线索第二条又插得进去了：守卫没活过重跑")
	}
}

// S19：存量里真有重复的库跑一次真启动 ⇒ 钩子**只报不改**：索引不在场、Warn 点名人工清重、
// 两行重复原样留着、库级兜底确实没铺上（第三条同线索仍可插）。
//
// 这一段是 §23.12 第 9 段第 ① 条里"今天只做了一半"的那一半：旧腿只断"没 panic + 表没被清空"，
// 而"不自动改写"这件事要的是**索引不在场**这一句 —— 因为唯一能让索引在场的自动化做法
// 就是先把重复行删掉，那正是不能自动做的决定。
func TestBatchM_StartupDoesNotAutoRewriteLegacyDuplicateOpportunities(t *testing.T) {
	gdb := batchM4StartupDB(t)
	batchM6ResetOppTable(t, gdb)
	t.Cleanup(func() { batchM6Cleanup(t, gdb) })

	// 先跑一遍真启动把表建出来，再把它摆成"钩子那次上线之前的库"：表在、索引不在、存量有重复。
	if p := batchM4RunStartup(t); p != nil {
		t.Fatalf("铺形阶段启动 panic: %v", p)
	}
	if err := gdb.Exec(`DROP INDEX IF EXISTS ` + batchM6IndexName).Error; err != nil {
		t.Fatalf("临时删索引失败: %v", err)
	}
	for _, row := range []struct{ id, code string }{
		{"batchm6-legacy-a", "OPP-BM6-L1"},
		{"batchm6-legacy-b", "OPP-BM6-L2"},
	} {
		if err := batchM6InsertOpp(gdb, row.id, row.code, batchM6LegacyClue); err != nil {
			t.Fatalf("造存量重复行 %s 失败: %v", row.id, err)
		}
	}
	// 两句前置 Fatal 判的都是夹具：缺任一句，下面的状态断言就等于对空气。
	if _, _, _, ok := batchM6OppIndex(t, gdb); ok {
		t.Fatal("前置不成立：索引还在场 ⇒ 钩子那句 CREATE 会撞 IF NOT EXISTS 空转，本腿的『建不成只 Warn』这一支根本不会被走到")
	}
	var legacyRows int64
	if err := gdb.Raw(`SELECT COUNT(*) FROM opportunities WHERE clue_id = ?`, batchM6LegacyClue).
		Row().Scan(&legacyRows); err != nil {
		t.Fatalf("统计存量重复行失败: %v", err)
	}
	if legacyRows != 2 {
		t.Fatalf("前置不成立：同一 clue_id 的存量行有 %d 条，期望 2 ⇒ 重复没铺出来，建索引会当场成功", legacyRows)
	}

	log, panicked := batchM4CaptureStartup(t)
	if panicked != nil {
		t.Errorf("存量重复把启动弄成了 panic ⇒ 一次历史旁路写入让整个服务起不来；钩子承诺的是 Warn 后 return: %v", panicked)
	}
	if !strings.Contains(log, batchM6LogCreateFail) {
		t.Errorf("启动日志里没有建索引失败那一句 ⇒ 要么重复行没挡住 CREATE（谓词/形状变了），要么失败不再报出来:\n%s", log)
	}
	if !strings.Contains(log, batchM6LogManualFix) {
		t.Errorf("失败分支没点名『人工清重』（这条 Warn 的全部作用是告诉运维下一步该谁动手，改成『可忽略』就会没人管）:\n%s", log)
	}
	if strings.Contains(log, batchM6LogReady) {
		t.Errorf("带重复存量还报了『已就绪』⇒ 日志在说反话，运维据此会以为第二层已经铺上:\n%s", log)
	}

	// 状态面：这三段钉的是"没自己动手"，与上面三段日志各自独立（改文案红日志、改行为红这里）。
	if _, _, pred, ok := batchM6OppIndex(t, gdb); ok {
		t.Errorf("启动把 %s 建出来了（谓词 %q）⇒ 唯一能自动化建成的办法是先删掉重复行，而那是要人判断的数据改写", batchM6IndexName, pred)
	}
	var codes []string
	if err := gdb.Raw(`SELECT code FROM opportunities WHERE clue_id = ? ORDER BY code`, batchM6LegacyClue).
		Scan(&codes).Error; err != nil {
		t.Fatalf("读回存量重复行失败: %v", err)
	}
	if strings.Join(codes, ",") != "OPP-BM6-L1,OPP-BM6-L2" {
		t.Errorf("存量重复行 = %v，期望两行原样都在（OPP-BM6-L1,OPP-BM6-L2）：清掉任意一条都是替业务决定了哪个商机才是真身", codes)
	}
	if err := batchM6InsertOpp(gdb, "batchm6-legacy-c", "OPP-BM6-L3", batchM6LegacyClue); err != nil {
		t.Errorf("第三条同线索商机反而插不进（%v）⇒ 索引不在场却有东西在挡重，本腿读到的形状自相矛盾", err)
	}
	// 收尾把这一条再插一次前先删掉：它只为证明"没兜底"，不该留在库里影响后面的腿。
	if err := gdb.Exec(`DELETE FROM opportunities WHERE id = 'batchm6-legacy-c'`).Error; err != nil {
		t.Errorf("删掉探针行失败: %v", err)
	}
}
