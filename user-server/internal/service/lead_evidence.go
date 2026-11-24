package service

import (
	"context"
	"strconv"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/repository"
)

// LeadEvidenceService 判定证据读取（FR-013 回放；三路证据逐条可审计）。
type LeadEvidenceService struct {
	hits       repository.LeadMiningHitRepository
	judgements repository.LeadMiningJudgementRepository
}

func NewLeadEvidenceService() *LeadEvidenceService {
	return &LeadEvidenceService{
		hits:       repository.NewLeadMiningHitRepository(),
		judgements: repository.NewLeadMiningJudgementRepository(),
	}
}

func (s *LeadEvidenceService) Hits(ctx context.Context, judgementID string) ([]model.LeadMiningHit, error) {
	if s == nil || s.hits == nil {
		return nil, ErrLeadNotInitialized
	}
	return s.hits.ListByJudgement(ctx, judgementID)
}

// HitsScoped 带账号数据权限的证据读取（FR-010）。
// 判定的 AccountID 必须落在调用方可见账号集合内；受限且无账号集合时拒绝全部。
func (s *LeadEvidenceService) HitsScoped(ctx context.Context, judgementID string, scope CustomerDataScope) ([]model.LeadMiningHit, error) {
	if s == nil || s.hits == nil || s.judgements == nil {
		return nil, ErrLeadNotInitialized
	}
	if scope.AllAccounts {
		return s.hits.ListByJudgement(ctx, judgementID)
	}
	if scope.RestrictedAccounts && len(scope.AccountIDs) == 0 {
		return nil, ErrCRMCustomerForbidden
	}
	rec, err := s.judgements.GetByID(ctx, judgementID)
	if err != nil || rec == nil {
		return nil, err
	}
	if rec.AccountID != "" && !containsCRMString(scope.AccountIDs, rec.AccountID) {
		// 识别码/numeric id 与 scope 中的账号集合口径可能不同，交给解析器归一后再判一次。
		resolved, rerr := NewChannelResolver().ResolveByAccountRef(ctx, rec.Platform, rec.AccountID)
		if rerr != nil || resolved == nil ||
			!containsCRMString(scope.AccountIDs, toCRMUintString(resolved.AccountID)) {
			return nil, ErrCRMCustomerForbidden
		}
	}
	// 无 AccountID 的历史判定只做租户过滤（仓储层已强制），保持既有可读性。
	return s.hits.ListByJudgement(ctx, judgementID)
}

func toCRMUintString(v uint) string {
	if v == 0 {
		return ""
	}
	return strconv.FormatUint(uint64(v), 10)
}
