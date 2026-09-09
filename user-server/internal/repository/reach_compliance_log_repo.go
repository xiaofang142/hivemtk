// reach_compliance_log_repo.go 合规审计日志批量落库仓储（五层 L5）
package repository

import (
	"context"

	"hivemtk-user/internal/model"

	"gorm.io/gorm"
)

// ReachComplianceLogRepository reach_compliance_log 批量写入收口
type ReachComplianceLogRepository struct {
	db *gorm.DB
}

// NewReachComplianceLogRepository 构造
func NewReachComplianceLogRepository(db *gorm.DB) *ReachComplianceLogRepository {
	return &ReachComplianceLogRepository{db: db}
}

// CreateInBatches 批量写入合规日志（ctx 透传：flushLoop 为后台任务，用 context.Background()）
func (r *ReachComplianceLogRepository) CreateInBatches(ctx context.Context, batch []*model.ReachComplianceLog, batchSize int) error {
	if r.db == nil || len(batch) == 0 {
		return nil
	}
	return r.db.WithContext(ctx).CreateInBatches(batch, batchSize).Error
}
