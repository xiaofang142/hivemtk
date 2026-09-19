package repository

import (
	"context"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/timeutil"
	"time"

	"gorm.io/gorm"
)

// ShortLinkAccessRepository 短链访问统计仓储接口
type ShortLinkAccessRepository interface {
	Create(ctx context.Context, access *model.ShortLinkAccess) error
	GetByID(ctx context.Context, id uint) (*model.ShortLinkAccess, error)
	GetByShortLinkID(ctx context.Context, shortLinkID uint, page, pageSize int) ([]*model.ShortLinkAccess, int64, error)
	GetStatsByShortLinkID(ctx context.Context, shortLinkID uint, startDate, endDate time.Time) (*model.ShortLinkAccess, error)
	GetDailyStatsByShortLinkID(ctx context.Context, shortLinkID uint, startDate, endDate time.Time) ([]map[string]any, error)
	GetDeviceTypeStatsByShortLinkID(ctx context.Context, shortLinkID uint, startDate, endDate time.Time) ([]map[string]any, error)
	GetAllDailyStats(ctx context.Context, startDate, endDate time.Time) ([]map[string]any, error)
	GetAllDeviceTypeStats(ctx context.Context, startDate, endDate time.Time) ([]map[string]any, error)
	GetAllShortLinksBasicStats(ctx context.Context, startDate, endDate time.Time) ([]map[string]any, error)
	DeleteByShortLinkID(ctx context.Context, shortLinkID uint) error
}

type shortLinkAccessRepository struct {
	db *gorm.DB
}

// NewShortLinkAccessRepository 创建短链访问统计仓储实例
func NewShortLinkAccessRepository(db *gorm.DB) ShortLinkAccessRepository {
	return &shortLinkAccessRepository{db: db}
}

func (r *shortLinkAccessRepository) Create(ctx context.Context, access *model.ShortLinkAccess) error {
	return r.db.Create(access).Error
}

func (r *shortLinkAccessRepository) DeleteByShortLinkID(ctx context.Context, shortLinkID uint) error {
	return r.db.Where("short_link_id = ?", shortLinkID).Delete(&model.ShortLinkAccess{}).Error
}

func (r *shortLinkAccessRepository) GetByID(ctx context.Context, id uint) (*model.ShortLinkAccess, error) {
	var access model.ShortLinkAccess
	err := r.db.First(&access, id).Error
	if err != nil {
		return nil, err
	}
	return &access, nil
}

func (r *shortLinkAccessRepository) GetByShortLinkID(ctx context.Context, shortLinkID uint, page, pageSize int) ([]*model.ShortLinkAccess, int64, error) {
	var accesses []*model.ShortLinkAccess
	var total int64

	query := r.db.Model(&model.ShortLinkAccess{}).Where("short_link_id = ?", shortLinkID)

	err := query.Count(&total).Error
	if err != nil {
		return nil, 0, err
	}

	offset := (page - 1) * pageSize
	err = query.Offset(offset).Limit(pageSize).Order("access_time DESC").Find(&accesses).Error
	if err != nil {
		return nil, 0, err
	}

	return accesses, total, nil
}

func (r *shortLinkAccessRepository) GetStatsByShortLinkID(ctx context.Context, shortLinkID uint, startDate, endDate time.Time) (*model.ShortLinkAccess, error) {
	var stats struct {
		TotalCount int64 `json:"total_count"`
	}

	query := r.db.Model(&model.ShortLinkAccess{}).Where("short_link_id = ?", shortLinkID)

	if !startDate.IsZero() {
		query = query.Where("DATE(access_time) >= ?", timeutil.BusinessDate(startDate))
	}

	if !endDate.IsZero() {
		query = query.Where("DATE(access_time) <= ?", timeutil.BusinessDate(endDate))
	}

	err := query.Count(&stats.TotalCount).Error
	if err != nil {
		return nil, err
	}

	today := timeutil.BusinessDate(time.Now())

	var todayCount int64
	err = r.db.Model(&model.ShortLinkAccess{}).
		Where("short_link_id = ? AND DATE(access_time) = ?", shortLinkID, today).
		Count(&todayCount).Error
	if err != nil {
		return nil, err
	}

	return &model.ShortLinkAccess{
		ShortLinkID: shortLinkID,
	}, nil
}

