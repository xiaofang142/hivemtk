package repository

import (
	"hivemtk-user/internal/geo/model"
	_db "hivemtk-user/internal/pkg/db"

	"gorm.io/gorm"
)

// GeoSchemaTemplateRepository Schema 模板库仓储
type GeoSchemaTemplateRepository interface {
	Create(tpl *model.GeoSchemaTemplate) error
	GetByID(id string) (*model.GeoSchemaTemplate, error)
	GetActiveByPageType(pageType string) ([]*model.GeoSchemaTemplate, error)
	GetActiveBySchemaType(schemaType string) (*model.GeoSchemaTemplate, error)
	GetList(page, limit int, pageType, schemaType string) ([]*model.GeoSchemaTemplate, int64, error)
	Update(tpl *model.GeoSchemaTemplate) error
	Delete(id string) error
}

type geoSchemaTemplateRepo struct {
	db *gorm.DB
}

func NewGeoSchemaTemplateRepository() GeoSchemaTemplateRepository {
	return &geoSchemaTemplateRepo{db: _db.GetDB()}
}

func NewGeoSchemaTemplateRepositoryWithDB(db *gorm.DB) GeoSchemaTemplateRepository {
	return &geoSchemaTemplateRepo{db: db}
}

func (r *geoSchemaTemplateRepo) Create(tpl *model.GeoSchemaTemplate) error {
	return r.db.Create(tpl).Error
}

func (r *geoSchemaTemplateRepo) GetByID(id string) (*model.GeoSchemaTemplate, error) {
	var t model.GeoSchemaTemplate
	err := r.db.Where("id = ?", id).First(&t).Error
	return &t, err
}

func (r *geoSchemaTemplateRepo) GetActiveByPageType(pageType string) ([]*model.GeoSchemaTemplate, error) {
	var list []*model.GeoSchemaTemplate
	err := r.db.Where("active = true AND page_type = ?", pageType).Find(&list).Error
	return list, err
}

func (r *geoSchemaTemplateRepo) GetActiveBySchemaType(schemaType string) (*model.GeoSchemaTemplate, error) {
	var t model.GeoSchemaTemplate
	err := r.db.Where("active = true AND schema_type = ?", schemaType).First(&t).Error
	return &t, err
}

func (r *geoSchemaTemplateRepo) GetList(page, limit int, pageType, schemaType string) ([]*model.GeoSchemaTemplate, int64, error) {
	if page <= 0 {
		page = 1
	}
	if limit <= 0 {
		limit = 20
	}
	q := r.db.Model(&model.GeoSchemaTemplate{})
	if pageType != "" {
		q = q.Where("page_type = ?", pageType)
	}
	if schemaType != "" {
		q = q.Where("schema_type = ?", schemaType)
	}
	var total int64
	q.Count(&total)
	var list []*model.GeoSchemaTemplate
	err := q.Order("page_type ASC, schema_type ASC").
		Offset((page - 1) * limit).Limit(limit).Find(&list).Error
	return list, total, err
}

func (r *geoSchemaTemplateRepo) Update(tpl *model.GeoSchemaTemplate) error {
	return r.db.Save(tpl).Error
}

func (r *geoSchemaTemplateRepo) Delete(id string) error {
	return r.db.Delete(&model.GeoSchemaTemplate{}, "id = ?", id).Error
}

var _ = gorm.Expr
