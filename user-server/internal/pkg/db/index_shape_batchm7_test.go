// index_shape_batchm7_test.go 立 N-36：三条 post-migrate 守卫那句『已就绪』 today 只由
// `CREATE UNIQUE INDEX IF NOT EXISTS` 的 **err** 供给，而 PG 对"同名但非唯一"的历史索引
// 是一句**静默 no-op**（探针 J 实测：err=<nil>、indisunique=false、同键两行都落得进去）。
// 于是名字被占住的那一格，日志说的正是反话 —— 而这三条钩子存在的唯一理由（钩子注释原文）
// 就是"让『第二层没铺上』这件事在启动日志里看得见"。
//
// 探针 I 补了另一半：GORM 自己按名字对账（`driver/postgres@v1.6.0/migrator.go:109` HasIndex
// 只查 pg_indexes.indexname），所以标签那一层同样不会把它补回来 —— 名字在场 ≠ 形状在场，
// 这条与 `opportunity_clue_startup_batchm6_test.go` 里 S18 为什么读 `indisunique` 同源，
// 本批把同一个口径搬到钩子自己的**结论**上。
//
// 三条腿各钉一处钩子（同一符号被三处消费 ⇒ 逐处拆刀，见 §23.13 的口径）：
// S27 opportunities / S28 obs_config / S29 message_hub。
//
// 账上留一格本文件**判不到**的：helper 有两支红因（"名字下不是唯一索引"与"读形状这件事本身失败"），
// 两句都含同一句『不是唯一索引』+ 索引名 ⇒ 把 helper 里那句 SELECT 改坏成恒不返行、三条腿全绿。
// 那一格由批M-6 的 `TestBatchM_StartupBuildsOpportunityClueGuard`（正判"好库上必须有『已就绪』"）
// 与格 M81 判 ⇒ 本文件的口径是"钩子不许说谎"，不是"钩子不许沉默"，两句差别就登记在这儿。
//
// 这三条腿**只判钩子的结论，不判装配**：三条钩子挂在真启动路径上分别由批M-4 的 S11、
// 批M-6 的 S18、批M-5 的 S16 证过，这里直调是刻意选了便宜的那一头（表由 `AutoMigrate(单个模型)` 铺）。
package db

import (
	"database/sql"
	"errors"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/utils/logger"

	"gorm.io/gorm"
)

// 新契约文案的字面片段（同批M-4/M-5/M-6：抄字面是有意的，改文案必须让腿红一次）。
// 只钉两个要件 —— ① 点名是哪个索引，② 说清"不是唯一索引"。运维据此能直接决定下一步；
// 至于"名字被非唯一历史索引占住"的具体措辞，留给 Errorf 的说明文本去讲。
const batchM7LogNotUnique = "不是唯一索引"

// batchM7CaptureLogs 抓一段代码期间落到 stdout 的日志。
//
// 与 `batchM4CaptureStartup` 同一套接缝（`logger.stdout` 是**函数**，换 os.Stdout 之后必须再
// InitLogger 才生效；读端必须并发，否则管道写满把这条腿卡死），差别只在它跑的是传进来的闭包
// 而不是整趟启动 —— 本批这三条腿只调一个钩子，跑整趟等于把别人的日志混进判据面。
func batchM7CaptureLogs(t *testing.T, run func()) string {
	t.Helper()
	oldOut := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("管道创建失败: %v", err)
	}
	os.Stdout = w
	logger.InitLogger(logger.LoggingConfig{Level: "info", Format: "json", Output: "stdout"})

	done := make(chan string, 1)
	go func() {
		b, _ := io.ReadAll(r)
		done <- string(b)
	}()

	func() {
		defer func() {
			os.Stdout = oldOut
			logger.InitLogger(logger.DefaultConfig())
			_ = w.Close()
			if p := recover(); p != nil {
				t.Fatalf("钩子当场 panic（三条钩子的既有承诺是失败只 Warn 后 return）: %v", p)
			}
		}()
		run()
	}()

	log := <-done
	_ = r.Close()
	return log
}

