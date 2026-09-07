package service

import (
	"context"
	"errors"
	"fmt"

	"gorm.io/gorm"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/utils/logger"
	"hivemtk-user/internal/repository"
)

// AuthorizationService 授权管理服务
//
// 与 service/permission_check.go 中 PermissionService 命名区分：
//   - PermissionService：业务权限点判断（频道权限/资源权限）
//   - AuthorizationService：admin 授权操作（启停/改密/审计）
type AuthorizationService struct {
	userRepo     repository.SystemUserRepository
	auditService *OperationLogService
}

// NewAuthorizationService 构造
func NewAuthorizationService() *AuthorizationService {
	return &AuthorizationService{
		userRepo:     repository.NewSystemUserRepository(),
		auditService: NewOperationLogService(),
	}
}

// NewAuthorizationServiceWithRepo 注入 repository（便于测试）
func NewAuthorizationServiceWithRepo(userRepo repository.SystemUserRepository, audit *OperationLogService) *AuthorizationService {
	return &AuthorizationService{userRepo: userRepo, auditService: audit}
}

// SetEnabled 启用/禁用账号
func (s *AuthorizationService) SetEnabled(ctx context.Context, actorID, targetID uint, enabled bool) error {
	if actorID == targetID {
		return fmt.Errorf("不能启停自己的账号: %w", ErrInvalidInput)
	}
	target, err := s.userRepo.GetByID(ctx, targetID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return fmt.Errorf("用户不存在: %w", ErrInvalidInput)
		}
		return err
	}
	if !enabled && target.Role == model.SystemUserRoleAdmin {
		enabledCount, err := s.userRepo.CountEnabledAdmins(ctx)
		if err != nil {
			return err
		}
		if enabledCount <= 1 {
			return fmt.Errorf("系统至少需要保留一个启用的超管账号: %w", ErrInvalidInput)
		}
	}
	if err := s.userRepo.SetEnabled(ctx, targetID, enabled); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return fmt.Errorf("用户不存在: %w", ErrInvalidInput)
		}
		return err
	}
	action := "user.enable"
	if !enabled {
		action = "user.disable"
	}
	if err := s.writeAuditLog(ctx, actorID, targetID, target.Username, action,
		fmt.Sprintf("actor=%d -> target=%d (%s) enabled=%v", actorID, targetID, target.Username, enabled)); err != nil {
		logger.Errorf("[permission] 审计写库失败（用户 %s %s）: %v", target.Username, action, err)
	}
	return nil
}

// ResetPassword admin 重置密码
func (s *AuthorizationService) ResetPassword(ctx context.Context, actorID, targetID uint, newPassword string) error {
	if actorID == targetID {
		return fmt.Errorf("不能重置自己的密码，请使用修改密码功能: %w", ErrInvalidInput)
	}
	if targetID == initialAdminID {
		return ErrInitialAdminProtected
	}
	if err := validatePassword(newPassword); err != nil {
		return fmt.Errorf("%w: %s", ErrInvalidInput, err.Error())
	}
	target, err := s.userRepo.GetByID(ctx, targetID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return fmt.Errorf("用户不存在: %w", ErrInvalidInput)
		}
		return err
	}
	hashed, err := HashPassword(newPassword)
	if err != nil {
		return err
	}
	if err := s.userRepo.UpdatePassword(ctx, targetID, hashed); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return fmt.Errorf("用户不存在: %w", ErrInvalidInput)
		}
		return err
	}
	if err := s.writeAuditLog(ctx, actorID, targetID, target.Username, "user.reset_password",
		fmt.Sprintf("actor=%d -> target=%d (%s)", actorID, targetID, target.Username)); err != nil {
		logger.Errorf("[permission] 审计写库失败（重置密码）: %v", err)
	}
	return nil
}

// ListAuditLogsRequest 审计日志查询请求
type ListAuditLogsRequest struct {
	UserID   uint   `json:"user_id"`
	Action   string `json:"action"`
	Module   string `json:"module"`
	Page     int    `json:"page"`
	PageSize int    `json:"page_size"`
}

// ListAuditLogsResponse 审计日志查询响应
type ListAuditLogsResponse struct {
	Total int64               `json:"total"`
	List  []*OperationLogView `json:"list"`
}

// ListAuditLogs 操作审计日志列表
func (s *AuthorizationService) ListAuditLogs(ctx context.Context, req *ListAuditLogsRequest) (*ListAuditLogsResponse, error) {
	page := req.Page
	if page < 1 {
		page = 1
	}
	size := req.PageSize
	if size <= 0 {
		size = 20
	}
	if size > 100 {
		size = 100
	}
	filters := map[string]any{"module": "role"}
	if req.UserID > 0 {
		filters["user_id"] = req.UserID
	}
	if req.Action != "" {
		filters["action"] = req.Action
	}
	logs, total, err := s.auditService.GetAll(ctx, page, size, filters)
	if err != nil {
		return nil, err
	}
	if req.Action != "" {
		filtered := make([]*OperationLogView, 0, len(logs))
		for _, l := range logs {
			if l.Action == req.Action {
				filtered = append(filtered, l)
			}
		}
		logs = filtered
		total = int64(len(logs))
	}
	return &ListAuditLogsResponse{Total: total, List: logs}, nil
}

func (s *AuthorizationService) writeAuditLog(ctx context.Context, actorID, targetID uint, targetUsername, action, detail string) error {
	log := &model.OperationLog{
		UserID:     actorID,
		Username:   targetUsername,
		Action:     action,
		Module:     "role",
		Resource:   "system_user",
		ResourceID: fmt.Sprintf("%d", targetID),
		Detail:     detail,
	}
	return repository.NewOperationLogRepository().Create(ctx, log)
}
