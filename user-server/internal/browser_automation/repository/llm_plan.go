package repository

import (
	"context"
	"time"

	"hivemtk-user/internal/browser_automation/model"
	_db "hivemtk-user/internal/pkg/db"

	"gorm.io/gorm"
)

// BrowserLLMPlanRepository LLM 计划仓储
type BrowserLLMPlanRepository interface {
	Create(ctx context.Context, p *model.BrowserLLMPlan) error
	GetByID(ctx context.Context, id uint) (*model.BrowserLLMPlan, error)
	ListByTaskID(ctx context.Context, taskID uint, limit int) ([]*model.BrowserLLMPlan, error)
	// ListBySessionID I5：session 导出取该会话全部 plan/judge/summary 记录（成本账全量，limit 保护）
	ListBySessionID(ctx context.Context, sessionID uint, limit int) ([]*model.BrowserLLMPlan, error)
	// PruneSnapshotText G19：清空 cutoff 前的 snapshot 大文本（成本账 token/model/kind 保留）
	PruneSnapshotText(ctx context.Context, cutoff time.Time) (int64, error)
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

// ListBySessionID I5：session 维度的全量 LLM 记录（plan/judge/summary 按时间升序，导出用）。
// limit 上限放宽到 200（Brain 40 迭代×多类 + judge/summary，单 session 足够全量）。
func (r *browserLLMPlanRepo) ListBySessionID(ctx context.Context, sessionID uint, limit int) ([]*model.BrowserLLMPlan, error) {
	if limit <= 0 || limit > 200 {
		limit = 200
	}
	var list []*model.BrowserLLMPlan
	err := r.db.WithContext(ctx).Where("session_id = ?", sessionID).Order("id ASC").Limit(limit).Find(&list).Error
	return list, err
}

// PruneSnapshotText G19：仅置空快照大字段（审计与成本账不丢），分批 pruneBatchRows。
// 必须分批的理由与 command_log 侧同源：快照每行可到 64 KiB，首次裁剪时超期行数=历史全量，
// 一条 UPDATE 吃完就是拿写放大大事务去换治理窗口（注释原先写着「分批 5000」、实现没有）。
// 清完的行不再满足「snapshot 非空」，故谓词本身保证每批都在推进、循环必然收敛。
func (r *browserLLMPlanRepo) PruneSnapshotText(ctx context.Context, cutoff time.Time) (int64, error) {
	match := func() *gorm.DB {
		return r.db.WithContext(ctx).Model(&model.BrowserLLMPlan{}).
			Where("created_at < ? AND snapshot <> ''", cutoff)
	}
	var total int64
	for {
		res := match().Where("id IN (?)", match().Order("id ASC").Limit(pruneBatchRows).Select("id")).
			Update("snapshot", "")
		if res.Error != nil {
			return total, res.Error
		}
		total += res.RowsAffected
		if res.RowsAffected == 0 {
			return total, nil
		}
	}
}
