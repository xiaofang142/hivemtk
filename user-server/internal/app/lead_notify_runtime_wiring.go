package app

import (
	"context"
	"sync"
	"time"

	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/pkg/utils"
	"hivemtk-user/internal/pkg/utils/logger"
	"hivemtk-user/internal/repository"
	"hivemtk-user/internal/service"

	"gorm.io/gorm"
)

const leadNotifyRuntimeInterval = 5 * time.Second

// LeadNotifyRuntime 是本进程的线索通知耐久 outbox 运行时。
// service 同时供归档/发掘通知与历史 API 使用；worker 只从同一 outbox 认领到期意图。
type LeadNotifyRuntime struct {
	svc    *service.LeadNotifyService
	worker *service.LeadNotifyWorker
	cancel context.CancelFunc
	done   chan struct{}
	once   sync.Once
}

func (r *LeadNotifyRuntime) Service() *service.LeadNotifyService {
	if r == nil {
		return nil
	}
	return r.svc
}

func (r *LeadNotifyRuntime) Worker() *service.LeadNotifyWorker {
	if r == nil {
		return nil
	}
	return r.worker
}

var (
	leadNotifyRuntimeMu  sync.RWMutex
	leadNotifyRuntimeRef *LeadNotifyRuntime
)

// InitLeadNotifyRuntime 用显式 DB 句柄装配通知服务与后台 worker；重复装配先停旧运行时。
func InitLeadNotifyRuntime(gormDB *gorm.DB) *LeadNotifyRuntime {
	leadNotifyRuntimeMu.Lock()
	prev := leadNotifyRuntimeRef
	leadNotifyRuntimeRef = nil
	leadNotifyRuntimeMu.Unlock()
	stopLeadNotifyRuntime(prev)

	if gormDB == nil {
		service.SetGlobalLeadNotifyService(nil)
		logger.Warnf("[lead-notify] 无 DB 句柄，不装配耐久 outbox 运行时")
		return nil
	}

	svc := service.NewLeadNotifyServiceWithRepositories(
		repository.NewLeadMiningNotifyRouteRepository(),
		repository.NewChannelAccountRepositoryWithDB(gormDB),
		repository.NewLeadNotifyUserRepository(gormDB),
		repository.NewLeadNotifyRepository(gormDB),
	)
	worker := service.NewLeadNotifyWorker(svc)
	ctx, cancel := context.WithCancel(tenant.WithSystemScope(context.Background()))
	rt := &LeadNotifyRuntime{svc: svc, worker: worker, cancel: cancel, done: make(chan struct{})}
	utils.SafeGo(ctx, "lead-notify-outbox", func(ctx context.Context) {
		defer close(rt.done)
		ticker := time.NewTicker(leadNotifyRuntimeInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				report, err := worker.RunOnce(ctx)
				if err != nil && ctx.Err() == nil {
					logger.Warnf("[lead-notify] outbox worker 轮次失败：%v", err)
					continue
				}
				if report.Claimed > 0 {
					logger.Infof("[lead-notify] outbox worker claimed=%d delivered=%d retried=%d exhausted=%d receipt_unknown=%d authorization_denied=%d",
						report.Claimed, report.Delivered, report.Retried, report.Exhausted, report.ReceiptUnknown, report.AuthorizationDenied)
				}
			}
		}
	})

	leadNotifyRuntimeMu.Lock()
	leadNotifyRuntimeRef = rt
	leadNotifyRuntimeMu.Unlock()
	service.SetGlobalLeadNotifyService(svc)
	logger.Infof("[lead-notify] 耐久 outbox 运行时已装配：interval=%s", leadNotifyRuntimeInterval)
	return rt
}

// GetLeadNotifyService 返回当前装配的通知服务；未装配时返回 nil，调用方必须 fail-closed。
func GetLeadNotifyService() *service.LeadNotifyService {
	leadNotifyRuntimeMu.RLock()
	rt := leadNotifyRuntimeRef
	leadNotifyRuntimeMu.RUnlock()
	if rt == nil {
		return nil
	}
	return rt.svc
}

// StopLeadNotifyRuntime 停止 worker 并清空运行时/服务全局引用，可重复调用。
func StopLeadNotifyRuntime() {
	leadNotifyRuntimeMu.Lock()
	rt := leadNotifyRuntimeRef
	leadNotifyRuntimeRef = nil
	leadNotifyRuntimeMu.Unlock()
	stopLeadNotifyRuntime(rt)
}

func stopLeadNotifyRuntime(rt *LeadNotifyRuntime) {
	if rt == nil {
		return
	}
	rt.once.Do(rt.cancel)
	<-rt.done
	if service.GlobalLeadNotifyService() == rt.svc {
		service.SetGlobalLeadNotifyService(nil)
	}
	logger.Infof("[lead-notify] 耐久 outbox 运行时已停止")
}
