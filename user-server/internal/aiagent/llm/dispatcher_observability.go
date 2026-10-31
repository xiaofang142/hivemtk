package llm

import (
	"context"
	"hash/fnv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"gorm.io/gorm"

	"hivemtk-user/internal/pkg/db"
	"hivemtk-user/internal/pkg/timeutil"
	"hivemtk-user/internal/pkg/utils"
	"hivemtk-user/internal/pkg/utils/logger"
)

const (
	TokenSourceActual    = "actual"
	TokenSourceEstimated = "estimated"
	TokenSourceMissing   = "missing"

	ModelTypeLocal = "local"
	ModelTypeCloud = "cloud"

	SourceDispatch = "dispatch"
	SourceCache    = "cache"
	SourceFallback = "fallback"
	SourceEmpty    = "null"

	EstimatorCharWeight    = "char_weight"
	EstimatorEmptyFallback = "empty_fallback"
)

var (
	missingCounter   int64
	totalCounter     int64
	missingThreshold = int64(20)
)

// InferVendor 根据 BaseURL 推断厂商
//
// 与 service/llm_routing_service.go 的 vendorFromBaseURL 保持一致，
// 避免 service → llm 包的反向依赖。
func InferVendor(baseURL string) string {
	low := strings.ToLower(baseURL)
	switch {
	case strings.Contains(low, "deepseek"):
		return "deepseek"
	case strings.Contains(low, "dashscope"), strings.Contains(low, "qwen"):
		return "qwen"
	case strings.Contains(low, "openai"), strings.Contains(low, "gpt"):
		return "openai"
	case strings.Contains(low, "zhipu"), strings.Contains(low, "glm"), strings.Contains(low, "bigmodel"):
		return "zhipu"
	case strings.Contains(low, "moonshot"), strings.Contains(low, "kimi"):
		return "moonshot"
	case strings.Contains(low, "127.0.0.1"), strings.Contains(low, "localhost"), strings.Contains(low, "mtk-llm"):
		return "local"
	default:
		return "other"
	}
}

// InferModelType 根据 BaseURL 推断模型类型（local / cloud）
func InferModelType(baseURL string) string {
	low := strings.ToLower(baseURL)
	if low == "" || strings.Contains(low, "127.0.0.1") || strings.Contains(low, "localhost") || strings.Contains(low, "mtk-llm") {
		return ModelTypeLocal
	}
	return ModelTypeCloud
}

// InferTokenSource 根据 LLM 真实 Usage 判定 token_source
//
//   - TotalTokens > 0 → actual（LLM 返回了真实 usage）
//   - TotalTokens == 0 且有 content → estimated（需要字符估算兜底）
//   - 无 content 且无 usage → missing（响应异常）
func InferTokenSource(totalTokens int, content string) string {
	if totalTokens > 0 {
		return TokenSourceActual
	}
	if content != "" {
		return TokenSourceEstimated
	}
	return TokenSourceMissing
}

// ClassifyEstimator 根据估算路径标记 estimator 字段
func ClassifyEstimator(tokenSource string) string {
	switch tokenSource {
	case TokenSourceEstimated:
		return EstimatorCharWeight
	case TokenSourceMissing:
		return EstimatorEmptyFallback
	default:
		return ""
	}
}

var (
	auditDBMu sync.RWMutex
	auditDB   *gorm.DB
)

func setAuditDB(d *gorm.DB) {
	auditDBMu.Lock()
	defer auditDBMu.Unlock()
	auditDB = d
}

func getAuditDB() *gorm.DB {
	auditDBMu.RLock()
	defer auditDBMu.RUnlock()
	return auditDB
}

func AttachAuditDB(d *gorm.DB) {
	setAuditDB(d)
}

