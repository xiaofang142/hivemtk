package repository

import (
	"context"
	"errors"

	"gorm.io/gorm"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tenant"
)

// IntentCatalogRepository 统一意图目录仓储（TB-10 刀1）。
//
// 租户作用域：写侧显式 TenantID（FromContext；无租户上下文回落
// DefaultTenantID）；读侧不拼 tenant_id，由 GORM 租户隔离 callback 盖章
// （CS-05 口径，同观测看板仓储）。唯一性由 service 写侧保证。
type IntentCatalogRepository interface {
	// ListEnabled 当前租户启用中目录行（识别/绑定消费方装载用）。
	ListEnabled(ctx context.Context) ([]model.IntentCatalog, error)
	// GetByKey 同租户按 intent_key 读启用中单行；未命中返回 (nil, nil)。
	GetByKey(ctx context.Context, intentKey string) (*model.IntentCatalog, error)
	// GetByKeyAny 同租户按 intent_key 读单行（忽略启用态；种子幂等判重用——
	// 禁用行也占位，不重建）。
	GetByKeyAny(ctx context.Context, intentKey string) (*model.IntentCatalog, error)
	// HasAnyRow 当前租户是否存在任意目录行（含禁用；区分「未播种」与「全禁用」）。
	HasAnyRow(ctx context.Context) (bool, error)
	Create(ctx context.Context, rec *model.IntentCatalog) error
	Update(ctx context.Context, rec *model.IntentCatalog) error
	Delete(ctx context.Context, id uint) error
}

type intentCatalogRepo struct {
	db *gorm.DB
}

// NewIntentCatalogRepositoryWithDB 显式句柄构造（装配层传 db，不回落全局 GetDB）。
func NewIntentCatalogRepositoryWithDB(db *gorm.DB) IntentCatalogRepository {
	return &intentCatalogRepo{db: db}
}

// tenantScope 从 ctx 解析租户；拿不到租户上下文按 DefaultTenantID 收敛
// （fail-closed：后台路径只看默认租户数据，不放大可见面）。
func (r *intentCatalogRepo) tenantScope(ctx context.Context) uint {
	if id, ok := tenant.FromContext(ctx); ok && id > 0 {
		return id
	}
	return tenant.DefaultTenantID
}

var errIntentCatalogDBNotReady = errors.New("intent_catalog repository: db not ready")

func (r *intentCatalogRepo) ListEnabled(ctx context.Context) ([]model.IntentCatalog, error) {
	if r.db == nil {
		return nil, errIntentCatalogDBNotReady
	}
	var rows []model.IntentCatalog
	err := r.db.WithContext(ctx).
		Where("tenant_id = ? AND enabled = ?", r.tenantScope(ctx), true).
		Order("intent_key asc").
		Find(&rows).Error
	if err != nil {
		return nil, err
	}
	return rows, nil
}

func (r *intentCatalogRepo) GetByKey(ctx context.Context, intentKey string) (*model.IntentCatalog, error) {
	if r.db == nil {
		return nil, errIntentCatalogDBNotReady
	}
	var row model.IntentCatalog
	err := r.db.WithContext(ctx).
		Where("tenant_id = ? AND intent_key = ? AND enabled = ?", r.tenantScope(ctx), intentKey, true).
		First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &row, nil
}

func (r *intentCatalogRepo) GetByKeyAny(ctx context.Context, intentKey string) (*model.IntentCatalog, error) {
	if r.db == nil {
		return nil, errIntentCatalogDBNotReady
	}
	var row model.IntentCatalog
	err := r.db.WithContext(ctx).
		Where("tenant_id = ? AND intent_key = ?", r.tenantScope(ctx), intentKey).
		First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &row, nil
}

func (r *intentCatalogRepo) HasAnyRow(ctx context.Context) (bool, error) {
	if r.db == nil {
		return false, errIntentCatalogDBNotReady
	}
	var n int64
	err := r.db.WithContext(ctx).Model(&model.IntentCatalog{}).
		Where("tenant_id = ?", r.tenantScope(ctx)).
		Count(&n).Error
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

func (r *intentCatalogRepo) Create(ctx context.Context, rec *model.IntentCatalog) error {
	if r.db == nil {
		return errIntentCatalogDBNotReady
	}
	if rec.TenantID == 0 {
		rec.TenantID = r.tenantScope(ctx)
	}
	return r.db.WithContext(ctx).Create(rec).Error
}

func (r *intentCatalogRepo) Update(ctx context.Context, rec *model.IntentCatalog) error {
	if r.db == nil {
		return errIntentCatalogDBNotReady
	}
	res := r.db.WithContext(ctx).Model(&model.IntentCatalog{}).
		Where("tenant_id = ? AND id = ?", r.tenantScope(ctx), rec.ID).
		Updates(map[string]any{
			"intent_key":    rec.IntentKey,
			"source":        rec.Source,
			"family":        rec.Family,
			"name":          rec.Name,
			"keywords_json": rec.KeywordsJSON,
			"examples_json": rec.ExamplesJSON,
			"description":   rec.Description,
			"enabled":       rec.Enabled,
		})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

func (r *intentCatalogRepo) Delete(ctx context.Context, id uint) error {
	if r.db == nil {
		return errIntentCatalogDBNotReady
	}
	res := r.db.WithContext(ctx).Model(&model.IntentCatalog{}).
		Where("tenant_id = ? AND id = ?", r.tenantScope(ctx), id).
		Delete(&model.IntentCatalog{})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}
