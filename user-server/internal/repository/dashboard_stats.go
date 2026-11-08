package repository

import (
	"context"
	"time"

	"hivemtk-user/internal/model"

	"gorm.io/gorm"
)

// DashboardStatsRow 仪表盘会话统计聚合行
type DashboardStatsRow struct {
	OnlineSessions int64
	AISessions     int64
	HumanSessions  int64
	WaitingCount   int64
}

// HumanizeScoreRow 拟人度评分聚合行
type HumanizeScoreRow struct {
	Total int64
	Low   int64
	Mid   int64
	High  int64
	Avg   float64
}

// FunnelStageRow 漏斗阶段聚合行
type FunnelStageRow struct {
	Stage string
	Count int64
}

// FunnelStatusRow 漏斗兜底（按 session 状态聚合）行
type FunnelStatusRow struct {
	Status string
	Count  int64
}

// MessageVolumeRawRow message_hub 原生 SQL 小时聚合行（summary 双读之兜底路径）
type MessageVolumeRawRow struct {
	HourBucket   time.Time
	Platform     string
	SessionCount int64
	AICount      int64
	HumanCount   int64
	MessageCount int64
}

type DashboardStatsRepository interface {
	Available(ctx context.Context) bool
	CountSessionsByStatus(ctx context.Context, statuses []model.SessionStatus, onlineThreshold time.Time) (int64, error)
	CountSessionsBySingleStatus(ctx context.Context, status model.SessionStatus) (int64, error)
	QueryHumanizeDistribution(ctx context.Context, since time.Time) (*HumanizeScoreRow, error)
	QueryJourneyFunnel(ctx context.Context, since time.Time) ([]FunnelStageRow, error)
	QuerySessionFunnel(ctx context.Context, since time.Time) ([]FunnelStatusRow, error)

	// QueryMessageVolumeFromSummary 读 summary 表（主路径）
	QueryMessageVolumeFromSummary(ctx context.Context, since time.Time) ([]model.MsgHourlySummary, error)
	// LatestSummaryBucket summary 最近一次聚合刷新时间（MAX(updated_at)，陈旧判定）
	LatestSummaryBucket(ctx context.Context) (*time.Time, error)
	// QueryMessageVolumeRaw 回源 message_hub 原生 SQL 聚合（兜底路径）
	QueryMessageVolumeRaw(ctx context.Context, since time.Time) ([]MessageVolumeRawRow, error)
}

type dashboardStatsRepo struct {
	db          *gorm.DB
	summaryRepo MessageHubSummaryRepository
}

// NewDashboardStatsRepositoryDefault 默认构造（全局句柄）。
func NewDashboardStatsRepositoryDefault() DashboardStatsRepository {
	return NewDashboardStatsRepository(GetDB())
}

func NewDashboardStatsRepository(db *gorm.DB) DashboardStatsRepository {
	return &dashboardStatsRepo{db: db, summaryRepo: NewMessageHubSummaryRepository(db)}
}

func (r *dashboardStatsRepo) Available(ctx context.Context) bool {
	return r.db != nil
}

func (r *dashboardStatsRepo) CountSessionsByStatus(ctx context.Context, statuses []model.SessionStatus, onlineThreshold time.Time) (int64, error) {
	var n int64
	err := r.db.WithContext(ctx).Model(&model.CustomerSession{}).
		Where("status IN ?", statuses).
		Where("last_message_at > ? OR last_message_at IS NULL", onlineThreshold).
		Count(&n).Error
	if err != nil {
		return 0, err
	}
	return n, nil
}

func (r *dashboardStatsRepo) CountSessionsBySingleStatus(ctx context.Context, status model.SessionStatus) (int64, error) {
	var n int64
	err := r.db.WithContext(ctx).Model(&model.CustomerSession{}).
		Where("status = ?", status).
		Count(&n).Error
	if err != nil {
		return 0, err
	}
	return n, nil
}

func (r *dashboardStatsRepo) QueryHumanizeDistribution(ctx context.Context, since time.Time) (*HumanizeScoreRow, error) {
	var row HumanizeScoreRow
	err := r.db.WithContext(ctx).Table("humanize_scores").
		Select("COUNT(*) as total, "+
			"COUNT(*) FILTER (WHERE score < 0.7) as low, "+
			"COUNT(*) FILTER (WHERE score >= 0.7 AND score < 0.85) as mid, "+
			"COUNT(*) FILTER (WHERE score >= 0.85) as high, "+
			"COALESCE(AVG(score), 0) as avg").
		Where("created_at > ?", since).
		Scan(&row).Error
	if err != nil {
		return nil, err
	}
	return &row, nil
}

func (r *dashboardStatsRepo) QueryJourneyFunnel(ctx context.Context, since time.Time) ([]FunnelStageRow, error) {
	var rows []FunnelStageRow
	err := r.db.WithContext(ctx).Table("customer_journey").
		Select("stage, COUNT(*) as count").
		Where("created_at > ?", since).
		Group("stage").
		Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	return rows, nil
}

func (r *dashboardStatsRepo) QuerySessionFunnel(ctx context.Context, since time.Time) ([]FunnelStatusRow, error) {
	var rows []FunnelStatusRow
	err := r.db.WithContext(ctx).Table("customer_sessions").
		Select("status, COUNT(DISTINCT user_id) as count").
		Where("created_at > ?", since).
		Group("status").
		Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	return rows, nil
}

func (r *dashboardStatsRepo) QueryMessageVolumeFromSummary(ctx context.Context, since time.Time) ([]model.MsgHourlySummary, error) {
	if r.summaryRepo == nil {
		r.summaryRepo = NewMessageHubSummaryRepository(r.db)
	}
	return r.summaryRepo.QuerySince(ctx, since)
}

func (r *dashboardStatsRepo) LatestSummaryBucket(ctx context.Context) (*time.Time, error) {
	if r.summaryRepo == nil {
		r.summaryRepo = NewMessageHubSummaryRepository(r.db)
	}
	return r.summaryRepo.LatestUpdate(ctx)
}

func (r *dashboardStatsRepo) QueryMessageVolumeRaw(ctx context.Context, since time.Time) ([]MessageVolumeRawRow, error) {
	var rows []MessageVolumeRawRow
	// CS-04 观测口径与 summary 主路径（message_hub_summary_agg.go）同一条：桥接出站行
	// IsAIReply=true 是全桥接常量，转接公告（Extra.handler_type=human，SampleB 形态）
	// 不是 AI 应答，不计 ai_count、也不冒充 human_count。summary 兜底到本 raw 聚合时
	// 口径必须一致，否则驾驶舱曲线在主/兜底切换点跳变。handler_type=ai 与无标记的
	// 既有行照旧计 ai_count。
	err := r.db.WithContext(ctx).Table("message_hub").
		Select("date_trunc('hour', created_at) AS hour_bucket, platform, "+
			"COUNT(DISTINCT conversation_id) AS session_count, "+
			"COUNT(*) FILTER (WHERE is_ai_reply AND COALESCE(extra::jsonb->>'handler_type','') <> 'human') AS ai_count, "+
			"COUNT(*) FILTER (WHERE direction = 'outbound' AND NOT is_ai_reply) AS human_count, "+
			"COUNT(*) AS message_count").
		Where("created_at >= ?", since).
		Group("1, 2").
		Order("1 ASC").
		Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	return rows, nil
}