// LogEntry 路由决策日志条目（包含 v3.6.0 基础字段 + v3.7.0 扩展字段）
//
// 设计原则：
//   - 一份结构体承载所有字段，避免函数参数爆炸
//   - 调用方通过 NewLogEntry 构造，保证扩展字段自动填充
//   - 落库失败仅记日志，不阻塞业务
type LogEntry struct {
	TraceID          string           `json:"trace_id"`
	Scenario         DispatchScenario `json:"scenario"`
	Provider         string           `json:"provider"`
	Model            string           `json:"model"`
	PromptTokens     int              `json:"prompt_tokens"`
	CompletionTokens int              `json:"completion_tokens"`
	TotalTokens      int              `json:"total_tokens"`
	Cost             float64          `json:"cost"`
	LatencyMs        int              `json:"latency_ms"`
	Success          bool             `json:"success"`
	ErrorMsg         string           `json:"error_msg"`
	FromCache        bool             `json:"from_cache"`
	ModelType        string           `json:"model_type"`
	Vendor           string           `json:"vendor"`
	BaseURL          string           `json:"base_url"`
	IsFallback       bool             `json:"is_fallback"`
	PromptCost       float64          `json:"prompt_cost"`
	CompletionCost   float64          `json:"completion_cost"`
	TokenSource      string           `json:"token_source"`
	Estimator        string           `json:"estimator"`
	Source           string           `json:"source"`
	ScenarioProvider string           `json:"scenario_provider"`
	InternalLang     string           `json:"internal_lang,omitempty"`
	TargetLang       string           `json:"target_lang,omitempty"`
	CrossLingual     bool             `json:"cross_lingual,omitempty"`
	GlossaryVersion  string           `json:"glossary_version,omitempty"`
	CacheHit         bool             `json:"cache_hit,omitempty"`
}

// NewLogEntry 根据基础信息构造 LogEntry，自动填充扩展字段
//
// 参数：
//   - scenario:    调度场景
//   - provider:    命中的 provider 配置（用于推断 vendor/model_type/base_url）
//   - model:       实际调用的 model 名
//   - usage:       LLM 真实 token 用量（PromptTokens/CompletionTokens/TotalTokens）
//   - cost:        总成本（用于 cost 字段，prompt/completion 拆分按 token 比例计算）
//   - latencyMs:   调用耗时
//   - success:     是否成功
//   - errMsg:      失败原因
//   - fromCache:   是否命中缓存
//   - isFallback:  是否为降级调用
//   - traceID:     请求 trace_id
//   - content:     LLM 响应内容（用于判定 token_source 是否 estimated）
//   - source:      调用来源（dispatch/cache/fallback/null）
func NewLogEntry(scenario DispatchScenario, provider *ProviderConfig, model string,
	promptTokens, completionTokens, totalTokens int, cost float64, latencyMs int,
	success bool, errMsg string, fromCache, isFallback bool, traceID, content, source string) *LogEntry {

	baseURL := ""
	if provider != nil {
		baseURL = provider.BaseURL
	}
	tokenSource := InferTokenSource(totalTokens, content)
	estimator := ClassifyEstimator(tokenSource)
	modelType := InferModelType(baseURL)
	vendor := InferVendor(baseURL)

	promptCost, completionCost := splitCost(cost, promptTokens, completionTokens, totalTokens)

	if fromCache {
		source = SourceCache
	} else if isFallback {

		source = SourceFallback
	}

	return &LogEntry{
		TraceID:          traceID,
		Scenario:         scenario,
		Provider:         providerName(provider),
		Model:            model,
		PromptTokens:     promptTokens,
		CompletionTokens: completionTokens,
		TotalTokens:      totalTokens,
		Cost:             cost,
		LatencyMs:        latencyMs,
		Success:          success,
		ErrorMsg:         errMsg,
		FromCache:        fromCache,
		ModelType:        modelType,
		Vendor:           vendor,
		BaseURL:          baseURL,
		IsFallback:       isFallback,
		PromptCost:       promptCost,
		CompletionCost:   completionCost,
		TokenSource:      tokenSource,
		Estimator:        estimator,
		Source:           source,
		ScenarioProvider: string(scenario) + "|" + providerName(provider),
	}
}

