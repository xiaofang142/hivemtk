package service

import (
	"context"
	"sort"
	"strings"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/repository"
)

// OrderDraftEvent 订单草稿事件（-11）
type OrderDraftEvent struct {
	DraftID     string    `json:"draft_id"`
	CustomerID  string    `json:"customer_id"`
	OwnerID     string    `json:"owner_id"`
	ProductName string    `json:"product_name"`
	Amount      float64   `json:"amount"`
	Action      string    `json:"action"`
	Source      string    `json:"source"`
	Confidence  float64   `json:"confidence"`
	OccurredAt  time.Time `json:"occurred_at"`
}

// OrderEvent 订单事件
type OrderEvent struct {
	OrderID     string    `json:"order_id"`
	CustomerID  string    `json:"customer_id"`
	OwnerID     string    `json:"owner_id"`
	Amount      float64   `json:"amount"`
	ProductName string    `json:"product_name"`
	IsAIHandled bool      `json:"is_ai_handled"`
	OrderedAt   time.Time `json:"ordered_at"`
}

// FollowUpEvent 跟进事件
type FollowUpEvent struct {
	CustomerID string    `json:"customer_id"`
	OwnerID    string    `json:"owner_id"`
	Channel    string    `json:"channel"`
	IsAI       bool      `json:"is_ai"`
	Result     string    `json:"result"`
	OccurredAt time.Time `json:"occurred_at"`
}

// AIDealEvent AI 谈单事件
type AIDealEvent struct {
	CustomerID  string    `json:"customer_id"`
	OwnerID     string    `json:"owner_id"`
	Intent      string    `json:"intent"`
	Replied     bool      `json:"replied"`
	Transferred bool      `json:"transferred"`
	CostTokens  int       `json:"cost_tokens"`
	LatencyMs   int       `json:"latency_ms"`
	OccurredAt  time.Time `json:"occurred_at"`
}

// SalesProfile 销售档案
type SalesProfile struct {
	SalesID  string    `json:"sales_id"`
	Name     string    `json:"name"`
	Team     string    `json:"team"`
	JoinedAt time.Time `json:"joined_at"`
	Tags     []string  `json:"tags"`
}

// SalesEventStatsService 销售事件统计服务（DB 权威版销售仪表盘）
type SalesEventStatsService struct {
	repo repository.SalesEventRepository
}

// NewSalesEventStatsService 创建销售事件统计服务
func NewSalesEventStatsService() *SalesEventStatsService {
	return &SalesEventStatsService{repo: repository.NewSalesEventRepository()}
}

// NewSalesEventStatsServiceWithRepo 通过仓库创建（用于测试）
func NewSalesEventStatsServiceWithRepo(repo repository.SalesEventRepository) *SalesEventStatsService {
	return &SalesEventStatsService{repo: repo}
}

// RegisterSales 注册销售档案
func (s *SalesEventStatsService) RegisterSales(ctx context.Context, profile SalesProfile) {
	if profile.JoinedAt.IsZero() {
		profile.JoinedAt = time.Now()
	}
	ev := &model.SalesEvent{
		EventType:  model.SalesEventTypeSalesProfile,
		OwnerID:    profile.SalesID,
		SalesName:  profile.Name,
		Team:       profile.Team,
		Tags:       strings.Join(profile.Tags, ","),
		JoinedAt:   &profile.JoinedAt,
		OccurredAt: time.Now(),
	}
	_ = s.repo.Create(ctx, ev)
}

// RecordOrder 记录订单
func (s *SalesEventStatsService) RecordOrder(ctx context.Context, ev OrderEvent) {
	if ev.OrderedAt.IsZero() {
		ev.OrderedAt = time.Now()
	}
	_ = s.repo.Create(ctx, &model.SalesEvent{
		EventType:   model.SalesEventTypeOrder,
		OrderID:     ev.OrderID,
		CustomerID:  ev.CustomerID,
		OwnerID:     ev.OwnerID,
		ProductName: ev.ProductName,
		Amount:      ev.Amount,
		IsAIHandled: ev.IsAIHandled,
		OccurredAt:  ev.OrderedAt,
	})
}

