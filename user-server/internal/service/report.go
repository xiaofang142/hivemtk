// Package service 报表服务（T3 观测报表）。
//
// 设计文档：docs/architecture/design/T3-observability-reports.md
// 数据流：llm_routing_logs（明细）→ llm_usage_daily（日聚合）→ 报表 API；
// 平台模型成本以 platform_llm_usage_records（真分列计价）为准，
// llm_routing_logs 的 splitCost 假拆分口径只用于延迟/错误聚合。
package service

import (
	"context"
	"fmt"
	"strings"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/timeutil"
	"hivemtk-user/internal/pkg/utils/logger"
	"hivemtk-user/internal/repository"
)

// ============================================================
// 错误分类
// ============================================================

// ClassifyError 把错误文本归入固定错误类（关键字匹配，简单可测）。
// 返回值与 model.ErrClass* 常量一致；空串归 unknown（失败但无消息）。
func ClassifyError(msg string) string {
	low := strings.ToLower(msg)
	switch {
	case low == "":
		return model.ErrClassUnknown
	case containsAny(low, "context length", "context_length", "maximum context", "too many tokens", "token limit"):
		return model.ErrClassContextLength
	case containsAny(low, "rate limit", "rate_limit", "ratelimit", "429", "too many requests", "限流"):
		return model.ErrClassRateLimit
	case containsAny(low, "timeout", "timed out", "deadline exceeded", "context deadline", "连接超时", "超时"):
		return model.ErrClassTimeout
	case containsAny(low, "unauthorized", "401", "invalid api key", "authentication", "forbidden", "403", "鉴权"):
		return model.ErrClassAuth
	case containsAny(low, "余额不足", "insufficient balance", "insufficient_llm_balance"):
		return model.ErrClassInsuffBalance
	case containsAny(low, "500", "502", "503", "504", "internal server error", "bad gateway", "service unavailable", "gateway timeout"):
		return model.ErrClassUpstream5xx
	case containsAny(low, "empty response", "empty_response", "空响应", "no content", "响应为空"):
		return model.ErrClassEmptyResponse
	default:
		return model.ErrClassUnknown
	}
}

