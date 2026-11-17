package service

// channel_interaction_expiry_cron.go 卡片交互上下文到期清理（specs/024 T211）。
//
// 形态与 retention_cleanup_cron 对齐：Ticker + utils.SafeGo + 成对 Start/Stop，
// 过 check-naked-goroutine.sh。为什么需要它：pending 行是「等客户点按钮」的短期状态，
// 平台回调有效期过了就永远不会变成 answered，不清理会让表随发送量线性膨胀、
// 且运营查「待响应交互」时被一堆僵尸行污染。

import (
	"context"
	"runtime/debug"
	"sync"
	"time"

	"hivemtk-user/internal/pkg/utils"
	"hivemtk-user/internal/pkg/utils/logger"
	"hivemtk-user/internal/repository"
)

type ChannelInteractionExpiryTask struct {
	repo     *repository.ChannelInteractionRepository
	interval time.Duration
	stopCh   chan struct{}
	stopOnce sync.Once
	wg       sync.WaitGroup
	nowFn    func() time.Time
}

func NewChannelInteractionExpiryTask(repo *repository.ChannelInteractionRepository) *ChannelInteractionExpiryTask {
	return &ChannelInteractionExpiryTask{
		repo:     repo,
		interval: 24 * time.Hour,
		stopCh:   make(chan struct{}),
		nowFn:    time.Now,
	}
}

// RunOnce 把已过期仍 pending 的行置为 expired，返回影响行数。
func (t *ChannelInteractionExpiryTask) RunOnce(ctx context.Context) (int64, error) {
	if t == nil || t.repo == nil {
		return 0, nil
	}
	now := time.Now
	if t.nowFn != nil {
		now = t.nowFn
	}
	return t.repo.MarkExpired(ctx, now())
}

func (t *ChannelInteractionExpiryTask) Start(ctx context.Context) {
	if t == nil || t.repo == nil {
		return
	}
	t.wg.Add(1)
	// SafeGo 而非裸 go（裸 go 闸）：panic 面收进 recover。
	utils.SafeGo(ctx, "channel_interaction_expiry_cron.run", t.run)
}

func (t *ChannelInteractionExpiryTask) run(ctx context.Context) {
	defer t.wg.Done()
	interval := t.interval
	if interval <= 0 {
		interval = 24 * time.Hour
	}
	ticker := time.NewTicker(interval)
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

func (t *ChannelInteractionExpiryTask) trigger(ctx context.Context) {
	defer func() {
		if r := recover(); r != nil {
			logger.Errorf("[interaction_expiry_cron] panic: %v\n%s", r, debug.Stack())
		}
	}()
	expired, err := t.RunOnce(ctx)
	if err != nil {
		logger.Errorf("[interaction_expiry_cron] run failed: expired=%d err=%v", expired, err)
		return
	}
	if expired > 0 {
		logger.Infof("[interaction_expiry_cron] done: expired=%d", expired)
	}
}

// Stop 幂等：重复调用不会重复 close（stopOnce），并等到循环真正退出。
func (t *ChannelInteractionExpiryTask) Stop(ctx context.Context) {
	if t == nil {
		return
	}
	t.stopOnce.Do(func() { close(t.stopCh) })
	done := make(chan struct{})
	utils.SafeGo(ctx, "channel_interaction_expiry_cron.stop_wait", func(_ context.Context) {
		t.wg.Wait()
		close(done)
	})
	select {
	case <-done:
	case <-ctx.Done():
	}
}

var channelInteractionExpiryCron *ChannelInteractionExpiryTask

func init() {
	repo := repository.NewChannelInteractionRepositoryDefault()
	channelInteractionExpiryCron = NewChannelInteractionExpiryTask(repo)
	channelInteractionExpiryCron.Start(context.Background())
}

// ChannelInteractionExpiryForTest 暴露当前全局任务（仅测试用）。
func ChannelInteractionExpiryForTest() *ChannelInteractionExpiryTask {
	return channelInteractionExpiryCron
}
