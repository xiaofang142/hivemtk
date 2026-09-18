package service

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"
)

// WorkbenchTodo 待办事项（销售工作台首页核心）
type WorkbenchTodo struct {
	Type        string    `json:"type"`
	Priority    int       `json:"priority"`
	Title       string    `json:"title"`
	Description string    `json:"description"`
	TargetID    string    `json:"target_id"`
	TargetType  string    `json:"target_type"`
	CustomerID  string    `json:"customer_id"`
	DueAt       time.Time `json:"due_at"`
	CreatedAt   time.Time `json:"created_at"`
	URL         string    `json:"url"`
}

// WorkbenchToday 今日业绩
type WorkbenchToday struct {
	Date           time.Time `json:"date"`
	NewOrders      int       `json:"new_orders"`
	NewRevenue     float64   `json:"new_revenue"`
	FollowUps      int       `json:"follow_ups"`
	Conversions    int       `json:"conversions"`
	ConversionRate float64   `json:"conversion_rate"`
	AIDeals        int       `json:"ai_deals"`
}

// WorkbenchMonth 本月业绩
type WorkbenchMonth struct {
	Month          string  `json:"month"`
	TotalOrders    int     `json:"total_orders"`
	TotalRevenue   float64 `json:"total_revenue"`
	FollowUps      int     `json:"follow_ups"`
	Conversions    int     `json:"conversions"`
	ConversionRate float64 `json:"conversion_rate"`
	AvgDealAmount  float64 `json:"avg_deal_amount"`
	NewCustomers   int     `json:"new_customers"`
}

// WorkbenchKeyMetrics 关键指标
type WorkbenchKeyMetrics struct {
	RenewalRate      float64 `json:"renewal_rate"`
	AvgDealAmount    float64 `json:"avg_deal_amount"`
	RepurchaseRate   float64 `json:"repurchase_rate"`
	ChurnRate        float64 `json:"churn_rate"`
	AIAssistRate     float64 `json:"ai_assist_rate"`
	ActiveCustomers  int     `json:"active_customers"`
	DormantCustomers int     `json:"dormant_customers"`
}

// WorkbenchOverview 工作台首页综合数据
type WorkbenchOverview struct {
	SalesID     string               `json:"sales_id"`
	Name        string               `json:"name"`
	Team        string               `json:"team"`
	Todos       []*WorkbenchTodo     `json:"todos"`
	Today       *WorkbenchToday      `json:"today"`
	Month       *WorkbenchMonth      `json:"month"`
	AIProduct   *AIProductivity      `json:"ai_product"`
	Funnel      *JourneyFunnel       `json:"funnel"`
	Leaderboard []*SalesPerformance  `json:"leaderboard"`
	MyRank      int                  `json:"my_rank"`
	Metrics     *WorkbenchKeyMetrics `json:"metrics"`
	GeneratedAt time.Time            `json:"generated_at"`
}

// SalesWorkbenchService 销售工作台服务
type SalesWorkbenchService struct {
	mu sync.RWMutex

	stats    *SalesEventStatsService
	journey  *CustomerJourneyService
	followup *FollowUpService
	draft    *OrderDraftService
	tagger   *AITagger
}

// NewSalesWorkbenchService 创建工作台服务
func NewSalesWorkbenchService() *SalesWorkbenchService {
	return &SalesWorkbenchService{}
}

// SetStats 注入销售事件统计服务（H2：替代原 SalesDashboard）
func (s *SalesWorkbenchService) SetStats(ctx context.Context, svc *SalesEventStatsService) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.stats = svc
}

// SetJourney 注入客户旅程
func (s *SalesWorkbenchService) SetJourney(ctx context.Context, j *CustomerJourneyService) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.journey = j
}

// SetFollowUp 注入跟进服务
func (s *SalesWorkbenchService) SetFollowUp(ctx context.Context, f *FollowUpService) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.followup = f
}

// SetDraft 注入订单草稿服务
func (s *SalesWorkbenchService) SetDraft(ctx context.Context, d *OrderDraftService) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.draft = d
}

// SetTagger 注入标签服务
func (s *SalesWorkbenchService) SetTagger(ctx context.Context, t *AITagger) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tagger = t
}

