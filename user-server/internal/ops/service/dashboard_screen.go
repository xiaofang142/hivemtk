package service

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"hivemtk-user/internal/ops/model"
	opsrepo "hivemtk-user/internal/ops/repository"
	"time"
)

// DashboardScreenService 数据大屏服务
type DashboardScreenService struct {
	screenRepo *opsrepo.DashboardScreenRepository
	widgetRepo *opsrepo.DashboardWidgetRepository
}

// NewDashboardScreenService 创建数据大屏服务实例
func NewDashboardScreenService() *DashboardScreenService {
	return &DashboardScreenService{
		screenRepo: opsrepo.NewDashboardScreenRepository(),
		widgetRepo: opsrepo.NewDashboardWidgetRepository(),
	}
}

// CreateScreenRequest 创建大屏请求
type CreateScreenRequest struct {
	Name     string         `json:"name" binding:"required"`
	Layout   map[string]any `json:"layout"`
	Theme    string         `json:"theme"`
	IsPublic bool           `json:"is_public"`
}

// CreateScreen 创建大屏
func (s *DashboardScreenService) CreateScreen(ctx context.Context, createdBy uint, req *CreateScreenRequest) (*model.DashboardScreen, error) {
	layout, _ := json.Marshal(req.Layout)

	screen := &model.DashboardScreen{
		Name:      req.Name,
		Code:      generateScreenCode(),
		Layout:    string(layout),
		Theme:     req.Theme,
		IsPublic:  req.IsPublic,
		CreatedBy: createdBy,
	}

	if screen.Theme == "" {
		screen.Theme = "dark"
	}

	if err := s.screenRepo.Create(ctx, screen); err != nil {
		return nil, err
	}

	return screen, nil
}

func generateScreenCode() string {
	timestamp := time.Now().Format("20060102150405")
	randomBytes := make([]byte, 4)
	if _, err := rand.Read(randomBytes); err != nil {
		return fmt.Sprintf("screen_%s_%08x", timestamp, time.Now().UnixNano()&0xFFFFFFFF)
	}
	return "screen_" + timestamp + "_" + hex.EncodeToString(randomBytes)
}

// CreateScreenWithWidgets 建屏并落 widgets（大屏模板 clone 的复用入口）。
// 先建屏再覆盖式写 widgets；widgets 为空时等价 CreateScreen。
func (s *DashboardScreenService) CreateScreenWithWidgets(ctx context.Context, createdBy uint, req *CreateScreenRequest, widgets []WidgetConfig) (*model.DashboardScreen, error) {
	screen, err := s.CreateScreen(ctx, createdBy, req)
	if err != nil {
		return nil, err
	}
	if len(widgets) > 0 {
		if err := s.updateWidgets(ctx, screen.ID, widgets); err != nil {
			return nil, err
		}
	}
	return screen, nil
}

// GetScreenList 获取大屏列表
func (s *DashboardScreenService) GetScreenList(ctx context.Context, page, pageSize int) ([]*model.DashboardScreen, int64, error) {
	return s.screenRepo.GetAll(ctx, page, pageSize)
}

// GetScreenByID 获取大屏详情
func (s *DashboardScreenService) GetScreenByID(ctx context.Context, id uint) (*model.DashboardScreen, error) {
	screen, err := s.screenRepo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	return screen, nil
}

// UpdateScreenRequest 更新大屏请求
type UpdateScreenRequest struct {
	Name     string         `json:"name"`
	Layout   map[string]any `json:"layout"`
	Widgets  []WidgetConfig `json:"widgets"`
	Theme    string         `json:"theme"`
	IsPublic bool           `json:"is_public"`
}

// WidgetConfig Widget 配置
type WidgetConfig struct {
	WidgetType string         `json:"widget_type"`
	Title      string         `json:"title"`
	Config     map[string]any `json:"config"`
	DataSource string         `json:"data_source"`
	X          int            `json:"x"`
	Y          int            `json:"y"`
	Width      int            `json:"width"`
	Height     int            `json:"height"`
}

