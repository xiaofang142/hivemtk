package repository

import (
	"context"
	"time"

	"hivemtk-user/internal/browser_automation/model"
	_db "hivemtk-user/internal/pkg/db"

	"gorm.io/gorm"
)

// BrowserCronTriggerRepository 定时触发器仓储
type BrowserCronTriggerRepository interface {
	Create(ctx context.Context, t *model.BrowserCronTrigger) error
	GetByID(ctx context.Context, id, userID uint) (*model.BrowserCronTrigger, error)
	GetByTaskID(ctx context.Context, taskID uint) (*model.BrowserCronTrigger, error)
	ListByUser(ctx context.Context, userID uint) ([]*model.BrowserCronTrigger, error)
	ListAllEnabled(ctx context.Context) ([]*model.BrowserCronTrigger, error)
	Update(ctx context.Context, t *model.BrowserCronTrigger) error
	UpdateEnabled(ctx context.Context, id uint, enabled bool) error
	UpdateTimes(ctx context.Context, id uint, nextRunAt, lastRunAt time.Time) error
	Delete(ctx context.Context, id, userID uint) error
}

type browserCronTriggerRepo struct {
	db *gorm.DB
}

func NewBrowserCronTriggerRepository() BrowserCronTriggerRepository {
	return &browserCronTriggerRepo{db: _db.GetDB()}
}

// NewBrowserCronTriggerRepositoryWithDB 显式注入 gormDB（路由装配用，测试可替换）
func NewBrowserCronTriggerRepositoryWithDB(db *gorm.DB) BrowserCronTriggerRepository {
	return &browserCronTriggerRepo{db: db}
}

func (r *browserCronTriggerRepo) Create(ctx context.Context, t *model.BrowserCronTrigger) error {
	return r.db.WithContext(ctx).Create(t).Error
}

func (r *browserCronTriggerRepo) GetByID(ctx context.Context, id, userID uint) (*model.BrowserCronTrigger, error) {
	var t model.BrowserCronTrigger
	err := r.db.WithContext(ctx).
		Joins("JOIN browser_tasks ON browser_tasks.id = browser_cron_triggers.task_id AND browser_tasks.user_id = ?", userID).
		Where("browser_cron_triggers.id = ?", id).
		First(&t).Error
	if err != nil {
		return nil, err
	}
	return &t, nil
}

func (r *browserCronTriggerRepo) GetByTaskID(ctx context.Context, taskID uint) (*model.BrowserCronTrigger, error) {
	var t model.BrowserCronTrigger
	err := r.db.WithContext(ctx).Where("task_id = ?", taskID).First(&t).Error
	if err != nil {
		return nil, err
	}
	return &t, nil
}

func (r *browserCronTriggerRepo) ListByUser(ctx context.Context, userID uint) ([]*model.BrowserCronTrigger, error) {
	var list []*model.BrowserCronTrigger
	err := r.db.WithContext(ctx).
		Joins("JOIN browser_tasks ON browser_tasks.id = browser_cron_triggers.task_id AND browser_tasks.user_id = ?", userID).
		Order("browser_cron_triggers.id DESC").
		Find(&list).Error
	return list, err
}

func (r *browserCronTriggerRepo) ListAllEnabled(ctx context.Context) ([]*model.BrowserCronTrigger, error) {
	var list []*model.BrowserCronTrigger
	err := r.db.WithContext(ctx).Where("enabled = ?", true).Find(&list).Error
	return list, err
}

func (r *browserCronTriggerRepo) Update(ctx context.Context, t *model.BrowserCronTrigger) error {
	return r.db.WithContext(ctx).Save(t).Error
}

func (r *browserCronTriggerRepo) UpdateEnabled(ctx context.Context, id uint, enabled bool) error {
	return r.db.WithContext(ctx).Model(&model.BrowserCronTrigger{}).Where("id = ?", id).
		Update("enabled", enabled).Error
}

func (r *browserCronTriggerRepo) UpdateTimes(ctx context.Context, id uint, nextRunAt, lastRunAt time.Time) error {
	return r.db.WithContext(ctx).Model(&model.BrowserCronTrigger{}).Where("id = ?", id).
		Updates(map[string]any{"next_run_at": nextRunAt, "last_run_at": lastRunAt}).Error
}

func (r *browserCronTriggerRepo) Delete(ctx context.Context, id, userID uint) error {
	res := r.db.WithContext(ctx).
		Joins("JOIN browser_tasks ON browser_tasks.id = browser_cron_triggers.task_id AND browser_tasks.user_id = ?", userID).
		Where("browser_cron_triggers.id = ?", id).
		Delete(&model.BrowserCronTrigger{})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}
