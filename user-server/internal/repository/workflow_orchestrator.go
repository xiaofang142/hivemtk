package repository

import (
	"context"
	"errors"

	_db "hivemtk-user/internal/pkg/db"
	"time"

	"hivemtk-user/internal/model"

	"gorm.io/gorm"
)

// WorkflowVersionRepository 工作流版本仓储
type WorkflowVersionRepository struct {
	db *gorm.DB
}

// NewWorkflowVersionRepository 创建工作流版本仓储
func NewWorkflowVersionRepository(db *gorm.DB) *WorkflowVersionRepository {
	return &WorkflowVersionRepository{db: db}
}

// Save 保存工作流版本（存在则更新，不存在则创建）。
//
// 双语义：w.ID == 0 走 Create（新建草稿），否则走 db.SaveWholeRow——
// 裸 Save 命中 0 行会回退成 INSERT，把「发布页读出后、保存前被删掉」的版本按原 id 插回。
func (r *WorkflowVersionRepository) Save(ctx context.Context, w *model.WorkflowVersion) error {
	if r == nil || r.db == nil {
		return errors.New("workflow version repository not initialized")
	}
	if w == nil {
		return errors.New("workflow version: nil 不是保存对象")
	}
	if w.ID == 0 {
		return r.db.WithContext(ctx).Create(w).Error
	}
	return _db.SaveWholeRow(r.db.WithContext(ctx), w)
}

// GetByID 按 ID 获取工作流版本
func (r *WorkflowVersionRepository) GetByID(ctx context.Context, id uint) (*model.WorkflowVersion, error) {
	if r == nil || r.db == nil {
		return nil, errors.New("workflow version repository not initialized")
	}
	var w model.WorkflowVersion
	if err := r.db.WithContext(ctx).First(&w, id).Error; err != nil {
		return nil, err
	}
	return &w, nil
}

// GetLatestPublished 按 workflow_id 获取最新的已发布版本
func (r *WorkflowVersionRepository) GetLatestPublished(ctx context.Context, workflowID string) (*model.WorkflowVersion, error) {
	if r == nil || r.db == nil {
		return nil, errors.New("workflow version repository not initialized")
	}
	var w model.WorkflowVersion
	if err := r.db.WithContext(ctx).
		Where("workflow_id = ? AND status = ?", workflowID, model.WorkflowStatusPublished).
		Order("version DESC").
		First(&w).Error; err != nil {
		return nil, err
	}
	return &w, nil
}

// GetByWorkflowIDAndVersion 按 workflow_id + version 精确获取版本（dispatcher 加载定义用）
func (r *WorkflowVersionRepository) GetByWorkflowIDAndVersion(ctx context.Context, workflowID string, version int) (*model.WorkflowVersion, error) {
	if r == nil || r.db == nil {
		return nil, errors.New("workflow version repository not initialized")
	}
	var w model.WorkflowVersion
	if err := r.db.WithContext(ctx).
		Where("workflow_id = ? AND version = ?", workflowID, version).
		First(&w).Error; err != nil {
		return nil, err
	}
	return &w, nil
}

// ListVersions 按 workflow_id 列出所有版本（按 version DESC 排序）
func (r *WorkflowVersionRepository) ListVersions(ctx context.Context, workflowID string) ([]model.WorkflowVersion, error) {
	if r == nil || r.db == nil {
		return nil, errors.New("workflow version repository not initialized")
	}
	var list []model.WorkflowVersion
	if err := r.db.WithContext(ctx).
		Where("workflow_id = ?", workflowID).
		Order("version DESC").
		Find(&list).Error; err != nil {
		return nil, err
	}
	return list, nil
}

