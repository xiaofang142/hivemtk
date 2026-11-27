package main

import (
	"context"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/pkg/utils"
	"hivemtk-user/internal/pkg/utils/logger"
	"hivemtk-user/internal/repository"
	"hivemtk-user/internal/service"

	"gorm.io/gorm"
)

// outboundReclaimInterval 出站回收巡检周期。卡住消息的判定阈值由
// OutboundReclaimTask（claimTimeout / staleAfter）决定，这里只控制扫描频率。
const outboundReclaimInterval = 2 * time.Minute

// startTenantScopedOutboundReclaim 在 DB 就绪后接上此前只打 disabled 信号的出站回收。
//
// 为什么按租户扫：OutboundDeliveryService.ReclaimOutbound 强制要求普通租户上下文，
// 且明确拒绝 system scope（tenant_scope_invalid）；因此不能用一条系统级全表扫描。
// 租户清单取自 channel_accounts 中已有识别码的行——只有这些租户才可能有桥接出站，
// 既避免无租户上下文误扫，也避免在从未配置渠道的租户上空跑。
func startTenantScopedOutboundReclaim(gormDB *gorm.DB) {
	if gormDB == nil {
		service.ReportOutboundReclaimDisabled()
		return
	}

	// 列租户是平台级只读查询，与 migration/备份同类，走 system scope 豁免。
	scanCtx := tenant.WithSystemScope(context.Background())
	var tenantIDs []uint
	if err := gormDB.WithContext(scanCtx).
		Model(&model.ChannelAccount{}).
		Where("identify_code <> ''").
		Distinct("tenant_id").
		Pluck("tenant_id", &tenantIDs).Error; err != nil {
		logger.Warnf("[OutboundReclaim] 租户清单读取失败，保持 disabled: %v", err)
		service.ReportOutboundReclaimDisabled()
		return
	}
	if len(tenantIDs) == 0 {
		service.ReportOutboundReclaimDisabled()
		return
	}

	task := service.NewOutboundReclaimTask(repository.NewMessageHubRepositoryWithDB(gormDB))
	utils.SafeGo(context.Background(), "outbound_reclaim.loop", func(ctx context.Context) {
		ticker := time.NewTicker(outboundReclaimInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				runOutboundReclaimForTenants(task, tenantIDs)
			}
		}
	})
	logger.Infof("[OutboundReclaim] 已装配租户级出站回收: tenants=%d interval=%s", len(tenantIDs), outboundReclaimInterval)
}

func runOutboundReclaimForTenants(task *service.OutboundReclaimTask, tenantIDs []uint) {
	for _, tenantID := range tenantIDs {
		if tenantID == 0 {
			continue
		}
		ctx := tenant.WithTenantID(context.Background(), tenantID)
		reclaimed, exhausted, aged, err := task.RunOnce(ctx)
		if err != nil {
			logger.Warnf("[OutboundReclaim] 租户 %d 巡检失败: %v", tenantID, err)
			continue
		}
		if reclaimed+exhausted+aged > 0 {
			logger.Infof("[OutboundReclaim] 租户 %d 巡检: reclaimed=%d exhausted=%d aged=%d",
				tenantID, reclaimed, exhausted, aged)
		}
	}
}
