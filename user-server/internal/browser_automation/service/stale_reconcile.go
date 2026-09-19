package service

import (
	"context"
	"fmt"
	"time"

	"hivemtk-user/internal/browser_automation/model"
	"hivemtk-user/internal/browser_automation/repository"
	"hivemtk-user/internal/pkg/utils/logger"
)

// stale_reconcile.go — 批8 任务快照对账器。
//
// 要收的口：task 行的终态只有 OnSessionFinished 一个写入口，而它活在执行协程里。
// 进程在执行期被重启（或那一次写库本身失败）后，session 侧由 R22/R25 的两道看门狗收敛了，
// task 侧却没人管——status 永久停在 running。后果不是显示难看，是**任务砖化**：
// running 的任务不能编辑/再下发，用户看到的是一个既不出结果也不报错的僵尸。
//
// 判据（两条同时成立才动，缺一即跳过）：
//  1. status=running 且 updated_at 老于该任务自己的执行预算 + taskWatchdogGrace
//     （预算含 D7 确认等待，见 taskExecBudget——正常挂起中的任务不能被对账掉）；
//  2. 该任务下仍在途的老会话先收口为 failed（否则并发闸被永久占住），
//     再按最新 session 的真实终态回填 task 快照；扫不到终态 session → 置 failed 并写明归因。
//
// 写库走 ReconcileRunResult / FailStaleUnfinished（条件 UPDATE）：与执行协程同刻收口时谁先到谁算，
// 后到的一方 RowsAffected=0 自然让位，不会把真实终态盖成对账值。

const (
	// staleScanFloorAge 粗筛下限：没老过这个年纪的任务连查都不用查（绝大多数轮次的全部工作量在此省掉）。
	staleScanFloorAge = 60 * time.Second
	staleScanLimit    = 100
	staleScanInterval = time.Minute
	// staleReconcileNote 无终态 session 可依据时的归因原文（必须自证「不是平台/编排失败」）
	staleReconcileNote = "对账收敛：执行协程已退出（进程重启或超时），任务快照未回写"
	// staleSessionNote 僵尸会话的收口原文。会话侧必须一起收敛，不能只回填 task 快照：
	// CountRunningByUser/ByTask 两道闸按 created/active 计数，一条永不终态的会话等于把用户
	// 锁在「再也发不动任务」上（ErrUserBusy/ErrTaskRunning 无限重复），且报错指向一个不存在的执行。
	staleSessionNote = "对账收敛：执行协程已退出（进程重启或超时），会话未回写终态"
)

// StartStaleTaskReconcile 启动时收敛一遍（重启留下的砖当场化掉）+ 每分钟一轮。
func StartStaleTaskReconcile(ctx context.Context, taskRepo repository.BrowserTaskRepository, sessRepo repository.BrowserSessionRepository) {
	if taskRepo == nil || sessRepo == nil {
		return
	}
	go func() {
		t := time.NewTicker(staleScanInterval)
		defer t.Stop()
		// 首轮不等 tick：进程重启后砖态任务是立刻可感的，等到下一分钟只是把故障摊给运维
		if n := reconcileStaleTasks(ctx, taskRepo, sessRepo); n > 0 {
			logger.Infof("[BrowserReconcile] 启动对账收敛 %d 个砖化 running 任务", n)
		}
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				if n := reconcileStaleTasks(ctx, taskRepo, sessRepo); n > 0 {
					logger.Infof("[BrowserReconcile] 周期对账收敛 %d 个砖化 running 任务", n)
				}
			}
		}
	}()
}

// reconcileStaleTasks 一轮对账，返回回填生效的任务数（导出点留给测试直接驱动，不起 goroutine）。
func reconcileStaleTasks(ctx context.Context, taskRepo repository.BrowserTaskRepository,
	sessRepo repository.BrowserSessionRepository) int {
	tasks, err := taskRepo.FindStaleRunningAll(ctx, staleScanFloorAge, staleScanLimit)
	if err != nil {
		logger.Warnf("[BrowserReconcile] 扫描 running 任务失败: %v", err)
		return 0
	}
	converged := 0
	for _, t := range tasks {
		if time.Since(t.UpdatedAt) < taskExecBudget(t)+taskWatchdogGrace {
			continue // 还在自己的预算内（含确认挂起）——不是僵尸
		}
		// 会话先于快照收敛：classify 读的是「最新 session 的终态」，所以把该任务下仍在途的老会话
		// 先落终态，task 回填值才与会话事实同源（否则 task 记 failed 而 session 还挂着 active）。
		if n, err := sessRepo.FailStaleUnfinished(ctx, t.ID, time.Now().Add(-staleScanFloorAge), staleSessionNote); err != nil {
			logger.Warnf("[BrowserReconcile] 会话收敛失败 task=%d: %v", t.ID, err)
		} else if n > 0 {
			logger.Infof("[BrowserReconcile] task=%d 收敛 %d 条僵尸会话（并发闸已释放）", t.ID, n)
		}
		status, lastResult, errMsg := classifyStaleTask(ctx, sessRepo, t)
		ok, err := taskRepo.ReconcileRunResult(ctx, t.ID, status, lastResult, errMsg)
		if err != nil {
			logger.Warnf("[BrowserReconcile] 回填失败 task=%d: %v", t.ID, err)
			continue
		}
		if ok {
			converged++
			logger.Infof("[BrowserReconcile] task=%d 快照收敛 → %s（%s）", t.ID, status, lastResult)
		}
	}
	return converged
}

// classifyStaleTask 依最新 session 的真实终态决定回填值。
// 注意 stopped 在 task 面仍记 failed——task.status 词表里没有 stopped，
// 且 OnSessionFinished 正常路径本就是「非 completed 即 failed」，对账不另立口径（用户中止
// 的事实保存在 session 与 error_msg 上，不在 task 状态位上重复发明）。
func classifyStaleTask(ctx context.Context, sessRepo repository.BrowserSessionRepository,
	t *model.BrowserTask) (status, lastResult, errMsg string) {
	s, err := sessRepo.GetLatestByTaskID(ctx, t.ID)
	if err == nil && s != nil && sessionStatusTerminal(s.Status) {
		if s.Status == "completed" {
			return "done", fmt.Sprintf("对账回填自 session=%d", s.ID), ""
		}
		msg := s.ErrorMsg
		if msg == "" {
			msg = staleReconcileNote
		}
		return "failed", fmt.Sprintf("对账回填自 session=%d", s.ID), msg
	}
	return "failed", "对账回填：无可依据的终态会话", staleReconcileNote
}

// sessionStatusTerminal 会话终态集合（与 executor.go 收口、repository/session.go 的
// completed_at 落戳口径一致；created/active 之外再无第三种在途态）。
func sessionStatusTerminal(status string) bool {
	switch status {
	case "completed", "failed", "stopped":
		return true
	}
	return false
}
