package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/pkg/utils"
	"hivemtk-user/internal/pkg/utils/logger"
	"hivemtk-user/internal/repository"

	"gorm.io/gorm"
)

const (
	// HeartbeatOfflineTimeout 工作台连接断开判定窗口：last_active_at 超过该时长
	// 未刷新的在线坐席视为失联，自动 offline 并释放在办会话。
	HeartbeatOfflineTimeout = 5 * time.Minute

	// HeartbeatMonitorInterval 后台心跳检查默认间隔
	HeartbeatMonitorInterval = time.Minute
)

// 坐席状态取值域：前端工作台只有在线/忙碌/离线三档。
//
// 不设白名单时任意串都能落库（例如 status="bogus"），后果是该坐席既不在
// 在线列表（status IN online,busy）也不被判离线，监管台仍显示一条"幽灵"行。
const (
	AgentStatusOnline  = "online"
	AgentStatusBusy    = "busy"
	AgentStatusOffline = "offline"

	// AgentMaxSessionsDefault 新建坐席未指定接待上限时的默认值
	AgentMaxSessionsDefault = 5
	// AgentMaxSessionsLimit 接待上限的上界（与前端 input-number 的 max 对齐）
	AgentMaxSessionsLimit = 50
)

// 坐席域错误哨兵：控制器据此映射 400/404/409。
var (
	// ErrAgentInvalidInput 入参不合法（状态取值、接待上限、坐席号/名称）
	ErrAgentInvalidInput = errors.New("坐席参数非法")
	// ErrAgentNotFound 坐席不存在
	ErrAgentNotFound = errors.New("坐席不存在")
	// ErrAgentDuplicated 同一坐席号已有状态记录
	ErrAgentDuplicated = errors.New("坐席已存在")
)

// isValidAgentStatus 状态取值是否在域内
func isValidAgentStatus(status string) bool {
	switch status {
	case AgentStatusOnline, AgentStatusBusy, AgentStatusOffline:
		return true
	}
	return false
}

// AgentStatusService 客服状态服务
type AgentStatusService struct {
	agentRepo   *repository.AgentStatusRepository
	sessionRepo *repository.CustomerSessionRepository

	mu        sync.Mutex
	monitorOn bool
}

// NewAgentStatusService 创建客服状态服务实例；两个仓储都由装配层注入，
// 服务内不再直取全局 db 句柄。
func NewAgentStatusService(
	agentRepo *repository.AgentStatusRepository,
	sessionRepo *repository.CustomerSessionRepository,
) *AgentStatusService {
	return &AgentStatusService{agentRepo: agentRepo, sessionRepo: sessionRepo}
}

// CreateAgentRequest 创建客服请求
type CreateAgentRequest struct {
	AgentID     uint   `json:"agent_id" binding:"required"`
	AgentName   string `json:"agent_name" binding:"required"`
	MaxSessions int    `json:"max_sessions"`
}

// CreateAgent 创建客服状态记录
func (s *AgentStatusService) CreateAgent(ctx context.Context, req *CreateAgentRequest) (*model.AgentStatus, error) {
	name := strings.TrimSpace(req.AgentName)
	if req.AgentID == 0 {
		return nil, fmt.Errorf("%w: 坐席 ID 必须是正整数", ErrAgentInvalidInput)
	}
	if name == "" {
		return nil, fmt.Errorf("%w: 坐席姓名不能为空", ErrAgentInvalidInput)
	}
	if utf8.RuneCountInString(name) > 100 {
		return nil, fmt.Errorf("%w: 坐席姓名不能超过 100 个字符", ErrAgentInvalidInput)
	}
	maxSessions := req.MaxSessions
	if maxSessions == 0 {
		maxSessions = AgentMaxSessionsDefault
	}
	if maxSessions < 1 || maxSessions > AgentMaxSessionsLimit {
		return nil, fmt.Errorf("%w: 接待上限必须在 1~%d 之间（当前 %d）", ErrAgentInvalidInput, AgentMaxSessionsLimit, maxSessions)
	}

	agent := &model.AgentStatus{
		AgentID:     req.AgentID,
		AgentName:   name,
		Status:      AgentStatusOffline,
		MaxSessions: maxSessions,
	}

	if err := s.agentRepo.Create(ctx, agent); err != nil {
		if repository.IsDuplicateKeyErr(err) {
			return nil, fmt.Errorf("%w: 坐席 %d 已存在，请直接编辑现有坐席", ErrAgentDuplicated, req.AgentID)
		}
		return nil, err
	}

	return agent, nil
}

