package repository

import (
	"context"

	"hivemtk-user/internal/browser_automation/model"
	_db "hivemtk-user/internal/pkg/db"

	"gorm.io/gorm"
)

// BrowserLLMPlanRepository LLM 计划仓储
type BrowserLLMPlanRepository interface {
	Create(ctx context.Context, p *model.BrowserLLMPlan) error
	GetByID(ctx context.Context, id uint) (*model.BrowserLLMPlan, error)
	ListByTaskID(ctx context.Context, taskID uint, limit int) ([]*model.BrowserLLMPlan, error)
}

type browserLLMPlanRepo struct {
	db *gorm.DB
}

func NewBrowserLLMPlanRepository() BrowserLLMPlanRepository {
	return &browserLLMPlanRepo{db: _db.GetDB()}
}

// NewBrowserLLMPlanRepositoryWithDB 显式注入 gormDB（路由装配用，测试可替换）
func NewBrowserLLMPlanRepositoryWithDB(db *gorm.DB) BrowserLLMPlanRepository {
	return &browserLLMPlanRepo{db: db}
}

func (r *browserLLMPlanRepo) Create(ctx context.Context, p *model.BrowserLLMPlan) error {
	return r.db.WithContext(ctx).Create(p).Error
}

func (r *browserLLMPlanRepo) GetByID(ctx context.Context, id uint) (*model.BrowserLLMPlan, error) {
	var p model.BrowserLLMPlan
	err := r.db.WithContext(ctx).Where("id = ?", id).First(&p).Error
	if err != nil {
		return nil, err
	}
	return &p, nil
}

func (r *browserLLMPlanRepo) ListByTaskID(ctx context.Context, taskID uint, limit int) ([]*model.BrowserLLMPlan, error) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	var list []*model.BrowserLLMPlan
	err := r.db.WithContext(ctx).Where("task_id = ?", taskID).Order("id DESC").Limit(limit).Find(&list).Error
	return list, err
}
