// observability_dashboard.go 统一看板口径的**只读**仓储（D2 + B2，specs/007）。
//
// 本文件只做「把既有表读出聚合行」，不做任何判定：送达率、在线率、渠道状态、
// 转化率全部在 service 层算——这样判定口径只有 service 一处，用例可以不连库直接测。
// 全仓库层统一 `db.WithContext(ctx)`：租户隔离由既有 tenant callback 盖章，
// 本层不自己拼 tenant_id 条件（与 bridge_selector_health 仓储同口径）。
package repository

import (
	"context"
	"time"

	"gorm.io/gorm"

	"hivemtk-user/internal/model"
)

// ObservabilityDashboardRepository 统一看板只读仓储。
type ObservabilityDashboardRepository interface {
	// LLMCost 窗口内按 (provider, scenario) 聚合 llm_routing_logs。
	LLMCost(ctx context.Context, since time.Time) ([]ObsDashLLMCostRow, error)
	// Delivery 窗口内按渠道（message_hub.platform）聚合出站送达计数。
	Delivery(ctx context.Context, since time.Time) ([]ObsDashDeliveryRow, error)
	// Funnel 窗口内 message_trace 六个生命周期节点的去重 trace 计数。
	Funnel(ctx context.Context, since time.Time) ([]ObsDashFunnelStageRow, error)
	// ChannelAccounts 按渠道聚合 bridge_accounts 的账号总数与在线数。
	// 在线数用 bridgeOnlineSQLPredicate（同一谓词，见 bridge_online_predicate.go），
	// graceSeconds 由调用方从 service.BridgeOnlineGraceWindow 读好传入。
	ChannelAccounts(ctx context.Context, graceSeconds int) ([]ObsDashChannelAccountRow, error)
	// SelectorHealthRows 当前租户全部选择器健康计数（复用 B1-a 表，判定在 service）。
	SelectorHealthRows(ctx context.Context) ([]model.BridgeSelectorHealth, error)
	// DomainBanSummary domain_pool 的封禁/风险域名计数。
	DomainBanSummary(ctx context.Context) (ObsDashDomainBanSummaryRow, error)

	// ── TB-11 意图路由观测（刀4） ──────────────────────────────────────────
	// RouteIntentDistribution 窗口内 intent_records 按 intent_type 聚合计数
	// （真实识别分布，含未绑定链路的意图）。
	RouteIntentDistribution(ctx context.Context, since time.Time) ([]ObsDashRouteIntentRow, error)
	// RouteChainHitRate 窗口内 intent_route_daily 链级行按 chain_name 聚合
	// 命中/跳过（tool_name='' 的链级行；含本轮 0 工具调用的 miss 行）。
	RouteChainHitRate(ctx context.Context, since time.Time) ([]ObsDashRouteChainRow, error)
	// RouteToolSuccessRate 窗口内 intent_route_daily 工具级行按 tool_name 聚合
	// 调用数/成功数（过滤伪行：链级空串行与 '__correction__' 纠错事件行）。
	RouteToolSuccessRate(ctx context.Context, since time.Time) ([]ObsDashRouteToolRow, error)
	// RouteNorthStar 窗口内北极星三指标的分子/分母读数：
	// sessions/resolved/handoff（customer_sessions）+ corrections（纠错事件行
	// 计数）+ recognized（intent_records 识别量），比率在 service 层算。
	RouteNorthStar(ctx context.Context, since time.Time) (ObsDashRouteNorthStarRow, error)
}

// ObsDashLLMCostRow 一条 (provider, scenario) 聚合。
type ObsDashLLMCostRow struct {
	Provider         string  `gorm:"column:provider"`
	Scenario         string  `gorm:"column:scenario"`
	Calls            int64   `gorm:"column:calls"`
	SuccessCalls     int64   `gorm:"column:success_calls"`
	PromptTokens     int64   `gorm:"column:prompt_tokens"`
	CompletionTokens int64   `gorm:"column:completion_tokens"`
	Cost             float64 `gorm:"column:cost"`
	LatencyMs        int64   `gorm:"column:latency_ms"`
}

