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
	"time"

	"hivemtk-user/internal/model"

	"github.com/lib/pq"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
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
	UpdatePlatformRef(ctx context.Context, id int64, platformAssetID int64, status, note string) error
	UpsertRating(ctx context.Context, m *model.AssetBundleRating) error
	RatingSummary(ctx context.Context, assetID string) (float64, int64, error)
	ApplyRating(ctx context.Context, assetID string, rating float64, count int64) error
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
		// 前端 placeholder 承诺「搜索标题 / Asset ID」，所以 asset_id 也必须参与
		// 关键词匹配；只搜 title/description 会让「按 Asset ID 找包」永远落空
		// （asset_id 是用户在列表页最常用的精确定位键）。
		q = q.Where("title ILIKE ? OR description ILIKE ? OR asset_id ILIKE ?", kw, kw, kw)
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

// UpdatePlatformRef 落库平台提交回执（platform_asset_id + 审核状态）。
// 独立于 Update 白名单：业务更新不得碰回执列，回执只由提交流程写入。
func (r *assetBundleRepo) UpdatePlatformRef(ctx context.Context, id int64, platformAssetID int64, status, note string) error {
	return r.db.WithContext(ctx).Model(&model.AssetBundle{}).
		Where("id = ?", id).
		Updates(map[string]any{
			"platform_asset_id":     platformAssetID,
			"platform_status":       status,
			"platform_review_note":  note,
			"platform_submitted_at": time.Now(),
		}).Error
}

// UpsertRating 写入/更新评分（一人一包一票：唯一索引冲突则改分）。
// 冲突列与 tenancyidx 规范化后的 (tenant_id,asset_id,user_id) 唯一索引对齐。
func (r *assetBundleRepo) UpsertRating(ctx context.Context, m *model.AssetBundleRating) error {
	if m == nil {
		return errors.New("asset bundle rating is nil")
	}
	return r.db.WithContext(ctx).
		Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "tenant_id"}, {Name: "asset_id"}, {Name: "user_id"}},
			DoUpdates: clause.Assignments(map[string]any{"score": m.Score, "updated_at": time.Now()}),
		}).
		Create(m).Error
}

// RatingSummary 聚合评分（平均分 + 票数）
func (r *assetBundleRepo) RatingSummary(ctx context.Context, assetID string) (float64, int64, error) {
	var out struct {
		Avg   float64 `gorm:"column:avg"`
		Count int64   `gorm:"column:cnt"`
	}
	err := r.db.WithContext(ctx).Model(&model.AssetBundleRating{}).
		Select("COALESCE(AVG(score), 0) AS avg, COUNT(*) AS cnt").
		Where("asset_id = ?", assetID).
		Scan(&out).Error
	return out.Avg, out.Count, err
}

// ApplyRating 把聚合结果回写资产包 rating/rating_count 列（与 Update 白名单隔离的专用路径）
func (r *assetBundleRepo) ApplyRating(ctx context.Context, assetID string, rating float64, count int64) error {
	return r.db.WithContext(ctx).Model(&model.AssetBundle{}).
		Where("asset_id = ?", assetID).
		Updates(map[string]any{"rating": rating, "rating_count": count}).Error
}
