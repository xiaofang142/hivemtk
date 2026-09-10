package repository

import (
	"context"

	"hivemtk-user/internal/browser_automation/model"
	_db "hivemtk-user/internal/pkg/db"

	"gorm.io/gorm"
)

// BrowserStepRepository 步骤仓储
type BrowserStepRepository interface {
	BatchCreate(ctx context.Context, steps []*model.BrowserStep) error
	ListBySessionID(ctx context.Context, sessionID uint) ([]*model.BrowserStep, error)
	ListBySessionIDAnyUser(ctx context.Context, sessionID uint) ([]*model.BrowserStep, error)
	UpdateStatus(ctx context.Context, id uint, status, errMsg string) error
	UpdateResult(ctx context.Context, id uint, status string, result []byte, durationMs int64, errMsg string) error
	DeleteBySessionID(ctx context.Context, sessionID uint) error
}

// StepResultJSON result 列的 JSON 载荷
type StepResultJSON = []byte

type browserStepRepo struct {
	db *gorm.DB
}

func NewBrowserStepRepository() BrowserStepRepository {
	return &browserStepRepo{db: _db.GetDB()}
}

// NewBrowserStepRepositoryWithDB 显式注入 gormDB（路由装配用，测试可替换）
func NewBrowserStepRepositoryWithDB(db *gorm.DB) BrowserStepRepository {
	return &browserStepRepo{db: db}
}

func (r *browserStepRepo) BatchCreate(ctx context.Context, steps []*model.BrowserStep) error {
	if len(steps) == 0 {
		return nil
	}
	return r.db.WithContext(ctx).Create(&steps).Error
}

func (r *browserStepRepo) ListBySessionID(ctx context.Context, sessionID uint) ([]*model.BrowserStep, error) {
	var list []*model.BrowserStep
	err := r.db.WithContext(ctx).Where("session_id = ?", sessionID).Order("step_index ASC").Find(&list).Error
	return list, err
}

// ListBySessionIDAnyUser 供归属校验后的内部流程使用（session 归属已在上层校验）
func (r *browserStepRepo) ListBySessionIDAnyUser(ctx context.Context, sessionID uint) ([]*model.BrowserStep, error) {
	return r.ListBySessionID(ctx, sessionID)
}

func (r *browserStepRepo) UpdateStatus(ctx context.Context, id uint, status, errMsg string) error {
	return r.db.WithContext(ctx).Model(&model.BrowserStep{}).Where("id = ?", id).
		Updates(map[string]any{"status": status, "error_msg": errMsg}).Error
}

func (r *browserStepRepo) UpdateResult(ctx context.Context, id uint, status string, result StepResultJSON, durationMs int64, errMsg string) error {
	updates := map[string]any{
		"status":      status,
		"duration_ms": durationMs,
		"error_msg":   errMsg,
	}
	if len(result) > 0 {
		updates["result"] = result
	}
	return r.db.WithContext(ctx).Model(&model.BrowserStep{}).Where("id = ?", id).Updates(updates).Error
}

func (r *browserStepRepo) DeleteBySessionID(ctx context.Context, sessionID uint) error {
	return r.db.WithContext(ctx).Where("session_id = ?", sessionID).Delete(&model.BrowserStep{}).Error
}
