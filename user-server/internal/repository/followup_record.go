package repository

import (
	"context"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tenant"

	"gorm.io/gorm"
)

type FollowupRecordRepository interface {
	Create(ctx context.Context, r *model.FollowupRecord) error
	Update(ctx context.Context, r *model.FollowupRecord) error
	GetByRecordID(ctx context.Context, recordID string) (*model.FollowupRecord, error)
	List(ctx context.Context, customerID, reminderID string, ownerID uint, page, pageSize int) ([]model.FollowupRecord, int64, error)
}

type followupRecordRepository struct{}

func NewFollowupRecordRepository() FollowupRecordRepository { return &followupRecordRepository{} }

func (r *followupRecordRepository) db(ctx context.Context) *gorm.DB { return dbFromCtx(ctx) }

func (r *followupRecordRepository) Create(ctx context.Context, rec *model.FollowupRecord) error {
	if rec.TenantID == 0 {
		rec.TenantID = tenant.WriteTenantID(ctx)
	}
	return r.db(ctx).Create(rec).Error
}

func (r *followupRecordRepository) Update(ctx context.Context, rec *model.FollowupRecord) error {
	tid := tenant.WriteTenantID(ctx)
	return r.db(ctx).Model(&model.FollowupRecord{}).
		Where("tenant_id=? AND record_id=?", tid, rec.RecordID).
		Updates(rec).Error
}

func (r *followupRecordRepository) GetByRecordID(ctx context.Context, recordID string) (*model.FollowupRecord, error) {
	tid := tenant.WriteTenantID(ctx)
	var rec model.FollowupRecord
	err := r.db(ctx).Where("tenant_id=? AND record_id=?", tid, recordID).First(&rec).Error
	if err != nil {
		return nil, err
	}
	return &rec, nil
}

func (r *followupRecordRepository) List(ctx context.Context, customerID, reminderID string, ownerID uint, page, pageSize int) ([]model.FollowupRecord, int64, error) {
	if page < 1 {
		page = 1
	}
	if pageSize <= 0 || pageSize > 100 {
		pageSize = 20
	}
	tid := tenant.WriteTenantID(ctx)
	q := r.db(ctx).Model(&model.FollowupRecord{}).Where("tenant_id=?", tid)
	if customerID != "" {
		q = q.Where("customer_id=?", customerID)
	}
	if reminderID != "" {
		q = q.Where("reminder_id=?", reminderID)
	}
	if ownerID > 0 {
		q = q.Where("owner_id=?", ownerID)
	}
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var items []model.FollowupRecord
	err := q.Order("created_at desc").Offset((page - 1) * pageSize).Limit(pageSize).Find(&items).Error
	return items, total, err
}