// UpdateScreen 更新大屏
func (s *DashboardScreenService) UpdateScreen(ctx context.Context, id uint, req *UpdateScreenRequest) (*model.DashboardScreen, error) {
	screen, err := s.screenRepo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}

	if req.Name != "" {
		screen.Name = req.Name
	}
	if req.Layout != nil {
		layout, _ := json.Marshal(req.Layout)
		screen.Layout = string(layout)
	}
	if req.Theme != "" {
		screen.Theme = req.Theme
	}
	screen.IsPublic = req.IsPublic

	if err := s.screenRepo.Update(ctx, screen); err != nil {
		return nil, err
	}

	if req.Widgets != nil {
		if err := s.updateWidgets(ctx, screen.ID, req.Widgets); err != nil {
			return nil, err
		}
	}

	return screen, nil
}

func (s *DashboardScreenService) updateWidgets(ctx context.Context, screenID uint, widgets []WidgetConfig) error {
	if err := s.widgetRepo.DeleteByScreenID(ctx, screenID); err != nil {
		return err
	}

	for i, w := range widgets {
		config, _ := json.Marshal(w.Config)
		widget := &model.DashboardWidget{
			ScreenID:   screenID,
			WidgetType: w.WidgetType,
			Title:      w.Title,
			Config:     string(config),
			DataSource: w.DataSource,
			X:          w.X,
			Y:          w.Y,
			Width:      w.Width,
			Height:     w.Height,
			SortOrder:  i,
		}
		if err := s.widgetRepo.Create(ctx, widget); err != nil {
			return err
		}
	}

	return nil
}

// DeleteScreen 删除大屏
func (s *DashboardScreenService) DeleteScreen(ctx context.Context, id uint) error {
	screen, err := s.screenRepo.GetByID(ctx, id)
	if err != nil {
		return err
	}
	_ = screen

	if err := s.widgetRepo.DeleteByScreenID(ctx, id); err != nil {
		return err
	}

	return s.screenRepo.Delete(ctx, id)
}

// GetPublicScreenCtx 公开访问大屏（匿名）：租户由 code 自证，
// 后续读取与浏览量自增都钉死在该租户内。
func (s *DashboardScreenService) GetPublicScreenCtx(ctx context.Context, code string) (*model.DashboardScreen, error) {
	return s.screenRepo.GetPublicByCodeCtx(ctx, code)
}

// GetScreenWidgetsCtx 公开访问路径的 widgets 读取
func (s *DashboardScreenService) GetScreenWidgetsCtx(ctx context.Context, screenID uint) ([]*model.DashboardWidget, error) {
	return s.widgetRepo.GetByScreenIDCtx(ctx, screenID)
}

// DashboardAggregate 大屏聚合数据（与前端 dashboardScreen/List.vue 契约对齐）
type DashboardAggregate struct {
	Kpis       []DashboardKpiItem  `json:"kpis"`
	Trend      DashboardTrend      `json:"trend"`
	Channels   []NameValue         `json:"channels"`
	Sources    []NameValue         `json:"sources"`
	Funnel     []NameValue         `json:"funnel"`
	Regions    []NameValue         `json:"regions"`
	Conversion DashboardConversion `json:"conversion"`
}

// DashboardKpiItem KPI 卡片
type DashboardKpiItem struct {
	Label string `json:"label"`
	Value any    `json:"value"`
	Color string `json:"color"`
	Trend int    `json:"trend"`
}

// NameValue 名称-数值对（渠道/来源/地区/漏斗）
type NameValue struct {
	Name  string `json:"name"`
	Value int64  `json:"value"`
}

// DashboardTrend 近 30 天趋势
type DashboardTrend struct {
	Dates       []string `json:"dates"`
	Visits      []int64  `json:"visits"`
	Clues       []int64  `json:"clues"`
	Conversions []int64  `json:"conversions"`
}

// DashboardConversion 转化率对比（本周 vs 上周）
type DashboardConversion struct {
	Dates    []string `json:"dates"`
	ThisWeek []int64  `json:"thisWeek"`
	LastWeek []int64  `json:"lastWeek"`
}

func pct(num, den int64) int64 {
	if den <= 0 {
		return 0
	}
	return int64(float64(num) / float64(den) * 100)
}

