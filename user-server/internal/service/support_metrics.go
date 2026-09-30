package service

import (
	"context"
	"time"

	"gorm.io/gorm"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/repository"
)

// SupportMetricsService 客服支持度量服务（运营后台「指标看板」）。
type SupportMetricsService interface {
	Available() bool
	Collect(ctx context.Context, days int) (*model.SupportMetrics, error)
}

type supportMetricsService struct {
	repo repository.SupportMetricsRepository
}

// NewSupportMetricsService 构造客服支持度量服务。
func NewSupportMetricsService(db *gorm.DB) SupportMetricsService {
	return &supportMetricsService{repo: repository.NewSupportMetricsRepository(db)}
}

// Available 数据层是否可用。
func (s *supportMetricsService) Available() bool {
	return s != nil && s.repo != nil && s.repo.Available()
}

// Collect 汇总最近 days 天的客服支持度量。
func (s *supportMetricsService) Collect(ctx context.Context, days int) (*model.SupportMetrics, error) {
	days = normalizeDays(days)
	end := time.Now()
	start := end.AddDate(0, 0, -days)

	row, err := s.repo.QuerySupportMetrics(ctx, start, end)
	if err != nil {
		return nil, err
	}

	return &model.SupportMetrics{
		WindowStart:         start,
		WindowEnd:           end,
		TotalSessions:       row.TotalSessions,
		HandoffSessions:     row.HandoffSessions,
		SelfServiceSessions: row.SelfServiceSessions,
		SelfServiceRate:     supportRatio(row.SelfServiceSessions, row.TotalSessions),
		HandoffRate:         supportRatio(row.HandoffSessions, row.TotalSessions),
		AvgTurns:            avgCount(row.MessageTotal, row.TotalSessions),
		RagRecallAccuracy:   row.RagAvgRecall,
		RagPrecision:        row.RagAvgPrecision,
		RagTotalQueries:     row.RagTotal,
	}, nil
}

// normalizeDays 窗口天数钳到 [1,365]，非法值回落到 30。
func normalizeDays(days int) int {
	if days <= 0 {
		return 30
	}
	if days > 365 {
		return 365
	}
	return days
}

// supportRatio 分子/分母的百分比，分母为 0 归 0。
// 注：包内 intent_metrics.go 已有同名 ratio(int)，此处加前缀避让（构建修复，无行为变更）。
func supportRatio(num, den int64) float64 {
	if den <= 0 {
		return 0
	}
	return float64(num) / float64(den) * 100
}

// avgCount 均值，样本为 0 归 0。
func avgCount(sum, count int64) float64 {
	if count <= 0 {
		return 0
	}
	return float64(sum) / float64(count)
}
