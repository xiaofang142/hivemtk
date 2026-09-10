package repository

import (
	"context"
	"time"

	"hivemtk-user/internal/browser_automation/model"
	_db "hivemtk-user/internal/pkg/db"

	"gorm.io/gorm"
)

// BrowserSessionRepository 会话仓储
type BrowserSessionRepository interface {
	Create(ctx context.Context, s *model.BrowserSession) error
	GetByID(ctx context.Context, id, userID uint) (*model.BrowserSession, error)
	ListByTaskID(ctx context.Context, taskID, userID uint, page, limit int) ([]*model.BrowserSession, int64, error)
	ListByUser(ctx context.Context, userID uint, status string, page, limit int) ([]*model.BrowserSession, int64, error)
	UpdateStatus(ctx context.Context, id uint, status, errMsg string) error
	UpdateChromeTabID(ctx context.Context, id uint, tabID int) error
	UpdateMetrics(ctx context.Context, id uint, total, success, failed int, handLatencyMs int64) error
	UpdateTitleAndSnapshot(ctx context.Context, id uint, title, snapshot string) error
	UpdateExtractedData(ctx context.Context, id uint, extractedData ExtractedJSON) error
	UpdateArtifacts(ctx context.Context, id uint, extractedData []byte, finalScreenshotURL, llmSummary, consoleErrors string) error
	// FailRunningByUser Host 断连清理钩子：该用户所有 running session 置 failed，返回受影响 session 列表
	FailRunningByUser(ctx context.Context, userID uint, reason string) ([]*model.BrowserSession, error)
	HasSuccess(ctx context.Context, taskID uint) (bool, error)
	GetLatestByTaskID(ctx context.Context, taskID uint) (*model.BrowserSession, error)
	CountRunningByUser(ctx context.Context, userID uint) (int64, error)
	// CountRunningByTask 同任务运行中 session 数（RunTask 幂等防重）
	CountRunningByTask(ctx context.Context, userID, taskID uint) (int64, error)
}

// ExtractedJSON extracted_data 列的 JSON 载荷（避免 repo 直接依赖 datatypes 的写法扩散）
type ExtractedJSON = []byte

type browserSessionRepo struct {
	db *gorm.DB
}

func NewBrowserSessionRepository() BrowserSessionRepository {
	return &browserSessionRepo{db: _db.GetDB()}
}

// NewBrowserSessionRepositoryWithDB 显式注入 gormDB（路由装配用，测试可替换）
func NewBrowserSessionRepositoryWithDB(db *gorm.DB) BrowserSessionRepository {
	return &browserSessionRepo{db: db}
}

func (r *browserSessionRepo) Create(ctx context.Context, s *model.BrowserSession) error {
	return r.db.WithContext(ctx).Create(s).Error
}

func (r *browserSessionRepo) GetByID(ctx context.Context, id, userID uint) (*model.BrowserSession, error) {
	var s model.BrowserSession
	err := r.db.WithContext(ctx).Where("id = ? AND user_id = ?", id, userID).First(&s).Error
	if err != nil {
		return nil, err
	}
	return &s, nil
}

func (r *browserSessionRepo) ListByTaskID(ctx context.Context, taskID, userID uint, page, limit int) ([]*model.BrowserSession, int64, error) {
	if page <= 0 {
		page = 1
	}
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	q := r.db.WithContext(ctx).Model(&model.BrowserSession{}).Where("task_id = ? AND user_id = ?", taskID, userID)
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var list []*model.BrowserSession
	if err := q.Order("id DESC").Offset((page - 1) * limit).Limit(limit).Find(&list).Error; err != nil {
		return nil, 0, err
	}
	return list, total, nil
}

func (r *browserSessionRepo) ListByUser(ctx context.Context, userID uint, status string, page, limit int) ([]*model.BrowserSession, int64, error) {
	if page <= 0 {
		page = 1
	}
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	q := r.db.WithContext(ctx).Model(&model.BrowserSession{}).Where("user_id = ?", userID)
	if status != "" {
		q = q.Where("status = ?", status)
	}
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var list []*model.BrowserSession
	if err := q.Order("id DESC").Offset((page - 1) * limit).Limit(limit).Find(&list).Error; err != nil {
		return nil, 0, err
	}
	return list, total, nil
}

func (r *browserSessionRepo) UpdateStatus(ctx context.Context, id uint, status, errMsg string) error {
	updates := map[string]any{"status": status, "error_msg": errMsg}
	switch status {
	case "active":
		now := time.Now()
		updates["started_at"] = &now
	case "completed", "failed", "stopped":
		now := time.Now()
		updates["completed_at"] = &now
	}
	return r.db.WithContext(ctx).Model(&model.BrowserSession{}).Where("id = ?", id).Updates(updates).Error
}

