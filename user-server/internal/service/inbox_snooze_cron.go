package service

// inbox_snooze_cron.go 「稍后处理」到期重置（specs/024 T602）。
//
// 形态与 retention_cleanup_cron / channel_interaction_expiry_cron 对齐：
// Ticker + utils.SafeGo + 成对 Start/Stop，过 check-naked-goroutine。
//
// 为什么需要它：snoozed 是「我稍后再看」的**临时**状态，没有到期重置就永远停在那里，
// 客户的会话既不出现在待处理里、也不再计入未读——静默丢客户。

import (
	"context"
	"runtime/debug"
	"sync"
	"time"

	"hivemtk-user/internal/pkg/utils"
	"hivemtk-user/internal/pkg/utils/logger"
	"hivemtk-user/internal/repository"
)

// InboxSnoozeCron 到期扫描任务。
type InboxSnoozeCron struct {
	repo     *repository.InboxConversationRepository
	interval time.Duration
	stopCh   chan struct{}
	stopOnce sync.Once
	wg       sync.WaitGroup
	nowFn    func() time.Time
}

func NewInboxSnoozeCron(repo *repository.InboxConversationRepository) *InboxSnoozeCron {
	return &InboxSnoozeCron{
		repo:     repo,
		interval: time.Minute,
		stopCh:   make(chan struct{}),
		nowFn:    time.Now,
	}
}

// RunOnce 把到期的 snoozed 会话改回 open，返回影响行数。
func (c *InboxSnoozeCron) RunOnce(ctx context.Context) (int64, error) {
	if c == nil || c.repo == nil {
		return 0, nil
	}
	now := time.Now
	if c.nowFn != nil {
		now = c.nowFn
	}
	return c.repo.ReopenDueSnoozed(ctx, now())
}

func (c *InboxSnoozeCron) Start(ctx context.Context) {
	if c == nil || c.repo == nil {
		return
	}
	c.wg.Add(1)
	utils.SafeGo(ctx, "inbox_snooze_cron.run", c.run)
}

func (c *InboxSnoozeCron) run(ctx context.Context) {
	defer c.wg.Done()
	interval := c.interval
	if interval <= 0 {
		interval = time.Minute
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-c.stopCh:
			return
		case <-ticker.C:
			c.trigger(ctx)
		}
	}
}

func (c *InboxSnoozeCron) trigger(ctx context.Context) {
	defer func() {
		if r := recover(); r != nil {
			logger.Errorf("[inbox_snooze_cron] panic: %v\n%s", r, debug.Stack())
		}
	}()
	n, err := c.RunOnce(ctx)
	if err != nil {
		logger.Errorf("[inbox_snooze_cron] run failed: reopened=%d err=%v", n, err)
		return
	}
	if n > 0 {
		logger.Infof("[inbox_snooze_cron] done: reopened=%d", n)
	}
}

// Stop 幂等：重复调用不会重复 close，并等到循环真正退出。
func (c *InboxSnoozeCron) Stop(ctx context.Context) {
	if c == nil {
		return
	}
	c.stopOnce.Do(func() { close(c.stopCh) })
	done := make(chan struct{})
	utils.SafeGo(ctx, "inbox_snooze_cron.stop_wait", func(_ context.Context) {
		c.wg.Wait()
		close(done)
	})
	select {
	case <-done:
	case <-ctx.Done():
	}
}

var inboxSnoozeCron *InboxSnoozeCron

func init() {
	repo := repository.NewInboxConversationRepository()
	if repo == nil {
		return
	}
	inboxSnoozeCron = NewInboxSnoozeCron(repo)
	inboxSnoozeCron.Start(context.Background())
}

// InboxSnoozeCronForTest 暴露全局任务（仅测试用）。
func InboxSnoozeCronForTest() *InboxSnoozeCron { return inboxSnoozeCron }