// ListAll 列出所有工作流版本，支持按 workflow_id 和 status 过滤，带分页
func (r *WorkflowVersionRepository) ListAll(ctx context.Context, workflowID, status string, page, pageSize int) ([]model.WorkflowVersion, int64, error) {
	if r == nil || r.db == nil {
		return nil, 0, errors.New("workflow version repository not initialized")
	}
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 200 {
		pageSize = 20
	}
	q := r.db.WithContext(ctx).Model(&model.WorkflowVersion{})
	if workflowID != "" {
		q = q.Where("workflow_id = ?", workflowID)
	}
	if status != "" {
		q = q.Where("status = ?", status)
	}
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var list []model.WorkflowVersion
	err := q.Order("updated_at DESC").
		Offset((page - 1) * pageSize).Limit(pageSize).
		Find(&list).Error
	return list, total, err
}

// DeleteByID 按 ID 删除工作流版本
func (r *WorkflowVersionRepository) DeleteByID(ctx context.Context, id uint) error {
	if r == nil || r.db == nil {
		return errors.New("workflow version repository not initialized")
	}
	// D-43：GORM 的 Delete 命中 0 行不报错，控制器就会回「删除成功」——
	// 不存在的版本被当成已删除。按文档级/分段级删除的口径返 not found。
	res := r.db.WithContext(ctx).Where("id = ?", id).Delete(&model.WorkflowVersion{})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

// UpdateStatus 更新版本状态
func (r *WorkflowVersionRepository) UpdateStatus(ctx context.Context, id uint, status string) error {
	if r == nil || r.db == nil {
		return errors.New("workflow version repository not initialized")
	}
	// D-43：同一形状——发布/归档不存在的版本原来静默成功（200「发布成功/归档成功」），
	// 前端会以为状态已变，回读却仍是原记录。0 行受影响即视为不存在。
	res := r.db.WithContext(ctx).Model(&model.WorkflowVersion{}).
		Where("id = ?", id).
		Update("status", status)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

// CountByWorkflowID 统计指定 workflow_id 的版本数（用于自增 version）
func (r *WorkflowVersionRepository) CountByWorkflowID(ctx context.Context, workflowID string) (int64, error) {
	if r == nil || r.db == nil {
		return 0, errors.New("workflow version repository not initialized")
	}
	var count int64
	if err := r.db.WithContext(ctx).Model(&model.WorkflowVersion{}).
		Where("workflow_id = ?", workflowID).
		Count(&count).Error; err != nil {
		return 0, err
	}
	return count, nil
}

// WorkflowExecutionRepository 工作流执行实例仓储
type WorkflowExecutionRepository struct {
	db *gorm.DB
}

// NewWorkflowExecutionRepository 创建工作流执行实例仓储
func NewWorkflowExecutionRepository(db *gorm.DB) *WorkflowExecutionRepository {
	return &WorkflowExecutionRepository{db: db}
}

// Create 创建执行实例
func (r *WorkflowExecutionRepository) Create(ctx context.Context, e *model.WorkflowExecution) error {
	if r == nil || r.db == nil {
		return errors.New("workflow execution repository not initialized")
	}
	return r.db.WithContext(ctx).Create(e).Error
}

// GetByID 按 ID 获取执行实例
func (r *WorkflowExecutionRepository) GetByID(ctx context.Context, id uint) (*model.WorkflowExecution, error) {
	if r == nil || r.db == nil {
		return nil, errors.New("workflow execution repository not initialized")
	}
	var e model.WorkflowExecution
	if err := r.db.WithContext(ctx).First(&e, id).Error; err != nil {
		return nil, err
	}
	return &e, nil
}

// Update 整行回写执行实例。走 db.SaveWholeRow（理由同版本：dispatcher 是
// Get 后改状态再回写，行被并发清掉时裸 Save 会把执行实例按原 id 插回 ——
// 一个「已不存在」的执行还在跑）。
func (r *WorkflowExecutionRepository) Update(ctx context.Context, e *model.WorkflowExecution) error {
	if r == nil || r.db == nil {
		return errors.New("workflow execution repository not initialized")
	}
	if e == nil || e.ID == 0 {
		return errors.New("workflow execution: 缺主键，不能整行回写")
	}
	return _db.SaveWholeRow(r.db.WithContext(ctx), e)
}

// UpdateFields 按字段更新
func (r *WorkflowExecutionRepository) UpdateFields(ctx context.Context, id uint, fields map[string]any) error {
	if r == nil || r.db == nil {
		return errors.New("workflow execution repository not initialized")
	}
	return r.db.WithContext(ctx).Model(&model.WorkflowExecution{}).
		Where("id = ?", id).
		Updates(fields).Error
}

// List 列出执行实例
func (r *WorkflowExecutionRepository) List(ctx context.Context, workflowID, status string, page, pageSize int) ([]model.WorkflowExecution, int64, error) {
	if r == nil || r.db == nil {
		return nil, 0, errors.New("workflow execution repository not initialized")
	}
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 200 {
		pageSize = 20
	}
	q := r.db.WithContext(ctx).Model(&model.WorkflowExecution{})
	if workflowID != "" {
		q = q.Where("workflow_id = ?", workflowID)
	}
	if status != "" {
		q = q.Where("status = ?", status)
	}
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var list []model.WorkflowExecution
	err := q.Order("started_at DESC").
		Offset((page - 1) * pageSize).Limit(pageSize).
		Find(&list).Error
	return list, total, err
}

// FindStuck 查询卡死的执行实例
func (r *WorkflowExecutionRepository) FindStuck(ctx context.Context, threshold time.Time, limit int) ([]model.WorkflowExecution, error) {
	if r == nil || r.db == nil {
		return nil, errors.New("workflow execution repository not initialized")
	}
	if limit <= 0 {
		limit = 50
	}
	var list []model.WorkflowExecution
	err := r.db.WithContext(ctx).
		Where("status = ? AND started_at < ?", model.WorkflowExecRunning, threshold).
		Order("started_at ASC").
		Limit(limit).
		Find(&list).Error
	return list, err
}

// WorkflowNodeExecutionRepository 节点执行明细仓储
type WorkflowNodeExecutionRepository struct {
	db *gorm.DB
}

// NewWorkflowNodeExecutionRepository 创建节点执行明细仓储
func NewWorkflowNodeExecutionRepository(db *gorm.DB) *WorkflowNodeExecutionRepository {
	return &WorkflowNodeExecutionRepository{db: db}
}

// Create 创建节点执行记录
func (r *WorkflowNodeExecutionRepository) Create(ctx context.Context, n *model.WorkflowNodeExecution) error {
	if r == nil || r.db == nil {
		return errors.New("workflow node execution repository not initialized")
	}
	return r.db.WithContext(ctx).Create(n).Error
}

// Update 整行回写节点执行记录。走 db.SaveWholeRow（理由同执行实例）。
func (r *WorkflowNodeExecutionRepository) Update(ctx context.Context, n *model.WorkflowNodeExecution) error {
	if r == nil || r.db == nil {
		return errors.New("workflow node execution repository not initialized")
	}
	if n == nil || n.ID == 0 {
		return errors.New("workflow node execution: 缺主键，不能整行回写")
	}
	return _db.SaveWholeRow(r.db.WithContext(ctx), n)
}

// ListByExecutionID 按 execution_id 列出节点执行明细（按 started_at ASC）
func (r *WorkflowNodeExecutionRepository) ListByExecutionID(ctx context.Context, executionID uint) ([]model.WorkflowNodeExecution, error) {
	if r == nil || r.db == nil {
		return nil, errors.New("workflow node execution repository not initialized")
	}
	var list []model.WorkflowNodeExecution
	if err := r.db.WithContext(ctx).
		Where("execution_id = ?", executionID).
		Order("started_at ASC").
		Find(&list).Error; err != nil {
		return nil, err
	}
	return list, nil
}
