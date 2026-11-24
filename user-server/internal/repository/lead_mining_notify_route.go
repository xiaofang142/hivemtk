package repository

import (
	"context"

	"gorm.io/gorm"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tenant"
)

type LeadMiningNotifyRouteRepository struct{}

func NewLeadMiningNotifyRouteRepository() *LeadMiningNotifyRouteRepository {
	return &LeadMiningNotifyRouteRepository{}
}

func (r *LeadMiningNotifyRouteRepository) db(ctx context.Context) *gorm.DB { return dbFromCtx(ctx) }

func (r *LeadMiningNotifyRouteRepository) Create(ctx context.Context, m *model.LeadMiningNotifyRoute) error {
	if m.TenantID == 0 {
		m.TenantID = tenant.WriteTenantID(ctx)
	}
	return r.db(ctx).Create(m).Error
}

func (r *LeadMiningNotifyRouteRepository) List(ctx context.Context, channel, accountID string) ([]model.LeadMiningNotifyRoute, error) {
	q := r.db(ctx).Model(&model.LeadMiningNotifyRoute{}).Where("deleted_at IS NULL")
	if channel != "" {
		q = q.Where("channel = ?", channel)
	}
	if accountID != "" {
		q = q.Where("account_id = ?", accountID)
	}
	var items []model.LeadMiningNotifyRoute
	err := q.Order("id desc").Find(&items).Error
	return items, err
}

func (r *LeadMiningNotifyRouteRepository) Delete(ctx context.Context, routeID string) error {
	return r.db(ctx).Where("route_id = ?", routeID).Delete(&model.LeadMiningNotifyRoute{}).Error
}

func (r *LeadMiningNotifyRouteRepository) ListEnabledByAccount(ctx context.Context, channel, accountID string) ([]model.LeadMiningNotifyRoute, error) {
	q := r.db(ctx).Model(&model.LeadMiningNotifyRoute{}).Where("deleted_at IS NULL AND enabled = true")
	if channel != "" {
		q = q.Where("channel = ? OR channel = ''", channel)
	}
	if accountID != "" {
		q = q.Where("account_id = ? OR account_id = ''", accountID)
	}
	var items []model.LeadMiningNotifyRoute
	err := q.Order("id desc").Find(&items).Error
	return items, err
}

// ListByChannelAccount 读取指定渠道账号可见的启用路由（含 account/channel/tenant 三级）。
func (r *LeadMiningNotifyRouteRepository) ListByChannelAccount(ctx context.Context, channelAccountID uint, channel string) ([]model.LeadMiningNotifyRoute, error) {
	q, err := leadNotifyScoped(ctx, r.db(ctx))
	if err != nil {
		return nil, err
	}
	q = q.Model(&model.LeadMiningNotifyRoute{}).Where("deleted_at IS NULL AND enabled = true")
	if channelAccountID > 0 {
		q = q.Where("channel_account_id = ? OR channel_account_id = 0", channelAccountID)
	}
	if channel != "" {
		q = q.Where("channel = ? OR channel = ''", channel)
	}
	var items []model.LeadMiningNotifyRoute
	err = q.Order("priority ASC, id ASC").Find(&items).Error
	return items, err
}

// UpdateDeliveryRecords 落送达记录（业务码判成败的结果数组，宪法 IV）。
func (r *LeadMiningNotifyRouteRepository) UpdateDeliveryRecords(ctx context.Context, routeID, records string) error {
	q, err := leadNotifyScoped(ctx, r.db(ctx))
	if err != nil {
		return err
	}
	return q.Model(&model.LeadMiningNotifyRoute{}).
		Where("route_id = ?", routeID).
		Update("delivery_records", records).Error
}

// leadNotifyScoped 给通知路由查询补显式租户条件：多租户缺租户时 fail-closed，
// 单租户部署回退默认租户，与 Phase 2 ChannelResolver 口径一致。
func leadNotifyScoped(ctx context.Context, db *gorm.DB) (*gorm.DB, error) {
	tid, ok := tenant.FromContext(ctx)
	if !ok || tid == 0 {
		if tenant.IsMultiTenant() {
			return nil, ErrLeadTenantRequired
		}
		tid = tenant.DefaultTenantID
	}
	return db.Where("tenant_id = ?", tid), nil
}
