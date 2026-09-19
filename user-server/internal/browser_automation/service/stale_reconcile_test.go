package service

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"hivemtk-user/internal/browser_automation/model"
	"hivemtk-user/internal/browser_automation/repository"
	"hivemtk-user/internal/pkg/testutil"

	"gorm.io/gorm"
)

// stale_reconcile_test.go — 批8 任务快照对账器契约锁。
//
// 对账器是「只在异常路径上生效」的后台职责，正常执行永远不触发它，所以这里的用例
// 全是刻意造异常态：砖化 running、僵尸 active 会话、预算内的活任务、非 running 的真实终态。
// 判据写反的两个方向都要锁——该收敛的不收敛（任务永久砖化），不该收敛的收敛了
// （把正在等人放行的 D7 任务判死，等于用对账器亲手破坏 D7）。

type reconcileBundle struct {
	ctx      context.Context
	db       *gorm.DB
	taskRepo repository.BrowserTaskRepository
	sessRepo repository.BrowserSessionRepository
}

func newReconcileDB(t *testing.T) *reconcileBundle {
	t.Helper()
	db := testutil.NewTestDB(t, &model.BrowserTask{}, &model.BrowserSession{})
	if db == nil {
		t.Skip("测试库不可达")
	}
	return &reconcileBundle{ctx: context.Background(), db: db,
		taskRepo: repository.NewBrowserTaskRepositoryWithDB(db),
		sessRepo: repository.NewBrowserSessionRepositoryWithDB(db)}
}

// mustExec 裸 SQL 落库失败即停：造不出前置状态时，后面的断言全是无意义的假绿。
func mustExec(t *testing.T, res *gorm.DB) {
	t.Helper()
	if res.Error != nil {
		t.Fatal(res.Error)
	}
}

// readTask 用独立零值 struct 读回（复用已填充的 struct 再 First 会把旧字段并入 WHERE）。
func readTask(t *testing.T, b *reconcileBundle, id uint) *model.BrowserTask {
	t.Helper()
	var g model.BrowserTask
	if err := b.db.First(&g, id).Error; err != nil {
		t.Fatal(err)
	}
	return &g
}

func readSession(t *testing.T, b *reconcileBundle, id uint) *model.BrowserSession {
	t.Helper()
	var s model.BrowserSession
	if err := b.db.First(&s, id).Error; err != nil {
		t.Fatal(err)
	}
	return &s
}

// mkRunningTask 造一个「running 且 updated_at 老于 age」的任务快照（进程重启后的砖态形状）。
func mkRunningTask(t *testing.T, b *reconcileBundle, userID uint, age time.Duration,
	timeoutSec int, requireConfirm bool, confirmWaitSec int) *model.BrowserTask {
	t.Helper()
	task := &model.BrowserTask{
		Name: fmt.Sprintf("reconcile-%d", time.Now().UnixNano()), TaskType: "one_shot", Status: "running",
		Url: "https://example.com", UserID: userID, Platform: "fixture",
		TimeoutSec: timeoutSec, RequireConfirm: requireConfirm, ConfirmWaitSec: confirmWaitSec,
	}
	if err := b.taskRepo.Create(b.ctx, task); err != nil {
		t.Fatal(err)
	}
	// updated_at 必须裸 SQL 改：走 GORM 会被 autoUpdateTime 重新盖成 now，造不出「老快照」
	mustExec(t, b.db.Exec("UPDATE browser_tasks SET updated_at = ? WHERE id = ?", time.Now().Add(-age), task.ID))
	return task
}

func mkSession(t *testing.T, b *reconcileBundle, taskID, userID uint,
	status, errMsg string, age time.Duration) *model.BrowserSession {
	t.Helper()
	s := &model.BrowserSession{TaskID: taskID, UserID: userID, Status: status, ErrorMsg: errMsg,
		Url: "https://example.com"}
	if err := b.sessRepo.Create(b.ctx, s); err != nil {
		t.Fatal(err)
	}
	if age > 0 {
		mustExec(t, b.db.Exec("UPDATE browser_sessions SET created_at = ? WHERE id = ?", time.Now().Add(-age), s.ID))
	}
	return s
}

