package repository

import (
	"hivemtk-user/internal/geo/model"

	"gorm.io/gorm"
)

// GeoWorkflowRepository GEO 工作流仓储接口
type GeoWorkflowRepository interface {
	Create(wf *model.GeoWorkflow) error
	Update(wf *model.GeoWorkflow) error
	Delete(id string) error
	GetByID(id string) (*model.GeoWorkflow, error)
	GetList() ([]*model.GeoWorkflow, error)
}

type geoWorkflowRepo struct {
	db *gorm.DB
}

func NewGeoWorkflowRepositoryWithDB(db *gorm.DB) GeoWorkflowRepository {
	return &geoWorkflowRepo{db: db}
}

func (r *geoWorkflowRepo) Create(wf *model.GeoWorkflow) error {
	return r.db.Create(wf).Error
}

func (r *geoWorkflowRepo) Update(wf *model.GeoWorkflow) error {
	return r.db.Save(wf).Error
}

func (r *geoWorkflowRepo) Delete(id string) error {
	return r.db.Where("id = ?", id).Delete(&model.GeoWorkflow{}).Error
}

func (r *geoWorkflowRepo) GetByID(id string) (*model.GeoWorkflow, error) {
	var wf model.GeoWorkflow
	err := r.db.Where("id = ?", id).First(&wf).Error
	return &wf, err
}

func (r *geoWorkflowRepo) GetList() ([]*model.GeoWorkflow, error) {
	var wfs []*model.GeoWorkflow
	err := r.db.Order("created_at DESC").Find(&wfs).Error
	return wfs, err
}

// GeoWorkflowExecutionRepository GEO 工作流执行记录仓储接口
type GeoWorkflowExecutionRepository interface {
	Create(exec *model.GeoWorkflowExecution) error
	Update(exec *model.GeoWorkflowExecution) error
	GetByID(id string) (*model.GeoWorkflowExecution, error)
	GetByWorkflowID(workflowID string) ([]*model.GeoWorkflowExecution, error)
}

type geoWorkflowExecRepo struct {
	db *gorm.DB
}

func NewGeoWorkflowExecutionRepositoryWithDB(db *gorm.DB) GeoWorkflowExecutionRepository {
	return &geoWorkflowExecRepo{db: db}
}

func (r *geoWorkflowExecRepo) Create(exec *model.GeoWorkflowExecution) error {
	return r.db.Create(exec).Error
}

func (r *geoWorkflowExecRepo) Update(exec *model.GeoWorkflowExecution) error {
	return r.db.Model(&model.GeoWorkflowExecution{}).Where("id = ?", exec.ID).
		Updates(map[string]any{
			"status":       exec.Status,
			"completed_at": exec.CompletedAt,
			"result":       exec.Result,
			"error":        exec.Error,
		}).Error
}

func (r *geoWorkflowExecRepo) GetByID(id string) (*model.GeoWorkflowExecution, error) {
	var exec model.GeoWorkflowExecution
	err := r.db.Where("id = ?", id).First(&exec).Error
	return &exec, err
}

func (r *geoWorkflowExecRepo) GetByWorkflowID(workflowID string) ([]*model.GeoWorkflowExecution, error) {
	var execs []*model.GeoWorkflowExecution
	err := r.db.Where("workflow_id = ?", workflowID).Order("started_at DESC").Find(&execs).Error
	return execs, err
}

// GeoWorkflowTemplateRepository GEO 工作流模板仓储接口
type GeoWorkflowTemplateRepository interface {
	Create(tpl *model.GeoWorkflowTemplate) error
	Delete(id string) error
	GetByID(id string) (*model.GeoWorkflowTemplate, error)
	GetList() ([]*model.GeoWorkflowTemplate, error)
}

type geoWorkflowTplRepo struct {
	db *gorm.DB
}

func NewGeoWorkflowTemplateRepositoryWithDB(db *gorm.DB) GeoWorkflowTemplateRepository {
	return &geoWorkflowTplRepo{db: db}
}

func (r *geoWorkflowTplRepo) Create(tpl *model.GeoWorkflowTemplate) error {
	return r.db.Create(tpl).Error
}

func (r *geoWorkflowTplRepo) Delete(id string) error {
	return r.db.Where("id = ?", id).Delete(&model.GeoWorkflowTemplate{}).Error
}

func (r *geoWorkflowTplRepo) GetByID(id string) (*model.GeoWorkflowTemplate, error) {
	var tpl model.GeoWorkflowTemplate
	err := r.db.Where("id = ?", id).First(&tpl).Error
	return &tpl, err
}

func (r *geoWorkflowTplRepo) GetList() ([]*model.GeoWorkflowTemplate, error) {
	var tpls []*model.GeoWorkflowTemplate
	err := r.db.Order("created_at DESC").Find(&tpls).Error
	return tpls, err
}
