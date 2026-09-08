package service

import (
	"context"
	"errors"
	"sync"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/utils"
	"hivemtk-user/internal/pkg/utils/logger"
	"hivemtk-user/internal/repository"
)

const (
	// HeartbeatOfflineTimeout 工作台连接断开判定窗口：last_active_at 超过该时长
	// 未刷新的在线坐席视为失联，自动 offline 并释放在办会话。
	HeartbeatOfflineTimeout = 5 * time.Minute

	// HeartbeatMonitorInterval 后台心跳检查默认间隔
	HeartbeatMonitorInterval = time.Minute
)

// AgentStatusService 客服状态服务
type AgentStatusService struct {
	agentRepo *repository.AgentStatusRepository

	mu        sync.Mutex
	monitorOn bool
}

// NewAgentStatusService 创建客服状态服务实例
func NewAgentStatusService() *AgentStatusService {
	return &AgentStatusService{
		agentRepo: repository.NewAgentStatusRepository(),
	}
}

// CreateAgentRequest 创建客服请求
type CreateAgentRequest struct {
	AgentID     uint   `json:"agent_id" binding:"required"`
	AgentName   string `json:"agent_name" binding:"required"`
	MaxSessions int    `json:"max_sessions"`
}

// CreateAgent 创建客服状态记录
func (s *AgentStatusService) CreateAgent(ctx context.Context, req *CreateAgentRequest) (*model.AgentStatus, error) {
	agent := &model.AgentStatus{
		AgentID:     req.AgentID,
		AgentName:   req.AgentName,
		Status:      "offline",
		MaxSessions: req.MaxSessions,
	}
	if agent.MaxSessions == 0 {
		agent.MaxSessions = 5
	}

	if err := s.agentRepo.Create(ctx, agent); err != nil {
		return nil, err
	}

	return agent, nil
}

// GetAgentStatus 获取客服状态
func (s *AgentStatusService) GetAgentStatus(ctx context.Context, agentID uint) (*model.AgentStatus, error) {
	agent, err := s.agentRepo.GetByAgentID(ctx, agentID)
	if err != nil {
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
func (s *AgentStatusService) UpdateAgentStatus(ctx context.Context, agentID uint, status string) error {
	agent, err := s.agentRepo.GetByAgentID(ctx, agentID)
	if err != nil {
		return err
	}
	_ = agent

	if agent.Status == "offline" && status != "online" {
		return errors.New("客服离线时只能切换到在线状态")
	}

	return s.agentRepo.UpdateStatus(ctx, agentID, status)
}

// GoOnline 客服上线
func (s *AgentStatusService) GoOnline(ctx context.Context, agentID uint) error {
	return s.UpdateAgentStatus(ctx, agentID, "online")
}

// GoOffline 客服下线
func (s *AgentStatusService) GoOffline(ctx context.Context, agentID uint) error {
	agent, err := s.agentRepo.GetByAgentID(ctx, agentID)
	if err != nil {
		return err
	}
	_ = agent

	if agent.ActiveSessions > 0 {
		return errors.New("还有未完成的会话，请先处理或转接")
	}

	return s.agentRepo.UpdateStatus(ctx, agentID, "offline")
}

// GetAgentSessions 获取客服的活跃会话
func (s *AgentStatusService) GetAgentSessions(ctx context.Context, agentID uint) ([]*model.CustomerSession, error) {
	sessionRepo := repository.NewCustomerSessionRepository()
	return sessionRepo.GetAgentSessions(ctx, agentID)
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
	sessionRepo := repository.NewCustomerSessionRepository()
	sessions, err := sessionRepo.GetAgentSessions(ctx, agentID)
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
		if err := sessionRepo.UpdateHandlerType(ctx, sess.ID, model.HandlerTypeAI); err != nil {
			logger.Ctx(ctx).Warn().Err(err).
				Str("session_id", sess.SessionID).
				Msg("[S-3] set session handler=ai failed")
			continue
		}
		if err := sessionRepo.UpdateStatus(ctx, sess.ID, model.SessionStatusAIHandling); err != nil {
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
