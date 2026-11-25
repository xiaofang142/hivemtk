package repository

import (
	"context"
	"errors"
	"time"

	"hivemtk-user/internal/model"
	_db "hivemtk-user/internal/pkg/db"
	"hivemtk-user/internal/pkg/tenant"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
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

// CreateIfAbsent atomically inserts a non-empty installation identity. The
// partial unique index is the final concurrency guard; callers must re-read and
// validate ownership/status when created is false.
func (r *SageXInstallationRepository) CreateIfAbsent(ctx context.Context, inst *model.SageXInstallation) (bool, error) {
	if r.db == nil {
		return false, gorm.ErrInvalidDB
	}
	if inst == nil || inst.TenantID == 0 {
		return false, errors.New("sagex installation tenant is required")
	}
	result := r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{
			{Name: "tenant_id"},
			{Name: "installation_id"},
		},
		TargetWhere: clause.Where{Exprs: []clause.Expression{
			clause.Neq{Column: clause.Column{Name: "installation_id"}, Value: ""},
		}},
		DoNothing: true,
	}).Create(inst)
	if result.Error != nil {
		return false, result.Error
	}
	return result.RowsAffected > 0, nil
}

func (r *SageXInstallationRepository) GetByTenantAndInstallationID(
	ctx context.Context,
	tenantID uint,
	installationID string,
) (*model.SageXInstallation, error) {
	if r.db == nil {
		return nil, gorm.ErrInvalidDB
	}
	if installationID == "" {
		return nil, nil
	}
	var inst model.SageXInstallation
	err := r.db.WithContext(ctx).
		Where("tenant_id = ? AND installation_id = ?", tenantID, installationID).
		First(&inst).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &inst, nil
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

// GetByToken is a deprecated compatibility lookup for legacy admin/bootstrap
// metadata. SageX upload authentication uses identify_code only.
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
