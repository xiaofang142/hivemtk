// llm_usage_report.go LLM 观测报表（T3）的数据访问仓储。
//
// 为什么单开一个仓储：报表服务原先自己握 *gorm.DB 做窗口聚合与四张报表查询，
// 那是五层架构 §3.2 明令阻断的形态（Service 直访 db）。这里收口的都是"数据访问细节"：
//   - 聚合口径：分组列序、upsert 冲突目标（必须与迁移建的唯一索引逐列一致，42P10 教训）、
//     advisory lock 的持有与释放；
//   - 读侧口径：分位计算（P50/P95 只在明细侧精确）、当日实时兜底、错误类聚合 + 失败明细分页。
//
// service 侧从此只剩编排与日志：它拿到的是行结构体，不是查询链。
// 设计文档：docs/architecture/design/T3-observability-reports.md
package repository

import (
	"context"
	"fmt"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"hivemtk-user/internal/model"
	_db "hivemtk-user/internal/pkg/db"
)

// LLMUsageReportRepository 报表聚合与查询仓储
type LLMUsageReportRepository struct {
	db *gorm.DB
}

// NewLLMUsageReportRepository 用显式句柄构造（装配点在 router / app）
func NewLLMUsageReportRepository(db *gorm.DB) *LLMUsageReportRepository {
	return &LLMUsageReportRepository{db: db}
}

// LLMUsageReportFilter 读侧维度过滤（空串 = 不过滤）
type LLMUsageReportFilter struct {
	Model    string
	Scenario string
	Channel  string
	Source   string
}

// LLMUsageReportTotalsRow 总览 KPI 的聚合行（成本口径：llm_usage_daily.total_cost_cents）
type LLMUsageReportTotalsRow struct {
	CallCount        int64 `gorm:"column:call_count"`
	ErrorCount       int64 `gorm:"column:error_count"`
	CacheHitCount    int64 `gorm:"column:cache_hit_count"`
	PromptTokens     int64 `gorm:"column:prompt_tokens"`
	CompletionTokens int64 `gorm:"column:completion_tokens"`
	TotalCostCents   int64 `gorm:"column:total_cost_cents"`
	LatencySumMs     int64 `gorm:"column:latency_sum_ms"`
}

// LLMUsageReportTimeseriesRow 按天序列行
type LLMUsageReportTimeseriesRow struct {
	StatDate         string `gorm:"column:stat_date"`
	CallCount        int64  `gorm:"column:call_count"`
	ErrorCount       int64  `gorm:"column:error_count"`
	PromptTokens     int64  `gorm:"column:prompt_tokens"`
	CompletionTokens int64  `gorm:"column:completion_tokens"`
	TotalCostCents   int64  `gorm:"column:total_cost_cents"`
	AvgLatencyMs     int64  `gorm:"column:avg_latency_ms"`
}

// LLMUsageReportBreakdownRow 单维度分解行（错误类聚合复用同一形状）
type LLMUsageReportBreakdownRow struct {
	Dim              string `gorm:"column:dim"`
	CallCount        int64  `gorm:"column:call_count"`
	ErrorCount       int64  `gorm:"column:error_count"`
	CompletionTokens int64  `gorm:"column:completion_tokens"`
	TotalCostCents   int64  `gorm:"column:total_cost_cents"`
	P95LatencyMs     int64  `gorm:"column:p95_latency_ms"`
}

// LLMUsageReportErrorRow 失败明细行
type LLMUsageReportErrorRow struct {
	TraceID    string    `gorm:"column:trace_id"`
	Scenario   string    `gorm:"column:scenario"`
	Provider   string    `gorm:"column:provider"`
	Model      string    `gorm:"column:model"`
	ErrorClass string    `gorm:"column:error_class"`
	ErrorMsg   string    `gorm:"column:error_msg"`
	LatencyMs  int64     `gorm:"column:latency_ms"`
	CreatedAt  time.Time `gorm:"column:created_at"`
}

// ============================================================
// 聚合任务
// ============================================================

