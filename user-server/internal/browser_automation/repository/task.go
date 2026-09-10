package repository

import (
	"context"
	"errors"
	"time"

	"hivemtk-user/internal/browser_automation/model"
	_db "hivemtk-user/internal/pkg/db"

	"gorm.io/gorm"
)

// BrowserTaskRepository 任务仓储。除 GetByIDAnyUser 外全部强制 userID 过滤，防越权串号。
type BrowserTaskRepository interface {
	Create(ctx context.Context, t *model.BrowserTask) error
	GetByID(ctx context.Context, id, userID uint) (*model.BrowserTask, error)
	// GetByIDAnyUser 仅用于依赖检查（读取前置任务的归属/状态），业务读写仍必须走 GetByID
	GetByIDAnyUser(ctx context.Context, id uint) (*model.BrowserTask, error)
	List(ctx context.Context, userID uint, status, taskType string, page, limit int) ([]*model.BrowserTask, int64, error)
	Update(ctx context.Context, t *model.BrowserTask) error
	UpdateStatus(ctx context.Context, id uint, status, errMsg string) error
	UpdateRunResult(ctx context.Context, id uint, status, lastResult, errMsg string, retryCount int) error
	SoftDelete(ctx context.Context, id, userID uint) error
	FindRunning(ctx context.Context, userID uint) ([]*model.BrowserTask, error)
	ListDependents(ctx context.Context, taskID uint) ([]*model.BrowserTask, error)
}

type browserTaskRepo struct {
	db *gorm.DB
}

func NewBrowserTaskRepository() BrowserTaskRepository {
	return &browserTaskRepo{db: _db.GetDB()}
}

// NewBrowserTaskRepositoryWithDB 显式注入 gormDB（路由装配用，测试可替换）
func NewBrowserTaskRepositoryWithDB(db *gorm.DB) BrowserTaskRepository {
	return &browserTaskRepo{db: db}
}

func (r *browserTaskRepo) Create(ctx context.Context, t *model.BrowserTask) error {
	return r.db.WithContext(ctx).Create(t).Error
}

func (r *browserTaskRepo) GetByID(ctx context.Context, id, userID uint) (*model.BrowserTask, error) {
	var t model.BrowserTask
	err := r.db.WithContext(ctx).Where("id = ? AND user_id = ?", id, userID).First(&t).Error
	if err != nil {
		return nil, err
	}
	return &t, nil
}

func (r *browserTaskRepo) GetByIDAnyUser(ctx context.Context, id uint) (*model.BrowserTask, error) {
	var t model.BrowserTask
	err := r.db.WithContext(ctx).Where("id = ?", id).First(&t).Error
	if err != nil {
		return nil, err
	}
	return &t, nil
}

func (r *browserTaskRepo) List(ctx context.Context, userID uint, status, taskType string, page, limit int) ([]*model.BrowserTask, int64, error) {
	if page <= 0 {
		page = 1
	}
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	q := r.db.WithContext(ctx).Model(&model.BrowserTask{}).Where("user_id = ?", userID)
	if status != "" {
		q = q.Where("status = ?", status)
	}
	if taskType != "" {
		q = q.Where("task_type = ?", taskType)
	}
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var list []*model.BrowserTask
	if err := q.Order("id DESC").Offset((page - 1) * limit).Limit(limit).Find(&list).Error; err != nil {
		return nil, 0, err
	}
	return list, total, nil
}

func (r *browserTaskRepo) Update(ctx context.Context, t *model.BrowserTask) error {
	return r.db.WithContext(ctx).Save(t).Error
}

func (r *browserTaskRepo) UpdateStatus(ctx context.Context, id uint, status, errMsg string) error {
	updates := map[string]any{"status": status, "error_msg": errMsg}
	if status == "running" {
		now := time.Now()
		updates["last_run_at"] = &now
	}
	return r.db.WithContext(ctx).Model(&model.BrowserTask{}).Where("id = ?", id).Updates(updates).Error
}

func (r *browserTaskRepo) UpdateRunResult(ctx context.Context, id uint, status, lastResult, errMsg string, retryCount int) error {
	now := time.Now()
	return r.db.WithContext(ctx).Model(&model.BrowserTask{}).Where("id = ?", id).Updates(map[string]any{
		"status":      status,
		"last_run_at": &now,
		"last_result": lastResult,
		"error_msg":   errMsg,
		"retry_count": retryCount,
	}).Error
}

func (r *browserTaskRepo) SoftDelete(ctx context.Context, id, userID uint) error {
	res := r.db.WithContext(ctx).Where("id = ? AND user_id = ?", id, userID).Delete(&model.BrowserTask{})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

func (r *browserTaskRepo) FindRunning(ctx context.Context, userID uint) ([]*model.BrowserTask, error) {
	var list []*model.BrowserTask
	err := r.db.WithContext(ctx).Where("user_id = ? AND status = ?", userID, "running").Find(&list).Error
	return list, err
}

func (r *browserTaskRepo) ListDependents(ctx context.Context, taskID uint) ([]*model.BrowserTask, error) {
	var list []*model.BrowserTask
	err := r.db.WithContext(ctx).Where("depends_on_task_id = ? AND deleted_at IS NULL", taskID).Find(&list).Error
	return list, err
}

var ErrTaskNotFound = errors.New("browser task not found")
