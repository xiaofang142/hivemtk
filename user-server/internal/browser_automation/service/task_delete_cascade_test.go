package service

// 契约锁：删除任务必须让它名下仍在途的会话收口。
//
// 立项依据（活库实测）：DELETE /tasks/552 回 200「已删除」，而该任务的 3 条会话、27 条步、
// 84 条命令流一行没动，其中 1 条永远停在 active。全库同类孤儿会话 46 条。两个后果都是
// 用户直接能撞上的：
//  1. CountRunningByUser 按 created/active 计数 ⇒ 一条在途会话把用户的并发闸永久占住
//     （ErrUserBusy 反复报「已有浏览器任务执行中」，而那个任务在列表里已经不存在了）；
//     对账器（stale_reconcile）本应兜这一格，但它按「running 的任务」选行，任务一旦被软删
//     就永远扫不到 ⇒ 删除动作亲手把唯一的兜底路径关掉了。
//  2. 监控页（ListByUser 只过滤 user_id）永远列着一份读不到的执行历史。
//
// 收敛口径分两半，不是偷懒：
//   - 执行协程**活着**（本进程 stopRegistry 有这一条）：只发中止信号，不写终态。
//     提前写终态等于提前放闸，而那个协程还在往同一条 Host 连接发命令——正是
//     ErrUserBusy 要防的「命令逻辑级交织」。终态归执行方写（与 POST /sessions/:id/stop 同构）。
//   - 执行协程**不在本进程**（进程重启后留下的僵尸会话，即本次实测的形态）：没有人在跑，
//     由删除动作就地收口，状态取 stopped 而非 failed——删任务是用户主动动作，
//     「系统跑失败」是另一类事实（与执行方「用户叫停记 stopped、失败记 failed」的分档同口径）。
//
// 已终态的会话一个都不许改写：那是这个任务真实跑过的凭据，删除任务不等于销毁审计事实
// （command_log 的 append-only 立场在同一处）。

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

const (
	dcUser    = uint(792001)
	dcUserB   = uint(792002)
	dcSessA   = uint(792101) // 在途（active），执行协程不在本进程
	dcSessB   = uint(792102) // 在途（created，排队中就被删）
	dcSessC   = uint(792103) // 已终态 completed，不许被删任务改写
	dcSessD   = uint(792104) // 别的任务的在途会话，不许被牵连
	dcSessE   = uint(792105) // 在途且执行协程活着，只该收到信号
	dcTaskA   = uint(792201)
	dcTaskB   = uint(792202)
	dcDeleted = "任务已删除"
)

type dcBundle struct {
	ctx      context.Context
	db       *gorm.DB
	taskRepo repository.BrowserTaskRepository
	sessRepo repository.BrowserSessionRepository
}

func dcDB(t *testing.T) *dcBundle {
	t.Helper()
	db := testutil.NewTestDB(t, &model.BrowserTask{}, &model.BrowserSession{})
	if db == nil {
		t.Fatal("测试库不可达：级联收口与并发闸都判不了（Skip 等于没锁）")
	}
	return &dcBundle{ctx: context.Background(), db: db,
		taskRepo: repository.NewBrowserTaskRepositoryWithDB(db),
		sessRepo: repository.NewBrowserSessionRepositoryWithDB(db)}
}

func dcTask(t *testing.T, b *dcBundle, id, userID uint) {
	t.Helper()
	// Status=ready：Delete 不看状态，任何态都能删（实测里 552 是 ready）。
	if err := b.db.Create(&model.BrowserTask{
		ID: id, Name: fmt.Sprintf("dc-%d", id), TaskType: "one_shot", Status: "ready",
		Url: "https://example.com", UserID: userID, Platform: "fixture", TimeoutSec: 60,
	}).Error; err != nil {
		t.Fatalf("种子任务 %d 落库失败: %v", id, err)
	}
}

// dcSession 铺一条会话；inFlight=true 时按 age 决定起点（排队态 started_at 为空）。
func dcSession(t *testing.T, b *dcBundle, id, taskID, userID uint, status string, startedAgo time.Duration) {
	t.Helper()
	s := model.BrowserSession{ID: id, TaskID: taskID, UserID: userID, Status: status,
		Url: "https://example.com", CreatedAt: time.Now().Add(-startedAgo)}
	if status == "active" || status == "completed" {
		at := time.Now().Add(-startedAgo)
		s.StartedAt = &at
	}
	if status == "completed" {
		at := time.Now()
		s.CompletedAt = &at
		s.DurationMs = int64(startedAgo / time.Millisecond)
		s.ErrorMsg = ""
	}
	if err := b.db.Create(&s).Error; err != nil {
		t.Fatalf("种子会话 %d 落库失败: %v", id, err)
	}
}

func dcRead(t *testing.T, b *dcBundle, id uint) *model.BrowserSession {
	t.Helper()
	var s model.BrowserSession
	if err := b.db.First(&s, id).Error; err != nil {
		t.Fatalf("读回会话 %d 失败: %v", id, err)
	}
	return &s
}

func dcSvc(b *dcBundle, exec *Executor) *TaskService {
	return NewTaskService(b.taskRepo, b.sessRepo, exec)
}

func dcBusy(t *testing.T, b *dcBundle, userID uint) int64 {
	t.Helper()
	n, err := b.sessRepo.CountRunningByUser(b.ctx, userID)
	if err != nil {
		t.Fatalf("CountRunningByUser 失败: %v", err)
	}
	return n
}

