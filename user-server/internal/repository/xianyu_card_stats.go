package repository

import (
	"context"
	"fmt"
	"hivemtk-user/internal/model"
	"time"

	"gorm.io/gorm"
)

// CardStatsByDate 按日期统计数据（repository 层本地类型）
type CardStatsByDate struct {
	Date   string
	Views  int
	Clicks int
	Shares int
}

// CardTopStats 热门卡片统计（repository 层本地类型）
type CardTopStats struct {
	ID        uint
	Title     string
	ViewCount int
	CreatedAt string
}

// CardStatsResult 卡片统计数据结果（repository 层本地类型，service 层负责转 dto）
type CardStatsResult struct {
	CardID      uint
	Views       int
	Clicks      int
	Shares      int
	StatsByDate []CardStatsByDate
}

// CardOverallStatsResult 整体统计数据结果（repository 层本地类型，service 层负责转 dto）
type CardOverallStatsResult struct {
	TotalViewCount  int
	TotalClickCount int
	TotalShareCount int
	TotalCards      int64
	ActiveCards     int64
	StatsByDate     []CardStatsByDate
	TopCards        []CardTopStats
}

// XianyuCardStatsRepository 闲鱼卡片统计仓库接口
type XianyuCardStatsRepository interface {
	GetCardStats(ctx context.Context, cardID uint, startDate, endDate time.Time) (*CardStatsResult, error)
	GetOverallStats(ctx context.Context, startDate, endDate time.Time) (*CardOverallStatsResult, error)
	RecordActivity(ctx context.Context, cardID uint, activityType, ip, userAgent, referer string) error
}

type xianyuCardStatsRepository struct {
	db *gorm.DB
}

// NewXianyuCardStatsRepository 创建闲鱼卡片统计仓库
func NewXianyuCardStatsRepository(db *gorm.DB) XianyuCardStatsRepository {
	return &xianyuCardStatsRepository{db: db}
}

func (r *xianyuCardStatsRepository) GetCardStats(ctx context.Context, cardID uint, startDate, endDate time.Time) (*CardStatsResult, error) {
	var stats CardStatsResult
	// endDate 是**含端点**的日历日 ⇒ 对时间戳列用半开区间 `>= start AND < 次日零点`。
	// 原写法 `<= 次日零点` 会把次日 00:00:00 那一瞬也算进来。
	endExclusive := endDate.AddDate(0, 0, 1)

	type basicStats struct {
		ViewCount  int64
		ClickCount int64
		ShareCount int64
	}
	var basic basicStats

	err := r.db.WithContext(ctx).
		Model(&model.XianyuCardActivity{}).
		Where("card_id = ? AND activity_type = ? AND created_at >= ? AND created_at < ?", cardID, "view", startDate, endExclusive).
		Count(&basic.ViewCount).Error
	if err != nil {
		return nil, fmt.Errorf("获取浏览数失败: %w", err)
	}

	err = r.db.WithContext(ctx).
		Model(&model.XianyuCardActivity{}).
		Where("card_id = ? AND activity_type = ? AND created_at >= ? AND created_at < ?", cardID, "click", startDate, endExclusive).
		Count(&basic.ClickCount).Error
	if err != nil {
		return nil, fmt.Errorf("获取点击数失败: %w", err)
	}

	err = r.db.WithContext(ctx).
		Model(&model.XianyuCardActivity{}).
		Where("card_id = ? AND activity_type = ? AND created_at >= ? AND created_at < ?", cardID, "share", startDate, endExclusive).
		Count(&basic.ShareCount).Error
	if err != nil {
		return nil, fmt.Errorf("获取分享数失败: %w", err)
	}

	type dateStats struct {
		Date       string
		ViewCount  int64
		ClickCount int64
		ShareCount int64
	}
	var dateStatsList []dateStats

	err = r.db.WithContext(ctx).
		Raw(`
			SELECT 
				DATE(created_at) as date,
				SUM(CASE WHEN activity_type = 'view' THEN 1 ELSE 0 END) as view_count,
				SUM(CASE WHEN activity_type = 'click' THEN 1 ELSE 0 END) as click_count,
				SUM(CASE WHEN activity_type = 'share' THEN 1 ELSE 0 END) as share_count
			FROM xianyu_card_activities 
			WHERE card_id = ? AND created_at >= ? AND created_at < ?
			GROUP BY DATE(created_at)
			ORDER BY date DESC
		`, cardID, startDate, endExclusive).
		Scan(&dateStatsList).Error
	if err != nil {
		return nil, fmt.Errorf("获取按日期统计数据失败: %w", err)
	}

	statsByDate := make([]CardStatsByDate, len(dateStatsList))
	for i, ds := range dateStatsList {
		statsByDate[i] = CardStatsByDate{
			Date:   ds.Date,
			Views:  int(ds.ViewCount),
			Clicks: int(ds.ClickCount),
			Shares: int(ds.ShareCount),
		}
	}

	stats = CardStatsResult{
		Views:       int(basic.ViewCount),
		Clicks:      int(basic.ClickCount),
		Shares:      int(basic.ShareCount),
		StatsByDate: statsByDate,
	}

	return &stats, nil
}

