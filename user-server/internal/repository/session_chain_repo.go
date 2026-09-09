// session_chain_repo.go 会话生命周期链与自动化规则仓储（五层 L5）
package repository

import (
	"context"
	"errors"
	"time"

	"hivemtk-user/internal/model"

	"gorm.io/gorm"
)

// SessionChainRepository 会话生命周期链查询/更新收口
type SessionChainRepository struct {
	db *gorm.DB
}

// NewSessionChainRepository 构造
func NewSessionChainRepository(db *gorm.DB) *SessionChainRepository {
	return &SessionChainRepository{db: db}
}

// ListStaleSessions SLA 扫描：取可自动关闭的超时会话
func (r *SessionChainRepository) ListStaleSessions(ctx context.Context, threshold time.Time) ([]*model.CustomerSession, error) {
	if r.db == nil {
		return nil, nil
	}
	var sessions []*model.CustomerSession
	err := r.db.WithContext(ctx).
		Where("status IN ? AND updated_at < ?", []model.SessionStatus{
			model.SessionStatusPending, model.SessionStatusAIHandling, model.SessionStatusWaiting,
		}, threshold).
		Limit(200).
		Find(&sessions).Error
	return sessions, err
}

// UpdateSessionTags 按 PK 更新会话标签 JSON
func (r *SessionChainRepository) UpdateSessionTags(ctx context.Context, id uint, tagsJSON string) error {
	if r.db == nil {
		return nil
	}
	return r.db.WithContext(ctx).Model(&model.CustomerSession{}).
		Where("id = ?", id).Update("tags", tagsJSON).Error
}

// CloseSessionByPK 按 PK 关闭会话
func (r *SessionChainRepository) CloseSessionByPK(ctx context.Context, id uint) error {
	if r.db == nil {
		return nil
	}
	return r.db.WithContext(ctx).Model(&model.CustomerSession{}).
		Where("id = ?", id).Update("status", model.SessionStatusClosed).Error
}

// ReopenSessionOnInbound resolved/closed 会话在收到访客消息后回 waiting，返回是否发生迁移
func (r *SessionChainRepository) ReopenSessionOnInbound(ctx context.Context, sessionID string) (bool, error) {
	if r.db == nil {
		return false, nil
	}
	res := r.db.WithContext(ctx).
		Model(&model.CustomerSession{}).
		Where("session_id = ? AND status IN ?", sessionID, []model.SessionStatus{
			model.SessionStatusResolved, model.SessionStatusClosed,
		}).
		Update("status", model.SessionStatusWaiting)
	if res.Error != nil {
		return false, res.Error
	}
	return res.RowsAffected > 0, nil
}

// AutomationRuleRepository 自动化规则 CRUD 与延迟执行收口
type AutomationRuleRepository struct {
	db *gorm.DB
}

// NewAutomationRuleRepository 构造
func NewAutomationRuleRepository(db *gorm.DB) *AutomationRuleRepository {
	return &AutomationRuleRepository{db: db}
}

// CreateRule 创建规则
func (r *AutomationRuleRepository) CreateRule(ctx context.Context, rule *model.AutomationRule) error {
	if r.db == nil {
		return errors.New("automation rule repository not initialized")
	}
	return r.db.WithContext(ctx).Create(rule).Error
}

// ListRules 按 event 过滤规则列表
func (r *AutomationRuleRepository) ListRules(ctx context.Context, event string) ([]*model.AutomationRule, error) {
	if r.db == nil {
		return nil, nil
	}
	q := r.db.WithContext(ctx).Model(&model.AutomationRule{})
	if event != "" {
		q = q.Where("event = ?", event)
	}
	var list []*model.AutomationRule
	err := q.Order("priority ASC, id ASC").Find(&list).Error
	return list, err
}

// ListEnabledRulesByEvent 按 event 取启用规则（调度入口，限 20 条）
func (r *AutomationRuleRepository) ListEnabledRulesByEvent(ctx context.Context, event string) ([]*model.AutomationRule, error) {
	if r.db == nil {
		return nil, nil
	}
	var rules []*model.AutomationRule
	err := r.db.WithContext(ctx).
		Where("event = ? AND enabled = ?", event, true).
		Order("priority ASC, id ASC").Limit(20).
		Find(&rules).Error
	return rules, err
}

