package repository

import (
	"context"
	"fmt"

	"hivemtk-user/internal/aiagent/knowledge/model"
	"hivemtk-user/internal/pkg/tenant"
)

// CreateObservation 只接受已确认归属的检索元数据，不复制客户查询正文。
func (r *KnowledgeSearchLogRepository) CreateObservation(ctx context.Context, observation *model.KnowledgeSearchLog) error {
	if r == nil || r.db == nil || ctx == nil || observation == nil || tenant.IsSystemScope(ctx) {
		return fmt.Errorf("retrieval observation dependencies or scope unavailable")
	}
	tenantID := tenant.WriteTenantID(ctx)
	if tenantID == 0 || observation.TenantID != tenantID {
		return fmt.Errorf("retrieval observation tenant mismatch")
	}
	if !observation.DurationMeasured || observation.StartedAt == nil || observation.CompletedAt == nil || observation.CompletedAt.Before(*observation.StartedAt) {
		return fmt.Errorf("retrieval operation timing unavailable")
	}
	row := *observation
	row.Query = ""
	return r.db.WithContext(tenant.OnlyTenant(ctx, tenantID)).Create(&row).Error
}