func (r *browserSessionRepo) UpdateChromeTabID(ctx context.Context, id uint, tabID int) error {
	return r.db.WithContext(ctx).Model(&model.BrowserSession{}).Where("id = ?", id).
		Update("chrome_tab_id", tabID).Error
}

func (r *browserSessionRepo) UpdateMetrics(ctx context.Context, id uint, total, success, failed int, handLatencyMs int64) error {
	return r.db.WithContext(ctx).Model(&model.BrowserSession{}).Where("id = ?", id).Updates(map[string]any{
		"total_steps":     total,
		"success_steps":   success,
		"failed_steps":    failed,
		"hand_latency_ms": handLatencyMs,
	}).Error
}

func (r *browserSessionRepo) UpdateTitleAndSnapshot(ctx context.Context, id uint, title, snapshot string) error {
	updates := map[string]any{}
	if title != "" {
		updates["title"] = title
	}
	if snapshot != "" {
		updates["snapshot"] = snapshot
	}
	if len(updates) == 0 {
		return nil
	}
	return r.db.WithContext(ctx).Model(&model.BrowserSession{}).Where("id = ?", id).Updates(updates).Error
}

// UpdateExtractedData 合并后的 extract 结果落库（executor 负责合并，repo 只写列）
func (r *browserSessionRepo) UpdateExtractedData(ctx context.Context, id uint, extractedData ExtractedJSON) error {
	if len(extractedData) == 0 {
		return nil
	}
	return r.db.WithContext(ctx).Model(&model.BrowserSession{}).Where("id = ?", id).
		Update("extracted_data", extractedData).Error
}

func (r *browserSessionRepo) UpdateArtifacts(ctx context.Context, id uint, extractedData ExtractedJSON, finalScreenshotURL, llmSummary, consoleErrors string) error {
	updates := map[string]any{}
	if len(extractedData) > 0 {
		updates["extracted_data"] = extractedData
	}
	if finalScreenshotURL != "" {
		updates["final_screenshot_url"] = finalScreenshotURL
	}
	if llmSummary != "" {
		updates["llm_summary"] = llmSummary
	}
	if consoleErrors != "" {
		updates["console_errors"] = consoleErrors
	}
	if len(updates) == 0 {
		return nil
	}
	return r.db.WithContext(ctx).Model(&model.BrowserSession{}).Where("id = ?", id).Updates(updates).Error
}

func (r *browserSessionRepo) FailRunningByUser(ctx context.Context, userID uint, reason string) ([]*model.BrowserSession, error) {
	var running []*model.BrowserSession
	if err := r.db.WithContext(ctx).Where("user_id = ? AND status IN ?", userID, []string{"created", "active"}).Find(&running).Error; err != nil {
		return nil, err
	}
	if len(running) == 0 {
		return nil, nil
	}
	now := time.Now()
	ids := make([]uint, 0, len(running))
	for _, s := range running {
		ids = append(ids, s.ID)
	}
	if err := r.db.WithContext(ctx).Model(&model.BrowserSession{}).Where("id IN ?", ids).Updates(map[string]any{
		"status":       "failed",
		"error_msg":    reason,
		"completed_at": &now,
	}).Error; err != nil {
		return nil, err
	}
	return running, nil
}

func (r *browserSessionRepo) HasSuccess(ctx context.Context, taskID uint) (bool, error) {
	var n int64
	err := r.db.WithContext(ctx).Model(&model.BrowserSession{}).
		Where("task_id = ? AND status = ?", taskID, "completed").Count(&n).Error
	return n > 0, err
}

func (r *browserSessionRepo) GetLatestByTaskID(ctx context.Context, taskID uint) (*model.BrowserSession, error) {
	var s model.BrowserSession
	err := r.db.WithContext(ctx).Where("task_id = ?", taskID).Order("id DESC").First(&s).Error
	if err != nil {
		return nil, err
	}
	return &s, nil
}

func (r *browserSessionRepo) CountRunningByUser(ctx context.Context, userID uint) (int64, error) {
	var n int64
	err := r.db.WithContext(ctx).Model(&model.BrowserSession{}).
		Where("user_id = ? AND status IN ?", userID, []string{"created", "active"}).Count(&n).Error
	return n, err
}

// CountRunningByTask 同任务运行中 session 数（RunTask 幂等防重）
func (r *browserSessionRepo) CountRunningByTask(ctx context.Context, userID, taskID uint) (int64, error) {
	var n int64
	err := r.db.WithContext(ctx).Model(&model.BrowserSession{}).
		Where("user_id = ? AND task_id = ? AND status IN ?", userID, taskID, []string{"created", "active"}).Count(&n).Error
	return n, err
}
