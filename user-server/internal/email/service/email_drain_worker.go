// email_drain_worker.go 排期邮件的排水节拍（R21）。
//
// 排水判据本身在 EmailSendService.ProcessPendingEmails，本文件只管"有人按节拍跑它"。
// 这件事之所以要单独写一遍：ProcessPendingEmails 交付时就存在，但全仓非测试调用点为 0，
// 于是 dto 里那条 sendTime 分支变成一条"合法入库、永不执行、也不报错"的路径。
// 有方法没调用方这个形状本仓已经踩过四次（审批 ExpireOverdue、草稿 PurgeTerminal、
// 恢复队列、这里是邮件），所以这次直接做成装配到进程上的 worker，而不是留一句"运维记得调"。
package email

import (
	"context"
	"errors"
	"sync"
	"time"

	"hivemtk-user/internal/pkg/utils/logger"
)

// DefaultEmailDrainInterval 排水节拍。
//
// 排期粒度是分钟级（用户在后台能选"9 点发"），所以 30 秒的误差远小于使用者对
// "定时"的预期；再快就多打一次带锁的 SELECT，而这一格没有任何客户可感知收益。
const DefaultEmailDrainInterval = 30 * time.Second

// EmailDrainWorker 排水节拍器。
type EmailDrainWorker struct {
	svc      *EmailSendService
	interval time.Duration

	startOnce sync.Once
	stop      chan struct{}
	wg        sync.WaitGroup

	mu      sync.RWMutex
	rounds  int64
	stopped bool
}

// NewEmailDrainWorker 构造。interval <= 0 走默认值，两处不各写一份默认。
func NewEmailDrainWorker(svc *EmailSendService, interval time.Duration) *EmailDrainWorker {
	if interval <= 0 {
		interval = DefaultEmailDrainInterval
	}
	return &EmailDrainWorker{svc: svc, interval: interval, stop: make(chan struct{})}
}

// Start 启动节拍协程（幂等）。svc 为 nil 时不启动并出声。
//
// 首轮在 interval 之后才跑，与审批/草稿清扫同口径：进程刚起来时正有请求在恢复期里抢锁。
func (w *EmailDrainWorker) Start(ctx context.Context) {
	if w == nil {
		return
	}
	w.startOnce.Do(func() {
		if w.svc == nil {
			logger.Warnf("[email-drain] ⚠️ 未注入邮件发送服务 ⇒ 排水 worker 不启动（排期邮件会继续停在待发送，且无人报错）")
			return
		}
		w.wg.Add(1)
		go w.loop(ctx)
		logger.Infof("[email-drain] 排期邮件排水 worker 已启动：间隔=%s 单轮上限=%d 龄上限=%s（首轮在 %s 之后）",
			w.interval, emailDrainBatch, EmailPendingTTL, w.interval)
	})
}

// Stop 停止并等待进行中的那一轮收尾（幂等）。
func (w *EmailDrainWorker) Stop(_ context.Context) {
	if w == nil {
		return
	}
	w.mu.Lock()
	if !w.stopped {
		w.stopped = true
		close(w.stop)
	}
	w.mu.Unlock()
	w.wg.Wait()
}

func (w *EmailDrainWorker) loop(ctx context.Context) {
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
			// 失败在这一格只出声不上抛：没有调用方可给（进程自己就是调用方），
			// 而"下一轮再试"正是队列该有的恢复方式。
			if err := w.RunOnce(ctx); err != nil {
				logger.Errorf("[email-drain] 本轮排水失败 ⇒ 未认领的邮件留待下一轮：%v", err)
			}
		}
	}
}

// Rounds 已跑过的轮次（含失败轮：轮次回答的是「节拍跑过没有」，不是「投递成功没有」）。
func (w *EmailDrainWorker) Rounds() int64 {
	if w == nil {
		return 0
	}
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.rounds
}

// RunOnce 跑一轮排水，返回本轮结果。导出的理由是"测的那份就是生产跑的那份"，
// 同时给运维一个手动催一轮的入口。
//
// recover 必须在这一层：排水体走真实 DB + SMTP，而调用方是裸协程，
// 未捕获的 panic 会把整个进程带走（同仓 cron 侧 5393b8dc 的同一条口径）。
func (w *EmailDrainWorker) RunOnce(ctx context.Context) (err error) {
	if w == nil {
		return errors.New("邮件排水 worker 未装配")
	}
	defer func() {
		if r := recover(); r != nil {
			err = errors.New("邮件排水轮 panic")
			logger.Errorf("[email-drain] 本轮 panic 已捕获（进程不受影响）: %v", r)
		}
	}()
	if w.svc == nil {
		return errors.New("邮件发送服务未注入 ⇒ 排水轮没有可执行的对象")
	}

	w.mu.Lock()
	w.rounds++
	w.mu.Unlock()

	return w.svc.ProcessPendingEmails(ctx)
}
