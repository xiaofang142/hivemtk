// approval_sweep.go 审批 pending 的到期清扫（T-P3-02，收口 T-P3-01 的移交项）
//
// T-P3-01 交付了 ExpireOverdue，但全仓非测试调用点是 0 —— 那时 pending 行只增不减，
// "24 小时没人裁决算放弃"只是一个写在常量旁边的句子。同一张表上"有方法没调用方"
// 这个形状本仓已经踩过三次（草稿竖的 ExpireOverdue/PurgeTerminal、恢复队列、审批门），
// 所以本卡把它做成真的有人按节拍跑。
//
// 与流程侧的关系必须说清，否则这一格会被读成"不跑清扫流程就卡死"：
// 挂起的流程**不依赖**本 worker 收口 —— ResolveOnFire 在点火时自己会从
// "仍 pending 且已过 expires_at" 推导出 expired（见 sop_approval_resume.go）。
// 本 worker 管的是另一件事：**库里那一行**。少了它，待办中心会一直显示一条
// 早就该作废的待办，而审批人点开会发现"这条还能批吗"——状态不一致的表比没有表更难查。
package service

import (
	"context"
	"sync"
	"time"

	"hivemtk-user/internal/pkg/utils/logger"
)

const (
	// DefaultApprovalSweepInterval 清扫节拍。取 5 分钟对齐"待办中心给人看"这件事：
	// 审批的 TTL 以小时计，5 分钟与 6 小时在"多久算过期"上没有区别，差别只在
	// "过期之后这条待办还要挂多久才消失"。
	DefaultApprovalSweepInterval = 5 * time.Minute

	// approvalSweepBatchLimit 单轮上限。必须有界：一次 UPDATE 的最坏耗时与锁范围
	// 不能随表大小增长（口径同恢复队列的单轮上限）。
	approvalSweepBatchLimit = 200
)

// ApprovalSweepReport 一轮清扫的结果。
type ApprovalSweepReport struct {
	At      time.Time `json:"at"`
	Expired int       `json:"expired"`
	Err     string    `json:"err,omitempty"`
}

// ApprovalSweepWorker 到期审批的节拍器。
type ApprovalSweepWorker struct {
	svc      *ApprovalRequestService
	interval time.Duration

	startOnce sync.Once
	stop      chan struct{}
	wg        sync.WaitGroup

	mu         sync.RWMutex
	started    bool
	rounds     int64
	expiredAll int64
}

// NewApprovalSweepWorker 构造。interval <= 0 走默认值，两处不各写一份默认。
func NewApprovalSweepWorker(svc *ApprovalRequestService, interval time.Duration) *ApprovalSweepWorker {
	if interval <= 0 {
		interval = DefaultApprovalSweepInterval
	}
	return &ApprovalSweepWorker{svc: svc, interval: interval, stop: make(chan struct{})}
}

// Start 启动轮询协程（幂等）。svc 为 nil 时不启动并出声。
//
// 首轮在 interval 之后才跑，与草稿清扫同口径：启动瞬间正有进程在重启，
// 这时做一轮全表 UPDATE 会与恢复期的读请求抢锁。
func (w *ApprovalSweepWorker) Start(ctx context.Context) {
	if w == nil {
		return
	}
	w.startOnce.Do(func() {
		if w.svc == nil {
			logger.Warnf("[approval] ⚠️ 未注入审批服务 ⇒ 清扫 worker 不启动（到期 pending 不会自动过期）")
			return
		}
		w.mu.Lock()
		w.started = true
		w.mu.Unlock()
		w.wg.Add(1)
		go w.loop(ctx)
		logger.Infof("[approval] 到期清扫 worker 已启动：间隔=%s 单轮上限=%d（首轮在 %s 之后）",
			w.interval, approvalSweepBatchLimit, w.interval)
	})
}

// Stop 停止并等待进行中的那一轮收尾（幂等）。
func (w *ApprovalSweepWorker) Stop(_ context.Context) {
	if w == nil {
		return
	}
	select {
	case <-w.stop:
		return
	default:
		close(w.stop)
	}
	w.wg.Wait()
	w.mu.Lock()
	w.started = false
	w.mu.Unlock()
}

func (w *ApprovalSweepWorker) loop(ctx context.Context) {
	defer w.wg.Done()
	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-w.stop:
			return
		case <-ticker.C:
			w.RunOnce(ctx)
		}
	}
}

// RunOnce 跑一轮到期清扫，返回本轮结果。导出的理由是"测的那份就是生产跑的那份"：
// 只靠节拍触发的循环无法在测试里等到，而不限窗口的话 AC③ 只能测常量不能测行为。
func (w *ApprovalSweepWorker) RunOnce(ctx context.Context) *ApprovalSweepReport {
	report := &ApprovalSweepReport{At: time.Now()}
	if w == nil || w.svc == nil {
		report.Err = "审批服务未注入"
		return report
	}
	flipped, err := w.svc.ExpireOverdue(ctx, approvalSweepBatchLimit)
	if err != nil {
		report.Err = err.Error()
		// 失败必须出声：这一格静默失败的结果是"pending 永不退场"，
		// 而那是 T-P3-01 移交过来的原病灶，不能修完又无声退化回去。
		logger.Errorf("[approval] 到期清扫失败 ⇒ 已翻 %d 条，剩余留待下一轮：%v", len(flipped), err)
	}
	report.Expired = len(flipped)

	w.mu.Lock()
	w.rounds++
	w.expiredAll += int64(report.Expired)
	w.mu.Unlock()

	if report.Expired > 0 {
		logger.Warnf("[approval] 到期清扫：%d 条 pending 因无人裁决翻成 expired（待办中心需据此收敛展示）", report.Expired)
	}
	return report
}

// Running 轮询协程是否在跑。
func (w *ApprovalSweepWorker) Running() bool {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.started
}

// Rounds 已完成的轮次数（含失败轮）。
func (w *ApprovalSweepWorker) Rounds() int64 {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.rounds
}

// ExpiredTotal 累计过期条数（装配日志退出时回显用）。
func (w *ApprovalSweepWorker) ExpiredTotal() int64 {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.expiredAll
}