// RecordFollowUp 记录跟进
func (s *SalesEventStatsService) RecordFollowUp(ctx context.Context, ev FollowUpEvent) {
	if ev.OccurredAt.IsZero() {
		ev.OccurredAt = time.Now()
	}
	_ = s.repo.Create(ctx, &model.SalesEvent{
		EventType:  model.SalesEventTypeFollowUp,
		CustomerID: ev.CustomerID,
		OwnerID:    ev.OwnerID,
		Channel:    ev.Channel,
		Result:     ev.Result,
		IsAI:       ev.IsAI,
		OccurredAt: ev.OccurredAt,
	})
}

// RecordAIDeal 记录 AI 谈单
func (s *SalesEventStatsService) RecordAIDeal(ctx context.Context, ev AIDealEvent) {
	if ev.OccurredAt.IsZero() {
		ev.OccurredAt = time.Now()
	}
	_ = s.repo.Create(ctx, &model.SalesEvent{
		EventType:   model.SalesEventTypeAIDeal,
		CustomerID:  ev.CustomerID,
		OwnerID:     ev.OwnerID,
		Intent:      ev.Intent,
		Replied:     ev.Replied,
		Transferred: ev.Transferred,
		CostTokens:  ev.CostTokens,
		LatencyMs:   ev.LatencyMs,
		OccurredAt:  ev.OccurredAt,
	})
}

// RecordOrderDraft 记录订单草稿事件（-11）
func (s *SalesEventStatsService) RecordOrderDraft(ctx context.Context, ev OrderDraftEvent) {
	if ev.OccurredAt.IsZero() {
		ev.OccurredAt = time.Now()
	}
	_ = s.repo.Create(ctx, &model.SalesEvent{
		EventType:   model.SalesEventTypeOrderDraft,
		DraftID:     ev.DraftID,
		CustomerID:  ev.CustomerID,
		OwnerID:     ev.OwnerID,
		ProductName: ev.ProductName,
		Amount:      ev.Amount,
		Action:      ev.Action,
		Source:      ev.Source,
		Confidence:  ev.Confidence,
		OccurredAt:  ev.OccurredAt,
	})
}

// Orders 查询订单事件
func (s *SalesEventStatsService) Orders(ctx context.Context, ownerID string, since time.Time) []OrderEvent {
	raw, _ := s.repo.ListByType(ctx, model.SalesEventTypeOrder, ownerID, sinceUnix(since))
	out := make([]OrderEvent, 0, len(raw))
	for _, e := range raw {
		out = append(out, OrderEvent{
			OrderID:     e.OrderID,
			CustomerID:  e.CustomerID,
			OwnerID:     e.OwnerID,
			Amount:      e.Amount,
			ProductName: e.ProductName,
			IsAIHandled: e.IsAIHandled,
			OrderedAt:   e.OccurredAt,
		})
	}
	return out
}

// FollowUps 查询跟进事件
func (s *SalesEventStatsService) FollowUps(ctx context.Context, ownerID string, since time.Time) []FollowUpEvent {
	raw, _ := s.repo.ListByType(ctx, model.SalesEventTypeFollowUp, ownerID, sinceUnix(since))
	out := make([]FollowUpEvent, 0, len(raw))
	for _, e := range raw {
		out = append(out, FollowUpEvent{
			CustomerID: e.CustomerID,
			OwnerID:    e.OwnerID,
			Channel:    e.Channel,
			IsAI:       e.IsAI,
			Result:     e.Result,
			OccurredAt: e.OccurredAt,
		})
	}
	return out
}

// AIDeals 查询 AI 谈单事件
func (s *SalesEventStatsService) AIDeals(ctx context.Context, ownerID string, since time.Time) []AIDealEvent {
	raw, _ := s.repo.ListByType(ctx, model.SalesEventTypeAIDeal, ownerID, sinceUnix(since))
	out := make([]AIDealEvent, 0, len(raw))
	for _, e := range raw {
		out = append(out, AIDealEvent{
			CustomerID:  e.CustomerID,
			OwnerID:     e.OwnerID,
			Intent:      e.Intent,
			Replied:     e.Replied,
			Transferred: e.Transferred,
			CostTokens:  e.CostTokens,
			LatencyMs:   e.LatencyMs,
			OccurredAt:  e.OccurredAt,
		})
	}
	return out
}

