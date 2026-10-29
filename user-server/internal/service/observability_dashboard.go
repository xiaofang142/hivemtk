// observability_dashboard.go 统一看板口径（D2 + B2，specs/007）。
//
// 为什么叫「口径」而不是「监控」：monitor 包已有节点健康/异常/时延一组技术面读数，
// 但运营面对的是另一组问题——AI 花多少钱、消息送到没有、链路卡在哪、渠道还活着吗。
// 这些问题各读一张表、各有分母，散在各处就是四个互相矛盾的答案。
// 本服务把「分母怎么取、状态怎么判」收敛到这一处；判定全部是纯函数，可不连库测。
package service

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/repository"
)

// ErrDashboardRepoNotReady 仓储未注入（装配缺失）。
var ErrDashboardRepoNotReady = errors.New("observability dashboard repository not initialized")

// DashboardDefaultWindowHours 看板默认窗口的小时数**代码兜底**（7 天）。
// 生效值由参数中心 `telemetry.dashboard_default_window_hours` 决定（改完即时生效）；
// 这个常量只在库/参数服务不可用时生效，且必须与种子 DefaultValue 一致
// （判据见 scripts/check-config-param-defaults.py）。
const DashboardDefaultWindowHours = 168

// DashboardDefaultWindow 看板默认窗口（7 天）。window 参数非法时回落这里。
const DashboardDefaultWindow = time.Duration(DashboardDefaultWindowHours) * time.Hour

// ChannelOnlineDegradedBelow 渠道在线率低于该百分比即判 degraded 的**代码兜底**。
// 生效值由参数中心 `telemetry.channel_online_degraded_below` 决定。
//
// 90 的理由：在线率不是「有没有」的二元量——3 个账号掉 1 个就是 66.7%，
// 但对这个租户来说「有一条消息发不出去」已经是真问题；而 100% 又不该是门槛
// （偶发重连会天天飘红）。90 允许 10 个账号掉 1 个不响，掉 2 个才响。
const ChannelOnlineDegradedBelow = 90

// ── 状态取值 ────────────────────────────────────────────────────────────────

// 渠道健康状态。顺序有意义（见 ClassifyChannelHealth）：banned 与 offline 必须先于
// degraded 判定，否则「选择器全失效（封禁/改版）」会被显示成温和的「降级」。
const (
	ChannelHealthStatusHealthy  = "healthy"
	ChannelHealthStatusDegraded = "degraded"
	ChannelHealthStatusOffline  = "offline"
	ChannelHealthStatusBanned   = "banned"
	ChannelHealthStatusEmpty    = "empty" // 未接入渠道：不算异常
)

// ── 响应形状（与 selector_health 同风格：服务层给 view，controller 只包装） ──

// AICostSection D2 · AI 成本。
type AICostSection struct {
	Calls            int64           `json:"calls"`
	SuccessCalls     int64           `json:"success_calls"`
	PromptTokens     int64           `json:"prompt_tokens"`
	CompletionTokens int64           `json:"completion_tokens"`
	TotalTokens      int64           `json:"total_tokens"`
	Cost             float64         `json:"cost"`
	AvgLatencyMs     int64           `json:"avg_latency_ms"`
	ByProvider       []AICostByGroup `json:"by_provider"`
	ByScenario       []AICostByGroup `json:"by_scenario"`
}

// AICostByGroup 一条分组聚合。
type AICostByGroup struct {
	Key          string  `json:"key"`
	Calls        int64   `json:"calls"`
	Tokens       int64   `json:"tokens"`
	Cost         float64 `json:"cost"`
	AvgLatencyMs int64   `json:"avg_latency_ms"`

	latencyMs int64 // 内部累加（同包聚合用），不参与 JSON 输出
}

// DeliverySection D2 · 送达率（全局 + 按渠道）。
type DeliverySection struct {
	Delivered int64             `json:"delivered"`
	Failed    int64             `json:"failed"`
	Inflight  int64             `json:"inflight"`
	Rate      float64           `json:"rate"` // 0–100，一位小数；分母 0 → 0
	ByChannel []ChannelDelivery `json:"by_channel"`
}

