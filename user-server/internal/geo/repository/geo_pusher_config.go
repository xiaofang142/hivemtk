package repository

import (
	"hivemtk-user/internal/geo/model"
	_db "hivemtk-user/internal/pkg/db"

	"gorm.io/gorm"
)

// GeoPusherConfigRepository 蜘蛛平台配置仓储
type GeoPusherConfigRepository interface {
	Create(cfg *model.GeoPusherConfig) error
	GetByID(id string) (*model.GeoPusherConfig, error)
	GetByPlatform(platform string) (*model.GeoPusherConfig, error)
	GetActive() ([]*model.GeoPusherConfig, error)
	GetList(page, limit int) ([]*model.GeoPusherConfig, int64, error)
	Update(cfg *model.GeoPusherConfig) error
	ResetDailyUsed() error
	ConsumeQuota(platform string, n int) error
	Delete(id string) error
}

type geoPusherConfigRepo struct {
	db *gorm.DB
}

func NewGeoPusherConfigRepository() GeoPusherConfigRepository {
	return &geoPusherConfigRepo{db: _db.GetDB()}
}

func NewGeoPusherConfigRepositoryWithDB(db *gorm.DB) GeoPusherConfigRepository {
	return &geoPusherConfigRepo{db: db}
}

func (r *geoPusherConfigRepo) Create(cfg *model.GeoPusherConfig) error {
	return r.db.Create(cfg).Error
}

func (r *geoPusherConfigRepo) GetByID(id string) (*model.GeoPusherConfig, error) {
	var c model.GeoPusherConfig
	err := r.db.Where("id = ?", id).First(&c).Error
	return &c, err
}

func (r *geoPusherConfigRepo) GetByPlatform(platform string) (*model.GeoPusherConfig, error) {
	var c model.GeoPusherConfig
	err := r.db.Where("platform = ? AND active = true", platform).First(&c).Error
	return &c, err
}

func (r *geoPusherConfigRepo) GetActive() ([]*model.GeoPusherConfig, error) {
	var list []*model.GeoPusherConfig
	err := r.db.Where("active = true").Find(&list).Error
	return list, err
}

func (r *geoPusherConfigRepo) GetList(page, limit int) ([]*model.GeoPusherConfig, int64, error) {
	if page <= 0 {
		page = 1
	}
	if limit <= 0 {
		limit = 20
	}
	var list []*model.GeoPusherConfig
	var total int64
	r.db.Model(&model.GeoPusherConfig{}).Count(&total)
	err := r.db.Order("platform ASC").Offset((page - 1) * limit).Limit(limit).Find(&list).Error
	return list, total, err
}

func (r *geoPusherConfigRepo) Update(cfg *model.GeoPusherConfig) error {
	return r.db.Save(cfg).Error
}

func (r *geoPusherConfigRepo) ResetDailyUsed() error {
	return r.db.Model(&model.GeoPusherConfig{}).Update("used_today", 0).Error
}

func (r *geoPusherConfigRepo) ConsumeQuota(platform string, n int) error {
	return r.db.Model(&model.GeoPusherConfig{}).
		Where("platform = ? AND daily_limit - used_today >= ?", platform, n).
		UpdateColumn("used_today", gorm.Expr("used_today + ?", n)).Error
}

func (r *geoPusherConfigRepo) Delete(id string) error {
	return r.db.Delete(&model.GeoPusherConfig{}, "id = ?", id).Error
}
