package service

import (
	"context"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/repository"
)

// OperationLogService 操作日志门面服务
type OperationLogService struct {
	logRepo repository.OperationLogRepository
}

// NewOperationLogService 创建操作日志门面服务
func NewOperationLogService() *OperationLogService {
	return &OperationLogService{logRepo: repository.NewOperationLogRepository()}
}

// OperationLogView 操作日志视图（脱离 model 的 DTO）
type OperationLogView struct {
	ID         uint      `json:"id"`
	UserID     uint      `json:"user_id"`
	Username   string    `json:"username"`
	Action     string    `json:"action"`
	Module     string    `json:"module"`
	Resource   string    `json:"resource"`
	ResourceID string    `json:"resource_id"`
	IP         string    `json:"ip"`
	CreatedAt  time.Time `json:"created_at"`
}

func toOperationLogView(log *model.OperationLog) *OperationLogView {
	if log == nil {
		return nil
	}
	return &OperationLogView{
		ID:         log.ID,
		UserID:     log.UserID,
		Username:   log.Username,
		Action:     log.Action,
		Module:     log.Module,
		Resource:   log.Resource,
		ResourceID: log.ResourceID,
		IP:         log.IP,
		CreatedAt:  log.CreatedAt,
	}
}

// Create 写入操作日志（审计 sink 用，保持 Router→Service→Repository 分层）
func (s *OperationLogService) Create(ctx context.Context, log *model.OperationLog) error {
	return s.logRepo.Create(ctx, log)
}

// GetAll 获取操作日志列表
func (s *OperationLogService) GetAll(ctx context.Context, page, pageSize int, filters map[string]any) ([]*OperationLogView, int64, error) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 50000 {
		pageSize = 20
	}
	logs, total, err := s.logRepo.GetAll(ctx, page, pageSize, filters)
	if err != nil {
		return nil, 0, err
	}
	out := make([]*OperationLogView, 0, len(logs))
	for _, l := range logs {
		out = append(out, toOperationLogView(l))
	}
	return out, total, nil
}

// GetByID 获取操作日志详情
func (s *OperationLogService) GetByID(ctx context.Context, id uint) (*OperationLogView, error) {
	log, err := s.logRepo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if log == nil {
		return nil, nil
	}
	return toOperationLogView(log), nil
}

// GetByUserID 获取指定用户的操作日志
func (s *OperationLogService) GetByUserID(ctx context.Context, userID uint, page, pageSize int) ([]*OperationLogView, int64, error) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 50000 {
		pageSize = 20
	}
	logs, total, err := s.logRepo.GetByUserID(ctx, userID, page, pageSize)
	if err != nil {
		return nil, 0, err
	}
	out := make([]*OperationLogView, 0, len(logs))
	for _, l := range logs {
		out = append(out, toOperationLogView(l))
	}
	return out, total, nil
}

// DeleteOldLogs 清理指定日期之前的操作日志
func (s *OperationLogService) DeleteOldLogs(ctx context.Context, cutoff time.Time) error {
	return s.logRepo.DeleteOldLogs(ctx, cutoff)
}

// DeleteByIDs 批量删除操作日志，返回删除条数
func (s *OperationLogService) DeleteByIDs(ctx context.Context, ids []uint) (int64, error) {
	return s.logRepo.DeleteByIDs(ctx, ids)
}

// OperationLogStats 操作日志统计
type OperationLogStats struct {
	Total       int64            `json:"total"`
	ModuleStats map[string]int64 `json:"module_stats"`
	ActionStats map[string]int64 `json:"action_stats"`
	UserStats   map[string]int64 `json:"user_stats"`
}

// GetStatistics 获取操作日志统计（统计整表）
func (s *OperationLogService) GetStatistics(ctx context.Context) (*OperationLogStats, error) {
	logs, total, err := s.logRepo.GetAll(ctx, 1, 1000, nil)
	if err != nil {
		return nil, err
	}
	moduleStats := make(map[string]int64)
	actionStats := make(map[string]int64)
	userStats := make(map[string]int64)
	for _, log := range logs {
		moduleStats[log.Module]++
		actionStats[log.Action]++
		if log.Username != "" {
			userStats[log.Username]++
		}
	}
	return &OperationLogStats{
		Total:       total,
		ModuleStats: moduleStats,
		ActionStats: actionStats,
		UserStats:   userStats,
	}, nil
}

// ExportAll 导出操作日志（返回视图列表，controller 负责渲染 CSV）
func (s *OperationLogService) ExportAll(ctx context.Context, pageSize int) ([]*OperationLogView, error) {
	if pageSize <= 0 || pageSize > 50000 {
		pageSize = 10000
	}
	logs, _, err := s.logRepo.GetAll(ctx, 1, pageSize, nil)
	if err != nil {
		return nil, err
	}
	out := make([]*OperationLogView, 0, len(logs))
	for _, l := range logs {
		out = append(out, toOperationLogView(l))
	}
	return out, nil
}
