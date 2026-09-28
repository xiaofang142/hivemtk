package repository

// 契约锁：裁剪不等于证据消失。
//
// command_log 的 90 天界一到，PruneBefore 就把那段历史整行删掉。删完之后，
// 「那三天确实没有命令」与「那三天的命令被人在保留期外删了/删错了」在库里完全同形，
// 而前者是唯一可接受的解释、没人能证明它是。本批把它变成可证明的：删之前把
// (session, 行数, seq 区间, 逐行内容哈希折成的批摘要, 接上一条的链哈希) 写进
// browser_audit_digests（永不裁剪），并给每次扫描（含 0 行的那种）留一行 browser_audit_prune_runs。
//
// 这些腿各自钉住一句承诺，缺一句就退化成「记账好看但证不了事」：
//   - 摘要是删除的**前置条件**（写不进摘要就不许删）——否则它可以被悄悄跳过；
//   - 摘要覆盖的正是被删的那些行（闭合算术 sum(row_count)==rows_pruned==rows_before）；
//   - 摘要按 session 各自成链，跨扫描相接（prev_seq / prev_chain_hash 首尾对齐）；
//   - 批摘要对**内容**敏感（只有 payload 不同的两份数据不得折出同一个指纹）；
//   - 0 行的扫描也有一行留痕（CloudTrail 的 logFiles: [] —— 空摘要是一句可断言的话）；
//   - 分批承诺不被收回（一批 digest 对应一批 delete，且单批行数不越界）。
//
// 观测口径沿用 b19g：断言打到库里的那几行，不打错误文案。

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"testing"
	"time"

	"hivemtk-user/internal/browser_automation/model"
	"hivemtk-user/internal/pkg/testutil"

	"gorm.io/datatypes"
	"gorm.io/gorm"
)

const (
	a6SessionA = uint(782201)
	a6SessionB = uint(782202)
	a6Task     = uint(782203)
	a6Days     = 200
	// a6Source 是裁剪界的来源标注（/ §7.28 八-3）。PruneBefore 现在要求非空来源，
	// 本包所有裁剪腿都填它；「空来源必须被拒绝」单独由 TestR23PruneRefusesEmptyCutoffSource 钉。
	a6Source = "test:retention_a6"
	// a6SessionLate 只给「裁剪进行中才越界的那一行」那条腿用：不与既有 782201-782206 的任何
	// 行/链共用，免得并行残留改变本腿的批次切分（那条腿数的是批数与入口行数）。
	a6SessionLate = uint(782207)
	// a6SessionActor / a6SessionCrossed 只给「摘要声明行数与实删行数分家」那条腿用，理由同上。
	// 那一幕需要**两个** session（同一事务里窗口先裁 actor、再裁 crossed），actor 的 id 必须小
	// ——窗口按 session_id 升序返回，循环也就按这个顺序逐 session「先落摘要、再删行」。
	a6SessionActor   = uint(782208)
	a6SessionCrossed = uint(782209)
)

func a6DB(t *testing.T, models ...any) *gorm.DB {
	t.Helper()
	db := testutil.NewTestDB(t, models...)
	if db == nil {
		t.Fatal("测试库不可达：摘要是否先于删除落库无法判定（不 Skip，跳过等于没锁）")
	}
	return db
}

func a6Cutoff() time.Time { return time.Now().AddDate(0, 0, -a6Days) }

func a6Row(sessionID uint, seq int, action, payload string) *model.BrowserCommandLog {
	return &model.BrowserCommandLog{
		SessionID: sessionID, TaskID: a6Task, Seq: seq,
		Direction: "command", Action: action,
		Payload: datatypes.JSON([]byte(payload)),
	}
}

// a6Seed 落一批命令日志并把它们推到保留期之前。created_at 用一条 UPDATE 位移，
// 且必须在 PruneBefore 之前完成（之后表里已经没有这些行了）。
func a6Seed(t *testing.T, db *gorm.DB, rows ...*model.BrowserCommandLog) {
	t.Helper()
	if err := db.Create(rows).Error; err != nil {
		t.Fatalf("种子命令日志落库失败：%v", err)
	}
	sessions := map[uint]bool{}
	for _, r := range rows {
		sessions[r.SessionID] = true
	}
	for sid := range sessions {
		if err := db.Model(&model.BrowserCommandLog{}).Where("session_id = ?", sid).
			Update("created_at", a6Cutoff().Add(-24*time.Hour)).Error; err != nil {
			t.Fatalf("位移 created_at 失败：%v", err)
		}
	}
}

func a6Digests(t *testing.T, db *gorm.DB, sessionID uint) []*model.BrowserAuditDigest {
	t.Helper()
	var list []*model.BrowserAuditDigest
	if err := db.Where("session_id = ?", sessionID).Order("ordinal ASC").Find(&list).Error; err != nil {
		t.Fatalf("读摘要失败：%v", err)
	}
	return list
}

func a6RunRows(t *testing.T, db *gorm.DB) []*model.BrowserAuditPruneRun {
	t.Helper()
	var list []*model.BrowserAuditPruneRun
	if err := db.Order("id ASC").Find(&list).Error; err != nil {
		t.Fatalf("读扫描留痕失败：%v", err)
	}
	return list
}

func a6CountLog(t *testing.T, db *gorm.DB, sessionID uint) int64 {
	t.Helper()
	var n int64
	if err := db.Model(&model.BrowserCommandLog{}).
		Where("session_id = ?", sessionID).Count(&n).Error; err != nil {
		t.Fatalf("数命令日志失败：%v", err)
	}
	return n
}

func a6Prune(t *testing.T, db *gorm.DB) int64 {
	t.Helper()
	pruned, err := NewBrowserCommandLogRepositoryWithDB(db).PruneBefore(context.Background(), a6Cutoff(), a6Source)
	if err != nil {
		t.Fatalf("PruneBefore 报错：%v", err)
	}
	return pruned
}

// TestA6DigestCoversDeletedRows 摘要必须如实描述被删掉的那一段：行数、seq 区间、界。
func TestA6DigestCoversDeletedRows(t *testing.T) {
	db := a6DB(t, &model.BrowserCommandLog{}, &model.BrowserAuditDigest{}, &model.BrowserAuditPruneRun{})
	a6Seed(t, db,
		a6Row(a6SessionA, 1, "click", `{"x":1}`),
		a6Row(a6SessionA, 2, "type", `{"text":"你好"}`),
		a6Row(a6SessionA, 3, "click", `{"x":9}`),
	)
	if got := a6Prune(t, db); got != 3 {
		t.Fatalf("裁剪行数 %d want 3", got)
	}

	list := a6Digests(t, db, a6SessionA)
	if len(list) != 1 {
		t.Fatalf("摘要行数 %d want 1（实为 %+v）", len(list), list)
	}
	d := list[0]
	if d.Ordinal != 1 || d.PrevSeq != 0 {
		t.Errorf("首条摘要 ordinal=%d prev_seq=%d，want 1/0：本 session 的链应从 1 起、无前驱", d.Ordinal, d.PrevSeq)
	}
	if d.RowCount != 3 || d.FirstSeq != 1 || d.LastSeq != 3 {
		t.Errorf("摘要记的行数/seq 区间 = %d/%d-%d，want 3/1-3：删掉的那一段没被如实描述", d.RowCount, d.FirstSeq, d.LastSeq)
	}
	if len(d.BatchDigest) != 64 || len(d.ChainHash) != 64 {
		t.Errorf("批摘要/链哈希长度 = %d/%d，want 64/64（sha256 hex）", len(d.BatchDigest), len(d.ChainHash))
	}
	if d.PrevChainHash != "" {
		t.Errorf("首条摘要 prev_chain_hash=%q want 空串", d.PrevChainHash)
	}
	if d.Cutoff.IsZero() {
		t.Error("摘要没记下这次裁剪的时间界：日后无从回答「按哪个界放行的」")
	}
	if a6CountLog(t, db, a6SessionA) != 0 {
		t.Error("命令日志没删干净")
	}
}

