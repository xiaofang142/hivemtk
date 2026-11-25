package repository

import (
	"context"

	"hivemtk-user/internal/model"
	_db "hivemtk-user/internal/pkg/db"

	"gorm.io/gorm"
)

// ChannelAccountManageRepository 管理面账号查询：保留备注/状态过滤与准确分页计数。
// 与业务下拉（ListOptions 默认仅启用、不含备注）刻意分开，避免「用业务下拉默认
// 过滤掩盖管理对象」。
type ChannelAccountManageRepository interface {
	Available() bool
	// ListManaged 按启停/全部过滤 + 分页返回账号行（含 remark）。
	ListManaged(ctx context.Context, opts ChannelAccountListOptions) ([]model.ChannelAccount, error)
	// CountManaged 用与 ListManaged 完全一致的过滤条件计数（不含分页）。
	CountManaged(ctx context.Context, opts ChannelAccountListOptions) (int64, error)
}

type channelAccountManageRepo struct {
	db *gorm.DB
}

// NewChannelAccountManageRepository 创建管理面账号仓储（全局 DB 装配）。
func NewChannelAccountManageRepository() ChannelAccountManageRepository {
	return &channelAccountManageRepo{db: _db.GetDB()}
}

// NewChannelAccountManageRepositoryWithDB 用显式句柄构造（装配点/测试用）。
func NewChannelAccountManageRepositoryWithDB(gormDB *gorm.DB) ChannelAccountManageRepository {
	return &channelAccountManageRepo{db: gormDB}
}

func (r *channelAccountManageRepo) Available() bool { return r != nil && r.db != nil }

// managedBase 是 ListManaged/CountManaged 共享的唯一过滤构造口：
// 两方法必须共用它，否则 total 与实际返回行口径漂移。
func (r *channelAccountManageRepo) managedBase(ctx context.Context, opts ChannelAccountListOptions) *gorm.DB {
	q := r.db.WithContext(ctx).Model(&model.ChannelAccount{})
	q = applyChannelEditionGate(ctx, q.Where("tenant_id = ?", opts.TenantID))
	if opts.ChannelID > 0 {
		q = q.Where("channel_id = ?", opts.ChannelID)
	}
	if opts.Status != nil {
		q = q.Where("status = ?", *opts.Status)
	}
	if opts.Keyword != "" {
		q = q.Where("name ILIKE ? OR identify_code ILIKE ? OR remark ILIKE ?",
			"%"+opts.Keyword+"%", "%"+opts.Keyword+"%", "%"+opts.Keyword+"%")
	}
	if len(opts.IDs) > 0 {
		q = q.Where("id IN ?", opts.IDs)
	}
	return q
}

func (r *channelAccountManageRepo) ListManaged(ctx context.Context, opts ChannelAccountListOptions) ([]model.ChannelAccount, error) {
	if !r.Available() {
		return nil, ErrChannelStoreUnavailable
	}
	q := r.managedBase(ctx, opts)
	if opts.Limit > 0 {
		q = q.Limit(opts.Limit)
	}
	if opts.Offset > 0 {
		q = q.Offset(opts.Offset)
	}
	var rows []model.ChannelAccount
	if err := q.Order("id ASC").Find(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

func (r *channelAccountManageRepo) CountManaged(ctx context.Context, opts ChannelAccountListOptions) (int64, error) {
	if !r.Available() {
		return 0, ErrChannelStoreUnavailable
	}
	var total int64
	if err := r.managedBase(ctx, opts).Count(&total).Error; err != nil {
		return 0, err
	}
	return total, nil
}
