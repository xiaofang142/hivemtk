// ai_performance_repo.go AI 绩效聚合查询仓储（五层 L5）
package repository

import (
	"context"
	"time"

	"gorm.io/gorm"
)

// AIPerformanceRepository 自动化率漏斗聚合收口
type AIPerformanceRepository struct {
	db *gorm.DB
}

// NewAIPerformanceRepository 构造
func NewAIPerformanceRepository(db *gorm.DB) *AIPerformanceRepository {
	return &AIPerformanceRepository{db: db}
}

// CountSessionsSince 窗口内会话计数（cond 附加过滤，可为空串）
func (r *AIPerformanceRepository) CountSessionsSince(ctx context.Context, since time.Time, cond string) (int64, error) {
	if r.db == nil {
		return 0, nil
	}
	var n int64
	q := r.db.WithContext(ctx).Table("customer_sessions").Where("created_at >= ?", since)
	if cond != "" {
		q = q.Where(cond)
	}
	err := q.Count(&n).Error
	return n, err
}

// LLMRoutingScenarioRow 按场景聚合的 LLM 调用量与成本
type LLMRoutingScenarioRow struct {
	Scenario string  `gorm:"column:scenario"`
	Cnt      int64   `gorm:"column:cnt"`
	Cost     float64 `gorm:"column:cost"`
}

// SumLLMRoutingByScenario 窗口内按 scenario 聚合 llm_routing_logs
func (r *AIPerformanceRepository) SumLLMRoutingByScenario(ctx context.Context, since time.Time) ([]LLMRoutingScenarioRow, error) {
	if r.db == nil {
		return nil, nil
	}
	var lrs []LLMRoutingScenarioRow
	err := r.db.WithContext(ctx).Table("llm_routing_logs").
		Select("scenario, COUNT(*) AS cnt, COALESCE(SUM(cost),0) AS cost").
		Where("created_at >= ?", since).
		Group("scenario").Scan(&lrs).Error
	return lrs, err
}
