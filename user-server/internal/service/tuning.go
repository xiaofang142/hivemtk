package service

import (
	"context"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/pagination"
	"hivemtk-user/internal/repository"
)

// TuningService 统一管理服务接口
//
// 所有列表方法返回 **值切片**（[]T），与下层 Repository 保持一致；
// 单条记录仍用 *T 指针。
type TuningService interface {
	ListConfidenceSignals(ctx context.Context, sessionID string, page, pageSize int) ([]model.ConfidenceSignal, int64, error)
	GetConfidenceSignal(ctx context.Context, id string) (*model.ConfidenceSignal, error)
	StatsConfidenceSignals(ctx context.Context, since time.Time) (map[string]int64, error)

	ListCalibrations(ctx context.Context, signalType string, page, pageSize int) ([]model.ConfidenceCalibration, int64, error)

	ListThresholdPolicies(ctx context.Context) ([]model.ThresholdPolicy, error)
	UpsertThresholdPolicy(ctx context.Context, p *model.ThresholdPolicy) error

	ListHumanizeScores(ctx context.Context, sessionID string, passed *bool, page, pageSize int) ([]model.HumanizeScore, int64, error)
	StatsHumanizeScores(ctx context.Context, since time.Time) (*HumanizeStats, error)

	ListChampionBaselines(ctx context.Context) ([]model.ChampionBaseline, error)

	ListFeedbackEvents(ctx context.Context, sessionID, signalKey string, page, pageSize int, cursor pagination.Cursor) ([]model.FeedbackEvent, int64, error)
	StatsFeedbackEvents(ctx context.Context, since time.Time) (map[string]int64, error)

	ListChampionDialogues(ctx context.Context, intent, industry string, page, pageSize int, cursor pagination.Cursor) ([]model.ChampionDialogue, int64, error)

	ListPromptCandidates(ctx context.Context, status string, page, pageSize int, cursor pagination.Cursor) ([]model.PromptCandidate, int64, error)
	UpdatePromptCandidateStatus(ctx context.Context, id, status string) error

	ListBanditArms(ctx context.Context, experimentID, sopID string, page, pageSize int, cursor pagination.Cursor) ([]model.BanditArm, int64, error)

	ListLowQualitySamples(ctx context.Context, sampleType string, page, pageSize int) ([]model.LowQualitySample, int64, error)
}

// HumanizeStats 拟人度统计结果
type HumanizeStats struct {
	AvgScore float64
	Passed   int64
	Failed   int64
	Total    int64
}

type tuningService struct {
	signalRepo   *repository.ConfidenceSignalRepository
	calibRepo    *repository.ConfidenceCalibrationRepository
	policyRepo   *repository.ThresholdPolicyRepository
	scoreRepo    *repository.HumanizeScoreRepository
	baselineRepo *repository.ChampionBaselineRepositoryImpl
	lowQRepo     *repository.LowQualitySampleRepository
	feedbackRepo *repository.FeedbackLoopRepository
}

// NewTuningService 构造服务(注入所有 tuning 仓储)
func NewTuningService() TuningService {
	return &tuningService{
		signalRepo:   repository.NewConfidenceSignalRepository(),
		calibRepo:    repository.NewConfidenceCalibrationRepository(),
		policyRepo:   repository.NewThresholdPolicyRepository(),
		scoreRepo:    repository.NewHumanizeScoreRepository(),
		baselineRepo: repository.NewChampionBaselineRepository(),
		lowQRepo:     repository.NewLowQualitySampleRepository(),
		feedbackRepo: repository.NewFeedbackLoopRepository(),
	}
}

func (s *tuningService) ListConfidenceSignals(ctx context.Context, sessionID string, page, pageSize int) ([]model.ConfidenceSignal, int64, error) {
	return s.signalRepo.List(ctx, sessionID, page, pageSize)
}

func (s *tuningService) GetConfidenceSignal(ctx context.Context, id string) (*model.ConfidenceSignal, error) {
	return s.signalRepo.GetByID(ctx, id)
}

