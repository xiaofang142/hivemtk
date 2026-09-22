// message_hub_unique_batchm5_test.go 把 §23.8 第 8 条从"这句话没人读"变成"这句话有腿"：
// `AutoMigrate()` 末尾三条 post-migrate 守卫里，obs 那条由批M/批M-2/批M-4 接到了真实启动路径上，
// clue_id 那条有 `opportunity_migration_test.go` 的四处直调，只有 message_hub 这一条今天
// 全仓只有两处命中（`migrate.go` 的定义 + 调用）⇒ 零测试面。
//
// 先说清这条钩子**三句里哪一句承重**，因为它和 obs 那条形状不同、判据也就不同。
// 三句逐刀量过，结论都落在格号上（不是读码推演）：
//
//  1. `CREATE UNIQUE INDEX IF NOT EXISTS …` —— 建索引这一句**不承重**。三元组同时写在
//     `model/ai_sales_champion.go` 的 uniqueIndex 标签里（priority 1/2/3），而按标签建缺失索引
//     跑在钩子**之前** ⇒ 这句永远撞在一个已经在场的同名索引上。改它的列清单（格 M63）
//     没有任何可观察差异 ⇒ 登记为等价格；把**标签**那一列摘掉（格 M64）才红 ⇒ 形状的事实源是标签。
//     它还剩下的作用只有那行"已就绪"日志（格 M66 摘掉 `IF NOT EXISTS` 由 S17 的日志断言杀掉）。
//  2. `ALTER TABLE … DROP CONSTRAINT IF EXISTS uni_message_hub_msg_id` —— 删单列唯一约束**也不承重**，
//     但原因不是"GORM 只加不删"（那句话是错的，本批当场改掉）：GORM v1.30.0 的
//     `Migrator.MigrateColumnUnique`（`gorm@v1.30.0/migrator/migrator.go:580-598`）每次列迁移都按
//     命名约定算出 `uni_<表>_<列>`，在"列上报唯一、字段标签不唯一"时替它 DROP。而库里 `Unique()`
//     只由**单列 UNIQUE 约束**供给（`driver/postgres@v1.6.0/migrator.go:541-579`，
//     `if uniqueContraints[constraintName] == 1`）⇒ 这一类恰好落在 GORM 的对账半径里。
//     这个约束本身就是历史标签留下的（初始提交 `e2829727` 里是 `MsgID … gorm:"…;unique"`），
//     名字因此与 GORM 的约定同源 ⇒ 钩子这句是**双保险**：摘掉它没有腿会红（格 M61，实测存活 ⇒ 等价格）。
//  3. `DROP INDEX IF EXISTS uni_message_hub_msg_id_conv` —— **唯一承重的半截**。它是上一版
//     `uniqueIndex:uni_message_hub_msg_id_conv` 标签留下的**裸索引**（不是约束），
//     既不在上面那条查询的结果里、名字也不是 `uni_message_hub_msg_id` ⇒ GORM 一辈子不碰它。
//     摘掉这一句（格 M62）与摘掉整个钩子（格 M60）红在同一处：旧二元索引还在场、跨会话同 msg_id 撞键。
//
// 为什么这两道旧唯一性值得管：它们的键比三元组**窄**，窄出来的那部分正是本批的判据 ——
// 同一条 msg_id 换个渠道/换个会话再进来会被**当成重复消息吞掉**
// （与批F-4b-补那条"HubMsgID 跨账号撞唯一键致消息丢弃"同一族）。
//
// 于是 S16 不测"索引在不在"（那由标签决定，钩子不参与），测的是**老形状走一次真启动之后
// 旧的两道窄唯一性必须消失、且消失之后跨渠道/跨会话同 msg_id 落得进去、全同三元组仍被挡**。
// S17 测形状与幂等重跑：三元组索引的列清单与唯一性、第二次启动既不报错也不把旧约束又留下。
//
// 腿名前缀仍是 `TestBatchM_`（与 §23.10/§23.11 同一口径：电池的 `-run TestBatchM_` 是子串匹配，
// 换前缀等于新腿不进电池），夹具复用批M-4 的 `batchM4StartupDB` / `batchM4RunStartup` /
// `batchM4CaptureStartup`，不另起一套。

package db

import (
	"database/sql"
	"errors"
	"strings"
	"testing"

	"hivemtk-user/internal/model"

	"gorm.io/gorm"
)

