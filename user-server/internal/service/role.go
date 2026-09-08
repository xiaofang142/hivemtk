package service

import (
	"context"
	"fmt"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/utils/logger"
	"hivemtk-user/internal/repository"
)

// RoleWithCount 角色 + 成员数（DTO）
type RoleWithCount struct {
	model.SystemRole
	MemberCount int64 `json:"member_count"`
}

// RoleService 角色管理服务
type RoleService struct {
	userRepo repository.SystemUserRepository
}

// NewRoleService 构造
func NewRoleService() *RoleService {
	return &RoleService{
		userRepo: repository.NewSystemUserRepository(),
	}
}

// NewRoleServiceWithRepo 注入 repository（便于测试）
func NewRoleServiceWithRepo(repo repository.SystemUserRepository) *RoleService {
	return &RoleService{userRepo: repo}
}

func (s *RoleService) ListRoles(ctx context.Context) ([]*RoleWithCount, error) {
	codes := make([]string, 0, len(model.SystemRoleList))
	for _, r := range model.SystemRoleList {
		codes = append(codes, r.Code)
	}
	counts, err := s.userRepo.CountByRoles(ctx, codes)
	if err != nil {
		logger.Warnf("[RoleService] 批量统计角色成员数失败，全部记 0: %v", err)
		counts = map[string]int64{}
	}
	roles := make([]*RoleWithCount, 0, len(model.SystemRoleList))
	for _, r := range model.SystemRoleList {
		roles = append(roles, &RoleWithCount{
			SystemRole:  r,
			MemberCount: counts[r.Code],
		})
	}
	return roles, nil
}

// GetRole 按 code 取角色详情（带成员数）
//
// 错误：
//   - 角色 code 非法 → ErrInvalidInput
func (s *RoleService) GetRole(ctx context.Context, code string) (*RoleWithCount, error) {
	role := model.GetRoleByCode(code)
	if role == nil {
		return nil, fmt.Errorf("角色不存在: %w", ErrInvalidInput)
	}
	count, err := s.userRepo.CountByRole(ctx, code)
	if err != nil {
		return nil, err
	}
	return &RoleWithCount{
		SystemRole:  *role,
		MemberCount: count,
	}, nil
}

// ListMembersByRole 按角色分页查询成员
//
// 业务规则：
//   - 角色 code 非法 → ErrInvalidInput
//   - page < 1 → 1
//   - size <= 0 → 20，size > 100 → 100
//
// 返回值：成员列表（model.SystemUser）+ 总数
func (s *RoleService) ListMembersByRole(ctx context.Context, code string, page, size int) ([]*model.SystemUser, int64, error) {
	if !model.IsValidRole(code) {
		return nil, 0, fmt.Errorf("角色不存在: %w", ErrInvalidInput)
	}
	if page < 1 {
		page = 1
	}
	if size <= 0 {
		size = 20
	}
	if size > 100 {
		size = 100
	}
	users, total, err := s.userRepo.ListByRole(ctx, code, page, size)
	if err != nil {
		return nil, 0, err
	}
	return users, total, nil
}
