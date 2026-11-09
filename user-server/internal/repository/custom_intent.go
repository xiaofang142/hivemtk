package repository

import (
	"context"
	"errors"

	"gorm.io/gorm"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tenant"
)

// CustomIntentRepository 自定义意图词条仓储（T-09）。
// 租户作用域显式过滤（与 intent_tool_chain_repo 同口径：无 ctx 按 DefaultTenantID 收敛）。
type CustomIntentRepository interface {
	Create(ctx context.Context, rec *model.CustomIntent) error
	Update(ctx context.Context, rec *model.CustomIntent) error
	Delete(ctx context.Context, id uint) error
	GetByID(ctx context.Context, id uint) (*model.CustomIntent, error)
	GetByType(ctx context.Context, intentType string) (*model.CustomIntent, error)
	ListEnabled(ctx context.Context) ([]model.CustomIntent, error)
	List(ctx context.Context, page, pageSize int) ([]model.CustomIntent, int64, error)
}

type customIntentRepo struct {
	db *gorm.DB
}

func NewCustomIntentRepositoryWithDB(db *gorm.DB) CustomIntentRepository {
	return &customIntentRepo{db: db}
}

func (r *customIntentRepo) tenantScope(ctx context.Context) uint {
	if id, ok := tenant.FromContext(ctx); ok && id > 0 {
		return id
	}
	return tenant.DefaultTenantID
}

func (r *customIntentRepo) dbw(ctx context.Context) *gorm.DB {
	if r.db == nil {
		return nil
	}
	return r.db.WithContext(ctx).Model(&model.CustomIntent{}).
		Where("tenant_id = ?", r.tenantScope(ctx))
}

var errCustomIntentDBNotReady = errors.New("custom_intent repository: db not ready")

func (r *customIntentRepo) Create(ctx context.Context, rec *model.CustomIntent) error {
	if r.db == nil {
		return errCustomIntentDBNotReady
	}
	if rec.TenantID == 0 {
		rec.TenantID = r.tenantScope(ctx)
	}
	return r.db.WithContext(ctx).Create(rec).Error
}

func (r *customIntentRepo) Update(ctx context.Context, rec *model.CustomIntent) error {
	db := r.dbw(ctx)
	if db == nil {
		return errCustomIntentDBNotReady
	}
	res := db.Where("id = ?", rec.ID).Updates(map[string]any{
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

func (r *customIntentRepo) Delete(ctx context.Context, id uint) error {
	db := r.dbw(ctx)
	if db == nil {
		return errCustomIntentDBNotReady
	}
	res := db.Where("id = ?", id).Delete(&model.CustomIntent{})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

func (r *customIntentRepo) GetByID(ctx context.Context, id uint) (*model.CustomIntent, error) {
	db := r.dbw(ctx)
	if db == nil {
		return nil, errCustomIntentDBNotReady
	}
	var rec model.CustomIntent
	if err := db.Where("id = ?", id).First(&rec).Error; err != nil {
		return nil, err
	}
	return &rec, nil
}

func (r *customIntentRepo) GetByType(ctx context.Context, intentType string) (*model.CustomIntent, error) {
	db := r.dbw(ctx)
	if db == nil {
		return nil, errCustomIntentDBNotReady
	}
	var rec model.CustomIntent
	err := db.Where("type = ?", intentType).First(&rec).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &rec, nil
}

func (r *customIntentRepo) ListEnabled(ctx context.Context) ([]model.CustomIntent, error) {
	db := r.dbw(ctx)
	if db == nil {
		return nil, errCustomIntentDBNotReady
	}
	var rows []model.CustomIntent
	if err := db.Where("enabled = ?", true).Order("id ASC").Find(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

func (r *customIntentRepo) List(ctx context.Context, page, pageSize int) ([]model.CustomIntent, int64, error) {
	db := r.dbw(ctx)
	if db == nil {
		return nil, 0, errCustomIntentDBNotReady
	}
	var total int64
	if err := db.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	if page <= 0 {
		page = 1
	}
	if pageSize <= 0 || pageSize > 200 {
		pageSize = 50
	}
	var rows []model.CustomIntent
	if err := db.Order("id ASC").Offset((page - 1) * pageSize).Limit(pageSize).Find(&rows).Error; err != nil {
		return nil, 0, err
	}
	return rows, total, nil
}