// 钩子里那几句日志的**字面**文本。抄字面是有意的（同批M-4）：改文案必须让腿红一次，
// 否则"运维从启动日志里看得见什么"这条判据就是空的。
const (
	batchM5LogHubReady   = "post-migrate: message_hub (platform, msg_id, conversation_id) 三元组唯一索引已就绪"
	batchM5LogDropFailed = "DROP 旧 uni_message_hub_msg_id"
	batchM5LogCreateFail = "CREATE uni_message_hub_platform_msg_conv 失败"
	batchM5HubIndexName  = "uni_message_hub_platform_msg_conv"
	batchM5LegacyConst   = "uni_message_hub_msg_id"
	batchM5LegacyConvIdx = "uni_message_hub_msg_id_conv"
	batchM5HubMsgID      = "batchm5-cross-channel"
)

// batchM5IndexShape 直读系统目录拿索引的**形状**：唯一性、是否 partial、以及列的**顺序**。
//
// 为什么不查 `pg_indexes.indexdef` 那一整句文本：它带 schema 名与 `USING btree`，
// 换 schema/换访问路径方法都会红，而那些都不是判据。列序单独用 `unnest(indkey) WITH ORDINALITY`
// 取，才是"这道索引到底按哪几列判重"的原话。
func batchM5IndexShape(t *testing.T, gdb *gorm.DB, indexName string) (unique, partial bool, cols string, exists bool) {
	t.Helper()
	row := gdb.Raw(`SELECT i.indisunique, (i.indpred IS NOT NULL),
			(SELECT string_agg(a.attname, ',' ORDER BY p.ord)
			   FROM unnest(i.indkey) WITH ORDINALITY AS p(k, ord)
			   JOIN pg_attribute a ON a.attrelid = i.indrelid AND a.attnum::int = p.k::int)
		FROM pg_index i
		JOIN pg_class ic ON ic.oid = i.indexrelid
		JOIN pg_class tc ON tc.oid = i.indrelid
		JOIN pg_namespace n ON n.oid = tc.relnamespace
		WHERE tc.relname = 'message_hub' AND ic.relname = ? AND n.nspname = current_schema()`,
		indexName).Row()
	err := row.Scan(&unique, &partial, &cols)
	if errors.Is(err, sql.ErrNoRows) {
		return false, false, "", false
	}
	if err != nil {
		t.Fatalf("读取索引 %s 的形状失败: %v", indexName, err)
	}
	return unique, partial, cols, true
}

// batchM5ConstraintGone 读的是**约束**目录而不是索引目录：`ADD CONSTRAINT … UNIQUE` 与
// `CREATE UNIQUE INDEX` 在 PG 里是两种存法（前者是 pg_constraint 里的一行 + 它自带的支撑索引）。
// 钩子那句 `DROP CONSTRAINT IF EXISTS` 只能清前一种，所以判据也必须读前一种 ——
// 拿索引名判"旧约束没了"会读错对象（旧约束的支撑索引名恰好与约束同名，但反过来不成立）。
func batchM5ConstraintPresent(t *testing.T, gdb *gorm.DB, name string) bool {
	t.Helper()
	var n int
	if err := gdb.Raw(`SELECT count(*) FROM pg_constraint c
		JOIN pg_class tc ON tc.oid = conrelid
		JOIN pg_namespace ns ON ns.oid = tc.relnamespace
		WHERE tc.relname = 'message_hub' AND c.conname = ? AND ns.nspname = current_schema()`, name).
		Row().Scan(&n); err != nil {
		t.Fatalf("读取约束 %s 失败: %v", name, err)
	}
	if n > 1 {
		t.Fatalf("约束 %s 在库里出现 %d 次，本腿的前置需要重看", name, n)
	}
	return n == 1
}

// batchM5DropLegacy 把本腿自己铺出来的两道旧唯一性抹掉。
// 注册成 cleanup 而不是只在成功路径上调：本腿有一半的断言是 `t.Fatal`，
// 挂在中间把 `UNIQUE (msg_id)` 留给同一个 slot 库的话，后面任何一条往 message_hub
// 插同 msg_id 的腿都会撞在我留下的键上（同进程共享库，见 testutil 的槽位分配）。
func batchM5DropLegacy(t *testing.T, gdb *gorm.DB) {
	t.Helper()
	if err := gdb.Exec(`ALTER TABLE message_hub DROP CONSTRAINT IF EXISTS ` + batchM5LegacyConst).Error; err != nil {
		t.Errorf("收尾 DROP 旧约束失败（会把形状留给同槽位的其它腿）: %v", err)
	}
	if err := gdb.Exec(`DROP INDEX IF EXISTS ` + batchM5LegacyConvIdx).Error; err != nil {
		t.Errorf("收尾 DROP 旧索引失败: %v", err)
	}
	// 本腿自己插的行一并带走（物理删，理由见 S16 第三段：不留软删标记占着三元组键）。
	if err := gdb.Unscoped().Where("account_id = ?", "batchm5-acct").
		Delete(&model.MessageHub{}).Error; err != nil {
		t.Errorf("收尾清理本腿行失败: %v", err)
	}
}

