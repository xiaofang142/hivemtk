package repository

import (
	"context"
	"hivemtk-user/internal/aiagent/knowledge/model"
	"hivemtk-user/internal/pkg/timeutil"
	"time"

	"gorm.io/gorm"
)

// KnowledgeSearchLogRepository 知识库检索日志仓储
type KnowledgeSearchLogRepository struct {
	db *gorm.DB
}

// NewKnowledgeSearchLogRepository 创建检索日志仓储
func NewKnowledgeSearchLogRepository(db *gorm.DB) *KnowledgeSearchLogRepository {
	return &KnowledgeSearchLogRepository{db: db}
}

// Create 创建日志
func (r *KnowledgeSearchLogRepository) Create(ctx context.Context, log *model.KnowledgeSearchLog) error {
	return r.db.WithContext(ctx).Create(log).Error
}

// ListFilter 检索日志筛选
type SearchLogListFilter struct {
	ProductID string
	StartTime *time.Time
	EndTime   *time.Time
	Hit       *int
	Page      int
	PageSize  int
}

// List 列出检索日志
func (r *KnowledgeSearchLogRepository) List(ctx context.Context, filter SearchLogListFilter) ([]model.KnowledgeSearchLog, int64, error) {
	if filter.Page < 1 {
		filter.Page = 1
	}
	if filter.PageSize < 1 {
		filter.PageSize = 20
	}
	q := r.db.WithContext(ctx).Model(&model.KnowledgeSearchLog{})
	if filter.ProductID != "" {
		q = q.Where("product_id = ?", filter.ProductID)
	}
	if filter.StartTime != nil {
		q = q.Where("created_at >= ?", filter.StartTime)
	}
	if filter.EndTime != nil {
		q = q.Where("created_at <= ?", filter.EndTime)
	}
	if filter.Hit != nil {
		q = q.Where("hit = ?", *filter.Hit)
	}
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var logs []model.KnowledgeSearchLog
	offset := (filter.Page - 1) * filter.PageSize
	if err := q.Order("created_at DESC").Offset(offset).Limit(filter.PageSize).Find(&logs).Error; err != nil {
		return nil, 0, err
	}
	return logs, total, nil
}

// QualityStats 检索质量统计
type QualityStats struct {
	TotalSearches int     `json:"total_searches"`
	HitCount      int     `json:"hit_count"`
	NoResultCount int     `json:"no_result_count"`
	HitRate       float64 `json:"hit_rate"`
	AvgScore      float64 `json:"avg_score"`
	MaxScore      float64 `json:"max_score"`
	MinScore      float64 `json:"min_score"`
	AvgLatencyMs  float64 `json:"avg_latency_ms"`
}

// GetQualityStats 获取检索质量统计
func (r *KnowledgeSearchLogRepository) GetQualityStats(ctx context.Context, productID string, start, end time.Time) (*QualityStats, error) {
	type Result struct {
		Total      int
		Hit        int
		AvgScore   float64
		MaxScore   float64
		MinScore   float64
		AvgLatency float64
	}
	var result Result
	q := r.db.WithContext(ctx).Model(&model.KnowledgeSearchLog{}).
		Select("COUNT(*) as total, SUM(CASE WHEN hit = 1 THEN 1 ELSE 0 END) as hit, AVG(avg_score) as avg_score, MAX(max_score) as max_score, MIN(min_score) as min_score, AVG(latency_ms) as avg_latency").
		Where("created_at BETWEEN ? AND ?", start, end)
	if productID != "" {
		q = q.Where("product_id = ?", productID)
	}
	if err := q.Scan(&result).Error; err != nil {
		return nil, err
	}
	stats := &QualityStats{
		TotalSearches: result.Total,
		HitCount:      result.Hit,
		NoResultCount: result.Total - result.Hit,
		AvgScore:      result.AvgScore,
		MaxScore:      result.MaxScore,
		MinScore:      result.MinScore,
		AvgLatencyMs:  result.AvgLatency,
	}
	if result.Total > 0 {
		stats.HitRate = float64(result.Hit) / float64(result.Total)
	}
	return stats, nil
}

// HotQuery 热点查询
type HotQuery struct {
	Query    string  `json:"query"`
	Count    int     `json:"count"`
	AvgScore float64 `json:"avg_score"`
	HitRate  float64 `json:"hit_rate"`
}

