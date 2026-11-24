package repository

import (
	"context"
	"errors"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tenant"

	"gorm.io/gorm"
)

// CustomerMergeRepository 合并审计留痕仓储（FR-003/005）。
type CustomerMergeRepository interface {
	Create(ctx context.Context, rec *model.CustomerMerge) error
	GetByMergeID(ctx context.Context, mergeID string) (*model.CustomerMerge, error)
	ListByCustomer(ctx context.Context, customerID string, limit int) ([]*model.CustomerMerge, error)
}

type customerMergeRepository struct{}

func NewCustomerMergeRepository() CustomerMergeRepository { return &customerMergeRepository{} }

func (r *customerMergeRepository) db(ctx context.Context) *gorm.DB { return dbFromCtx(ctx) }

func (r *customerMergeRepository) Create(ctx context.Context, rec *model.CustomerMerge) error {
	if rec == nil || rec.MergeID == "" {
		return errors.New("invalid customer merge record")
	}
	if rec.TenantID == 0 {
		rec.TenantID = tenant.WriteTenantID(ctx)
	}
	return r.db(ctx).Create(rec).Error
}

func (r *customerMergeRepository) GetByMergeID(ctx context.Context, mergeID string) (*model.CustomerMerge, error) {
	tid := tenant.WriteTenantID(ctx)
	var rec model.CustomerMerge
	err := r.db(ctx).Where("tenant_id = ? AND merge_id = ?", tid, mergeID).First(&rec).Error
	if err != nil {
		return nil, err
	}
	return &rec, nil
}

func (r *customerMergeRepository) ListByCustomer(ctx context.Context, customerID string, limit int) ([]*model.CustomerMerge, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	tid := tenant.WriteTenantID(ctx)
	var rows []*model.CustomerMerge
	err := r.db(ctx).
		Where("tenant_id = ? AND (primary_id = ? OR secondary_id = ?)", tid, customerID, customerID).
		Order("created_at DESC").Limit(limit).Find(&rows).Error
	return rows, err
}
