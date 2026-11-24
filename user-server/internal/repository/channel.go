package repository

import (
	"context"
	"errors"

	"hivemtk-user/internal/model"
	_db "hivemtk-user/internal/pkg/db"

	"gorm.io/gorm"
)

// ErrChannelStoreUnavailable 渠道域仓储未装配 DB 句柄时的哨兵错（口径同
// ErrConfigParamStoreUnavailable / ErrKVStoreUnavailable）。
var ErrChannelStoreUnavailable = errors.New("channels: 未装配数据库句柄")

// ChannelRepository 渠道主数据仓储。
//
// 所有导出方法首参均为 context.Context；租户由调用方显式传入（缺租户由上层
// fail-closed），唯一例外 GetByCode 是跨租户预设码查找，显式走系统作用域。
type ChannelRepository interface {
	Available() bool
	Create(ctx context.Context, ch *model.Channel) error
	GetByID(ctx context.Context, tenantID, id uint) (*model.Channel, error)
	GetByCode(ctx context.Context, code string) (*model.Channel, error)
	List(ctx context.Context, opts ChannelListOptions) ([]model.Channel, error)
	Update(ctx context.Context, tenantID, id uint, updates map[string]any) error
	Delete(ctx context.Context, tenantID, id uint) error
}

// ChannelListOptions 渠道列表查询条件（Repository 层只做数据过滤，不含业务判断）。
type ChannelListOptions struct {
	TenantID uint
	Keyword  string
	Enabled  *bool
	Limit    int
	Offset   int
}

type channelRepo struct {
	db *gorm.DB
}

// NewChannelRepository 创建渠道仓储（全局 DB 装配）。
func NewChannelRepository() ChannelRepository {
	return &channelRepo{db: _db.GetDB()}
}

// NewChannelRepositoryWithDB 用显式句柄构造（装配点/测试用）。
func NewChannelRepositoryWithDB(gormDB *gorm.DB) ChannelRepository {
	return &channelRepo{db: gormDB}
}

func (r *channelRepo) Available() bool { return r != nil && r.db != nil }

func (r *channelRepo) Create(ctx context.Context, ch *model.Channel) error {
	if !r.Available() {
		return ErrChannelStoreUnavailable
	}
	if ch == nil {
		return errors.New("渠道为空")
	}
	return r.db.WithContext(ctx).Create(ch).Error
}

func (r *channelRepo) GetByID(ctx context.Context, tenantID, id uint) (*model.Channel, error) {
	if !r.Available() {
		return nil, ErrChannelStoreUnavailable
	}
	var ch model.Channel
	err := r.db.WithContext(ctx).First(&ch, "tenant_id = ? AND id = ?", tenantID, id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &ch, nil
}

func (r *channelRepo) GetByCode(ctx context.Context, code string) (*model.Channel, error) {
	if !r.Available() {
		return nil, ErrChannelStoreUnavailable
	}
	var ch model.Channel
	err := r.db.WithContext(ctx).First(&ch, "code = ?", code).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &ch, nil
}

func (r *channelRepo) List(ctx context.Context, opts ChannelListOptions) ([]model.Channel, error) {
	if !r.Available() {
		return nil, ErrChannelStoreUnavailable
	}
	q := r.db.WithContext(ctx).Model(&model.Channel{}).Where("tenant_id = ?", opts.TenantID)
	if opts.Keyword != "" {
		q = q.Where("name ILIKE ? OR code ILIKE ?", "%"+opts.Keyword+"%", "%"+opts.Keyword+"%")
	}
	if opts.Enabled != nil {
		q = q.Where("enabled = ?", *opts.Enabled)
	}
	if opts.Limit > 0 {
		q = q.Limit(opts.Limit)
	}
	if opts.Offset > 0 {
		q = q.Offset(opts.Offset)
	}
	var rows []model.Channel
	if err := q.Order("id ASC").Find(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

func (r *channelRepo) Update(ctx context.Context, tenantID, id uint, updates map[string]any) error {
	if !r.Available() {
		return ErrChannelStoreUnavailable
	}
	if len(updates) == 0 {
		return nil
	}
	return r.db.WithContext(ctx).Model(&model.Channel{}).
		Where("tenant_id = ? AND id = ?", tenantID, id).
		Updates(updates).Error
}

func (r *channelRepo) Delete(ctx context.Context, tenantID, id uint) error {
	if !r.Available() {
		return ErrChannelStoreUnavailable
	}
	return r.db.WithContext(ctx).
		Where("tenant_id = ? AND id = ?", tenantID, id).
		Delete(&model.Channel{}).Error
}