// TestA6DigestClosesOverRunRow 闭合算术：每行被删前落进且只落进一条摘要。
// 对不上就是「有行既没被裁、也没留摘要地消失了」——本批立项的那件事。
func TestA6DigestClosesOverRunRow(t *testing.T) {
	db := a6DB(t, &model.BrowserCommandLog{}, &model.BrowserAuditDigest{}, &model.BrowserAuditPruneRun{})
	a6Seed(t, db,
		a6Row(a6SessionA, 1, "click", `{"x":1}`),
		a6Row(a6SessionA, 2, "type", `{"text":"a"}`),
		a6Row(a6SessionB, 1, "click", `{"x":7}`),
		a6Row(a6SessionB, 2, "click", `{"x":8}`),
		a6Row(a6SessionB, 3, "judge", `{"ok":true}`),
	)
	pruned := a6Prune(t, db)

	runs := a6RunRows(t, db)
	if len(runs) != 1 {
		t.Fatalf("扫描留痕行数 %d want 1", len(runs))
	}
	run := runs[0]
	var digestRows int64
	if err := db.Model(&model.BrowserAuditDigest{}).
		Where("session_id IN ?", []uint{a6SessionA, a6SessionB}).
		Select("COALESCE(sum(row_count),0)").Scan(&digestRows).Error; err != nil {
		t.Fatalf("汇总摘要行数失败：%v", err)
	}
	if pruned != run.RowsPruned {
		t.Errorf("返回值 %d 与留痕 rows_pruned=%d 不一致：调用方据返回值记日志，两边必须同源", pruned, run.RowsPruned)
	}
	if digestRows != run.RowsPruned {
		t.Errorf("摘要覆盖 %d 行、实删 %d 行：有行绕开摘要消失了", digestRows, run.RowsPruned)
	}
	if run.RowsBefore != run.RowsPruned {
		t.Errorf("入口该走的行 %d != 实删 %d：扫描没跑到「没有该走的行」就返回了", run.RowsBefore, run.RowsPruned)
	}
	if run.Digests != 2 {
		t.Errorf("留痕记的摘要条数 = %d want 2（两个 session 各一条）", run.Digests)
	}
	// 两个 session 各自独立成链：首条都不接前驱，且互不污染
	for _, sid := range []uint{a6SessionA, a6SessionB} {
		list := a6Digests(t, db, sid)
		if len(list) != 1 || list[0].PrevChainHash != "" {
			t.Errorf("session %d 的摘要链头不对：%+v", sid, list)
		}
	}
}

// TestA6ChainLinksAcrossRuns 跨扫描接续：第二条摘要必须钉住第一条的尾巴。
// 只删中间某一条会当场断链——这是「连摘要一起删、再补一段假历史」的唯一代价。
func TestA6ChainLinksAcrossRuns(t *testing.T) {
	db := a6DB(t, &model.BrowserCommandLog{}, &model.BrowserAuditDigest{}, &model.BrowserAuditPruneRun{})
	a6Seed(t, db,
		a6Row(a6SessionA, 1, "click", `{"x":1}`),
		a6Row(a6SessionA, 2, "type", `{"text":"第一轮"}`),
		a6Row(a6SessionA, 3, "click", `{"x":3}`),
	)
	a6Prune(t, db)

	// 第二轮：同 session 新攒 3 行（seq 4-6），推到界外再裁
	a6Seed(t, db,
		a6Row(a6SessionA, 4, "click", `{"x":4}`),
		a6Row(a6SessionA, 5, "type", `{"text":"第二轮"}`),
		a6Row(a6SessionA, 6, "click", `{"x":6}`),
	)
	a6Prune(t, db)

	list := a6Digests(t, db, a6SessionA)
	if len(list) != 2 {
		t.Fatalf("摘要条数 %d want 2：%+v", len(list), list)
	}
	first, second := list[0], list[1]
	if second.Ordinal != 2 {
		t.Errorf("第二条 ordinal=%d want 2：重放同一次裁剪没有撞约束，而是悄悄多出一条并行的历史", second.Ordinal)
	}
	if second.PrevSeq != first.LastSeq || second.PrevSeq != 3 {
		t.Errorf("第二条 prev_seq=%d，want 第一条 last_seq=3：seq 区间首尾不接，中间那段无从交代", second.PrevSeq)
	}
	if second.PrevChainHash != first.ChainHash {
		t.Errorf("第二条 prev_chain_hash=%q != 第一条 chain_hash=%q：链在这里断了",
			second.PrevChainHash, first.ChainHash)
	}
	// 链哈希的算法本身也要钉住：chain = sha256(prev_chain_hash || batch_digest)。
	// 不钉这一条，实现退化成 chain=batch_digest（前驱根本没参与）也照样全绿。
	sum := sha256.Sum256([]byte(first.ChainHash + second.BatchDigest))
	if want := hex.EncodeToString(sum[:]); second.ChainHash != want {
		t.Errorf("chain_hash=%q want %q（sha256(prev||batch)）", second.ChainHash, want)
	}
	sum1 := sha256.Sum256([]byte("" + first.BatchDigest))
	if want := hex.EncodeToString(sum1[:]); first.ChainHash != want {
		t.Errorf("首条 chain_hash=%q want %q：空前驱的算法与后续不一致", first.ChainHash, want)
	}
}

// TestA6DigestRecordsSeqGap 断号要在删除前被如实记下：区间长度对不上行数，
// 就是「裁剪之前那一行已经不在了」的凭据（而不是删完之后无从分辨）。
func TestA6DigestRecordsSeqGap(t *testing.T) {
	db := a6DB(t, &model.BrowserCommandLog{}, &model.BrowserAuditDigest{}, &model.BrowserAuditPruneRun{})
	a6Seed(t, db,
		a6Row(a6SessionA, 1, "click", `{"x":1}`),
		a6Row(a6SessionA, 2, "click", `{"x":2}`),
		a6Row(a6SessionA, 3, "click", `{"x":3}`),
		a6Row(a6SessionA, 5, "click", `{"x":5}`), // seq 4 从未落库
	)
	a6Prune(t, db)

	list := a6Digests(t, db, a6SessionA)
	if len(list) != 1 {
		t.Fatalf("摘要条数 %d want 1", len(list))
	}
	d := list[0]
	if d.RowCount != 4 || d.FirstSeq != 1 || d.LastSeq != 5 {
		t.Fatalf("摘要 = 行数%d 区间%d-%d，want 4/1-5", d.RowCount, d.FirstSeq, d.LastSeq)
	}
	if span := int64(d.LastSeq - d.FirstSeq + 1); span != d.RowCount {
		t.Logf("区间长度 %d != 行数 %d：断号 %d 处，摘要已如实记下（这正是本腿要的形状）", span, d.RowCount, span-d.RowCount)
	} else {
		t.Errorf("区间长度 %d == 行数：seq 没被如实读出，断号将无声消失", span)
	}
}