// GetAgentStatus 获取客服状态
func (s *AgentStatusService) GetAgentStatus(ctx context.Context, agentID uint) (*model.AgentStatus, error) {
	agent, err := s.agentRepo.GetByAgentID(ctx, agentID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, fmt.Errorf("%w: %d", ErrAgentNotFound, agentID)
		}
		return nil, err
	}
	return agent, nil
}

// GetOnlineAgents 获取在线客服列表
func (s *AgentStatusService) GetOnlineAgents(ctx context.Context) ([]*model.AgentStatus, error) {
	return s.agentRepo.GetOnlineAgents(ctx)
}

// ListAllAgents 列出全部客服（不分在线/离线），用于客服监管控制台
func (s *AgentStatusService) ListAllAgents(ctx context.Context) ([]*model.AgentStatus, error) {
	return s.agentRepo.ListAllAgents(ctx)
}

// UpdateAgentStatus 更新客服状态
func (s *AgentStatusService) UpdateAgentStatus(ctx context.Context, agentID uint, status string, maxSessions *int) error {
	status = strings.TrimSpace(status)
	if !isValidAgentStatus(status) {
		return fmt.Errorf("%w: 坐席状态只能是 online / busy / offline（当前 %q）", ErrAgentInvalidInput, status)
	}
	if maxSessions != nil && (*maxSessions < 1 || *maxSessions > AgentMaxSessionsLimit) {
		return fmt.Errorf("%w: 接待上限必须在 1~%d 之间（当前 %d）", ErrAgentInvalidInput, AgentMaxSessionsLimit, *maxSessions)
	}

	agent, err := s.agentRepo.GetByAgentID(ctx, agentID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return fmt.Errorf("%w: %d", ErrAgentNotFound, agentID)
		}
		return err
	}

	if agent.Status == AgentStatusOffline && status != AgentStatusOnline {
		return fmt.Errorf("%w: 客服离线时只能切换到在线状态", ErrAgentInvalidInput)
	}

	return s.agentRepo.UpdateStatusAndLimit(ctx, agentID, status, maxSessions)
}

// GoOnline 客服上线
func (s *AgentStatusService) GoOnline(ctx context.Context, agentID uint) error {
	return s.UpdateAgentStatus(ctx, agentID, AgentStatusOnline, nil)
}

// GoOffline 客服下线
func (s *AgentStatusService) GoOffline(ctx context.Context, agentID uint) error {
	agent, err := s.agentRepo.GetByAgentID(ctx, agentID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return fmt.Errorf("%w: %d", ErrAgentNotFound, agentID)
		}
		return err
	}

	if agent.ActiveSessions > 0 {
		return fmt.Errorf("%w: 还有 %d 个未完成的会话，请先处理或转接", ErrAgentInvalidInput, agent.ActiveSessions)
	}

	return s.agentRepo.UpdateStatus(ctx, agentID, AgentStatusOffline)
}

// GetAgentSessions 获取客服的活跃会话
func (s *AgentStatusService) GetAgentSessions(ctx context.Context, agentID uint) ([]*model.CustomerSession, error) {
	return s.sessionRepo.GetAgentSessions(ctx, agentID)
}

// TouchHeartbeat 记录坐席工作台连接活跃（S-3）。
//
// 接线点（见报告）：工作台 WS/SSE 连接的消息/心跳帧事件处调用；
// HTTP 轮询型工作台可在轮询端点处兜底调用。
func (s *AgentStatusService) TouchHeartbeat(ctx context.Context, agentID uint) error {
	return s.agentRepo.TouchHeartbeat(ctx, agentID)
}

// StartHeartbeatMonitor 启动后台心跳检查（S-3，项目惯例 ticker 循环）。
//
// 每个周期执行 CheckStaleAgents：last_active_at 超过 timeout 的在线坐席
// 自动 offline，其在办会话转回 AI。重复调用幂等（仅启动一个 monitor）。
func (s *AgentStatusService) StartHeartbeatMonitor(ctx context.Context, interval, timeout time.Duration) {
	if interval <= 0 {
		interval = HeartbeatMonitorInterval
	}
	if timeout <= 0 {
		timeout = HeartbeatOfflineTimeout
	}
	s.mu.Lock()
	if s.monitorOn {
		s.mu.Unlock()
		return
	}
	s.monitorOn = true
	s.mu.Unlock()

	utils.SafeGo(ctx, "agent_status.heartbeat_monitor", func(ctx context.Context) {
		defer func() { s.mu.Lock(); s.monitorOn = false; s.mu.Unlock() }()
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				s.CheckStaleAgents(context.Background(), timeout) //nolint:ctxcheck 后台任务与请求生命周期解耦
			}
		}
	})
}