// AggregateWindow 不加锁的窗口聚合（幂等：同一窗口重跑结果一致），返回桶数。
// 测试直接调用；生产路径走 AggregateWindowLocked。
func (r *LLMUsageReportRepository) AggregateWindow(ctx context.Context, from, to time.Time) (int, error) {
	if r.db == nil {
		return 0, fmt.Errorf("llm usage report repository: db handle is nil")
	}
	rows := []struct {
		TenantID         uint    `gorm:"column:tenant_id"`
		StatDate         string  `gorm:"column:stat_date"`
		Provider         string  `gorm:"column:provider"`
		Model            string  `gorm:"column:model"`
		Scenario         string  `gorm:"column:scenario"`
		Channel          string  `gorm:"column:channel"`
		Source           string  `gorm:"column:source"`
		ErrorClass       string  `gorm:"column:error_class"`
		CallCount        int64   `gorm:"column:call_count"`
		ErrorCount       int64   `gorm:"column:error_count"`
		CacheHitCount    int64   `gorm:"column:cache_hit_count"`
		PromptTokens     int64   `gorm:"column:prompt_tokens"`
		CompletionTokens int64   `gorm:"column:completion_tokens"`
		CostCents        float64 `gorm:"column:cost_cents"`
		LatencySumMs     int64   `gorm:"column:latency_sum_ms"`
		SumSquaresLat    int64   `gorm:"column:sum_squares_latency_ms"`
	}{}
	// 明细 → 维度桶聚合。列名分组；tenant_id 取 routing_logs 自身列（迁移 v3.6.0 起有此列）。
	// Group("1") 在 PG 上是坏的（按常量分组），必须写真实列名。
	err := r.db.WithContext(ctx).Table("llm_routing_logs").
		Select(`
			tenant_id,
			to_char(created_at, 'YYYY-MM-DD') AS stat_date,
			provider,
			model,
			scenario,
			COALESCE(channel, '') AS channel,
			COALESCE(source, '') AS source,
			`+errorClassExpr+` AS error_class,
			COUNT(*) AS call_count,
			SUM(CASE WHEN success THEN 0 ELSE 1 END) AS error_count,
			SUM(CASE WHEN from_cache THEN 1 ELSE 0 END) AS cache_hit_count,
			SUM(prompt_tokens) AS prompt_tokens,
			SUM(completion_tokens) AS completion_tokens,
			SUM(CASE WHEN total_tokens > 0 THEN cost ELSE 0 END) * 100 AS cost_cents,
			SUM(latency_ms) AS latency_sum_ms,
			SUM(latency_ms * latency_ms) AS sum_squares_latency_ms`).
		Where("created_at >= ? AND created_at < ?", from, to).
		Group(`tenant_id, stat_date, provider, model, scenario, channel, source, ` + errorClassExpr).
		Scan(&rows).Error
	if err != nil {
		return 0, fmt.Errorf("aggregate routing_logs: %w", err)
	}

	for _, r0 := range rows {
		costCents := int64(r0.CostCents + 0.5)
		avg := int64(0)
		if r0.CallCount > 0 {
			avg = r0.LatencySumMs / r0.CallCount
		}
		// 覆盖式 upsert：冲突目标 = 迁移建的 uk_llm_usage_daily_dims 复合唯一索引
		//（列序严格一致——42P10 教训）。同一窗口重跑时整行覆盖，保证幂等。
		// 用 clause.OnConflict 表达（GORM 链式 API，禁止 .Exec 直连）。
		row := model.LLMUsageDaily{
			TenantID:         r0.TenantID,
			StatDate:         r0.StatDate,
			Provider:         r0.Provider,
			Model:            r0.Model,
			Scenario:         r0.Scenario,
			Channel:          r0.Channel,
			Source:           r0.Source,
			ErrorClass:       r0.ErrorClass,
			CallCount:        r0.CallCount,
			ErrorCount:       r0.ErrorCount,
			CacheHitCount:    r0.CacheHitCount,
			PromptTokens:     r0.PromptTokens,
			CompletionTokens: r0.CompletionTokens,
			TotalCostCents:   costCents,
			LatencySumMs:     r0.LatencySumMs,
			P50LatencyMs:     avg,
			P95LatencyMs:     avg,
			SumSquaresLat:    r0.SumSquaresLat,
			UpdatedAt:        time.Now(),
		}
		if err := r.db.WithContext(ctx).
			Clauses(clause.OnConflict{
				Columns: []clause.Column{
					{Name: "tenant_id"}, {Name: "stat_date"}, {Name: "provider"},
					{Name: "model"}, {Name: "scenario"}, {Name: "channel"},
					{Name: "source"}, {Name: "error_class"},
				},
				DoUpdates: clause.AssignmentColumns([]string{
					"call_count", "error_count", "cache_hit_count", "prompt_tokens",
					"completion_tokens", "total_cost_cents", "latency_sum_ms",
					"p50_latency_ms", "p95_latency_ms", "sum_squares_latency_ms", "updated_at",
				}),
			}).
			Create(&row).Error; err != nil {
			return 0, fmt.Errorf("upsert llm_usage_daily: %w", err)
		}
	}
	return len(rows), nil
}

