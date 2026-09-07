package service

import (
	"context"
	"time"

	"hivemtk-user/internal/pkg/db"

	"gorm.io/gorm"
)

// SalesCockpitService 驾驶舱聚合服务
type SalesCockpitService struct {
	db *gorm.DB
}

// NewSalesCockpitService 构造
func NewSalesCockpitService() *SalesCockpitService {
	return &SalesCockpitService{db: db.GetDB()}
}

// GetCockpit 全景聚合（单次请求 5 条聚合 SQL，均带 LIMIT/索引时间过滤）
// 返回字段与前端 Index.vue 模板期望结构严格对齐：
//   react.totalRuns / sop.executions / rag.queries / reach.sentToday
//   llmRoutes / channelHealth / intentDistribution / topTools
func (s *SalesCockpitService) GetCockpit(ctx context.Context) (map[string]any, error) {
	today := time.Now().Format("2006-01-02")
	weekAgo := time.Now().AddDate(0, 0, -7)

	reactRuns := s.countWhere(ctx, "llm_routing_logs", "created_at >= ?", today)
	sopRunning := s.countWhere(ctx, "sop_executions", "created_at >= ? AND (state IN ('running','pending','completed') OR status IN ('running','pending','completed'))", today)
	ragQueries := s.countWhere(ctx, "rag_query_logs", "created_at >= ?", today)
	reachSent := s.countWhere(ctx, "reach_jobs", "created_at >= ? AND state IS NOT NULL", today)

	llmRoutes := s.groupQuery(ctx,
		`SELECT scenario, provider,
		        COUNT(*) AS calls,
		        COALESCE(AVG(latency_ms),0) AS avg_latency,
		        COALESCE(SUM(cost),0)       AS total_cost
		 FROM llm_routing_logs
		 WHERE created_at >= ?
		 GROUP BY scenario, provider
		 ORDER BY calls DESC
		 LIMIT 10`, today)

	channelHealth := s.groupQuery(ctx,
		`SELECT platform, status, COUNT(*) AS cnt
		 FROM platform_accounts
		 GROUP BY platform, status
		 ORDER BY cnt DESC
		 LIMIT 30`)

	intentDist := s.groupQuery(ctx,
		`SELECT intent_type AS intent, COUNT(*) AS cnt
		 FROM intent_logs
		 WHERE created_at >= ?
		 GROUP BY intent_type
		 ORDER BY cnt DESC
		 LIMIT 12`, weekAgo)

	// topTools 原表 tool_audit_logs 不存在，降级用 message_trace 按 node 分组统计调用次数
	topTools := s.groupQuery(ctx,
		`SELECT node AS tool_name, COUNT(*) AS calls
		 FROM message_trace
		 WHERE created_at >= ?
		 GROUP BY node
		 ORDER BY calls DESC
		 LIMIT 10`, weekAgo)

	return map[string]any{
		"react":              map[string]any{"totalRuns": reactRuns},
		"sop":                map[string]any{"executions": sopRunning},
		"rag":                map[string]any{"queries": ragQueries},
		"reach":              map[string]any{"sentToday": reachSent},
		"llmRoutes":          llmRoutes,
		"channelHealth":      channelHealth,
		"intentDistribution": intentDist,
		"topTools":           topTools,
	}, nil
}

func (s *SalesCockpitService) countWhere(ctx context.Context, table, cond string, args ...any) int64 {
	if s.db == nil {
		return 0
	}
	var n int64
	if err := s.db.WithContext(ctx).Table(table).Where(cond, args...).Count(&n).Error; err != nil {
		return 0
	}
	return n
}

func (s *SalesCockpitService) groupQuery(ctx context.Context, sql string, args ...any) []map[string]any {
	if s.db == nil {
		return []map[string]any{}
	}
	out := []map[string]any{}
	if err := s.db.WithContext(ctx).Raw(sql, args...).Scan(&out).Error; err != nil {
		return []map[string]any{}
	}
	return out
}
