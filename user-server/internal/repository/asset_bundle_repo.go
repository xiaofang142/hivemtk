// Package repository 提供 AssetBundle（资产包）的 CRUD 仓储实现。
//
// 设计原则：
//  1. 五层架构：Repository 只做 GORM 映射，不做业务校验（业务校验放 Service）
//  2. 软删除：使用 gorm.DeletedAt
//  3. 唯一约束：AssetID 业务唯一键
//  4. 索引：Status / Scope / Author / Industry / Language
package repository

import (
	"context"
	"errors"
	"strings"

	"hivemtk-user/internal/model"

	"github.com/lib/pq"
	"gorm.io/gorm"
)

// applyListLimit 统一"可选 limit"查询语义:limit<=0 时默认 500,防止增长型表无界全表载入。
// 函数调用方几乎都显式传参;该兜底仅封顶 cron/旧调用方的不设防路径。
func applyListLimit(q *gorm.DB, limit int) *gorm.DB {
	if limit > 0 {
		return q.Limit(limit)
	}
	return q.Limit(500)
}

// AssetBundleFilter 列表过滤
type AssetBundleFilter struct {
	Keyword  string
	Author   string
	Industry string
	Language string
	Scope    model.AssetBundleScope
	Status   model.AssetBundleStatus
	Tags     []string
	Page     int
	Size     int
}

// AssetBundleRepository 资产包仓储接口
type AssetBundleRepository interface {
	Create(ctx context.Context, m *model.AssetBundle) error
	Update(ctx context.Context, m *model.AssetBundle) error
	SoftDelete(ctx context.Context, id int64) error
	FindByID(ctx context.Context, id int64) (*model.AssetBundle, error)
	FindByAssetID(ctx context.Context, assetID string) (*model.AssetBundle, error)
	List(ctx context.Context, f AssetBundleFilter) ([]*model.AssetBundle, int64, error)
	IncrementUseCount(ctx context.Context, assetID string) error
	ExistsByAssetID(ctx context.Context, assetID string) (bool, error)
}

type assetBundleRepo struct {
	db *gorm.DB
}

func NewAssetBundleRepository(db *gorm.DB) AssetBundleRepository {
	return &assetBundleRepo{db: db}
}

func (r *assetBundleRepo) Create(ctx context.Context, m *model.AssetBundle) error {
	if m == nil {
		return errors.New("asset bundle is nil")
	}
	return r.db.WithContext(ctx).Create(m).Error
}

// assetBundleUpdateColumns 允许经 Update 回写的列。
//
// 刻意不含身份与计数列：asset_id / tenant_id 归创建时定死，use_count 由
// IncrementUseCount 专用路径自增，rating* 目前无写路径。examples /
// supported_languages 也不在内——HTTP 更新 DTO 不携带这两列，若放进白名单，
// 一次普通编辑会把种子写入的示例与语言清空。
var assetBundleUpdateColumns = []string{
	"title", "description", "author", "version", "scope", "status",
	"industry", "language", "tags", "messages", "cover_image",
}

func (r *assetBundleRepo) Update(ctx context.Context, m *model.AssetBundle) error {
	if m == nil {
		return errors.New("asset bundle is nil")
	}
	res := r.db.WithContext(ctx).Model(&model.AssetBundle{}).
		Where("id = ?", m.ID).
		Select(assetBundleUpdateColumns).
		Updates(m)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

func (r *assetBundleRepo) SoftDelete(ctx context.Context, id int64) error {
	res := r.db.WithContext(ctx).Delete(&model.AssetBundle{}, id)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

func (r *assetBundleRepo) FindByID(ctx context.Context, id int64) (*model.AssetBundle, error) {
	var m model.AssetBundle
	if err := r.db.WithContext(ctx).First(&m, id).Error; err != nil {
		return nil, err
	}
	return &m, nil
}

func (r *assetBundleRepo) FindByAssetID(ctx context.Context, assetID string) (*model.AssetBundle, error) {
	var m model.AssetBundle
	if err := r.db.WithContext(ctx).Where("asset_id = ?", assetID).First(&m).Error; err != nil {
		return nil, err
	}
	return &m, nil
}

func (r *assetBundleRepo) List(ctx context.Context, f AssetBundleFilter) ([]*model.AssetBundle, int64, error) {
	var list []*model.AssetBundle
	var total int64
	q := r.db.WithContext(ctx).Model(&model.AssetBundle{}).Where("deleted_at IS NULL")

	if strings.TrimSpace(f.Keyword) != "" {
		kw := "%" + f.Keyword + "%"
		q = q.Where("title ILIKE ? OR description ILIKE ?", kw, kw)
	}
	if f.Author != "" {
		q = q.Where("author = ?", f.Author)
	}
	if f.Industry != "" {
		q = q.Where("industry = ?", f.Industry)
	}
	if f.Language != "" {
		q = q.Where("language = ?", f.Language)
	}
	if f.Scope != "" {
		q = q.Where("scope = ?", f.Scope)
	}
	if f.Status != "" {
		q = q.Where("status = ?", f.Status)
	}
	if len(f.Tags) > 0 {
		// 必须包成 pq.StringArray：直接传 []string 时 GORM 会把它展开成
		// `tags && $1,$2` 这种非法数组字面量，PG 直接报 SQLSTATE 22P02，
		// 而且原始 SQL 会被回传前端。&& 是数组重叠运算符，右侧必须是单个数组。
		q = q.Where("tags && ?", pq.StringArray(f.Tags))
	}
	if f.Page <= 0 {
		f.Page = 1
	}
	if f.Size <= 0 {
		f.Size = 20
	}
	if f.Size > 200 {
		f.Size = 200
	}
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	err := q.Order("updated_at DESC, id DESC").Offset((f.Page - 1) * f.Size).Limit(f.Size).Find(&list).Error
	return list, total, err
}

func (r *assetBundleRepo) IncrementUseCount(ctx context.Context, assetID string) error {
	return r.db.WithContext(ctx).Model(&model.AssetBundle{}).
		Where("asset_id = ?", assetID).
		UpdateColumn("use_count", gorm.Expr("use_count + 1")).Error
}

func (r *assetBundleRepo) ExistsByAssetID(ctx context.Context, assetID string) (bool, error) {
	var c int64
	if err := r.db.WithContext(ctx).Model(&model.AssetBundle{}).
		Where("asset_id = ?", assetID).Count(&c).Error; err != nil {
		return false, err
	}
	return c > 0, nil
}
