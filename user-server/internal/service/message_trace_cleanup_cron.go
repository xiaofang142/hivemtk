package service

import (
	"context"
	"regexp"
	"sync"
	"time"

	"hivemtk-user/internal/pkg/utils/logger"
	"hivemtk-user/internal/repository"
)

type MessageTraceCleanupTask struct {
	repo     *repository.MessageTraceCleanupRepo
	stopCh   chan struct{}
	stopOnce sync.Once
	wg       sync.WaitGroup

	nowFn func() time.Time
}

func NewMessageTraceCleanupTask(repo *repository.MessageTraceCleanupRepo) *MessageTraceCleanupTask {
	return &MessageTraceCleanupTask{
		repo:   repo,
		stopCh: make(chan struct{}),
		nowFn:  time.Now,
	}
}

var (
	piiPhoneRe = regexp.MustCompile(`(1[3-9]\d)\d{4}\d{2}(\d{2})`)
	piiEmailRe = regexp.MustCompile(`([A-Za-z0-9._%+-])[A-Za-z0-9._%+-]*(@[A-Za-z0-9.-]+\.[A-Za-z]{2,})`)
)

func MaskPII(text string) string {
	if text == "" {
		return text
	}
	out := piiPhoneRe.ReplaceAllString(text, "$1****$2")
	out = piiEmailRe.ReplaceAllString(out, "$1***$2")
	return out
}

const (
	tracePIIMaskAge    = 30 * 24 * time.Hour
	traceTTLLimitAge   = 90 * 24 * time.Hour
	traceMaskBatchSize = 500
)

func (t *MessageTraceCleanupTask) RunOnce(ctx context.Context) (masked int64, nulled int64, err error) {
	if t.repo == nil {
		return 0, 0, nil
	}
	masked, err = t.maskAgedBodies(ctx)
	if err != nil {
		return masked, 0, err
	}
	nulled, err = t.nullExpiredBodies(ctx)
	return masked, nulled, err
}

func (t *MessageTraceCleanupTask) maskAgedBodies(ctx context.Context) (int64, error) {
	now := t.nowFn()
	upper := now.Add(-tracePIIMaskAge)
	lower := now.Add(-traceTTLLimitAge)
	var masked int64
	var lastID uint
	for {
		rows, err := t.repo.ListForPIIMask(ctx, upper, lower, lastID, traceMaskBatchSize)
		if err != nil {
			return masked, err
		}
		if len(rows) == 0 {
			return masked, nil
		}
		for _, r := range rows {
			lastID = r.ID
			newIn := MaskPII(r.Input)
			newOut := MaskPII(r.Output)
			if newIn == r.Input && newOut == r.Output {
				continue
			}
			n, err := t.repo.UpdateBody(ctx, r.ID, newIn, newOut)
			if err != nil {
				return masked, err
			}
			masked += n
		}
		if len(rows) < traceMaskBatchSize {
			return masked, nil
		}
	}
}

func (t *MessageTraceCleanupTask) nullExpiredBodies(ctx context.Context) (int64, error) {
	cutoff := t.nowFn().Add(-traceTTLLimitAge)
	return t.repo.NullExpiredBodies(ctx, cutoff)
}

func (t *MessageTraceCleanupTask) Start(ctx context.Context) {
	if t == nil || t.repo == nil {
		return
	}
	t.wg.Add(1)
	go t.run(ctx)
}

func (t *MessageTraceCleanupTask) run(ctx context.Context) {
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

func (t *MessageTraceCleanupTask) trigger(ctx context.Context) {
	defer func() {
		if r := recover(); r != nil {
			logger.Errorf("[trace_cleanup_cron] panic: %v", r)
		}
	}()
	masked, nulled, err := t.RunOnce(ctx)
	if err != nil {
		logger.Errorf("[trace_cleanup_cron] run failed: masked=%d nulled=%d err=%v", masked, nulled, err)
		return
	}
	if masked > 0 || nulled > 0 {
		logger.Infof("[trace_cleanup_cron] done: pii_masked=%d ttl_nulled=%d", masked, nulled)
	}
}

func (t *MessageTraceCleanupTask) Stop(ctx context.Context) {
	if t == nil {
		return
	}
	t.stopOnce.Do(func() { close(t.stopCh) })
	done := make(chan struct{})
	go func() { t.wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-ctx.Done():
	}
}

var traceCleanupCron *MessageTraceCleanupTask

func init() {
	repo := repository.NewMessageTraceCleanupRepo()
	traceCleanupCron = NewMessageTraceCleanupTask(repo)
	traceCleanupCron.Start(context.Background())
}

func StopMessageTraceCleanupCron(ctx context.Context) {
	if traceCleanupCron != nil {
		traceCleanupCron.Stop(ctx)
	}
}
