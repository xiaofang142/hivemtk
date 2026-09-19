package repository

import (
	"context"
	"fmt"

	"hivemtk-user/internal/model"

	"gorm.io/gorm"
)

// XiaohongshuCardStatsTempStat 按时间分组的临时统计结果（repository 层本地类型）
type XiaohongshuCardStatsTempStat struct {
	Date   string
	Action string
	Count  int
}

// XiaohongshuCardStatsRepository 小红书卡片统计仓库接口
type XiaohongshuCardStatsRepository interface {
	GetCardByID(ctx context.Context, cardID uint) (*model.XiaohongshuCard, error)
	CountCardViews(ctx context.Context, cardID uint) (int64, error)
	GetCardDailyStats(ctx context.Context, cardID uint, startDate, endDate, groupBy string) ([]XiaohongshuCardStatsTempStat, error)
	GetRecentActivitiesByCard(ctx context.Context, cardID uint, limit int) ([]model.XiaohongshuCardActivity, error)
	CountTotalCards(ctx context.Context) (int64, error)
	CountActiveCards(ctx context.Context) (int64, error)
	CountTotalViews(ctx context.Context) (int64, error)
	GetTopCards(ctx context.Context, limit int) ([]model.XiaohongshuCard, error)
	GetOverallDailyStats(ctx context.Context, startDate, endDate, groupBy string) ([]XiaohongshuCardStatsTempStat, error)
	GetRecentActivities(ctx context.Context, limit int) ([]model.XiaohongshuCardActivity, error)
	CreateActivity(ctx context.Context, activity *model.XiaohongshuCardActivity) error
	IncrementCardViewCount(ctx context.Context, cardID uint) error
}

type xiaohongshuCardStatsRepository struct {
	db *gorm.DB
}

// NewXiaohongshuCardStatsRepository 创建小红书卡片统计仓库
func NewXiaohongshuCardStatsRepository(db *gorm.DB) XiaohongshuCardStatsRepository {
	return &xiaohongshuCardStatsRepository{db: db}
}

func (r *xiaohongshuCardStatsRepository) GetCardByID(ctx context.Context, cardID uint) (*model.XiaohongshuCard, error) {
	var card model.XiaohongshuCard
	if err := r.db.WithContext(ctx).First(&card, cardID).Error; err != nil {
		return nil, err
	}
	return &card, nil
}

func (r *xiaohongshuCardStatsRepository) CountCardViews(ctx context.Context, cardID uint) (int64, error) {
	var views int64
	err := r.db.WithContext(ctx).Model(&model.XiaohongshuCardActivity{}).
		Where("card_id = ? AND activity_type = ?", cardID, "view").
		Count(&views).Error
	return views, err
}

// applyXiaohongshuStatsGroupBy 按 groupBy 选定分桶表达式，并把它**原样**同时放进 SELECT 和 GROUP BY。
//
// 必须是同一个字符串：PG 不做表达式等价推导，
// `SELECT TO_CHAR(created_at,'YYYY-MM') … GROUP BY DATE_TRUNC('month', created_at)`
// 直接 42803（column … must appear in the GROUP BY clause）。
//
// 原实现抄的是 MySQL 的 YEARWEEK()/DATE_FORMAT()，而本仓只跑 PostgreSQL
// （pkg/db/db.go 用 postgres.Open）⇒「按周/按月统计」这两个已经暴露给前端的旋钮一直在报错。
// DATE_TRUNC 作用于 timestamptz 时按会话时区（连接串钉 Asia/Shanghai）切桶，与 day 分支同口径。
func applyXiaohongshuStatsGroupBy(query *gorm.DB, groupBy string) *gorm.DB {
	bucket := "DATE(created_at)"
	switch groupBy {
	case "week":
		bucket = "TO_CHAR(DATE_TRUNC('week', created_at), 'YYYY-MM-DD')"
	case "month":
		bucket = "TO_CHAR(created_at, 'YYYY-MM')"
	}
	return query.Select(bucket + " as date, activity_type as action, COUNT(*) as count").
		Group(bucket + ", activity_type").
		Order("date, action")
}

