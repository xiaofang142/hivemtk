package repository

import (
	"context"
	"errors"
	"strings"
	"time"

	"hivemtk-user/internal/model"
	_db "hivemtk-user/internal/pkg/db"
	"hivemtk-user/internal/pkg/tenant"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// ErrIdentifyVersionConflict 识别码版本冲突（并发重置时 CAS 失败，调用方须重试/拒绝）。
var ErrIdentifyVersionConflict = errors.New("识别码版本冲突")

// ChannelIdentifyCodeRepository 存量渠道级识别码仓储（历史兼容；账号级权威在 ChannelAccountRepository）。
type ChannelIdentifyCodeRepository interface {
	Available() bool
	GetByChannel(ctx context.Context, tenantID, channelID uint) (*model.ChannelIdentifyCode, error)
	GetByCode(ctx context.Context, code string) (*model.ChannelIdentifyCode, error)
	ListByTenant(ctx context.Context, tenantID uint) ([]model.ChannelIdentifyCode, error)
	// ValidCodes 返回当前生效的全部识别码 + 重置在途窗口内仍有效的旧码。
	// 跨租户系统作用域读（凭证闸门在租户解析之前运行），供 guard 候选源使用。
	ValidCodes(ctx context.Context) ([]string, error)
	Upsert(ctx context.Context, rec *model.ChannelIdentifyCode) error
	// WithTransaction 在单个数据库事务内执行 fn，供服务层把权威码写入与冗余列写入
	// 原子化（CH-06）；fn 收到携带事务句柄的 ctx，仓储方法经 dbFromCtx 自动参与同一事务。
	WithTransaction(ctx context.Context, fn func(ctx context.Context) error) error
	// DeleteByChannel 按租户+渠道清除该渠道的全部识别码行（含重置在途旧码）。
	// 渠道删除时必须级联调用，否则已删渠道的旧码仍会被 ValidCodes 视为有效。
	DeleteByChannel(ctx context.Context, tenantID, channelID uint) error
}

type channelIdentifyCodeRepo struct {
	db *gorm.DB
}

// NewChannelIdentifyCodeRepository 创建识别码仓储。
func NewChannelIdentifyCodeRepository() ChannelIdentifyCodeRepository {
	return &channelIdentifyCodeRepo{db: _db.GetDB()}
}

// NewChannelIdentifyCodeRepositoryWithDB 用显式句柄构造。
func NewChannelIdentifyCodeRepositoryWithDB(gormDB *gorm.DB) ChannelIdentifyCodeRepository {
	return &channelIdentifyCodeRepo{db: gormDB}
}

func (r *channelIdentifyCodeRepo) Available() bool { return r != nil && r.db != nil }

func (r *channelIdentifyCodeRepo) GetByChannel(ctx context.Context, tenantID, channelID uint) (*model.ChannelIdentifyCode, error) {
	if !r.Available() {
		return nil, ErrChannelStoreUnavailable
	}
	var rec model.ChannelIdentifyCode
	err := dbFromCtx(ctx).
		First(&rec, "tenant_id = ? AND channel_id = ?", tenantID, channelID).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &rec, nil
}

// GetByCode 跨租户解析入口：显式系统作用域，限 2 行判歧义。
func (r *channelIdentifyCodeRepo) GetByCode(ctx context.Context, code string) (*model.ChannelIdentifyCode, error) {
	if !r.Available() {
		return nil, ErrChannelStoreUnavailable
	}
	if code == "" {
		return nil, nil
	}
	var rows []model.ChannelIdentifyCode
	if err := dbFromCtx(tenant.WithSystemScope(ctx)).
		Where("code = ?", code).Limit(2).Find(&rows).Error; err != nil {
		return nil, err
	}
	if len(rows) > 1 {
		return nil, ErrAmbiguousIdentifyCode
	}
	if len(rows) == 0 {
		return nil, nil
	}
	return &rows[0], nil
}

// findValidIdentifyCode 解析当前码或在途窗口内旧码：与 ValidCodes 同一时间判定，
// 窗口过后的旧码一律返回 nil（fail-closed）。包级函数供账号仓储复用，
// 不扩充 ChannelIdentifyCodeRepository 接口（避免破坏既有测试替身）。
func findValidIdentifyCode(ctx context.Context, gdb *gorm.DB, code string) (*model.ChannelIdentifyCode, error) {
	repo := &channelIdentifyCodeRepo{db: gdb}
	rec, err := repo.GetByCode(ctx, code)
	if err != nil || rec != nil {
		return rec, err
	}
	if code == "" {
		return nil, nil
	}
	var rows []model.ChannelIdentifyCode
	if err := gdb.WithContext(tenant.WithSystemScope(ctx)).
		Where("prev_code = ? AND prev_valid_until > ?", code, time.Now()).
		Limit(2).Find(&rows).Error; err != nil {
		return nil, err
	}
	if len(rows) > 1 {
		return nil, ErrAmbiguousIdentifyCode
	}
	if len(rows) == 0 {
		return nil, nil
	}
	return &rows[0], nil
}

func (r *channelIdentifyCodeRepo) ListByTenant(ctx context.Context, tenantID uint) ([]model.ChannelIdentifyCode, error) {
	if !r.Available() {
		return nil, ErrChannelStoreUnavailable
	}
	var rows []model.ChannelIdentifyCode
	if err := dbFromCtx(ctx).Where("tenant_id = ?", tenantID).Order("id ASC").Find(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

// ValidCodes 收集凭证闸门可接受的识别码集合。唯一权威来源是**账号级**识别码
// （channel_accounts.identify_code），每个账号一枚；重置在途旧码仅在
// PrevValidUntil 落在未来时有效。历史渠道级码（channel_identify_codes）已彻底
// 退出鉴权链，不再作为候选。窗口过后的旧码一律失效（fail-closed）。仓储运行
// 在租户解析之前，显式系统作用域。
func (r *channelIdentifyCodeRepo) ValidCodes(ctx context.Context) ([]string, error) {
	if !r.Available() {
		return nil, ErrChannelStoreUnavailable
	}
	sysCtx := tenant.WithSystemScope(ctx)
	now := time.Now()
	seen := make(map[string]bool)
	out := make([]string, 0)
	add := func(code string) {
		code = strings.TrimSpace(code)
		if code != "" && !seen[code] {
			seen[code] = true
			out = append(out, code)
		}
	}

	// ① 权威：账号当前码 + 账号重置在途旧码
	var accounts []model.ChannelAccount
	if err := dbFromCtx(sysCtx).Model(&model.ChannelAccount{}).
		Where("identify_code <> '' OR (prev_code <> '' AND prev_valid_until > ?)", now).
		Find(&accounts).Error; err != nil {
		return nil, err
	}
	for i := range accounts {
		add(accounts[i].IdentifyCode)
		if accounts[i].PrevValidUntil != nil && now.Before(*accounts[i].PrevValidUntil) {
			add(accounts[i].PrevCode)
		}
	}

	return out, nil
}

// Upsert 以 channel_id 为唯一键原子写入，避免「先查后建」并发撞唯一索引。
func (r *channelIdentifyCodeRepo) Upsert(ctx context.Context, rec *model.ChannelIdentifyCode) error {
	if !r.Available() {
		return ErrChannelStoreUnavailable
	}
	if rec == nil {
		return errors.New("识别码为空")
	}
	return dbFromCtx(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "channel_id"}},
		DoUpdates: clause.AssignmentColumns([]string{"code", "version", "prev_code", "prev_valid_until", "reset_at", "updated_at"}),
	}).Create(rec).Error
}

