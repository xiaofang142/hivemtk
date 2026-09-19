package repository

import (
	"context"
	"fmt"

	"hivemtk-user/internal/model"

	"gorm.io/gorm"
)

// DouyinCardStatsTempStat 按时间分组的临时统计结果（repository 层本地类型）
type DouyinCardStatsTempStat struct {
	Date   string
	Action string
	Count  int
}

// DouyinCardStatsRepository 抖音卡片统计仓库接口
type DouyinCardStatsRepository interface {
	GetCardByID(ctx context.Context, cardID uint) (*model.DouyinCard, error)
	CountCardViews(ctx context.Context, cardID uint) (int64, error)
	GetCardDailyStats(ctx context.Context, cardID uint, startDate, endDate, groupBy string) ([]DouyinCardStatsTempStat, error)
	GetRecentActivitiesByCard(ctx context.Context, cardID uint, limit int) ([]model.DouyinCardActivity, error)
	CountTotalCards(ctx context.Context) (int64, error)
	CountActiveCards(ctx context.Context) (int64, error)
	CountTotalViews(ctx context.Context) (int64, error)
	GetTopCards(ctx context.Context, limit int) ([]model.DouyinCard, error)
	GetOverallDailyStats(ctx context.Context, startDate, endDate, groupBy string) ([]DouyinCardStatsTempStat, error)
	GetRecentActivities(ctx context.Context, limit int) ([]model.DouyinCardActivity, error)
	CreateActivity(ctx context.Context, activity *model.DouyinCardActivity) error
	IncrementCardViewCount(ctx context.Context, cardID uint) error
}

type douyinCardStatsRepository struct {
	db *gorm.DB
}

// NewDouyinCardStatsRepository 创建抖音卡片统计仓库
func NewDouyinCardStatsRepository(db *gorm.DB) DouyinCardStatsRepository {
	return &douyinCardStatsRepository{db: db}
}

func (r *douyinCardStatsRepository) GetCardByID(ctx context.Context, cardID uint) (*model.DouyinCard, error) {
	var card model.DouyinCard
	if err := r.db.WithContext(ctx).First(&card, cardID).Error; err != nil {
		return nil, err
	}
	return &card, nil
}

func (r *douyinCardStatsRepository) CountCardViews(ctx context.Context, cardID uint) (int64, error) {
	var views int64
	err := r.db.WithContext(ctx).Model(&model.DouyinCardActivity{}).
		Where("card_id = ? AND action = ?", cardID, "view").
		Count(&views).Error
	return views, err
}

// applyDouyinStatsGroupBy 与 applyXiaohongshuStatsGroupBy 同源同改：
// MySQL 的 YEARWEEK/DATE_FORMAT 换成本仓实际使用的 PG 函数，
// 且分桶表达式必须在 SELECT 与 GROUP BY 里逐字一致（否则 42803）。
func applyDouyinStatsGroupBy(query *gorm.DB, groupBy string) *gorm.DB {
	bucket := "DATE(created_at)"
	switch groupBy {
	case "week":
		bucket = "TO_CHAR(DATE_TRUNC('week', created_at), 'YYYY-MM-DD')"
	case "month":
		bucket = "TO_CHAR(created_at, 'YYYY-MM')"
	}
	return query.Select(bucket + " as date, action, COUNT(*) as count").
		Group(bucket + ", action").
		Order("date, action")
}

func (r *douyinCardStatsRepository) GetCardDailyStats(ctx context.Context, cardID uint, startDate, endDate, groupBy string) ([]DouyinCardStatsTempStat, error) {
	query := r.db.WithContext(ctx).Model(&model.DouyinCardActivity{}).Where("card_id = ?", cardID)
	// 日期边界口径见 xiaohongshu_card_stats.go 的同名说明（半开区间，结束日全天计入）。
	if startDate != "" {
		query = query.Where("created_at >= ?::date", startDate)
	}
	if endDate != "" {
		query = query.Where("created_at < (?::date + interval '1 day')", endDate)
	}
	query = applyDouyinStatsGroupBy(query, groupBy)
	var stats []DouyinCardStatsTempStat
	if err := query.Scan(&stats).Error; err != nil {
		return nil, err
	}
	return stats, nil
}

func (r *douyinCardStatsRepository) GetRecentActivitiesByCard(ctx context.Context, cardID uint, limit int) ([]model.DouyinCardActivity, error) {
	var activities []model.DouyinCardActivity
	err := r.db.WithContext(ctx).Where("card_id = ? AND action = ?", cardID, "view").
		Order("created_at DESC").Limit(limit).Find(&activities).Error
	return activities, err
}

func (r *douyinCardStatsRepository) CountTotalCards(ctx context.Context) (int64, error) {
	var n int64
	err := r.db.WithContext(ctx).Model(&model.DouyinCard{}).Count(&n).Error
	return n, err
}

func (r *douyinCardStatsRepository) CountActiveCards(ctx context.Context) (int64, error) {
	var n int64
	err := r.db.WithContext(ctx).Model(&model.DouyinCard{}).Where("is_active = ?", true).Count(&n).Error
	return n, err
}

func (r *douyinCardStatsRepository) CountTotalViews(ctx context.Context) (int64, error) {
	var n int64
	err := r.db.WithContext(ctx).Model(&model.DouyinCardActivity{}).Where("action = ?", "view").Count(&n).Error
	return n, err
}

func (r *douyinCardStatsRepository) GetTopCards(ctx context.Context, limit int) ([]model.DouyinCard, error) {
	var cards []model.DouyinCard
	err := r.db.WithContext(ctx).Order("view_count DESC").Limit(limit).Find(&cards).Error
	return cards, err
}

func (r *douyinCardStatsRepository) GetOverallDailyStats(ctx context.Context, startDate, endDate, groupBy string) ([]DouyinCardStatsTempStat, error) {
	query := r.db.WithContext(ctx).Model(&model.DouyinCardActivity{})
	// 半开区间：`<= 'YYYY-MM-DD'` 只到当天 00:00，会把结束日整天丢掉（详见 xiaohongshu_card_stats.go）。
	if startDate != "" {
		query = query.Where("created_at >= ?::date", startDate)
	}
	if endDate != "" {
		query = query.Where("created_at < (?::date + interval '1 day')", endDate)
	}
	query = applyDouyinStatsGroupBy(query, groupBy)
	var stats []DouyinCardStatsTempStat
	if err := query.Find(&stats).Error; err != nil {
		return nil, err
	}
	return stats, nil
}

func (r *douyinCardStatsRepository) GetRecentActivities(ctx context.Context, limit int) ([]model.DouyinCardActivity, error) {
	var activities []model.DouyinCardActivity
	err := r.db.WithContext(ctx).Where("action = ?", "view").
		Order("created_at DESC").Limit(limit).Find(&activities).Error
	return activities, err
}

func (r *douyinCardStatsRepository) CreateActivity(ctx context.Context, activity *model.DouyinCardActivity) error {
	if err := r.db.WithContext(ctx).Create(activity).Error; err != nil {
		return fmt.Errorf("记录活动失败: %w", err)
	}
	return nil
}

func (r *douyinCardStatsRepository) IncrementCardViewCount(ctx context.Context, cardID uint) error {
	return r.db.WithContext(ctx).Model(&model.DouyinCard{}).
		Where("id = ?", cardID).
		Update("view_count", gorm.Expr("view_count + 1")).Error
}
