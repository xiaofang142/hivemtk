package service

import (
	"context"
	"time"

	"hivemtk-user/internal/repository"
)

// CustomerQueueService 客服队列与容量管理业务层
type CustomerQueueService struct {
	agentRepo   *repository.AgentStatusRepository
	sessionRepo *repository.CustomerSessionRepository
}

// NewCustomerQueueService 构造
func NewCustomerQueueService() *CustomerQueueService {
	return &CustomerQueueService{
		agentRepo:   repository.NewAgentStatusRepository(),
		sessionRepo: repository.NewCustomerSessionRepository(),
	}
}

// NewCustomerQueueServiceWithRepos 注入 repo（测试用）
func NewCustomerQueueServiceWithRepos(
	agentRepo *repository.AgentStatusRepository,
	sessionRepo *repository.CustomerSessionRepository,
) *CustomerQueueService {
	return &CustomerQueueService{agentRepo: agentRepo, sessionRepo: sessionRepo}
}

// QueueSnapshot 队列快照
type QueueSnapshot struct {
	WaitingCount     int64 `json:"waiting_count"`
	LongestWaitSec   int64 `json:"longest_wait_sec"`
	EstimatedWaitSec int64 `json:"estimated_wait_sec"`
}

// GetQueue 获取客服队列长度
func (s *CustomerQueueService) GetQueue(ctx context.Context) (*QueueSnapshot, error) {
	waitingCount, err := s.sessionRepo.CountPendingUnassigned(ctx)
	if err != nil {
		return nil, err
	}

	availableAgents, err := s.agentRepo.CountByStatus(ctx, "available")
	if err != nil {
		return nil, err
	}

	estimated := int64(0)
	longestWait := int64(0)
	if availableAgents > 0 {
		estimated = waitingCount / availableAgents * 60
	}

	return &QueueSnapshot{
		WaitingCount:     waitingCount,
		LongestWaitSec:   longestWait,
		EstimatedWaitSec: estimated,
	}, nil
}

// CapacitySnapshot 坐席容量快照
type CapacitySnapshot struct {
	TotalAgents      int64   `json:"total_agents"`
	OnlineAgents     int64   `json:"online_agents"`
	AvailableAgents  int64   `json:"available_agents"`
	BusyAgents       int64   `json:"busy_agents"`
	OfflineAgents    int64   `json:"offline_agents"`
	TotalMaxCapacity int64   `json:"total_max_capacity"`
	TotalActiveLoad  int64   `json:"total_active_load"`
	AvgLoadRatio     float64 `json:"avg_load_ratio"`
}

// GetCapacity 获取坐席容量聚合视图
func (s *CustomerQueueService) GetCapacity(ctx context.Context) (*CapacitySnapshot, error) {
	agents, err := s.agentRepo.ListAllAgents(ctx)
	if err != nil {
		return nil, err
	}

	result := &CapacitySnapshot{}
	result.TotalAgents = int64(len(agents))
	for _, a := range agents {
		switch a.Status {
		case "offline":
			result.OfflineAgents++
		case "busy":
			result.BusyAgents++
			result.OnlineAgents++
		case "available", "idle":
			result.AvailableAgents++
			result.OnlineAgents++
		default:
			result.OnlineAgents++
		}
		result.TotalMaxCapacity += int64(a.MaxSessions)
		result.TotalActiveLoad += int64(a.ActiveSessions)
	}

	if result.TotalMaxCapacity > 0 {
		result.AvgLoadRatio = float64(result.TotalActiveLoad) / float64(result.TotalMaxCapacity)
	}

	return result, nil
}

// AgentStatusItem 简化的坐席状态项（不含敏感字段）
type AgentStatusItem struct {
	AgentID        uint    `json:"agent_id"`
	AgentName      string  `json:"agent_name"`
	Status         string  `json:"status"`
	MaxSessions    int     `json:"max_sessions"`
	ActiveSessions int     `json:"active_sessions"`
	LoadRatio      float64 `json:"load_ratio"`
	// AutoOnlineAt/Source：J-13 选项 A——「回复时自动上线」只作提示，不改
	// status。管理端据此显示「回复时自动上线」，在线率/派单仍按 status 计。
	AutoOnlineAt     *time.Time `json:"auto_online_at,omitempty"`
	AutoOnlineSource string     `json:"auto_online_source,omitempty"`
}

// GetAgents 获取所有坐席的实时状态列表
func (s *CustomerQueueService) GetAgents(ctx context.Context) ([]AgentStatusItem, error) {
	agents, err := s.agentRepo.ListAllAgents(ctx)
	if err != nil {
		return nil, err
	}
	items := make([]AgentStatusItem, 0, len(agents))
	for _, a := range agents {
		ratio := 0.0
		if a.MaxSessions > 0 {
			ratio = float64(a.ActiveSessions) / float64(a.MaxSessions)
		}
		items = append(items, AgentStatusItem{
			AgentID:          a.AgentID,
			AgentName:        a.AgentName,
			Status:           a.Status,
			MaxSessions:      a.MaxSessions,
			ActiveSessions:   a.ActiveSessions,
			LoadRatio:        ratio,
			AutoOnlineAt:     a.AutoOnlineAt,
			AutoOnlineSource: a.AutoOnlineSource,
		})
	}
	return items, nil
}