// TestA6BatchDigestSensitiveToPayload 批摘要必须吃进 payload 全文。
// 只按 (seq, action) 折哈希也能产出一条形似的摘要，但它证不了「删掉的是哪些内容」——
// 两条内容不同、形状相同的记录会折出同一个指纹，摘要当场失去资格。
//
// 判据形状与 ok 三态那一腿同源：**同 session、同 seq、同正文形状、同 created_at 的两轮窗口，
// 只有 payload 一列不同**，两轮批摘要必须不同。
// 首版这里是"两个 session 各一行、正文不同"，电池 D9（把 payload 从哈希字段里删掉）当场存活——
// session_id 本来就在哈希里，两条指纹当然不同，那红的是"两个 session 不一样"，与 payload 无关。
// 留这段理由是因为它是一类通用错法：**跨主键对照证不了任何单列的敏感性**。
func TestA6BatchDigestSensitiveToPayload(t *testing.T) {
	db := a6DB(t, &model.BrowserCommandLog{}, &model.BrowserAuditDigest{}, &model.BrowserAuditPruneRun{})
	a6SeedFrozen(t, db, a6Row(a6SessionA, 1, "type", `{"text":"发给 A 的正文"}`))
	a6Prune(t, db)
	a6SeedFrozen(t, db, a6Row(a6SessionA, 1, "type", `{"text":"发给 B 的正文"}`))
	a6Prune(t, db)

	list := a6Digests(t, db, a6SessionA)
	if len(list) != 2 {
		t.Fatalf("摘要条数 %d want 2：%+v", len(list), list)
	}
	first, second := list[0], list[1]
	// 前提：两轮窗口除正文外逐字段同形（不同形时下面那句"必须不同"就成了白送）。
	if first.RowCount != second.RowCount || first.FirstSeq != second.FirstSeq ||
		first.LastSeq != second.LastSeq {
		t.Fatalf("两轮窗口不再同形：%+v vs %+v —— 本腿的对照失效（只有 payload 该不同）", first, second)
	}
	if first.BatchDigest == second.BatchDigest {
		t.Errorf("两份不同正文折出了同一个批摘要 %s：payload 没进哈希，摘要证不了「删掉的是哪些内容」",
			first.BatchDigest)
	}
}

// TestA6PruneFailsCloseWhenDigestWriteBlocked 摘要**写不进**就不许删（读得到也一样）。
//
// 与上一腿（表整个不在）不是同一件事，而且是更要紧的那件：表不在时
// `lastAuditDigest` 的那条 SELECT 就先报错，整条路径根本走不到 INSERT——
// 所以"缺表"那一腿**证不了写侧有牙**（电池 D1/D2 首轮实跑：注掉/吞掉 `tx.Create` 之后
// 它照样绿，因为红因压根不在它断的那一格上）。这里给的是「读得到、写不进」这个形状：
// 一个恒假 CHECK 约束把摘要行挡在库门外，事务必须整体回滚，一行都不许消失。
//
// 为什么不用更直觉的 BEFORE INSERT 触发器（实测过，2026-09-22）：testutil 的连接池在建池时
// 执行了 `SET session_replication_role = 'replica'`（internal/pkg/testutil/testdb.go:180），
// 而 PG 默认（ENABLE ORIGIN）触发器在这个角色下**静默不触发**——触发器在 `pg_trigger` 里查得到、
// 摘要照样插得进去，本腿会以「PruneBefore 无错」假红，读起来像实现坏了。要它有牙得再补一句
// `ALTER TABLE ... ENABLE ALWAYS TRIGGER`（同样实测有效）。CHECK 约束不受这个角色影响，
// 少一处环境耦合，故用它。
//
// 末尾还有一条反向锁：约束摘掉后同一批行必须裁得动。少了它，上面的红可能只是
// 夹具把整条路（含删除）一起堵死，那等于什么都没测。
func TestA6PruneFailsCloseWhenDigestWriteBlocked(t *testing.T) {
	db := a6DB(t, &model.BrowserCommandLog{}, &model.BrowserAuditDigest{}, &model.BrowserAuditPruneRun{})
	add := `ALTER TABLE browser_audit_digests ADD CONSTRAINT a6_block_digest CHECK (FALSE) NOT VALID`
	drop := func() error {
		return db.Exec(`ALTER TABLE browser_audit_digests DROP CONSTRAINT IF EXISTS a6_block_digest`).Error
	}
	if err := db.Exec(add).Error; err != nil {
		t.Fatalf("造拦截约束失败（本腿前提）：%v", err)
	}
	// 前提还要再钉一层：约束建成了不代表它拦得住写。不验这一格的话，本腿会以
	// 「PruneBefore 无错」的形状假红，而红因在夹具上而不是实现上。
	if err := db.Create(&model.BrowserAuditDigest{SessionID: a6SessionA, RowCount: 1, FirstSeq: 1,
		LastSeq: 1, Ordinal: 1, BatchDigest: "x", ChainHash: "x", Cutoff: a6Cutoff()}).Error; err == nil {
		t.Fatal("拦截约束没拦住直插：前提不成立，本腿无判据")
	}
	t.Cleanup(func() {
		// 影子库按进程共享：约束留着会挡住后面每一条写摘要的用例（表现是一堆莫名的红）。
		if err := drop(); err != nil {
			t.Errorf("还原拦截约束失败，后续用例会撞： %v", err)
		}
	})

	a6Seed(t, db,
		a6Row(a6SessionA, 1, "click", `{"x":1}`),
		a6Row(a6SessionA, 2, "click", `{"x":2}`),
	)
	// 上面那次探路插入被约束挡下了，库里没有残留；但 a6SessionA 已被"读过"，
	// 摘要链的尾巴查不到（表里空），行为与干净库一致。
	pruned, err := NewBrowserCommandLogRepositoryWithDB(db).PruneBefore(context.Background(), a6Cutoff(), a6Source)
	if err == nil {
		t.Errorf("摘要写不进却返回了无错（裁剪 %d 行）：INSERT 失败被吞掉了", pruned)
	}
	if left := a6CountLog(t, db, a6SessionA); left != 2 {
		t.Errorf("还剩 %d 行 want 2：摘要没落库就把行删了，这段历史从此无从自证", left)
	}

	if err := drop(); err != nil {
		t.Fatalf("摘掉拦截约束失败，无法做反向锁：%v", err)
	}
	if got := a6Prune(t, db); got != 2 {
		t.Errorf("拦截约束摘掉后仍裁了 %d 行 want 2：上面的红是夹具把整条路堵死，不是写侧的牙", got)
	}
}

