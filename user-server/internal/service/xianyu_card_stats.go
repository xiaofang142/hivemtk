package service

import (
	"context"
	"fmt"
	"hivemtk-user/internal/dto"
	"hivemtk-user/internal/pkg/timeutil"
	"hivemtk-user/internal/repository"

	"gorm.io/gorm"
)

// XianyuCardStatsService 闲鱼卡片统计服务接口
type XianyuCardStatsService interface {
	GetCardStats(ctx context.Context, cardID uint, startDate, endDate string) (*dto.XianyuCardStatsResponse, error)
	GetOverallStats(ctx context.Context, startDate, endDate string) (*dto.XianyuCardOverallStatsResponse, error)
	GetCardStatsRaw(ctx context.Context, cardID uint, startDate, endDate string) (*dto.CardStatsData, error)
	GetOverallStatsRaw(ctx context.Context, startDate, endDate string) (*dto.OverallStatsData, error)
	RecordView(ctx context.Context, cardID uint, ip, userAgent, referer string) error
	RecordClick(ctx context.Context, cardID uint, ip, userAgent, referer string) error
	RecordShare(ctx context.Context, cardID uint, ip, userAgent, referer string) error
}

type xianyuCardStatsService struct {
	repo repository.XianyuCardStatsRepository
}

// NewXianyuCardStatsService 创建闲鱼卡片统计服务
func NewXianyuCardStatsService(db any) XianyuCardStatsService {
	gormDB := db.(*gorm.DB)
	return &xianyuCardStatsService{
		repo: repository.NewXianyuCardStatsRepository(gormDB),
	}
}

func (s *xianyuCardStatsService) GetCardStats(ctx context.Context, cardID uint, startDate, endDate string) (*dto.XianyuCardStatsResponse, error) {
	start, err := timeutil.ParseBusinessDate(startDate)
	if err != nil {
		return nil, fmt.Errorf("开始日期格式错误: %w", err)
	}
	end, err := timeutil.ParseBusinessDate(endDate)
	if err != nil {
		return nil, fmt.Errorf("结束日期格式错误: %w", err)
	}

	stats, err := s.repo.GetCardStats(ctx, cardID, start, end)
	if err != nil {
		return nil, fmt.Errorf("获取卡片统计数据失败: %w", err)
	}

	dailyStats := make([]dto.DailyStat, len(stats.StatsByDate))
	for i, stat := range stats.StatsByDate {
		dailyStats[i] = dto.DailyStat{
			Date: stat.Date,
			View: stat.Views,
		}
	}

	return &dto.XianyuCardStatsResponse{
		CardID:     cardID,
		Title:      "",
		ViewCount:  stats.Views,
		ClickCount: stats.Clicks,
		ShareCount: stats.Shares,
		DailyStats: dailyStats,
	}, nil
}

func (s *xianyuCardStatsService) GetOverallStats(ctx context.Context, startDate, endDate string) (*dto.XianyuCardOverallStatsResponse, error) {
	start, err := timeutil.ParseBusinessDate(startDate)
	if err != nil {
		return nil, fmt.Errorf("开始日期格式错误: %w", err)
	}
	end, err := timeutil.ParseBusinessDate(endDate)
	if err != nil {
		return nil, fmt.Errorf("结束日期格式错误: %w", err)
	}

	stats, err := s.repo.GetOverallStats(ctx, start, end)
	if err != nil {
		return nil, fmt.Errorf("获取整体统计数据失败: %w", err)
	}

	dailyStats := make([]dto.DailyStat, len(stats.StatsByDate))
	for i, stat := range stats.StatsByDate {
		dailyStats[i] = dto.DailyStat{
			Date: stat.Date,
			View: stat.Views,
		}
	}

	popularCards := make([]dto.PopularCard, len(stats.TopCards))
	for i, card := range stats.TopCards {
		popularCards[i] = dto.PopularCard{
			ID:        card.ID,
			Title:     card.Title,
			ViewCount: card.ViewCount,
			CreatedAt: card.CreatedAt,
		}
	}

	return &dto.XianyuCardOverallStatsResponse{
		TotalCards:     int(stats.TotalCards),
		ActiveCards:    int(stats.ActiveCards),
		TotalViews:     stats.TotalViewCount,
		TotalClicks:    stats.TotalClickCount,
		TotalShares:    stats.TotalShareCount,
		DailyStats:     dailyStats,
		PopularCards:   popularCards,
		RecentActivity: []dto.Activity{},
	}, nil
}