// batchM5HubRow 造一条能过 NOT NULL 的 message_hub 行（platform/account_id/direction/msg_type 必填）。
func batchM5HubRow(platform, msgID, conv string) *model.MessageHub {
	return &model.MessageHub{
		Platform:       platform,
		MsgID:          msgID,
		AccountID:      "batchm5-acct",
		Direction:      "inbound",
		MsgType:        "text",
		ConversationID: conv,
	}
}

// batchM5ResetHubTable 把 message_hub 抹回"这张表还不存在"，好让**本趟进程**的迁移
// （标签 + 钩子）成为它形状的唯一来源。
//
// 为什么必须抹（批M-4 的 S11 对 obs_config 做同一件事）：槽位库在同一台 PG 上是复用的，
// 上一趟留下的三元组索引会让这一趟的 `AutoMigrate` 直接跳过建索引 ——
// 于是"标签改坏了"那一刀（格 M64）会打在别人的残留上、静默存活。
func batchM5ResetHubTable(t *testing.T, gdb *gorm.DB) {
	t.Helper()
	if err := gdb.Migrator().DropTable(&model.MessageHub{}); err != nil {
		t.Fatalf("清表失败: %v", err)
	}
	if gdb.Migrator().HasTable(&model.MessageHub{}) {
		t.Fatal("前置不成立：message_hub 清不掉，本腿会把上一趟的残留当本趟迁移的成果")
	}
}

