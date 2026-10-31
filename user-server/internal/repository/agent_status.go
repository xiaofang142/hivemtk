package repository

import (
	"context"
	"fmt"
	"hivemtk-user/internal/model"
	_db "hivemtk-user/internal/pkg/db"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	// agentOnlineListLimit 在线坐席候选上限：按负载升序取最闲的，封顶不影响分配质量。
	agentOnlineListLimit = 500
	// agentAllListLimit 监管控制台/心跳扫描的全量坐席上限。
	agentAllListLimit = 1000
)

// AgentStatusRepository 客服状态仓库
type AgentStatusRepository struct {
	db *gorm.DB
}

func NewAgentStatusRepository() *AgentStatusRepository {
	return &AgentStatusRepository{
		db: _db.GetDB(),
	}
}

func NewAgentStatusRepositoryWithDB(db *gorm.DB) *AgentStatusRepository {
	return &AgentStatusRepository{db: db}
}

func (r *AgentStatusRepository) Create(ctx context.Context, status *model.AgentStatus) error {
	return r.db.WithContext(ctx).Create(status).Error
}

func (r *AgentStatusRepository) GetByAgentID(ctx context.Context, agentID uint) (*model.AgentStatus, error) {
	var status model.AgentStatus
	err := r.db.WithContext(ctx).Where("agent_id = ?", agentID).First(&status).Error
	if err != nil {
		return nil, err
	}
	return &status, nil
}

// GetByID 按主键取坐席状态（创建客服挂载时校验 agent_status_id 是否真实存在）。
func (r *AgentStatusRepository) GetByID(ctx context.Context, id uint) (*model.AgentStatus, error) {
	var status model.AgentStatus
	if err := r.db.WithContext(ctx).Where("id = ?", id).First(&status).Error; err != nil {
		return nil, err
	}
	return &status, nil
}

func (r *AgentStatusRepository) GetOnlineAgents(ctx context.Context) ([]*model.AgentStatus, error) {
	var agents []*model.AgentStatus
	cutoff := time.Now().Add(-5 * time.Minute)
	err := r.db.WithContext(ctx).Where("status IN ? AND active_sessions < max_sessions AND last_active_at > ?",
		[]string{"online", "busy"}, cutoff).
		Order("active_sessions ASC").Limit(agentOnlineListLimit).Find(&agents).Error
	return agents, err
}

// ListAllAgents 列出全部客服（不分在线/离线），用于客服监管控制台
func (r *AgentStatusRepository) ListAllAgents(ctx context.Context) ([]*model.AgentStatus, error) {
	var agents []*model.AgentStatus
	// 必须 WithContext：on 模式 tenancyidx 从 ctx 取租户注入过滤，
	// 丢 ctx 会触发 fail-closed 5000
	err := r.db.WithContext(ctx).Order("agent_id ASC").Limit(agentAllListLimit).Find(&agents).Error
	return agents, err
}

// UpdateStatus 更新客服状态
func (r *AgentStatusRepository) UpdateStatus(ctx context.Context, agentID uint, status string) error {
	updates := map[string]any{"status": status}
	now := time.Now()
	if status == "online" {
		updates["online_at"] = &now
	} else if status == "offline" {
		updates["offline_at"] = &now
	}
	updates["last_active_at"] = &now
	// 计数/状态更新保持"0 行即 no-op"：心跳与计数是 best-effort，
	// 坐席行缺失时按幂等成功处理（既有契约，由仓储用例锁死）。
	return r.db.WithContext(ctx).Model(&model.AgentStatus{}).Where("agent_id = ?", agentID).Updates(updates).Error
}

// UpdateStatusAndLimit 更新状态并可选更新接待上限。
//
// 为什么单独一个方法：UpdateStatus 是心跳/上线/下线的热路径，签名里塞
// 一个 *int 会让所有调用点都要考虑"上限要不要动"；而监管台的"编辑坐席"
// 弹窗同时提交 status 与 max_sessions（前端把两者放在同一个表单里）。
func (r *AgentStatusRepository) UpdateStatusAndLimit(ctx context.Context, agentID uint, status string, maxSessions *int) error {
	if maxSessions != nil {
		if err := r.db.WithContext(ctx).Model(&model.AgentStatus{}).
			Where("agent_id = ?", agentID).Update("max_sessions", *maxSessions).Error; err != nil {
			return err
		}
	}
	return r.UpdateStatus(ctx, agentID, status)
}

