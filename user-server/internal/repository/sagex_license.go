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

type SageXActivationCodeRepository struct {
	db *gorm.DB
}

func NewSageXActivationCodeRepository() *SageXActivationCodeRepository {
	return &SageXActivationCodeRepository{db: _db.GetDB()}
}

func (r *SageXActivationCodeRepository) SetDB(db *gorm.DB) {
	if db != nil {
		r.db = db
	}
}

func (r *SageXActivationCodeRepository) Create(ctx context.Context, code *model.SageXActivationCode) error {
	return r.db.WithContext(tenant.WithSystemScope(ctx)).Create(code).Error
}

func (r *SageXActivationCodeRepository) GetByCode(ctx context.Context, code string) (*model.SageXActivationCode, error) {
	if code == "" {
		return nil, nil
	}
	var c model.SageXActivationCode
	err := r.db.WithContext(tenant.WithSystemScope(ctx)).Where("code = ?", code).First(&c).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &c, nil
}

func (r *SageXActivationCodeRepository) Update(ctx context.Context, code *model.SageXActivationCode) error {
	return saveWholeRow(r.db.WithContext(tenant.WithSystemScope(ctx)), code)
}

// ActivateIfIssued 条件激活：仅当状态为 issued 时更新为 activated，返回影响行数。
func (r *SageXActivationCodeRepository) ActivateIfIssued(ctx context.Context, code, fingerprint, deviceModel, ip, phone string, now time.Time) (int64, error) {
	q := r.db.WithContext(tenant.WithSystemScope(ctx)).Model(&model.SageXActivationCode{}).
		Where("code = ? AND status = ?", code, "issued").
		Updates(map[string]any{
			"status":            "activated",
			"device_fingerprint": fingerprint,
			"device_model":      deviceModel,
			"activated_at":      now,
		})
	if ip != "" {
		r.db.WithContext(tenant.WithSystemScope(ctx)).Model(&model.SageXActivationCode{}).
			Where("code = ? AND status = ?", code, "activated").Update("ip", ip)
	}
	if phone != "" {
		r.db.WithContext(tenant.WithSystemScope(ctx)).Model(&model.SageXActivationCode{}).
			Where("code = ? AND status = ?", code, "activated").Update("phone", phone)
	}
	return q.RowsAffected, q.Error
}

func (r *SageXActivationCodeRepository) List(ctx context.Context, status string) ([]*model.SageXActivationCode, error) {
	var out []*model.SageXActivationCode
	q := r.db.WithContext(tenant.WithSystemScope(ctx)).Order("id DESC")
	if status != "" {
		q = q.Where("status = ?", status)
	}
	err := q.Find(&out).Error
	if err != nil {
		return nil, err
	}
	return out, nil
}

type SageXHeartbeatRepository struct {
	db *gorm.DB
}

func NewSageXHeartbeatRepository() *SageXHeartbeatRepository {
	return &SageXHeartbeatRepository{db: _db.GetDB()}
}

func (r *SageXHeartbeatRepository) SetDB(db *gorm.DB) {
	if db != nil {
		r.db = db
	}
}

func (r *SageXHeartbeatRepository) Create(ctx context.Context, hb *model.SageXHeartbeat) error {
	return r.db.WithContext(tenant.WithSystemScope(ctx)).Create(hb).Error
}

func (r *SageXHeartbeatRepository) List(ctx context.Context, code string) ([]*model.SageXHeartbeat, error) {
	var out []*model.SageXHeartbeat
	q := r.db.WithContext(tenant.WithSystemScope(ctx)).Order("id DESC")
	if code != "" {
		q = q.Where("code = ?", code)
	}
	err := q.Find(&out).Error
	if err != nil {
		return nil, err
	}
	return out, nil
}

// TouchLastSeen placeholder to keep interface parity; not used.
var _ = time.Now
