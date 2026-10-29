package repository

import (
	"context"

	"hivemtk-user/internal/geo/model"
	_db "hivemtk-user/internal/pkg/db"

	"gorm.io/gorm"
)

// GeoAutopilotRunRepository 一键自动驾驶运行记录仓储
type GeoAutopilotRunRepository interface {
	Create(ctx context.Context, run *model.GeoAutopilotRun) error
	Update(ctx context.Context, run *model.GeoAutopilotRun) error
	GetByID(ctx context.Context, id string) (*model.GeoAutopilotRun, error)
	List(ctx context.Context, page, limit int) ([]*model.GeoAutopilotRun, int64, error)
	// HasRunning 同租户是否已有 running 记录（并发闸）
	HasRunning(ctx context.Context) (bool, error)
}

type geoAutopilotRunRepo struct {
	db *gorm.DB
}

func NewGeoAutopilotRunRepository() GeoAutopilotRunRepository {
	return &geoAutopilotRunRepo{db: _db.GetDB()}
}

// NewGeoAutopilotRunRepositoryWithDB 创建指定数据库连接的实例（用于测试）
func NewGeoAutopilotRunRepositoryWithDB(db *gorm.DB) GeoAutopilotRunRepository {
	return &geoAutopilotRunRepo{db: db}
}

func (r *geoAutopilotRunRepo) Create(ctx context.Context, run *model.GeoAutopilotRun) error {
	return r.db.WithContext(ctx).Create(run).Error
}

func (r *geoAutopilotRunRepo) Update(ctx context.Context, run *model.GeoAutopilotRun) error {
	return r.db.WithContext(ctx).Save(run).Error
}

func (r *geoAutopilotRunRepo) GetByID(ctx context.Context, id string) (*model.GeoAutopilotRun, error) {
	var run model.GeoAutopilotRun
	if err := r.db.WithContext(ctx).Where("id = ?", id).First(&run).Error; err != nil {
		return nil, err
	}
	return &run, nil
}

func (r *geoAutopilotRunRepo) List(ctx context.Context, page, limit int) ([]*model.GeoAutopilotRun, int64, error) {
	if page < 1 {
		page = 1
	}
	if limit < 1 || limit > 100 {
		limit = 20
	}
	var total int64
	if err := r.db.WithContext(ctx).Model(&model.GeoAutopilotRun{}).Count(&total).Error; err != nil {
		return nil, 0, err
	}
	rows := make([]*model.GeoAutopilotRun, 0, limit)
	err := r.db.WithContext(ctx).
		Order("created_at DESC").
		Offset((page - 1) * limit).Limit(limit).
		Find(&rows).Error
	return rows, total, err
}

func (r *geoAutopilotRunRepo) HasRunning(ctx context.Context) (bool, error) {
	var n int64
	err := r.db.WithContext(ctx).Model(&model.GeoAutopilotRun{}).
		Where("status = ?", "running").Count(&n).Error
	return n > 0, err
}
