package repository

import (
	"context"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tenant"

	"gorm.io/gorm"
)

type BusinessAccountAuthRepository interface {
	Upsert(ctx context.Context, a *model.BusinessAccountAuthorization) error
	ListByUser(ctx context.Context, userID uint) ([]model.BusinessAccountAuthorization, error)
	ListByTenant(ctx context.Context) ([]model.BusinessAccountAuthorization, error)
	Delete(ctx context.Context, id int64) error
	IsAuthorized(ctx context.Context, userID uint, channel, accountID string) (bool, error)
}

type businessAccountAuthRepository struct{}

func NewBusinessAccountAuthRepository() BusinessAccountAuthRepository { return &businessAccountAuthRepository{} }
func (r *businessAccountAuthRepository) db(ctx context.Context) *gorm.DB    { return dbFromCtx(ctx) }

func (r *businessAccountAuthRepository) Upsert(ctx context.Context, a *model.BusinessAccountAuthorization) error {
	if a.TenantID == 0 {
		a.TenantID = tenant.WriteTenantID(ctx)
	}
	return r.db(ctx).Save(a).Error
}

func (r *businessAccountAuthRepository) ListByUser(ctx context.Context, userID uint) ([]model.BusinessAccountAuthorization, error) {
	tid := tenant.WriteTenantID(ctx)
	var items []model.BusinessAccountAuthorization
	err := r.db(ctx).Where("tenant_id=? AND user_id=?", tid, userID).Find(&items).Error
	return items, err
}

func (r *businessAccountAuthRepository) ListByTenant(ctx context.Context) ([]model.BusinessAccountAuthorization, error) {
	tid := tenant.WriteTenantID(ctx)
	var items []model.BusinessAccountAuthorization
	err := r.db(ctx).Where("tenant_id=?", tid).Find(&items).Error
	return items, err
}

func (r *businessAccountAuthRepository) Delete(ctx context.Context, id int64) error {
	tid := tenant.WriteTenantID(ctx)
	return r.db(ctx).Where("tenant_id=? AND id=?", tid, id).Delete(&model.BusinessAccountAuthorization{}).Error
}

func (r *businessAccountAuthRepository) IsAuthorized(ctx context.Context, userID uint, channel, accountID string) (bool, error) {
	tid := tenant.WriteTenantID(ctx)
	var count int64
	err := r.db(ctx).Model(&model.BusinessAccountAuthorization{}).
		Where("tenant_id=? AND user_id=? AND channel=? AND account_id=?", tid, userID, channel, accountID).
		Count(&count).Error
	return count > 0, err
}
