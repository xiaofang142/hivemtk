package repository

import (
	"context"
	"errors"

	"hivemtk-user/internal/model"
	_db "hivemtk-user/internal/pkg/db"
	"hivemtk-user/internal/pkg/tenant"

	"gorm.io/gorm"
)

// ErrAmbiguousIdentifyCode 同一识别码命中多条渠道账号（歧义必须拒绝，不可任选一条）。
var ErrAmbiguousIdentifyCode = errors.New("识别码命中多条渠道账号，拒绝解析")

// ChannelAccountRepository 渠道账号仓储。
type ChannelAccountRepository interface {
	Available() bool
	Create(ctx context.Context, acc *model.ChannelAccount) error
	GetByID(ctx context.Context, tenantID, id uint) (*model.ChannelAccount, error)
	List(ctx context.Context, opts ChannelAccountListOptions) ([]model.ChannelAccount, error)
	GetByIDs(ctx context.Context, tenantID uint, ids []uint) ([]model.ChannelAccount, error)
	// GetByIdentifyCode 是跨租户解析入口，内部显式切系统作用域；歧义返回
	// ErrAmbiguousIdentifyCode，未命中返回 (nil, nil)。
	GetByIdentifyCode(ctx context.Context, code string) (*model.ChannelAccount, error)
	Update(ctx context.Context, tenantID, id uint, updates map[string]any) error
	Delete(ctx context.Context, tenantID, id uint) error
	// SetIdentifyCodeByChannel 双写识别码到渠道账号的冗余列（T016）。
	SetIdentifyCodeByChannel(ctx context.Context, tenantID, channelID uint, code string) error
}

// ChannelAccountListOptions 渠道账号列表/下拉统一查询条件。
// 所有内部选择必须经 ChannelAccountService.ListOptions 收口（FR-029）。
type ChannelAccountListOptions struct {
	TenantID  uint
	ChannelID uint
	Status    *int
	Keyword   string
	IDs       []uint
	Limit     int
	Offset    int
}

type channelAccountRepo struct {
	db *gorm.DB
}

// NewChannelAccountRepository 创建渠道账号仓储（全局 DB 装配）。
func NewChannelAccountRepository() ChannelAccountRepository {
	return &channelAccountRepo{db: _db.GetDB()}
}

// NewChannelAccountRepositoryWithDB 用显式句柄构造。
func NewChannelAccountRepositoryWithDB(gormDB *gorm.DB) ChannelAccountRepository {
	return &channelAccountRepo{db: gormDB}
}

func (r *channelAccountRepo) Available() bool { return r != nil && r.db != nil }

func (r *channelAccountRepo) Create(ctx context.Context, acc *model.ChannelAccount) error {
	if !r.Available() {
		return ErrChannelStoreUnavailable
	}
	if acc == nil {
		return errors.New("渠道账号为空")
	}
	return r.db.WithContext(ctx).Create(acc).Error
}

func (r *channelAccountRepo) GetByID(ctx context.Context, tenantID, id uint) (*model.ChannelAccount, error) {
	if !r.Available() {
		return nil, ErrChannelStoreUnavailable
	}
	var acc model.ChannelAccount
	err := r.db.WithContext(ctx).First(&acc, "tenant_id = ? AND id = ?", tenantID, id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &acc, nil
}

func (r *channelAccountRepo) List(ctx context.Context, opts ChannelAccountListOptions) ([]model.ChannelAccount, error) {
	if !r.Available() {
		return nil, ErrChannelStoreUnavailable
	}
	q := r.db.WithContext(ctx).Model(&model.ChannelAccount{}).Where("tenant_id = ?", opts.TenantID)
	if opts.ChannelID > 0 {
		q = q.Where("channel_id = ?", opts.ChannelID)
	}
	if opts.Status != nil {
		q = q.Where("status = ?", *opts.Status)
	}
	if opts.Keyword != "" {
		q = q.Where("name ILIKE ? OR identify_code ILIKE ?", "%"+opts.Keyword+"%", "%"+opts.Keyword+"%")
	}
	if len(opts.IDs) > 0 {
		q = q.Where("id IN ?", opts.IDs)
	}
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

func (r *channelAccountRepo) GetByIDs(ctx context.Context, tenantID uint, ids []uint) ([]model.ChannelAccount, error) {
	if !r.Available() {
		return nil, ErrChannelStoreUnavailable
	}
	if len(ids) == 0 {
		return nil, nil
	}
	var rows []model.ChannelAccount
	if err := r.db.WithContext(ctx).
		Where("tenant_id = ? AND id IN ?", tenantID, ids).
		Find(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

// GetByIdentifyCode 解析顺序：先查账号上的冗余码，再查权威识别码表并按
// (tenant_id, channel_id) 反查账号。两条都限制 2 行——命中 2 行即歧义拒绝。
func (r *channelAccountRepo) GetByIdentifyCode(ctx context.Context, code string) (*model.ChannelAccount, error) {
	if !r.Available() {
		return nil, ErrChannelStoreUnavailable
	}
	if code == "" {
		return nil, nil
	}
	sysCtx := tenant.WithSystemScope(ctx)

	var byCode []model.ChannelAccount
	if err := r.db.WithContext(sysCtx).Where("identify_code = ?", code).Limit(2).Find(&byCode).Error; err != nil {
		return nil, err
	}
	if len(byCode) > 1 {
		return nil, ErrAmbiguousIdentifyCode
	}
	if len(byCode) == 1 {
		return &byCode[0], nil
	}

	var codes []model.ChannelIdentifyCode
	if err := r.db.WithContext(sysCtx).Where("code = ?", code).Limit(2).Find(&codes).Error; err != nil {
		return nil, err
	}
	if len(codes) > 1 {
		return nil, ErrAmbiguousIdentifyCode
	}
	if len(codes) == 0 {
		return nil, nil
	}
	var byChannel []model.ChannelAccount
	if err := r.db.WithContext(sysCtx).
		Where("tenant_id = ? AND channel_id = ?", codes[0].TenantID, codes[0].ChannelID).
		Limit(2).Find(&byChannel).Error; err != nil {
		return nil, err
	}
	if len(byChannel) > 1 {
		return nil, ErrAmbiguousIdentifyCode
	}
	if len(byChannel) == 0 {
		return nil, nil
	}
	return &byChannel[0], nil
}

func (r *channelAccountRepo) Update(ctx context.Context, tenantID, id uint, updates map[string]any) error {
	if !r.Available() {
		return ErrChannelStoreUnavailable
	}
	if len(updates) == 0 {
		return nil
	}
	return r.db.WithContext(ctx).Model(&model.ChannelAccount{}).
		Where("tenant_id = ? AND id = ?", tenantID, id).
		Updates(updates).Error
}

func (r *channelAccountRepo) Delete(ctx context.Context, tenantID, id uint) error {
	if !r.Available() {
		return ErrChannelStoreUnavailable
	}
	return r.db.WithContext(ctx).
		Where("tenant_id = ? AND id = ?", tenantID, id).
		Delete(&model.ChannelAccount{}).Error
}

func (r *channelAccountRepo) SetIdentifyCodeByChannel(ctx context.Context, tenantID, channelID uint, code string) error {
	if !r.Available() {
		return ErrChannelStoreUnavailable
	}
	return r.db.WithContext(ctx).Model(&model.ChannelAccount{}).
		Where("tenant_id = ? AND channel_id = ?", tenantID, channelID).
		Update("identify_code", code).Error
}