// TestA6PruneFailsCloseWithoutDigestTable 摘要表整个不在时不许删。
// 这一腿是「前置条件」的一个形状：表不在 → 删除必须整体不发生（而不是删完再补记）。
//
// 但要说清它**管不到**哪一格：表不在时 `lastAuditDigest` 的 SELECT 先报错，路径走不到 INSERT，
// 所以「摘要写不上还被吞掉」这一坏法在这里是测不到的（电池首轮实锤：D1 注掉 `tx.Create`、
// D2 吞掉它的错误，本腿两样都照样绿）。那一格由 TestA6PruneFailsCloseWhenDigestWriteBlocked
// 守——「读得到、写不进」的形状。
func TestA6PruneFailsCloseWithoutDigestTable(t *testing.T) {
	db := a6DB(t, &model.BrowserCommandLog{})
	// 进程级影子库里，前面某条用例建过的摘要表会一直留在同一个库中（NewTestDB 只
	// drop+重建本次列出的模型），所以「表不在」这个前提必须自己摘掉，并且摘不成就停机
	// ——前提没成立时本腿会以「PruneBefore 报错」的形状假绿，那比没有这条腿更糟。
	if err := db.Migrator().DropTable(&model.BrowserAuditDigest{}, &model.BrowserAuditPruneRun{}); err != nil {
		t.Fatalf("摘掉摘要表失败（本腿的前提就是它不在）：%v", err)
	}
	t.Cleanup(func() {
		if err := db.AutoMigrate(&model.BrowserAuditDigest{}, &model.BrowserAuditPruneRun{}); err != nil {
			t.Errorf("还原摘要表失败，后续用例会撞缺表：%v", err)
		}
	})
	a6Seed(t, db,
		a6Row(a6SessionA, 1, "click", `{"x":1}`),
		a6Row(a6SessionA, 2, "click", `{"x":2}`),
	)
	pruned, err := NewBrowserCommandLogRepositoryWithDB(db).PruneBefore(context.Background(), a6Cutoff(), a6Source)
	if err == nil {
		t.Errorf("摘要表不存在却返回了无错（裁剪 %d 行）：删除没把摘要当前置条件", pruned)
	}
	if left := a6CountLog(t, db, a6SessionA); left != 2 {
		t.Errorf("还剩 %d 行 want 2：摘要没写成却把行删了，这段历史从此无从自证", left)
	}
}

// TestA6EmptyWindowStillWritesRunRow 0 行的扫描也要留下一行「该界内无到期行」。
// CloudTrail 的 logFiles: [] 就是这个用法：空摘要不是一句「没跑」，而是一句可断言的事实。
// 少了它，「那段本来就没有日志」和「扫描器那天挂了」在库里同形，而处置动作相反。
func TestA6EmptyWindowStillWritesRunRow(t *testing.T) {
	db := a6DB(t, &model.BrowserCommandLog{}, &model.BrowserAuditDigest{}, &model.BrowserAuditPruneRun{})
	// 只落未到期行（不位移 created_at）
	if err := db.Create(a6Row(a6SessionA, 1, "click", `{"x":1}`)).Error; err != nil {
		t.Fatalf("种子未到期日志落库失败：%v", err)
	}
	if got := a6Prune(t, db); got != 0 {
		t.Errorf("裁剪行数 %d want 0：未到期行被裁了", got)
	}
	runs := a6RunRows(t, db)
	if len(runs) != 1 {
		t.Fatalf("扫描留痕行数 %d want 1：空窗口没有自证，读库的人无法区分「无事件」与「没跑」", len(runs))
	}
	run := runs[0]
	if run.RowsPruned != 0 || run.RowsBefore != 0 || run.Digests != 0 || run.Batches != 0 {
		t.Errorf("空窗口留痕 = 裁%d 入口%d 摘要%d 批%d，want 全 0",
			run.RowsPruned, run.RowsBefore, run.Digests, run.Batches)
	}
	if run.Cutoff.IsZero() {
		t.Error("留痕没记下这次用的是哪个界：空断言没有对象")
	}
	if n := len(a6Digests(t, db, a6SessionA)); n != 0 {
		t.Errorf("空窗口写出了 %d 条摘要：那行根本不存在", n)
	}
}

// TestA6DigestReproducibleFromRowContent 摘要必须能从**行内容**复现，且在不是 UTC 的
// 连接上复现得一模一样。
//
// 这条腿管两件别人管不了的事：
//  1. 复算走的是与实现同一份 SQL 常量（同包可见），于是「批摘要 = 逐行哈希按 id 序折起来」
//     这条式子被钉成一条可执行的断言，而不是注释里的一句承诺；日后改动字段清单
//     （漏掉 payload、把 ok 的三态折成一态）都会在这里当场红。
//  2. 复算刻意跑在 Asia/Kolkata 会话时区里。created_at 是 timestamptz，
//     直接 ::text 会按**会话时区**渲染 ⇒ 同一批行在 UTC 主库和任何一颗不同时区的副本上
//     折出不同指纹——那意味着「凭据」其实是凭据 + 环境，重算的人必须先猜对连接参数。
//     （本仓已有过一次同类裂脑：日期分桶因 Go/PG 各按一个时区而只在某几个小时内红。）
func TestA6DigestReproducibleFromRowContent(t *testing.T) {
	db := a6DB(t, &model.BrowserCommandLog{}, &model.BrowserAuditDigest{}, &model.BrowserAuditPruneRun{})

	// 前提：默认会话时区不是我们要切过去的那个，否则「换个时区复算」这句是空话。
	var tz string
	if err := db.Raw("SHOW timezone").Scan(&tz).Error; err != nil {
		t.Fatalf("读会话默认时区失败：%v", err)
	}
	if tz == "Asia/Kolkata" {
		t.Fatalf("测试库会话时区本来就是 Asia/Kolkata，本腿的对照失效（换个时区再跑）")
	}

	a6Seed(t, db,
		a6Row(a6SessionA, 1, "click", `{"x":1}`),
		a6Row(a6SessionA, 2, "type", `{"text":"含中文与引号 \\\" 的正文"}`),
		a6Row(a6SessionA, 3, "click", `{"x":3}`),
	)

	recomputed := a6DigestUnderSessionTZ(t, db, "Asia/Kolkata")
	pruned := a6Prune(t, db)
	if pruned != 3 {
		t.Fatalf("裁剪行数 %d want 3", pruned)
	}

	list := a6Digests(t, db, a6SessionA)
	if len(list) != 1 {
		t.Fatalf("摘要条数 %d want 1", len(list))
	}
	if list[0].BatchDigest != recomputed {
		t.Errorf("批摘要不可从行内容复现：库里 %s，非 UTC 连接上重算得 %s。\n"+
			"要么字段清单/折叠顺序被改动了却没同步这条式子，要么哈希吃进了会话时区渲染出来的时间文本",
			list[0].BatchDigest, recomputed)
	}
}

// a6DigestUnderSessionTZ 在指定会话时区里、同一个事务内（set_config 的 local 标志
// 不污染池子里别的连接）跑一遍实现用的那段窗口 SQL，返回该 session 的批摘要。
func a6DigestUnderSessionTZ(t *testing.T, db *gorm.DB, zone string) string {
	t.Helper()
	tx := db.Begin()
	if tx.Error != nil {
		t.Fatalf("开复算事务失败：%v", tx.Error)
	}
	defer tx.Rollback()
	// set_config 的第三参 true = 只在本事务内生效：池子里别的连接不受影响。
	// （SET LOCAL 本身不接受绑定参数，写成 `SET LOCAL timezone = ?` 会在 PG 侧报语法错。）
	if err := tx.Exec("SELECT set_config('timezone', ?, true)", zone).Error; err != nil {
		t.Fatalf("切会话时区到 %s 失败：%v", zone, err)
	}
	var window []pruneWindow
	// 高水位取 int64 上界：本函数只负责「这批内容折出来是什么」，不参与裁剪的批次切分。
	if err := tx.Raw(pruneWindowSQL, a6Cutoff(), int64(1)<<62, pruneBatchRows).Scan(&window).Error; err != nil {
		t.Fatalf("复算批摘要失败：%v", err)
	}
	for _, w := range window {
		if w.SessionID == a6SessionA {
			return w.BatchDigest
		}
	}
	t.Fatalf("复算窗口里没有 session %d 的摘要段：%+v", a6SessionA, window)
	return ""
}

