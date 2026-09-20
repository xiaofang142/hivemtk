package repository

import (
	"context"
	"fmt"

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
	// UpdateSubmitState 批6（F11b）写台账状态转移：只写 submit_state/text_hash 两列，
	// 与 UpdateResult 分道——步终态（status）由步收口写，提交归因由台账写，两者在
	// 「已提交但验证未见」时必然不同值，合成一列就会把结果未知态当成可重发。
	UpdateSubmitState(ctx context.Context, id uint, state, textHash string) error
	// FindSubmitAttempt 跨 session 查「同任务、同文本」是否已存在提交尝试
	// （sent/verified/unattributed 三态都算尝试过；prepared 不算——点击从未发生）。
	// 无尝试返回 gorm.ErrRecordNotFound。
	// 批7（F-N4）键里不再有 step_index：Brain 模式的步下标每轮递增，带下标的键会让同一条
	// 评论在换轮重放时落到不同下标上而漏闸。
	FindSubmitAttempt(ctx context.Context, taskID uint, textHash string, excludeID uint) (*model.BrowserStep, error)
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

// UpdateSubmitState 写台账。0 行受影响**必须报错**：本方法是「重发闸门的唯一事实来源」的
// 唯一写入口，而 gorm 对 0 行只回 Error==nil。两种真实形态都会命中 0 行——
// 行不存在（上层拿到过另一个库的 id）与行已软删（BrowserStep 带 DeletedAt，gorm 自动加
// deleted_at IS NULL；同一条件也让 FindSubmitAttempt 查不到它）。放行即「已记为尝试」
// 变成一句谎话，下一轮闸门查空 ⇒ 双发。口径同 task.go:125 / cron.go:101 的条件更新。
func (r *browserStepRepo) UpdateSubmitState(ctx context.Context, id uint, state, textHash string) error {
	res := r.db.WithContext(ctx).Model(&model.BrowserStep{}).Where("id = ?", id).
		Updates(map[string]any{"submit_state": state, "text_hash": textHash})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return fmt.Errorf("台账写入命中 0 行（step=%d state=%s）：该行不存在或已被软删，闸门查不到这次提交", id, state)
	}
	return nil
}

func (r *browserStepRepo) FindSubmitAttempt(ctx context.Context, taskID uint, textHash string, excludeID uint) (*model.BrowserStep, error) {
	if textHash == "" {
		return nil, gorm.ErrRecordNotFound
	}
	var row model.BrowserStep
	q := r.db.WithContext(ctx).Where("task_id = ? AND text_hash = ? AND submit_state IN ?",
		taskID, textHash, model.StepSubmitAttemptedStates())
	if excludeID > 0 {
		q = q.Where("id <> ?", excludeID)
	}
	if err := q.Order("id asc").First(&row).Error; err != nil {
		return nil, err
	}
	return &row, nil
}