// GetOverview 获取销售工作台首页综合数据（核心入口）
// 商业产品级：一次调用聚合 7 大模块，前端无需多次请求
func (s *SalesWorkbenchService) GetOverview(ctx context.Context, salesID string) *WorkbenchOverview {
	s.mu.RLock()
	stats := s.stats
	journey := s.journey
	followup := s.followup
	draft := s.draft
	tagger := s.tagger
	s.mu.RUnlock()

	now := time.Now()
	todayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	monthStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location())

	overview := &WorkbenchOverview{
		SalesID:     salesID,
		GeneratedAt: now,
		Todos:       []*WorkbenchTodo{},
	}

	overview.Todos = s.aggregateTodos(ctx, salesID, followup, draft)

	overview.Today = s.aggregateToday(ctx, salesID, stats, todayStart)

	overview.Month = s.aggregateMonth(ctx, salesID, stats, monthStart)

	if stats != nil {
		overview.AIProduct = stats.GetAIProductivity(ctx, monthStart)
	}

	if journey != nil {
		overview.Funnel = journey.Funnel(ctx)
	}

	if stats != nil {
		overview.Leaderboard = stats.GetTeamRanking(ctx, monthStart, 5)
		overview.MyRank = s.findMyRank(ctx, overview.Leaderboard, salesID)
	}

	overview.Metrics = s.aggregateMetrics(ctx, salesID, stats, journey, tagger, monthStart)

	if stats != nil {
		perf := stats.GetSalesPerformance(ctx, salesID, time.Time{})
		if perf != nil {
			overview.Name = perf.Name
			overview.Team = perf.Team
		}
	}

	return overview
}

func (s *SalesWorkbenchService) aggregateTodos(ctx context.Context, salesID string, followup *FollowUpService, draft *OrderDraftService) []*WorkbenchTodo {
	todos := make([]*WorkbenchTodo, 0)
	now := time.Now()

	if draft != nil {
		for _, d := range draft.ListPending(ctx, salesID, 0) {
			todos = append(todos, &WorkbenchTodo{
				Type:        "draft",
				Priority:    5,
				Title:       "待确认订单草稿",
				Description: fmt.Sprintf("%s x %d = ¥%.2f（置信度 %.0f%%）", d.ProductName, d.Quantity, d.TotalAmount, d.Confidence*100),
				TargetID:    d.ID,
				TargetType:  "order_draft",
				CustomerID:  d.CustomerID,
				DueAt:       d.ExpiresAt,
				CreatedAt:   d.CreatedAt,
				URL:         "/dashboard/drafts/" + d.ID,
			})
		}
	}

	if followup != nil {
		for _, r := range followup.ListPending(ctx, salesID, 0) {
			if r.DueAt.Before(todayStart(now)) || r.Status != "pending" {
				continue
			}
			priority := 4
			if r.Priority == PriorityUrgent {
				priority = 5
			} else if r.Priority == PriorityLow {
				priority = 3
			}
			todos = append(todos, &WorkbenchTodo{
				Type:        "followup",
				Priority:    priority,
				Title:       "跟进：" + r.Title,
				Description: r.Description,
				TargetID:    r.ID,
				TargetType:  "reminder",
				CustomerID:  r.CustomerID,
				DueAt:       r.DueAt,
				CreatedAt:   r.CreatedAt,
				URL:         "/dashboard/followups/" + r.ID,
			})
		}
		for _, r := range followup.ListOverdue(ctx, salesID) {
			todos = append(todos, &WorkbenchTodo{
				Type:        "followup",
				Priority:    5,
				Title:       "【逾期】跟进：" + r.Title,
				Description: "已逾期 " + now.Sub(r.DueAt).Round(time.Hour).String(),
				TargetID:    r.ID,
				TargetType:  "reminder",
				CustomerID:  r.CustomerID,
				DueAt:       r.DueAt,
				CreatedAt:   r.CreatedAt,
				URL:         "/dashboard/followups/" + r.ID,
			})
		}
	}

	sort.Slice(todos, func(i, j int) bool {
		if todos[i].Priority != todos[j].Priority {
			return todos[i].Priority > todos[j].Priority
		}
		return todos[i].DueAt.Before(todos[j].DueAt)
	})

	if len(todos) > 50 {
		todos = todos[:50]
	}
	return todos
}

func (s *SalesWorkbenchService) aggregateToday(ctx context.Context, salesID string, stats *SalesEventStatsService, todayStart time.Time) *WorkbenchToday {
	day := &WorkbenchToday{Date: todayStart}
	if stats == nil {
		return day
	}
	bg := ctx
	for _, o := range stats.Orders(bg, salesID, todayStart) {
		day.NewOrders++
		day.NewRevenue += o.Amount
	}
	for _, f := range stats.FollowUps(bg, salesID, todayStart) {
		day.FollowUps++
		if f.Result == "converted" {
			day.Conversions++
		}
	}
	day.AIDeals = len(stats.AIDeals(bg, salesID, todayStart))
	if day.FollowUps > 0 {
		day.ConversionRate = float64(day.Conversions) / float64(day.FollowUps) * 100
	}
	return day
}

