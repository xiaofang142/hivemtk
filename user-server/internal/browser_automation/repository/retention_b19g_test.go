package repository

// 批19g 契约锁：治理裁剪必须真的分批。
//
// retention.go 对运维的承诺是「分批避免长事务锁」（G19），PruneSnapshotText 的注释也写着
// 「分批 5000」——但它的实现是一条不带批的 UPDATE：首次上线时表里攒着多少超期行，
// 就在一个事务里重写多少行（快照每行可到 64 KiB，十万行就是 GB 级 WAL + 全程持锁）。
// 注释与实现不符的坏处不在注释，在于读它的人以为这件事已经被处理过了。
//
// 两条腿各自独立：plan 侧（清文本）与 command_log 侧（删行）是两处实现，
// 只修一处另一处照样裸奔；command_log 侧现在是对的，这条锁是防止它被「简化」回去。
//
// 观测方式：往 gorm 的 update/delete 回调链上挂一个计数器——一次回调 = 实际发出的一条语句。
// 不数语句就看不见分批：只看结果的话，一条 UPDATE 和两条 UPDATE 都是「全清干净」，永远绿。
// 种子取「一批 + 1 行」（直接用生产常量 pruneBatchRows）：批量改了种子跟着改，
// 不会出现「断言的已经不是那件事」的漂移。

import (
	"context"
	"sync"
	"testing"
	"time"

	"hivemtk-user/internal/browser_automation/model"
	"hivemtk-user/internal/pkg/testutil"

	"gorm.io/gorm"
)

const (
	b19gOldTask   = uint(781501)
	b19gFreshTask = uint(781502)
	b19gText      = "snapshot-payload"
	b19gDays      = 200
	// b19gSource：批23 起 PruneBefore 要求写明界的来源（见 retention_a6_test.go 的 a6Source）。
	b19gSource = "test:retention_b19g"
)

func b19gDB(t *testing.T, models ...any) *gorm.DB {
	t.Helper()
	db := testutil.NewTestDB(t, models...)
	if db == nil {
		t.Fatal("测试库不可达：裁剪的分批口径无法判定（不 Skip，跳过等于没锁）")
	}
	return db
}

// b19gStmts 发往某张表的语句统计：条数 + 单条最多动了几行。
type b19gStmts struct {
	count int
	max   int64
}

// b19gWatchStatements 往 gorm 的 update/delete 回调链上挂计数器，一次回调=实际发出的一条语句。
func b19gWatchStatements(t *testing.T, db *gorm.DB, table string) func() b19gStmts {
	t.Helper()
	var mu sync.Mutex
	var seen b19gStmts
	hit := func(tx *gorm.DB) {
		if tx.Statement.Table != table {
			return
		}
		mu.Lock()
		defer mu.Unlock()
		seen.count++
		if tx.RowsAffected > seen.max {
			seen.max = tx.RowsAffected
		}
	}
	if err := db.Callback().Update().After("gorm:update").Register("b19g:update", hit); err != nil {
		t.Fatalf("挂 update 计数回调失败：%v", err)
	}
	if err := db.Callback().Delete().After("gorm:delete").Register("b19g:delete", hit); err != nil {
		t.Fatalf("挂 delete 计数回调失败：%v", err)
	}
	t.Cleanup(func() {
		_ = db.Callback().Update().Remove("b19g:update")
		_ = db.Callback().Delete().Remove("b19g:delete")
	})
	return func() b19gStmts {
		mu.Lock()
		defer mu.Unlock()
		return seen
	}
}

func b19gCutoff() time.Time { return time.Now().AddDate(0, 0, -b19gDays) }

// b19gShiftOld 把本用例那批种子的 created_at 推到保留期之前。
// 用一条 UPDATE 位移而不是逐行写死时间：逐行 Create 会先把测试自己拖死。
// 必须在挂计数器之前调用，否则这条位移会被算进「批数」。
func b19gShiftOld(t *testing.T, db *gorm.DB, dest any, where string, args ...any) {
	t.Helper()
	if err := db.Model(dest).Where(where, args...).
		Update("created_at", b19gCutoff().Add(-24*time.Hour)).Error; err != nil {
		t.Fatalf("位移 created_at 失败：%v", err)
	}
}

// assertBatched 分批的真正判据：一条语句动过的行数不得超过一批。
// 光数语句条数是不够的——收敛用的那条「RowsAffected==0」空跑本身就会多发一条，
// 于是「批放大到整表」也照样 ≥2 条，门槛恒真（第一版就是这么漏掉 H1/H5 两格的）。
// 上下界都要有：单批不得越界（上界），且这批数据确实必须分多句才走得完（下界）。
func assertBatched(t *testing.T, got b19gStmts) {
	t.Helper()
	if got.max > pruneBatchRows {
		t.Errorf("单条语句一次动了 %d 行，超过分批上限 %d：等于整表在一句里吃完，"+
			"正是 retention.go 说要避免的长事务锁", got.max, pruneBatchRows)
	}
	if got.count < 2 {
		t.Errorf("裁剪 %d 行只发了 %d 条语句，want ≥2：分批根本没发生", pruneBatchRows+1, got.count)
	}
}