func providerName(p *ProviderConfig) string {
	if p == nil {
		return ""
	}
	return p.Name
}

// splitCost 把一次调用的总价拆成「输入部分 / 输出部分」。
//
// ============================================================
// ⚠️ 这不是「输入输出分别计价」
//
// cost = totalTokens/1000 × 单价，然后按 token 比例劈成两半。
// 数学上它等价于「输入输出同价」—— 你把单价设成任何值都表达不出
// 「输出贵、输入便宜」这个真实世界的事实。
//
// 需求原文：「商户使用平台端 llM 时，实时扣减剩余 token 数，
// **输入和输出分别计价**以及扣费」。因此计价改在 computeCost 里
// 按两个单价分别计算，splitCost 只保留给**已经算好的总价**做展示拆分。
// 它仍被 LogRoutingDecision 的行模型使用（历史数据没有分列单价），
// 因此不能删，但**不能再用于计费**。
// ============================================================
//
// 保留理由：历史 llm_routing_logs 的 prompt_cost / completion_cost 是按比例
// 拆出来的，报表要继续能画；新数据走分别计价后两者口径不同，展示层需要能分辨。
func splitCost(totalCost float64, promptTokens, completionTokens, totalTokens int) (promptCost, completionCost float64) {
	if totalTokens <= 0 {
		return totalCost, 0
	}
	ratio := float64(promptTokens) / float64(totalTokens)
	promptCost = totalCost * ratio
	completionCost = totalCost - promptCost
	return
}

// computeCost 按「输入单价 × 输入 token + 输出单价 × 输出 token」计价。
//
// 与 splitCost 的区别是这一条才**分别计价**：provider 配了
// cost_per_1k_input / cost_per_1k_output 时，两部分按各自单价累加；
// 只配了单一 cost_per_1k 时两者回落成同价（兼容老配置）。
func computeCost(p ProviderConfig, promptTokens, completionTokens int) (total, inputCost, outputCost float64) {
	if promptTokens <= 0 && completionTokens <= 0 {
		return 0, 0, 0
	}
	inRate := p.EffectiveInputCostPer1k()
	outRate := p.EffectiveOutputCostPer1k()
	inputCost = float64(promptTokens) / 1000.0 * inRate
	outputCost = float64(completionTokens) / 1000.0 * outRate
	return inputCost + outputCost, inputCost, outputCost
}

// LogRoutingDecision 记录一次 dispatch 决策（v3.7.0 重构版）
//
// 调用方优先使用 NewLogEntry 构造 entry，再传入本函数。
// 落库失败仅记录日志，不阻塞业务。
// 同时维护 missing 计数器，超过阈值时触发告警。
func LogRoutingDecision(ctx context.Context, entry *LogEntry) {
	if entry == nil {
		return
	}
	d := getAuditDB()
	if d == nil {
		updateMissingCounter(entry)
		return
	}
	row := map[string]any{
		"trace_id":          entry.TraceID,
		"scenario":          string(entry.Scenario),
		"provider":          entry.Provider,
		"model":             entry.Model,
		"prompt_tokens":     entry.PromptTokens,
		"completion_tokens": entry.CompletionTokens,
		"total_tokens":      entry.TotalTokens,
		"cost":              entry.Cost,
		"latency_ms":        entry.LatencyMs,
		"success":           entry.Success,
		"error_msg":         entry.ErrorMsg,
		"from_cache":        entry.FromCache,
		"model_type":        entry.ModelType,
		"vendor":            entry.Vendor,
		"base_url":          entry.BaseURL,
		"is_fallback":       entry.IsFallback,
		"prompt_cost":       entry.PromptCost,
		"completion_cost":   entry.CompletionCost,
		"token_source":      entry.TokenSource,
		"estimator":         entry.Estimator,
		"source":            entry.Source,
		"scenario_provider": entry.ScenarioProvider,
		"internal_lang":     entry.InternalLang,
		"target_lang":       entry.TargetLang,
		"cross_lingual":     entry.CrossLingual,
		"glossary_version":  entry.GlossaryVersion,
		"cache_hit":         entry.CacheHit,
		// map 插入不经过 GORM schema，autoCreateTime 不会生效；created_at 必须显式写入。
		"created_at": time.Now(),
	}
	if err := d.WithContext(ctx).Table("llm_routing_logs").Create(row).Error; err != nil {
		logger.Warnf("[LLM] LogRoutingDecision write failed: %v (entry=%+v)", err, entry)
	}
	updateMissingCounter(entry)
}