// AggregateWindowLocked 在会话级 advisory lock 保护下跑一次窗口聚合。
//
// 返回 locked=false 表示锁被别的实例持有（本轮跳过，不是错误）。
//
// ⚠️ 2026-11-10 修：原实现用 `TryAdvisoryLock(ctx, r.db, ...)` + defer
// `ReleaseAdvisoryLock(..., r.db, ...)`，两次都从**连接池取任意连接**。会话级
// advisory lock 的解锁落在别的会话上时返回 false 且**不报错** ⇒ 锁永久滞留在
// 加锁那条连接上，此后每次聚合都读成"别人在跑"并静默跳过（生产实证：锁键
// 830083 从 12:16 起一直卡在同一后端会话）。
// 现在用 TryAdvisoryLockOnConn 拿到**扣住独占连接的句柄**，解锁走同一连接。
//
// 锁的释放不依赖调用方 ctx（其可能已随请求结束被取消）：用短超时 Background，
// 保证锁一定被还回去。
func (r *LLMUsageReportRepository) AggregateWindowLocked(ctx context.Context, key int64, from, to time.Time) (locked bool, buckets int, err error) {
	if r == nil || r.db == nil {
		return false, 0, fmt.Errorf("llm usage report repository: db handle is nil")
	}
	handle, locked, err := _db.TryAdvisoryLockOnConn(ctx, r.db, key)
	if err != nil {
		return false, 0, fmt.Errorf("advisory lock: %w", err)
	}
	if !locked {
		return false, 0, nil
	}
	defer func() {
		rctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if rerr := handle.Release(rctx); rerr != nil {
			// 解锁失败 = 锁可能滞留到连接断开为止：不能吞，否则下一轮又静默跳过。
			if err == nil {
				err = rerr
			}
		}
	}()
	buckets, err = r.AggregateWindow(ctx, from, to)
	return true, buckets, err
}

// ============================================================
// 查询侧（报表 API）
// ============================================================

// Totals 总览 KPI（llm_usage_daily 天粒度求和）
func (r *LLMUsageReportRepository) Totals(ctx context.Context, from, to time.Time, f LLMUsageReportFilter) (LLMUsageReportTotalsRow, error) {
	var row LLMUsageReportTotalsRow
	if r.db == nil {
		return row, fmt.Errorf("llm usage report repository: db handle is nil")
	}
	q := r.db.WithContext(ctx).Table("llm_usage_daily").
		Select(`COALESCE(SUM(call_count),0) AS call_count,
			COALESCE(SUM(error_count),0) AS error_count,
			COALESCE(SUM(cache_hit_count),0) AS cache_hit_count,
			COALESCE(SUM(prompt_tokens),0) AS prompt_tokens,
			COALESCE(SUM(completion_tokens),0) AS completion_tokens,
			COALESCE(SUM(total_cost_cents),0) AS total_cost_cents,
			COALESCE(SUM(latency_sum_ms),0) AS latency_sum_ms`).
		Where("stat_date >= ? AND stat_date < ?", from.Format("2006-01-02"), to.Format("2006-01-02"))
	q = applyReportChannelSource(q, f.Channel, f.Source)
	if err := q.Scan(&row).Error; err != nil {
		return row, err
	}
	return row, nil
}

