package repository

import (
	"context"
	"fmt"
	"time"

	"hivemtk-user/internal/model"

	"gorm.io/gorm"
)

// KuaishouCardStatsDailyResult 按日期分组的统计结果（repository 层本地类型）
type KuaishouCardStatsDailyResult struct {
	Date         string
	ActivityType string
	Count        int
}

// KuaishouRecentActivity 快手最近活动记录（JOIN 查询结果）
type KuaishouRecentActivity struct {
	ID        uint
	CardID    uint
	CardTitle string
	Action    string
	UserIP    string
	UserAgent string
	ExtraData string
	CreatedAt time.Time
}

type KuaishouCardStatsRepository interface {
	GetCardByID(ctx context.Context, cardID uint) (*model.KuaishouCard, error)
	CountCardViews(ctx context.Context, cardID uint) (int64, error)
	// CountCardViewsInRange 区间内的浏览数：单卡统计页的「总浏览量」旁边的日期范围
	// 也控制着「访问趋势」，两块口径必须一致，否则窄区间下 KPI 有值而趋势为空。
	CountCardViewsInRange(ctx context.Context, cardID uint, startDate, endDate time.Time) (int64, error)
	GetCardDailyStats(ctx context.Context, cardID uint, startDate, endDate time.Time, groupBy string) ([]KuaishouCardStatsDailyResult, error)
	CountTotalCards(ctx context.Context) (int64, error)
	CountActiveCards(ctx context.Context) (int64, error)
	CountTotalViews(ctx context.Context, startDate, endDate time.Time) (int64, error)
	GetPopularCards(ctx context.Context, limit int) ([]model.KuaishouCard, error)
	GetOverallDailyStats(ctx context.Context, startDate, endDate time.Time, groupBy string) ([]KuaishouCardStatsDailyResult, error)
	GetRecentActivitiesWithJoin(ctx context.Context, limit int) ([]KuaishouRecentActivity, error)
	// GetRecentActivitiesByCard 单卡维度的最近浏览记录（抖音/小红书两仓都有
	// 同名方法，快手此前漏实现导致单卡统计页「最近活动」恒空）。
	GetRecentActivitiesByCard(ctx context.Context, cardID uint, limit int) ([]KuaishouRecentActivity, error)
	CreateActivity(ctx context.Context, activity *model.KuaishouCardActivity) error
	IncrementViewCount(ctx context.Context, id uint) error
}

type kuaishouCardStatsRepository struct {
	db *gorm.DB
}

func NewKuaishouCardStatsRepository(db *gorm.DB) KuaishouCardStatsRepository {
	return &kuaishouCardStatsRepository{db: db}
}

func (r *kuaishouCardStatsRepository) GetCardByID(ctx context.Context, cardID uint) (*model.KuaishouCard, error) {
	var card model.KuaishouCard
	if err := r.db.WithContext(ctx).First(&card, cardID).Error; err != nil {
		return nil, err
	}
	return &card, nil
}

func (r *kuaishouCardStatsRepository) CountCardViews(ctx context.Context, cardID uint) (int64, error) {
	var n int64
	err := r.db.WithContext(ctx).Model(&model.KuaishouCardActivity{}).
		Where("card_id = ? AND activity_type = ?", cardID, "view").
		Count(&n).Error
	return n, err
}

// dateGroupExpr 把分组粒度翻译成 SQL 日期表达式。
// 之前单卡统计直接 DATE(created_at)，页面上的「按周/按月」等于没选。
func dateGroupExpr(groupBy string) string {
	switch groupBy {
	case "week":
		return "TO_CHAR(DATE_TRUNC('week', created_at), 'YYYY-MM-DD')"
	case "month":
		return "TO_CHAR(DATE_TRUNC('month', created_at), 'YYYY-MM-DD')"
	default:
		return "TO_CHAR(DATE(created_at), 'YYYY-MM-DD')"
	}
}

func (r *kuaishouCardStatsRepository) CountCardViewsInRange(ctx context.Context, cardID uint, startDate, endDate time.Time) (int64, error) {
	var n int64
	err := r.db.WithContext(ctx).Model(&model.KuaishouCardActivity{}).
		Where("card_id = ? AND activity_type = ? AND created_at >= ? AND created_at <= ?", cardID, "view", startDate, endDate).
		Count(&n).Error
	return n, err
}

func (r *kuaishouCardStatsRepository) GetCardDailyStats(ctx context.Context, cardID uint, startDate, endDate time.Time, groupBy string) ([]KuaishouCardStatsDailyResult, error) {
	var results []KuaishouCardStatsDailyResult
	expr := dateGroupExpr(groupBy)
	err := r.db.WithContext(ctx).Table("kuaishou_card_activities").
		Select(expr+" as date, activity_type, COUNT(*) as count").
		Where("card_id = ? AND created_at BETWEEN ? AND ?", cardID, startDate, endDate).
		Group(expr + ", activity_type").
		Order("date ASC").
		Scan(&results).Error
	return results, err
}

