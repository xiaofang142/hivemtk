package repository

import (
	"context"
	"errors"

	"gorm.io/gorm"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tenant"
)

// IntentToolChainRepository 意图→工具链绑定仓储（TB-01 绑定层数据面）。
//
// 租户作用域：显式 tenant_id 过滤（FromContext；单租户/无租户上下文回落
// DefaultTenantID，与 tenant.ContextWithDefault 语义一致），
// GORM 租户隔离 callback 作为第二道防线（CS-05 口径）。
type IntentToolChainRepository interface {
	Create(ctx context.Context, rec *model.IntentToolChain) error
	Update(ctx context.Context, rec *model.IntentToolChain) error
	Delete(ctx context.Context, id uint) error
	GetByID(ctx context.Context, id uint) (*model.IntentToolChain, error)
	// GetByIntent 同租户同意图唯一（service 写侧保证），未命中返回 (nil, nil)。
	GetByIntent(ctx context.Context, intentCode string) (*model.IntentToolChain, error)
	// ListEnabled 全量启用链（租户级），供 service 进程内缓存整表装载。
	ListEnabled(ctx context.Context) ([]model.IntentToolChain, error)
	List(ctx context.Context, intentCode string, page, pageSize int) ([]model.IntentToolChain, int64, error)
}

type intentToolChainRepo struct {
	db *gorm.DB
}

// NewIntentToolChainRepositoryWithDB 显式句柄构造（装配层传 db，不回落全局 GetDB）。
func NewIntentToolChainRepositoryWithDB(db *gorm.DB) IntentToolChainRepository {
	return &intentToolChainRepo{db: db}
}

// tenantScope 从 ctx 解析租户过滤；拿不到租户上下文按 DefaultTenantID 收敛
// （fail-closed：后台路径只看默认租户数据，不放大可见面）。
func (r *intentToolChainRepo) tenantScope(ctx context.Context) uint {
	if id, ok := tenant.FromContext(ctx); ok && id > 0 {
		return id
	}
	return tenant.DefaultTenantID
}

func (r *intentToolChainRepo) dbw(ctx context.Context) *gorm.DB {
	if r.db == nil {
		return nil
	}
	return r.db.WithContext(ctx).Model(&model.IntentToolChain{}).
		Where("tenant_id = ?", r.tenantScope(ctx))
}

var errRepoDBNotReady = errors.New("intent_tool_chain repository: db not ready")

func (r *intentToolChainRepo) Create(ctx context.Context, rec *model.IntentToolChain) error {
	if r.db == nil {
		return errRepoDBNotReady
	}
	if rec.TenantID == 0 {
		rec.TenantID = r.tenantScope(ctx)
	}
	return r.db.WithContext(ctx).Create(rec).Error
}

func (r *intentToolChainRepo) Update(ctx context.Context, rec *model.IntentToolChain) error {
	db := r.dbw(ctx)
	if db == nil {
		return errRepoDBNotReady
	}
	res := db.Where("id = ?", rec.ID).Updates(map[string]any{
		"name":       rec.Name,
		"steps_json": rec.StepsJSON,
		"slots_json": rec.SlotsJSON,
		"reply_hint": rec.ReplyHint,
		"enabled":    rec.Enabled,
	})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

func (r *intentToolChainRepo) Delete(ctx context.Context, id uint) error {
	db := r.dbw(ctx)
	if db == nil {
		return errRepoDBNotReady
	}
	res := db.Where("id = ?", id).Delete(&model.IntentToolChain{})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

func (r *intentToolChainRepo) GetByID(ctx context.Context, id uint) (*model.IntentToolChain, error) {
	db := r.dbw(ctx)
	if db == nil {
		return nil, errRepoDBNotReady
	}
	var rec model.IntentToolChain
	if err := db.Where("id = ?", id).First(&rec).Error; err != nil {
		return nil, err
	}
	return &rec, nil
}

func (r *intentToolChainRepo) GetByIntent(ctx context.Context, intentCode string) (*model.IntentToolChain, error) {
	db := r.dbw(ctx)
	if db == nil {
		return nil, errRepoDBNotReady
	}
	var rec model.IntentToolChain
	err := db.Where("intent_code = ?", intentCode).First(&rec).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &rec, nil
}

func (r *intentToolChainRepo) ListEnabled(ctx context.Context) ([]model.IntentToolChain, error) {
	db := r.dbw(ctx)
	if db == nil {
		return nil, errRepoDBNotReady
	}
	var rows []model.IntentToolChain
	if err := db.Where("enabled = ?", true).Find(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

func (r *intentToolChainRepo) List(ctx context.Context, intentCode string, page, pageSize int) ([]model.IntentToolChain, int64, error) {
	db := r.dbw(ctx)
	if db == nil {
		return nil, 0, errRepoDBNotReady
	}
	if intentCode != "" {
		db = db.Where("intent_code = ?", intentCode)
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
	var rows []model.IntentToolChain
	if err := db.Order("intent_code ASC, id ASC").
		Offset((page - 1) * pageSize).Limit(pageSize).
		Find(&rows).Error; err != nil {
		return nil, 0, err
	}
	return rows, total, nil
}