func (s *xianyuCardStatsService) GetCardStatsRaw(ctx context.Context, cardID uint, startDate, endDate string) (*dto.CardStatsData, error) {
	start, err := timeutil.ParseBusinessDate(startDate)
	if err != nil {
		return nil, fmt.Errorf("开始日期格式错误: %w", err)
	}
	end, err := timeutil.ParseBusinessDate(endDate)
	if err != nil {
		return nil, fmt.Errorf("结束日期格式错误: %w", err)
	}
	result, err := s.repo.GetCardStats(ctx, cardID, start, end)
	if err != nil {
		return nil, err
	}
	statsByDate := make([]dto.StatsByDate, len(result.StatsByDate))
	for i, sd := range result.StatsByDate {
		statsByDate[i] = dto.StatsByDate{
			Date:   sd.Date,
			Views:  sd.Views,
			Clicks: sd.Clicks,
			Shares: sd.Shares,
		}
	}
	return &dto.CardStatsData{
		CardID:      result.CardID,
		Views:       result.Views,
		Clicks:      result.Clicks,
		Shares:      result.Shares,
		StatsByDate: statsByDate,
	}, nil
}

func (s *xianyuCardStatsService) GetOverallStatsRaw(ctx context.Context, startDate, endDate string) (*dto.OverallStatsData, error) {
	start, err := timeutil.ParseBusinessDate(startDate)
	if err != nil {
		return nil, fmt.Errorf("开始日期格式错误: %w", err)
	}
	end, err := timeutil.ParseBusinessDate(endDate)
	if err != nil {
		return nil, fmt.Errorf("结束日期格式错误: %w", err)
	}
	result, err := s.repo.GetOverallStats(ctx, start, end)
	if err != nil {
		return nil, err
	}
	statsByDate := make([]dto.StatsByDate, len(result.StatsByDate))
	for i, sd := range result.StatsByDate {
		statsByDate[i] = dto.StatsByDate{
			Date:   sd.Date,
			Views:  sd.Views,
			Clicks: sd.Clicks,
			Shares: sd.Shares,
		}
	}
	topCards := make([]dto.TopCard, len(result.TopCards))
	for i, c := range result.TopCards {
		topCards[i] = dto.TopCard{
			ID:        c.ID,
			Title:     c.Title,
			ViewCount: c.ViewCount,
			CreatedAt: c.CreatedAt,
		}
	}
	return &dto.OverallStatsData{
		TotalViewCount:  result.TotalViewCount,
		TotalClickCount: result.TotalClickCount,
		TotalShareCount: result.TotalShareCount,
		TotalCards:      result.TotalCards,
		ActiveCards:     result.ActiveCards,
		StatsByDate:     statsByDate,
		TopCards:        topCards,
	}, nil
}

func (s *xianyuCardStatsService) RecordView(ctx context.Context, cardID uint, ip, userAgent, referer string) error {
	return s.repo.RecordActivity(ctx, cardID, "view", ip, userAgent, referer)
}

func (s *xianyuCardStatsService) RecordClick(ctx context.Context, cardID uint, ip, userAgent, referer string) error {
	return s.repo.RecordActivity(ctx, cardID, "click", ip, userAgent, referer)
}

func (s *xianyuCardStatsService) RecordShare(ctx context.Context, cardID uint, ip, userAgent, referer string) error {
	return s.repo.RecordActivity(ctx, cardID, "share", ip, userAgent, referer)
}