func (s *tuningService) StatsConfidenceSignals(ctx context.Context, since time.Time) (map[string]int64, error) {
	rows, err := s.signalRepo.StatsByBand(ctx, since)
	if err != nil {
		return nil, err
	}
	out := make(map[string]int64, len(rows))
	for _, r := range rows {
		out[r.DecisionBand] = r.Count
	}
	return out, nil
}

func (s *tuningService) ListCalibrations(ctx context.Context, signalType string, page, pageSize int) ([]model.ConfidenceCalibration, int64, error) {
	return s.calibRepo.List(ctx, signalType, page, pageSize)
}

func (s *tuningService) ListThresholdPolicies(ctx context.Context) ([]model.ThresholdPolicy, error) {
	return s.policyRepo.ListActive(ctx)
}

func (s *tuningService) UpsertThresholdPolicy(ctx context.Context, p *model.ThresholdPolicy) error {
	p.IsActive = true
	p.UpdatedAt = time.Now()
	return s.policyRepo.Save(ctx, p)
}

func (s *tuningService) ListHumanizeScores(ctx context.Context, sessionID string, passed *bool, page, pageSize int) ([]model.HumanizeScore, int64, error) {
	return s.scoreRepo.List(ctx, sessionID, passed, page, pageSize)
}

func (s *tuningService) StatsHumanizeScores(ctx context.Context, since time.Time) (*HumanizeStats, error) {
	stat, err := s.scoreRepo.Stats(ctx, since)
	if err != nil {
		return nil, err
	}
	return &HumanizeStats{
		AvgScore: stat.AvgScore,
		Passed:   stat.Passed,
		Failed:   stat.Failed,
		Total:    stat.Total,
	}, nil
}

func (s *tuningService) ListChampionBaselines(ctx context.Context) ([]model.ChampionBaseline, error) {
	return s.baselineRepo.ListEnabledModels(ctx)
}

func (s *tuningService) ListFeedbackEvents(ctx context.Context, sessionID, signalKey string, page, pageSize int, cursor pagination.Cursor) ([]model.FeedbackEvent, int64, error) {
	return s.feedbackRepo.ListFeedbackEvents(ctx, sessionID, signalKey, page, pageSize, cursor)
}

func (s *tuningService) StatsFeedbackEvents(ctx context.Context, since time.Time) (map[string]int64, error) {
	rows, err := s.feedbackRepo.StatsFeedbackEvents(ctx, since)
	if err != nil {
		return nil, err
	}
	out := make(map[string]int64, len(rows))
	for _, r := range rows {
		out[r.SignalKey] = r.Count
	}
	return out, nil
}

func (s *tuningService) ListChampionDialogues(ctx context.Context, intent, industry string, page, pageSize int, cursor pagination.Cursor) ([]model.ChampionDialogue, int64, error) {
	return s.feedbackRepo.ListChampionDialogues(ctx, intent, industry, page, pageSize, cursor)
}

func (s *tuningService) ListPromptCandidates(ctx context.Context, status string, page, pageSize int, cursor pagination.Cursor) ([]model.PromptCandidate, int64, error) {
	return s.feedbackRepo.ListPromptCandidates(ctx, status, page, pageSize, cursor)
}

func (s *tuningService) UpdatePromptCandidateStatus(ctx context.Context, id, status string) error {
	return s.feedbackRepo.UpdatePromptCandidateStatus(ctx, id, status)
}

func (s *tuningService) ListBanditArms(ctx context.Context, experimentID, sopID string, page, pageSize int, cursor pagination.Cursor) ([]model.BanditArm, int64, error) {
	return s.feedbackRepo.ListBanditArms(ctx, experimentID, sopID, page, pageSize, cursor)
}

func (s *tuningService) ListLowQualitySamples(ctx context.Context, sampleType string, page, pageSize int) ([]model.LowQualitySample, int64, error) {
	return s.lowQRepo.List(ctx, sampleType, page, pageSize)
}
