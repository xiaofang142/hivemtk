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
	// 批8 对账器（stale_reconcile.go）专用两条，见各自注释
	FindStaleRunningAll(ctx context.Context, minAge time.Duration, limit int) ([]*model.BrowserTask, error)
	ReconcileRunResult(ctx context.Context, id uint, status, lastResult, errMsg string) (bool, error)
	ListDependents(ctx context.Context, taskID uint) ([]*model.BrowserTask, error)
	// D4b（G5）：重试持久化——scheduleRetry 落 next_retry_at，扫描器条件认领（置 NULL）防双触发
	SetNextRetryAt(ctx context.Context, id uint, at *time.Time) error
	// ownerUserIDs：本进程持有 Host 连接的用户白名单（空=不加此过滤，见 ClaimDueRetries 注释）
	ClaimDueRetries(ctx context.Context, now time.Time, limit int, ownerUserIDs []uint) ([]*model.BrowserTask, error)
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

// FindStaleRunningAll 批8 对账器专用：跨用户扫「running 且 updated_at 老于 minAge」的任务。
// 与 FindRunning 分列而非复用：那条带 userID 语义（用户侧「我的执行中」），对账是全库后台职责，
// 混用会让「加个 userID 参数」变成越权读取的入口。minAge 只是粗筛下限，
// 每任务真实预算（含 D7 确认等待）由调用方按 taskExecBudget 判。
func (r *browserTaskRepo) FindStaleRunningAll(ctx context.Context, minAge time.Duration, limit int) ([]*model.BrowserTask, error) {
	if limit <= 0 || limit > 200 {
		limit = 100
	}
	var list []*model.BrowserTask
	err := r.db.WithContext(ctx).
		Where("status = ? AND deleted_at IS NULL AND updated_at <= ?", "running", time.Now().Add(-minAge)).
		Order("id ASC").Limit(limit).Find(&list).Error
	return list, err
}

// ReconcileRunResult 批8 对账回填：仅当任务仍是 running 时写终态，返回是否由本次回填生效。
// 条件 WHERE 不可省——对账器与执行协程可能同刻收口，无条件更新会把刚落下的真实终态盖成对账值。
func (r *browserTaskRepo) ReconcileRunResult(ctx context.Context, id uint, status, lastResult, errMsg string) (bool, error) {
	res := r.db.WithContext(ctx).Model(&model.BrowserTask{}).
		Where("id = ? AND status = ?", id, "running").
		Updates(map[string]any{"status": status, "last_result": lastResult, "error_msg": errMsg})
	if res.Error != nil {
		return false, res.Error
	}
	return res.RowsAffected == 1, nil
}

func (r *browserTaskRepo) ListDependents(ctx context.Context, taskID uint) ([]*model.BrowserTask, error) {
	var list []*model.BrowserTask
	err := r.db.WithContext(ctx).Where("depends_on_task_id = ? AND deleted_at IS NULL", taskID).Find(&list).Error
	return list, err
}

// SetNextRetryAt D4b（G5）：设置/清除任务的重试到期时间（nil=取消挂起重试）
func (r *browserTaskRepo) SetNextRetryAt(ctx context.Context, id uint, at *time.Time) error {
	return r.db.WithContext(ctx).Model(&model.BrowserTask{}).Where("id = ?", id).
		Update("next_retry_at", at).Error
}

// ClaimDueRetries D4b（G5）：原子认领到期重试——条件更新置 NULL，多副本同库仅一方 RowsAffected=1；
// 认领成功后进程崩溃则重试丢失（与改造前语义相同），但重启不再丢挂起重试。
//
// 批9 归属门：Host 连接是**进程内**状态（registry.conns），挂起重试却是**库内**共享队列。
// 无门时任一实例都能认领任一用户的重试，然后在自己空空的 registry 上判「browser host
// 未连接」，把 MaxRetryTimes 的额度烧在一次根本不可能执行的认领上（真机实测：task=377
// session=429 由另一实例认领，而本机 Host 全程在线）。ownerUserIDs 即本进程持有连接的用户
// 白名单。**空=不加过滤**：装配遗漏时退化成改造前行为，也不要静默停掉全部重试
// （调用方若要表达「本机无人」，应自行不调用，见 feedback.scanDueRetries）。
//
// 状态门（批13）：挂起重试只在**仍处于失败态**时才有意义。next_retry_at 是失败时写下的，
// 之后行的状态可以走到任何一处，而这四条路都不会回头清这个字段：
// done —— 用户手工重跑成功了，RunTask 明确放行 done 态执行，扫描器到点就把一条已经成功
// 的任务再跑一遍（含写步，防双发只剩台账这一道，等于把「成功即终止」推翻）；
// paused —— 同样被 RunTask 放行（它是 Resume 的合法前态），于是「用户按了暂停」被后台
// 重试悄悄解除；archived / running —— RunTask 会拒，但认领已经把字段置空，挂起被无声吞掉。
// 认领条件与条件更新两处都带 status='failed'：只挑一处会留下 Pluck→Update 之间用户正好
// 归档/重跑的竞态窗口（也正因如此，单独摘掉任一处都测不出红，两处一起摘才红——
// 与 deleted_at 条件同性质，见 retry_claim_b9_test.go）。软删行另有 deleted_at 条件兜。
func (r *browserTaskRepo) ClaimDueRetries(ctx context.Context, now time.Time, limit int, ownerUserIDs []uint) ([]*model.BrowserTask, error) {
	if limit <= 0 || limit > 50 {
		limit = 10
	}
	ids := []uint{}
	q := r.db.WithContext(ctx).Model(&model.BrowserTask{}).
		Where("next_retry_at IS NOT NULL AND next_retry_at <= ? AND deleted_at IS NULL AND status = ?", now, "failed")
	if len(ownerUserIDs) > 0 {
		q = q.Where("user_id IN ?", ownerUserIDs)
	}
	err := q.Order("next_retry_at ASC").Limit(limit).Pluck("id", &ids).Error
	if err != nil || len(ids) == 0 {
		return nil, err
	}
	claimed := make([]uint, 0, len(ids))
	for _, id := range ids {
		res := r.db.WithContext(ctx).Model(&model.BrowserTask{}).
			Where("id = ? AND next_retry_at IS NOT NULL AND next_retry_at <= ? AND status = ?", id, now, "failed").
			Update("next_retry_at", nil)
		if res.Error == nil && res.RowsAffected == 1 {
			claimed = append(claimed, id)
		}
	}
	if len(claimed) == 0 {
		return nil, nil
	}
	var list []*model.BrowserTask
	err = r.db.WithContext(ctx).Where("id IN ?", claimed).Find(&list).Error
	return list, err
}

var ErrTaskNotFound = errors.New("browser task not found")
