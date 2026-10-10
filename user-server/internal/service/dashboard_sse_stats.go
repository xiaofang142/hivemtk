package service

import (
	"context"
	"math"
	"time"

	"gorm.io/gorm"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/utils/logger"
	"hivemtk-user/internal/repository"
)

// DashboardSnapshot 实时驾驶舱数据快照
type DashboardSnapshot struct {
	GeneratedAt time.Time `json:"generated_at"`

	OnlineSessions  int `json:"online_sessions"`
	AISessions      int `json:"ai_sessions"`
	HumanSessions   int `json:"human_sessions"`
	WaitingSessions int `json:"waiting_sessions"`
	InFlightReplies int `json:"in_flight_replies"`

	HumanizeDistribution HumanizeDistribution `json:"humanize_distribution"`

	Funnel *FunnelProgress `json:"funnel"`

	// MessageVolume 消息量小时聚合（D-3/X-8 双读：summary 优先，陈旧回源 raw）
	MessageVolume []MessageVolumePoint `json:"message_volume"`

	LLMMetrics *LLMRealTimeMetrics `json:"llm_metrics"`
}

// HumanizeDistribution AI 拟人度分布（最近 1h）
type HumanizeDistribution struct {
	WindowHours      int     `json:"window_hours"`
	TotalScored      int     `json:"total_scored"`
	LowScoreCount    int     `json:"low_score_count"`
	MediumScoreCount int     `json:"medium_score_count"`
	HighScoreCount   int     `json:"high_score_count"`
	AvgScore         float64 `json:"avg_score"`
	PassRate         float64 `json:"pass_rate"`
}

// FunnelProgress 转化漏斗进度
type FunnelProgress struct {
	Stages       []FunnelStage `json:"stages"`
	TotalEntered int           `json:"total_entered"`
	TotalWon     int           `json:"total_won"`
	OverallRate  float64       `json:"overall_rate"`
}

// FunnelStage 漏斗单个阶段
type FunnelStage struct {
	Name      string  `json:"name"`
	Code      string  `json:"code"`
	Count     int     `json:"count"`
	StageRate float64 `json:"stage_rate"`
	StepRate  float64 `json:"step_rate"`
}

// LLMRealTimeMetrics LLM 实时指标
type LLMRealTimeMetrics struct {
	ActiveProviders int     `json:"active_providers"`
	DownProviders   int     `json:"down_providers"`
	CircuitOpen     int     `json:"circuit_open"`
	AvgLatencyMs    float64 `json:"avg_latency_ms"`
	FailureRate     float64 `json:"failure_rate"`
}

// MessageVolumePoint 消息量小时聚合点（D-3/X-8 双读输出，维度=platform）
type MessageVolumePoint struct {
	HourBucket   time.Time `json:"hour_bucket"`
	Platform     string    `json:"platform"`
	SessionCount int64     `json:"session_count"`
	AICount      int64     `json:"ai_count"`
	HumanCount   int64     `json:"human_count"`
	MessageCount int64     `json:"message_count"`

	// Source 数据来源：summary（msg_hourly_summary 主路径）/ raw（message_hub 原生 SQL 兜底）
	Source string `json:"source"`
}

// summaryStaleThresholdDefault 汇总表新鲜度上限的代码兜底值，与参数中心
// `misc.summary_stale_threshold` 的 DefaultValue（600 秒）一致。
const summaryStaleThresholdDefault = 10 * time.Minute

// summaryStaleThresholdProvider 由装配层注入；未注入时回落默认值。
var summaryStaleThresholdProvider = func() time.Duration { return summaryStaleThresholdDefault }

// SetSummaryStaleThresholdProvider 注入读取函数（装配层调用；测试可注入桩）。nil 视为不注入。
func SetSummaryStaleThresholdProvider(fn func() time.Duration) {
	if fn != nil {
		summaryStaleThresholdProvider = fn
	}
}

// summaryStaleThreshold 取当前上限（非正数一律回落默认值）。
func summaryStaleThreshold() time.Duration {
	if d := summaryStaleThresholdProvider(); d > 0 {
		return d
	}
	return summaryStaleThresholdDefault
}

const (
	volumeSourceSummary = "summary"
	volumeSourceRaw     = "raw"
)

// DashboardStatsService 实时驾驶舱统计服务
//
// 五层架构归属: L2 服务层
// 职责：聚合 customer_sessions / humanize_scores / customer_journey 等表的实时指标，
// 供 controller 层 SSE 流式推送使用。controller 不再直接访问 db。
type DashboardStatsService interface {
	Available(ctx context.Context) bool
	CollectSessionStats(ctx context.Context, snap *DashboardSnapshot)
	CollectHumanizeDistribution(ctx context.Context) HumanizeDistribution
	CollectFunnel(ctx context.Context) *FunnelProgress
	// CollectMessageVolume 采集消息量指标（X-8 双读：summary 优先，陈旧回源 raw）
	CollectMessageVolume(ctx context.Context, windowHours int) []MessageVolumePoint
}