// batchM7IndexShape 直读系统目录拿**任意一张表上任意一个索引名**的形状。
// 为什么不复用 batchM5IndexShape / batchM6OppIndex：那两条各自写死了表名（message_hub /
// opportunities），而本批要在一处比较三张表的同名对象。唯一性读 `pg_index.indisunique`
// 这一位 —— 它是"挡不挡重"的库级原话，名字与 indexdef 文本都不是。
func batchM7IndexShape(t *testing.T, gdb *gorm.DB, table, indexName string) (unique, partial bool, exists bool) {
	t.Helper()
	row := gdb.Raw(`SELECT i.indisunique, (i.indpred IS NOT NULL)
		FROM pg_index i
		JOIN pg_class ic ON ic.oid = i.indexrelid
		JOIN pg_class tc ON tc.oid = i.indrelid
		JOIN pg_namespace n ON n.oid = tc.relnamespace
		WHERE tc.relname = ? AND ic.relname = ? AND n.nspname = current_schema()`,
		table, indexName).Row()
	err := row.Scan(&unique, &partial)
	if errors.Is(err, sql.ErrNoRows) {
		return false, false, false
	}
	if err != nil {
		t.Fatalf("读取 %s.%s 形状失败: %v", table, indexName, err)
	}
	return unique, partial, true
}

// batchM7Squat 用**非唯一**索引占住钩子要点名的那个名字（= N-36 的现场），并当场核验铺上了。
//
// 先 DROP 再 CREATE：这三枚名字在生产里本来就该是唯一索引（由钩子或标签建出），
// 不先摘掉就占不住；而"摘掉之后确实换成了非唯一的那一枚"必须 Fatal 钉住 ——
// 前置没成立时，后面那句"日志不许说已就绪"就等于对空气判。
func batchM7Squat(t *testing.T, gdb *gorm.DB, table, indexName, columns string) {
	t.Helper()
	if err := gdb.Exec(`DROP INDEX IF EXISTS ` + indexName).Error; err != nil {
		t.Fatalf("摘掉原名下的既有索引失败: %v", err)
	}
	if err := gdb.Exec(`CREATE INDEX ` + indexName + ` ON ` + table + ` (` + columns + `)`).Error; err != nil {
		t.Fatalf("铺非唯一占位索引失败: %v", err)
	}
	unique, _, ok := batchM7IndexShape(t, gdb, table, indexName)
	if !ok {
		t.Fatalf("前置不成立：占位索引 %s 根本不在场，钩子那句 CREATE 会当场真的建成唯一索引，本腿走不到 N-36 那一格", indexName)
	}
	if unique {
		t.Fatalf("前置不成立：占位索引 %s 报的是唯一 ⇒ 非唯一形状没铺出来，本腿的判据无从命中", indexName)
	}
}

// batchM7Restore 收尾：删掉占位索引，再跑一遍钩子把真形状补回来。
//
// 槽位库在同进程共享（见 testutil 的咨询锁槽位），把一枚**非唯一**的 `idx_opportunities_clue_id`
// 留给后面的腿，等于让批M-6 那几条腿撞在我留下的"名字在、约束不在"上 ——
// 那正是本批要修的缺陷自己造出来的形状。
func batchM7Restore(t *testing.T, gdb *gorm.DB, table, indexName string, rerun func()) {
	t.Helper()
	if err := gdb.Exec(`DROP INDEX IF EXISTS ` + indexName).Error; err != nil {
		t.Errorf("收尾删占位索引失败: %v", err)
	}
	func() {
		defer func() {
			if p := recover(); p != nil {
				t.Errorf("收尾重跑钩子 panic: %v", p)
			}
		}()
		rerun()
	}()
	if unique, _, ok := batchM7IndexShape(t, gdb, table, indexName); !ok || !unique {
		t.Errorf("收尾之后 %s 仍不是唯一索引（在场=%v 唯一=%v）⇒ 本腿把坏形状留给了同槽位的其它腿", indexName, ok, unique)
	}
}

// batchM7InsertObs 用与批M `batchMInsertObs` 同一份行形状插一行 obs_config，
// 差别只在**把错误还给调用方**（理由见 S28 里那段注释）。
func batchM7InsertObs(gdb *gorm.DB, name string, isDefault bool, at time.Time) error {
	return gdb.Create(&model.ObsConfig{
		Name: name, Provider: model.ObsProviderLocal,
		AccessKey: "ak", SecretKey: "sk", Bucket: "b",
		Status: model.ObsStatusActive, MaxSize: 1 << 20, MaxCount: 10,
		IsDefault: isDefault, CreatedAt: at,
	}).Error
}

// batchM7HubRow 造一条能过 NOT NULL 的 message_hub 行，形状同 `batchM5HubRow`，
// 但 account_id 用本批自己的键（理由见 S29 里那段注释）。
func batchM7HubRow(msgID, conv string) *model.MessageHub {
	return &model.MessageHub{
		Platform:       "batchm7",
		MsgID:          msgID,
		AccountID:      "batchm7-acct",
		Direction:      "inbound",
		MsgType:        "text",
		ConversationID: conv,
	}
}