func (r *shortLinkAccessRepository) GetDailyStatsByShortLinkID(ctx context.Context, shortLinkID uint, startDate, endDate time.Time) ([]map[string]any, error) {
	query := r.db.Model(&model.ShortLinkAccess{}).
		Select("DATE(access_time) as date, COUNT(*) as count").
		Where("short_link_id = ?", shortLinkID)

	if !startDate.IsZero() {
		query = query.Where("DATE(access_time) >= ?", timeutil.BusinessDate(startDate))
	}

	if !endDate.IsZero() {
		query = query.Where("DATE(access_time) <= ?", timeutil.BusinessDate(endDate))
	}

	var results []map[string]any
	err := query.Group("DATE(access_time)").Order("date").Scan(&results).Error
	return results, err
}

func (r *shortLinkAccessRepository) GetDeviceTypeStatsByShortLinkID(ctx context.Context, shortLinkID uint, startDate, endDate time.Time) ([]map[string]any, error) {
	query := r.db.Model(&model.ShortLinkAccess{}).
		Select("device_type, COUNT(*) as count").
		Where("short_link_id = ?", shortLinkID)

	if !startDate.IsZero() {
		query = query.Where("DATE(access_time) >= ?", timeutil.BusinessDate(startDate))
	}

	if !endDate.IsZero() {
		query = query.Where("DATE(access_time) <= ?", timeutil.BusinessDate(endDate))
	}

	var results []map[string]any
	err := query.Group("device_type").Order("count DESC").Scan(&results).Error
	return results, err
}

func (r *shortLinkAccessRepository) GetAllDailyStats(ctx context.Context, startDate, endDate time.Time) ([]map[string]any, error) {
	query := r.db.Model(&model.ShortLinkAccess{}).
		Select("DATE(access_time) as date, COUNT(*) as count")

	if !startDate.IsZero() {
		query = query.Where("DATE(access_time) >= ?", timeutil.BusinessDate(startDate))
	}

	if !endDate.IsZero() {
		query = query.Where("DATE(access_time) <= ?", timeutil.BusinessDate(endDate))
	}

	var results []map[string]any
	err := query.Group("DATE(access_time)").Order("date").Scan(&results).Error
	return results, err
}

func (r *shortLinkAccessRepository) GetAllDeviceTypeStats(ctx context.Context, startDate, endDate time.Time) ([]map[string]any, error) {
	query := r.db.Model(&model.ShortLinkAccess{}).
		Select("device_type, COUNT(*) as count")

	if !startDate.IsZero() {
		query = query.Where("DATE(access_time) >= ?", timeutil.BusinessDate(startDate))
	}

	if !endDate.IsZero() {
		query = query.Where("DATE(access_time) <= ?", timeutil.BusinessDate(endDate))
	}

	var results []map[string]any
	err := query.Group("device_type").Order("count DESC").Scan(&results).Error
	return results, err
}

func (r *shortLinkAccessRepository) GetAllShortLinksBasicStats(ctx context.Context, startDate, endDate time.Time) ([]map[string]any, error) {
	query := r.db.Table("short_links sl").
		Select("sl.id, sl.short_code, sl.original_url AS title, COUNT(sla.id) as access_count").
		Joins("LEFT JOIN short_link_accesses sla ON sl.id = sla.short_link_id")

	if !startDate.IsZero() {
		query = query.Where("DATE(sla.access_time) >= ? OR sla.access_time IS NULL", timeutil.BusinessDate(startDate))
	}

	if !endDate.IsZero() {
		query = query.Where("DATE(sla.access_time) <= ? OR sla.access_time IS NULL", timeutil.BusinessDate(endDate))
	}

	var results []map[string]any
	err := query.Group("sl.id, sl.short_code, sl.original_url").Order("access_count DESC").Scan(&results).Error
	return results, err
}
