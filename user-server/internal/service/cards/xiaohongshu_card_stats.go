package cards

import (
	"context"
	"fmt"
	"time"

	"hivemtk-user/internal/dto"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/repository"
)

// XiaohongshuCardStatsService 小红书卡片统计服务接口
type XiaohongshuCardStatsService interface {
	GetCardStats(ctx context.Context, req *dto.XiaohongshuCardStatsRequest) (*dto.XiaohongshuCardStatsResponse, error)
	GetOverallStats(ctx context.Context, req *dto.XiaohongshuCardOverallStatsRequest) (*dto.XiaohongshuCardOverallStatsResponse, error)
	RecordActivity(ctx context.Context, cardID uint, userID uint, action string, username, ipAddress, userAgent string) error
}

type xiaohongshuCardStatsService struct {
	repo repository.XiaohongshuCardStatsRepository
}

// NewXiaohongshuCardStatsService 创建小红书卡片统计服务实例
func NewXiaohongshuCardStatsService(repo repository.XiaohongshuCardStatsRepository) XiaohongshuCardStatsService {
	return &xiaohongshuCardStatsService{
		repo: repo,
	}
}

func (s *xiaohongshuCardStatsService) GetCardStats(ctx context.Context, req *dto.XiaohongshuCardStatsRequest) (*dto.XiaohongshuCardStatsResponse, error) {
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
	dailyStats := convertXiaohongshuTempStats(tempStats)

	recentActivities, err := s.repo.GetRecentActivitiesByCard(ctx, req.CardID, 10)
	if err != nil {
		return nil, err
	}

	var activities []dto.Activity
	for _, activity := range recentActivities {
		activities = append(activities, dto.Activity{
			ID:        activity.ID,
			CardID:    activity.CardID,
			Action:    activity.ActivityType,
			Username:  activity.Username,
			IPAddress: activity.IPAddress,
			CreatedAt: activity.CreatedAt.Format(time.RFC3339),
		})
	}

	response := &dto.XiaohongshuCardStatsResponse{
		CardID:         req.CardID,
		Title:          card.Title,
		ViewCount:      int(views),
		DailyStats:     dailyStats,
		RecentActivity: activities,
	}

	return response, nil
}

func convertXiaohongshuTempStats(tempStats []repository.XiaohongshuCardStatsTempStat) []dto.DailyStat {
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

func (s *xiaohongshuCardStatsService) GetOverallStats(ctx context.Context, req *dto.XiaohongshuCardOverallStatsRequest) (*dto.XiaohongshuCardOverallStatsResponse, error) {
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
	dailyStats := convertXiaohongshuTempStats(tempStats)

	recentActivities, err := s.repo.GetRecentActivities(ctx, 10)
	if err != nil {
		return nil, err
	}

	var activities []dto.Activity
	for _, activity := range recentActivities {
		activities = append(activities, dto.Activity{
			ID:        activity.ID,
			CardID:    activity.CardID,
			Action:    activity.ActivityType,
			Username:  activity.Username,
			IPAddress: activity.IPAddress,
			CreatedAt: activity.CreatedAt.Format(time.RFC3339),
		})
	}

	return &dto.XiaohongshuCardOverallStatsResponse{
		TotalCards:     int(totalCards),
		ActiveCards:    int(activeCards),
		TotalViews:     int(totalViews),
		PopularCards:   cards,
		DailyStats:     dailyStats,
		RecentActivity: activities,
	}, nil
}

func (s *xiaohongshuCardStatsService) RecordActivity(ctx context.Context, cardID uint, userID uint, action string, username, ipAddress, userAgent string) error {
	if action != "view" {
		return nil
	}

	activity := &model.XiaohongshuCardActivity{
		CardID:       cardID,
		UserID:       userID,
		ActivityType: action,
		Username:     username,
		IPAddress:    ipAddress,
		UserAgent:    userAgent,
		CreatedAt:    time.Now(),
	}

	if err := s.repo.CreateActivity(ctx, activity); err != nil {
		return err
	}

	return s.repo.IncrementCardViewCount(ctx, cardID)
}
