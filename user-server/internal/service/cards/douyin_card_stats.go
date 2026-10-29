package cards

import (
	"context"
	"fmt"
	"time"

	"hivemtk-user/internal/dto"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/repository"
)

// DouyinCardStatsService 抖音卡片统计服务接口
type DouyinCardStatsService interface {
	GetCardStats(ctx context.Context, req *dto.DouyinCardStatsRequest) (*dto.DouyinCardStatsResponse, error)
	GetOverallStats(ctx context.Context, req *dto.DouyinCardOverallStatsRequest) (*dto.DouyinCardOverallStatsResponse, error)
	RecordActivity(ctx context.Context, cardID uint, userID uint, action string, username, ipAddress, userAgent string) error
}

type douyinCardStatsService struct {
	repo repository.DouyinCardStatsRepository
}

// NewDouyinCardStatsService 创建抖音卡片统计服务实例
func NewDouyinCardStatsService(repo repository.DouyinCardStatsRepository) DouyinCardStatsService {
	return &douyinCardStatsService{
		repo: repo,
	}
}

func (s *douyinCardStatsService) GetCardStats(ctx context.Context, req *dto.DouyinCardStatsRequest) (*dto.DouyinCardStatsResponse, error) {
	card, err := s.repo.GetCardByID(ctx, req.CardID)
	if err != nil {
		return nil, fmt.Errorf("卡片不存在: %w", err)
	}

	views, err := s.repo.CountCardViews(ctx, req.CardID)
	if err != nil {
		return nil, err
	}

	tempStats, err := s.repo.GetCardDailyStats(ctx, req.CardID, req.StartDate, req.EndDate, req.GroupBy)
	if err != nil {
		return nil, err
	}

	dailyStats := convertDouyinTempStats(tempStats)

	// 单卡响应必须带 recentActivity：douyinCard/CardStats.vue 的「最近活动」表格正是绑它，
	// 字段缺失时前端只能兜成空数组 ⇒ 表格永远「暂无数据」，看着像"这张卡没人看过"。
	// 仓储的 GetRecentActivitiesByCard 早就在，只是这条链路没调它（快手单卡同形问题已按此补齐）。
	recentResults, err := s.repo.GetRecentActivitiesByCard(ctx, req.CardID, 10)
	if err != nil {
		return nil, err
	}
	recentActivity := make([]dto.Activity, 0, len(recentResults))
	for _, a := range recentResults {
		recentActivity = append(recentActivity, dto.Activity{
			ID:        a.ID,
			CardID:    a.CardID,
			Action:    a.Action,
			Username:  a.Username,
			IPAddress: a.IPAddress,
			CreatedAt: a.CreatedAt.Format(time.RFC3339),
		})
	}

	response := &dto.DouyinCardStatsResponse{
		CardID:         req.CardID,
		Title:          card.Title,
		ViewCount:      int(views),
		DailyStats:     dailyStats,
		RecentActivity: recentActivity,
	}

	return response, nil
}

func convertDouyinTempStats(tempStats []repository.DouyinCardStatsTempStat) []dto.DailyStat {
	dateMap := make(map[string]*dto.DailyStat)
	for _, stat := range tempStats {
		if _, exists := dateMap[stat.Date]; !exists {
			dateMap[stat.Date] = &dto.DailyStat{
				Date: stat.Date,
				View: 0,
			}
		}

		if stat.Action == "view" {
			dateMap[stat.Date].View = stat.Count
		}
	}

	var dailyStats []dto.DailyStat
	for _, stat := range dateMap {
		dailyStats = append(dailyStats, *stat)
	}
	return dailyStats
}

func (s *douyinCardStatsService) GetOverallStats(ctx context.Context, req *dto.DouyinCardOverallStatsRequest) (*dto.DouyinCardOverallStatsResponse, error) {
	totalCards, err := s.repo.CountTotalCards(ctx)
	if err != nil {
		return nil, err
	}
	activeCards, err := s.repo.CountActiveCards(ctx)
	if err != nil {
		return nil, err
	}

	totalViews, err := s.repo.CountTotalViews(ctx)
	if err != nil {
		return nil, err
	}

	topCards, err := s.repo.GetTopCards(ctx, 10)
	if err != nil {
		return nil, err
	}

	var cards []dto.PopularCard
	for _, card := range topCards {
		cards = append(cards, dto.PopularCard{
			ID:        card.ID,
			Title:     card.Title,
			ViewCount: card.ViewCount,
			CreatedAt: card.CreatedAt.Format(time.RFC3339),
		})
	}

	tempStats, err := s.repo.GetOverallDailyStats(ctx, req.StartDate, req.EndDate, req.GroupBy)
	if err != nil {
		return nil, err
	}
	dailyStats := convertDouyinTempStats(tempStats)

	recentActivities, err := s.repo.GetRecentActivities(ctx, 10)
	if err != nil {
		return nil, err
	}

	var activities []dto.Activity
	for _, activity := range recentActivities {
		activities = append(activities, dto.Activity{
			ID:        activity.ID,
			CardID:    activity.CardID,
			Action:    activity.Action,
			Username:  activity.Username,
			IPAddress: activity.IPAddress,
			CreatedAt: activity.CreatedAt.Format(time.RFC3339),
		})
	}

	return &dto.DouyinCardOverallStatsResponse{
		TotalCards:     int(totalCards),
		ActiveCards:    int(activeCards),
		TotalViews:     int(totalViews),
		PopularCards:   cards,
		DailyStats:     dailyStats,
		RecentActivity: activities,
	}, nil
}

func (s *douyinCardStatsService) RecordActivity(ctx context.Context, cardID uint, userID uint, action string, username, ipAddress, userAgent string) error {
	if action != "view" {
		return nil
	}

	activity := &model.DouyinCardActivity{
		CardID:    cardID,
		UserID:    userID,
		Action:    action,
		Username:  username,
		IPAddress: ipAddress,
		UserAgent: userAgent,
		CreatedAt: time.Now(),
	}

	if err := s.repo.CreateActivity(ctx, activity); err != nil {
		return err
	}

	return s.repo.IncrementCardViewCount(ctx, cardID)
}
