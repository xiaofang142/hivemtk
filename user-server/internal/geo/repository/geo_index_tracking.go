package repository

import (
	"time"

	"hivemtk-user/internal/geo/model"
	_db "hivemtk-user/internal/pkg/db"

	"gorm.io/gorm"
)

// GeoIndexTrackingRepository 收录 + AI 引用追踪仓储
type GeoIndexTrackingRepository interface {
	Upsert(tracking *model.GeoIndexTracking) error
	GetByArticle(articleID string) ([]*model.GeoIndexTracking, error)
	GetByEngine(engine string) ([]*model.GeoIndexTracking, error)
	GetList(page, limit int, articleID, engine string) ([]*model.GeoIndexTracking, int64, error)
	FunnelCount() (int64, error)
	CountAICitations() (int64, error)
	CountIndexed() (int64, error)
	UpdateByArticleAndEngine(articleID, engine string, updates map[string]any) error
}

type geoIndexTrackingRepo struct {
	db *gorm.DB
}

func NewGeoIndexTrackingRepository() GeoIndexTrackingRepository {
	return &geoIndexTrackingRepo{db: _db.GetDB()}
}

func NewGeoIndexTrackingRepositoryWithDB(db *gorm.DB) GeoIndexTrackingRepository {
	return &geoIndexTrackingRepo{db: db}
}

func (r *geoIndexTrackingRepo) Upsert(tracking *model.GeoIndexTracking) error {
	var existing model.GeoIndexTracking
	err := r.db.Where("article_id = ? AND engine = ?", tracking.ArticleID, tracking.Engine).
		First(&existing).Error
	if err == nil {
		tracking.ID = existing.ID
		return r.db.Save(tracking).Error
	}
	return r.db.Create(tracking).Error
}

func (r *geoIndexTrackingRepo) GetByArticle(articleID string) ([]*model.GeoIndexTracking, error) {
	var list []*model.GeoIndexTracking
	err := r.db.Where("article_id = ?", articleID).Find(&list).Error
	return list, err
}

func (r *geoIndexTrackingRepo) GetByEngine(engine string) ([]*model.GeoIndexTracking, error) {
	var list []*model.GeoIndexTracking
	err := r.db.Where("engine = ?", engine).Find(&list).Error
	return list, err
}

func (r *geoIndexTrackingRepo) GetList(page, limit int, articleID, engine string) ([]*model.GeoIndexTracking, int64, error) {
	if page <= 0 {
		page = 1
	}
	if limit <= 0 {
		limit = 50
	}
	q := r.db.Model(&model.GeoIndexTracking{})
	if articleID != "" {
		q = q.Where("article_id = ?", articleID)
	}
	if engine != "" {
		q = q.Where("engine = ?", engine)
	}
	var total int64
	q.Count(&total)
	var list []*model.GeoIndexTracking
	err := q.Order("last_checked DESC NULLS LAST").Offset((page - 1) * limit).Limit(limit).Find(&list).Error
	return list, total, err
}

func (r *geoIndexTrackingRepo) FunnelCount() (int64, error) {
	var count int64
	err := r.db.Model(&model.GeoIndexTracking{}).Distinct("article_id").Count(&count).Error
	return count, err
}

func (r *geoIndexTrackingRepo) CountAICitations() (int64, error) {
	var count int64
	err := r.db.Model(&model.GeoIndexTracking{}).
		Where("ai_cited = true").Count(&count).Error
	return count, err
}

func (r *geoIndexTrackingRepo) CountIndexed() (int64, error) {
	var count int64
	err := r.db.Model(&model.GeoIndexTracking{}).
		Where("indexed = true").Count(&count).Error
	return count, err
}

func (r *geoIndexTrackingRepo) UpdateByArticleAndEngine(articleID, engine string, updates map[string]any) error {
	if updates == nil {
		updates["last_checked"] = time.Now()
	} else {
		updates["last_checked"] = time.Now()
	}
	return r.db.Model(&model.GeoIndexTracking{}).
		Where("article_id = ? AND engine = ?", articleID, engine).
		Updates(updates).Error
}

// gorm import used indirectly
var _ = gorm.Expr
