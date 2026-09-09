package service

import (
	"context"
	"time"

	"hivemtk-user/internal/repository"
)

type ResolutionStats struct {
	TotalSuggestions   int64                  `json:"total_suggestions"`
	AdoptedSuggestions int64                  `json:"adopted_suggestions"`
	AdoptionRate       float64                `json:"adoption_rate"`
	ResolvedByAI       int64                  `json:"resolved_by_ai"`
	ResolvedRate       float64                `json:"resolved_rate"`
	DailyTrend         []DailyResolutionPoint `json:"daily_trend"`
}

type DailyResolutionPoint struct {
	Date    string `json:"date"`
	Total   int64  `json:"total"`
	Adopted int64  `json:"adopted"`
}

type AIResolutionStatsService struct {
	repo *repository.AIResolutionStatsRepository
}

func NewAIResolutionStatsService(repo *repository.AIResolutionStatsRepository) *AIResolutionStatsService {
	return &AIResolutionStatsService{repo: repo}
}

func (s *AIResolutionStatsService) GetStats(ctx context.Context, days int) (*ResolutionStats, error) {
	if days <= 0 {
		days = 30
	}
	since := time.Now().AddDate(0, 0, -days)
	stats := &ResolutionStats{}
	if s.repo == nil {
		return stats, nil
	}
	total, adopted, err := s.repo.CountSuggestions(ctx, since)
	if err != nil {
		return stats, err
	}
	stats.TotalSuggestions = total
	stats.AdoptedSuggestions = adopted
	if stats.TotalSuggestions > 0 {
		stats.AdoptionRate = float64(stats.AdoptedSuggestions) / float64(stats.TotalSuggestions) * 100
	}
	stats.ResolvedByAI = stats.AdoptedSuggestions
	stats.ResolvedRate = stats.AdoptionRate
	stats.DailyTrend = []DailyResolutionPoint{}
	return stats, nil
}