func updateMissingCounter(entry *LogEntry) {
	if entry == nil {
		return
	}
	if entry.Source == SourceCache {
		return
	}
	total := atomic.AddInt64(&totalCounter, 1)
	if entry.TokenSource == TokenSourceMissing {
		atomic.AddInt64(&missingCounter, 1)
	}
	if total%100 == 0 {
		missing := atomic.LoadInt64(&missingCounter)
		if total > 0 {
			ratio := missing * 100 / total
			if ratio >= missingThreshold {
				logger.Errorf("[LLM] token_source=missing 占比告警: %d/%d (%d%%) >= %d%%阈值，请检查 LLM API 响应完整性",
					missing, total, ratio, missingThreshold)
			}
		}
	}
}

func GetTokenSourceStats() (total, missing int64) {
	total = atomic.LoadInt64(&totalCounter)
	missing = atomic.LoadInt64(&missingCounter)
	if total > 0 {
		return total, missing
	}
	if dbTotal, dbMissing := queryTokenSourceStatsFromDB(); dbTotal > 0 {
		return dbTotal, dbMissing
	}
	return total, missing
}

func queryTokenSourceStatsFromDB() (total, missing int64) {
	d := getAuditDB()
	if d == nil {
		return 0, 0
	}
	type row struct {
		Total   int64
		Missing int64
	}
	var r row
	err := d.WithContext(context.Background()).
		Table("llm_routing_logs").
		Select("COUNT(*) AS total, COALESCE(SUM(CASE WHEN token_source = ? THEN 1 ELSE 0 END), 0) AS missing", TokenSourceMissing).
		Where("source <> ?", SourceCache).
		Scan(&r).Error
	if err != nil {
		logger.Warnf("[LLM] GetTokenSourceStats DB fallback failed: %v", err)
		return 0, 0
	}
	return r.Total, r.Missing
}

// ResetTokenSourceStats 重置计数器（仅供测试使用）
func ResetTokenSourceStats() {
	atomic.StoreInt64(&totalCounter, 0)
	atomic.StoreInt64(&missingCounter, 0)
}

// ScenarioStat 场景维度统计（用于 Usage API）
type ScenarioStat struct {
	Scenario     string  `json:"scenario"`
	Provider     string  `json:"provider"`
	Model        string  `json:"model"`
	CallCount    int64   `json:"call_count"`
	SuccessCount int64   `json:"success_count"`
	FailedCount  int64   `json:"failed_count"`
	TotalTokens  int64   `json:"total_tokens"`
	TotalCost    float64 `json:"total_cost"`
	AvgLatencyMs int64   `json:"avg_latency_ms"`
	WindowLabel  string  `json:"window_label"`
}