// CheckStaleAgents 扫描在线坐席：心跳超时者自动 offline 并释放在办会话。
//
// 判定：status ∈ {online,busy} 且 last_active_at 非空且距今 > timeout。
// （last_active_at 为空的在线坐席属存量数据，交由 GetOnlineAgents 的既有
// cutoff 过滤兜底，此处不动以免误伤。）
//
// 返回本次被自动下线的 agent_id 列表（供日志/告警）。
func (s *AgentStatusService) CheckStaleAgents(ctx context.Context, timeout time.Duration) []uint {
	if timeout <= 0 {
		timeout = HeartbeatOfflineTimeout
	}
	// 后台心跳扫描没有请求租户，而 agent_statuses 是租户表：裸 ctx 会被
	// 隔离 callback fail-closed 拒绝，表现为"在线坐席永远不自动下线"。
	// 逐活跃租户扇出；已绑定租户的 ctx（手动触发）直接单租户执行。
	if _, ok := tenant.FromContext(ctx); ok || !tenant.IsMultiTenant() {
		return s.checkStaleAgentsForTenant(ctx, timeout)
	}
	var offlined []uint
	if _, err := tenant.Fanout(ctx, nil, func(tctx context.Context, _ uint) (int64, error) {
		ids := s.checkStaleAgentsForTenant(tctx, timeout)
		offlined = append(offlined, ids...)
		return int64(len(ids)), nil
	}); err != nil {
		logger.Ctx(ctx).Warn().Err(err).Msg("[S-3] heartbeat check: tenant fanout failed")
	}
	return offlined
}

func (s *AgentStatusService) checkStaleAgentsForTenant(ctx context.Context, timeout time.Duration) []uint {
	agents, err := s.agentRepo.ListAllAgents(ctx)
	if err != nil {
		logger.Ctx(ctx).Warn().Err(err).Msg("[S-3] heartbeat check: list agents failed")
		return nil
	}

	cutoff := time.Now().Add(-timeout)
	offlined := make([]uint, 0)
	for _, a := range agents {
		if a == nil || (a.Status != "online" && a.Status != "busy") {
			continue
		}
		if a.LastActiveAt == nil || a.LastActiveAt.After(cutoff) {
			continue
		}
		returned := s.returnSessionsToAI(ctx, a.AgentID)
		if err := s.agentRepo.UpdateStatus(ctx, a.AgentID, "offline"); err != nil {
			logger.Ctx(ctx).Error().Err(err).
				Uint("agent_id", a.AgentID).
				Msg("[S-3] auto-offline stale agent failed")
			continue
		}
		offlined = append(offlined, a.AgentID)
		logger.Ctx(ctx).Info().
			Uint("agent_id", a.AgentID).
			Int("sessions_returned", returned).
			Time("last_active_at", *a.LastActiveAt).
			Msg("[S-3] agent heartbeat timeout: auto-offline, sessions returned to AI")
	}
	return offlined
}

func (s *AgentStatusService) returnSessionsToAI(ctx context.Context, agentID uint) int {
	sessions, err := s.sessionRepo.GetAgentSessions(ctx, agentID)
	if err != nil {
		logger.Ctx(ctx).Warn().Err(err).
			Uint("agent_id", agentID).
			Msg("[S-3] list agent sessions for return-to-AI failed")
		return 0
	}
	escalation := GetGlobalEscalationManager()
	returned := 0
	for _, sess := range sessions {
		if sess == nil {
			continue
		}
		if escalation != nil {
			if err := escalation.ReleaseHumanLock(ctx, sess.SessionID); err != nil {
				logger.Ctx(ctx).Warn().Err(err).
					Str("session_id", sess.SessionID).
					Msg("[S-3] release human lock failed (continue)")
			}
		}
		if err := s.sessionRepo.UpdateHandlerType(ctx, sess.ID, model.HandlerTypeAI); err != nil {
			logger.Ctx(ctx).Warn().Err(err).
				Str("session_id", sess.SessionID).
				Msg("[S-3] set session handler=ai failed")
			continue
		}
		if err := s.sessionRepo.UpdateStatus(ctx, sess.ID, model.SessionStatusAIHandling); err != nil {
			logger.Ctx(ctx).Warn().Err(err).
				Str("session_id", sess.SessionID).
				Msg("[S-3] set session status=ai_handling failed")
			continue
		}
		if err := s.agentRepo.DecrementActiveSessions(ctx, agentID); err != nil {
			logger.Ctx(ctx).Warn().Err(err).
				Uint("agent_id", agentID).
				Msg("[S-3] decrement active sessions failed")
		}
		returned++
	}
	return returned
}
