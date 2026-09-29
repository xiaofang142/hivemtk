package repository

import (
	"hivemtk-user/internal/geo/model"
	_db "hivemtk-user/internal/pkg/db"

	"github.com/google/uuid"
	"gorm.io/gorm"
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
	// 必须用原生 SQL：GORM 的 `default:true` 标签会在零值时直接替换绑定变量
	// （DryRun 实测：结构体 Active=false 进 VARS 变成 true），Select 也拦不住；
	// 导致 active=false 永远写不进去（EXCLUDED.active 取到列 DEFAULT true）。
	// 原生 Exec 显式绑定，false 就是 false。BeforeCreate hook 在 Exec 下不触发，
	// 故此处显式补 UUID（与 model.BeforeCreate 保持一致）。
	if d.ID == "" {
		d.ID = uuid.NewString()
	}
	return r.db.Exec(`INSERT INTO geo_dicts (id, category, "key", value, remark, active, sort, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, NOW(), NOW())
		ON CONFLICT (category, "key") DO UPDATE SET
			value = EXCLUDED.value, remark = EXCLUDED.remark, active = EXCLUDED.active,
			sort = EXCLUDED.sort, updated_at = NOW()`,
		d.ID, d.Category, d.Key, d.Value, d.Remark, d.Active, d.Sort).Error
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