// OrderDrafts 查询草稿事件
func (s *SalesEventStatsService) OrderDrafts(ctx context.Context, ownerID string, since time.Time) []OrderDraftEvent {
	raw, _ := s.repo.ListByType(ctx, model.SalesEventTypeOrderDraft, ownerID, sinceUnix(since))
	out := make([]OrderDraftEvent, 0, len(raw))
	for _, e := range raw {
		out = append(out, OrderDraftEvent{
			DraftID:     e.DraftID,
			CustomerID:  e.CustomerID,
			OwnerID:     e.OwnerID,
			ProductName: e.ProductName,
			Amount:      e.Amount,
			Action:      e.Action,
			Source:      e.Source,
			Confidence:  e.Confidence,
			OccurredAt:  e.OccurredAt,
		})
	}
	return out
}

func sinceUnix(t time.Time) int64 {
	if t.IsZero() {
		return 0
	}
	return t.Unix()
}

// JourneyFunnel 基于客户旅程的转化漏斗
// 商业逻辑：每阶段的客户数 + 阶段间转化率 + 端到端转化率
type JourneyFunnel struct {
	Stages       []JourneyFunnelStage `json:"stages"`
	TotalEntered int                  `json:"total_entered"`
	TotalWon     int                  `json:"total_won"`
	EndToEndRate float64              `json:"end_to_end_rate"`
	AvgDwellDays float64              `json:"avg_dwell_days"`
	GeneratedAt  time.Time            `json:"generated_at"`
}

// JourneyFunnelStage 漏斗阶段
type JourneyFunnelStage struct {
	Stage        JourneyStage   `json:"stage"`
	Label        string         `json:"label"`
	Customers    int            `json:"customers"`
	StageRate    float64        `json:"stage_rate"`
	StepRate     float64        `json:"step_rate"`
	AvgDwellDays float64        `json:"avg_dwell_days"`
	OwnerLoad    map[string]int `json:"owner_load"`
}

// DraftStats 草稿统计
type DraftStats struct {
	OwnerID         string         `json:"owner_id"`
	Total           int            `json:"total"`
	ByAction        map[string]int `json:"by_action"`
	ByProduct       map[string]int `json:"by_product"`
	ConversionRate  float64        `json:"conversion_rate"`
	AvgAmount       float64        `json:"avg_amount"`
	ConfirmedAmount float64        `json:"confirmed_amount"`
	GeneratedAt     time.Time      `json:"generated_at"`
}

// GetDraftStats 获取草稿统计（按 ownerID + 动作聚合）
// 转化率 = 确认数 / 创建数（更准确反映草稿到成单的转化）
func (s *SalesEventStatsService) GetDraftStats(ctx context.Context, ownerID string, since time.Time) *DraftStats {
	events := s.OrderDrafts(ctx, ownerID, since)
	stats := &DraftStats{
		OwnerID:     ownerID,
		ByAction:    make(map[string]int),
		ByProduct:   make(map[string]int),
		GeneratedAt: time.Now(),
	}
	totalAmount := 0.0
	for _, ev := range events {
		stats.Total++
		stats.ByAction[ev.Action]++
		stats.ByProduct[ev.ProductName]++
		totalAmount += ev.Amount
		if ev.Action == "confirmed" {
			stats.ConfirmedAmount += ev.Amount
		}
	}
	created := stats.ByAction["created"]
	confirmed := stats.ByAction["confirmed"]
	if created > 0 {
		stats.ConversionRate = float64(confirmed) / float64(created) * 100
	}
	if stats.Total > 0 {
		stats.AvgAmount = totalAmount / float64(stats.Total)
	}
	return stats
}

