package app

import (
	"context"
	"gorm.io/gorm"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/pkg/utils"
	"hivemtk-user/internal/pkg/utils/logger"
	"hivemtk-user/internal/repository"
	"hivemtk-user/internal/service"
	"sync"
	"time"
)

type CommerceExecutionRuntime struct {
	cancel context.CancelFunc
	done   chan struct{}
	once   sync.Once
}

// InitCommerceExecutionRuntime restores tenant queues without registering unapproved production transports.
func InitCommerceExecutionRuntime(database *gorm.DB) *CommerceExecutionRuntime {
	if database == nil {
		return nil
	}
	svc := service.NewCommerceService(repository.NewCommerceRepository(database), nil)
	ctx, cancel := context.WithCancel(context.Background())
	r := &CommerceExecutionRuntime{cancel: cancel, done: make(chan struct{})}
	utils.SafeGo(ctx, "commerce-execution-recovery", func(ctx context.Context) {
		defer close(r.done)
		ticker := time.NewTicker(15 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				_, e := tenant.Fanout(ctx, database, func(tc context.Context, _ uint) (int64, error) { n, e := svc.RunDue(tc, 10); return int64(n), e })
				if e != nil && ctx.Err() == nil {
					logger.Warn("[Commerce] durable execution requires provider or operator review")
				}
			}
		}
	})
	return r
}
func (r *CommerceExecutionRuntime) Stop(ctx context.Context) {
	if r == nil {
		return
	}
	r.once.Do(r.cancel)
	select {
	case <-r.done:
	case <-ctx.Done():
	}
}
