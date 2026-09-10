// ai_resolution_stats.go AI 采纳率统计仓储（五层 L5）
package repository

import (
	"context"
	"time"

	"gorm.io/gorm"
)

// AIResolutionStatsRepository ai_suggestions 统计仓储
type AIResolutionStatsRepository struct {
	db *gorm.DB
}

// NewAIResolutionStatsRepository 构造
func NewAIResolutionStatsRepository(db *gorm.DB) *AIResolutionStatsRepository {
	return &AIResolutionStatsRepository{db: db}
}

// CountSuggestions 统计时间窗口内建议总数与采纳数
func (r *AIResolutionStatsRepository) CountSuggestions(ctx context.Context, since time.Time) (total, adopted int64, err error) {
	if r.db == nil {
		return 0, 0, nil
	}
	if err = r.db.WithContext(ctx).Table("ai_suggestions").
		Where("created_at >= ?", since).
		Count(&total).Error; err != nil {
		return 0, 0, err
	}
	if err = r.db.WithContext(ctx).Table("ai_suggestions").
		Where("created_at >= ? AND adopted = ?", since, true).
		Count(&adopted).Error; err != nil {
		return total, 0, err
	}
	return total, adopted, nil
}