// SalesPerformance 销售业绩
type SalesPerformance struct {
	SalesID        string    `json:"sales_id"`
	Name           string    `json:"name"`
	Team           string    `json:"team"`
	TotalOrders    int       `json:"total_orders"`
	TotalRevenue   float64   `json:"total_revenue"`
	TotalFollowUps int       `json:"total_follow_ups"`
	Conversions    int       `json:"conversions"`
	ConversionRate float64   `json:"conversion_rate"`
	AvgDealAmount  float64   `json:"avg_deal_amount"`
	ActiveDays     int       `json:"active_days"`
	Rank           int       `json:"rank"`
	GeneratedAt    time.Time `json:"generated_at"`
}

func (s *SalesEventStatsService) getProfile(ctx context.Context, salesID string) *SalesProfile {
	raw, err := s.repo.ListByType(ctx, model.SalesEventTypeSalesProfile, salesID, 0)
	if err != nil || len(raw) == 0 {
		return nil
	}
	e := raw[len(raw)-1]
	p := &SalesProfile{SalesID: e.OwnerID, Name: e.SalesName, Team: e.Team}
	if e.JoinedAt != nil {
		p.JoinedAt = *e.JoinedAt
	}
	if e.Tags != "" {
		p.Tags = strings.Split(e.Tags, ",")
	}
	return p
}

// GetSalesPerformance 获取销售个人业绩
func (s *SalesEventStatsService) GetSalesPerformance(ctx context.Context, salesID string, since time.Time) *SalesPerformance {
	perf := &SalesPerformance{
		SalesID:     salesID,
		GeneratedAt: time.Now(),
	}
	if profile := s.getProfile(ctx, salesID); profile != nil {
		if !profile.JoinedAt.IsZero() {
			perf.ActiveDays = int(time.Since(profile.JoinedAt).Hours() / 24)
		}
		perf.Name = profile.Name
		perf.Team = profile.Team
	}

	for _, o := range s.Orders(ctx, salesID, since) {
		perf.TotalOrders++
		perf.TotalRevenue += o.Amount
	}
	for _, f := range s.FollowUps(ctx, salesID, since) {
		perf.TotalFollowUps++
		if f.Result == "converted" {
			perf.Conversions++
		}
	}
	if perf.TotalOrders > 0 {
		perf.AvgDealAmount = perf.TotalRevenue / float64(perf.TotalOrders)
	}
	if perf.TotalFollowUps > 0 {
		perf.ConversionRate = float64(perf.Conversions) / float64(perf.TotalFollowUps) * 100
	}
	return perf
}

func (s *SalesEventStatsService) listSalesIDs(ctx context.Context, since time.Time) []string {
	seen := make(map[string]bool)
	ids := make([]string, 0)
	appendID := func(id string) {
		if id != "" && !seen[id] {
			seen[id] = true
			ids = append(ids, id)
		}
	}
	for _, o := range s.Orders(ctx, "", since) {
		appendID(o.OwnerID)
	}
	for _, f := range s.FollowUps(ctx, "", since) {
		appendID(f.OwnerID)
	}
	for _, p := range s.allProfiles(ctx) {
		appendID(p.SalesID)
	}
	return ids
}

func (s *SalesEventStatsService) allProfiles(ctx context.Context) []*SalesProfile {
	raw, err := s.repo.ListByType(ctx, model.SalesEventTypeSalesProfile, "", 0)
	if err != nil {
		return nil
	}
	latest := make(map[string]*model.SalesEvent, len(raw))
	order := make([]string, 0, len(raw))
	for _, e := range raw {
		if _, ok := latest[e.OwnerID]; !ok {
			order = append(order, e.OwnerID)
		}
		latest[e.OwnerID] = e
	}
	out := make([]*SalesProfile, 0, len(latest))
	for _, id := range order {
		e := latest[id]
		p := &SalesProfile{SalesID: e.OwnerID, Name: e.SalesName, Team: e.Team}
		if e.JoinedAt != nil {
			p.JoinedAt = *e.JoinedAt
		}
		if e.Tags != "" {
			p.Tags = strings.Split(e.Tags, ",")
		}
		out = append(out, p)
	}
	return out
}

