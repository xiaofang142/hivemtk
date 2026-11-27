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

	// 第 1 道：模块闸。
	//
	// 2026-11-27 修复（多租户菜单口径缺陷，线上实测复现）：
	// 此前这里用 model.GetModuleEnabledFilter()，那是**进程级单快照** ——
	// ModuleEnabledFunc() 取 EnabledKeys(WithSystemScope)，而 SystemScope 在
	// gateTenantOf 里恒定解析为 DefaultTenantID。后果是：
	//
	//   非默认租户购买的付费模块，接口闸（IsEnabled，逐请求按租户）放行 200，
	//   菜单却按默认租户的开通集渲染 ⇒ 整枝被剪掉 ⇒ 买到了但侧栏没有入口。
	//   线上样本：租户 18 购买 browser-automation（¥1 买断、12 个菜单键），
	//   /api/browser-automation/tasks 返回 200，而 /api/menus 一个键都不下发。
	//
	// 这不是"菜单与接口短暂不一致"，只要购买方不是默认租户就**永久不一致**：
	// refreshProcessGate 重灌的仍是 SystemScope 视图，重灌多少次都算不进租户 18 的购买。
	//
	// 现在改成按**当前请求的租户**判定：EnabledKeys(ctx) 读的就是 ctx 绑定租户的镜像
	// （TenantMiddleware 已在 auth 组上注入，本方法只从 /api/menus 调用，ctx 必带租户）。
	// 模块闸与接口闸自此同源同口径。
	//
	// 保留全局 filter 作兜底：仅当 gate 服务不可用（未装配）时才用它，
	// 保证离线首启 / 单测不注入时不会因闸没接上而空菜单。
	moduleTree := model.FilterMenuTreeByModule(model.SystemMenuTree, s.moduleEnabledFuncFor(ctx))

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

// moduleEnabledFuncFor 构造**按当前请求租户**判定的模块开通函数。
//
// 与 ModuleGateService.ModuleEnabledFunc() 的区别只有一点，但正是缺陷所在：
// 那个方法把 ctx 固定成 tenant.WithSystemScope（= DefaultTenantID 视图）并固化进
// 闭包，供 cron / 装配等**没有请求租户**的场景使用；本方法用调用方 ctx，
// 于是读的是**这台机器上当前登录用户所属租户**的模块镜像。
//
// 为什么必须这样：菜单是"这台租户买到了什么"的呈现，和接口闸（IsEnabled，
// 逐请求按租户）必须是同一口径。菜单按默认租户渲染而接口按真实租户放行，
// 结果就是「买到了但侧栏没有入口」—— 租户 18 买 browser-automation 的线上样本即是。
//
// 兜底口径：gate 服务不可用（未注入仓储 / 离线首启）时回落到全局 filter；
// 全局 filter 也没有（测试环境未注入）时返回 nil，FilterMenuTreeByModule 会
// 原样返回全树，保证不会因闸没接上而空菜单。
func (s *MenuService) moduleEnabledFuncFor(ctx context.Context) func(string) bool {
	gate := NewModuleGateService()
	// gate 服务不可用（进程单例未装配 / 全局 DB 未就绪 / 离线首启）时，
	// 回落 to 进程级全局 filter，保证不因闸没接上而空菜单。
	if gate == nil || (gate.repo == nil && !repository.DBReady()) {
		return model.GetModuleEnabledFilter()
	}
	keys := gate.EnabledKeys(ctx)
	return func(moduleKey string) bool {
		if moduleKey == "" {
			return true
		}
		_, ok := keys[moduleKey]
		return ok
	}
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