// TestA6DigestSplitsWithDeleteBatches 一批删除对应一条摘要：分批（b19g 立的锁）与自证
// 必须是同一套切分，否则「分批」会变成把一次裁剪的凭据切碎的手段，
// 而首尾相接的 seq 区间正是检查切碎没切错的地方。
func TestA6DigestSplitsWithDeleteBatches(t *testing.T) {
	db := a6DB(t, &model.BrowserCommandLog{}, &model.BrowserAuditDigest{}, &model.BrowserAuditPruneRun{})
	rows := make([]*model.BrowserCommandLog, 0, pruneBatchRows+1)
	for i := 0; i < pruneBatchRows+1; i++ {
		rows = append(rows, a6Row(a6SessionA, i+1, "click", `{"x":1}`))
	}
	if err := db.CreateInBatches(rows, 500).Error; err != nil {
		t.Fatalf("种子落库失败：%v", err)
	}
	if err := db.Model(&model.BrowserCommandLog{}).Where("session_id = ?", a6SessionA).
		Update("created_at", a6Cutoff().Add(-24*time.Hour)).Error; err != nil {
		t.Fatalf("位移 created_at 失败：%v", err)
	}

	if got := a6Prune(t, db); got != int64(pruneBatchRows+1) {
		t.Fatalf("裁剪行数 %d want %d", got, pruneBatchRows+1)
	}
	list := a6Digests(t, db, a6SessionA)
	if len(list) != 2 {
		t.Fatalf("摘要条数 %d want 2（一批删除一条摘要）：%+v", len(list), list)
	}
	if list[0].RowCount != int64(pruneBatchRows) || list[1].RowCount != 1 {
		t.Errorf("两条摘要各覆盖 %d/%d 行，want %d/1：切分与删除的批次不是一套",
			list[0].RowCount, list[1].RowCount, pruneBatchRows)
	}
	if list[0].FirstSeq != 1 || list[0].LastSeq != pruneBatchRows ||
		list[1].FirstSeq != pruneBatchRows+1 || list[1].LastSeq != pruneBatchRows+1 {
		t.Errorf("seq 区间 = %d-%d / %d-%d，want 1-%d / %d-%d：第二批多算或少算了行",
			list[0].FirstSeq, list[0].LastSeq, list[1].FirstSeq, list[1].LastSeq,
			pruneBatchRows, pruneBatchRows+1, pruneBatchRows+1)
	}
	if list[1].PrevSeq != list[0].LastSeq || list[1].PrevChainHash != list[0].ChainHash {
		t.Error("批次之间没接上链：中间那一段可以整块换掉而无人能发现")
	}
	runs := a6RunRows(t, db)
	if len(runs) != 1 || runs[0].Batches != 2 || runs[0].Digests != 2 {
		t.Errorf("留痕 = %+v，want 1 行 batches=2 digests=2", runs)
	}
	if len(runs) == 1 && runs[0].RowsPruned != int64(pruneBatchRows+1) {
		t.Errorf("留痕 rows_pruned=%d want %d", runs[0].RowsPruned, pruneBatchRows+1)
	}
}

// a6FrozenStamp 是一个固定在过去（必然早于 a6Cutoff）的瞬时，用来把两轮的 created_at
// 钉成同一个值——a6Seed 走 time.Now，两轮之间会自己差出微秒，那哈希当然不同，但那是
// 「时间不同」的功劳，不是本腿要证的「ok 不同」。
var a6FrozenStamp = time.Date(2025, 1, 2, 3, 4, 5, 0, time.UTC)

// a6SeedFrozen 与 a6Seed 同义，唯一区别是把 created_at 位移到 a6FrozenStamp。
func a6SeedFrozen(t *testing.T, db *gorm.DB, rows ...*model.BrowserCommandLog) {
	t.Helper()
	if !a6FrozenStamp.Before(a6Cutoff()) {
		t.Fatalf("固定瞬时 %v 不在保留界 %v 之前：这一轮根本不会被裁，本腿的对照失效",
			a6FrozenStamp, a6Cutoff())
	}
	if err := db.Create(rows).Error; err != nil {
		t.Fatalf("种子落库失败：%v", err)
	}
	sessions := map[uint]bool{}
	for _, r := range rows {
		sessions[r.SessionID] = true
	}
	for sid := range sessions {
		if err := db.Model(&model.BrowserCommandLog{}).Where("session_id = ?", sid).
			Update("created_at", a6FrozenStamp).Error; err != nil {
			t.Fatalf("钉住 created_at 失败：%v", err)
		}
	}
}

// TestA6DigestSeparatesUnackedFromFailedFrame 「没有回执」与「回执为否」不得折进同一个指纹。
//
// 把 ok 从 bool 改成 *bool，为的就是这一字之差：command 帧写下时 Host 还没回执，
// 把 nil 画成 false 等于宣称「这帧失败了」。裁剪之后那一段只剩摘要，如果摘要把两者折成
// 同一个哈希，那次「发出去没回音」的观察就永久变成了「做过了且失败」——
// 而这正是双发/重发判断最怕的那种改写（它会把一条待查的帧说成一条已定的帧）。
//
// 判据形状：同一 session、同一 seq、同一正文、同一 created_at 的两轮窗口，
// 只有 ok 一列不同（nil vs false），两轮批摘要必须不同。
func TestA6DigestSeparatesUnackedFromFailedFrame(t *testing.T) {
	db := a6DB(t, &model.BrowserCommandLog{}, &model.BrowserAuditDigest{}, &model.BrowserAuditPruneRun{})
	neg := false

	a6SeedFrozen(t, db, a6Row(a6SessionA, 1, "click", `{"x":1}`)) // ok 留空 = 无回执
	a6Prune(t, db)

	acked := a6Row(a6SessionA, 1, "click", `{"x":1}`)
	acked.Ok = &neg
	a6SeedFrozen(t, db, acked)
	a6Prune(t, db)

	// 摘要是**累加**的（本表永不裁剪），所以两轮下来这个 session 该有两条：
	// ordinal 1 = 无回执那一批，ordinal 2 = 回执为否那一批。
	list := a6Digests(t, db, a6SessionA)
	if len(list) != 2 {
		t.Fatalf("摘要条数 %d want 2：%+v", len(list), list)
	}
	first, second := list[0], list[1]
	if first.Ordinal != 1 || second.Ordinal != 2 {
		t.Fatalf("两条摘要的 ordinal = %d/%d want 1/2：读到的顺序不对，下面的对照没有对象",
			first.Ordinal, second.Ordinal)
	}
	// 前提：两轮窗口除 ok 外逐字段同形。不钉这一格，两串哈希不同可能只是别处抖了。
	if first.RowCount != second.RowCount || first.FirstSeq != second.FirstSeq ||
		first.LastSeq != second.LastSeq {
		t.Fatalf("两轮窗口不再同形：%+v vs %+v —— 本腿的对照失效（只有 ok 该不同）", first, second)
	}
	if first.BatchDigest == second.BatchDigest {
		t.Errorf("「无回执」与「回执为否」折出同一个批摘要 %s：ok 的三态被折成一态，"+
			"裁掉的那一段里一条没回音的帧会被说成一条失败的帧", first.BatchDigest)
	}
}