// ObsDashDeliveryRow 一条渠道送达聚合。
type ObsDashDeliveryRow struct {
	Channel   string `gorm:"column:channel"`
	Delivered int64  `gorm:"column:delivered"`
	Failed    int64  `gorm:"column:failed"`
	Inflight  int64  `gorm:"column:inflight"`
}

// ObsDashFunnelStageRow 一个生命周期节点的去重 trace 计数。
type ObsDashFunnelStageRow struct {
	Node string `gorm:"column:node"`
	Cnt  int64  `gorm:"column:cnt"`
}

// ObsDashChannelAccountRow 一条渠道账号聚合。
type ObsDashChannelAccountRow struct {
	Channel  string `gorm:"column:channel"`
	Accounts int64  `gorm:"column:accounts"`
	Online   int64  `gorm:"column:online"`
}

// ObsDashDomainBanSummaryRow domain_pool 封禁/风险域名计数。
type ObsDashDomainBanSummaryRow struct {
	Blacklisted int64 `gorm:"column:blacklisted"`
	Total       int64 `gorm:"column:total"`
}

// ── TB-11 意图路由观测行结构（刀4） ──────────────────────────────────────

// ObsDashRouteIntentRow 意图分布一行（intent_type → 识别量）。
type ObsDashRouteIntentRow struct {
	IntentType string `gorm:"column:intent_type"`
	Cnt        int64  `gorm:"column:cnt"`
}

// ObsDashRouteChainRow 链命中率一行（chain_name → 命中/跳过）。
type ObsDashRouteChainRow struct {
	ChainName string `gorm:"column:chain_name"`
	Hits      int64  `gorm:"column:hits"`
	Misses    int64  `gorm:"column:misses"`
}

// ObsDashRouteToolRow 工具成功率一行（tool_name → 调用数/成功数）。
type ObsDashRouteToolRow struct {
	ToolName string `gorm:"column:tool_name"`
	Calls    int64  `gorm:"column:calls"`
	Success  int64  `gorm:"column:success"`
}

// ObsDashRouteNorthStarRow 北极星读数行（sessions/resolved/handoff 来自
// customer_sessions；corrections/recognized 来自意图路由聚合表与 intent_records）。
type ObsDashRouteNorthStarRow struct {
	Sessions    int64 `gorm:"column:sessions"`
	Resolved    int64 `gorm:"column:resolved"`
	Handoff     int64 `gorm:"column:handoff"`
	Corrections int64 `gorm:"column:corrections"`
	Recognized  int64 `gorm:"column:recognized"`
}

type observabilityDashboardRepo struct {
	db *gorm.DB
}

// NewObservabilityDashboardRepository 构造仓储。db 为 nil 时返回 nil 接口——
// 与仓内其它 repository 同口径（ARC-01：未初始化守卫由调用方用 repo == nil 表达）。
func NewObservabilityDashboardRepository(db *gorm.DB) ObservabilityDashboardRepository {
	if db == nil {
		return nil
	}
	return &observabilityDashboardRepo{db: db}
}

func (r *observabilityDashboardRepo) LLMCost(ctx context.Context, since time.Time) ([]ObsDashLLMCostRow, error) {
	var rows []ObsDashLLMCostRow
	err := r.db.WithContext(ctx).Table("llm_routing_logs").
		Select("provider, scenario, COUNT(*) AS calls, "+
			"COALESCE(SUM(CASE WHEN success THEN 1 ELSE 0 END),0) AS success_calls, "+
			"COALESCE(SUM(prompt_tokens),0) AS prompt_tokens, "+
			"COALESCE(SUM(completion_tokens),0) AS completion_tokens, "+
			"COALESCE(SUM(cost),0) AS cost, "+
			"COALESCE(SUM(latency_ms),0) AS latency_ms").
		Where("created_at >= ?", since).
		Group("provider, scenario").
		Order("provider asc, scenario asc").
		Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	return rows, nil
}

