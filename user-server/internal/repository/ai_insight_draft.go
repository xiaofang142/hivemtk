package repository

import (
	"context"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tenant"

	"gorm.io/gorm"
)

type AIInsightDraftRepository interface {
	Create(ctx context.Context, d *model.AIInsightDraft) error
	List(ctx context.Context, entityType, entityID, status string, page, pageSize int) ([]model.AIInsightDraft, int64, error)
	UpdateStatus(ctx context.Context, draftID, status string, confirmedBy *uint) error
	UpdateReview(ctx context.Context, draftID, status string, confirmedBy *uint, reviewNote, appliedActionJSON string) error
	GetByDraftID(ctx context.Context, draftID string) (*model.AIInsightDraft, error)
}

type aiInsightDraftRepository struct{}

func NewAIInsightDraftRepository() AIInsightDraftRepository         { return &aiInsightDraftRepository{} }
func (r *aiInsightDraftRepository) db(ctx context.Context) *gorm.DB { return dbFromCtx(ctx) }

func (r *aiInsightDraftRepository) Create(ctx context.Context, d *model.AIInsightDraft) error {
	if d.TenantID == 0 {
		d.TenantID = tenant.WriteTenantID(ctx)
	}
	return r.db(ctx).Create(d).Error
}

func (r *aiInsightDraftRepository) List(ctx context.Context, entityType, entityID, status string, page, pageSize int) ([]model.AIInsightDraft, int64, error) {
	if page < 1 {
		page = 1
	}
	if pageSize <= 0 || pageSize > 100 {
		pageSize = 20
	}
	tid := tenant.WriteTenantID(ctx)
	q := r.db(ctx).Model(&model.AIInsightDraft{}).Where("tenant_id=?", tid)
	if entityType != "" {
		q = q.Where("entity_type=?", entityType)
	}
	if entityID != "" {
		q = q.Where("entity_id=?", entityID)
	}
	if status != "" {
		q = q.Where("status=?", status)
	}
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var items []model.AIInsightDraft
	err := q.Order("created_at desc").Offset((page - 1) * pageSize).Limit(pageSize).Find(&items).Error
	return items, total, err
}

func (r *aiInsightDraftRepository) UpdateStatus(ctx context.Context, draftID, status string, confirmedBy *uint) error {
	return r.UpdateReview(ctx, draftID, status, confirmedBy, "", "")
}

func (r *aiInsightDraftRepository) UpdateReview(ctx context.Context, draftID, status string, confirmedBy *uint, reviewNote, appliedActionJSON string) error {
	tid := tenant.WriteTenantID(ctx)
	updates := map[string]any{"status": status}
	if confirmedBy != nil {
		updates["confirmed_by"] = *confirmedBy
		updates["confirmed_at"] = gorm.Expr("now()")
	}
	if reviewNote != "" {
		updates["review_note"] = reviewNote
	}
	if appliedActionJSON != "" {
		updates["applied_action_json"] = appliedActionJSON
	}
	return r.db(ctx).Model(&model.AIInsightDraft{}).Where("tenant_id=? AND draft_id=?", tid, draftID).Updates(updates).Error
}

func (r *aiInsightDraftRepository) GetByDraftID(ctx context.Context, draftID string) (*model.AIInsightDraft, error) {
	tid := tenant.WriteTenantID(ctx)
	var d model.AIInsightDraft
	err := r.db(ctx).Where("tenant_id=? AND draft_id=?", tid, draftID).First(&d).Error
	if err != nil {
		return nil, err
	}
	return &d, nil
}