// errorClassExpr 把失败调用归一成可读的错误分类。
// 背景：llm_routing_logs.error_class 历史上没有写入方，失败行清一色落 unknown，
// 报表「错误类型分布」只有 unknown 一片、明细「错误类型」列对排障没信息量；
// 而 error_msg 里带着 status=401 / invalid_api_key / Invalid Authentication 等可分类特征，
// 因此在读取侧用文本回推分类。success 行恒为空串，与 llm_usage_daily 存量口径一致。
// 写入侧已由 dispatcher_observability.classifyErrorClass 标注（F-6-5②，2026-11-12）：
// 新行自带分类走第二分支（error_class 非空直取）；本表达式保留为历史行
// （error_class 空值）的回推口径，关键词表与写入侧单点对齐，改任一侧必须同改。
const errorClassExpr = "CASE WHEN success THEN '' " +
	"WHEN NULLIF(error_class, '') IS NOT NULL THEN error_class " +
	"WHEN COALESCE(error_msg, '') ~* '(invalid_api_key|invalid authentication|unauthorized|status=401|incorrect api key)' THEN 'auth' " +
	"WHEN COALESCE(error_msg, '') ~* '(status=429|rate limit|quota|insufficient_quota)' THEN 'rate_limit' " +
	"WHEN COALESCE(error_msg, '') ~* '(timeout|timed out|deadline exceeded)' THEN 'timeout' " +
	"WHEN COALESCE(error_msg, '') ~* '(status=5[0-9][0-9]|connection refused|no such host|bad gateway|service unavailable)' THEN 'upstream' " +
	"ELSE 'unknown' END"

// LatencyPercentiles 明细侧实时分位（聚合表存的是 AVG，P95 精确值只在明细侧）
func (r *LLMUsageReportRepository) LatencyPercentiles(ctx context.Context, from, to time.Time) (p50, p95 int64, err error) {
	if r.db == nil {
		return 0, 0, fmt.Errorf("llm usage report repository: db handle is nil")
	}
	var lat struct {
		P50 int64 `gorm:"column:p50"`
		P95 int64 `gorm:"column:p95"`
	}
	err = r.db.WithContext(ctx).Table("llm_routing_logs").
		Select(`COALESCE(percentile_cont(0.5) WITHIN GROUP (ORDER BY latency_ms),0) AS p50,
			COALESCE(percentile_cont(0.95) WITHIN GROUP (ORDER BY latency_ms),0) AS p95`).
		Where("created_at >= ? AND created_at < ?", from, to).
		Scan(&lat).Error
	if err != nil {
		return 0, 0, err
	}
	return lat.P50, lat.P95, nil
}

