package service

import (
	"context"
	"runtime/debug"
	"sync"
	"time"

	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/pkg/utils/logger"
	"hivemtk-user/internal/repository"
)

type FeedbackLearningCron struct {
	svc      *FeedbackLearningService
	sopRepo  *repository.SopAgentRepository
	stopCh   chan struct{}
	stopOnce sync.Once
	wg       sync.WaitGroup
}

func NewFeedbackLearningCron(repo *repository.FeedbackLearningRepository) *FeedbackLearningCron {
	// AV-04c 批次5 刀2：句柄判断改仓储 DBReady() 就绪探针（原 repo.GetDB() == nil），
	// SOP 仓储改包级 Shared 构造共享注入连接（批次4 先例），service 不再取句柄。
	// 未装配时返回 nil 的语义不变。
	if repo == nil || !repo.DBReady() {
		return nil
	}
	c := &FeedbackLearningCron{
		svc:     NewFeedbackLearningService(repo),
		sopRepo: repository.NewSopAgentRepositoryShared(repo),
		stopCh:  make(chan struct{}),
	}
	c.wg.Add(1)
	go c.runDaily(context.Background())
	return c
}

// Stop 优雅停止
func (c *FeedbackLearningCron) Stop(ctx context.Context) {
	if c == nil {
		return
	}
	c.stopOnce.Do(func() { close(c.stopCh) })
	done := make(chan struct{})
	go func() {
		c.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-ctx.Done():
		logger.Errorf("[feedback_learning_cron] Stop ctx done before goroutine exited: %v", ctx.Err())
	}
}

func (c *FeedbackLearningCron) runDaily(ctx context.Context) {
	defer c.wg.Done()
	ticker := time.NewTicker(24 * time.Hour)
	defer ticker.Stop()
	c.trigger(ctx)
	for {
		select {
		case <-ticker.C:
			c.trigger(ctx)
		case <-c.stopCh:
			return
		}
	}
}

func (c *FeedbackLearningCron) trigger(ctx context.Context) {
	defer func() {
		if r := recover(); r != nil {
			logger.Errorf("[feedback_learning_cron] trigger panic: %v\n%s", r, debug.Stack())
		}
	}()
	if c.svc == nil {
		return
	}

	// 多租户模式下，需以租户扇出方式逐租户执行
	if tenant.IsMultiTenant() {
		_, _ = tenant.Fanout(ctx, nil, func(ctx context.Context, _ uint) (int64, error) {
			c.triggerTenant(ctx)
			return 0, nil
		})
		return
	}
	c.triggerTenant(ctx)
}

func (c *FeedbackLearningCron) triggerTenant(ctx context.Context) {
	periodEnd := time.Now()
	periodStart := periodEnd.AddDate(0, 0, -30)
	if _, err := c.svc.ExtractProfile(ctx, 0, "系统智能体", "ai_champion", periodStart, periodEnd); err != nil {
		logger.Ctx(ctx).Warn().Err(err).Msg("[feedback_learning_cron] ExtractProfile failed")
	}

	if c.sopRepo == nil {
		return
	}
	sops, err := c.sopRepo.ListAll(ctx)
	if err != nil {
		logger.Ctx(ctx).Warn().Err(err).Msg("[feedback_learning_cron] list sops failed")
		return
	}
	for _, sop := range sops {
		stats, aerr := c.svc.AnalyzeNodeConversion(ctx, sop.ID, "")
		if aerr != nil {
			logger.Ctx(ctx).Warn().Err(aerr).Uint("sop_id", sop.ID).
				Msg("[feedback_learning_cron] AnalyzeNodeConversion failed")
			continue
		}
		if _, gerr := c.svc.GenerateOptimizationSuggestions(ctx, OptimizationSuggestionInput{
			SOPID:          sop.ID,
			SOPName:        sop.Name,
			NodeConversion: stats,
		}); gerr != nil {
			logger.Ctx(ctx).Warn().Err(gerr).Uint("sop_id", sop.ID).
				Msg("[feedback_learning_cron] GenerateOptimizationSuggestions failed")
		}
	}
}
