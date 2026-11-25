package app

import (
	"context"
	"sync"
	"time"

	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/pkg/utils"
	"hivemtk-user/internal/pkg/utils/logger"
	"hivemtk-user/internal/service"

	"gorm.io/gorm"
)

type WebhookDeliveryRuntime struct {
	cancel context.CancelFunc
	done   chan struct{}
	once   sync.Once
}

// InitWebhookDeliveryRuntime periodically replays dead-letter webhook deliveries (Route 3).
func InitWebhookDeliveryRuntime(database *gorm.DB) *WebhookDeliveryRuntime {
	if database == nil {
		return nil
	}
	svc := service.NewWebhookSubService()
	ctx, cancel := context.WithCancel(tenant.WithSystemScope(context.Background()))
	r := &WebhookDeliveryRuntime{cancel: cancel, done: make(chan struct{})}
	utils.SafeGo(ctx, "webhook-delivery-replay", func(ctx context.Context) {
		defer close(r.done)
		ticker := time.NewTicker(60 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				n, err := svc.ReplayDeadWebhookDeliveries(ctx, 50)
				if err != nil && ctx.Err() == nil {
					logger.Warnf("[WebhookOut] dead-letter replay error: %v", err)
				} else if n > 0 {
					logger.Infof("[WebhookOut] dead-letter replayed=%d", n)
				}
			}
		}
	})
	return r
}

func (r *WebhookDeliveryRuntime) Stop(ctx context.Context) {
	if r == nil {
		return
	}
	r.once.Do(r.cancel)
	select {
	case <-r.done:
	case <-ctx.Done():
	}
}