// RotateCAS 条件更新：WHERE tenant_id+channel_id+version=期望值，RowsAffected=0 即冲突。
// 首次生成（expectVersion=0）走 OnConflict DoNothing，两个并发首建只有一个成功。
func (r *channelIdentifyCodeRepo) RotateCAS(ctx context.Context, tenantID, channelID uint, expectVersion int, rec *model.ChannelIdentifyCode) error {
	if !r.Available() || rec == nil {
		return ErrChannelStoreUnavailable
	}
	if expectVersion <= 0 {
		// 并发首次轮转：DoNothing 抑制唯一键冲突但不返回错误；
		// 必须检查 RowsAffected，否则败者会把库里并不存在的码当成成功返回。
		res := dbFromCtx(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(rec)
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return ErrIdentifyVersionConflict
		}
		return nil
	}
	res := dbFromCtx(ctx).Model(&model.ChannelIdentifyCode{}).
		Where("tenant_id = ? AND channel_id = ? AND version = ?", tenantID, channelID, expectVersion).
		Updates(map[string]any{
			"code": rec.Code, "version": rec.Version,
			"prev_code": rec.PrevCode, "prev_valid_until": rec.PrevValidUntil,
			"reset_at": rec.ResetAt, "updated_at": time.Now(),
		})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrIdentifyVersionConflict
	}
	return nil
}

// DeleteByChannel 硬删除该渠道的全部识别码行。两表模型均无 gorm.DeletedAt，
// 口径与 ChannelAccountRepository.Delete 一致；缺 DB 句柄时 fail-closed。
func (r *channelIdentifyCodeRepo) DeleteByChannel(ctx context.Context, tenantID, channelID uint) error {
	if !r.Available() {
		return ErrChannelStoreUnavailable
	}
	return dbFromCtx(ctx).
		Where("tenant_id = ? AND channel_id = ?", tenantID, channelID).
		Delete(&model.ChannelIdentifyCode{}).Error
}

// WithTransaction 在单个数据库事务内执行 fn，并把事务句柄放进回调 ctx，
// 供服务层把权威码写入与冗余列写入纳入同一原子提交（CH-06）。
// 仓储方法经 dbFromCtx 读取 txCtx，确保读改写都在同一事务。
func (r *channelIdentifyCodeRepo) WithTransaction(ctx context.Context, fn func(ctx context.Context) error) error {
	if r == nil || r.db == nil {
		return ErrChannelStoreUnavailable
	}
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		return fn(context.WithValue(ctx, txContextKey{}, tx))
	})
}