// Timeseries 按天序列（列名分组，禁 Group("1")）
func (r *LLMUsageReportRepository) Timeseries(ctx context.Context, from, to time.Time, f LLMUsageReportFilter) ([]LLMUsageReportTimeseriesRow, error) {
	if r.db == nil {
		return nil, fmt.Errorf("llm usage report repository: db handle is nil")
	}
	rows := []LLMUsageReportTimeseriesRow{}
	q := r.db.WithContext(ctx).Table("llm_usage_daily").
		Select(`stat_date,
			SUM(call_count) AS call_count,
			SUM(error_count) AS error_count,
			SUM(prompt_tokens) AS prompt_tokens,
			SUM(completion_tokens) AS completion_tokens,
			SUM(total_cost_cents) AS total_cost_cents,
			-- 整数列扫描：numeric 除法会产生 "0.533..." 字符串，直接 Scan 到 int64 会
			-- 500（Scan error converting string to int64）。显式取整并转 bigint。
			CASE WHEN SUM(call_count) > 0
				THEN ROUND(SUM(latency_sum_ms)::numeric / SUM(call_count))::bigint
				ELSE 0 END AS avg_latency_ms`).
		Where("stat_date >= ? AND stat_date < ?", from.Format("2006-01-02"), to.Format("2006-01-02")).
		Group("stat_date").Order("stat_date")
	q = applyReportDims(q, f.Model, f.Scenario)
	q = applyReportChannelSource(q, f.Channel, f.Source)
	if err := q.Scan(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

// Breakdown 单维度分解。column 必须是白名单列（by 参数的校验留在 service 侧），
// 本仓储不接收用户原始输入。
func (r *LLMUsageReportRepository) Breakdown(ctx context.Context, column string, from, to time.Time, f LLMUsageReportFilter) ([]LLMUsageReportBreakdownRow, error) {
	if r.db == nil {
		return nil, fmt.Errorf("llm usage report repository: db handle is nil")
	}
	rows := []LLMUsageReportBreakdownRow{}
	q := r.db.WithContext(ctx).Table("llm_usage_daily").
		Select(column+` AS dim,
			SUM(call_count) AS call_count,
			SUM(error_count) AS error_count,
			SUM(completion_tokens) AS completion_tokens,
			SUM(total_cost_cents) AS total_cost_cents,
			COALESCE(MAX(p95_latency_ms),0) AS p95_latency_ms`).
		Where("stat_date >= ? AND stat_date < ?", from.Format("2006-01-02"), to.Format("2006-01-02")).
		Group(column).Order("call_count DESC")
	q = applyReportDims(q, f.Model, f.Scenario)
	q = applyReportChannelSource(q, f.Channel, f.Source)
	if err := q.Scan(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

// ErrorClassAgg 错误类聚合（错误类维度，复用 breakdown 行形状）
func (r *LLMUsageReportRepository) ErrorClassAgg(ctx context.Context, from, to time.Time) ([]LLMUsageReportBreakdownRow, error) {
	if r.db == nil {
		return nil, fmt.Errorf("llm usage report repository: db handle is nil")
	}
	rows := []LLMUsageReportBreakdownRow{}
	// 直接按明细表实时归类：llm_usage_daily 里历史行的 error_class 全是 unknown
	//（写入侧从未标注），只有明细表带 error_msg 能回推分类，聚合口径必须与「错误明细」一致。
	if err := r.db.WithContext(ctx).Table("llm_routing_logs").
		Select(errorClassExpr+" AS dim, COUNT(*) AS call_count").
		Where("success = false AND created_at >= ? AND created_at < ?", from, to).
		Group(errorClassExpr).
		Order("call_count DESC").
		Scan(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

// FailedCalls 失败明细分页（明细表直查，量小）。返回 total 与当页行。
func (r *LLMUsageReportRepository) FailedCalls(ctx context.Context, from, to time.Time, errorClass string, page, pageSize int) (int64, []LLMUsageReportErrorRow, error) {
	if r.db == nil {
		return 0, nil, fmt.Errorf("llm usage report repository: db handle is nil")
	}
	dq := r.db.WithContext(ctx).Table("llm_routing_logs").
		Select(`trace_id, scenario, provider, model,
			`+errorClassExpr+` AS error_class,
			LEFT(COALESCE(error_msg,''),500) AS error_msg,
			latency_ms, created_at`).
		Where("success = false AND created_at >= ? AND created_at < ?", from, to)
	if errorClass != "" {
		// 过滤走同一表达式，否则「按分类筛选」与展示的分类对不上
		dq = dq.Where(errorClassExpr+" = ?", errorClass)
	}
	var total int64
	if err := dq.Count(&total).Error; err != nil {
		return 0, nil, err
	}
	rows := []LLMUsageReportErrorRow{}
	if err := dq.Order("created_at DESC").
		Offset((page - 1) * pageSize).Limit(pageSize).Scan(&rows).Error; err != nil {
		return 0, nil, err
	}
	return total, rows, nil
}

// applyReportDims 维度过滤（model/scenario）
func applyReportDims(q *gorm.DB, model_, scenario string) *gorm.DB {
	if model_ != "" {
		q = q.Where("model = ?", model_)
	}
	if scenario != "" {
		q = q.Where("scenario = ?", scenario)
	}
	return q
}

// applyReportChannelSource 渠道/计费来源过滤
func applyReportChannelSource(q *gorm.DB, channel, source string) *gorm.DB {
	if channel != "" {
		q = q.Where("channel = ?", channel)
	}
	if source != "" {
		q = q.Where("source = ?", source)
	}
	return q
}