func (r *observabilityDashboardRepo) Delivery(ctx context.Context, since time.Time) ([]ObsDashDeliveryRow, error) {
	var rows []ObsDashDeliveryRow
	// 送达率的分母是「已了结」行（delivered + failed）；pending/acked 是「在途」，
	// 塞进分母会把刚发出的消息算成「未送达」压低送达率——所以单列 inflight 展示。
	err := r.db.WithContext(ctx).Table("message_hub").
		Select("platform AS channel, "+
			"COALESCE(SUM(CASE WHEN status='delivered' THEN 1 ELSE 0 END),0) AS delivered, "+
			"COALESCE(SUM(CASE WHEN status='failed' THEN 1 ELSE 0 END),0) AS failed, "+
			"COALESCE(SUM(CASE WHEN status NOT IN ('delivered','failed') THEN 1 ELSE 0 END),0) AS inflight").
		Where("direction = 'outbound' AND created_at >= ?", since).
		Group("platform").
		Order("platform asc").
		Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	return rows, nil
}

func (r *observabilityDashboardRepo) Funnel(ctx context.Context, since time.Time) ([]ObsDashFunnelStageRow, error) {
	var rows []ObsDashFunnelStageRow
	// 六个生命周期节点固定存在于 tracing.nodeOrder；agent_turn / tool_call 的 node 值
	// 不同（挂在 ai_dispatch 之下），天然被 node IN(...) 排除，无需再滤 span_kind。
	err := r.db.WithContext(ctx).Table("message_trace").
		Select("node, COUNT(DISTINCT trace_id) AS cnt").
		Where("node IN ('ingest','ai_dispatch','outbound_enqueue','inbox_sync','downlink_fetch','delivered_ack')").
		Where("created_at >= ?", since).
		Group("node").
		Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	return rows, nil
}

func (r *observabilityDashboardRepo) ChannelAccounts(ctx context.Context, graceSeconds int) ([]ObsDashChannelAccountRow, error) {
	var rows []ObsDashChannelAccountRow
	// 在线判定必须与其它三个读侧（渠道总览/主动触达/离线回扫）同一条 predicate：
	// 各写一份 SQL 会量出互相矛盾的「在线数」。占位符顺序 (offline, graceSeconds)。
	err := r.db.WithContext(ctx).Table("bridge_accounts").
		Select("channel, COUNT(*) AS accounts, "+
			"COALESCE(SUM(CASE WHEN "+bridgeOnlineSQLPredicate+" THEN 1 ELSE 0 END),0) AS online",
			bridgeStatusOffline, graceSeconds).
		Group("channel").
		Order("channel asc").
		Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	return rows, nil
}

func (r *observabilityDashboardRepo) SelectorHealthRows(ctx context.Context) ([]model.BridgeSelectorHealth, error) {
	var rows []model.BridgeSelectorHealth
	err := r.db.WithContext(ctx).
		Order("channel asc, field asc").
		Limit(BridgeSelectorHealthListLimit).
		Find(&rows).Error
	if err != nil {
		return nil, err
	}
	return rows, nil
}

func (r *observabilityDashboardRepo) DomainBanSummary(ctx context.Context) (ObsDashDomainBanSummaryRow, error) {
	var row ObsDashDomainBanSummaryRow
	err := r.db.WithContext(ctx).Table("domain_pool").
		Select("COALESCE(SUM(CASE WHEN on_blacklist THEN 1 ELSE 0 END),0) AS blacklisted, COUNT(*) AS total").
		Scan(&row).Error
	if err != nil {
		return ObsDashDomainBanSummaryRow{}, err
	}
	return row, nil
}