// S27：opportunities 那道 clue_id 索引的名字被非唯一索引占住 ⇒ 钩子不许报『已就绪』，
// 必须点名"不是唯一索引"，并且**不许自己动手删别人的索引**（形状仍是非唯一、同键两行仍落得进）。
func TestBatchM_OpportunityGuardDoesNotClaimReadyWhenNameIsNotUnique(t *testing.T) {
	gdb := batchM4StartupDB(t)
	const (
		table   = "opportunities"
		indexNA = batchM6IndexName
	)
	if err := gdb.AutoMigrate(&model.Opportunity{}); err != nil {
		t.Fatalf("铺 opportunities 表失败: %v", err)
	}
	t.Cleanup(func() {
		if err := gdb.Exec(`DELETE FROM opportunities WHERE id LIKE 'batchm7-%'`).Error; err != nil {
			t.Errorf("收尾清理本腿行失败: %v", err)
		}
		batchM7Restore(t, gdb, table, indexNA, func() { postMigrateOpportunityClueUniqueIndex(gdb) })
	})
	batchM7Squat(t, gdb, table, indexNA, "clue_id")

	log := batchM7CaptureLogs(t, func() { postMigrateOpportunityClueUniqueIndex(gdb) })
	if strings.Contains(log, batchM6LogReady) {
		t.Errorf("名字被非唯一索引占住还报『已就绪』⇒ 日志在说反话，运维据此以为同线索转化的库级兜底已经铺上:\n%s", log)
	}
	if !strings.Contains(log, batchM7LogNotUnique) {
		t.Errorf("启动日志里没有『%s』那一句 ⇒ N-36 没人报出来（钩子只核 err 不核形状，正是这一格）:\n%s", batchM7LogNotUnique, log)
	}
	if !strings.Contains(log, indexNA) {
		t.Errorf("失败分支没点名是哪枚索引（三条钩子同族、文案相近，不点名运维不知道该去 DROP 谁）:\n%s", log)
	}

	// 状态面两件事：① 钩子不许"顺手"把别人的索引删了重造（那是启动路径上的静默结构改写）；
	// ② 兜底确实没铺上 —— 同 clue_id 两行都落得进去。
	unique, _, ok := batchM7IndexShape(t, gdb, table, indexNA)
	if !ok || unique {
		t.Errorf("钩子跑完之后形状变成 在场=%v 唯一=%v ⇒ 它自己动手换掉了库里那枚索引；本钩子的既有口径是只报不改", ok, unique)
	}
	if err := batchM6InsertOpp(gdb, "batchm7-opp-1", "OPP-BM7-1", "clue_batchm7_dup"); err != nil {
		t.Fatalf("先放一条商机失败: %v", err)
	}
	if err := batchM6InsertOpp(gdb, "batchm7-opp-2", "OPP-BM7-2", "clue_batchm7_dup"); err != nil {
		t.Errorf("同线索第二条反而被挡（%v）⇒ 占位索引其实是唯一的，本腿读到的形状自相矛盾", err)
	}
}

// S28：obs_config 那道"全站最多一条默认"的索引同名非唯一 ⇒ 同一格缺陷的另一处消费。
//
// 与 S27 的差别不是文案：obs 这条钩子在建索引**之前**有一段清重 UPDATE，
// 所以它还可能"把双默认降级掉、再对着非唯一占位索引报就绪"。本腿刻意不铺双默认，
// 把降级那一段留在批M 的 I 腿上，这里只判结论与"不许自己换索引"。
func TestBatchM_ObsGuardDoesNotClaimReadyWhenNameIsNotUnique(t *testing.T) {
	gdb := batchM4StartupDB(t)
	const (
		table   = "obs_config"
		indexNA = "idx_obs_config_single_default"
	)
	if err := gdb.AutoMigrate(&model.ObsConfig{}); err != nil {
		t.Fatalf("铺 obs_config 表失败: %v", err)
	}
	t.Cleanup(func() {
		if err := gdb.Exec(`DELETE FROM obs_config WHERE name LIKE 'batchm7-%'`).Error; err != nil {
			t.Errorf("收尾清理本腿行失败: %v", err)
		}
		batchM7Restore(t, gdb, table, indexNA, func() { postMigrateObsDefaultUniqueIndex(gdb) })
	})
	batchM7Squat(t, gdb, table, indexNA, "is_default")

	log := batchM7CaptureLogs(t, func() { postMigrateObsDefaultUniqueIndex(gdb) })
	if strings.Contains(log, batchM4LogReady) {
		t.Errorf("名字被非唯一索引占住还报『已就绪』⇒ 双默认的库级兜底其实没铺上，日志却报铺上了:\n%s", log)
	}
	if !strings.Contains(log, batchM7LogNotUnique) {
		t.Errorf("启动日志里没有『%s』那一句:\n%s", batchM7LogNotUnique, log)
	}
	if !strings.Contains(log, indexNA) {
		t.Errorf("失败分支没点名是哪枚索引:\n%s", log)
	}
	unique, _, ok := batchM7IndexShape(t, gdb, table, indexNA)
	if !ok || unique {
		t.Errorf("钩子跑完之后形状变成 在场=%v 唯一=%v ⇒ 它换掉了库里那枚索引", ok, unique)
	}

	// 兜底确实没铺上：钩子跑完之后再来两条默认，第二条必须还插得进去（谓词索引在场时它必撞 23505）。
	// 这一句为什么不复用批M 的 `batchMInsertObs`：那个 helper 对插入错误 `t.Fatalf` ⇒
	// "第二条被挡"这一档会把整条腿当场掐掉，判据永远读不到那句 Errorf；本批要读的正是这个错误。
	// 它也**不是**上面形状断言的重复：钩子改去建一枚**别的名字**的唯一索引时，形状断言只看
	// 我 squat 的那枚名字（仍非唯一、仍报『不是唯一索引』）⇒ 全绿，而这一句会红。
	now := time.Now()
	if err := batchM7InsertObs(gdb, "batchm7-obs-a", true, now); err != nil {
		t.Fatalf("先放一条默认失败: %v", err)
	}
	if err := batchM7InsertObs(gdb, "batchm7-obs-b", true, now); err != nil {
		t.Errorf("第二条默认反而被挡（%v）⇒ 占位索引其实是唯一的，或者钩子另建了一枚唯一索引，本腿读到的形状自相矛盾", err)
	}
}