func containsAny(s string, subs ...string) bool {
	for _, sub := range subs {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}

// ============================================================
// 日聚合任务
// ============================================================

// DailyAggWindow 一次聚合的输入窗口
type DailyAggWindow struct {
	From time.Time // 含
	To   time.Time // 含（按 stat_date 落桶）
}

// ReportService 报表聚合与查询
//
// 数据访问全部在 repository.LLMUsageReportRepository 里：聚合列序、upsert 冲突目标
// （uk_llm_usage_daily_dims，42P10 教训）、advisory lock 的持有与释放、分位口径、
// 当日兜底明细。本服务只做编排、指标换算与日志——不握 DB 句柄（五层架构 §3.2）。
type ReportService struct {
	repo *repository.LLMUsageReportRepository
	// nowFn 时钟接缝：默认 time.Now；测试注入冻结时钟验证跨午夜/业务时区重算。
	nowFn func() time.Time
}

// NewReportService 构造（仓储由装配点注入，见 internal/router/report_routes.go）
func NewReportService(repo *repository.LLMUsageReportRepository) *ReportService {
	return &ReportService{repo: repo, nowFn: time.Now}
}

func (s *ReportService) now() time.Time {
	if s.nowFn != nil {
		return s.nowFn()
	}
	return time.Now()
}

// advisoryLockKey 聚合任务互斥锁键（本仓多实例防并发）
const advisoryLockKey = 830083

// AggregateDaily 把 (from, to] 窗口内的 llm_routing_logs 明细聚合 upsert 进
// llm_usage_daily。幂等：同一窗口重跑结果一致（ON CONFLICT DO UPDATE 覆盖式写入）。
//
// 加锁、释放与 upsert 口径都在仓储的 AggregateWindowLocked 里（锁载体与判读见
// internal/pkg/db/advisory_lock.go）；这里只判读"本轮有没有拿到锁"并记一条聚合结果日志。
func (s *ReportService) AggregateDaily(ctx context.Context, from, to time.Time, resetStatDate string) error {
	locked, rows, err := s.repo.AggregateWindowLocked(ctx, advisoryLockKey, from, to, resetStatDate)
	if err != nil {
		return err
	}
	if !locked {
		return nil // 其他实例正在聚合，本轮跳过（下轮重算会覆盖）
	}
	logger.Infof("[Report] aggregated %d buckets into llm_usage_daily (%s ~ %s, reset=%q)", rows,
		from.Format("2006-01-02 15:04"), to.Format("2006-01-02 15:04"), resetStatDate)
	return nil
}

// recalcBusinessDay 重算单个业务日（CST 自然日）：在同一把 advisory lock 内先删空该
// 业务日的日桶，再用整天权威明细重建。这样任意重跑后，该日总数/成本恒等于明细，
// 且删除/迟到数据也有一致规则（不再被 10 分钟窗口覆盖成尾部值）。
func (s *ReportService) recalcBusinessDay(ctx context.Context, day time.Time) error {
	dayStart := timeutil.StartOfBusinessDay(day)
	dayEnd := dayStart.AddDate(0, 0, 1)
	statDate := timeutil.BusinessDate(dayStart)
	return s.AggregateDaily(ctx, dayStart, dayEnd, statDate)
}

// RecalcAffectedDays 重算 [from, to) 触达的每一个业务日。
//
// FA-REPORT-01：增量不能再只聚合 10 分钟窗口（那会用窗口值覆盖整天计数）——
// 而是找出窗口覆盖的所有业务日（含跨午夜两侧），逐日按完整自然日重算。
// 窗口重叠、乱序、迟到数据都因此收敛到权威明细。
func (s *ReportService) RecalcAffectedDays(ctx context.Context, from, to time.Time) error {
	if to.Before(from) {
		from, to = to, from
	}
	for day := timeutil.StartOfBusinessDay(from); day.Before(to); day = day.AddDate(0, 0, 1) {
		if err := s.recalcBusinessDay(ctx, day); err != nil {
			return err
		}
	}
	return nil
}

// AggregateRecent 增量触发：重算最近 10 分钟窗口触达的业务日（覆盖乱序/迟到写入）。
// 供每 5 分钟的定时任务调用——重算的是"完整受影响日"，不是 10 分钟窗口本身。
func (s *ReportService) AggregateRecent(ctx context.Context) error {
	now := s.now()
	return s.RecalcAffectedDays(ctx, now.Add(-10*time.Minute), now)
}

// RunDailyRecalc 日终重算前一天（00:10 调度调用），按 CST 业务时区自然日取整，
// 不再用 Truncate(24h)（那是 UTC 零点，会把业务日前 8 小时算错到相邻日）。
func (s *ReportService) RunDailyRecalc(ctx context.Context) error {
	yesterday := timeutil.StartOfBusinessDay(s.now()).AddDate(0, 0, -1)
	return s.recalcBusinessDay(ctx, yesterday)
}

// ============================================================
// 查询侧（报表 API）
// ============================================================

// ReportTotals 总览 KPI
type ReportTotals struct {
	CallCount        int64   `json:"call_count"`
	ErrorCount       int64   `json:"error_count"`
	ErrorRate        float64 `json:"error_rate"`
	CacheHitRate     float64 `json:"cache_hit_rate"`
	PromptTokens     int64   `json:"prompt_tokens"`
	CompletionTokens int64   `json:"completion_tokens"`
	InputCostCents   int64   `json:"input_cost_cents"`
	OutputCostCents  int64   `json:"output_cost_cents"`
	TotalCostCents   int64   `json:"total_cost_cents"`
	AvgLatencyMs     int64   `json:"avg_latency_ms"`
	P50LatencyMs     int64   `json:"p50_latency_ms"`
	P95LatencyMs     int64   `json:"p95_latency_ms"`
}

// Overview 聚合表（天粒度）+ 当日实时兜底。
// 成本口径：llm_routing_logs.cost 单位为元（浮点），聚合时 ×100 入 cents；
// 平台模型真分列成本本期以 routing_logs 成本口径出数，platform 表对账接口后置。
func (s *ReportService) Overview(ctx context.Context, from, to time.Time, channel, source string) (*ReportTotals, error) {
	out := &ReportTotals{}
	row, err := s.repo.Totals(ctx, from, to, repository.LLMUsageReportFilter{Channel: channel, Source: source})
	if err != nil {
		return nil, err
	}
	out.CallCount = row.CallCount
	out.ErrorCount = row.ErrorCount
	out.PromptTokens = row.PromptTokens
	out.CompletionTokens = row.CompletionTokens
	out.TotalCostCents = row.TotalCostCents
	if row.CallCount > 0 {
		out.ErrorRate = float64(row.ErrorCount) / float64(row.CallCount)
		out.CacheHitRate = float64(row.CacheHitCount) / float64(row.CallCount)
		out.AvgLatencyMs = row.LatencySumMs / row.CallCount
	}
	// P50/P95 当日实时查明细（聚合表存的是 AVG，P95 精确值在明细侧）。
	// 兜底查询失败不拖垮总览：分位留 0，其余 KPI 照常返回（与历史行为一致）。
	if p50, p95, perr := s.repo.LatencyPercentiles(ctx, from, to); perr == nil {
		out.P50LatencyMs = p50
		out.P95LatencyMs = p95
	}
	return out, nil
}

// TimeseriesPoint 时间序列点
type TimeseriesPoint struct {
	StatDate         string  `json:"date"`
	CallCount        int64   `json:"call_count"`
	ErrorCount       int64   `json:"error_count"`
	ErrorRate        float64 `json:"error_rate"`
	PromptTokens     int64   `json:"prompt_tokens"`
	CompletionTokens int64   `json:"completion_tokens"`
	TotalCostCents   int64   `json:"total_cost_cents"`
	AvgLatencyMs     int64   `json:"avg_latency_ms"`
}

// Timeseries 按天序列（列名分组，禁 Group("1")）
func (s *ReportService) Timeseries(ctx context.Context, from, to time.Time, model_, scenario, channel, source string) ([]TimeseriesPoint, error) {
	rows, err := s.repo.Timeseries(ctx, from, to, repository.LLMUsageReportFilter{
		Model: model_, Scenario: scenario, Channel: channel, Source: source,
	})
	if err != nil {
		return nil, err
	}
	out := make([]TimeseriesPoint, 0, len(rows))
	for _, r := range rows {
		p := TimeseriesPoint{
			StatDate:         r.StatDate,
			CallCount:        r.CallCount,
			ErrorCount:       r.ErrorCount,
			PromptTokens:     r.PromptTokens,
			CompletionTokens: r.CompletionTokens,
			TotalCostCents:   r.TotalCostCents,
			AvgLatencyMs:     r.AvgLatencyMs,
		}
		if p.CallCount > 0 {
			p.ErrorRate = float64(p.ErrorCount) / float64(p.CallCount)
		}
		out = append(out, p)
	}
	return out, nil
}

// BreakdownRow 维度分解行
type BreakdownRow struct {
	Dim              string  `json:"dim"`
	CallCount        int64   `json:"call_count"`
	ErrorCount       int64   `json:"error_count"`
	ErrorRate        float64 `json:"error_rate"`
	CompletionTokens int64   `json:"completion_tokens"`
	TotalCostCents   int64   `json:"total_cost_cents"`
	P95LatencyMs     int64   `json:"p95_latency_ms"`
}

// breakdownCols 维度白名单：HTTP 的 by 参数只能取这里面的列（防注入）。
// 校验留在 service（错误文案属于对外契约），仓储收的是已校验的列名。
var breakdownCols = map[string]string{
	"model":       "model",
	"scenario":    "scenario",
	"channel":     "channel",
	"error_class": "error_class",
	"provider":    "provider",
}

// Breakdown 单维度分解；by 必须在白名单内（防注入）
func (s *ReportService) Breakdown(ctx context.Context, from, to time.Time, by, model_, scenario, channel, source string) ([]BreakdownRow, error) {
	col, ok := breakdownCols[by]
	if !ok {
		return nil, fmt.Errorf("by 参数仅支持 model|scenario|channel|error_class|provider")
	}
	rows, err := s.repo.Breakdown(ctx, col, from, to, repository.LLMUsageReportFilter{
		Model: model_, Scenario: scenario, Channel: channel, Source: source,
	})
	if err != nil {
		return nil, err
	}
	out := make([]BreakdownRow, 0, len(rows))
	for _, r := range rows {
		row := BreakdownRow{
			Dim:              r.Dim,
			CallCount:        r.CallCount,
			ErrorCount:       r.ErrorCount,
			CompletionTokens: r.CompletionTokens,
			TotalCostCents:   r.TotalCostCents,
			P95LatencyMs:     r.P95LatencyMs,
		}
		if row.CallCount > 0 {
			row.ErrorRate = float64(row.ErrorCount) / float64(row.CallCount)
		}
		if row.Dim == "" {
			row.Dim = "(未知)"
		}
		out = append(out, row)
	}
	return out, nil
}

// ErrorRow 错误明细行
type ErrorRow struct {
	TraceID    string    `json:"trace_id"`
	Scenario   string    `json:"scenario"`
	Provider   string    `json:"provider"`
	Model      string    `json:"model"`
	ErrorClass string    `json:"error_class"`
	ErrorMsg   string    `json:"error_msg"`
	LatencyMs  int64     `json:"latency_ms"`
	CreatedAt  time.Time `json:"created_at"`
}

// ErrorReport 错误类聚合 + 失败明细（明细表直查，量小）
func (s *ReportService) ErrorReport(ctx context.Context, from, to time.Time, errorClass string, page, pageSize int) (map[string]any, error) {
	agg, err := s.repo.ErrorClassAgg(ctx, from, to)
	if err != nil {
		return nil, err
	}
	byClass := make([]BreakdownRow, 0, len(agg))
	for _, r := range agg {
		byClass = append(byClass, BreakdownRow{Dim: r.Dim, CallCount: r.CallCount, ErrorCount: r.ErrorCount})
	}
	total, rows, err := s.repo.FailedCalls(ctx, from, to, errorClass, page, pageSize)
	if err != nil {
		return nil, err
	}
	failed := make([]ErrorRow, 0, len(rows))
	for _, r := range rows {
		failed = append(failed, ErrorRow{
			TraceID:    r.TraceID,
			Scenario:   r.Scenario,
			Provider:   r.Provider,
			Model:      r.Model,
			ErrorClass: r.ErrorClass,
			ErrorMsg:   r.ErrorMsg,
			LatencyMs:  r.LatencyMs,
			CreatedAt:  r.CreatedAt,
		})
	}
	return map[string]any{"by_class": byClass, "total": total, "rows": failed,
		"page": page, "page_size": pageSize}, nil
}