//  1. 有终态会话可依据 → 按会话真实终态回填（completed=done / failed=failed 且带上会话原文），
//     last_result 必须点明回填来源 session（运维要能追回「这个 done 是对账给的还是执行写的」）。
func TestReconcileBackfillsFromTerminalSession(t *testing.T) {
	b := newReconcileDB(t)
	userID := uint(771001)

	ok := mkRunningTask(t, b, userID, 5*time.Minute, 120, false, 0)
	mkSession(t, b, ok.ID, userID, "completed", "", 5*time.Minute)
	bad := mkRunningTask(t, b, userID, 5*time.Minute, 120, false, 0)
	mkSession(t, b, bad.ID, userID, "failed", "平台返回：内容含敏感词", 5*time.Minute)

	if n := reconcileStaleTasks(b.ctx, b.taskRepo, b.sessRepo); n != 2 {
		t.Fatalf("本轮应收敛 2 个，got %d", n)
	}
	gOK := readTask(t, b, ok.ID)
	if gOK.Status != "done" || !strings.Contains(gOK.LastResult, fmt.Sprintf("session=%d", ok.ID)) {
		t.Errorf("completed 会话应回填 done 且带来源，got status=%s last_result=%q", gOK.Status, gOK.LastResult)
	}
	gBad := readTask(t, b, bad.ID)
	if gBad.Status != "failed" || gBad.ErrorMsg != "平台返回：内容含敏感词" {
		t.Errorf("failed 会话应回填 failed 并沿用会话归因，got status=%s err=%q", gBad.Status, gBad.ErrorMsg)
	}
	// 幂等：再扫一轮不该有变化（status 已非 running，粗筛就排除了）
	if n := reconcileStaleTasks(b.ctx, b.taskRepo, b.sessRepo); n != 0 {
		t.Errorf("已收敛任务不得重复回填，got %d", n)
	}
}

//  2. 僵尸会话必须一起收口：task 回填了而 session 还挂着 active，等于用户侧并发闸
//     （CountRunningByUser / CountRunningByTask）被永久占住——再也发不动任务且报错指向不存在的执行。
func TestReconcileReleasesZombieSessionFromBusyGate(t *testing.T) {
	b := newReconcileDB(t)
	userID := uint(771002)
	task := mkRunningTask(t, b, userID, 5*time.Minute, 120, false, 0)
	zombie := mkSession(t, b, task.ID, userID, "active", "", 5*time.Minute)
	mkSession(t, b, task.ID, userID, "created", "", 5*time.Minute)

	if n := reconcileStaleTasks(b.ctx, b.taskRepo, b.sessRepo); n != 1 {
		t.Fatalf("本轮应收敛 1 个，got %d", n)
	}
	s := readSession(t, b, zombie.ID)
	if s.Status != "failed" || !strings.Contains(s.ErrorMsg, "会话未回写终态") {
		t.Errorf("僵尸会话应收敛为 failed+对账归因，got %s/%q", s.Status, s.ErrorMsg)
	}
	if s.CompletedAt == nil {
		t.Error("收口必须落 completed_at（监控/时长统计按它算，缺了就是永久在跑）")
	}
	if n, err := b.sessRepo.CountRunningByUser(b.ctx, userID); err != nil || n != 0 {
		t.Errorf("对账后并发闸必须释放，got running=%d err=%v", n, err)
	}
	if n, err := b.sessRepo.CountRunningByTask(b.ctx, userID, task.ID); err != nil || n != 0 {
		t.Errorf("同任务幂等闸必须释放，got running=%d err=%v", n, err)
	}
	g := readTask(t, b, task.ID)
	// 回填值与会话事实同源：会话刚被记 failed（含对账原文），task 就不能报「无可依据的终态会话」
	if g.Status != "failed" || strings.Contains(g.ErrorMsg, "无可依据") || !strings.Contains(g.LastResult, "对账回填自 session") {
		t.Errorf("task 快照应跟随刚收口的会话，got status=%s err=%q last=%q", g.Status, g.ErrorMsg, g.LastResult)
	}
}