type dashboardStatsService struct {
	repo repository.DashboardStatsRepository
}

// NewDashboardStatsService 创建实时驾驶舱统计服务
//
// 注：保留 db *gorm.DB 入参以维持向后兼容（router / 调用方不改动），
// 内部在构造函数中实例化 repository，service struct 不直接持有 *gorm.DB。
func NewDashboardStatsService(db *gorm.DB) DashboardStatsService {
	return &dashboardStatsService{repo: repository.NewDashboardStatsRepository(db)}
}

func (s *dashboardStatsService) Available(ctx context.Context) bool {
	return s.repo != nil && s.repo.Available(ctx)
}

func (s *dashboardStatsService) CollectSessionStats(ctx context.Context, snap *DashboardSnapshot) {
	if s.repo == nil {
		return
	}
	onlineThreshold := time.Now().Add(-5 * time.Minute)

	if onlineCount, err := s.repo.CountSessionsByStatus(ctx, []model.SessionStatus{
		model.SessionStatusAIHandling,
		model.SessionStatusHumanHandling,
		model.SessionStatusWaiting,
	}, onlineThreshold); err == nil {
		snap.OnlineSessions = int(onlineCount)
	}

	if aiCount, err := s.repo.CountSessionsBySingleStatus(ctx, model.SessionStatusAIHandling); err == nil {
		snap.AISessions = int(aiCount)
	}

	if humanCount, err := s.repo.CountSessionsBySingleStatus(ctx, model.SessionStatusHumanHandling); err == nil {
		snap.HumanSessions = int(humanCount)
	}

	if waitingCount, err := s.repo.CountSessionsBySingleStatus(ctx, model.SessionStatusWaiting); err == nil {
		snap.WaitingSessions = int(waitingCount)
	}

	snap.InFlightReplies = snap.AISessions
}

func (s *dashboardStatsService) CollectHumanizeDistribution(ctx context.Context) HumanizeDistribution {
	dist := HumanizeDistribution{WindowHours: 1}
	if s.repo == nil {
		return dist
	}
	since := time.Now().Add(-1 * time.Hour)
	row, err := s.repo.QueryHumanizeDistribution(ctx, since)
	if err != nil || row == nil {
		if err != nil {
			logger.Ctx(ctx).Warn().Err(err).Msg("dashboard_sse: humanize query failed")
		}
		return dist
	}
	dist.TotalScored = int(row.Total)
	dist.LowScoreCount = int(row.Low)
	dist.MediumScoreCount = int(row.Mid)
	dist.HighScoreCount = int(row.High)
	dist.AvgScore = roundToFloat(row.Avg, 3)
	if row.Total > 0 {
		dist.PassRate = roundToFloat(float64(row.High)/float64(row.Total)*100, 2)
	}
	return dist
}

func (s *dashboardStatsService) CollectFunnel(ctx context.Context) *FunnelProgress {
	funnel := &FunnelProgress{
		Stages: []FunnelStage{},
	}
	if s.repo == nil {
		return funnel
	}

	rows, err := s.repo.QueryJourneyFunnel(ctx, time.Now().AddDate(0, 0, -30))
	if err != nil || len(rows) == 0 {
		return s.collectFunnelFromSessions(ctx)
	}

	stageOrder := []struct {
		Name string
		Code string
	}{
		{"陌生人", "stranger"},
		{"线索", "lead"},
		{"接触", "contact"},
		{"报价", "quote"},
		{"成单", "won"},
	}
	stageCount := make(map[string]int64)
	for _, r := range rows {
		stageCount[r.Stage] = r.Count
	}
	totalEntered := int64(0)
	totalWon := int64(0)
	prevCount := int64(0)
	for i, st := range stageOrder {
		cnt := stageCount[st.Code]
		if i == 0 {
			totalEntered = cnt
		}
		if st.Code == "won" {
			totalWon = cnt
		}
		stepRate := 0.0
		if i > 0 && prevCount > 0 {
			stepRate = float64(cnt) / float64(prevCount) * 100
		}
		stageRate := 0.0
		if totalEntered > 0 {
			stageRate = float64(cnt) / float64(totalEntered) * 100
		}
		funnel.Stages = append(funnel.Stages, FunnelStage{
			Name:      st.Name,
			Code:      st.Code,
			Count:     int(cnt),
			StageRate: roundToFloat(stageRate, 2),
			StepRate:  roundToFloat(stepRate, 2),
		})
		prevCount = cnt
	}
	funnel.TotalEntered = int(totalEntered)
	funnel.TotalWon = int(totalWon)
	if totalEntered > 0 {
		funnel.OverallRate = roundToFloat(float64(totalWon)/float64(totalEntered)*100, 2)
	}
	return funnel
}

