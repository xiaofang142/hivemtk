package repository

import (
	"context"
	"time"

	"hivemtk-user/internal/geo/model"

	"gorm.io/gorm"
)

// workflowListLimit 工作流/模板/执行记录列表返回上限：三者都是单页展示用，
// 不封顶时租户攒够数据会把整表读进内存。
const workflowListLimit = 200

// GeoWorkflowRepository GEO 工作流仓储接口
type GeoWorkflowRepository interface {
	Create(ctx context.Context, wf *model.GeoWorkflow) error
	Update(ctx context.Context, wf *model.GeoWorkflow) error
	Delete(ctx context.Context, id string) error
	GetByID(ctx context.Context, id string) (*model.GeoWorkflow, error)
	GetList(ctx context.Context) ([]*model.GeoWorkflow, error)
}

type geoWorkflowRepo struct {
	db *gorm.DB
}

func NewGeoWorkflowRepositoryWithDB(db *gorm.DB) GeoWorkflowRepository {
	return &geoWorkflowRepo{db: db}
}

func (r *geoWorkflowRepo) Create(ctx context.Context, wf *model.GeoWorkflow) error {
	return r.db.WithContext(ctx).Create(wf).Error
}

// Update 只写可变字段：Save 会连 tenant_id 一起回写，且 enabled=false 这类零值
// 依赖 map 显式赋值才能落库。
func (r *geoWorkflowRepo) Update(ctx context.Context, wf *model.GeoWorkflow) error {
	res := r.db.WithContext(ctx).Model(&model.GeoWorkflow{}).
		Where("id = ?", wf.ID).
		Updates(map[string]any{
			"name":       wf.Name,
			"steps":      wf.Steps,
			"conditions": wf.Conditions,
			"schedule":   wf.Schedule,
			"enabled":    wf.Enabled,
			"updated_at": time.Now(),
		})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

// Delete 0 行说明工作流不存在（含跨租户），不能当作删除成功。
func (r *geoWorkflowRepo) Delete(ctx context.Context, id string) error {
	res := r.db.WithContext(ctx).Where("id = ?", id).Delete(&model.GeoWorkflow{})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

func (r *geoWorkflowRepo) GetByID(ctx context.Context, id string) (*model.GeoWorkflow, error) {
	var wf model.GeoWorkflow
	err := r.db.WithContext(ctx).Where("id = ?", id).First(&wf).Error
	return &wf, err
}

// GetList 工作流列表，按创建时间倒序并封顶，避免无界查询。
func (r *geoWorkflowRepo) GetList(ctx context.Context) ([]*model.GeoWorkflow, error) {
	var wfs []*model.GeoWorkflow
	err := r.db.WithContext(ctx).Order("created_at DESC").Limit(workflowListLimit).Find(&wfs).Error
	return wfs, err
}

// GeoWorkflowExecutionRepository GEO 工作流执行记录仓储接口
type GeoWorkflowExecutionRepository interface {
	Create(ctx context.Context, exec *model.GeoWorkflowExecution) error
	Update(ctx context.Context, exec *model.GeoWorkflowExecution) error
	GetByWorkflowID(ctx context.Context, workflowID string) ([]*model.GeoWorkflowExecution, error)
}

type geoWorkflowExecRepo struct {
	db *gorm.DB
}

func NewGeoWorkflowExecutionRepositoryWithDB(db *gorm.DB) GeoWorkflowExecutionRepository {
	return &geoWorkflowExecRepo{db: db}
}

func (r *geoWorkflowExecRepo) Create(ctx context.Context, exec *model.GeoWorkflowExecution) error {
	return r.db.WithContext(ctx).Create(exec).Error
}

// Update 落执行终态；0 行说明执行记录已不存在（含跨租户），必须报不存在，
// 否则调用方会把"没写进去"当成"已写进去"。
func (r *geoWorkflowExecRepo) Update(ctx context.Context, exec *model.GeoWorkflowExecution) error {
	res := r.db.WithContext(ctx).Model(&model.GeoWorkflowExecution{}).Where("id = ?", exec.ID).
		Updates(map[string]any{
			"status":       exec.Status,
			"progress":     exec.Progress,
			"current_step": exec.CurrentStep,
			"completed_at": exec.CompletedAt,
			"result":       exec.Result,
			"error":        exec.Error,
			"updated_at":   time.Now(),
		})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

func (r *geoWorkflowExecRepo) GetByWorkflowID(ctx context.Context, workflowID string) ([]*model.GeoWorkflowExecution, error) {
	var execs []*model.GeoWorkflowExecution
	err := r.db.WithContext(ctx).Where("workflow_id = ?", workflowID).
		Order("started_at DESC").Limit(workflowListLimit).Find(&execs).Error
	return execs, err
}

// GeoWorkflowTemplateRepository GEO 工作流模板仓储接口
type GeoWorkflowTemplateRepository interface {
	Create(ctx context.Context, tpl *model.GeoWorkflowTemplate) error
	GetList(ctx context.Context) ([]*model.GeoWorkflowTemplate, error)
}

type geoWorkflowTplRepo struct {
	db *gorm.DB
}

func NewGeoWorkflowTemplateRepositoryWithDB(db *gorm.DB) GeoWorkflowTemplateRepository {
	return &geoWorkflowTplRepo{db: db}
}

func (r *geoWorkflowTplRepo) Create(ctx context.Context, tpl *model.GeoWorkflowTemplate) error {
	return r.db.WithContext(ctx).Create(tpl).Error
}

func (r *geoWorkflowTplRepo) GetList(ctx context.Context) ([]*model.GeoWorkflowTemplate, error) {
	var tpls []*model.GeoWorkflowTemplate
	err := r.db.WithContext(ctx).Order("created_at DESC").Limit(workflowListLimit).Find(&tpls).Error
	return tpls, err
}
