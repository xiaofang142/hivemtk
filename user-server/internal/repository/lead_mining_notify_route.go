package repository

import (
	"context"

	"gorm.io/gorm"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tenant"
)

type LeadMiningNotifyRouteRepository struct{}

func NewLeadMiningNotifyRouteRepository() *LeadMiningNotifyRouteRepository { return &LeadMiningNotifyRouteRepository{} }

func (r *LeadMiningNotifyRouteRepository) db(ctx context.Context) *gorm.DB { return dbFromCtx(ctx) }

func (r *LeadMiningNotifyRouteRepository) Create(ctx context.Context, m *model.LeadMiningNotifyRoute) error {
	if m.TenantID == 0 { m.TenantID = tenant.WriteTenantID(ctx) }
	return r.db(ctx).Create(m).Error
}

func (r *LeadMiningNotifyRouteRepository) List(ctx context.Context, channel, accountID string) ([]model.LeadMiningNotifyRoute, error) {
	q := r.db(ctx).Model(&model.LeadMiningNotifyRoute{}).Where("deleted_at IS NULL")
	if channel != "" { q = q.Where("channel = ?", channel) }
	if accountID != "" { q = q.Where("account_id = ?", accountID) }
	var items []model.LeadMiningNotifyRoute
	err := q.Order("id desc").Find(&items).Error
	return items, err
}

func (r *LeadMiningNotifyRouteRepository) Delete(ctx context.Context, routeID string) error {
	return r.db(ctx).Where("route_id = ?", routeID).Delete(&model.LeadMiningNotifyRoute{}).Error
}

func (r *LeadMiningNotifyRouteRepository) ListEnabledByAccount(ctx context.Context, channel, accountID string) ([]model.LeadMiningNotifyRoute, error) {
	q := r.db(ctx).Model(&model.LeadMiningNotifyRoute{}).Where("deleted_at IS NULL AND enabled = true")
	if channel != "" { q = q.Where("channel = ? OR channel = ''", channel) }
	if accountID != "" { q = q.Where("account_id = ? OR account_id = ''", accountID) }
	var items []model.LeadMiningNotifyRoute
	err := q.Order("id desc").Find(&items).Error
	return items, err
}