// S16：带旧形状的库存量部署走一次真启动 ⇒ 两道窄唯一性都被清掉，
// 清掉之后"同 msg_id 换渠道/换会话"落得进去，而三元组全同仍然进不来。
func TestBatchM_StartupClearsLegacyMessageHubNarrowUniqueness(t *testing.T) {
	gdb := batchM4StartupDB(t)
	batchM5ResetHubTable(t, gdb)
	// 先跑一趟真启动把表建出来（`testutil.NewTestDB` 只 AutoMigrate 它自己那组模型，
	// message_hub 不在里面 —— 批M-4 的 S11 就是因为这件事才在腿内自己跑本体）。
	// 用本体而不是 `Migrator().CreateTable`：本腿要的形状是"生产路径建出来的表"，
	// 抄近路就等于把"钩子跑在什么之上"换成测试自己造的东西。
	if p := batchM4RunStartup(t); p != nil {
		t.Fatalf("铺底迁移 panic: %v（本腿还没有任何判据，先修这一件）", p)
	}
	if !gdb.Migrator().HasTable(&model.MessageHub{}) {
		t.Fatal("前置不成立：真启动没建出 message_hub 表，本腿的『老部署』前提无从铺起")
	}
	t.Cleanup(func() { batchM5DropLegacy(t, gdb) })

	// 起点必须空：表刚由上面那趟迁移从 0 建出来，若里面有行，那是别人留下的，
	// 而 `ADD CONSTRAINT … UNIQUE (msg_id)` 会因他们的重复直接失败 —— 红因就跟钩子无关了。
	// 这里选 Fatal 而不是"顺手 TRUNCATE 一下"：清掉别人的行来让自己的前置成立，等于把污染藏起来。
	var hubRows int64
	if err := gdb.Model(&model.MessageHub{}).Count(&hubRows).Error; err != nil {
		t.Fatalf("数 message_hub 行数失败: %v", err)
	}
	if hubRows != 0 {
		t.Fatalf("前置不成立：刚建出来的 message_hub 里有 %d 行，本腿的旧约束铺不上（且铺上了也测不到钩子）", hubRows)
	}

	// 铺"老部署"形状：单列唯一约束 + 旧的 (msg_id, conversation_id) 二元唯一索引。
	// 两者都不是想象出来的形状：这两个名字正是钩子里那两句 DROP 点名的对象，而"一个铺成约束、
	// 一个铺成索引"也照抄了历史（前者来自初始提交的 `gorm:"…;unique"`，后者来自上一版
	// `uniqueIndex:uni_message_hub_msg_id_conv` 标签）。这个区分不是洁癖 —— GORM 的列级对账
	// 只认"单列唯一约束"那一类（见文件头第 2 点），所以两句 DROP 里只有第二句在替 GORM 补洞。
	if err := gdb.Exec(`ALTER TABLE message_hub ADD CONSTRAINT ` + batchM5LegacyConst +
		` UNIQUE (msg_id)`).Error; err != nil {
		t.Fatalf("铺旧单列唯一约束失败: %v", err)
	}
	if err := gdb.Exec(`CREATE UNIQUE INDEX ` + batchM5LegacyConvIdx +
		` ON message_hub (msg_id, conversation_id)`).Error; err != nil {
		t.Fatalf("铺旧二元唯一索引失败: %v", err)
	}
	// 前置钉成 Fatal：旧形状没铺上的话，"启动把它清掉了"这句话就是在断一件从未成立的事。
	// 这两句判的是**夹具**（形状铺上了没有），不是钩子 —— 钩子那一句做没做事后才知道。
	if !batchM5ConstraintPresent(t, gdb, batchM5LegacyConst) {
		t.Fatal("前置不成立：旧单列唯一约束没铺上，本腿后面的那条结果断言等于对空气")
	}
	if _, _, _, ok := batchM5IndexShape(t, gdb, batchM5LegacyConvIdx); !ok {
		t.Fatal("前置不成立：旧二元唯一索引没铺上，本腿等于没测钩子承重的第二句 DROP")
	}

	if p := batchM4RunStartup(t); p != nil {
		t.Fatalf("跑真启动迁移 panic: %v（下面的断言全在 panic 之后，先修这一件）", p)
	}

	// 第一段：两道旧形状都必须不在场。各占一句，红因才分得开是哪一道没清掉。
	// **前一句钉的是结果，不是钩子第一句 DROP**：单列唯一约束这条路上有两个人在删
	// （GORM 的列级对账 + 钩子那句 DROP），摘掉钩子那一句它照样绿（格 M61 ⇒ 等价格），
	// 要让这一句红得把两处一起摘掉。留着它的理由是那一句运维承诺本身（"老库升级完不再按
	// msg_id 单列判重"），以及 GORM 哪天改掉对账时由钩子兜底 —— 两个方向互为兜底，谁都不许删。
	if batchM5ConstraintPresent(t, gdb, batchM5LegacyConst) {
		t.Error("启动之后旧单列唯一约束 uni_message_hub_msg_id 还在：老库升级完仍然按 msg_id 单列判重" +
			" ⇒ 同一条消息在第二个渠道入库时被当重复吞掉。钩子那句 DROP 被摘掉不会走到这里" +
			"（GORM 自己删得掉，见文件头第 2 点）⇒ 真红到这一句时先查 gorm 版本/标签，再查钩子")
	}
	if _, _, cols, ok := batchM5IndexShape(t, gdb, batchM5LegacyConvIdx); ok {
		t.Errorf("启动之后旧二元唯一索引 uni_message_hub_msg_id_conv 还在（列 = %q）："+
			"它的键是 (msg_id, conversation_id)，同一 msg_id 在另一个会话的第二条照样撞键", cols)
	}

	// 第二段：把"清掉之后到底放行了什么"用入库口量一遍。
	if err := gdb.Create(batchM5HubRow("telegram", batchM5HubMsgID, "c1")).Error; err != nil {
		t.Fatalf("基准行插入失败: %v", err)
	}
	if err := gdb.Create(batchM5HubRow("qq", batchM5HubMsgID, "c1")).Error; err != nil {
		t.Errorf("同 msg_id、跨渠道的第二条被挡: %v —— 这条消息在生产里就是静静丢的"+
			"（入库口拿到唯一键冲突只会记一条日志，不会重试）", err)
	}
	if err := gdb.Create(batchM5HubRow("telegram", batchM5HubMsgID, "c2")).Error; err != nil {
		t.Errorf("同 msg_id、跨会话的第二条被挡: %v", err)
	}
	dup := gdb.Create(batchM5HubRow("telegram", batchM5HubMsgID, "c1"))
	switch {
	case dup.Error == nil:
		t.Error("三元组完全相同的第二条插进去了 ⇒ 『同一条消息只入库一次』在库级没有任何兜底")
	case !strings.Contains(dup.Error.Error(), batchM5HubIndexName):
		t.Errorf("全同三元组被挡了，但挡它的不是那道三元组索引（键名读不到 %q）: %v", batchM5HubIndexName, dup.Error)
	}

	// 第三段：死信丢弃之后，同一条消息要能重新入库。
	// 形状镜像 `repository/message_hub_inbox.go:133`（`Unscoped().Where("id = ?").Delete(&model.MessageHub{})`）
	// —— 全仓对 message_hub 的两个生产删除口都是**物理删**（另一个是 `csplus_ops_repo.go:78` 的
	// `Table("message_hub").Delete(nil)`，本批实测它走的也是 DELETE 而不是软删标记）。
	// 这一句在"N-35 若将来改成软删 + partial 索引"的世界里同样成立（见 §23.12 第 4 段），
	// 所以它钉的是可复用契约，不是待排口径的遮羞布。
	var seeded model.MessageHub
	if err := gdb.Where("msg_id = ? AND platform = ? AND conversation_id = ?", batchM5HubMsgID, "telegram", "c1").
		First(&seeded).Error; err != nil {
		t.Fatalf("读回基准行失败: %v", err)
	}
	if err := gdb.Unscoped().Where("id = ?", seeded.ID).Delete(&model.MessageHub{}).Error; err != nil {
		t.Fatalf("物理删基准行失败: %v", err)
	}
	if err := gdb.Create(batchM5HubRow("telegram", batchM5HubMsgID, "c1")).Error; err != nil {
		t.Errorf("物理删掉一行之后同三元组重投被挡: %v —— 丢弃过的死信再也回不了库", err)
	}
}