func (r *xianyuCardStatsRepository) GetOverallStats(ctx context.Context, startDate, endDate time.Time) (*CardOverallStatsResult, error) {
	var stats CardOverallStatsResult
	// 同 GetCardStats：日历日含端点 ⇒ 半开区间上界。
	endExclusive := endDate.AddDate(0, 0, 1)

	type totalStats struct {
		TotalViewCount  int64
		TotalClickCount int64
		TotalShareCount int64
		TotalCards      int64
		ActiveCards     int64
	}
	var total totalStats

	err := r.db.WithContext(ctx).
		Model(&model.XianyuCardActivity{}).
		Where("created_at >= ? AND created_at < ?", startDate, endExclusive).
		Where("activity_type = ?", "view").
		Count(&total.TotalViewCount).Error
	if err != nil {
		return nil, fmt.Errorf("获取总浏览数失败: %w", err)
	}

	err = r.db.WithContext(ctx).
		Model(&model.XianyuCardActivity{}).
		Where("created_at >= ? AND created_at < ?", startDate, endExclusive).
		Where("activity_type = ?", "click").
		Count(&total.TotalClickCount).Error
	if err != nil {
		return nil, fmt.Errorf("获取总点击数失败: %w", err)
	}

	err = r.db.WithContext(ctx).
		Model(&model.XianyuCardActivity{}).
		Where("created_at >= ? AND created_at < ?", startDate, endExclusive).
		Where("activity_type = ?", "share").
		Count(&total.TotalShareCount).Error
	if err != nil {
		return nil, fmt.Errorf("获取总分享数失败: %w", err)
	}

	err = r.db.WithContext(ctx).
		Model(&model.XianyuCard{}).
		Count(&total.TotalCards).Error
	if err != nil {
		return nil, fmt.Errorf("获取总卡片数失败: %w", err)
	}

	err = r.db.WithContext(ctx).
		Model(&model.XianyuCard{}).
		Where("is_active = ?", true).
		Count(&total.ActiveCards).Error
	if err != nil {
		return nil, fmt.Errorf("获取活跃卡片数失败: %w", err)
	}

	type dateStats struct {
		Date       string
		ViewCount  int64
		ClickCount int64
		ShareCount int64
	}
	var dateStatsList []dateStats

	err = r.db.WithContext(ctx).
		Raw(`
			SELECT 
				DATE(created_at) as date,
				SUM(CASE WHEN activity_type = 'view' THEN 1 ELSE 0 END) as view_count,
				SUM(CASE WHEN activity_type = 'click' THEN 1 ELSE 0 END) as click_count,
				SUM(CASE WHEN activity_type = 'share' THEN 1 ELSE 0 END) as share_count
			FROM xianyu_card_activities 
			WHERE created_at >= ? AND created_at < ?
			GROUP BY DATE(created_at)
			ORDER BY date DESC
		`, startDate, endExclusive).
		Scan(&dateStatsList).Error
	if err != nil {
		return nil, fmt.Errorf("获取按日期统计数据失败: %w", err)
	}

	type topCard struct {
		CardID     uint   `json:"card_id"`
		CardTitle  string `json:"card_title"`
		ViewCount  int64  `json:"view_count"`
		ClickCount int64  `json:"click_count"`
		ShareCount int64  `json:"share_count"`
	}
	var topCards []topCard

	err = r.db.WithContext(ctx).
		Raw(`
			SELECT 
				c.id as card_id,
				c.title as card_title,
				SUM(CASE WHEN ca.activity_type = 'view' THEN 1 ELSE 0 END) as view_count,
				SUM(CASE WHEN ca.activity_type = 'click' THEN 1 ELSE 0 END) as click_count,
				SUM(CASE WHEN ca.activity_type = 'share' THEN 1 ELSE 0 END) as share_count
			FROM xianyu_cards c
			LEFT JOIN xianyu_card_activities ca ON c.id = ca.card_id
			WHERE ca.created_at >= ? AND ca.created_at < ?
			GROUP BY c.id, c.title
			ORDER BY view_count DESC
			LIMIT 10
		`, startDate, endExclusive).
		Scan(&topCards).Error
	if err != nil {
		return nil, fmt.Errorf("获取热门卡片失败: %w", err)
	}

	statsByDate := make([]CardStatsByDate, len(dateStatsList))
	for i, ds := range dateStatsList {
		statsByDate[i] = CardStatsByDate{
			Date:   ds.Date,
			Views:  int(ds.ViewCount),
			Clicks: int(ds.ClickCount),
			Shares: int(ds.ShareCount),
		}
	}

	topCardsResponse := make([]CardTopStats, len(topCards))
	for i, tc := range topCards {
		topCardsResponse[i] = CardTopStats{
			ID:        tc.CardID,
			Title:     tc.CardTitle,
			ViewCount: int(tc.ViewCount),
		}
	}

	stats = CardOverallStatsResult{
		TotalViewCount:  int(total.TotalViewCount),
		TotalClickCount: int(total.TotalClickCount),
		TotalShareCount: int(total.TotalShareCount),
		TotalCards:      total.TotalCards,
		ActiveCards:     total.ActiveCards,
		StatsByDate:     statsByDate,
		TopCards:        topCardsResponse,
	}

	return &stats, nil
}

func (r *xianyuCardStatsRepository) RecordActivity(ctx context.Context, cardID uint, activityType, ip, userAgent, referer string) error {
	activity := &model.XianyuCardActivity{
		CardID:       cardID,
		ActivityType: activityType,
		IP:           ip,
		UserAgent:    userAgent,
		Referer:      referer,
		CreatedAt:    time.Now(),
	}

	if err := r.db.WithContext(ctx).Create(activity).Error; err != nil {
		return fmt.Errorf("记录活动失败: %w", err)
	}

	if activityType == "view" {
		if err := r.db.WithContext(ctx).Model(&model.XianyuCard{}).
			Where("id = ?", cardID).
			UpdateColumn("view_count", gorm.Expr("view_count + 1")).Error; err != nil {
			return fmt.Errorf("递增卡片浏览数失败: %w", err)
		}
	}

	return nil
}
