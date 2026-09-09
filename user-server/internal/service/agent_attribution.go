// Package service - Agent 绩效归因（人工+AI混合，G5）
//
// 统计 ai_agents 的 performance：
//   - 自动解决率（AI 处理会话数/总会话数）
//   - 人工接手率
//   - 平均解决时间
//   - CSAT
//
// 数据源：customer_sessions（agent_id + handler_type=ai|human） + csat_surveys（score）
package service

import (
	"context"
	"fmt"
	"time"

	"hivemtk-user/internal/repository"
)

// AgentAttributionService Agent 绩效归因服务
type AgentAttributionService struct {
	repo *repository.AgentAttributionRepository
}

// NewAgentAttributionService 创建服务实例
func NewAgentAttributionService() *AgentAttributionService {
	return &AgentAttributionService{
		repo: repository.NewAgentAttributionRepository(),
	}
}

// AgentPerformance 单个 Agent 的绩效指标
type AgentPerformance struct {
	AgentID            uint      `json:"agent_id"`
	AgentName          string    `json:"agent_name"`
	TotalSessions      int64     `json:"total_sessions"`
	AIResolvedCount    int64     `json:"ai_resolved_count"`
	HumanTakeoverCount int64     `json:"human_takeover_count"`
	AutoResolveRate    float64   `json:"auto_resolve_rate"`
	HumanTakeoverRate  float64   `json:"human_takeover_rate"`
	AvgResolveSeconds  float64   `json:"avg_resolve_seconds"`
	AvgCSAT            float64   `json:"avg_csat"`
	CSATRespondedCount int64     `json:"csat_responded_count"`
	PeriodStart        time.Time `json:"period_start"`
	PeriodEnd          time.Time `json:"period_end"`
}

// PerformanceQuery 查询参数
type PerformanceQuery struct {
	AgentID    uint       `form:"agent_id"`
	PeriodDays int        `form:"period_days"`
	StartTime  *time.Time `form:"start_time"`
	EndTime    *time.Time `form:"end_time"`
}

// GetPerformance 获取指定时间窗口内的 Agent 绩效
func (s *AgentAttributionService) GetPerformance(ctx context.Context, q *PerformanceQuery) ([]*AgentPerformance, error) {
	if q == nil {
		q = &PerformanceQuery{PeriodDays: 7}
	}
	if q.PeriodDays <= 0 {
		q.PeriodDays = 7
	}

	endTime := time.Now()
	startTime := endTime.AddDate(0, 0, -q.PeriodDays)
	if q.StartTime != nil {
		startTime = *q.StartTime
	}
	if q.EndTime != nil {
		endTime = *q.EndTime
	}

	rows, err := s.repo.AggregateSessionsByAgent(ctx, q.AgentID, startTime, endTime)
	if err != nil {
		return nil, fmt.Errorf("ATTRIB_001: 会话聚合查询失败: %w", err)
	}

	csatRows, err := s.repo.AggregateCSATByAgent(ctx, startTime, endTime)
	if err != nil {
		// CSAT 聚合失败不阻断绩效主指标，降级为无 CSAT 数据
		csatRows = nil
	}

	csatMap := make(map[uint]repository.CSATAggRow, len(csatRows))
	for _, r := range csatRows {
		csatMap[r.AgentID] = r
	}

	result := make([]*AgentPerformance, 0, len(rows))
	for _, row := range rows {
		perf := &AgentPerformance{
			AgentID:            row.AgentID,
			AgentName:          row.AgentName,
			TotalSessions:      row.Total,
			AIResolvedCount:    row.AIResolved,
			HumanTakeoverCount: row.HumanTakeover,
			AvgResolveSeconds:  row.AvgResolveSec,
			PeriodStart:        startTime,
			PeriodEnd:          endTime,
		}
		if row.Total > 0 {
			perf.AutoResolveRate = float64(row.AIResolved) / float64(row.Total)
			perf.HumanTakeoverRate = float64(row.HumanTakeover) / float64(row.Total)
		}
		if csat, ok := csatMap[row.AgentID]; ok {
			perf.AvgCSAT = csat.AvgScore
			perf.CSATRespondedCount = csat.Responded
		}
		result = append(result, perf)
	}
	return result, nil
}
