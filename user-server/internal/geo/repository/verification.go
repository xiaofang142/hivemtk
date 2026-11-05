package repository

import (
	"context"
	"hivemtk-user/internal/geo/model"
	_db "hivemtk-user/internal/pkg/db"
	"time"

	"gorm.io/gorm"
)

// GeoVerifyResultRepository GEO 验证结果仓储接口
type GeoVerifyResultRepository interface {
	Create(ctx context.Context, result *model.GeoVerifyResult) error
	// GetByArticleID 单篇文章的验证结果（最近 200 条，接口无分页参数）
	GetByArticleID(ctx context.Context, articleID string) ([]*model.GeoVerifyResult, error)
	// GetByBrandName 品牌历史验证结果（最近 2000 条，分析用）
	GetByBrandName(ctx context.Context, brandName string) ([]*model.GeoVerifyResult, error)
	GetStatistics(ctx context.Context) ([]map[string]any, error)
}

type geoVerifyResultRepo struct {
	db *gorm.DB
}

func NewGeoVerifyResultRepository() GeoVerifyResultRepository {
	return &geoVerifyResultRepo{db: _db.GetDB()}
}

// NewGeoVerifyResultRepositoryWithDB 创建指定数据库连接的实例（用于测试）
func NewGeoVerifyResultRepositoryWithDB(db *gorm.DB) GeoVerifyResultRepository {
	return &geoVerifyResultRepo{db: db}
}

func (r *geoVerifyResultRepo) Create(ctx context.Context, result *model.GeoVerifyResult) error {
	return r.db.WithContext(ctx).Create(result).Error
}

func (r *geoVerifyResultRepo) GetByArticleID(ctx context.Context, articleID string) ([]*model.GeoVerifyResult, error) {
	var results []*model.GeoVerifyResult
	err := r.db.WithContext(ctx).Where("article_id = ?", articleID).
		Order("created_at DESC").Limit(200).Find(&results).Error
	return results, err
}

func (r *geoVerifyResultRepo) GetByBrandName(ctx context.Context, brandName string) ([]*model.GeoVerifyResult, error) {
	var results []*model.GeoVerifyResult
	err := r.db.WithContext(ctx).Where("brand_name = ?", brandName).
		Order("created_at DESC").Limit(2000).Find(&results).Error
	return results, err
}

func (r *geoVerifyResultRepo) GetStatistics(ctx context.Context) ([]map[string]any, error) {
	var statistics []map[string]any
	err := r.db.WithContext(ctx).Model(&model.GeoVerifyResult{}).
		Select("model AS model, COUNT(*) AS total, " +
			"SUM(CASE WHEN brand_mentioned = true THEN 1 ELSE 0 END) AS mentioned_total").
		Group("model").Order("model").
		Scan(&statistics).Error
	return statistics, err
}

// GeoAPICallRepository GEO API 调用记录仓储接口
type GeoAPICallRepository interface {
	Create(ctx context.Context, call *model.GeoAPICall) error
	GetCostStatistics(ctx context.Context, startDate, endDate string) ([]map[string]any, error)
}

type geoAPICallRepo struct {
	db *gorm.DB
}

func NewGeoAPICallRepository() GeoAPICallRepository {
	return &geoAPICallRepo{db: _db.GetDB()}
}

// NewGeoAPICallRepositoryWithDB 创建指定数据库连接的实例（用于测试）
func NewGeoAPICallRepositoryWithDB(db *gorm.DB) GeoAPICallRepository {
	return &geoAPICallRepo{db: db}
}

func (r *geoAPICallRepo) Create(ctx context.Context, call *model.GeoAPICall) error {
	return r.db.WithContext(ctx).Create(call).Error
}

func (r *geoAPICallRepo) GetCostStatistics(ctx context.Context, startDate, endDate string) ([]map[string]any, error) {
	var statistics []map[string]any

	query := r.db.WithContext(ctx).Model(&model.GeoAPICall{}).
		Select("provider, model, COUNT(*) AS call_count, COALESCE(SUM(input_tokens),0) AS input_tokens, COALESCE(SUM(output_tokens),0) AS output_tokens, COALESCE(SUM(cost_usd),0) AS cost_usd, COALESCE(SUM(cost_cny),0) AS cost_cny").
		Group("provider, model").
		Order("provider, model")

	if startDate != "" {
		query = query.Where("created_at >= ?", startDate)
	}
	if endDate != "" {
		// 前端传 YYYY-MM-DD：`<= '2026-11-06'` 会被 PG 折算成当天 00:00，
		// 整个结束日的数据全丢。日期形态改成半开区间 [start, end+1d)。
		if t, perr := time.Parse("2006-01-02", endDate); perr == nil {
			query = query.Where("created_at < ?", t.AddDate(0, 0, 1))
		} else {
			query = query.Where("created_at <= ?", endDate)
		}
	}

	err := query.Scan(&statistics).Error
	return statistics, err
}