// GetTeamRanking 获取销售团队排行榜
// 按成单金额排序，返回前 topN 名（topN<=0 返回全部）
func (s *SalesEventStatsService) GetTeamRanking(ctx context.Context, since time.Time, topN int) []*SalesPerformance {
	salesIDs := s.listSalesIDs(ctx, since)

	performances := make([]*SalesPerformance, 0, len(salesIDs))
	for _, sid := range salesIDs {
		performances = append(performances, s.GetSalesPerformance(ctx, sid, since))
	}
	sort.Slice(performances, func(i, j int) bool {
		return performances[i].TotalRevenue > performances[j].TotalRevenue
	})
	for i, p := range performances {
		p.Rank = i + 1
	}
	if topN > 0 && len(performances) > topN {
		performances = performances[:topN]
	}
	return performances
}

// AIProductivity AI 产能分析
type AIProductivity struct {
	TotalAIDeals        int       `json:"total_ai_deals"`
	AIReplied           int       `json:"ai_replied"`
	ReplyRate           float64   `json:"reply_rate"`
	TransferredCount    int       `json:"transferred_count"`
	TransferRate        float64   `json:"transfer_rate"`
	AISoloDeals         int       `json:"ai_solo_deals"`
	SoloDealAmount      float64   `json:"solo_deal_amount"`
	AISoloRate          float64   `json:"ai_solo_rate"`
	TotalCostTokens     int       `json:"total_cost_tokens"`
	AvgCostPerDeal      float64   `json:"avg_cost_per_deal"`
	AvgLatencyMs        float64   `json:"avg_latency_ms"`
	AIConversionRate    float64   `json:"ai_conversion_rate"`
	HumanConversionRate float64   `json:"human_conversion_rate"`
	ProductivityGain    float64   `json:"productivity_gain"`
	GeneratedAt         time.Time `json:"generated_at"`
}

// GetAIProductivity 获取 AI 产能
func (s *SalesEventStatsService) GetAIProductivity(ctx context.Context, since time.Time) *AIProductivity {
	prod := &AIProductivity{
		GeneratedAt: time.Now(),
	}
	humanFollowups := 0
	humanConverted := 0
	latencySum := 0
	for _, ev := range s.AIDeals(ctx, "", since) {
		prod.TotalAIDeals++
		if ev.Replied {
			prod.AIReplied++
		}
		if ev.Transferred {
			prod.TransferredCount++
		}
		prod.TotalCostTokens += ev.CostTokens
		latencySum += ev.LatencyMs
	}
	for _, o := range s.Orders(ctx, "", since) {
		if o.IsAIHandled {
			prod.AISoloDeals++
			prod.SoloDealAmount += o.Amount
		}
	}
	for _, f := range s.FollowUps(ctx, "", since) {
		if !f.IsAI {
			humanFollowups++
			if f.Result == "converted" {
				humanConverted++
			}
		}
	}
	if prod.TotalAIDeals > 0 {
		prod.ReplyRate = float64(prod.AIReplied) / float64(prod.TotalAIDeals) * 100
		prod.TransferRate = float64(prod.TransferredCount) / float64(prod.TotalAIDeals) * 100
		prod.AvgCostPerDeal = float64(prod.TotalCostTokens) / float64(prod.TotalAIDeals)
		prod.AvgLatencyMs = float64(latencySum) / float64(prod.TotalAIDeals)
	}
	if humanFollowups > 0 {
		prod.HumanConversionRate = float64(humanConverted) / float64(humanFollowups) * 100
	}
	if humanFollowups > 0 {
		prod.ProductivityGain = float64(prod.AISoloDeals) / float64(humanFollowups)
	}
	return prod
}

// ChampionProfile 销冠画像
type ChampionProfile struct {
	TopPerformers     []*SalesPerformance `json:"top_performers"`
	CommonTags        []string            `json:"common_tags"`
	AvgConversionRate float64             `json:"avg_conversion_rate"`
	AvgDealAmount     float64             `json:"avg_deal_amount"`
	RecommendedSOPs   []string            `json:"recommended_sops"`
	Insights          []string            `json:"insights"`
	GeneratedAt       time.Time           `json:"generated_at"`
}