// TestA6DigestChainIdempotentAtStorage 摘要表的自证资格住在存储层，不在 Go 代码里。
//
// 与同一课：本表自己若可重复、可软删，它就只是一份可被改写的记账，
// 而「删日志、再补一段假的」正好需要这两样。所以这里钉三格：
//   - 同 (session_id, ordinal) 再插一条必须被库拒掉（重放裁剪=撞约束，不是多出一条并行历史）；
//   - 换一个 ordinal 必须插得进（约束是复合的；写成 session_id 单列唯一的话本表只能记一批）；
//   - 表上不许有 deleted_at（软删等于给抹掉证据留一条正规通道）。
func TestA6DigestChainIdempotentAtStorage(t *testing.T) {
	db := a6DB(t, &model.BrowserAuditDigest{}, &model.BrowserAuditPruneRun{})
	row := func(ordinal int, digest string) *model.BrowserAuditDigest {
		return &model.BrowserAuditDigest{
			SessionID: a6SessionA, RowCount: 1, FirstSeq: 1, LastSeq: 1,
			Ordinal: ordinal, BatchDigest: digest, PrevChainHash: "", ChainHash: digest,
			Cutoff: a6Cutoff(),
		}
	}
	if err := db.Create(row(1, "aa")).Error; err != nil {
		t.Fatalf("首条摘要落库失败：%v", err)
	}
	if err := db.Create(row(2, "bb")).Error; err != nil {
		t.Errorf("同 session 第二条摘要插不进（%v）：唯一索引被写成了 session_id 单列唯一，本表只能记一批", err)
	}
	if err := db.Create(row(1, "cc")).Error; err == nil {
		t.Error("同 (session_id, ordinal) 的第二条插入成功了：重放一次裁剪会在库里多出一条并行的历史，摘要不再是指纹")
	}
	if db.Migrator().HasColumn(&model.BrowserAuditDigest{}, "deleted_at") {
		t.Error("摘要表带 deleted_at：软删让「抹掉证据」退化成一次正常 DELETE")
	}
}

// TestA6DigestSeparatesPassedFromFailedFrame 「回执为是」与「回执为否」不得折进同一个指纹。
//
// 与上一腿（nil vs false）打的**不是同一刀**，这一点必须写清楚，否则它会让人以为 ok 的三态
// 已经被整体锁住了：上一腿对照的是「没有回执」与「回执为否」，把 auditRowHashSQL 里
// `CASE WHEN ok IS NULL THEN 'n' WHEN ok THEN 't' ELSE 'f' END` 裁成
// `CASE WHEN ok IS NULL THEN 'n' ELSE 't' END` 之后，nil 仍走 'n'、false 仍走 't'，
// 那一腿照样绿（C 相电池 D22 实跑即如此）。真正被折成一态的是 **true 与 false**——
// 也就是前面拆开的「下发了 / 平台确认了」那根轴：裁掉的那一段里，
// 一条成功的帧与一条失败的帧在摘要上再也分不开。
//
// 判据形状与上一腿同源：同 session、同 seq、同正文、同 created_at 的两轮窗口，
// 只有 ok 一列不同（true vs false），两轮批摘要必须不同。
// 对照用的是 PruneBefore 自己写进库里的那条 batch_digest，不把 SQL 常量抄进测试（抄了就是自证）。
func TestA6DigestSeparatesPassedFromFailedFrame(t *testing.T) {
	db := a6DB(t, &model.BrowserCommandLog{}, &model.BrowserAuditDigest{}, &model.BrowserAuditPruneRun{})
	pos, neg := true, false

	passed := a6Row(a6SessionA, 1, "click", `{"x":1}`)
	passed.Ok = &pos
	a6SeedFrozen(t, db, passed) // ok=true：回执为是
	a6Prune(t, db)

	failed := a6Row(a6SessionA, 1, "click", `{"x":1}`)
	failed.Ok = &neg
	a6SeedFrozen(t, db, failed) // ok=false：回执为否
	a6Prune(t, db)

	list := a6Digests(t, db, a6SessionA)
	if len(list) != 2 {
		t.Fatalf("摘要条数 %d want 2：%+v", len(list), list)
	}
	first, second := list[0], list[1]
	if first.Ordinal != 1 || second.Ordinal != 2 {
		t.Fatalf("两条摘要的 ordinal = %d/%d want 1/2：读到的顺序不对，下面的对照没有对象",
			first.Ordinal, second.Ordinal)
	}
	// 前提：两轮窗口除 ok 外逐字段同形。不钉这一格，两串哈希不同可能只是别处抖了。
	if first.RowCount != second.RowCount || first.FirstSeq != second.FirstSeq ||
		first.LastSeq != second.LastSeq {
		t.Fatalf("两轮窗口不再同形：%+v vs %+v —— 本腿的对照失效（只有 ok 该不同）", first, second)
	}
	if first.BatchDigest == second.BatchDigest {
		t.Errorf("「回执为是」与「回执为否」折出同一个批摘要 %s：ok 的 t/f 两态被折成一态，"+
			"裁掉的那一段里一条成功的帧与一条失败的帧再也分不开（A2 拆开的两轴又并回一列）", first.BatchDigest)
	}
}

// a6LateCrossStamp 是「跑到一半才越界」那一行被扳到的瞬时。写成 SQL 字面量而不是绑定参数：
// PG 触发器体里不接受参数；扳错方向（扳到界之后）时下面那句「实删 6 行」的前提自证会当场停机，
// 不会静默把对照跑成空。
const a6LateCrossStamp = "2025-01-02 00:00:00+00"