//  3. 无任何会话可依据（跑到落 session 之前就崩了）→ failed + 自证归因，
//     绝不能留在 running，也不能伪装成「平台失败」。
func TestReconcileWithoutAnySession(t *testing.T) {
	b := newReconcileDB(t)
	userID := uint(771003)
	task := mkRunningTask(t, b, userID, 5*time.Minute, 120, false, 0)

	if n := reconcileStaleTasks(b.ctx, b.taskRepo, b.sessRepo); n != 1 {
		t.Fatalf("无会话也要收敛，got %d", n)
	}
	g := readTask(t, b, task.ID)
	if g.Status != "failed" || !strings.Contains(g.ErrorMsg, staleReconcileNote) {
		t.Errorf("got status=%s err=%q", g.Status, g.ErrorMsg)
	}
	if !strings.Contains(g.LastResult, "无可依据的终态会话") {
		t.Errorf("last_result 要写明无依据，got %q", g.LastResult)
	}
}

//  4. 预算内不得动（对账器的误杀面）：
//     ① 老于粗筛下限、但仍在自己的 timeout_sec 内；
//     ② D7 任务——timeout_sec 很短而确认等待很长，正在等人放行。
//     第 ② 条就是批8 解耦的反证：预算若不含 confirm_wait_sec，一个只是等人点确认的任务会被对账判死。
func TestReconcileSkipsTasksWithinOwnBudget(t *testing.T) {
	b := newReconcileDB(t)
	userID := uint(771004)

	plain := mkRunningTask(t, b, userID, 90*time.Second, 3600, false, 0) // 1h 预算，90s 快照
	mkSession(t, b, plain.ID, userID, "active", "", 90*time.Second)
	// 预算 60s + 确认 900s ≈ 16min，快照已 300s 老：只看 timeout_sec 就会被判僵尸
	d7 := mkRunningTask(t, b, userID, 300*time.Second, 60, true, 900)
	mkSession(t, b, d7.ID, userID, "active", "", 300*time.Second)

	if n := reconcileStaleTasks(b.ctx, b.taskRepo, b.sessRepo); n != 0 {
		t.Fatalf("预算内任务一个都不许收敛，got %d", n)
	}
	for _, id := range []uint{plain.ID, d7.ID} {
		if g := readTask(t, b, id); g.Status != "running" {
			t.Errorf("task=%d 仍在预算内却被改成 %s", id, g.Status)
		}
	}
	if n, _ := b.sessRepo.CountRunningByUser(b.ctx, userID); n != 2 {
		t.Errorf("在途会话不该被动，got running=%d", n)
	}

	// 反向：把 D7 那条的确认预算撤掉（require_confirm=false）→ 同样 300s 快照就越界了，
	// 证明闸门真的是「预算」而不是「写死了个大数」
	mustExec(t, b.db.Exec("UPDATE browser_tasks SET require_confirm = false, updated_at = ? WHERE id = ?",
		time.Now().Add(-300*time.Second), d7.ID))
	if n := reconcileStaleTasks(b.ctx, b.taskRepo, b.sessRepo); n != 1 {
		t.Fatalf("去掉确认预算后应恰好收敛 1 个，got %d", n)
	}
}

// 5) 条件更新不可省：对账器与执行协程同刻收口时，真实终态必须赢过对账值。
func TestReconcileRunResultDoesNotClobberRealTerminal(t *testing.T) {
	b := newReconcileDB(t)
	task := &model.BrowserTask{Name: "reconcile-clobber", TaskType: "one_shot", Status: "done",
		Url: "https://example.com", UserID: 771005, Platform: "fixture", TimeoutSec: 120, LastResult: "真实终态"}
	if err := b.taskRepo.Create(b.ctx, task); err != nil {
		t.Fatal(err)
	}
	ok, err := b.taskRepo.ReconcileRunResult(b.ctx, task.ID, "failed", "对账回填", staleReconcileNote)
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Error("非 running 行不得被对账覆盖（RowsAffected 必须为 0）")
	}
	g := readTask(t, b, task.ID)
	if g.Status != "done" || g.LastResult != "真实终态" {
		t.Errorf("真实终态被盖掉：status=%s last_result=%q", g.Status, g.LastResult)
	}
}