// QueryScenarioStats 查 llm_routing_logs 按 (scenario, provider) 聚合
//
// 参数：
//   - window: 时间窗口（"today" / "week" / "month" / "all"），默认 "all"
//   - limit:  最多返回多少条聚合（默认 200）
//   - enabledProviders: 只统计这些 provider（为空则不过滤）
func QueryScenarioStats(ctx context.Context, window string, limit int, enabledProviders []string) ([]ScenarioStat, error) {
	d := getAuditDB()
	if d == nil {
		return nil, nil
	}
	if limit <= 0 {
		limit = 200
	}
	var since time.Time
	now := time.Now()
	switch window {
	case "today":
		since = time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	case "week":
		since = now.AddDate(0, 0, -7)
	case "month":
		// 上一个自然月 1 号零点起算（月末 AddDate 归一化会漂移，见 timeutil.TrailingMonthStart）。
		since = timeutil.TrailingMonthStart(now)
	default:
		since = time.Time{}
	}

	q := d.WithContext(ctx).Table("llm_routing_logs").
		Select(`scenario, provider, MAX(model) AS model,
				COUNT(*) AS call_count,
				SUM(CASE WHEN success THEN 1 ELSE 0 END) AS success_count,
				SUM(CASE WHEN success THEN 0 ELSE 1 END) AS failed_count,
				COALESCE(SUM(total_tokens),0) AS total_tokens,
				COALESCE(SUM(cost),0) AS total_cost,
				COALESCE(AVG(latency_ms),0)::bigint AS avg_latency_ms`).
		Group("scenario, provider").
		Order("call_count DESC").
		Limit(limit)
	if !since.IsZero() {
		q = q.Where("created_at >= ?", since)
	}
	if len(enabledProviders) > 0 {
		q = q.Where("provider IN ?", enabledProviders)
	}
	var rows []ScenarioStat
	if err := q.Scan(&rows).Error; err != nil {
		return nil, err
	}
	for i := range rows {
		rows[i].WindowLabel = window
	}
	return rows, nil
}

