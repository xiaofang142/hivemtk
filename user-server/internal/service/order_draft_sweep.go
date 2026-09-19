// order_draft_sweep.go 订单草稿的定时清扫（新规划任务清单 T-P2-06 ②）。
//
// 一句话职责：把"7 天没确认就过期"和"终态行留 90 天"这两条本来只写在注释里的
// 口径变成真的有人按节拍执行的东西。T-P2-01 交付了 ExpireOverdue / PurgeTerminal
// 两个方法，但全仓非测试调用点是 0 —— 没人调的保留期只是一个装饰性的常量。
//
// 为什么两条要放同一个 worker、且顺序固定为"先过期再清理"：
//   - 清理只删终态行，而 pending 变成终态的自动路径只有 ExpireOverdue 一条
//     （Confirm/Cancel 靠人点）。只跑清理的话，无人处理的过期 pending 永远留在表里，
//     "有清理"这件事在最常见的路径上是空的；
//   - 反过来先过期会把到期行刷成 expired 并把 updated_at 挪到现在，于是它至少还能
//     再留一个保留期 —— 这正是"复盘时查得到为什么没成单"要的那段时间。
//
// 多副本：每副本都会跑这一轮。库侧 ExpirePendingBatch 走 FOR UPDATE SKIP LOCKED，
// 各副本拿到互不相交的集合，因此 expired 统计事件不会记两遍；PurgeTerminal 是幂等删除。
// 内存底座（off/shadow 的读侧）各扫各的，与今天"各副本各看各的内存"一致。
package service

import (
	"context"
	"sync"
	"time"

	"hivemtk-user/internal/pkg/utils/logger"
)

const (
	// DefaultOrderDraftSweepInterval 两轮清扫的间隔。取 6 小时是对齐"过期判据是天级
	// （7 天未确认）"这个粒度：更密的节拍只会多几次空转的 UPDATE，却不会让任何一张
	// 草稿更早地过期。
	DefaultOrderDraftSweepInterval = 6 * time.Hour
)

// OrderDraftSweepReport 一轮清扫的结果。
//
// 两条错误字段分开，因为处置动作不同：过期失败 = 工作台会一直显示一条其实早就该
// 作废的草稿；清理失败 = 表继续涨。合成一个 error 就分不清这轮是"没做成"还是"做了一半"。
type OrderDraftSweepReport struct {
	At          time.Time `json:"at"`
	Expired     int       `json:"expired"`
	Purged      int       `json:"purged"`
	ExpireError string    `json:"expire_error,omitempty"`
	PurgeError  string    `json:"purge_error,omitempty"`
	Store       string    `json:"store"`
	Durable     bool      `json:"durable"`
}

// Failed 本轮是否有任一半失败（端点与日志用）。
func (r *OrderDraftSweepReport) Failed() bool {
	return r != nil && (r.ExpireError != "" || r.PurgeError != "")
}

// OrderDraftSweepWorker 草稿清扫的节拍器。
type OrderDraftSweepWorker struct {
	svc       *OrderDraftService
	interval  time.Duration
	retention time.Duration

	startOnce sync.Once
	stop      chan struct{}
	wg        sync.WaitGroup

	mu         sync.RWMutex
	started    bool
	rounds     int64
	lastReport *OrderDraftSweepReport
}

// NewOrderDraftSweepWorker 构造清扫 worker。
//
// interval <= 0 走 DefaultOrderDraftSweepInterval；retention <= 0 交给
// PurgeTerminal 自己回落到 defaultDraftRetention（90 天），两处不各写一份默认值。
func NewOrderDraftSweepWorker(svc *OrderDraftService, interval, retention time.Duration) *OrderDraftSweepWorker {
	if interval <= 0 {
		interval = DefaultOrderDraftSweepInterval
	}
	return &OrderDraftSweepWorker{
		svc:       svc,
		interval:  interval,
		retention: retention,
		stop:      make(chan struct{}),
	}
}

