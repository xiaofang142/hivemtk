package service

import (
	"fmt"
	"time"

	"hivemtk-user/internal/dto"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/repository"

	"context"

	"gorm.io/gorm"
)

// KuaishouCardStatsService 快手卡片统计服务
type KuaishouCardStatsService struct {
	repo repository.KuaishouCardStatsRepository
}

// NewKuaishouCardStatsService 创建快手卡片统计服务实例
func NewKuaishouCardStatsService(db *gorm.DB) *KuaishouCardStatsService {
	return &KuaishouCardStatsService{repo: repository.NewKuaishouCardStatsRepository(db)}
}

// GetCardStats 获取单个快手卡片的统计数据
func (s *KuaishouCardStatsService) GetCardStats(ctx context.Context, req *dto.KuaishouCardStatsRequest) (*dto.KuaishouCardStatsResponse, error) {
	card, err := s.repo.GetCardByID(ctx, req.CardID)
	if err != nil {
		return nil, fmt.Errorf("卡片不存在: %w", err)
	}

	if req.StartDate.IsZero() {
		req.StartDate = time.Now().AddDate(0, -1, 0)
	}
	if req.EndDate.IsZero() {
		req.EndDate = time.Now()
	}

	if req.GroupBy == "" {
		req.GroupBy = "day"
	}

	results, err := s.repo.GetCardDailyStats(ctx, req.CardID, req.StartDate, req.EndDate)
	if err != nil {
		return nil, fmt.Errorf("查询统计数据失败: %w", err)
	}

	var dailyStats []dto.DailyStat
	dateMap := make(map[string]*dto.DailyStat)
	for _, result := range results {
		dateStr := result.Date
		if _, exists := dateMap[dateStr]; !exists {
			dateMap[dateStr] = &dto.DailyStat{
				Date: dateStr,
				View: 0,
			}
		}

		switch result.ActivityType {
		case "view":
			dateMap[dateStr].View = result.Count
		}
	}

	for _, stat := range dateMap {
		dailyStats = append(dailyStats, *stat)
	}

	totalViews, err := s.repo.CountCardViews(ctx, req.CardID)
	if err != nil {
		return nil, err
	}

	recentResults, err := s.repo.GetRecentActivitiesByCard(ctx, req.CardID, 10)
	if err != nil {
		return nil, err
	}
	recentActivity := make([]dto.Activity, 0, len(recentResults))
	for _, r := range recentResults {
		recentActivity = append(recentActivity, dto.Activity{
			ID:        r.ID,
			CardID:    r.CardID,
			Action:    r.Action,
			IPAddress: r.UserIP,
			CreatedAt: r.CreatedAt.Format(time.RFC3339),
		})
	}

	response := &dto.KuaishouCardStatsResponse{
		CardID:         card.ID,
		CardTitle:      card.Title,
		TotalViews:     int(totalViews),
		DailyStats:     dailyStats,
		RecentActivity: recentActivity,
	}

	return response, nil
}

// GetOverallStats 获取快手卡片总体统计数据
func (s *KuaishouCardStatsService) GetOverallStats(ctx context.Context, req *dto.KuaishouCardOverallStatsRequest) (*dto.KuaishouCardOverallStatsResponse, error) {
	if req.StartDate.IsZero() {
		req.StartDate = time.Now().AddDate(0, -1, 0)
	}
	if req.EndDate.IsZero() {
		req.EndDate = time.Now()
	}

	if req.GroupBy == "" {
		req.GroupBy = "day"
	}

	if req.Limit == 0 {
		req.Limit = 10
	}

	totalCards, err := s.repo.CountTotalCards(ctx)
	if err != nil {
		return nil, err
	}

	activeCards, err := s.repo.CountActiveCards(ctx)
	if err != nil {
		return nil, err
	}

	totalViews, err := s.repo.CountTotalViews(ctx, req.StartDate, req.EndDate)
	if err != nil {
		return nil, err
	}

	popularCards, err := s.repo.GetPopularCards(ctx, req.Limit)
	if err != nil {
		return nil, err
	}

	var popularCardDTOs []dto.PopularCard
	for _, card := range popularCards {
		popularCardDTOs = append(popularCardDTOs, dto.PopularCard{
			ID:        card.ID,
			Title:     card.Title,
			ViewCount: card.ViewCount,
			CreatedAt: card.CreatedAt.Format("2006-01-02"),
		})
	}

	results, err := s.repo.GetOverallDailyStats(ctx, req.StartDate, req.EndDate)
	if err != nil {
		return nil, fmt.Errorf("查询统计数据失败: %w", err)
	}

	var dailyStats []dto.DailyStat
	dateMap := make(map[string]*dto.DailyStat)
	for _, result := range results {
		dateStr := result.Date
		if _, exists := dateMap[dateStr]; !exists {
			dateMap[dateStr] = &dto.DailyStat{
				Date: dateStr,
				View: 0,
			}
		}

		switch result.ActivityType {
		case "view":
			dateMap[dateStr].View = result.Count
		}
	}

	for _, stat := range dateMap {
		dailyStats = append(dailyStats, *stat)
	}

	recentResults, err := s.repo.GetRecentActivitiesWithJoin(ctx, 10)
	if err != nil {
		return nil, err
	}
	var recentActivities []dto.Activity
	for _, r := range recentResults {
		recentActivities = append(recentActivities, dto.Activity{
			ID:        r.ID,
			CardID:    r.CardID,
			Action:    r.Action,
			IPAddress: r.UserIP,
			CreatedAt: r.CreatedAt.Format(time.RFC3339),
		})
	}

	response := &dto.KuaishouCardOverallStatsResponse{
		TotalCards:       int(totalCards),
		ActiveCards:      int(activeCards),
		TotalViews:       int(totalViews),
		PopularCards:     popularCardDTOs,
		DailyStats:       dailyStats,
		RecentActivities: recentActivities,
	}

	return response, nil
}

// RecordActivity 记录快手卡片活动
func (s *KuaishouCardStatsService) RecordActivity(ctx context.Context, cardID uint, action, userIP, userAgent, extraData string) error {
	if action != "view" {
		return nil
	}

	activity := model.KuaishouCardActivity{
		CardID:       cardID,
		ActivityType: action,
		IPAddress:    userIP,
		UserAgent:    userAgent,
		ExtraData:    extraData,
	}

	if err := s.repo.CreateActivity(ctx, &activity); err != nil {
		return err
	}

	switch action {
	case "view":
		if err := s.repo.IncrementViewCount(ctx, cardID); err != nil {
			return fmt.Errorf("更新卡片统计失败: %w", err)
		}
	}

	return nil
}