// S29：message_hub 三元组索引同名非唯一 ⇒ 第三处消费，也是**最便宜撞上一格**的一处：
// 那枚索引的生产者是 `model/ai_sales_champion.go` 的 uniqueIndex 标签（批M-5 第 1 点），
// 而 GORM 按名字对账 ⇒ 只要库里有一枚同名非唯一索引，标签与钩子**两层都不会**把它换成唯一的。
func TestBatchM_MessageHubGuardDoesNotClaimReadyWhenNameIsNotUnique(t *testing.T) {
	gdb := batchM4StartupDB(t)
	const (
		table   = "message_hub"
		indexNA = batchM5HubIndexName
	)
	if err := gdb.AutoMigrate(&model.MessageHub{}); err != nil {
		t.Fatalf("铺 message_hub 表失败: %v", err)
	}
	t.Cleanup(func() {
		if err := gdb.Unscoped().Where("account_id = ?", "batchm7-acct").
			Delete(&model.MessageHub{}).Error; err != nil {
			t.Errorf("收尾清理本腿行失败: %v", err)
		}
		batchM7Restore(t, gdb, table, indexNA, postMigrateMessageHubUniqueIndex)
	})
	batchM7Squat(t, gdb, table, indexNA, "platform, msg_id, conversation_id")

	log := batchM7CaptureLogs(t, postMigrateMessageHubUniqueIndex)
	if strings.Contains(log, batchM5LogHubReady) {
		t.Errorf("名字被非唯一索引占住还报『已就绪』⇒ 三元组去重其实没铺上，而这条键管的是消息会不会被当重复吞掉:\n%s", log)
	}
	if !strings.Contains(log, batchM7LogNotUnique) {
		t.Errorf("启动日志里没有『%s』那一句:\n%s", batchM7LogNotUnique, log)
	}
	if !strings.Contains(log, indexNA) {
		t.Errorf("失败分支没点名是哪枚索引:\n%s", log)
	}
	unique, _, ok := batchM7IndexShape(t, gdb, table, indexNA)
	if !ok || unique {
		t.Errorf("钩子跑完之后形状变成 在场=%v 唯一=%v ⇒ 它换掉了库里那枚索引", ok, unique)
	}

	// 两行都照 `batchM5HubRow` 的形状（platform/account_id/direction/msg_type 必填），
	// 但 account 用本腿自己的键：抄批M-5 的 batchM5HubRow 会把行落在 batchm5-acct 名下，
	// 上面那句按 account_id 的收尾就删不掉它们 ⇒ 留下的重复三元组会把收尾那次补索引顶成 23505，
	// 并把坏形状留给同槽位的批M-5 腿（实测踩过，红因是"could not create unique index"）。
	if err := gdb.Create(batchM7HubRow("batchm7-dup", "conv-1")).Error; err != nil {
		t.Fatalf("先放一条 hub 行失败: %v", err)
	}
	if err := gdb.Create(batchM7HubRow("batchm7-dup", "conv-1")).Error; err != nil {
		t.Errorf("三元组全同的第二行反而被挡（%v）⇒ 占位索引其实是唯一的，本腿读到的形状自相矛盾", err)
	}
}