func TestB19GPlanSnapshotPruneIsBatched(t *testing.T) {
	db := b19gDB(t, &model.BrowserLLMPlan{})
	ctx := context.Background()

	// 5001 行超期 + 2 行未超期：未超期那两条是「分批改写时别把 cutoff 谓词丢了」的反证。
	rows := make([]*model.BrowserLLMPlan, 0, pruneBatchRows+1)
	for i := 0; i < pruneBatchRows+1; i++ {
		rows = append(rows, &model.BrowserLLMPlan{TaskID: b19gOldTask, Goal: "old", Snapshot: b19gText})
	}
	if err := db.CreateInBatches(rows, 500).Error; err != nil {
		t.Fatalf("种子超期计划落库失败：%v", err)
	}
	b19gShiftOld(t, db, &model.BrowserLLMPlan{}, "task_id = ?", b19gOldTask)
	for i := 0; i < 2; i++ {
		if err := db.Create(&model.BrowserLLMPlan{TaskID: b19gFreshTask, Goal: "fresh", Snapshot: b19gText}).Error; err != nil {
			t.Fatalf("种子未超期计划落库失败：%v", err)
		}
	}

	count := b19gWatchStatements(t, db, "browser_llm_plans")
	cleared, err := NewBrowserLLMPlanRepositoryWithDB(db).PruneSnapshotText(ctx, b19gCutoff())
	if err != nil {
		t.Fatalf("PruneSnapshotText 报错：%v", err)
	}
	if cleared != int64(pruneBatchRows+1) {
		t.Errorf("清空行数 %d want %d（多出来的正是未到期那 2 行被一起清了）", cleared, pruneBatchRows+1)
	}
	assertBatched(t, count())

	var left int64
	if err := db.Model(&model.BrowserLLMPlan{}).
		Where("task_id = ? AND snapshot <> ''", b19gOldTask).Count(&left).Error; err != nil {
		t.Fatalf("数超期计划失败：%v", err)
	}
	if left != 0 {
		t.Errorf("还有 %d 行超期快照没清：分批循环没跑到干净就返回了", left)
	}
	var kept int64
	if err := db.Model(&model.BrowserLLMPlan{}).
		Where("task_id = ? AND snapshot <> ''", b19gFreshTask).Count(&kept).Error; err != nil {
		t.Fatalf("数未超期计划失败：%v", err)
	}
	if kept != 2 {
		t.Errorf("未超期计划的快照被清了（剩 %d 条带文本，want 2）：分批谓词丢了 cutoff", kept)
	}
}

func TestB19GCommandLogPruneIsBatched(t *testing.T) {
	// 批22 之后 PruneBefore 先把摘要落库再删行，摘要写不进去就整批回滚：
	// 这两张表本用例建不了、也必须建（缺表时本腿会以「PruneBefore 报错」的形式红，
	// 那正是 a6 那条 fail-close 腿在判的事）。
	db := b19gDB(t, &model.BrowserCommandLog{}, &model.BrowserAuditDigest{}, &model.BrowserAuditPruneRun{})
	ctx := context.Background()

	rows := make([]*model.BrowserCommandLog, 0, pruneBatchRows+1)
	for i := 0; i < pruneBatchRows+1; i++ {
		rows = append(rows, &model.BrowserCommandLog{
			SessionID: b19gOldTask, TaskID: b19gOldTask, Seq: i,
			Direction: "command", Action: "click",
		})
	}
	if err := db.CreateInBatches(rows, 500).Error; err != nil {
		t.Fatalf("种子超期日志落库失败：%v", err)
	}
	b19gShiftOld(t, db, &model.BrowserCommandLog{}, "session_id = ?", b19gOldTask)
	for i := 0; i < 2; i++ {
		fresh := &model.BrowserCommandLog{SessionID: b19gFreshTask, TaskID: b19gFreshTask,
			Seq: i, Direction: "command", Action: "click"}
		if err := db.Create(fresh).Error; err != nil {
			t.Fatalf("种子未超期日志落库失败：%v", err)
		}
	}

	count := b19gWatchStatements(t, db, "browser_command_log")
	deleted, err := NewBrowserCommandLogRepositoryWithDB(db).PruneBefore(ctx, b19gCutoff(), b19gSource)
	if err != nil {
		t.Fatalf("PruneBefore 报错：%v", err)
	}
	if deleted != int64(pruneBatchRows+1) {
		t.Errorf("删除行数 %d want %d", deleted, pruneBatchRows+1)
	}
	assertBatched(t, count())

	var left int64
	if err := db.Model(&model.BrowserCommandLog{}).
		Where("session_id = ?", b19gOldTask).Count(&left).Error; err != nil {
		t.Fatalf("数超期日志失败：%v", err)
	}
	if left != 0 {
		t.Errorf("还有 %d 行超期日志没删：分批循环没跑到干净就返回了", left)
	}
	var kept int64
	if err := db.Model(&model.BrowserCommandLog{}).
		Where("session_id = ?", b19gFreshTask).Count(&kept).Error; err != nil {
		t.Fatalf("数未超期日志失败：%v", err)
	}
	if kept != 2 {
		t.Errorf("未超期日志被删了（剩 %d 条，want 2）：分批谓词丢了 cutoff", kept)
	}
}
