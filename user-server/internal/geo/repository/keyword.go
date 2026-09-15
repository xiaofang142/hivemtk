package repository

import (
	"strings"

	"hivemtk-user/internal/geo/model"
	_db "hivemtk-user/internal/pkg/db"

	"gorm.io/gorm"
)

func escapeLike(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `%`, `\%`)
	s = strings.ReplaceAll(s, `_`, `\_`)
	return s
}

// GeoKeywordRepository GEO 关键词仓储接口
type GeoKeywordRepository interface {
	Create(keyword *model.GeoKeyword) error
	BatchCreate(keywords []*model.GeoKeyword) error
	GetByID(id string) (*model.GeoKeyword, error)
	GetList(search, category, source, cluster, status, layer string, page, limit int) ([]*model.GeoKeyword, int64, error)
	Delete(id string) error
	GetByCluster(cluster string) ([]*model.GeoKeyword, error)
	GetStatistics() ([]map[string]any, error)
}

type geoKeywordRepo struct {
	db *gorm.DB
}

func NewGeoKeywordRepository() GeoKeywordRepository {
	return &geoKeywordRepo{db: _db.GetDB()}
}

// NewGeoKeywordRepositoryWithDB 创建指定数据库连接的实例（用于测试）
func NewGeoKeywordRepositoryWithDB(db *gorm.DB) GeoKeywordRepository {
	return &geoKeywordRepo{db: db}
}

func (r *geoKeywordRepo) Create(keyword *model.GeoKeyword) error {
	return r.db.Create(keyword).Error
}

func (r *geoKeywordRepo) BatchCreate(keywords []*model.GeoKeyword) error {
	if len(keywords) == 0 {
		return nil
	}
	return r.db.CreateInBatches(keywords, 100).Error
}

func (r *geoKeywordRepo) GetByID(id string) (*model.GeoKeyword, error) {
	var keyword model.GeoKeyword
	err := r.db.Where("id = ?", id).First(&keyword).Error
	return &keyword, err
}

func (r *geoKeywordRepo) GetList(search, category, source, cluster, status, layer string, page, limit int) ([]*model.GeoKeyword, int64, error) {
	var keywords []*model.GeoKeyword
	var total int64
	offset := (page - 1) * limit

	query := r.db.Model(&model.GeoKeyword{})

	if search != "" {
		query = query.Where("keyword LIKE ?", "%"+escapeLike(search)+"%")
	}
	if category != "" {
		query = query.Where("category = ?", category)
	}
	if source != "" {
		query = query.Where("source = ?", source)
	}
	if cluster != "" {
		query = query.Where("cluster = ?", cluster)
	}
	if status != "" {
		query = query.Where("status = ?", status)
	}
	if layer != "" {
		query = query.Where("layer = ?", layer)
	}

	err := query.Count(&total).Error
	if err != nil {
		return nil, 0, err
	}

	err = query.Offset(offset).Limit(limit).Order("created_at DESC").Find(&keywords).Error
	return keywords, total, err
}

func (r *geoKeywordRepo) Delete(id string) error {
	return r.db.Where("id = ?", id).Delete(&model.GeoKeyword{}).Error
}

func (r *geoKeywordRepo) GetByCluster(cluster string) ([]*model.GeoKeyword, error) {
	var keywords []*model.GeoKeyword
	err := r.db.Where("cluster = ?", cluster).Order("created_at DESC").Find(&keywords).Error
	return keywords, err
}

func (r *geoKeywordRepo) GetStatistics() ([]map[string]any, error) {
	var statistics []map[string]any
	err := r.db.Raw("SELECT source AS source, COUNT(*) AS total FROM geo_keywords WHERE deleted_at IS NULL GROUP BY source ORDER BY source").Scan(&statistics).Error
	return statistics, err
}
