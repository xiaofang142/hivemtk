package service

import (
	"context"
	"time"

	"hivemtk-user/internal/pkg/timeutil"
	"hivemtk-user/internal/repository"
)

// SalesCockpitService 驾驶舱聚合服务
type SalesCockpitService struct {
	repo *repository.SalesCockpitRepository
}

// NewSalesCockpitService 构造
func NewSalesCockpitService() *SalesCockpitService {
	return &SalesCockpitService{repo: repository.NewSalesCockpitRepository()}
}

// GetCockpit 全景聚合（单次请求 5 条聚合 SQL，均带 LIMIT/索引时间过滤）
// 返回字段与前端 Index.vue 模板期望结构严格对齐：
//
//	react.totalRuns / sop.executions / rag.queries / reach.sentToday
//	llmRoutes / channelHealth / intentDistribution / topTools
func (s *SalesCockpitService) GetCockpit(ctx context.Context) (map[string]any, error) {
	// today 作为字符串下推给 `created_at >= ?`：PG 按会话时区（CST）解释它，
	// 所以串本身必须是业务日，否则 UTC 宿主的"今日"计数会多出 18~26 小时
	today := timeutil.BusinessToday()
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

	// OPT-DB-10：intent_logs 已并入 intent_records（source='fine_grained'）
	intentDist := s.groupQuery(ctx,
		`SELECT intent_type AS intent, COUNT(*) AS cnt
		 FROM intent_records
		 WHERE created_at >= ? AND source = 'fine_grained'
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
	return s.repo.CountWhere(ctx, table, cond, args...)
}

func (s *SalesCockpitService) groupQuery(ctx context.Context, sql string, args ...any) []map[string]any {
	return s.repo.GroupQuery(ctx, sql, args...)
}
