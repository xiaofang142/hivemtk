package service

import (
	"context"
	"errors"
	"fmt"

	"hivemtk-user/internal/rbac"
	"hivemtk-user/internal/repository"
)

// rbac_admin.go 角色-权限覆盖层管理（backlog P1-1 权限可编辑，2026-10-11）。
//
// 语义（与 model.RolePermission 注释一致）：DB 优先、生成为兜底、super_admin 锁定。
// 可编辑角色：team_admin / member（内置角色的"谁有什么"可管；super_admin 恒全量）。

var (
	ErrRBACRoleLocked        = errors.New("super_admin 权限恒为全量，不可编辑")
	ErrRBACRoleUnknown       = errors.New("未知矩阵角色（仅 team_admin/member 可编辑）")
	ErrRBACPermissionUnknown = errors.New("权限点不在目录中（目录以 registry.yaml 生成为准）")
)

// RbacAdminService 权限覆盖层管理。
type RbacAdminService struct {
	repo *repository.RolePermissionRepo
}

func NewRbacAdminService() *RbacAdminService {
	r := repository.NewRolePermissionRepo()
	if !r.Available() {
		return &RbacAdminService{}
	}
	return &RbacAdminService{repo: r}
}

func (s *RbacAdminService) Ready() bool { return s.repo != nil }

// editableRole 可编辑角色白名单。
func editableRole(roleCode string) bool {
	return roleCode == "team_admin" || roleCode == "member"
}

// permissionCatalog 合法权限点集合（生成层）。
func permissionCatalog() map[string]bool {
	catalog := map[string]bool{}
	for _, p := range rbac.PermissionCatalog() {
		catalog[p] = true
	}
	return catalog
}

// GetRolePermissions 角色当前生效权限 + 是否覆盖态。
func (s *RbacAdminService) GetRolePermissions(ctx context.Context, roleCode string) (permissions []string, override bool, err error) {
	if roleCode == "super_admin" {
		return rbac.PermissionsFor("super_admin"), false, nil
	}
	if !editableRole(roleCode) {
		return nil, false, ErrRBACRoleUnknown
	}
	if !s.Ready() {
		return rbac.PermissionsFor(roleCode), false, nil
	}
	has, err := s.repo.HasAnyOverride(ctx, roleCode)
	if err != nil {
		return nil, false, err
	}
	if !has {
		return rbac.PermissionsFor(roleCode), false, nil
	}
	perms, err := s.repo.ListByRole(ctx, roleCode)
	if err != nil {
		return nil, false, err
	}
	return perms, true, nil
}

// SetRolePermissions 覆盖式保存（super_admin 拒绝；权限点必须在目录内；空集=清空覆盖回落默认）。
func (s *RbacAdminService) SetRolePermissions(ctx context.Context, roleCode string, codes []string) error {
	if roleCode == "super_admin" {
		return ErrRBACRoleLocked
	}
	if !editableRole(roleCode) {
		return ErrRBACRoleUnknown
	}
	catalog := permissionCatalog()
	seen := map[string]bool{}
	for _, c := range codes {
		if !catalog[c] {
			return fmt.Errorf("%w: %s", ErrRBACPermissionUnknown, c)
		}
		if seen[c] {
			return fmt.Errorf("权限点重复: %s", c)
		}
		seen[c] = true
	}
	if !s.Ready() {
		return errors.New("db 未就绪")
	}
	if err := s.repo.Replace(ctx, roleCode, codes); err != nil {
		return err
	}
	rbac.InvalidateOverrides()
	return nil
}
