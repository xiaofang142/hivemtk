package service

import (
	"context"
	"fmt"
	"sort"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/utils/logger"
	"hivemtk-user/internal/repository"
)

// MenuService 菜单权限服务（RBAC 菜单分配的业务层）
//
// 规则：
//   - admin 恒返回全量菜单，不落库、不可被分配
//   - customer_service/staff 按 role_menu_permissions 表过滤
//   - 表的分配行还要过注册表的角色上界（model.EligibleMenuKeys）：
//     注册表收归超管的节点，即便老库里留着分配行，读侧也不再放行、写侧直接拒收
//   - 首次全表为空时按注册表 Roles 词表自动 seed 一次默认分配
type MenuService struct {
	repo repository.RoleMenuPermissionRepository
}

// NewMenuService 构造
func NewMenuService() *MenuService {
	return &MenuService{repo: repository.NewRoleMenuPermissionRepository()}
}

// NewMenuServiceWithRepo 注入 repository（便于测试）
func NewMenuServiceWithRepo(repo repository.RoleMenuPermissionRepository) *MenuService {
	return &MenuService{repo: repo}
}

// GetAllMenuTree 返回完整注册表（管理端 el-tree 展示用）
func (s *MenuService) GetAllMenuTree() []model.SystemMenuNode {
	return model.SystemMenuTree
}

// GetUserMenuTree 当前登录用户可见菜单树
//
// 两道闸的顺序不可换（.specify/002-modular-subscription FR-5.5）：
//
//	**先模块闸，再角色授权**。
//
// 反过来（先角色后模块）会有两个症状：
//  1. 超管走 `admin` 分支直接返回全树 —— 模块闸被整个跳过，
//     于是"菜单看得见、点进去才 403"，正是要避免的那种半开状态；
//  2. 非超管则要先在库里算出角色授权、再按模块裁，多一轮无用功。
//
// 错误：角色非法 → ErrInvalidInput
func (s *MenuService) GetUserMenuTree(ctx context.Context, role string) ([]model.SystemMenuNode, error) {
	// 兼容 v1 历史值：自助注册/SSO 写入的 role="user" 不在 v3.1 三档内，
	// 直接拒绝会让这类账号 /api/menus 报「角色不存在」、前端路由守卫拿不到
	// 菜单树而卡死在默认页。按 role.go 的既定口径归一化为 staff 再走两道闸。
	role = model.NormalizeRole(role)

	// 第 1 道：模块闸。未装配判定函数时 FilterMenuTreeByModule 原样返回全树
	// （离线首启 / 单测不注入），保证不会因为"闸没接上"而空菜单。
	moduleTree := model.FilterMenuTreeByModule(model.SystemMenuTree, model.GetModuleEnabledFilter())

	if role == model.SystemRoleCodeAdmin {
		return moduleTree, nil
	}
	if !model.IsValidRole(role) {
		return nil, fmt.Errorf("角色不存在: %w", ErrInvalidInput)
	}
	set, err := s.grantedKeysFor(ctx, role, true)
	if err != nil {
		return nil, err
	}
	// 第 2 道：角色授权。
	//
	// 注意这里传 moduleTree 而不是 SystemMenuTree：FilterMenuTree 是按 key
	// 在**给定树**上剪枝的，若传全树，已被模块闸剪掉的分支会因"子节点仍命中
	// 角色授权"而复活 —— 于是关掉的模块又被角色授权请回来。
	return model.FilterMenuTree(moduleTree, set), nil
}

