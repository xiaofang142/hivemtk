package app

import (
	"context"
	"sync"
	"time"

	contentrepo "hivemtk-user/internal/content/repository"
	contentservice "hivemtk-user/internal/content/service"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/pkg/utils"
	"hivemtk-user/internal/pkg/utils/logger"
	"hivemtk-user/internal/repository"

	"gorm.io/gorm"
)

type MarketingExecutionRuntime struct {
	cancel context.CancelFunc
	done   chan struct{}
	once   sync.Once
}

// InitMarketingExecutionRuntime 复用现有发送闸门；租户轮转，只续持久到期节点。
func InitMarketingExecutionRuntime(database *gorm.DB) *MarketingExecutionRuntime {
	if database == nil {
		return nil
	}
	svc := contentservice.NewMarketingFlowService(contentservice.MarketingFlowDeps{
		FlowRepo: contentrepo.NewMarketingFlowRepository(database), ExecutionRepo: contentrepo.NewFlowExecutionRepository(database),
		UserTagRepo: repository.NewUserTagRepositoryWithDB(database), OrderRepo: repository.NewOrderRepositoryWithDB(database),
		ClueRepo: repository.NewClueRepositoryWithDB(database), SessionRepo: repository.NewCustomerSessionRepositoryWithDB(database),
		AgentRepo: repository.NewAgentStatusRepositoryWithDB(database), OperationLogRepo: repository.NewOperationLogRepositoryWithDB(database),
	})
	ctx, cancel := context.WithCancel(context.Background())
	runtime := &MarketingExecutionRuntime{cancel: cancel, done: make(chan struct{})}
	utils.SafeGo(ctx, "marketing-execution-recovery", func(ctx context.Context) {
		defer close(runtime.done)
		ticker := time.NewTicker(15 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				_, err := tenant.Fanout(ctx, database, func(tenantCtx context.Context, _ uint) (int64, error) { return svc.SweepDue(tenantCtx) })
				if err != nil && ctx.Err() == nil {
					logger.Warn("[MarketingFlow] durable recovery requires review")
				}
			}
		}
	})
	return runtime
}

func (r *MarketingExecutionRuntime) Stop(ctx context.Context) {
	if r == nil {
		return
	}
	r.once.Do(r.cancel)
	select {
	case <-r.done:
	case <-ctx.Done():
	}
}
