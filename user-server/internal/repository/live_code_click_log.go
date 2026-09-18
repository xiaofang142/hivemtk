package repository

import (
	"context"
	"time"

	"hivemtk-user/internal/model"

	"gorm.io/gorm"
	"hivemtk-user/internal/pkg/timeutil"
)

// LiveCodeClickLogRepository 活码点击日志仓储接口
// 提供点击日志写入与按活码/二维码维度的真实聚合能力
type LiveCodeClickLogRepository interface {
	CreateLiveCodeClick(ctx context.Context, log *model.LiveCodeClickLog) error
	CreateQRCodeClick(ctx context.Context, log *model.QRCodeClickLog) error
	CountByLiveCode(ctx context.Context, liveCodeID string) (int64, error)
	CountTodayByLiveCode(ctx context.Context, liveCodeID string) (int64, error)
	CountByQRCode(ctx context.Context, qrCodeID string) (int64, error)
	CountTodayByQRCode(ctx context.Context, qrCodeID string) (int64, error)
}

type liveCodeClickLogRepository struct {
	db *gorm.DB
}

// NewLiveCodeClickLogRepository 创建活码点击日志仓储实例
func NewLiveCodeClickLogRepository(db *gorm.DB) LiveCodeClickLogRepository {
	return &liveCodeClickLogRepository{db: db}
}

func (r *liveCodeClickLogRepository) CreateLiveCodeClick(ctx context.Context, log *model.LiveCodeClickLog) error {
	return r.db.WithContext(ctx).Create(log).Error
}

func (r *liveCodeClickLogRepository) CreateQRCodeClick(ctx context.Context, log *model.QRCodeClickLog) error {
	return r.db.WithContext(ctx).Create(log).Error
}

func (r *liveCodeClickLogRepository) CountByLiveCode(ctx context.Context, liveCodeID string) (int64, error) {
	var count int64
	err := r.db.WithContext(ctx).Model(&model.LiveCodeClickLog{}).
		Where("live_code_id = ?", liveCodeID).
		Count(&count).Error
	return count, err
}

func (r *liveCodeClickLogRepository) CountTodayByLiveCode(ctx context.Context, liveCodeID string) (int64, error) {
	var count int64
	todayStart := timeutil.StartOfDay(time.Now())
	err := r.db.WithContext(ctx).Model(&model.LiveCodeClickLog{}).
		Where("live_code_id = ? AND created_at >= ?", liveCodeID, todayStart).
		Count(&count).Error
	return count, err
}

func (r *liveCodeClickLogRepository) CountByQRCode(ctx context.Context, qrCodeID string) (int64, error) {
	var count int64
	err := r.db.WithContext(ctx).Model(&model.QRCodeClickLog{}).
		Where("qr_code_id = ?", qrCodeID).
		Count(&count).Error
	return count, err
}

func (r *liveCodeClickLogRepository) CountTodayByQRCode(ctx context.Context, qrCodeID string) (int64, error) {
	var count int64
	todayStart := timeutil.StartOfDay(time.Now())
	err := r.db.WithContext(ctx).Model(&model.QRCodeClickLog{}).
		Where("qr_code_id = ? AND created_at >= ?", qrCodeID, todayStart).
		Count(&count).Error
	return count, err
}