// RouteIntentDistribution 窗口内 intent_records 按 intent_type 聚合计数。
// 直接查 intent_records（IntentLog.TableName）：含未绑定链路的意图，分布才 truthful。
func (r *observabilityDashboardRepo) RouteIntentDistribution(ctx context.Context, since time.Time) ([]ObsDashRouteIntentRow, error) {
	var rows []ObsDashRouteIntentRow
	err := r.db.WithContext(ctx).Model(&model.IntentLog{}).
		Select("intent_type, COUNT(*) AS cnt").
		Where("created_at >= ?", since).
		Group("intent_type").
		Order("cnt desc, intent_type asc").
		Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	return rows, nil
}

// RouteChainHitRate 窗口内链级行（tool_name=”）按 chain_name 聚合命中/跳过。
func (r *observabilityDashboardRepo) RouteChainHitRate(ctx context.Context, since time.Time) ([]ObsDashRouteChainRow, error) {
	var rows []ObsDashRouteChainRow
	err := r.db.WithContext(ctx).Table("intent_route_daily").
		Select("chain_name, COALESCE(SUM(chain_hits),0) AS hits, COALESCE(SUM(chain_misses),0) AS misses").
		Where("tool_name = '' AND created_at >= ?", since).
		Group("chain_name").
		Order("chain_name asc").
		Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	return rows, nil
}

// RouteToolSuccessRate 窗口内工具级行按 tool_name 聚合调用数/成功数；
// 伪行（链级空串行与 '__correction__' 纠错事件行）不是真实工具调用，排除。
func (r *observabilityDashboardRepo) RouteToolSuccessRate(ctx context.Context, since time.Time) ([]ObsDashRouteToolRow, error) {
	var rows []ObsDashRouteToolRow
	err := r.db.WithContext(ctx).Table("intent_route_daily").
		Select("tool_name, COALESCE(SUM(tool_calls),0) AS calls, COALESCE(SUM(tool_success),0) AS success").
		Where("tool_name NOT IN ('', '__correction__') AND created_at >= ?", since).
		Group("tool_name").
		Order("tool_name asc").
		Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	return rows, nil
}

// RouteNorthStar 窗口内北极星读数：会话三计数 + 纠错数 + 识别量。
// 会话口径在 customer_sessions（软删由 Model 查询自动排除）；纠错数取
// intent_route_daily '__correction__' 行 tool_calls 之和；识别量取 intent_records 计数。
func (r *observabilityDashboardRepo) RouteNorthStar(ctx context.Context, since time.Time) (ObsDashRouteNorthStarRow, error) {
	var row ObsDashRouteNorthStarRow
	err := r.db.WithContext(ctx).Model(&model.CustomerSession{}).
		Select("COUNT(*) AS sessions, "+
			"COALESCE(SUM(CASE WHEN closed_at IS NOT NULL OR resolved_at IS NOT NULL THEN 1 ELSE 0 END),0) AS resolved, "+
			"COALESCE(SUM(CASE WHEN handoff_at IS NOT NULL THEN 1 ELSE 0 END),0) AS handoff").
		Where("created_at >= ?", since).
		Scan(&row).Error
	if err != nil {
		return ObsDashRouteNorthStarRow{}, err
	}
	var corr struct {
		Corrections int64 `gorm:"column:corrections"`
	}
	err = r.db.WithContext(ctx).Table("intent_route_daily").
		Select("COALESCE(SUM(tool_calls),0) AS corrections").
		Where("tool_name = '__correction__' AND created_at >= ?", since).
		Scan(&corr).Error
	if err != nil {
		return ObsDashRouteNorthStarRow{}, err
	}
	var recognized int64
	err = r.db.WithContext(ctx).Model(&model.IntentLog{}).
		Where("created_at >= ?", since).
		Count(&recognized).Error
	if err != nil {
		return ObsDashRouteNorthStarRow{}, err
	}
	row.Corrections = corr.Corrections
	row.Recognized = recognized
	return row, nil
}