// GetHotQueries 获取热点查询
func (r *KnowledgeSearchLogRepository) GetHotQueries(ctx context.Context, productID string, days, limit int) ([]HotQuery, error) {
	if days <= 0 {
		days = 7
	}
	if limit <= 0 {
		limit = 20
	}
	start := time.Now().AddDate(0, 0, -days+1)
	type Result struct {
		QueryHash string
		Query     string
		Count     int
		AvgScore  float64
		Hit       int
	}
	var results []Result
	q := r.db.WithContext(ctx).Model(&model.KnowledgeSearchLog{}).
		Select("query_hash, MAX(query) as query, COUNT(*) as count, AVG(avg_score) as avg_score, SUM(CASE WHEN hit=1 THEN 1 ELSE 0 END) as hit").
		Where("created_at >= ? AND query IS NOT NULL AND query_hash IS NOT NULL", start).
		Group("query_hash").
		Order("count DESC").
		Limit(limit)
	if productID != "" {
		q = q.Where("product_id = ?", productID)
	}
	if err := q.Scan(&results).Error; err != nil {
		return nil, err
	}
	queries := make([]HotQuery, 0, len(results))
	for _, r := range results {
		hq := HotQuery{
			Query:    r.Query,
			Count:    r.Count,
			AvgScore: r.AvgScore,
		}
		if r.Count > 0 {
			hq.HitRate = float64(r.Hit) / float64(r.Count)
		}
		queries = append(queries, hq)
	}
	return queries, nil
}

// ScoreBucket 分数区间分布
type ScoreBucket struct {
	Range string `json:"range"`
	Count int    `json:"count"`
}

// GetScoreHistogram 获取分数分布直方图
func (r *KnowledgeSearchLogRepository) GetScoreHistogram(ctx context.Context, productID string, days int) ([]ScoreBucket, error) {
	if days <= 0 {
		days = 7
	}
	start := time.Now().AddDate(0, 0, -days+1)
	buckets := []struct {
		Label string
		Min   float64
		Max   float64
	}{
		{"0.0-0.2", 0, 0.2},
		{"0.2-0.4", 0.2, 0.4},
		{"0.4-0.6", 0.4, 0.6},
		{"0.6-0.8", 0.6, 0.8},
		{"0.8-1.0", 0.8, 1.0},
	}
	histogram := make([]ScoreBucket, 0, len(buckets))
	for _, b := range buckets {
		var count int64
		q := r.db.WithContext(ctx).Model(&model.KnowledgeSearchLog{}).
			Where("created_at >= ? AND max_score >= ? AND max_score < ?", start, b.Min, b.Max)
		if productID != "" {
			q = q.Where("product_id = ?", productID)
		}
		if err := q.Count(&count).Error; err != nil {
			return nil, err
		}
		histogram = append(histogram, ScoreBucket{Range: b.Label, Count: int(count)})
	}
	return histogram, nil
}

// TodayCount 今日检索次数
func (r *KnowledgeSearchLogRepository) TodayCount(ctx context.Context) (int64, error) {
	var count int64
	today := timeutil.BusinessToday()
	q := r.db.WithContext(ctx).Model(&model.KnowledgeSearchLog{}).Where("created_at >= ?", today)
	if err := q.Count(&count).Error; err != nil {
		return 0, err
	}
	return count, nil
}

// SearchTrend 检索趋势
func (r *KnowledgeSearchLogRepository) SearchTrend(ctx context.Context, productID string, days int) ([]DailyTrendItem, error) {
	if days <= 0 {
		days = 30
	}
	start := timeutil.BusinessDate(time.Now().AddDate(0, 0, -days+1))

	type Result struct {
		Day   time.Time
		Count int
	}
	var results []Result
	q := r.db.WithContext(ctx).Model(&model.KnowledgeSearchLog{}).
		Select("DATE(created_at) as day, COUNT(*) as count").
		Where("created_at >= ?", start).
		Group("DATE(created_at)")
	if productID != "" {
		q = q.Where("product_id = ?", productID)
	}
	if err := q.Scan(&results).Error; err != nil {
		return nil, err
	}

	trendMap := make(map[string]int)
	for _, r := range results {
		trendMap[timeutil.BusinessDate(r.Day)] = r.Count
	}
	trend := make([]DailyTrendItem, 0, days)
	for i := days - 1; i >= 0; i-- {
		day := timeutil.BusinessDate(time.Now().AddDate(0, 0, -i))
		trend = append(trend, DailyTrendItem{Day: day, Count: trendMap[day]})
	}
	return trend, nil
}
