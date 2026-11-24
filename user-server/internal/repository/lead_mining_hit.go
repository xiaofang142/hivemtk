package repository

import (
	"context"

	"hivemtk-user/internal/model"
	_db "hivemtk-user/internal/pkg/db"
	"hivemtk-user/internal/pkg/tenant"

	"gorm.io/gorm"
)

// LeadMiningHitRepository 三路检测证据仓储。每路检测独立落一行，不合并覆盖。
type LeadMiningHitRepository interface {
	CreateBatch(ctx context.Context, hits []*model.LeadMiningHit) error
	ListByJudgement(ctx context.Context, judgementID string) ([]model.LeadMiningHit, error)
}

type leadMiningHitRepo struct {
	db *gorm.DB
}

func NewLeadMiningHitRepository() LeadMiningHitRepository {
	return &leadMiningHitRepo{db: _db.GetDB()}
}

func NewLeadMiningHitRepositoryWithDB(db *gorm.DB) LeadMiningHitRepository {
	return &leadMiningHitRepo{db: db}
}

func (r *leadMiningHitRepo) scoped(ctx context.Context) (*gorm.DB, error) {
	if r == nil || r.db == nil {
		return nil, ErrChannelStoreUnavailable
	}
	tid, ok := tenant.FromContext(ctx)
	if !ok || tid == 0 {
		if tenant.IsMultiTenant() {
			return nil, ErrLeadTenantRequired
		}
		tid = tenant.DefaultTenantID
	}
	return r.db.WithContext(ctx).Where("tenant_id = ?", tid), nil
}

func (r *leadMiningHitRepo) CreateBatch(ctx context.Context, hits []*model.LeadMiningHit) error {
	if len(hits) == 0 {
		return nil
	}
	q, err := r.scoped(ctx)
	if err != nil {
		return err
	}
	return q.Create(hits).Error
}

func (r *leadMiningHitRepo) ListByJudgement(ctx context.Context, judgementID string) ([]model.LeadMiningHit, error) {
	q, err := r.scoped(ctx)
	if err != nil {
		return nil, err
	}
	var items []model.LeadMiningHit
	err = q.Where("judgement_id = ?", judgementID).Order("id ASC").Find(&items).Error
	return items, err
}