// ChannelDelivery 一条渠道送达读数。
type ChannelDelivery struct {
	Channel   string  `json:"channel"`
	Delivered int64   `json:"delivered"`
	Failed    int64   `json:"failed"`
	Inflight  int64   `json:"inflight"`
	Rate      float64 `json:"rate"`
}

// FunnelSection D2 · 转化漏斗。
type FunnelSection struct {
	Stages []ObsDashFunnelStage `json:"stages"`
}

// FunnelStage 一个生命周期节点。
type ObsDashFunnelStage struct {
	Node       string  `json:"node"`
	NodeLabel  string  `json:"node_label"`
	Count      int64   `json:"count"`
	Conversion float64 `json:"conversion"` // 对上一阶段的转化率；首节点为 0
}

// ChannelHealthSection B2 · 渠道健康（在线率 + 封禁信号）。
type ChannelHealthSection struct {
	Channels     []ChannelHealthView `json:"channels"`
	DomainBanned int64               `json:"domain_banned"`
	DomainTotal  int64               `json:"domain_total"`
}

// ChannelHealthView 一条渠道健康读数。
type ChannelHealthView struct {
	Channel       string  `json:"channel"`
	Accounts      int64   `json:"accounts"`
	Online        int64   `json:"online"`
	OnlineRate    float64 `json:"online_rate"`    // 0–100，一位小数
	SelectorWorst string  `json:"selector_worst"` // ok/degraded/broken/warming/none
	BrokenFields  int     `json:"broken_fields"`
	Status        string  `json:"status"` // healthy/degraded/offline/banned/empty
}

// DashboardSnapshot 统一看板快照。
type ObsDashSnapshot struct {
	GeneratedAt   time.Time            `json:"generated_at"`
	Window        string               `json:"window"`
	AICost        AICostSection        `json:"ai_cost"`
	Delivery      DeliverySection      `json:"delivery"`
	Funnel        FunnelSection        `json:"funnel"`
	ChannelHealth ChannelHealthSection `json:"channel_health"`
}

// ObservabilityDashboardService 统一看板服务。
type ObservabilityDashboardService struct {
	repo repository.ObservabilityDashboardRepository
	now  func() time.Time
}

// NewObservabilityDashboardService 构造服务。repo 为 nil 时 Snapshot 报显式错误，
// 而不是 nil 解引用（装配缺失 = 一次错误，与 selector_health 同口径）。
func NewObservabilityDashboardService(repo repository.ObservabilityDashboardRepository) *ObservabilityDashboardService {
	return &ObservabilityDashboardService{repo: repo, now: time.Now}
}

// Snapshot 聚合一份看板快照。window 形如 "24h" / "7d" / "30d"，非法回落 7d。
func (s *ObservabilityDashboardService) Snapshot(ctx context.Context, window string) (*ObsDashSnapshot, error) {
	if s.repo == nil {
		return nil, ErrDashboardRepoNotReady
	}
	defaultWindow := s.defaultWindow(ctx)
	since := s.now().Add(-windowDuration(window, defaultWindow))

	costRows, err := s.repo.LLMCost(ctx, since)
	if err != nil {
		return nil, fmt.Errorf("聚合 AI 成本失败: %w", err)
	}
	deliveryRows, err := s.repo.Delivery(ctx, since)
	if err != nil {
		return nil, fmt.Errorf("聚合送达率失败: %w", err)
	}
	funnelRows, err := s.repo.Funnel(ctx, since)
	if err != nil {
		return nil, fmt.Errorf("聚合转化漏斗失败: %w", err)
	}
	graceSeconds := int(BridgeOnlineGraceWindow(ctx).Seconds())
	accountRows, err := s.repo.ChannelAccounts(ctx, graceSeconds)
	if err != nil {
		return nil, fmt.Errorf("聚合渠道在线率失败: %w", err)
	}
	selectorRows, err := s.repo.SelectorHealthRows(ctx)
	if err != nil {
		return nil, fmt.Errorf("读取选择器健康失败: %w", err)
	}
	domainBan, err := s.repo.DomainBanSummary(ctx)
	if err != nil {
		return nil, fmt.Errorf("读取域名封禁失败: %w", err)
	}

	return &ObsDashSnapshot{
		GeneratedAt:   s.now(),
		Window:        normalizeWindow(window, defaultWindow),
		AICost:        buildAICostSection(costRows),
		Delivery:      buildDeliverySection(deliveryRows),
		Funnel:        buildFunnelSection(funnelRows),
		ChannelHealth: buildChannelHealthSection(accountRows, selectorRows, domainBan, s.onlineDegradedBelow(ctx)),
	}, nil
}