// Start 启动轮询协程（幂等）。svc 为 nil 时不启动并出声。
//
// 首轮在 interval 之后才跑，与 RecoveryQueueWorker 同口径：启动瞬间正有进程在重启，
// 这时做一轮全表 UPDATE 会与恢复期的读请求抢锁。
func (w *OrderDraftSweepWorker) Start(ctx context.Context) {
	if w == nil {
		return
	}
	w.startOnce.Do(func() {
		if w.svc == nil {
			logger.Warnf("[order-draft] ⚠️ 未注入草稿服务 ⇒ 清扫 worker 不启动（到期草稿不会自动过期）")
			return
		}
		w.mu.Lock()
		w.started = true
		w.mu.Unlock()
		w.wg.Add(1)
		go w.loop(ctx)
		logger.Infof("[order-draft] 清扫 worker 已启动：间隔=%s 保留期=%s 底座=%s durable=%t"+
			"（首轮在 %s 之后）",
			w.interval, w.retentionString(), w.svc.StoreKind(), w.svc.Durable(), w.interval)
	})
}

func (w *OrderDraftSweepWorker) retentionString() string {
	if w.retention <= 0 {
		return defaultDraftRetention.String() + "（默认）"
	}
	return w.retention.String()
}

// Stop 停止并等待进行中的那一轮收尾（幂等）。
func (w *OrderDraftSweepWorker) Stop(_ context.Context) {
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

// Running 轮询协程是否在跑。
func (w *OrderDraftSweepWorker) Running() bool {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.started
}

// Interval 生效的节拍（端点回显用）。
func (w *OrderDraftSweepWorker) Interval() time.Duration { return w.interval }

// Rounds 已完成的轮次数（含失败轮）。
func (w *OrderDraftSweepWorker) Rounds() int64 {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.rounds
}

// LastReport 最近一轮的结果；从未跑过为 nil。
func (w *OrderDraftSweepWorker) LastReport() *OrderDraftSweepReport {
	w.mu.RLock()
	defer w.mu.RUnlock()
	if w.lastReport == nil {
		return nil
	}
	copied := *w.lastReport
	return &copied
}

func (w *OrderDraftSweepWorker) loop(ctx context.Context) {
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

// RunOnce 跑一轮"过期 → 清理"，返回本轮结果。
//
// 两段各自独立成败：过期段失败**不**跳过清理段 —— 库里可能已经堆着一批准终态行，
// 因为一次 UPDATE 失败就连带停掉删除，等于把可恢复的小故障放大成两个都不做。
// 反过来清理失败也不影响过期结果已落库这件事。
func (w *OrderDraftSweepWorker) RunOnce(ctx context.Context) *OrderDraftSweepReport {
	report := &OrderDraftSweepReport{At: time.Now()}
	if w == nil || w.svc == nil {
		report.ExpireError = "草稿服务未注入"
		report.PurgeError = report.ExpireError
		return report
	}
	expired, err := w.svc.ExpireOverdue(ctx)
	if err != nil {
		report.ExpireError = err.Error()
		logger.Errorf("[order-draft] 清扫轮过期段失败 ⇒ 已翻 %d 条，剩余留待下一轮：%v", expired, err)
	}
	report.Expired = expired

	purged, err := w.svc.PurgeTerminal(ctx, w.retention)
	if err != nil {
		report.PurgeError = err.Error()
		logger.Errorf("[order-draft] 清扫轮清理段失败 ⇒ 已删 %d 行，保留期外的行留待下一轮：%v", purged, err)
	}
	report.Purged = purged

	report.Store = w.svc.StoreKind()
	report.Durable = w.svc.Durable()

	w.mu.Lock()
	w.rounds++
	w.lastReport = report
	w.mu.Unlock()

	if report.Failed() {
		logger.Warnf("[order-draft] 清扫轮结束（有失败）：expired=%d purge_err=%q purged=%d expire_err=%q store=%s",
			report.Expired, report.ExpireError, report.Purged, report.PurgeError, report.Store)
		return report
	}
	logger.Infof("[order-draft] 清扫轮结束：expired=%d purged=%d store=%s durable=%t",
		report.Expired, report.Purged, report.Store, report.Durable)
	return report
}