// S17：三元组索引的**形状**与第二次启动的静默。
//
// 形状断言读的是标签建出来的那道索引（S16 已证钩子的 CREATE 撞在同名索引上是空转），
// 所以这里钉的其实是"三元组 = 这三列、按这个顺序、且不带宽表谓词"。
// `partial` 那一句是**绊线**而不是契约：今天的形状确实没有 WHERE，而 §5 N-35 登记的
// "软删行永久占键"那条修法（改成 `WHERE deleted_at IS NULL`）一旦落地，这一句必须跟着翻成
// "必须是 partial" —— 红因里把两个世界都写出来，是为了让改的人**有意识地**改期望，
// 而不是看见红就删断言（同 §23.10 关于"别把待排口径钉成不可改契约"那条口径）。
func TestBatchM_StartupMessageHubIndexShapeAndSecondRunSilent(t *testing.T) {
	gdb := batchM4StartupDB(t)
	batchM5ResetHubTable(t, gdb)

	log1, p1 := batchM4CaptureStartup(t)
	if p1 != nil {
		t.Fatalf("第一次启动迁移 panic: %v", p1)
	}
	if !gdb.Migrator().HasTable(&model.MessageHub{}) {
		t.Fatal("前置不成立：第一次启动没建出 message_hub 表，后面的形状断言全是对空气")
	}
	if !strings.Contains(log1, batchM5LogHubReady) {
		t.Errorf("第一次启动的日志里读不到那句『已就绪』（%q）：钩子没跑、或跑到了但走的是失败分支，"+
			"而这条腿的其它断言读的是索引形状，分不开这两种情况", batchM5LogHubReady)
	}
	for _, frag := range []string{batchM5LogDropFailed, batchM5LogCreateFail} {
		if strings.Contains(log1, frag) {
			t.Errorf("第一次启动日志里出现了失败分支 %q", frag)
		}
	}

	unique, partial, cols, ok := batchM5IndexShape(t, gdb, batchM5HubIndexName)
	if !ok {
		t.Fatal("三元组唯一索引不在场：message_hub 的去重兜底整个不存在")
	}
	if !unique {
		t.Error("三元组索引不是唯一索引：去重兜底名存实亡")
	}
	if partial {
		t.Error("三元组索引带上了 WHERE 谓词：它不再约束全部行。两种可能都要认——" +
			"① 有人顺手加了个不合适的谓词（真回归）；② §5 N-35 的软删修法落地（那就是新契约，" +
			"本句要改成『必须是 partial 且谓词是 deleted_at IS NULL』，并连带重读 §23.12 第 4 段）")
	}
	if cols != "platform,msg_id,conversation_id" {
		t.Errorf("三元组索引的列 = %q，期望 platform,msg_id,conversation_id（键变了还叫同名，读日志的人看不出来）", cols)
	}

	log2, p2 := batchM4CaptureStartup(t)
	if p2 != nil {
		t.Fatalf("第二次启动迁移 panic: %v", p2)
	}
	for _, frag := range []string{batchM5LogDropFailed, batchM5LogCreateFail} {
		if strings.Contains(log2, frag) {
			t.Errorf("第二次启动日志里出现了失败分支 %q：这道守卫不幂等，"+
				"而钩子里所有失败都只落 logger.Warn ⇒ 长期没人看启动日志它就静默失效", frag)
		}
	}
	if !strings.Contains(log2, batchM5LogHubReady) {
		t.Errorf("第二次启动不再报『已就绪』：重跑时守卫走了失败分支")
	}
	if batchM5ConstraintPresent(t, gdb, batchM5LegacyConst) {
		t.Error("第二次启动之后旧单列唯一约束又出现在库里：清旧形状这件事只在首次生效")
	}
}
