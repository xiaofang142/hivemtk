package repository

import (
	"context"
	"errors"
	"time"

	"hivemtk-user/internal/model"
	_db "hivemtk-user/internal/pkg/db"
	"hivemtk-user/internal/pkg/edition"
	"hivemtk-user/internal/pkg/tenant"

	"gorm.io/gorm"
)

// ErrAmbiguousIdentifyCode 同一识别码命中多条渠道账号（歧义必须拒绝，不可任选一条）。
var ErrAmbiguousIdentifyCode = errors.New("识别码命中多条渠道账号，拒绝解析")

// ErrChannelAccountChannelUnavailable 账号引用的渠道不存在，或当前 edition
// 不允许该渠道。写侧用于 fail-closed；读侧通过 gate 直接过滤，不返回该错误。
var ErrChannelAccountChannelUnavailable = errors.New("渠道账号引用的渠道不存在或不可用")

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
	// DeleteByChannel 按租户+渠道删除该渠道下全部账号（含 identify_code 冗余列），
	// 配合渠道删除收口「按码反查账号」的旧码入口。
	DeleteByChannel(ctx context.Context, tenantID, channelID uint) error
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

// applyChannelEditionGate 是渠道账号所有读取/写入查询的统一版本门。
// custom 原样放行；opensource/unknown 仅保留非国内渠道，并且自然排除孤儿账号。
// 只使用 GORM 参数绑定，不把 edition 策略写成数据库方言 SQL。
func applyChannelEditionGate(ctx context.Context, q *gorm.DB) *gorm.DB {
	if edition.AllowsContext(ctx, edition.FeatureDomesticChannels) {
		return q
	}
	// 用 GORM 子查询而非裸 SQL 字面量：租户隔离 callback 会在子查询的 Query 链上
	// 照常注入 tenant_id；裸字面量里的 (SELECT … FROM channels) 是 callback
	// 注入不到的盲区（见 internal/router/tenant_scan_test.go S-18）。
	sub := q.Session(&gorm.Session{NewDB: true}).
		Model(&model.Channel{}).
		Select("id").
		Where("code NOT IN ?", edition.DomesticChannelCodes())
	return q.Where("channel_id IN (?)", sub)
}

func (r *channelAccountRepo) validateChannelForAccount(ctx context.Context, channelID uint) error {
	if channelID == 0 {
		return ErrChannelAccountChannelUnavailable
	}
	q := dbFromCtx(tenant.WithSystemScope(ctx)).Model(&model.Channel{}).Where("id = ?", channelID)
	if !edition.AllowsContext(ctx, edition.FeatureDomesticChannels) {
		q = q.Where("code NOT IN ?", edition.DomesticChannelCodes())
	}
	var count int64
	if err := q.Count(&count).Error; err != nil {
		return err
	}
	if count == 0 {
		return ErrChannelAccountChannelUnavailable
	}
	return nil
}

func accountChannelID(value any) (uint, bool) {
	switch id := value.(type) {
	case uint:
		return id, id > 0
	case uint64:
		return uint(id), id > 0 && uint64(uint(id)) == id
	case int:
		return uint(id), id > 0 && uint64(id) <= uint64(^uint(0))
	case int64:
		return uint(id), id > 0 && uint64(id) <= uint64(^uint(0))
	default:
		return 0, false
	}
}

func (r *channelAccountRepo) Create(ctx context.Context, acc *model.ChannelAccount) error {
	if !r.Available() {
		return ErrChannelStoreUnavailable
	}
	if acc == nil {
		return errors.New("渠道账号为空")
	}
	if err := r.validateChannelForAccount(ctx, acc.ChannelID); err != nil {
		return err
	}
	return dbFromCtx(ctx).Create(acc).Error
}