// TestA6RunRowCountsEntryWindowNotLateCrossing 钉住 `RowsBefore: rowsBefore` 这一格。
//
// 为什么既有腿钉不住它（C 相实跑：电池 D20 注 `RowsBefore: total` 后全绿）：
// PruneBefore 的入口计数谓词与窗口/删除谓词是同一套（`created_at < cutoff AND id <= highwater`），
// 循环又必须跑到窗口空才写 run 行，于是在「没人中途改数据」的任何一次成功裁剪上
// total 恒等于 rowsBefore——那条两批次的大腿（chain_prune 文件里）自己也是这么自陈的。
// 换成恒等值不构成一格有牙的注码，除非**造出 total != rowsBefore 的那一幕**：
// 「摘要写完到收口之间另一双手伸进来」。本腿用行级触发器造这一幕，且造在**真实代码路径**上
// （不注入 gorm callback、不手工往仓储里塞一行再造期望）：
//   - 先播一行未到期（created_at=now，id 必须最小，见下）；
//   - 再播 5 行超期；
//   - AFTER DELETE 触发器在删掉本 session 任何一行时，把那行未到期的补写成超期
//     ——形状 = 回填/迁移在裁剪进行中把老时间戳写进已有行（正是高水位那段注释防的事）。
//
// 于是入口数到 5 行、实删 6 行（越界那行落进后一批）。rows_before 必须是**入口那一瞬**的 5；
// 写成收口时的 total 就变成 6，而「一开始就该走掉几行」这句话从此由结果倒推、恒真。
//
// 为什么那一行的 id 必须最小：DELETE 语句按 id 升序扫（谓词含 `id <= ?`），若先删触发越界、
// 后扫到那一行，它会在**同一条语句**里被一起删掉 → RowsAffected(6) != RowCount(5) →
// 整批回滚、run 行根本不写，本腿就没有对象了。行序前提在代码里当场自证。
func TestA6RunRowCountsEntryWindowNotLateCrossing(t *testing.T) {
	db := a6DB(t, &model.BrowserCommandLog{}, &model.BrowserAuditDigest{}, &model.BrowserAuditPruneRun{})

	late := a6Row(a6SessionLate, 90, "click", `{"late":1}`) // created_at=now：入口这一刻不该走
	if err := db.Create(late).Error; err != nil {
		t.Fatalf("未到期种子落库失败：%v", err)
	}
	due := []*model.BrowserCommandLog{
		a6Row(a6SessionLate, 1, "click", `{"d":1}`),
		a6Row(a6SessionLate, 2, "click", `{"d":2}`),
		a6Row(a6SessionLate, 3, "click", `{"d":3}`),
		a6Row(a6SessionLate, 4, "click", `{"d":4}`),
		a6Row(a6SessionLate, 5, "click", `{"d":5}`),
	}
	if err := db.Create(due).Error; err != nil {
		t.Fatalf("超期种子落库失败：%v", err)
	}
	dueIDs := make([]uint, 0, len(due))
	for _, r := range due {
		dueIDs = append(dueIDs, r.ID)
	}
	if late.ID >= dueIDs[0] {
		t.Fatalf("行序前提不成立：未到期那行 id=%d 必须小于最早一条超期行 id=%d（否则它会在同一条语句里被一起删掉）",
			late.ID, dueIDs[0])
	}
	// 只把 5 行超期的推到界外（a6Seed 按 session 整片位移，会把 late 一起带走，故按 id 点名）。
	if err := db.Model(&model.BrowserCommandLog{}).Where("id IN ?", dueIDs).
		Update("created_at", a6Cutoff().Add(-24*time.Hour)).Error; err != nil {
		t.Fatalf("位移超期行失败：%v", err)
	}

	install := fmt.Sprintf(`CREATE FUNCTION a6_late_cross_fn() RETURNS trigger AS $fn$
BEGIN
	IF OLD.session_id = %d THEN
		UPDATE browser_command_log SET created_at = TIMESTAMP WITH TIME ZONE '%s' WHERE id = %d;
	END IF;
	RETURN OLD;
END $fn$ LANGUAGE plpgsql`, a6SessionLate, a6LateCrossStamp, late.ID)
	trigger := `CREATE TRIGGER a6_late_cross AFTER DELETE ON browser_command_log ` +
		`FOR EACH ROW EXECUTE FUNCTION a6_late_cross_fn()`
	// 池连接跑在 session_replication_role='replica' 下，默认触发器静默不触发，必须 ENABLE ALWAYS
	// （同 TestA6PruneFailsCloseWhenDeleteFailsMidRun 的实测）。
	always := `ALTER TABLE browser_command_log ENABLE ALWAYS TRIGGER a6_late_cross`
	remove := func() error {
		return db.Exec(`DROP TRIGGER IF EXISTS a6_late_cross ON browser_command_log`).Error
	}
	if err := db.Exec(install).Error; err != nil {
		t.Fatalf("造越界补写触发器失败（本腿前提）：%v", err)
	}
	if err := db.Exec(trigger).Error; err != nil {
		t.Fatalf("建越界补写触发器失败（本腿前提）：%v", err)
	}
	if err := db.Exec(always).Error; err != nil {
		t.Fatalf("启用 ALWAYS 触发器失败：%v —— 夹具没装上，本腿无判据", err)
	}
	t.Cleanup(func() {
		if err := remove(); err != nil {
			t.Errorf("删除越界补写触发器失败，同库后续用例会撞上：%v", err)
		}
		if err := db.Exec(`DROP FUNCTION IF EXISTS a6_late_cross_fn()`).Error; err != nil {
			t.Errorf("删除越界补写函数失败：%v", err)
		}
	})

	pruned := a6Prune(t, db)
	// 前提自证：那一行确实中途越界、确实被本轮裁掉了（只有 5 行时 total 与 rowsBefore 同值，
	// 对照就塌回成「恒真的恒等式」）。
	if pruned != 6 {
		t.Fatalf("本轮裁剪 %d 行 want 6（5 条超期 + 1 条进行中越界的补写）："+
			"越界那一幕没发生、或被同一条语句吞掉了，本腿的入口计数对照没有对象", pruned)
	}
	runs := a6RunRows(t, db)
	if len(runs) != 1 {
		t.Fatalf("run 行数 %d want 1：%+v", len(runs), runs)
	}
	run := runs[0]
	if run.RowsPruned != 6 {
		t.Fatalf("留痕 rows_pruned=%d want 6：%+v", run.RowsPruned, run)
	}
	if run.Batches != 2 || run.Digests != 2 {
		t.Errorf("留痕 = 批%d 摘要%d want 2/2：越界那一行没落进后一批，中途改数据这一幕没被分批承接",
			run.Batches, run.Digests)
	}
	// 核心判据：rows_before 记的是**入口那一瞬**数到的行，不是收口时一共删了多少。
	if run.RowsBefore != 5 {
		t.Errorf("rows_before=%d want 5：入口计数被写成了累计删除数，"+
			"「裁剪进行中才冒出来的行」被算进『一开始就该走』的那一批，rows_before==rows_pruned 从此恒真",
			run.RowsBefore)
	}
	if gap := run.RowsPruned - run.RowsBefore; gap != 1 {
		t.Errorf("实删 %d 与入口 %d 相差 %d want 1：那 1 行越界补写没在留痕上分出「入口」与「后到」两截",
			run.RowsPruned, run.RowsBefore, gap)
	}
	if left := a6CountLog(t, db, a6SessionLate); left != 0 {
		t.Errorf("本 session 还剩 %d 行 want 0：窗口没跑空就写了留痕", left)
	}
}