// ── 口径纯函数（可单测） ────────────────────────────────────────────────────

// defaultWindow 生效的默认窗口（参数中心 telemetry.dashboard_default_window_hours，兜底 7 天）。
//
// 越界一律回落兜底而不是夹：填 0 会被理解成「看板只看此刻」，那种读数还会显示成绿。
func (s *ObservabilityDashboardService) defaultWindow(ctx context.Context) time.Duration {
	hours := GlobalConfigParam().GetInt(ctx, "telemetry", "dashboard_default_window_hours", DashboardDefaultWindowHours)
	if hours <= 0 || hours > 8760 {
		return DashboardDefaultWindow
	}
	return time.Duration(hours) * time.Hour
}

// onlineDegradedBelow 生效的渠道在线率降级阈值（参数中心 telemetry.channel_online_degraded_below）。
func (s *ObservabilityDashboardService) onlineDegradedBelow(ctx context.Context) int {
	v := GlobalConfigParam().GetInt(ctx, "telemetry", "channel_online_degraded_below", ChannelOnlineDegradedBelow)
	if v <= 0 || v > 100 {
		return ChannelOnlineDegradedBelow
	}
	return v
}

// windowDuration 把 "24h"/"7d"/"30d" 换成时长。非法值回落 fallback（默认窗口）。
func windowDuration(window string, fallback time.Duration) time.Duration {
	switch window {
	case "24h":
		return 24 * time.Hour
	case "7d":
		return 7 * 24 * time.Hour
	case "30d":
		return 30 * 24 * time.Hour
	default:
		return fallback
	}
}

// normalizeWindow 返回对外展示用的窗口串（非法输入归一为 fallback 的可读形态）。
func normalizeWindow(window string, fallback time.Duration) string {
	switch window {
	case "24h", "7d", "30d":
		return window
	}
	if h := int(fallback.Hours()); h > 0 && h%24 == 0 {
		return fmt.Sprintf("%dd", h/24)
	}
	return fmt.Sprintf("%dh", int(fallback.Hours()))
}

// percent 算百分比（0–100，一位小数）。分母 0 返回 0 而不是 NaN——
// 看板把 NaN 渲染出来只会让运营以为接口坏了。
func percent(numerator, denominator int64) float64 {
	if denominator == 0 {
		return 0
	}
	rate := float64(numerator) * 100 / float64(denominator)
	return float64(int64(rate*10+0.5)) / 10
}

// DeliveryRate 送达率 = delivered / (delivered + failed) × 100。已了结行为 0 → 0。
func DeliveryRate(delivered, failed int64) float64 {
	return percent(delivered, delivered+failed)
}

// OnlineRate 在线率 = online / accounts × 100。账号 0 → 0。
func OnlineRate(online, accounts int64) float64 {
	return percent(online, accounts)
}

// FunnelConversion 本阶段 / 上一阶段 × 100。上一阶段 0 → 0。
func FunnelConversion(prev, curr int64) float64 {
	return percent(curr, prev)
}