func (r *channelAccountRepo) GetByID(ctx context.Context, tenantID, id uint) (*model.ChannelAccount, error) {
	if !r.Available() {
		return nil, ErrChannelStoreUnavailable
	}
	var acc model.ChannelAccount
	err := applyChannelEditionGate(ctx, dbFromCtx(ctx).Model(&model.ChannelAccount{}).
		Where("tenant_id = ? AND id = ?", tenantID, id)).First(&acc).Error
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
	q := applyChannelEditionGate(ctx, dbFromCtx(ctx).Model(&model.ChannelAccount{}).
		Where("tenant_id = ?", opts.TenantID))
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
	if err := applyChannelEditionGate(ctx, dbFromCtx(ctx).Model(&model.ChannelAccount{}).
		Where("tenant_id = ? AND id IN ?", tenantID, ids)).Find(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

// GetByIdentifyCode 解析顺序：先查账号上的冗余码，再查权威识别码表并按
// (tenant_id, channel_id) 反查账号。两条路径都在 Limit(2)/歧义判断前应用 edition gate。
func (r *channelAccountRepo) GetByIdentifyCode(ctx context.Context, code string) (*model.ChannelAccount, error) {
	if !r.Available() {
		return nil, ErrChannelStoreUnavailable
	}
	if code == "" {
		return nil, nil
	}
	sysCtx := tenant.WithSystemScope(ctx)

	var byCode []model.ChannelAccount
	if err := applyChannelEditionGate(ctx, dbFromCtx(sysCtx).Model(&model.ChannelAccount{}).
		Where("identify_code = ?", code)).Limit(2).Find(&byCode).Error; err != nil {
		return nil, err
	}
	if len(byCode) > 1 {
		return nil, ErrAmbiguousIdentifyCode
	}
	if len(byCode) == 1 {
		return &byCode[0], nil
	}

	// 账号级重置在途窗口：旧码在 PrevValidUntil 之前仍解析回同一账号。
	// 与 ValidCodes 的账号候选源保持同一时间判定，窗口外旧码一律 nil（fail-closed）。
	var byPrev []model.ChannelAccount
	if err := applyChannelEditionGate(ctx, dbFromCtx(sysCtx).Model(&model.ChannelAccount{}).
		Where("prev_code = ? AND prev_valid_until > ?", code, time.Now())).Limit(2).Find(&byPrev).Error; err != nil {
		return nil, err
	}
	if len(byPrev) > 1 {
		return nil, ErrAmbiguousIdentifyCode
	}
	if len(byPrev) == 1 {
		return &byPrev[0], nil
	}

	// 历史兼容的渠道级码表：当前码或在途窗口内旧码都接受，窗口外旧码返回 nil
	// （fail-closed）。渠道码只确定 tenant+channel，多账号时由调用方用明确账号
	// 信息消歧，这里绝不取首条。
	rec, err := findValidIdentifyCode(ctx, dbFromCtx(ctx), code)
	if err != nil {
		return nil, err
	}
	if rec == nil {
		return nil, nil
	}
	var byChannel []model.ChannelAccount
	if err := applyChannelEditionGate(ctx, dbFromCtx(sysCtx).Model(&model.ChannelAccount{}).
		Where("tenant_id = ? AND channel_id = ?", rec.TenantID, rec.ChannelID)).
		Limit(2).Find(&byChannel).Error; err != nil {
		return nil, err
	}
	if len(byChannel) > 1 {
		return nil, ErrAmbiguousIdentifyCode // 缺消歧信息：拒绝，禁止取首条
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
	if rawChannelID, ok := updates["channel_id"]; ok {
		channelID, valid := accountChannelID(rawChannelID)
		if !valid {
			return ErrChannelAccountChannelUnavailable
		}
		if err := r.validateChannelForAccount(ctx, channelID); err != nil {
			return err
		}
	}
	return applyChannelEditionGate(ctx, dbFromCtx(ctx).Model(&model.ChannelAccount{}).
		Where("tenant_id = ? AND id = ?", tenantID, id)).Updates(updates).Error
}

func (r *channelAccountRepo) Delete(ctx context.Context, tenantID, id uint) error {
	if !r.Available() {
		return ErrChannelStoreUnavailable
	}
	return applyChannelEditionGate(ctx, dbFromCtx(ctx).Model(&model.ChannelAccount{}).
		Where("tenant_id = ? AND id = ?", tenantID, id)).Delete(&model.ChannelAccount{}).Error
}

// DeleteByChannel 硬删除该渠道下的全部账号行，口径与 Delete 一致。
func (r *channelAccountRepo) DeleteByChannel(ctx context.Context, tenantID, channelID uint) error {
	if !r.Available() {
		return ErrChannelStoreUnavailable
	}
	return applyChannelEditionGate(ctx, dbFromCtx(ctx).Model(&model.ChannelAccount{}).
		Where("tenant_id = ? AND channel_id = ?", tenantID, channelID)).Delete(&model.ChannelAccount{}).Error
}

func (r *channelAccountRepo) SetIdentifyCodeByChannel(ctx context.Context, tenantID, channelID uint, code string) error {
	if !r.Available() {
		return ErrChannelStoreUnavailable
	}
	return applyChannelEditionGate(ctx, dbFromCtx(ctx).Model(&model.ChannelAccount{}).
		Where("tenant_id = ? AND channel_id = ?", tenantID, channelID)).Update("identify_code", code).Error
}