// DeleteRule 删除规则
func (r *AutomationRuleRepository) DeleteRule(ctx context.Context, id uint) error {
	if r.db == nil {
		return nil
	}
	return r.db.WithContext(ctx).Delete(&model.AutomationRule{}, id).Error
}

// ToggleRule 启停规则
func (r *AutomationRuleRepository) ToggleRule(ctx context.Context, id uint, enabled bool) error {
	if r.db == nil {
		return nil
	}
	return r.db.WithContext(ctx).Model(&model.AutomationRule{}).
		Where("id = ?", id).Update("enabled", enabled).Error
}

// IncrementRuleRunCount 规则执行次数自增
func (r *AutomationRuleRepository) IncrementRuleRunCount(ctx context.Context, rule *model.AutomationRule) error {
	if r.db == nil {
		return nil
	}
	return r.db.WithContext(ctx).Model(rule).UpdateColumn("run_count", gorm.Expr("run_count + 1")).Error
}

// CreatePendingRuleExecution 写延迟执行记录
func (r *AutomationRuleRepository) CreatePendingRuleExecution(ctx context.Context, pending *model.RulePendingExecution) error {
	if r.db == nil {
		return nil
	}
	return r.db.WithContext(ctx).Create(pending).Error
}

// ListDuePendingExecutions 取到期 pending 执行记录
func (r *AutomationRuleRepository) ListDuePendingExecutions(ctx context.Context, now time.Time) ([]*model.RulePendingExecution, error) {
	if r.db == nil {
		return nil, nil
	}
	var pendings []*model.RulePendingExecution
	err := r.db.WithContext(ctx).
		Where("status = ? AND execute_at <= ?", "pending", now).
		Order("execute_at ASC").Limit(50).Find(&pendings).Error
	return pendings, err
}

// UpdatePendingExecutionStatus 更新延迟执行记录状态
func (r *AutomationRuleRepository) UpdatePendingExecutionStatus(ctx context.Context, id uint, status string) error {
	if r.db == nil {
		return nil
	}
	return r.db.WithContext(ctx).Model(&model.RulePendingExecution{}).
		Where("id = ?", id).Update("status", status).Error
}

// GetRuleByID 按 PK 取规则
func (r *AutomationRuleRepository) GetRuleByID(ctx context.Context, id uint) (*model.AutomationRule, error) {
	if r.db == nil {
		return nil, errors.New("automation rule repository not initialized")
	}
	var rule model.AutomationRule
	if err := r.db.WithContext(ctx).First(&rule, id).Error; err != nil {
		return nil, err
	}
	return &rule, nil
}

// GetSessionBySessionID 按 session_id 取会话（规则执行上下文）
func (r *AutomationRuleRepository) GetSessionBySessionID(ctx context.Context, sessionID string) (*model.CustomerSession, error) {
	if r.db == nil {
		return nil, errors.New("automation rule repository not initialized")
	}
	var sess model.CustomerSession
	if err := r.db.WithContext(ctx).Where("session_id = ?", sessionID).First(&sess).Error; err != nil {
		return nil, err
	}
	return &sess, nil
}

// UpdateSessionFieldsBySessionID 按 session_id 更新会话单字段
func (r *AutomationRuleRepository) UpdateSessionFieldsBySessionID(ctx context.Context, sessionID, field string, value any) error {
	if r.db == nil {
		return nil
	}
	return r.db.WithContext(ctx).Model(&model.CustomerSession{}).
		Where("session_id = ?", sessionID).Update(field, value).Error
}

// CreateRuleOutboundMessage 写规则自动外发消息
func (r *AutomationRuleRepository) CreateRuleOutboundMessage(ctx context.Context, rec *model.MessageHub) error {
	if r.db == nil {
		return nil
	}
	return r.db.WithContext(ctx).Create(rec).Error
}