// 统计接口的日期边界一律用「半开区间」：
//   - `created_at <= 'YYYY-MM-DD'` 会被 PG 解释成**当天 00:00**，结束日那天除了零点整
//     全部静默丢失；而 controller 的默认结束日就是"今天"
//     （controller/xiaohongshu_card_stats.go:40）⇒ 默认视图永远看不到当天数据。
//   - 两个边界原先还要求"同时非空才生效"，只传 start_date 会被整段忽略。
//   - 日期串由调用方按业务日生成（timeutil.BusinessDate），口径与 DATE(created_at) 的
//     会话时区分桶一致。
func (r *xiaohongshuCardStatsRepository) GetCardDailyStats(ctx context.Context, cardID uint, startDate, endDate, groupBy string) ([]XiaohongshuCardStatsTempStat, error) {
	query := r.db.WithContext(ctx).Model(&model.XiaohongshuCardActivity{}).Where("card_id = ?", cardID)
	if startDate != "" {
		query = query.Where("created_at >= ?::date", startDate)
	}
	if endDate != "" {
		query = query.Where("created_at < (?::date + interval '1 day')", endDate)
	}
	query = applyXiaohongshuStatsGroupBy(query, groupBy)
	var stats []XiaohongshuCardStatsTempStat
	if err := query.Scan(&stats).Error; err != nil {
		return nil, err
	}
	return stats, nil
}

func (r *xiaohongshuCardStatsRepository) GetRecentActivitiesByCard(ctx context.Context, cardID uint, limit int) ([]model.XiaohongshuCardActivity, error) {
	var activities []model.XiaohongshuCardActivity
	err := r.db.WithContext(ctx).Where("card_id = ? AND activity_type = ?", cardID, "view").
		Order("created_at DESC").Limit(limit).Find(&activities).Error
	return activities, err
}

func (r *xiaohongshuCardStatsRepository) CountTotalCards(ctx context.Context) (int64, error) {
	var n int64
	err := r.db.WithContext(ctx).Model(&model.XiaohongshuCard{}).Count(&n).Error
	return n, err
}

func (r *xiaohongshuCardStatsRepository) CountActiveCards(ctx context.Context) (int64, error) {
	var n int64
	err := r.db.WithContext(ctx).Model(&model.XiaohongshuCard{}).Where("is_active = ?", true).Count(&n).Error
	return n, err
}

func (r *xiaohongshuCardStatsRepository) CountTotalViews(ctx context.Context) (int64, error) {
	var n int64
	err := r.db.WithContext(ctx).Model(&model.XiaohongshuCardActivity{}).Where("activity_type = ?", "view").Count(&n).Error
	return n, err
}

func (r *xiaohongshuCardStatsRepository) GetTopCards(ctx context.Context, limit int) ([]model.XiaohongshuCard, error) {
	var cards []model.XiaohongshuCard
	err := r.db.WithContext(ctx).Order("view_count DESC").Limit(limit).Find(&cards).Error
	return cards, err
}

func (r *xiaohongshuCardStatsRepository) GetOverallDailyStats(ctx context.Context, startDate, endDate, groupBy string) ([]XiaohongshuCardStatsTempStat, error) {
	query := r.db.WithContext(ctx).Model(&model.XiaohongshuCardActivity{})
	if startDate != "" {
		query = query.Where("created_at >= ?::date", startDate)
	}
	if endDate != "" {
		query = query.Where("created_at < (?::date + interval '1 day')", endDate)
	}
	query = applyXiaohongshuStatsGroupBy(query, groupBy)
	var stats []XiaohongshuCardStatsTempStat
	if err := query.Find(&stats).Error; err != nil {
		return nil, err
	}
	return stats, nil
}

func (r *xiaohongshuCardStatsRepository) GetRecentActivities(ctx context.Context, limit int) ([]model.XiaohongshuCardActivity, error) {
	var activities []model.XiaohongshuCardActivity
	err := r.db.WithContext(ctx).Where("activity_type = ?", "view").
		Order("created_at DESC").Limit(limit).Find(&activities).Error
	return activities, err
}

func (r *xiaohongshuCardStatsRepository) CreateActivity(ctx context.Context, activity *model.XiaohongshuCardActivity) error {
	if err := r.db.WithContext(ctx).Create(activity).Error; err != nil {
		return fmt.Errorf("记录活动失败: %w", err)
	}
	return nil
}

func (r *xiaohongshuCardStatsRepository) IncrementCardViewCount(ctx context.Context, cardID uint) error {
	return r.db.WithContext(ctx).Model(&model.XiaohongshuCard{}).
		Where("id = ?", cardID).
		Update("view_count", gorm.Expr("view_count + 1")).Error
}
