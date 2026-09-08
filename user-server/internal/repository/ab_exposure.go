package repository

import (
	"context"
	"time"

	_db "hivemtk-user/internal/pkg/db"

	"gorm.io/gorm"
)

// AbExposure AB 曝光/转化记录（表 ab_exposures）。
// service 层禁止直接持有 gorm.DB，持久化统一收敛到 repository。
type AbExposure struct {
	ID           uint       `gorm:"primarykey" json:"id"`
	ExperimentID string     `gorm:"size:64;index:idx_ab_exp_customer,priority:1" json:"experiment_id"`
	CustomerID   string     `gorm:"size:64;index:idx_ab_exp_customer,priority:2" json:"customer_id"`
	Variant      string     `gorm:"size:16" json:"variant"`
	SessionID    string     `gorm:"size:64" json:"session_id"`
	ExposedAt    time.Time  `json:"exposed_at"`
	ConvertedAt  *time.Time `json:"converted_at"`
}

func (AbExposure) TableName() string { return "ab_exposures" }

// ABExposureRepository AB 曝光记录仓库
type ABExposureRepository struct {
	db *gorm.DB
}

func NewABExposureRepository() *ABExposureRepository {
	return &ABExposureRepository{db: _db.GetDB()}
}

func NewABExposureRepositoryWithDB(db *gorm.DB) *ABExposureRepository {
	return &ABExposureRepository{db: db}
}

func (r *ABExposureRepository) Create(ctx context.Context, e *AbExposure) error {
	if r == nil || r.db == nil {
		return nil
	}
	return r.db.WithContext(ctx).Create(e).Error
}

func (r *ABExposureRepository) MarkConversion(ctx context.Context, expID, customerID string, at time.Time) error {
	if r == nil || r.db == nil {
		return nil
	}
	return r.db.WithContext(ctx).Model(&AbExposure{}).
		Where("experiment_id = ? AND customer_id = ? AND converted_at IS NULL", expID, customerID).
		Update("converted_at", at).Error
}

func (r *ABExposureRepository) ListSince(ctx context.Context, expID string, since time.Time) ([]AbExposure, error) {
	if r == nil || r.db == nil {
		return nil, nil
	}
	var rows []AbExposure
	err := r.db.WithContext(ctx).
		Where("experiment_id = ? AND exposed_at >= ?", expID, since).
		Find(&rows).Error
	return rows, err
}