//  6. FailStaleUnfinished 的下限：新于 before 的在途会话不许碰（正在被新一轮使用），
//     已终态的不得改写，别的任务的不得波及。
func TestFailStaleUnfinishedRespectsFloor(t *testing.T) {
	b := newReconcileDB(t)
	userID := uint(771006)
	task := &model.BrowserTask{Name: "reconcile-floor", TaskType: "one_shot", Status: "running",
		Url: "https://example.com", UserID: userID, Platform: "fixture", TimeoutSec: 120}
	if err := b.taskRepo.Create(b.ctx, task); err != nil {
		t.Fatal(err)
	}
	fresh := mkSession(t, b, task.ID, userID, "active", "", 0)
	old := mkSession(t, b, task.ID, userID, "created", "", 5*time.Minute)
	already := mkSession(t, b, task.ID, userID, "completed", "", 5*time.Minute)

	n, err := b.sessRepo.FailStaleUnfinished(b.ctx, task.ID, time.Now().Add(-staleScanFloorAge), staleSessionNote)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Errorf("只应收敛老且在途的那条，got %d", n)
	}
	if g := readSession(t, b, fresh.ID); g.Status != "active" {
		t.Errorf("新鲜会话被误杀：%s", g.Status)
	}
	if g := readSession(t, b, old.ID); g.Status != "failed" || g.CompletedAt == nil {
		t.Errorf("老会话未收口：%s/%v", g.Status, g.CompletedAt)
	}
	if g := readSession(t, b, already.ID); g.Status != "completed" {
		t.Errorf("已终态会话不得被改写：%s", g.Status)
	}
	// 跨任务不得波及
	other := mkSession(t, b, task.ID+1, userID, "active", "", 5*time.Minute)
	if _, err := b.sessRepo.FailStaleUnfinished(b.ctx, task.ID, time.Now().Add(-time.Hour), staleSessionNote); err != nil {
		t.Fatal(err)
	}
	if g := readSession(t, b, other.ID); g.Status != "active" {
		t.Errorf("对账越界改了别的任务的会话：%s", g.Status)
	}
}

//  7. FindStaleRunningAll 的粗筛下限：刚起步的任务不得进入对账候选。
//     每任务的真实预算门槛（taskExecBudget）确实能挡住误杀，但候选集一旦不限时间，
//     对账器每轮都会把全库 running 读进内存逐条判——活跃任务越多、扫描越慢，
//     且「门槛写错」的爆炸半径从「老快照」扩大到「所有在途任务」。
func TestReconcileFindStaleRunningAllRespectsFloor(t *testing.T) {
	b := newReconcileDB(t)
	userID := uint(771007)

	aged := mkRunningTask(t, b, userID, 5*time.Minute, 120, false, 0)
	fresh := mkRunningTask(t, b, userID, 0, 120, false, 0)
	// 非 running 的老任务同样不得进候选（对账只管在途快照）
	done := mkRunningTask(t, b, userID, 5*time.Minute, 120, false, 0)
	mustExec(t, b.db.Model(&model.BrowserTask{}).Where("id = ?", done.ID).Update("status", "done"))

	list, err := b.taskRepo.FindStaleRunningAll(b.ctx, staleScanFloorAge, 100)
	if err != nil {
		t.Fatal(err)
	}
	got := map[uint]bool{}
	for _, g := range list {
		got[g.ID] = true
	}
	if !got[aged.ID] {
		t.Errorf("老快照应进候选，ids=%v", got)
	}
	if got[fresh.ID] {
		t.Error("刚创建的任务进了候选（粗筛漏了时间下限）")
	}
	if got[done.ID] {
		t.Error("非 running 的任务进了候选")
	}
}