// ClassifyChannelHealth 判定渠道健康状态。
//
// 顺序有意义（同 B1-a 判定的「先看样本再判失效」思路）：
//
//	· 未接入（账号 0）不是异常——不存在的渠道不该在运营面上亮红；
//	· 选择器全失效是「平台改版/封禁」的最强信号，必须最先判（banned），
//	  否则会与「部分失效」混成一档黄；
//	· 全离线独立一档：一台在线的都没有与「个别选择器坏了」是两种处置动作。
func ClassifyChannelHealth(accounts, online int64, worstSelector string, brokenFields int) string {
	return ClassifyChannelHealthWithThreshold(accounts, online, worstSelector, brokenFields, ChannelOnlineDegradedBelow)
}

// ClassifyChannelHealthWithThreshold 带在线率阈值的判定（阈值来自参数中心）。
//
// 在线率这一档是 007 §3.6 登记过、当时没接的那个：只有「全离线」一档时，
// 5 个账号掉 4 个会显示成 healthy——运营面上那和「一切正常」长得一模一样。
func ClassifyChannelHealthWithThreshold(accounts, online int64, worstSelector string, brokenFields, onlineDegradedBelow int) string {
	if accounts == 0 {
		return ChannelHealthStatusEmpty
	}
	if worstSelector == SelectorHealthStatusBroken {
		return ChannelHealthStatusBanned
	}
	if online == 0 {
		return ChannelHealthStatusOffline
	}
	if worstSelector == SelectorHealthStatusDegraded || brokenFields > 0 {
		return ChannelHealthStatusDegraded
	}
	if onlineDegradedBelow > 0 && onlineDegradedBelow <= 100 && OnlineRate(online, accounts) < float64(onlineDegradedBelow) {
		return ChannelHealthStatusDegraded
	}
	return ChannelHealthStatusHealthy
}

// ── 聚合组装（不可单测的部分只做「拼装」，判定全走上面的纯函数） ───────────

func buildAICostSection(rows []repository.ObsDashLLMCostRow) AICostSection {
	sec := AICostSection{ByProvider: []AICostByGroup{}, ByScenario: []AICostByGroup{}}
	byProvider := map[string]*AICostByGroup{}
	byScenario := map[string]*AICostByGroup{}
	var latencySum int64
	for _, r := range rows {
		sec.Calls += r.Calls
		sec.SuccessCalls += r.SuccessCalls
		sec.PromptTokens += r.PromptTokens
		sec.CompletionTokens += r.CompletionTokens
		sec.Cost += r.Cost
		latencySum += r.LatencyMs
		accGroup(byProvider, r.Provider, r)
		accGroup(byScenario, r.Scenario, r)
	}
	sec.TotalTokens = sec.PromptTokens + sec.CompletionTokens
	sec.AvgLatencyMs = avgInt(latencySum, sec.Calls)
	sec.ByProvider = sortedGroups(byProvider)
	sec.ByScenario = sortedGroups(byScenario)
	return sec
}

func buildDeliverySection(rows []repository.ObsDashDeliveryRow) DeliverySection {
	sec := DeliverySection{ByChannel: []ChannelDelivery{}}
	for _, r := range rows {
		sec.Delivered += r.Delivered
		sec.Failed += r.Failed
		sec.Inflight += r.Inflight
		sec.ByChannel = append(sec.ByChannel, ChannelDelivery{
			Channel:   r.Channel,
			Delivered: r.Delivered,
			Failed:    r.Failed,
			Inflight:  r.Inflight,
			Rate:      DeliveryRate(r.Delivered, r.Failed),
		})
	}
	sec.Rate = DeliveryRate(sec.Delivered, sec.Failed)
	return sec
}

// funnelNodes 漏斗阶段顺序：与 tracing.nodeOrder 保持字面一致。
var funnelNodes = []string{
	"ingest", "ai_dispatch", "outbound_enqueue", "inbox_sync", "downlink_fetch", "delivered_ack",
}

func buildFunnelSection(rows []repository.ObsDashFunnelStageRow) FunnelSection {
	counts := map[string]int64{}
	for _, r := range rows {
		counts[r.Node] = r.Cnt
	}
	sec := FunnelSection{Stages: make([]ObsDashFunnelStage, 0, len(funnelNodes))}
	var prev int64
	for i, node := range funnelNodes {
		cnt := counts[node]
		conv := 0.0
		if i > 0 {
			conv = FunnelConversion(prev, cnt)
		}
		sec.Stages = append(sec.Stages, ObsDashFunnelStage{
			Node:       node,
			NodeLabel:  dashboardNodeLabel(node),
			Count:      cnt,
			Conversion: conv,
		})
		prev = cnt
	}
	return sec
}

