package repository

import (
	"context"
	"errors"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tenant"

	"gorm.io/gorm"
)

// SupplierRepository 供应商数据访问（T019/T027）。
type SupplierRepository interface {
	Create(ctx context.Context, row *model.Supplier) error
	GetByID(ctx context.Context, id int64) (*model.Supplier, error)
	ListByCustomer(ctx context.Context, customerID string, page, pageSize int, scopes ...CRMEntityAccountFilter) ([]*model.Supplier, int64, error)
	Update(ctx context.Context, row *model.Supplier) error
	Delete(ctx context.Context, id int64) error
}

type supplierRepository struct{}

func NewSupplierRepository() SupplierRepository { return &supplierRepository{} }

func (r *supplierRepository) db(ctx context.Context) *gorm.DB { return dbFromCtx(ctx) }

func (r *supplierRepository) Create(ctx context.Context, row *model.Supplier) error {
	if row == nil {
		return errors.New("supplier is nil")
	}
	if row.TenantID == 0 {
		row.TenantID = tenant.WriteTenantID(ctx)
	}
	return r.db(ctx).Create(row).Error
}

func (r *supplierRepository) GetByID(ctx context.Context, id int64) (*model.Supplier, error) {
	var row model.Supplier
	err := r.db(ctx).Where("id = ? AND tenant_id = ?", id, tenant.WriteTenantID(ctx)).First(&row).Error
	if err != nil {
		return nil, err
	}
	return &row, nil
}

func (r *supplierRepository) ListByCustomer(ctx context.Context, customerID string, page, pageSize int, scopes ...CRMEntityAccountFilter) ([]*model.Supplier, int64, error) {
	if page <= 0 {
		page = 1
	}
	if pageSize <= 0 || pageSize > 100 {
		pageSize = 20
	}
	q := r.db(ctx).Model(&model.Supplier{}).
		Where("tenant_id = ? AND customer_id = ? AND deleted_at IS NULL", tenant.WriteTenantID(ctx), customerID)
	q = applyCRMEntityAccounts(q, scopes)
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var rows []*model.Supplier
	err := q.Order("id DESC").Offset((page - 1) * pageSize).Limit(pageSize).Find(&rows).Error
	return rows, total, err
}

func (r *supplierRepository) Update(ctx context.Context, row *model.Supplier) error {
	if row == nil || row.ID == 0 {
		return errors.New("invalid supplier")
	}
	return r.db(ctx).Model(&model.Supplier{}).
		Where("id = ? AND tenant_id = ?", row.ID, tenant.WriteTenantID(ctx)).
		Updates(row).Error
}

func (r *supplierRepository) Delete(ctx context.Context, id int64) error {
	return r.db(ctx).Model(&model.Supplier{}).
		Where("id = ? AND tenant_id = ?", id, tenant.WriteTenantID(ctx)).
		Update("deleted_at", gorm.Expr("now()")).Error
}