// QueryAuditHistory 查路由变更历史
func QueryAuditHistory(ctx context.Context, scenario string, limit int) ([]map[string]any, error) {
	d := getAuditDB()
	if d == nil {
		return nil, nil
	}
	if limit <= 0 {
		limit = 50
	}
	q := d.WithContext(ctx).Table("llm_routing_audit").
		Order("created_at DESC").
		Limit(limit)
	if scenario != "" {
		q = q.Where("scenario = ?", scenario)
	}
	var rows []map[string]any
	if err := q.Scan(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

// DecideCanaryRoute 决定本次 Dispatch 走哪个 route（主或灰度）
//
// 决策规则：
//   - Weight = 0：返回 nil（灰度未启用，走主路由）
//   - Weight = 100：返回 CanaryRoute（全部走灰度）
//   - 0 < Weight < 100：
//     1) 若 CanaryKey 非空 → 用 fnv32(canaryKey) % 100 与 Weight 比较
//     2) 若 CanaryKey 为空 → 用 time.Now().UnixNano() % 100 与 Weight 比较
//   - 返回 nil 时走主 route；返回非 nil 时走 canary route
func DecideCanaryRoute(route *ScenarioRoute, canaryKey string) *ScenarioRoute {
	if route == nil || route.Weight <= 0 || route.CanaryRoute == nil {
		return nil
	}
	if route.Weight >= 100 {
		return route.CanaryRoute
	}
	var bucket uint64
	if canaryKey != "" {
		h := fnv.New64a()
		_, _ = h.Write([]byte(canaryKey))
		bucket = h.Sum64() % 100
	} else {
		bucket = uint64(time.Now().UnixNano() % 100)
	}
	if bucket < uint64(route.Weight) {
		return route.CanaryRoute
	}
	return nil
}

// StartCacheJanitor 启动后台 goroutine 定期清理过期 cache 项
//
// 间隔：60 秒一次；可调用返回的 stop 函数终止。
// 解决缺陷 10：cache map 只增不减导致 OOM 风险。
func (d *Dispatcher) StartCacheJanitor(ctx context.Context, interval time.Duration) (stop func()) {
	if interval <= 0 {
		interval = 60 * time.Second
	}
	ticker := time.NewTicker(interval)
	stopCh := make(chan struct{})
	var once sync.Once
	utils.SafeGo(ctx, "llm.dispatcher.cache_janitor", func(ctx context.Context) {
		for {
			select {
			case <-ctx.Done():
				return
			case <-stopCh:
				return
			case <-ticker.C:
				d.sweepExpiredCache()
			}
		}
	})
	return func() {
		once.Do(func() {
			close(stopCh)
			ticker.Stop()
		})
	}
}

func (d *Dispatcher) sweepExpiredCache() {}

func InitGlobalDispatcherWithDB(d *Dispatcher, gormDB *gorm.DB) {
	InitGlobalDispatcher(d)
	setAuditDB(gormDB)
	if getAuditDB() == nil {
		setAuditDB(db.GetDB())
	}
	// 启动全局熔断健康检查，首次请求前即可感知 LLM provider 不可达，
	// 配合 HasHealthyProvider 短路，消除首次请求的逐 provider 重试时延。
	if fo := GetGlobalFailover(); fo != nil {
		fo.Start(context.Background())
	}
}

// DailyUsagePoint 按日用量点（P1-4 模型管理面：按日/按场景聚合看板维度）。
type DailyUsagePoint struct {
	StatDate     string  `json:"stat_date"` // 'YYYY-MM-DD'（本地时区日）
	Scenario     string  `json:"scenario"`
	Provider     string  `json:"provider"`
	CallCount    int64   `json:"call_count"`
	FailedCount  int64   `json:"failed_count"`
	TotalTokens  int64   `json:"total_tokens"`
	TotalCost    float64 `json:"total_cost"`
	AvgLatencyMs int64   `json:"avg_latency_ms"`
}

// QueryDailyUsage 按日聚合用量（llm_routing_logs created_at 分日）。
// window: today / week / month / all（与 QueryScenarioStats 同口径）。
// groupBy: day / day_scenario / day_provider（默认 day_scenario）。
func QueryDailyUsage(ctx context.Context, window, groupBy string, limit int) ([]DailyUsagePoint, error) {
	d := getAuditDB()
	if d == nil {
		return nil, nil
	}
	if limit <= 0 {
		limit = 120
	}
	var since time.Time
	now := time.Now()
	switch window {
	case "today":
		since = time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	case "week":
		since = now.AddDate(0, 0, -7)
	case "month":
		// 上一个自然月 1 号零点起算（月末 AddDate 归一化会漂移，见 timeutil.TrailingMonthStart）。
		since = timeutil.TrailingMonthStart(now)
	}

	// 本地时区日：created_at::date（PG）。
	//
	// ⚠️ SELECT 与 GROUP BY 必须是两份表达式：PG 不接受 `GROUP BY expr AS alias`
	// （报 syntax error at or near "AS"）。此前两者共用同一个带别名的串，
	// 于是 /api/llm/usage/daily 在任何窗口下都 500——接口存在但从来没成功返回过。
	selectCols := "created_at::date AS stat_date, scenario, provider"
	groupCols := "created_at::date, scenario, provider"
	switch groupBy {
	case "day":
		selectCols = "created_at::date AS stat_date"
		groupCols = "created_at::date"
	case "day_provider":
		selectCols = "created_at::date AS stat_date, provider"
		groupCols = "created_at::date, provider"
	default: // day_scenario
	}
	q := d.WithContext(ctx).Table("llm_routing_logs").
		Select(selectCols + `, COUNT(*) AS call_count,
				SUM(CASE WHEN success THEN 0 ELSE 1 END) AS failed_count,
				COALESCE(SUM(total_tokens),0) AS total_tokens,
				COALESCE(SUM(cost),0) AS total_cost,
				COALESCE(AVG(latency_ms),0)::bigint AS avg_latency_ms`).
		Group(groupCols).
		Order("stat_date DESC").
		Limit(limit)
	if !since.IsZero() {
		q = q.Where("created_at >= ?", since)
	}
	var rows []DailyUsagePoint
	if err := q.Scan(&rows).Error; err != nil {
		return nil, err
	}
	// PG date 经驱动扫进 string 会变成 RFC3339（2026-10-24T00:00:00Z），
	// 前端表格按日期列直接渲染，统一裁成 YYYY-MM-DD。
	for i := range rows {
		if len(rows[i].StatDate) > 10 {
			rows[i].StatDate = rows[i].StatDate[:10]
		}
	}
	return rows, nil
}