func (s *dashboardStatsService) collectFunnelFromSessions(ctx context.Context) *FunnelProgress {
	funnel := &FunnelProgress{
		Stages: []FunnelStage{},
	}
	if s.repo == nil {
		return funnel
	}
	rows, _ := s.repo.QuerySessionFunnel(ctx, time.Now().AddDate(0, 0, -30))
	statusMap := make(map[string]int64)
	for _, r := range rows {
		statusMap[r.Status] = r.Count
	}
	stageMapping := []struct {
		Stage   FunnelStage
		Sources []string
	}{
		{FunnelStage{Name: "陌生人", Code: "stranger"}, []string{string(model.SessionStatusPending)}},
		{FunnelStage{Name: "接触", Code: "contact"}, []string{string(model.SessionStatusAIHandling), string(model.SessionStatusHumanHandling)}},
		{FunnelStage{Name: "等待", Code: "waiting"}, []string{string(model.SessionStatusWaiting)}},
		{FunnelStage{Name: "解决", Code: "resolved"}, []string{string(model.SessionStatusResolved)}},
		{FunnelStage{Name: "关闭", Code: "closed"}, []string{string(model.SessionStatusClosed)}},
	}
	var totalEntered int64
	prevCount := int64(0)
	for i, m := range stageMapping {
		cnt := int64(0)
		for _, src := range m.Sources {
			cnt += statusMap[src]
		}
		if i == 0 {
			totalEntered = cnt
		}
		stepRate := 0.0
		if i > 0 && prevCount > 0 {
			stepRate = float64(cnt) / float64(prevCount) * 100
		}
		stageRate := 0.0
		if totalEntered > 0 {
			stageRate = float64(cnt) / float64(totalEntered) * 100
		}
		stage := m.Stage
		stage.Count = int(cnt)
		stage.StageRate = roundToFloat(stageRate, 2)
		stage.StepRate = roundToFloat(stepRate, 2)
		funnel.Stages = append(funnel.Stages, stage)
		prevCount = cnt
	}
	funnel.TotalEntered = int(totalEntered)
	funnel.TotalWon = int(statusMap[string(model.SessionStatusResolved)])
	if totalEntered > 0 {
		funnel.OverallRate = roundToFloat(float64(funnel.TotalWon)/float64(totalEntered)*100, 2)
	}
	return funnel
}

func (s *dashboardStatsService) CollectMessageVolume(ctx context.Context, windowHours int) []MessageVolumePoint {
	if s.repo == nil || windowHours <= 0 {
		return []MessageVolumePoint{}
	}
	since := time.Now().Add(-time.Duration(windowHours) * time.Hour)

	if rows, err := s.repo.QueryMessageVolumeFromSummary(ctx, since); err == nil {
		stale := true
		if latest, lerr := s.repo.LatestSummaryBucket(ctx); lerr == nil && latest != nil {
			stale = time.Since(*latest) > summaryStaleThreshold()
		} else if lerr != nil {
			logger.Ctx(ctx).Warn().Err(lerr).Msg("dashboard_sse: latest summary bucket query failed")
		}
		if !stale {
			points := make([]MessageVolumePoint, 0, len(rows))
			for _, r := range rows {
				points = append(points, MessageVolumePoint{
					HourBucket:   r.HourBucket,
					Platform:     r.Platform,
					SessionCount: r.SessionCount,
					AICount:      r.AICount,
					HumanCount:   r.HumanCount,
					MessageCount: r.MessageCount,
					Source:       volumeSourceSummary,
				})
			}
			return points
		}
		logger.Ctx(ctx).Info().Msg("dashboard_sse: summary stale or empty, falling back to raw aggregation (X-8)")
	} else {
		logger.Ctx(ctx).Warn().Err(err).Msg("dashboard_sse: summary query failed, falling back to raw aggregation (X-8)")
	}

	rawRows, err := s.repo.QueryMessageVolumeRaw(ctx, since)
	if err != nil {
		logger.Ctx(ctx).Warn().Err(err).Msg("dashboard_sse: raw volume query failed")
		return []MessageVolumePoint{}
	}
	points := make([]MessageVolumePoint, 0, len(rawRows))
	for _, r := range rawRows {
		points = append(points, MessageVolumePoint{
			HourBucket:   r.HourBucket,
			Platform:     r.Platform,
			SessionCount: r.SessionCount,
			AICount:      r.AICount,
			HumanCount:   r.HumanCount,
			MessageCount: r.MessageCount,
			Source:       volumeSourceRaw,
		})
	}
	return points
}

func roundToFloat(v float64, n int) float64 {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return 0
	}
	mult := math.Pow(10, float64(n))
	return math.Round(v*mult) / mult
}
