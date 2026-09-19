package repository

import (
	"context"
	"hivemtk-user/internal/aiagent/knowledge/model"
	"hivemtk-user/internal/pkg/timeutil"
	"time"

	"gorm.io/gorm"
)

// KnowledgeImportLogRepository 知识库导入日志仓储
type KnowledgeImportLogRepository struct {
	db *gorm.DB
}

// NewKnowledgeImportLogRepository 创建导入日志仓储
func NewKnowledgeImportLogRepository(db *gorm.DB) *KnowledgeImportLogRepository {
	return &KnowledgeImportLogRepository{db: db}
}

// Create 创建日志
func (r *KnowledgeImportLogRepository) Create(ctx context.Context, log *model.KnowledgeImportLog) error {
	return r.db.WithContext(ctx).Create(log).Error
}

// ListFilter 导入日志筛选
type ImportLogListFilter struct {
	ProductID string
	BatchNo   string
	Page      int
	PageSize  int
}

// List 列出导入日志
func (r *KnowledgeImportLogRepository) List(ctx context.Context, filter ImportLogListFilter) ([]model.KnowledgeImportLog, int64, error) {
	if filter.Page < 1 {
		filter.Page = 1
	}
	if filter.PageSize < 1 {
		filter.PageSize = 20
	}
	q := r.db.WithContext(ctx).Model(&model.KnowledgeImportLog{})
	if filter.ProductID != "" {
		q = q.Where("product_id = ?", filter.ProductID)
	}
	if filter.BatchNo != "" {
		q = q.Where("batch_no = ?", filter.BatchNo)
	}
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var logs []model.KnowledgeImportLog
	offset := (filter.Page - 1) * filter.PageSize
	if err := q.Order("created_at DESC").Offset(offset).Limit(filter.PageSize).Find(&logs).Error; err != nil {
		return nil, 0, err
	}
	return logs, total, nil
}

// DailyTrendItem 每日趋势
type DailyTrendItem struct {
	Day    string `json:"day"`
	Count  int    `json:"count"`
	Failed int    `json:"failed"`
}

// DailyImportTrend 获取每日导入趋势
func (r *KnowledgeImportLogRepository) DailyImportTrend(ctx context.Context, productID string, days int) ([]DailyTrendItem, error) {
	if days <= 0 {
		days = 30
	}
	start := timeutil.BusinessDate(time.Now().AddDate(0, 0, -days+1))

	type Result struct {
		Day         time.Time
		TotalCount  int
		FailedCount int
	}

	var results []Result
	q := r.db.WithContext(ctx).Model(&model.KnowledgeImportLog{}).
		Select("DATE(created_at) as day, COUNT(*) as total_count, SUM(CASE WHEN status = 'failed' THEN 1 ELSE 0 END) as failed_count").
		Where("created_at >= ?", start).
		Group("DATE(created_at)")

	if productID != "" {
		q = q.Where("product_id = ?", productID)
	}

	if err := q.Scan(&results).Error; err != nil {
		return nil, err
	}

	trendMap := make(map[string]DailyTrendItem)
	for _, r := range results {
		day := timeutil.BusinessDate(r.Day)
		trendMap[day] = DailyTrendItem{
			Day:    day,
			Count:  r.TotalCount,
			Failed: r.FailedCount,
		}
	}
	trend := make([]DailyTrendItem, 0, days)
	for i := days - 1; i >= 0; i-- {
		day := timeutil.BusinessDate(time.Now().AddDate(0, 0, -i))
		if v, ok := trendMap[day]; ok {
			trend = append(trend, v)
		} else {
			trend = append(trend, DailyTrendItem{Day: day, Count: 0, Failed: 0})
		}
	}
	return trend, nil
}

// AvgImportDurationMs 平均导入耗时(毫秒)
//
// 使用 COALESCE 避免无记录时返回 NULL，productID=0 表示不按 product 过滤。
func (r *KnowledgeImportLogRepository) AvgImportDurationMs(ctx context.Context, productID string) (float64, error) {
	var avg float64
	q := r.db.WithContext(ctx).Model(&model.KnowledgeImportLog{}).
		Select("COALESCE(AVG(duration_ms), 0)")
	if productID != "" {
		q = q.Where("product_id = ?", productID)
	}
	if err := q.Scan(&avg).Error; err != nil {
		return 0, err
	}
	return avg, nil
}