// TestA6PruneRollsBackWhenDigestUnderstatesDeletedRows 钉住 `if res.RowsAffected != w.RowCount
// { 撤销本批 }`（command_log.go 里那句"摘要一旦可以说谎，它就不是凭据"）。
//
// 为什么既有各格钉不住它（C 相实跑：电池 D23 注 `&& false` 短路守卫后全绿）：
// 本仓所有裁剪夹具都走「没人中途改数据」的成功路径，窗口那一刻声明的行数与 DELETE 实删的行数
// 在那一幕里恒等 ⇒ 摘掉守卫库里读数一字不变。要钉它得造出「窗口看少了一行」的那一幕。
//
// 为什么这一幕必须**跨两条语句**（第一版写在库里被实测否掉）：
// 原先的形状是同一条 DELETE 少删一行——BEFORE DELETE 触发器在删掉首行时把末行的 created_at 推到
// 未来。实跑红在控制组（未注码）上：
//
//	retention_a6_test.go:903: 撤销原因不含 "摘要声明 3 行、实删 2 行"：
//	ERROR: tuple to be updated was already modified by an operation triggered by the current command (SQLSTATE 27000)
//
// 即 PG 禁止一条命令改动「已被本命令触发的操作改过」的元组 ⇒ 同一条语句内的回推根本走不到
// 行数比较那一步，报的是数据库的错、不是守卫的错。于是把分家挪到**同一事务的两条语句之间**：
// 窗口按 session_id 升序返回，本事务循环也就按这个顺序逐 session「先落摘要、再删行」。
//   - actor 会话（id 小）先被裁：它的那条 DELETE 触发 AFTER DELETE 触发器，把**另一个**会话
//     （victim）里一条**未到期、id 落在它区间中间**的行补写成超期；
//   - victim 的摘要用的是**进事务之前**那次窗口扫描的读数（2 行），而它的 DELETE 随后扫到 3 行
//     ⇒ 声明 2、实删 3 ⇒ 守卫开火、整个事务回滚（两条摘要与所有行都回到动手之前）。
//
// 与 TestA6RunRowCountsEntryWindowNotLateCrossing 是同一套机制的**两幕**：那条的越界行落进
// **下一批**（分家发生在批之间 ⇒ 它管 rows_before 记的是入口那一瞬），这条的越界行落进
// **同一个 session 的当前批**（分家发生在摘要与删除之间 ⇒ 这条管描述失实的那一批不许留凭据）。
// 触发器必须只在 actor 被删时开火一次：AFTER ROW 会为每条被删行各触发一次，同一个 UPDATE 第二次
// 就撞上上面那条 27000（victim 自己的三行由 session_id 判据挡在门外）。
func TestA6PruneRollsBackWhenDigestUnderstatesDeletedRows(t *testing.T) {
	db := a6DB(t, &model.BrowserCommandLog{}, &model.BrowserAuditDigest{}, &model.BrowserAuditPruneRun{})

	victim := []*model.BrowserCommandLog{
		a6Row(a6SessionCrossed, 1, "click", `{"v":1}`),
		a6Row(a6SessionCrossed, 2, "click", `{"v":2}`), // 入口这一刻未到期，删除时被补写成超期
		a6Row(a6SessionCrossed, 3, "click", `{"v":3}`),
	}
	actor := a6Row(a6SessionActor, 1, "click", `{"a":1}`)
	a6Seed(t, db, append(victim, actor)...)
	late := victim[1]
	// 把中间那行推回未到期（a6Seed 按 session 整片位移，故按 id 点名）。
	if err := db.Model(&model.BrowserCommandLog{}).Where("id = ?", late.ID).
		Update("created_at", time.Now()).Error; err != nil {
		t.Fatalf("回推未到期失败：%v", err)
	}
	// 行序前提（三条都在代码里自证，不成立就停机，别把没装上的夹具读成没牙的守卫）：
	//   - late 的 id 必须夹在 victim 两条超期行之间，才落在窗口那一段的 `id <= max_id` 里；
	//   - actor 的 id 必须最大，高水位取的是**入口那一刻**超期行的 max(id)，late 入口未到期，
	//     没有比它更大的超期行它就永远进不了删除谓词。
	if !(victim[0].ID < late.ID && late.ID < victim[2].ID && victim[2].ID < actor.ID) {
		t.Fatalf("行序前提不成立：victim=%d/%d/%d actor=%d，要的是 v1 < late < v3 < actor",
			victim[0].ID, late.ID, victim[2].ID, actor.ID)
	}

	install := fmt.Sprintf(`CREATE FUNCTION a6_guard_cross_fn() RETURNS trigger AS $fn$
BEGIN
	IF OLD.session_id = %d THEN
		UPDATE browser_command_log SET created_at = now() - interval '300 days' WHERE id = %d AND session_id = %d;
	END IF;
	RETURN OLD;
END $fn$ LANGUAGE plpgsql`, a6SessionActor, late.ID, a6SessionCrossed)
	trigger := `CREATE TRIGGER a6_guard_cross AFTER DELETE ON browser_command_log ` +
		`FOR EACH ROW EXECUTE FUNCTION a6_guard_cross_fn()`
	// 池连接跑在 session_replication_role='replica' 下，默认触发器静默不触发，必须 ENABLE ALWAYS
	// （同上一条腿的实测）。没启用＝守卫从未被要求开火，本腿会把它读成「守卫没牙」。
	always := `ALTER TABLE browser_command_log ENABLE ALWAYS TRIGGER a6_guard_cross`
	if err := db.Exec(install).Error; err != nil {
		t.Fatalf("造跨会话补写触发器函数失败（本腿前提）：%v", err)
	}
	if err := db.Exec(trigger).Error; err != nil {
		t.Fatalf("建跨会话补写触发器失败（本腿前提）：%v", err)
	}
	if err := db.Exec(always).Error; err != nil {
		t.Fatalf("启用 ALWAYS 触发器失败：%v —— 夹具没装上，本腿无判据", err)
	}
	t.Cleanup(func() {
		if err := db.Exec(`DROP TRIGGER IF EXISTS a6_guard_cross ON browser_command_log`).Error; err != nil {
			t.Errorf("删除跨会话补写触发器失败，同库后续用例会撞上：%v", err)
		}
		if err := db.Exec(`DROP FUNCTION IF EXISTS a6_guard_cross_fn()`).Error; err != nil {
			t.Errorf("删除跨会话补写函数失败：%v", err)
		}
	})

	pruned, err := NewBrowserCommandLogRepositoryWithDB(db).
		PruneBefore(context.Background(), a6Cutoff(), a6Source)
	// 判据分两层：先要「撤销」这件事发生，再要它**是因为**行数不符。只判 err != nil 的话，
	// 「触发器自己报错回滚」与「行数守卫开火」在库里同形（行都在、摘要为 0），
	// 那类假绿会把没生效的夹具读成生效（第一版正是靠这层读出 27000 的）。
	if err == nil {
		t.Fatalf("PruneBefore 没报错（pruned=%d）：摘要声明 2 行、实删 3 行的那一批照样提交 ⇒ 行数守卫没牙", pruned)
	}
	if want := "摘要声明 2 行、实删 3 行"; !strings.Contains(err.Error(), want) {
		t.Fatalf("撤销原因不含 %q：%v —— 报的是别的原因（触发器/约束/高水位），本腿的对照对象没成立", want, err)
	}
	if pruned != 0 {
		t.Errorf("返回值 pruned=%d want 0：撤销了本批却把行数报了出去", pruned)
	}
	if left := a6CountLog(t, db, a6SessionCrossed); left != 3 {
		t.Errorf("victim 还剩 %d 行 want 3：声明与实删不符时那一批没被整批撤销", left)
	}
	if left := a6CountLog(t, db, a6SessionActor); left != 1 {
		t.Errorf("actor 还剩 %d 行 want 1：victim 那一批失实，同事务里先提交的 actor 也该一起回滚", left)
	}
	if got := a6Digests(t, db, a6SessionCrossed); len(got) != 0 {
		t.Errorf("victim 摘要行数 %d want 0：%+v —— 一条描述失实的摘要留在了永不裁剪的表里，它就不是凭据了", len(got), got)
	}
	if got := a6Digests(t, db, a6SessionActor); len(got) != 0 {
		t.Errorf("actor 摘要行数 %d want 0：%+v —— 回滚只滚了一半", len(got), got)
	}
	if got := a6RunRows(t, db); len(got) != 0 {
		t.Errorf("扫描留痕行数 %d want 0：%+v —— 中途撤销的裁剪留下一行看着像跑完的账", len(got), got)
	}
}
