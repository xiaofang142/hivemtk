package repository

import (
	"hivemtk-user/internal/geo/model"
	_db "hivemtk-user/internal/pkg/db"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// GeoDictRepository GEO 通用配置字典
type GeoDictRepository interface {
	// Get 按类目+键取单条（仅启用）
	Get(category, key string) (*model.GeoDict, error)
	// GetAny 按类目+键取单条（含停用，用于区分“缺行”与“已停用”，避免停用被自动播种复活）
	GetAny(category, key string) (*model.GeoDict, error)
	// ListByCategory 按类目列出全部启用条目（sort 升序）
	ListByCategory(category string) ([]*model.GeoDict, error)
	// Upsert 按 (category,key) 幂等写入
	Upsert(d *model.GeoDict) error
	// Delete 按类目+键删除
	Delete(category, key string) error
	// CountByCategory 统计类目下条目数
	CountByCategory(category string) (int64, error)
}

type geoDictRepo struct {
	db *gorm.DB
}

// NewGeoDictRepository 构造 repository（使用默认 DB）
func NewGeoDictRepository() GeoDictRepository {
	return &geoDictRepo{db: _db.GetDB()}
}

// NewGeoDictRepositoryWithDB 构造 repository（使用指定 DB，用于路由注入和测试）
func NewGeoDictRepositoryWithDB(db *gorm.DB) GeoDictRepository {
	return &geoDictRepo{db: db}
}

func (r *geoDictRepo) Get(category, key string) (*model.GeoDict, error) {
	var d model.GeoDict
	if err := r.db.Where("category = ? AND \"key\" = ? AND active = ?", category, key, true).First(&d).Error; err != nil {
		return nil, err
	}
	return &d, nil
}

func (r *geoDictRepo) GetAny(category, key string) (*model.GeoDict, error) {
	var d model.GeoDict
	if err := r.db.Where("category = ? AND \"key\" = ?", category, key).First(&d).Error; err != nil {
		return nil, err
	}
	return &d, nil
}

func (r *geoDictRepo) ListByCategory(category string) ([]*model.GeoDict, error) {
	var list []*model.GeoDict
	if err := r.db.Where("category = ? AND active = ?", category, true).Order("\"sort\" ASC, \"key\" ASC").Find(&list).Error; err != nil {
		return nil, err
	}
	return list, nil
}

func (r *geoDictRepo) Upsert(d *model.GeoDict) error {
	if d == nil {
		return nil
	}
	// 必须显式 Select 全列：GORM Create 默认省略零值字段，否则 active=false
	// 进不了 INSERT 列，EXCLUDED.active 取列 DEFAULT(true)，停用永远写不进去。
	return r.db.Select("category", "key", "value", "remark", "active", "sort").Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "category"}, {Name: "key"}},
		DoUpdates: clause.AssignmentColumns([]string{"value", "remark", "active", "sort", "updated_at"}),
	}).Create(d).Error
}

func (r *geoDictRepo) Delete(category, key string) error {
	return r.db.Where("category = ? AND \"key\" = ?", category, key).Delete(&model.GeoDict{}).Error
}

func (r *geoDictRepo) CountByCategory(category string) (int64, error) {
	var n int64
	if err := r.db.Model(&model.GeoDict{}).Where("category = ?", category).Count(&n).Error; err != nil {
		return 0, err
	}
	return n, nil
}