// 1) 在途会话（本进程无执行协程）必须随删除收口为 stopped，并释放并发闸。
// 这一格就是活库实测到的形状：删除前 active、删除后仍 active、闸门锁死。
func TestDeleteConvergesInFlightSessions(t *testing.T) {
	b := dcDB(t)
	dcTask(t, b, dcTaskA, dcUser)
	dcSession(t, b, dcSessA, dcTaskA, dcUser, "active", 4*time.Second)
	dcSession(t, b, dcSessB, dcTaskA, dcUser, "created", 2*time.Second)
	dcSession(t, b, dcSessD, dcTaskB, dcUser, "active", 3*time.Second)
	if got := dcBusy(t, b, dcUser); got != 3 {
		t.Fatalf("前置不成立：并发闸应被 3 条在途会话占住，got %d", got)
	}

	if err := dcSvc(b, nil).Delete(b.ctx, dcTaskA, dcUser); err != nil {
		t.Fatalf("Delete 失败: %v", err)
	}

	for _, id := range []uint{dcSessA, dcSessB} {
		got := dcRead(t, b, id)
		if got.Status != "stopped" {
			t.Errorf("会话 %d 删除后 status=%q，期望 stopped（在途会话必须随任务收口，否则并发闸永久占住）", id, got.Status)
		}
		if !strings.Contains(got.ErrorMsg, dcDeleted) {
			t.Errorf("会话 %d error_msg=%q，期望写明「%s」（用户要能看出这次执行是被删除动作终止的）", id, got.ErrorMsg, dcDeleted)
		}
		if got.CompletedAt == nil {
			t.Errorf("会话 %d completed_at 为空：stopped 不是终态收口，对账与监控页都按它判「还在跑」", id)
		}
	}
	// 闸门口径：本任务的两条已收口，只剩别任务的 1 条在途。
	if got := dcBusy(t, b, dcUser); got != 1 {
		t.Errorf("删除后用户并发闸仍被 %d 条占住，期望 1（只剩 dcTaskB 那条）", got)
	}
	if other := dcRead(t, b, dcSessD); other.Status != "active" {
		t.Errorf("别的任务的在途会话被牵连：status=%q，期望仍为 active", other.Status)
	}
}

// 2) 执行协程活着时只发信号、不写终态。
// 反向危害（这条锁真正防的东西）：就地写终态会把并发闸提前放开，用户立刻能再发一个任务，
// 而旧协程还在往同一条 Host 连接发命令 —— 命令交织在一个浏览器上，写操作不可撤回。
func TestDeleteSignalsLiveRunnerWithoutWritingTerminal(t *testing.T) {
	b := dcDB(t)
	dcTask(t, b, dcTaskA, dcUser)
	dcSession(t, b, dcSessE, dcTaskA, dcUser, "active", 5*time.Second)

	exec := NewExecutor(nil, b.sessRepo, repository.NewBrowserStepRepositoryWithDB(b.db), nil, nil)
	ch := exec.registerStop(dcSessE)
	t.Cleanup(func() { exec.unregisterStop(dcSessE) })

	if err := dcSvc(b, exec).Delete(b.ctx, dcTaskA, dcUser); err != nil {
		t.Fatalf("Delete 失败: %v", err)
	}
	if !exec.stopFired(ch) {
		t.Error("删除任务没有向在跑的执行协程发出中止信号：用户删掉任务后浏览器仍在被操作")
	}
	if got := dcRead(t, b, dcSessE); got.Status != "active" {
		t.Errorf("执行协程仍在本进程时删除动作抢先写了终态：status=%q，期望保持 active（终态归执行方，闸门口径见用例头）", got.Status)
	}
	if got := dcBusy(t, b, dcUser); got != 1 {
		t.Errorf("在跑的执行被提前放闸：CountRunningByUser=%d，期望 1", got)
	}
}

// 3) 已终态的会话不被改写：删除任务销毁的是任务，不是这个任务真实跑过的凭据。
func TestDeleteKeepsTerminalSessionRecords(t *testing.T) {
	b := dcDB(t)
	dcTask(t, b, dcTaskA, dcUser)
	dcSession(t, b, dcSessC, dcTaskA, dcUser, "completed", 8*time.Second)
	before := dcRead(t, b, dcSessC)

	if err := dcSvc(b, nil).Delete(b.ctx, dcTaskA, dcUser); err != nil {
		t.Fatalf("Delete 失败: %v", err)
	}

	after := dcRead(t, b, dcSessC)
	if after.Status != before.Status || after.DurationMs != before.DurationMs ||
		!after.CompletedAt.Equal(*before.CompletedAt) {
		t.Errorf("已终态会话被删除动作改写：status %q→%q duration %d→%d",
			before.Status, after.Status, before.DurationMs, after.DurationMs)
	}
}

// 4) 归属：删除别人的任务不收口别人的会话。
// 会话侧的收口条件必须带上「这个任务确实属于本次删除请求的用户」，否则越权探测
// （知道 taskID 就能把别人的在途执行判死）与 46 条孤儿一样会扩散到别人名下。
func TestDeleteOtherUsersTaskTouchesNothing(t *testing.T) {
	b := dcDB(t)
	dcTask(t, b, dcTaskA, dcUserB)
	dcSession(t, b, dcSessA, dcTaskA, dcUserB, "active", 4*time.Second)

	svc := dcSvc(b, nil)
	if err := svc.Delete(b.ctx, dcTaskA, dcUser); err == nil {
		t.Fatal("删除他人任务竟然成功：前置归属校验失效，后面的收口断言都无意义")
	}
	if got := dcRead(t, b, dcSessA); got.Status != "active" {
		t.Errorf("越权删除请求收口了他人会话：status=%q，期望仍为 active", got.Status)
	}
	if got := dcBusy(t, b, dcUserB); got != 1 {
		t.Errorf("他人并发闸被越权请求改动：CountRunningByUser=%d，期望 1", got)
	}
}