// GetChampionProfile 获取销冠画像
func (s *SalesEventStatsService) GetChampionProfile(ctx context.Context, since time.Time) *ChampionProfile {
	all := s.GetTeamRanking(ctx, since, 0)
	if len(all) == 0 {
		return &ChampionProfile{
			GeneratedAt: time.Now(),
			Insights:    []string{"暂无销售数据"},
		}
	}
	cutoff := len(all) / 10
	if cutoff < 1 {
		cutoff = 1
	}
	if cutoff > 10 {
		cutoff = 10
	}
	topPerformers := all[:cutoff]
	profile := &ChampionProfile{
		TopPerformers: topPerformers,
		GeneratedAt:   time.Now(),
	}

	sumConv := 0.0
	sumDeal := 0.0
	tagCount := make(map[string]int)
	for _, p := range topPerformers {
		sumConv += p.ConversionRate
		sumDeal += p.AvgDealAmount
		if prof := s.getProfile(ctx, p.SalesID); prof != nil {
			for _, t := range prof.Tags {
				tagCount[t]++
			}
		}
	}
	if len(topPerformers) > 0 {
		profile.AvgConversionRate = sumConv / float64(len(topPerformers))
		profile.AvgDealAmount = sumDeal / float64(len(topPerformers))
	}
	half := len(topPerformers) / 2
	for tag, count := range tagCount {
		if count >= half {
			profile.CommonTags = append(profile.CommonTags, tag)
		}
	}
	sort.Strings(profile.CommonTags)

	if profile.AvgConversionRate > 30 {
		profile.Insights = append(profile.Insights, "销冠团队转化率 > 30%，远高于行业平均 5-10%")
	}
	if profile.AvgDealAmount > 0 {
		profile.Insights = append(profile.Insights, "销冠平均客单价显著高于团队，建议沉淀高客单 SOP")
	}
	if len(profile.CommonTags) > 0 {
		profile.Insights = append(profile.Insights, "销冠共性能力："+joinTags(profile.CommonTags))
	}
	profile.RecommendedSOPs = []string{
		"high_value_intro",
		"objection_handling",
		"closing_techniques",
	}
	return profile
}

func joinTags(tags []string) string {
	out := ""
	for i, t := range tags {
		if i > 0 {
			out += "、"
		}
		out += t
	}
	return out
}

// TeamDashboard 团队综合仪表盘
type TeamDashboard struct {
	Funnel         *JourneyFunnel      `json:"funnel"`
	TopSales       []*SalesPerformance `json:"top_sales"`
	AIProductivity *AIProductivity     `json:"ai_productivity"`
	Champion       *ChampionProfile    `json:"champion"`
	PeriodStart    time.Time           `json:"period_start"`
	PeriodEnd      time.Time           `json:"period_end"`
	TotalCustomers int                 `json:"total_customers"`
	GeneratedAt    time.Time           `json:"generated_at"`
}

// GetTeamDashboard 获取团队综合仪表盘（journey 用于构建转化漏斗）
//
// journey 允许为 nil：漏斗是从草稿竖借来的内存态，草稿竖未装配时仪表盘照样要出
// 其余三块（排行/AI 产能/销冠画像），少一块 ≠ 整个端点 500。
func (s *SalesEventStatsService) GetTeamDashboard(ctx context.Context, journey *CustomerJourneyService, since time.Time) *TeamDashboard {
	now := time.Now()
	if since.IsZero() {
		since = now.AddDate(0, 0, -30)
	}
	dash := &TeamDashboard{
		TopSales:       s.GetTeamRanking(ctx, since, 5),
		AIProductivity: s.GetAIProductivity(ctx, since),
		Champion:       s.GetChampionProfile(ctx, since),
		PeriodStart:    since,
		PeriodEnd:      now,
		GeneratedAt:    now,
	}
	if journey != nil {
		dash.Funnel = journey.Funnel(ctx)
		if dash.Funnel != nil {
			// 该字段此前永远是 0（只声明未赋值）。漏斗入口客户数是仪表盘里
			// 唯一同源的"客户总量"口径，直接对齐它，不另起第二条查询。
			dash.TotalCustomers = dash.Funnel.TotalEntered
		}
	}
	return dash
}
