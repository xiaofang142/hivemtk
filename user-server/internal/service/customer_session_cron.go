package service

import (
	"context"
	"sync"
	"time"

	"hivemtk-user/internal/pkg/utils/logger"
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
	go func() {
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
	if c.sessionSvc == nil || c.sessionSvc.sessionRepo == nil {
		return
	}
	if db := c.sessionSvc.sessionRepo.GetDB(ctx); db == nil {
		return
	}
	c.trigger(ctx)
}

func (c *SessionTTLCron) trigger(ctx context.Context) {
	defer func() {
		if r := recover(); r != nil {
			logger.Errorf("[session_ttl_cron] panic: %v", r)
		}
	}()
	_, err := c.sessionSvc.AutoCloseStaleSessions(ctx)
	if err != nil {
		logger.Errorf("[session_ttl_cron] auto close failed: %v", err)
	}
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
