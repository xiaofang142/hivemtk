package repository

import (
	"context"
	"errors"
	"time"

	"gorm.io/gorm"

	"hivemtk-user/internal/model"
	_db "hivemtk-user/internal/pkg/db"
)

// GlossaryRepository 术语表仓储。
type GlossaryRepository struct {
	db *gorm.DB
}

func NewGlossaryRepository() *GlossaryRepository {
	return &GlossaryRepository{db: _db.GetDB()}
}

// NewGlossaryRepositoryWithDB 创建指定 DB 的术语表仓库实例；db 为 nil 时回退全局 DB。
func NewGlossaryRepositoryWithDB(db *gorm.DB) *GlossaryRepository {
	if db == nil {
		return &GlossaryRepository{db: _db.GetDB()}
	}
	return &GlossaryRepository{db: db}
}

func (r *GlossaryRepository) SetDB(_ context.Context, db *gorm.DB) {
	if db != nil {
		r.db = db
	}
}

// GetDB 返回内部 db（用于测试与子事务透传）。
func (r *GlossaryRepository) GetDB(ctx context.Context) *gorm.DB {
	if r.db == nil {
		return nil
	}
	return r.db.WithContext(ctx)
}

// Create 创建术语。
func (r *GlossaryRepository) Create(ctx context.Context, g *model.Glossary) error {
	if g == nil {
		return errors.New("glossary: nil entity")
	}
	// 软删除行仍占着 term_id 唯一索引：删除术语后重新添加同 term_id，直接 Create
	// 会抛 23505（service 层归一为 GlossaryDuplicateError「术语已存在」），而
	// Get/List 只看活跃行永远查不到——同 term_id 永久无法重建。命中历史行时
	// 原位复活并覆盖可编辑字段（与 geo site repo Create 同一模式，2026-11-13
	// K-4 台账轮实测复现后修复）。
	var existing model.Glossary
	if err := r.db.WithContext(ctx).Unscoped().
		Where("term_id = ?", g.TermID).First(&existing).Error; err == nil && existing.DeletedAt.Valid {
		now := time.Now()
		if err := r.db.WithContext(ctx).Unscoped().Model(&model.Glossary{}).
			Where("id = ?", existing.ID).
			Updates(map[string]any{
				"category":     g.Category,
				"preserve":     g.Preserve,
				"translations": g.Translations,
				"pattern":      g.Pattern,
				"status":       g.Status,
				"deleted_at":   nil,
				"updated_at":   now,
			}).Error; err != nil {
			return err
		}
		g.ID = existing.ID
		g.TenantID = existing.TenantID
		g.CreatedAt = existing.CreatedAt
		g.UpdatedAt = now
		g.DeletedAt = gorm.DeletedAt{}
		return nil
	}
	now := time.Now()
	if g.CreatedAt.IsZero() {
		g.CreatedAt = now
	}
	g.UpdatedAt = now
	return r.db.WithContext(ctx).Create(g).Error
}

// Update 按主键定向更新、显式列出业务列：不用 Save，因为 Save 会把整个结构体
// 写回，包括从未赋值的 tenant_id（=0）——离线单机档会把行写成不属于任何租户
// 的 tenant_id=0（默认租户自己也读不到），SaaS 档会撞「更新不可改写 tenant_id」
// 闸门直接 500。tenant_id 由隔离 callback 负责，updated_at 交给 autoUpdateTime。
func (r *GlossaryRepository) Update(ctx context.Context, g *model.Glossary) error {
	if g == nil {
		return errors.New("glossary: nil entity")
	}
	if g.ID == 0 {
		return errors.New("glossary: id required")
	}
	g.UpdatedAt = time.Now()
	res := r.db.WithContext(ctx).Model(&model.Glossary{}).
		Where("id = ?", g.ID).
		Select("term_id", "category", "preserve", "translations", "pattern", "status").
		Updates(g)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		// 术语已被删除或属于别的租户：白名单 Updates 只体现为 0 行。
		return gorm.ErrRecordNotFound
	}
	return nil
}

// Delete 按 TermID 删除术语。模型带 gorm.DeletedAt 时是软删除，否则物理删除
// （由 GORM 按模型定义决定）。命中 0 行（不存在/别租户）报不存在。
func (r *GlossaryRepository) Delete(ctx context.Context, termID string) error {
	if termID == "" {
		return errors.New("glossary: empty term_id")
	}
	res := r.db.WithContext(ctx).
		Where("term_id = ?", termID).
		Delete(&model.Glossary{})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

// GetByTermID 按 TermID 查询单条术语。
func (r *GlossaryRepository) GetByTermID(ctx context.Context, termID string) (*model.Glossary, error) {
	if termID == "" {
		return nil, errors.New("glossary: empty term_id")
	}
	var g model.Glossary
	if err := r.db.WithContext(ctx).Where("term_id = ?", termID).First(&g).Error; err != nil {
		return nil, err
	}
	return &g, nil
}

// GetByID 按主键 ID 查询单条术语。
func (r *GlossaryRepository) GetByID(ctx context.Context, id int64) (*model.Glossary, error) {
	var g model.Glossary
	if err := r.db.WithContext(ctx).First(&g, id).Error; err != nil {
		return nil, err
	}
	return &g, nil
}

// ListActive 查询所有 status=active 的术语（按 CreatedAt 升序，保证缓存稳定）。
func (r *GlossaryRepository) ListActive(ctx context.Context) ([]*model.Glossary, error) {
	var list []*model.Glossary
	err := r.db.WithContext(ctx).
		Where("status = ?", "active").
		Order("created_at ASC, id ASC").
		Find(&list).Error
	if err != nil {
		return nil, err
	}
	return list, nil
}

// ListByCategory 按分类查询 active 术语（category: brand / sku / logistic / policy / other）。
func (r *GlossaryRepository) ListByCategory(ctx context.Context, category string) ([]*model.Glossary, error) {
	if category == "" {
		return nil, errors.New("glossary: empty category")
	}
	var list []*model.Glossary
	err := r.db.WithContext(ctx).
		Where("status = ? AND category = ?", "active", category).
		Order("created_at ASC, id ASC").
		Find(&list).Error
	if err != nil {
		return nil, err
	}
	return list, nil
}

// ListAll 查询全部术语（含 disabled），用于管理后台分页查询。
// status 为空时不过滤；keyword 匹配 TermID（前缀）。
func (r *GlossaryRepository) ListAll(ctx context.Context, status, keyword string, page, pageSize int) ([]*model.Glossary, int64, error) {
	if page <= 0 {
		page = 1
	}
	if pageSize <= 0 {
		pageSize = 20
	}
	q := r.db.WithContext(ctx).Model(&model.Glossary{})
	if status != "" {
		q = q.Where("status = ?", status)
	}
	if keyword != "" {
		q = q.Where("term_id LIKE ?", keyword+"%")
	}
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var list []*model.Glossary
	if err := q.
		Order("created_at DESC, id DESC").
		Offset((page - 1) * pageSize).
		Limit(pageSize).
		Find(&list).Error; err != nil {
		return nil, 0, err
	}
	return list, total, nil
}
