package service

// retention_cleanup_cron.go 高写入自观测表保留清理看门狗（CC-04 刀1）。
//
// 与 message_trace_cleanup_cron 同级同形：24h 周期、trigger 带 recover、
// Start/Stop 语义一致。八表接入规格在刀2（retention_cleanup_tables.go），
// 本阶段 specs 为空、RunOnce 是 no-op，看门狗先行就位。

import (
	"context"
	"runtime/debug"
	"sync"
	"time"

	"hivemtk-user/internal/pkg/utils/logger"
	"hivemtk-user/internal/repository"
)

type RetentionCleanupTask struct {
	cleaner  *RetentionCleaner
	stopCh   chan struct{}
	stopOnce sync.Once
	wg       sync.WaitGroup
}

func NewRetentionCleanupTask(cleaner *RetentionCleaner) *RetentionCleanupTask {
	return &RetentionCleanupTask{
		cleaner: cleaner,
		stopCh:  make(chan struct{}),
	}
}

func (t *RetentionCleanupTask) RunOnce(ctx context.Context) []RetentionTableResult {
	if t == nil || t.cleaner == nil {
		return nil
	}
	return t.cleaner.RunOnce(ctx)
}

func (t *RetentionCleanupTask) Start(ctx context.Context) {
	if t == nil || t.cleaner == nil {
		return
	}
	t.wg.Add(1)
	go t.run(ctx)
}

func (t *RetentionCleanupTask) run(ctx context.Context) {
	defer t.wg.Done()
	ticker := time.NewTicker(24 * time.Hour)
	defer ticker.Stop()
	for {
		select {
		case <-t.stopCh:
			return
		case <-ticker.C:
			t.trigger(ctx)
		}
	}
}

func (t *RetentionCleanupTask) trigger(ctx context.Context) {
	defer func() {
		if r := recover(); r != nil {
			logger.Errorf("[retention_cleanup_cron] panic: %v\n%s", r, debug.Stack())
		}
	}()
	results := t.RunOnce(ctx)
	for _, r := range results {
		if r.Err != nil {
			logger.Errorf("[retention_cleanup_cron] %s failed: deleted=%d err=%v", r.Table, r.Deleted, r.Err)
		} else if r.Skipped != "" {
			logger.Warnf("[retention_cleanup_cron] %s skipped: %s", r.Table, r.Skipped)
		}
	}
}

func (t *RetentionCleanupTask) Stop(ctx context.Context) {
	if t == nil {
		return
	}
	t.stopOnce.Do(func() { close(t.stopCh) })
	done := make(chan struct{})
	go func() { t.wg.Wait(); close(done) }() // gospawn-ok: 等待型 helper（wg.Wait→close done，CC-01 白名单），无业务 panic 面
	select {
	case <-done:
	case <-ctx.Done():
	}
}

var retentionCleanupCron *RetentionCleanupTask

func init() {
	// 刀1 阶段表清单为空（九表规格在刀2 接入，见 retention_cleanup_tables.go）；
	// 看门狗先行就位，空 specs 的 RunOnce 是 no-op。
	cleaner := NewRetentionCleaner(repository.NewRetentionCleanupRepo(), nil)
	retentionCleanupCron = NewRetentionCleanupTask(cleaner)
	retentionCleanupCron.Start(context.Background())
}
