package service

import (
	"context"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/timeutil"
	"hivemtk-user/internal/pkg/utils"
	"hivemtk-user/internal/repository"
	"time"

	"gorm.io/gorm"
)

type CardAccessService interface {
	RecordAccess(ctx context.Context, cardID uint, cardType string, ip, ua, referer string) error
	GetCardUVStats(ctx context.Context, cardID uint, cardType string, startDate, endDate time.Time) (*CardStatsResponse, error)
	GetDailyUVStats(ctx context.Context, cardID uint, cardType string) ([]DailyStats, error)
	GetTodayUV(ctx context.Context, cardID uint, cardType string) (int, error)
}

type CardStatsResponse struct {
	CardID    uint      `json:"card_id"`
	CardType  string    `json:"card_type"`
	TotalUV   int       `json:"total_uv"`
	TotalPV   int       `json:"total_pv"`
	TodayUV   int       `json:"today_uv"`
	TodayPV   int       `json:"today_pv"`
	StartDate time.Time `json:"start_date"`
	EndDate   time.Time `json:"end_date"`
}

type DailyStats struct {
	Date    string `json:"date"`
	UVCount int    `json:"uv_count"`
	PVCount int    `json:"pv_count"`
}

type cardAccessService struct {
	accessRepo  repository.CardAccessRepository
	uvStatsRepo repository.DailyCardUVStatsRepository
}

func NewCardAccessService(db *gorm.DB) CardAccessService {
	return &cardAccessService{
		accessRepo:  repository.NewCardAccessRepository(db),
		uvStatsRepo: repository.NewDailyCardUVStatsRepository(db),
	}
}

func (s *cardAccessService) RecordAccess(ctx context.Context, cardID uint, cardType string, ip, ua, referer string) error {
	access := &model.CardAccess{
		CardID:     cardID,
		CardType:   cardType,
		IPAddress:  ip,
		UserAgent:  ua,
		Referer:    referer,
		DeviceType: utils.ParseDeviceType(ua),
		Browser:    utils.ParseBrowser(ua),
		OS:         utils.ParseOS(ua),
	}

	if err := s.accessRepo.Create(ctx, access); err != nil {
		return err
	}

	// Date 是 daily_card_uv_stats 的日期键，必须与下面 HasAccessToday 的业务日窗口
	// 同口径：宿主时区一漂（UTC 容器 16:00–23:59）两者就差一天，UV 会累加到昨天的行上。
	today := timeutil.BusinessToday()

	stats, err := s.uvStatsRepo.GetByCardIDAndDate(ctx, cardID, cardType, today)
	if err != nil && err != gorm.ErrRecordNotFound {
		return err
	}

	if stats == nil {
		stats = &model.DailyCardUVStats{
			CardID:   cardID,
			CardType: cardType,
			Date:     today,
			UVCount:  1,
			PVCount:  1,
		}
		return s.uvStatsRepo.Create(ctx, stats)
	}

	stats.PVCount++

	hasAccessToday, err := s.accessRepo.HasAccessToday(ctx, cardID, ip)
	if err != nil {
		return err
	}
	if !hasAccessToday {
		stats.UVCount++
	}

	return s.uvStatsRepo.Update(ctx, stats)
}

func (s *cardAccessService) GetCardUVStats(ctx context.Context, cardID uint, cardType string, startDate, endDate time.Time) (*CardStatsResponse, error) {
	totalUV, err := s.accessRepo.CountDistinctIP(ctx, cardID, cardType, startDate, endDate)
	if err != nil {
		return nil, err
	}

	totalPV, err := s.accessRepo.CountAccess(ctx, cardID, cardType, startDate, endDate)
	if err != nil {
		return nil, err
	}

	today := timeutil.BusinessToday()
	todayStats, _ := s.uvStatsRepo.GetByCardIDAndDate(ctx, cardID, cardType, today)

	return &CardStatsResponse{
		CardID:    cardID,
		CardType:  cardType,
		TotalUV:   totalUV,
		TotalPV:   totalPV,
		TodayUV:   todayStats.UVCount,
		TodayPV:   todayStats.PVCount,
		StartDate: startDate,
		EndDate:   endDate,
	}, nil
}

func (s *cardAccessService) GetDailyUVStats(ctx context.Context, cardID uint, cardType string) ([]DailyStats, error) {
	stats, err := s.uvStatsRepo.GetByCardID(ctx, cardID, cardType)
	if err != nil {
		return nil, err
	}

	var result []DailyStats
	for _, stat := range stats {
		result = append(result, DailyStats{
			Date:    stat.Date,
			UVCount: stat.UVCount,
			PVCount: stat.PVCount,
		})
	}

	return result, nil
}

func (s *cardAccessService) GetTodayUV(ctx context.Context, cardID uint, cardType string) (int, error) {
	today := timeutil.BusinessToday()
	stats, err := s.uvStatsRepo.GetByCardIDAndDate(ctx, cardID, cardType, today)
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return 0, nil
		}
		return 0, err
	}

	return stats.UVCount, nil
}