// dashboardNodeLabel 节点中文名。与 tracing.NodeLabel 同义，但本服务不 import tracing
// （monitor 包已有 NodeLabel，这里独立一份避免把看板口径绑到 tracing 包的展示层）。
func dashboardNodeLabel(node string) string {
	labels := map[string]string{
		"ingest":           "消息上报接入",
		"ai_dispatch":      "AI 处理编排",
		"outbound_enqueue": "AI 回复出站入队",
		"inbox_sync":       "收件箱同步",
		"downlink_fetch":   "下行出库拉取",
		"delivered_ack":    "送达确认",
	}
	if l, ok := labels[node]; ok {
		return l
	}
	return node
}

func buildChannelHealthSection(accountRows []repository.ObsDashChannelAccountRow,
	selectorRows []model.BridgeSelectorHealth, domainBan repository.ObsDashDomainBanSummaryRow,
	onlineDegradedBelow int) ChannelHealthSection {
	sec := ChannelHealthSection{
		Channels:     make([]ChannelHealthView, 0, len(accountRows)),
		DomainBanned: domainBan.Blacklisted,
		DomainTotal:  domainBan.Total,
	}
	// 选择器按渠道聚合：记最差判定 + broken 字段数（判定口径复用 B1-a）。
	worst := map[string]string{}
	broken := map[string]int{}
	for _, r := range selectorRows {
		status := ClassifySelectorHealth(r.Attempts, r.Hits, SelectorHealthDegradedBelow)
		if prev, ok := worst[r.Channel]; !ok || selectorStatusWorse(status, prev) {
			worst[r.Channel] = status
		}
		if status == SelectorHealthStatusBroken {
			broken[r.Channel]++
		}
	}
	for _, a := range accountRows {
		w := worst[a.Channel]
		if w == "" {
			w = "none"
		}
		sec.Channels = append(sec.Channels, ChannelHealthView{
			Channel:       a.Channel,
			Accounts:      a.Accounts,
			Online:        a.Online,
			OnlineRate:    OnlineRate(a.Online, a.Accounts),
			SelectorWorst: w,
			BrokenFields:  broken[a.Channel],
			Status:        ClassifyChannelHealthWithThreshold(a.Accounts, a.Online, w, broken[a.Channel], onlineDegradedBelow),
		})
	}
	return sec
}

// selectorStatusWorse 判定 a 是否比 b 更严重（用于取「最差」）。
// 严重度：broken > degraded > warming > ok > none。
func selectorStatusWorse(a, b string) bool {
	rank := map[string]int{
		SelectorHealthStatusBroken:   4,
		SelectorHealthStatusDegraded: 3,
		SelectorHealthStatusWarming:  2,
		SelectorHealthStatusOK:       1,
		"none":                       0,
	}
	return rank[a] > rank[b]
}

// ── 小工具 ──────────────────────────────────────────────────────────────────

func accGroup(m map[string]*AICostByGroup, key string, r repository.ObsDashLLMCostRow) {
	g, ok := m[key]
	if !ok {
		g = &AICostByGroup{Key: key}
		m[key] = g
	}
	g.Calls += r.Calls
	g.Tokens += r.PromptTokens + r.CompletionTokens
	g.Cost += r.Cost
	g.latencyMs += r.LatencyMs
}

func sortedGroups(m map[string]*AICostByGroup) []AICostByGroup {
	out := make([]AICostByGroup, 0, len(m))
	for _, g := range m {
		g.AvgLatencyMs = avgInt(g.latencyMs, g.Calls)
		out = append(out, *g)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out
}

func avgInt(total, n int64) int64 {
	if n == 0 {
		return 0
	}
	return total / n
}
