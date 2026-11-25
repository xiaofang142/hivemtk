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

// ChannelIdentifyCodeRepository 渠道识别码仓储（权威记录）。
type ChannelIdentifyCodeRepository interface {
	Available() bool
	GetByChannel(ctx context.Context, tenantID, channelID uint) (*model.ChannelIdentifyCode, error)
	GetByCode(ctx context.Context, code string) (*model.ChannelIdentifyCode, error)
	ListByTenant(ctx context.Context, tenantID uint) ([]model.ChannelIdentifyCode, error)
	// ValidCodes 返回当前生效的全部识别码 + 重置在途窗口内仍有效的旧码。
	// 跨租户系统作用域读（凭证闸门在租户解析之前运行），供 guard 候选源使用。
	ValidCodes(ctx context.Context) ([]string, error)
	Upsert(ctx context.Context, rec *model.ChannelIdentifyCode) error
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
	err := r.db.WithContext(ctx).
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
	if err := r.db.WithContext(tenant.WithSystemScope(ctx)).
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
	if err := r.db.WithContext(ctx).Where("tenant_id = ?", tenantID).Order("id ASC").Find(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

// ValidCodes 收集凭证闸门可接受的识别码集合：当前码无条件有效；旧码仅在
// PrevValidUntil 落在未来时有效（在途消息窗口），窗口结束立即失效。
func (r *channelIdentifyCodeRepo) ValidCodes(ctx context.Context) ([]string, error) {
	if !r.Available() {
		return nil, ErrChannelStoreUnavailable
	}
	var rows []model.ChannelIdentifyCode
	if err := r.db.WithContext(tenant.WithSystemScope(ctx)).
		Where("code <> ''").Find(&rows).Error; err != nil {
		return nil, err
	}
	seen := make(map[string]bool, len(rows)*2)
	out := make([]string, 0, len(rows))
	now := time.Now()
	for i := range rows {
		if c := strings.TrimSpace(rows[i].Code); c != "" && !seen[c] {
			seen[c] = true
			out = append(out, c)
		}
		old := strings.TrimSpace(rows[i].PrevCode)
		if old == "" || seen[old] || rows[i].PrevValidUntil == nil {
			continue
		}
		if now.Before(*rows[i].PrevValidUntil) {
			seen[old] = true
			out = append(out, old)
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
	return r.db.WithContext(ctx).Clauses(clause.OnConflict{
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
		return r.db.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(rec).Error
	}
	res := r.db.WithContext(ctx).Model(&model.ChannelIdentifyCode{}).
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
	return r.db.WithContext(ctx).
		Where("tenant_id = ? AND channel_id = ?", tenantID, channelID).
		Delete(&model.ChannelIdentifyCode{}).Error
}