func (s *SalesWorkbenchService) aggregateMonth(ctx context.Context, salesID string, stats *SalesEventStatsService, monthStart time.Time) *WorkbenchMonth {
	month := &WorkbenchMonth{
		Month: monthStart.Format("2006-01"),
	}
	if stats == nil {
		return month
	}
	bg := ctx
	for _, o := range stats.Orders(bg, salesID, monthStart) {
		month.TotalOrders++
		month.TotalRevenue += o.Amount
	}
	for _, f := range stats.FollowUps(bg, salesID, monthStart) {
		month.FollowUps++
		if f.Result == "converted" {
			month.Conversions++
		}
	}
	if month.TotalOrders > 0 {
		month.AvgDealAmount = month.TotalRevenue / float64(month.TotalOrders)
	}
	if month.FollowUps > 0 {
		month.ConversionRate = float64(month.Conversions) / float64(month.FollowUps) * 100
	}
	return month
}

func (s *SalesWorkbenchService) aggregateMetrics(ctx context.Context, salesID string, stats *SalesEventStatsService, journey *CustomerJourneyService, tagger *AITagger, since time.Time) *WorkbenchKeyMetrics {
	metrics := &WorkbenchKeyMetrics{}
	if stats == nil {
		return metrics
	}
	orders := stats.Orders(ctx, salesID, since)
	totalOrders := 0
	repurchaseOrders := 0
	uniqueCustomers := make(map[string]bool)
	for _, o := range orders {
		totalOrders++
		uniqueCustomers[o.CustomerID] = true
		count := 0
		for _, oo := range orders {
			if oo.CustomerID == o.CustomerID && oo.OwnerID == salesID {
				count++
			}
		}
		if count >= 2 {
			repurchaseOrders++
		}
	}
	if totalOrders > 0 {
		metrics.RepurchaseRate = float64(repurchaseOrders) / float64(totalOrders) * 100
		metrics.AvgDealAmount = 0
	}
	metrics.ActiveCustomers = len(uniqueCustomers)

	if journey != nil {
		journey.mu.RLock()
		dormant := 0
		for _, st := range journey.states {
			if st.CurrentStage == StageSleeping {
				dormant++
			}
		}
		journey.mu.RUnlock()
		metrics.DormantCustomers = dormant
	}

	aiCount := 0
	totalCount := 0
	for _, f := range stats.FollowUps(ctx, salesID, since) {
		totalCount++
		if f.IsAI {
			aiCount++
		}
	}
	if totalCount > 0 {
		metrics.AIAssistRate = float64(aiCount) / float64(totalCount) * 100
	}
	return metrics
}

func (s *SalesWorkbenchService) findMyRank(ctx context.Context, leaderboard []*SalesPerformance, salesID string) int {
	for _, p := range leaderboard {
		if p.SalesID == salesID {
			return p.Rank
		}
	}
	return 0
}

func todayStart(now time.Time) time.Time {
	return time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
}

// GetTodosOnly 仅查询待办（前端轮询使用）
func (s *SalesWorkbenchService) GetTodosOnly(ctx context.Context, salesID string) []*WorkbenchTodo {
	s.mu.RLock()
	followup := s.followup
	draft := s.draft
	s.mu.RUnlock()
	return s.aggregateTodos(ctx, salesID, followup, draft)
}

// GetQuickActions 销售最常用的快捷入口
// 商业产品级：销售工作台首页"快链"区
func (s *SalesWorkbenchService) GetQuickActions(ctx context.Context, salesID string) []*QuickAction {
	return []*QuickAction{
		{ID: "new_draft", Title: "新建订单", Icon: "edit", URL: "/dashboard/drafts/new", Badge: 0},
		{ID: "ai_assist", Title: "AI 接管客户", Icon: "robot", URL: "/dashboard/ai/transfer", Badge: 0},
		{ID: "followup_today", Title: "今日跟进", Icon: "calendar", URL: "/dashboard/followups/today", Badge: 0},
		{ID: "lead_pool", Title: "线索池", Icon: "inbox", URL: "/dashboard/leads", Badge: 0},
		{ID: "dashboard", Title: "我的业绩", Icon: "chart", URL: "/dashboard/sales/" + salesID, Badge: 0},
	}
}

// QuickAction 快捷入口
type QuickAction struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	Icon  string `json:"icon"`
	URL   string `json:"url"`
	Badge int    `json:"badge"`
}
