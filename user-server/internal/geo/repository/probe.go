package repository

import (
	"context"
	"time"

	"hivemtk-user/internal/geo/model"
	_db "hivemtk-user/internal/pkg/db"

	"gorm.io/gorm"
)

// GeoProbeRunRepository interface {
type GeoProbeRunRepository interface {
	Create(ctx context.Context, run *model.GeoProbeRun) error
	BatchCreate(ctx context.Context, runs []*model.GeoProbeRun) error
	ListByEngine(ctx context.Context, engine string, page, limit int) ([]*model.GeoProbeRun, int64, error)
	ListRecent(ctx context.Context, limit int) ([]*model.GeoProbeRun, error)
	ListSince(ctx context.Context, since time.Time, limit int) ([]*model.GeoProbeRun, error)
	ListBetween(ctx context.Context, since, until time.Time) ([]*model.GeoProbeRun, error)
	ListByIntent(ctx context.Context, intent string, since time.Time) ([]*model.GeoProbeRun, error)
	DistinctEngines(ctx context.Context) ([]string, error)
}

type geoProbeRunRepo struct{ db *gorm.DB }

func NewGeoProbeRunRepository() GeoProbeRunRepository {
	return &geoProbeRunRepo{db: _db.GetDB()}
}
func NewGeoProbeRunRepositoryWithDB(db *gorm.DB) GeoProbeRunRepository {
	return &geoProbeRunRepo{db: db}
}

func (r *geoProbeRunRepo) Create(ctx context.Context, run *model.GeoProbeRun) error {
	return r.db.WithContext(ctx).Create(run).Error
}

func (r *geoProbeRunRepo) BatchCreate(ctx context.Context, runs []*model.GeoProbeRun) error {
	if len(runs) == 0 {
		return nil
	}
	return r.db.WithContext(ctx).CreateInBatches(runs, 50).Error
}

func (r *geoProbeRunRepo) ListByEngine(ctx context.Context, engine string, page, limit int) ([]*model.GeoProbeRun, int64, error) {
	if page <= 0 {
		page = 1
	}
	if limit <= 0 {
		limit = 20
	}
	var list []*model.GeoProbeRun
	var total int64
	q := r.db.WithContext(ctx).Model(&model.GeoProbeRun{})
	if engine != "" {
		q = q.Where("engine = ?", engine)
	}
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	err := q.Offset((page - 1) * limit).Limit(limit).Order("created_at DESC").Find(&list).Error
	return list, total, err
}

func (r *geoProbeRunRepo) ListRecent(ctx context.Context, limit int) ([]*model.GeoProbeRun, error) {
	if limit <= 0 {
		limit = 50
	}
	var list []*model.GeoProbeRun
	err := r.db.WithContext(ctx).Order("created_at DESC").Limit(limit).Find(&list).Error
	return list, err
}

func (r *geoProbeRunRepo) ListByIntent(ctx context.Context, intent string, since time.Time) ([]*model.GeoProbeRun, error) {

	var list []*model.GeoProbeRun
	q := r.db.WithContext(ctx)
	if !since.IsZero() {
		q = q.Where("created_at >= ?", since)
	}
	err := q.Order("created_at DESC").Find(&list).Error
	return list, err
}

func (r *geoProbeRunRepo) DistinctEngines(ctx context.Context) ([]string, error) {
	var engines []string
	err := r.db.WithContext(ctx).Model(&model.GeoProbeRun{}).
		Distinct("engine").
		Where("engine != ''").
		Pluck("engine", &engines).Error
	return engines, err
}

func (r *geoProbeRunRepo) ListSince(ctx context.Context, since time.Time, limit int) ([]*model.GeoProbeRun, error) {
	q := r.db.WithContext(ctx).Where("created_at >= ?", since).Order("created_at ASC")
	if limit > 0 {
		q = q.Limit(limit)
	}
	var list []*model.GeoProbeRun
	err := q.Find(&list).Error
	return list, err
}

// ListBetween 时间窗口 [since, until) 内的探针记录（until 为零值表示不限）
func (r *geoProbeRunRepo) ListBetween(ctx context.Context, since, until time.Time) ([]*model.GeoProbeRun, error) {
	q := r.db.WithContext(ctx).Where("created_at >= ?", since)
	if !until.IsZero() {
		q = q.Where("created_at < ?", until)
	}
	var list []*model.GeoProbeRun
	err := q.Order("created_at ASC").Find(&list).Error
	return list, err
}
