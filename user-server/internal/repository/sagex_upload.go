package repository

import (
	"context"
	"errors"
	"time"

	"hivemtk-user/internal/model"
	_db "hivemtk-user/internal/pkg/db"
	"hivemtk-user/internal/pkg/tenant"

	"gorm.io/gorm"
)

type SageXInstallationRepository struct {
	db *gorm.DB
}

func NewSageXInstallationRepository() *SageXInstallationRepository {
	return &SageXInstallationRepository{db: _db.GetDB()}
}

func (r *SageXInstallationRepository) SetDB(db *gorm.DB) {
	if db != nil {
		r.db = db
	}
}

func (r *SageXInstallationRepository) Create(ctx context.Context, inst *model.SageXInstallation) error {
	return r.db.WithContext(ctx).Create(inst).Error
}

func (r *SageXInstallationRepository) Update(ctx context.Context, inst *model.SageXInstallation) error {
	return saveWholeRow(r.db.WithContext(ctx), inst)
}

func (r *SageXInstallationRepository) GetByID(ctx context.Context, id uint) (*model.SageXInstallation, error) {
	var inst model.SageXInstallation
	err := r.db.WithContext(ctx).First(&inst, id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &inst, nil
}

func (r *SageXInstallationRepository) GetByToken(ctx context.Context, token string) (*model.SageXInstallation, error) {
	if token == "" {
		return nil, nil
	}
	var inst model.SageXInstallation
	err := r.db.WithContext(tenant.WithSystemScope(ctx)).
		Where("token = ? AND status = ?", token, 1).
		First(&inst).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &inst, nil
}

func (r *SageXInstallationRepository) GetByInstallationID(ctx context.Context, installationID string) (*model.SageXInstallation, error) {
	if installationID == "" {
		return nil, nil
	}
	var inst model.SageXInstallation
	err := r.db.WithContext(ctx).Where("installation_id = ?", installationID).First(&inst).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &inst, nil
}

func (r *SageXInstallationRepository) TouchLastSeen(ctx context.Context, id uint, at time.Time) error {
	return r.db.WithContext(ctx).Model(&model.SageXInstallation{}).
		Where("id = ?", id).
		Update("last_seen_at", at).Error
}

type SageXEventReceiptRepository struct {
	db *gorm.DB
}

func NewSageXEventReceiptRepository() *SageXEventReceiptRepository {
	return &SageXEventReceiptRepository{db: _db.GetDB()}
}

func (r *SageXEventReceiptRepository) SetDB(db *gorm.DB) {
	if db != nil {
		r.db = db
	}
}

func (r *SageXEventReceiptRepository) GetByInstallationAndEventID(ctx context.Context, tenantID uint, installationID, eventID string) (*model.SageXEventReceipt, error) {
	var receipt model.SageXEventReceipt
	err := r.db.WithContext(ctx).
		Where("tenant_id = ? AND installation_id = ? AND event_id = ?", tenantID, installationID, eventID).
		First(&receipt).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &receipt, nil
}

func (r *SageXEventReceiptRepository) Create(ctx context.Context, receipt *model.SageXEventReceipt) error {
	return r.db.WithContext(ctx).Create(receipt).Error
}

func (r *SageXEventReceiptRepository) Update(ctx context.Context, receipt *model.SageXEventReceipt) error {
	return saveWholeRow(r.db.WithContext(ctx), receipt)
}