// IncrementActiveSessions 增加活跃会话数
func (r *AgentStatusRepository) IncrementActiveSessions(ctx context.Context, agentID uint) error {
	return r.db.WithContext(ctx).Model(&model.AgentStatus{}).Where("agent_id = ?", agentID).
		Updates(map[string]any{
			"active_sessions": gorm.Expr("active_sessions + 1"),
			"today_sessions":  gorm.Expr("today_sessions + 1"),
		}).Error
}

// DecrementActiveSessions 减少活跃会话数
func (r *AgentStatusRepository) DecrementActiveSessions(ctx context.Context, agentID uint) error {
	return r.db.WithContext(ctx).Model(&model.AgentStatus{}).Where("agent_id = ? AND active_sessions > 0", agentID).
		Update("active_sessions", gorm.Expr("active_sessions - 1")).Error
}

// IncrementTodayMessages 增加今日消息数
func (r *AgentStatusRepository) IncrementTodayMessages(ctx context.Context, agentID uint) error {
	return r.db.WithContext(ctx).Model(&model.AgentStatus{}).Where("agent_id = ?", agentID).
		Update("today_messages", gorm.Expr("today_messages + 1")).Error
}

// CountOnlineAgents 统计在线坐席数（status IN ['online', 'busy']）
//
// 用于访客侧 OpenSession / SendMessage 时广播新会话通知前的快速判断：
// 仅返回数量，不返回坐席详情（避免不必要的扫描）。
func (r *AgentStatusRepository) CountOnlineAgents(ctx context.Context) (int, error) {
	var count int64
	err := r.db.WithContext(ctx).Model(&model.AgentStatus{}).
		Where("status IN ?", []string{"online", "busy"}).
		Count(&count).Error
	if err != nil {
		return 0, err
	}
	return int(count), nil
}

func (r *AgentStatusRepository) TouchHeartbeat(ctx context.Context, agentID uint) error {
	now := time.Now()
	return r.db.WithContext(ctx).Model(&model.AgentStatus{}).
		Where("agent_id = ?", agentID).
		Update("last_active_at", &now).Error
}

// CountByStatus 按 status 统计坐席数（客服队列/容量视图用）
func (r *AgentStatusRepository) CountByStatus(ctx context.Context, status string) (int64, error) {
	var count int64
	err := r.db.WithContext(ctx).Model(&model.AgentStatus{}).
		Where("status = ?", status).Count(&count).Error
	return count, err
}

// AutoAssignAgentTx 自动分配事务：行锁锁定最闲在线坐席 → 会话分配 → 坐席负载 +1，原子提交。
// 返回被分配的坐席（供调用方做 WS 通知）；无可用坐席返回 gorm.ErrRecordNotFound。
func (r *AgentStatusRepository) AutoAssignAgentTx(ctx context.Context, sessionID uint) (*model.AgentStatus, error) {
	tx := r.db.WithContext(ctx).Begin()
	if tx.Error != nil {
		return nil, fmt.Errorf("start transaction: %w", tx.Error)
	}
	defer func() {
		if rec := recover(); rec != nil {
			tx.Rollback()
			panic(rec)
		}
	}()

	var bestAgent model.AgentStatus
	cutoff := time.Now().Add(-5 * time.Minute)
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("status IN ? AND active_sessions < max_sessions AND last_active_at > ?",
			[]string{"online", "busy"}, cutoff).
		Order("active_sessions ASC").
		Limit(1).
		First(&bestAgent).Error; err != nil {
		tx.Rollback()
		return nil, err
	}

	// 必须用挂在同一事务上的仓储：传入外部仓储会走它自己的连接，
	// 后面的负载自增回滚了、会话分配却已经提交，事务名不副实。
	if err := NewCustomerSessionRepositoryWithDB(tx).AssignAgent(ctx, sessionID, bestAgent.AgentID, bestAgent.AgentName); err != nil {
		tx.Rollback()
		return nil, fmt.Errorf("assign agent: %w", err)
	}

	if err := tx.Model(&model.AgentStatus{}).Where("agent_id = ?", bestAgent.AgentID).
		Updates(map[string]any{
			"active_sessions": gorm.Expr("active_sessions + 1"),
			"today_sessions":  gorm.Expr("today_sessions + 1"),
		}).Error; err != nil {
		tx.Rollback()
		return nil, fmt.Errorf("increment agent load: %w", err)
	}

	if err := tx.Commit().Error; err != nil {
		return nil, fmt.Errorf("commit assign: %w", err)
	}
	return &bestAgent, nil
}
