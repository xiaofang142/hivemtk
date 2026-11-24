package repository

import (
	"context"
	"errors"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tenant"

	"gorm.io/gorm"
)

// ServiceProviderRepository 服务商数据访问（T019/T027）。
type ServiceProviderRepository interface {
	Create(ctx context.Context, row *model.ServiceProvider) error
	GetByID(ctx context.Context, id int64) (*model.ServiceProvider, error)
	ListByCustomer(ctx context.Context, customerID string, page, pageSize int) ([]*model.ServiceProvider, int64, error)
	Update(ctx context.Context, row *model.ServiceProvider) error
	Delete(ctx context.Context, id int64) error
}

type serviceProviderRepository struct{}

func NewServiceProviderRepository() ServiceProviderRepository { return &serviceProviderRepository{} }

func (r *serviceProviderRepository) db(ctx context.Context) *gorm.DB { return dbFromCtx(ctx) }

func (r *serviceProviderRepository) Create(ctx context.Context, row *model.ServiceProvider) error {
	if row == nil {
		return errors.New("service provider is nil")
	}
	if row.TenantID == 0 {
		row.TenantID = tenant.WriteTenantID(ctx)
	}
	return r.db(ctx).Create(row).Error
}

func (r *serviceProviderRepository) GetByID(ctx context.Context, id int64) (*model.ServiceProvider, error) {
	var row model.ServiceProvider
	err := r.db(ctx).Where("id = ? AND tenant_id = ?", id, tenant.WriteTenantID(ctx)).First(&row).Error
	if err != nil {
		return nil, err
	}
	return &row, nil
}

func (r *serviceProviderRepository) ListByCustomer(ctx context.Context, customerID string, page, pageSize int) ([]*model.ServiceProvider, int64, error) {
	if page <= 0 {
		page = 1
	}
	if pageSize <= 0 || pageSize > 100 {
		pageSize = 20
	}
	q := r.db(ctx).Model(&model.ServiceProvider{}).
		Where("tenant_id = ? AND customer_id = ? AND deleted_at IS NULL", tenant.WriteTenantID(ctx), customerID)
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var rows []*model.ServiceProvider
	err := q.Order("id DESC").Offset((page - 1) * pageSize).Limit(pageSize).Find(&rows).Error
	return rows, total, err
}

func (r *serviceProviderRepository) Update(ctx context.Context, row *model.ServiceProvider) error {
	if row == nil || row.ID == 0 {
		return errors.New("invalid service provider")
	}
	return r.db(ctx).Model(&model.ServiceProvider{}).
		Where("id = ? AND tenant_id = ?", row.ID, tenant.WriteTenantID(ctx)).
		Updates(row).Error
}

func (r *serviceProviderRepository) Delete(ctx context.Context, id int64) error {
	return r.db(ctx).Model(&model.ServiceProvider{}).
		Where("id = ? AND tenant_id = ?", id, tenant.WriteTenantID(ctx)).
		Update("deleted_at", gorm.Expr("now()")).Error
}