// RealtimeActivity 实时活动项
type RealtimeActivity struct {
	Type      string    `json:"type"`
	Title     string    `json:"title"`
	UserName  string    `json:"user_name"`
	CreatedAt time.Time `json:"created_at"`
}

// AggregateDashboardData 聚合大屏数据（Service 层）
// isAdmin=true  → 返回全租户数据；
// isAdmin=false → 按 data_scope 过滤（私域单租户当前无实际隔离，预留接口）。
// 错误不再直出吞掉，而是向上返回，让 Controller 包装成 500 响应。
//
// AV-04c 批次5 刀1：查询组下沉 DashboardScreenRepository（同谓词同投影同排序），
// service 不再持 gorm 句柄；原 scope 闭包是恒等函数（两分支同实现），随下沉删除。
func (s *DashboardScreenService) AggregateDashboardData(ctx context.Context, isAdmin bool) (*DashboardAggregate, error) {
	now := time.Now()
	loc := now.Location()
	todayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc)
	yesterdayStart := todayStart.AddDate(0, 0, -1)
	thirtyDaysAgo := todayStart.AddDate(0, 0, -30)
	thisWeekStart := todayStart.AddDate(0, 0, -int(todayStart.Weekday()))
	lastWeekStart := thisWeekStart.AddDate(0, 0, -7)

	agg := &DashboardAggregate{}

	totalClues, err := s.screenRepo.CountClues(ctx)
	if err != nil {
		return nil, fmt.Errorf("统计总线索失败: %w", err)
	}
	todayClues, err := s.screenRepo.CountCluesCreatedSince(ctx, todayStart)
	if err != nil {
		return nil, fmt.Errorf("统计今日线索失败: %w", err)
	}
	yesterdayClues, err := s.screenRepo.CountCluesCreatedBetween(ctx, yesterdayStart, todayStart)
	if err != nil {
		return nil, fmt.Errorf("统计昨日线索失败: %w", err)
	}
	verifiedClues, err := s.screenRepo.CountVerifiedClues(ctx)
	if err != nil {
		return nil, fmt.Errorf("统计已验证线索失败: %w", err)
	}

	totalCustomers, err := s.screenRepo.CountCustomersIfTableExists(ctx)
	if err != nil {
		return nil, fmt.Errorf("统计客户总数失败: %w", err)
	}
	totalOrders, err := s.screenRepo.CountOrdersIfTableExists(ctx)
	if err != nil {
		return nil, fmt.Errorf("统计订单总数失败: %w", err)
	}

	clueDays, err := s.screenRepo.ClueTrendDaily(ctx, thirtyDaysAgo)
	if err != nil {
		return nil, fmt.Errorf("线索趋势查询失败: %w", err)
	}
	orderDays, err := s.screenRepo.OrderTrendDaily(ctx, thirtyDaysAgo)
	if err != nil {
		return nil, fmt.Errorf("成单趋势查询失败: %w", err)
	}
	clueMap := make(map[string]int64, len(clueDays))
	orderMap := make(map[string]int64, len(orderDays))
	for _, r := range clueDays {
		clueMap[r.Day] = r.Cnt
	}
	for _, r := range orderDays {
		orderMap[r.Day] = r.Cnt
	}
	dates := make([]string, 0, 30)
	for i := 0; i < 30; i++ {
		dates = append(dates, thirtyDaysAgo.AddDate(0, 0, i).Format("01-02"))
	}
	agg.Trend.Dates = dates
	agg.Trend.Clues = make([]int64, len(dates))
	agg.Trend.Conversions = make([]int64, len(dates))
	agg.Trend.Visits = make([]int64, len(dates))
	for i, key := range dates {
		agg.Trend.Clues[i] = clueMap[key]
		agg.Trend.Conversions[i] = orderMap[key]
		agg.Trend.Visits[i] = clueMap[key]
	}

	// GROUP BY 写表达式本身，不写序号 1：GORM 的 Group 走 WriteQuoted，
	// 会把 "1" 引用成标识符 "1"（PG 报 column "1" does not exist）。
	// 该约束随查询下沉仓储（ClueChannelDistribution / ClueRegionDistribution）。
	chRows, err := s.screenRepo.ClueChannelDistribution(ctx)
	if err != nil {
		return nil, fmt.Errorf("渠道分布查询失败: %w", err)
	}
	for _, r := range chRows {
		agg.Channels = append(agg.Channels, NameValue(r))
	}
	for i, r := range agg.Channels {
		if i >= 5 {
			break
		}
		agg.Sources = append(agg.Sources, r)
	}

	regRows, err := s.screenRepo.ClueRegionDistribution(ctx)
	if err != nil {
		return nil, fmt.Errorf("地区分布查询失败: %w", err)
	}
	for _, r := range regRows {
		agg.Regions = append(agg.Regions, NameValue(r))
	}

	agg.Funnel = []NameValue{
		{Name: "线索", Value: totalClues},
		{Name: "客户", Value: totalCustomers},
		{Name: "成单", Value: totalOrders},
	}

	cluesTW, err := s.screenRepo.CountCluesCreatedSince(ctx, thisWeekStart)
	if err != nil {
		return nil, fmt.Errorf("本周线索统计失败: %w", err)
	}
	cluesLW, err := s.screenRepo.CountCluesCreatedBetween(ctx, lastWeekStart, thisWeekStart)
	if err != nil {
		return nil, fmt.Errorf("上周线索统计失败: %w", err)
	}
	ordersTW, err := s.screenRepo.CountOrdersCreatedSinceIfTableExists(ctx, thisWeekStart)
	if err != nil {
		return nil, fmt.Errorf("本周成单统计失败: %w", err)
	}
	ordersLW, err := s.screenRepo.CountOrdersCreatedBetweenIfTableExists(ctx, lastWeekStart, thisWeekStart)
	if err != nil {
		return nil, fmt.Errorf("上周成单统计失败: %w", err)
	}
	agg.Conversion.Dates = []string{"本周", "上周"}
	agg.Conversion.ThisWeek = []int64{pct(ordersTW, cluesTW)}
	agg.Conversion.LastWeek = []int64{pct(ordersLW, cluesLW)}

	trendToday := 0
	if yesterdayClues > 0 {
		trendToday = int(float64(todayClues-yesterdayClues) / float64(yesterdayClues) * 100)
	} else if todayClues > 0 {
		trendToday = 100
	}
	agg.Kpis = []DashboardKpiItem{
		{Label: "总线索", Value: totalClues, Color: "linear-gradient(135deg,#667eea,#764ba2)", Trend: trendToday},
		{Label: "今日线索", Value: todayClues, Color: "linear-gradient(135deg,#f093fb,#f5576c)", Trend: trendToday},
		{Label: "总客户", Value: totalCustomers, Color: "linear-gradient(135deg,#4facfe,#00f2fe)"},
		{Label: "总成单", Value: totalOrders, Color: "linear-gradient(135deg,#43e97b,#38f9d7)"},
		{Label: "已验证线索", Value: verifiedClues, Color: "linear-gradient(135deg,#fa709a,#fee140)"},
		{Label: "验证率", Value: pct(verifiedClues, totalClues), Color: "linear-gradient(135deg,#30cfd0,#330867)"},
	}

	return agg, nil
}

// FetchRealtimeActivities 获取实时活动（Service 层）
func (s *DashboardScreenService) FetchRealtimeActivities(ctx context.Context, limit int) ([]RealtimeActivity, error) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	activities := make([]RealtimeActivity, 0)

	// 原实现查询失败按空列表降级（err == nil 才填充），语义保留。
	clues, err := s.screenRepo.ListRecentClues(ctx, limit)
	if err == nil {
		for _, c := range clues {
			activities = append(activities, RealtimeActivity{
				Type:      "clue",
				Title:     fmt.Sprintf("新线索：%s", c.Name),
				UserName:  c.Account,
				CreatedAt: time.Unix(c.CreateTime, 0),
			})
		}
	}

	if len(activities) > limit {
		activities = activities[:limit]
	}

	return activities, nil
}
