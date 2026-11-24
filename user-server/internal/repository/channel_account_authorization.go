package repository

import (
	"context"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tenant"

	"gorm.io/gorm"
)

// ChannelAccountAuthorizationRepository 渠道账号数据授权读取口（Phase 2 表 092，
// 原 RBAC 表不动；本仓储只叠加数据权限维度）。
type ChannelAccountAuthorizationRepository interface {
	ListByUser(ctx context.Context, userID uint) ([]model.ChannelAccountAuthorization, error)
}

type channelAccountAuthorizationRepository struct{}

func NewChannelAccountAuthorizationRepository() ChannelAccountAuthorizationRepository {
	return &channelAccountAuthorizationRepository{}
}

func (r *channelAccountAuthorizationRepository) db(ctx context.Context) *gorm.DB {
	return dbFromCtx(ctx)
}

func (r *channelAccountAuthorizationRepository) ListByUser(ctx context.Context, userID uint) ([]model.ChannelAccountAuthorization, error) {
	tid := tenant.WriteTenantID(ctx)
	var rows []model.ChannelAccountAuthorization
	err := r.db(ctx).Where("tenant_id = ? AND user_id = ?", tid, userID).Find(&rows).Error
	return rows, err
}