func (r *kuaishouCardStatsRepository) CountTotalCards(ctx context.Context) (int64, error) {
	var n int64
	err := r.db.WithContext(ctx).Model(&model.KuaishouCard{}).Count(&n).Error
	return n, err
}

func (r *kuaishouCardStatsRepository) CountActiveCards(ctx context.Context) (int64, error) {
	var n int64
	err := r.db.WithContext(ctx).Model(&model.KuaishouCard{}).Where("is_active = ?", true).Count(&n).Error
	return n, err
}

func (r *kuaishouCardStatsRepository) CountTotalViews(ctx context.Context, startDate, endDate time.Time) (int64, error) {
	var n int64
	err := r.db.WithContext(ctx).Model(&model.KuaishouCardActivity{}).
		Where("activity_type = ? AND created_at BETWEEN ? AND ?", "view", startDate, endDate).
		Count(&n).Error
	return n, err
}

func (r *kuaishouCardStatsRepository) GetPopularCards(ctx context.Context, limit int) ([]model.KuaishouCard, error) {
	var cards []model.KuaishouCard
	err := r.db.WithContext(ctx).Where("is_active = ?", true).
		Order("view_count DESC").Limit(limit).Find(&cards).Error
	return cards, err
}

// GetOverallDailyStats 按时间粒度聚合全部卡片的浏览事件。
// groupBy 之前被服务层丢掉，导致前端「按天/按周/按月」切换毫无效果；
// 这里按 day/week/month 选不同的日期表达式，与抖音/小红书同口径。
func (r *kuaishouCardStatsRepository) GetOverallDailyStats(ctx context.Context, startDate, endDate time.Time, groupBy string) ([]KuaishouCardStatsDailyResult, error) {
	dateExpr := "TO_CHAR(DATE(created_at), 'YYYY-MM-DD')"
	switch groupBy {
	case "week":
		dateExpr = "TO_CHAR(DATE_TRUNC('week', created_at), 'YYYY-MM-DD')"
	case "month":
		dateExpr = "TO_CHAR(DATE_TRUNC('month', created_at), 'YYYY-MM-DD')"
	}

	var results []KuaishouCardStatsDailyResult
	err := r.db.WithContext(ctx).Table("kuaishou_card_activities").
		Select(dateExpr+" as date, activity_type, COUNT(*) as count").
		Where("created_at BETWEEN ? AND ?", startDate, endDate).
		Group(dateExpr + ", activity_type").
		Order("date ASC").
		Scan(&results).Error
	return results, err
}

func (r *kuaishouCardStatsRepository) GetRecentActivitiesWithJoin(ctx context.Context, limit int) ([]KuaishouRecentActivity, error) {
	var results []KuaishouRecentActivity
	err := r.db.WithContext(ctx).Table("kuaishou_card_activities").
		Select("kuaishou_card_activities.id, kuaishou_card_activities.card_id, kuaishou_cards.title as card_title, kuaishou_card_activities.activity_type as action, kuaishou_card_activities.ip_address as user_ip, kuaishou_card_activities.user_agent, kuaishou_card_activities.extra_data, kuaishou_card_activities.created_at").
		Joins("LEFT JOIN kuaishou_cards ON kuaishou_card_activities.card_id = kuaishou_cards.id").
		Where("kuaishou_card_activities.activity_type = ?", "view").
		Order("kuaishou_card_activities.created_at DESC").
		Limit(limit).
		Scan(&results).Error
	return results, err
}

func (r *kuaishouCardStatsRepository) GetRecentActivitiesByCard(ctx context.Context, cardID uint, limit int) ([]KuaishouRecentActivity, error) {
	var results []KuaishouRecentActivity
	err := r.db.WithContext(ctx).Table("kuaishou_card_activities").
		Select("kuaishou_card_activities.id, kuaishou_card_activities.card_id, kuaishou_cards.title as card_title, kuaishou_card_activities.activity_type as action, kuaishou_card_activities.ip_address as user_ip, kuaishou_card_activities.user_agent, kuaishou_card_activities.extra_data, kuaishou_card_activities.created_at").
		Joins("LEFT JOIN kuaishou_cards ON kuaishou_card_activities.card_id = kuaishou_cards.id").
		Where("kuaishou_card_activities.card_id = ? AND kuaishou_card_activities.activity_type = ?", cardID, "view").
		Order("kuaishou_card_activities.created_at DESC").
		Limit(limit).
		Scan(&results).Error
	return results, err
}

func (r *kuaishouCardStatsRepository) CreateActivity(ctx context.Context, activity *model.KuaishouCardActivity) error {
	if err := r.db.WithContext(ctx).Create(activity).Error; err != nil {
		return fmt.Errorf("记录活动失败: %w", err)
	}
	return nil
}

func (r *kuaishouCardStatsRepository) IncrementViewCount(ctx context.Context, id uint) error {
	return r.db.WithContext(ctx).Model(&model.KuaishouCard{}).
		Where("id = ?", id).
		UpdateColumn("view_count", gorm.Expr("view_count + 1")).Error
}