// GetRoleMenuKeys 某角色**有效**的菜单 key 列表（admin 返回全量）
//
// 返回的是"库里分配 ∩ 注册表上界"，与管理端回显、侧栏、能力面守卫同一份口径，
// 所以界面上勾了却不在上界里的节点不会回显成"已拥有"。
//
// 错误：角色非法 → ErrInvalidInput
func (s *MenuService) GetRoleMenuKeys(ctx context.Context, role string) ([]string, error) {
	// 同 GetUserMenuTree：历史 role="user" 归一化为 staff，避免管理端回显/守卫口径分裂
	role = model.NormalizeRole(role)
	if role == model.SystemRoleCodeAdmin {
		return model.FlattenMenuKeys(model.SystemMenuTree), nil
	}
	if !model.IsValidRole(role) {
		return nil, fmt.Errorf("角色不存在: %w", ErrInvalidInput)
	}
	set, err := s.grantedKeysFor(ctx, role, true)
	if err != nil {
		return nil, err
	}
	keys := make([]string, 0, len(set))
	for k := range set {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys, nil
}

// UpdateRoleMenuKeys 覆盖式保存某角色的菜单分配
//
// 业务规则：
//   - admin 不可被分配（恒全量）→ ErrInvalidInput
//   - 角色 code 非法 → ErrInvalidInput
//   - 任一 menu key 不在注册表 → ErrInvalidInput
//   - 任一 menu key 不在该角色的注册表上界内 → ErrInvalidInput
//     （落库会被读侧的上界交集抹掉，那样"保存成功但没生效"最难查，所以在写侧就拒）
//   - keys 为空 = 清空该角色全部菜单（合法，前端应二次确认）
func (s *MenuService) UpdateRoleMenuKeys(ctx context.Context, role string, keys []string) error {
	if role == model.SystemRoleCodeAdmin {
		return fmt.Errorf("超管拥有全部菜单权限，无需分配: %w", ErrInvalidInput)
	}
	if !model.IsValidRole(role) {
		return fmt.Errorf("角色不存在: %w", ErrInvalidInput)
	}
	eligible := model.EligibleMenuKeys(role)
	for _, k := range keys {
		if !model.ExistsMenuKey(k) {
			return fmt.Errorf("菜单不存在: %s: %w", k, ErrInvalidInput)
		}
		if _, ok := eligible[k]; !ok {
			return fmt.Errorf("菜单 %s 是超管专属节点，不能授予角色 %s: %w", k, role, ErrInvalidInput)
		}
	}
	return s.repo.ReplaceRoleKeys(ctx, role, keys)
}

// GrantProvider 给能力面守卫用的"角色 → 已授予菜单 key 集合"查询。
//
// 非法/空角色返回空集合：能力面按"无授予"拦（向拒的方向收）。
// admin 返回 nil（守卫对 admin 短路，不走到这里）。
//
// 首空表**不落库**：守卫跑在读到空表的那一刻，写库不是它的职责，
// 直接按 DefaultMenuKeys 的出厂分配判，保证首屏菜单和首屏接口档位一致。
func (s *MenuService) GrantProvider(ctx context.Context, role string) (map[string]struct{}, error) {
	return s.grantedKeysFor(ctx, role, false)
}

// grantedKeysFor 某角色的有效授予集合 = 库里的分配行 ∩ 注册表上界。
//
// 侧栏树、分配回显、能力面守卫三处读都收敛到这一个函数：菜单"看不到"与接口
// "调不动"从此是同一个集合的两次消费，不会再出现一处生效一处漏（菜单藏着、接口放行
// 就是越权入口；菜单给了、接口拦人是满屏 403）。
//
// seedWhenEmpty 只区分一件事：读的是用户界面（可以顺手把首空表填上）还是守卫
// （只读，不该在请求链路上写库）。两种情况下上界交集都照常生效。
func (s *MenuService) grantedKeysFor(ctx context.Context, role string, seedWhenEmpty bool) (map[string]struct{}, error) {
	if role == model.SystemRoleCodeAdmin {
		return nil, nil
	}
	if !model.IsValidRole(role) {
		return map[string]struct{}{}, nil
	}
	n, err := s.repo.CountAll(ctx)
	if err != nil {
		return nil, err
	}
	var assigned []string
	if n == 0 {
		if seedWhenEmpty {
			s.seedDefaults(ctx)
		}
		assigned = model.DefaultMenuKeys(role)
	} else {
		assigned, err = s.repo.ListKeysByRole(ctx, role)
		if err != nil {
			return nil, err
		}
	}
	eligible := model.EligibleMenuKeys(role)
	set := make(map[string]struct{}, len(assigned))
	for _, k := range assigned {
		if _, ok := eligible[k]; ok {
			set[k] = struct{}{}
		}
	}
	return set, nil
}

// seedDefaults 首空表为 customer_service/staff 写入默认分配（admin 不落库）
//
// seed 失败不阻断读取：并发 seed 撞唯一索引时以已写入行为准。
func (s *MenuService) seedDefaults(ctx context.Context) {
	for _, role := range []string{model.SystemRoleCodeCustomerService, model.SystemRoleCodeStaff} {
		keys := model.DefaultMenuKeys(role)
		rows := make([]*model.RoleMenuPermission, 0, len(keys))
		for _, k := range keys {
			rows = append(rows, &model.RoleMenuPermission{RoleCode: role, MenuKey: k})
		}
		if err := s.repo.BulkInsert(ctx, rows); err != nil {
			logger.Warnf("[MenuService] seed 默认菜单分配失败 role=%s: %v", role, err)
		}
	}
}
