package repository

import (
	"time"

	"hivemtk-user/internal/geo/model"
	_db "hivemtk-user/internal/pkg/db"

	"gorm.io/gorm"
)

// GeoSiteRepository 站点部署配置仓储
type GeoSiteRepository interface {
	Create(site *model.GeoSite) error
	GetByID(id string) (*model.GeoSite, error)
	GetByDomain(domain string) (*model.GeoSite, error)
	GetActive() (*model.GeoSite, error)
	GetList(page, limit int) ([]*model.GeoSite, int64, error)
	Update(site *model.GeoSite) error
	UpdateDeploy(id string, deployAt time.Time) error
	Delete(id string) error
}

type geoSiteRepo struct {
	db *gorm.DB
}

func NewGeoSiteRepository() GeoSiteRepository {
	return &geoSiteRepo{db: _db.GetDB()}
}

func NewGeoSiteRepositoryWithDB(db *gorm.DB) GeoSiteRepository {
	return &geoSiteRepo{db: db}
}

func (r *geoSiteRepo) Create(site *model.GeoSite) error {
	return r.db.Create(site).Error
}

func (r *geoSiteRepo) GetByID(id string) (*model.GeoSite, error) {
	var s model.GeoSite
	err := r.db.Where("id = ?", id).First(&s).Error
	return &s, err
}

func (r *geoSiteRepo) GetByDomain(domain string) (*model.GeoSite, error) {
	var s model.GeoSite
	err := r.db.Where("domain = ?", domain).First(&s).Error
	return &s, err
}

func (r *geoSiteRepo) GetActive() (*model.GeoSite, error) {
	var s model.GeoSite
	err := r.db.Where("active = true").First(&s).Error
	return &s, err
}

func (r *geoSiteRepo) GetList(page, limit int) ([]*model.GeoSite, int64, error) {
	if page <= 0 {
		page = 1
	}
	if limit <= 0 {
		limit = 20
	}
	var list []*model.GeoSite
	var total int64
	r.db.Model(&model.GeoSite{}).Count(&total)
	err := r.db.Order("created_at DESC").Offset((page - 1) * limit).Limit(limit).Find(&list).Error
	return list, total, err
}

func (r *geoSiteRepo) Update(site *model.GeoSite) error {
	return r.db.Save(site).Error
}

func (r *geoSiteRepo) UpdateDeploy(id string, deployAt time.Time) error {
	return r.db.Model(&model.GeoSite{}).Where("id = ?", id).Update("last_deploy_at", deployAt).Error
}

func (r *geoSiteRepo) Delete(id string) error {
	return r.db.Delete(&model.GeoSite{}, "id = ?", id).Error
}
