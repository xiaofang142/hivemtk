package service

import (
	"context"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/repository"
)

// LeadEvidenceService 判定证据读取（FR-013 回放；三路证据逐条可审计）。
type LeadEvidenceService struct {
	hits repository.LeadMiningHitRepository
}

func NewLeadEvidenceService() *LeadEvidenceService {
	return &LeadEvidenceService{hits: repository.NewLeadMiningHitRepository()}
}

func (s *LeadEvidenceService) Hits(ctx context.Context, judgementID string) ([]model.LeadMiningHit, error) {
	if s == nil || s.hits == nil {
		return nil, ErrLeadNotInitialized
	}
	return s.hits.ListByJudgement(ctx, judgementID)
}
