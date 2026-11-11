package service

import (
	"context"
	"sync"
	"time"

	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/pkg/utils/logger"

	"hivemtk-user/internal/repository"
)

// SessionTTLCron 会话 TTL 自动关闭定时任务
type SessionTTLCron struct {
	sessionSvc *CustomerSessionService
	stopCh     chan struct{}
	stopOnce   sync.Once
	wg         sync.WaitGroup
}

// NewSessionTTLCron 启动会话 TTL 定时任务
//
// 立即执行一次（避免首次 1h 延迟），之后按 hour ticker 触发。
// sessionSvc 不可为 nil（nil 时直接返回不启动）。
func NewSessionTTLCron(sessionSvc *CustomerSessionService) *SessionTTLCron {
	if sessionSvc == nil {
		return nil
	}
	c := &SessionTTLCron{
		sessionSvc: sessionSvc,
		stopCh:     make(chan struct{}),
	}
	c.wg.Add(1)
	go c.run(context.Background())
	return c
}

// Stop 优雅停止（与 SelfLearningCron.Stop 同模式）
func (c *SessionTTLCron) Stop(ctx context.Context) {
	if c == nil {
		return
	}
	c.stopOnce.Do(func() {
		close(c.stopCh)
	})
	done := make(chan struct{})
	go func() { // gospawn-ok: 等待型 helper（wg.Wait→close done，CC-01 白名单），无业务 panic 面
		c.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-ctx.Done():
		logger.Errorf("[session_ttl_cron] Stop ctx done before goroutine exited: %v", ctx.Err())
	}
}

func (c *SessionTTLCron) run(ctx context.Context) {
	defer c.wg.Done()
	c.tryTriggerWithDBWait(ctx)
	ticker := time.NewTicker(1 * time.Hour)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			c.tryTriggerWithDBWait(ctx)
		case <-c.stopCh:
			return
		}
	}
}

func (c *SessionTTLCron) tryTriggerWithDBWait(ctx context.Context) {
	if c.sessionSvc == nil || !repository.DBReady() {
		return
	}
	// 模块闸（FR-5.6，派发时刻判定）：每跳检查一次；customer 模块
	// 无任何租户开通（或 DB 抖动时反向放行）的语义见 module_runtime_filter.go。
	if !AllowWorker(context.Background(), "customer_session") {
		logger.Info("[session_ttl_cron] customer 模块无任何租户开通，跳过本轮会话 TTL 清扫")
		return
	}
	// 多租户模式下，需以租户扇出方式逐租户执行
	if tenant.IsMultiTenant() {
		_, _ = tenant.Fanout(ctx, nil, func(ctx context.Context, _ uint) (int64, error) {
			return c.trigger(ctx)
		})
		return
	}
	if _, err := c.trigger(ctx); err != nil {
		logger.Errorf("[session_ttl_cron] 会话 TTL 清扫失败: %v", err)
	}
}

func (c *SessionTTLCron) trigger(ctx context.Context) (int64, error) {
	defer func() {
		if r := recover(); r != nil {
			logger.Errorf("[session_ttl_cron] panic: %v", r)
		}
	}()
	return c.sessionSvc.AutoCloseStaleSessions(ctx)
}

var sessionTTLCron *SessionTTLCron

// StartSessionTTLCron 由 main 显式调用（替代原 init() 副作用：
// import 本包的测试/CLI 二进制不应被拉起带 DB 访问的定时任务）
func StartSessionTTLCron() {
	if sessionTTLCron == nil {
		sessionTTLCron = NewSessionTTLCron(NewCustomerSessionService())
	}
}

// StopSessionTTLCron 进程退出时由 main 调用（与 defer 配合）
func StopSessionTTLCron(ctx context.Context) {
	if sessionTTLCron != nil {
		sessionTTLCron.Stop(ctx)
	}
}
