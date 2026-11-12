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

	"hivemtk-user/internal/pkg/utils"
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
	// SafeGo 而非裸 go（裸 go 闸 2026-11-13）：panic 面收进 recover，行为不变。
	utils.SafeGo(ctx, "retention_cleanup_cron.run", t.run)
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
	// SafeGo 而非裸 go（裸 go 闸 2026-11-13，原 gospawn-ok 白名单注记随之撤销）：
	// 等待型 helper（wg.Wait→close done），SafeGo 语义等价且统一走 recover。
	utils.SafeGo(ctx, "retention_cleanup_cron.stop_wait", func(_ context.Context) {
		t.wg.Wait()
		close(done)
	})
	select {
	case <-done:
	case <-ctx.Done():
	}
}

var retentionCleanupCron *RetentionCleanupTask

func init() {
	// 九表接入规格在 retention_cleanup_tables.go（CC-04 刀2）；每轮热读
	// retention.<表名>_days，天数经 config_param 调，无需重启。
	cleaner := NewRetentionCleaner(repository.NewRetentionCleanupRepo(), retentionTableSpecs())
	retentionCleanupCron = NewRetentionCleanupTask(cleaner)
	retentionCleanupCron.Start(context.Background())
}
