package service

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/repository"
)

// CustomerDataScope 客户列表/导出/详情的数据权限范围。
// 四个维度为 AND 叠加：可见账号 ∩ 可见行业 ∩ 负责人 ∩ 登录身份。
type CustomerDataScope struct {
	UserID             uint     `json:"user_id"`
	AllAccounts        bool     `json:"all_accounts"`
	RestrictedAccounts bool     `json:"restricted_accounts"`
	AccountIDs         []string `json:"account_ids"`
	Industries         []string `json:"industries"`
	// OwnerUserID > 0 表示调用方只能查看该负责人名下的客户；0 表示不按负责人收敛。
	OwnerUserID uint `json:"owner_user_id"`
	// OnlyOwned 为 true 时强制按当前登录用户作为负责人过滤（授权角色 owner）。
	OnlyOwned bool `json:"only_owned"`
}

// CustomerPermissionService 数据权限过滤（FR-002/010/016）。
//
// 复用两块既有地基：
//   - BusinessScopeService：082 business_account_authorizations + 系统用户角色（账号维度）
//   - 092 channel_account_authorizations（Phase 2 新建，原 RBAC 表不动）：行业维度 + owner 角色
//
// 绝不修改 RBAC 表或 RoutePolicyGuard / TenantMiddleware 挂载顺序；本服务是新增过滤层。
type CustomerPermissionService struct {
	authRepo    repository.ChannelAccountAuthorizationRepository
	accountRepo repository.ChannelAccountRepository
	scopeSvc    *BusinessScopeService
	customers   repository.CRMCustomerRepository
}

func NewCustomerPermissionService() *CustomerPermissionService {
	return &CustomerPermissionService{
		authRepo:    repository.NewChannelAccountAuthorizationRepository(),
		accountRepo: repository.NewChannelAccountRepository(),
		scopeSvc:    NewBusinessScopeService(),
		customers:   repository.NewCRMCustomerRepository(),
	}
}

// Resolve 解析当前用户的数据权限范围。缺租户时仓储层 fail-closed；
// userID=0（无登录态）一律 restricted+空集合，拒绝全部数据。
func (s *CustomerPermissionService) Resolve(ctx context.Context, userID uint) (CustomerDataScope, error) {
	scope := CustomerDataScope{UserID: userID}
	if userID == 0 {
		scope.RestrictedAccounts = true
		return scope, nil
	}

	bizScope, err := s.scopeSvc.Resolve(ctx, userID)
	if err != nil {
		// fail-closed：权限解析失败时拒绝全部，禁止零值 scope 导致 fail-open 暴露整租户客户。
		// controller.scope() 吞掉 err，故此处必须自行闭合；空 AccountIDs + RestrictedAccounts=true
		// 会使 repository.applyCustomerScope 生成 1=0。
		scope.RestrictedAccounts = true
		return scope, err
	}
	if bizScope.AllAccounts {
		// 管理员/全量数据作用域不被 Phase 2 授权表反向收敛。
		scope.AllAccounts = true
		return scope, nil
	}
	bizIDs, bizRestricted := bizScope.AllowedSourceAccountIDs()

	var phase2IDs []string
	industrySet := make(map[string]bool)
	auths, err := s.authRepo.ListByUser(ctx, userID)
	if err != nil {
		// fail-closed：同上，授权表读取失败不得退化为不过滤。
		scope.RestrictedAccounts = true
		return scope, err
	}
	channelIDs := make([]uint, 0, len(auths))
	for _, a := range auths {
		if a.Role == "owner" {
			scope.OnlyOwned = true
		}
		for _, ind := range parseVisibleIndustries(a.VisibleIndustryScope) {
			industrySet[ind] = true
		}
		if a.ChannelAccountID > 0 {
			channelIDs = append(channelIDs, a.ChannelAccountID)
			phase2IDs = append(phase2IDs, strconv.FormatUint(uint64(a.ChannelAccountID), 10))
		}
	}
	// 渠道账号 numeric id 与 customers.source_account_id 的映射：
	// 识别码是 Phase 2 的权威对外标识，一并加入允许集合；查不到账号时保留数字 id 兜底，
	// 不让一条授权因关联行缺失而静默失效。
	if len(channelIDs) > 0 && s.accountRepo != nil && s.accountRepo.Available() {
		tid, ok := tenant.FromContext(ctx)
		if !ok || tid == 0 {
			tid = tenant.WriteTenantID(ctx)
		}
		if accounts, aerr := s.accountRepo.GetByIDs(ctx, tid, channelIDs); aerr == nil {
			for _, acc := range accounts {
				if acc.IdentifyCode != "" {
					phase2IDs = append(phase2IDs, acc.IdentifyCode)
				}
			}
		}
	}

	seen := make(map[string]bool)
	merged := make([]string, 0, len(bizIDs)+len(phase2IDs))
	for _, id := range append(append([]string{}, bizIDs...), phase2IDs...) {
		id = strings.TrimSpace(id)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		merged = append(merged, id)
	}

	// fail-closed：业务授权表判定受限且没有任何账号可授权时，拒绝全部；
	// 若只有 Phase 2 授权表有数据，同样以它为受限集合。
	if (bizRestricted || len(phase2IDs) > 0) && len(merged) == 0 {
		scope.RestrictedAccounts = true
		return scope, nil
	}
	if len(merged) > 0 {
		scope.RestrictedAccounts = true
		scope.AccountIDs = merged
	}
	for ind := range industrySet {
		scope.Industries = append(scope.Industries, ind)
	}
	return scope, nil
}

func parseVisibleIndustries(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	var arr []string
	if err := json.Unmarshal([]byte(raw), &arr); err == nil {
		return arr
	}
	var obj struct {
		Industries []string `json:"industries"`
	}
	if err := json.Unmarshal([]byte(raw), &obj); err == nil {
		return obj.Industries
	}
	return nil
}

// ToFilter 把权限范围叠加到调用方过滤条件上。
func (s *CustomerPermissionService) ToFilter(scope CustomerDataScope, base repository.CustomerScopeFilter) repository.CustomerScopeFilter {
	if !scope.AllAccounts {
		base.RestrictedAccounts = scope.RestrictedAccounts || base.RestrictedAccounts
		if scope.RestrictedAccounts && len(base.SourceAccountIDs) == 0 {
			base.SourceAccountIDs = scope.AccountIDs
		} else if len(scope.AccountIDs) > 0 {
			base.SourceAccountIDs = intersectCRMStrings(base.SourceAccountIDs, scope.AccountIDs)
		}
		if len(scope.Industries) > 0 {
			base.Industries = intersectCRMStrings(base.Industries, scope.Industries)
		}
	}
	if scope.OnlyOwned {
		base.OwnerUserID = scope.UserID
	} else if scope.OwnerUserID > 0 {
		base.OwnerUserID = scope.OwnerUserID
	}
	return base
}

// ListScoped 按权限范围读取客户列表。
func (s *CustomerPermissionService) ListScoped(ctx context.Context, scope CustomerDataScope, base repository.CustomerScopeFilter) ([]*model.Customer, int64, error) {
	return s.customers.ListScoped(ctx, s.ToFilter(scope, base))
}

// GetByIDScoped 读取单个客户并校验权限（越权返回 ErrCRMCustomerForbidden）。
func (s *CustomerPermissionService) GetByIDScoped(ctx context.Context, scope CustomerDataScope, id string) (*model.Customer, error) {
	row, err := s.customers.GetByIDScoped(ctx, id)
	if err != nil {
		return nil, err
	}
	if !s.CanAccess(scope, row) {
		return nil, ErrCRMCustomerForbidden
	}
	return row, nil
}

// ErrCRMCustomerForbidden 越权访问客户（与 customer.go 的 ErrCRMCustomerForbidden 区分）。
var ErrCRMCustomerForbidden = errors.New("无权访问该客户")

// CanAccess 判断某客户是否在权限范围内。
func (s *CustomerPermissionService) CanAccess(scope CustomerDataScope, c *model.Customer) bool {
	if c == nil {
		return false
	}
	if scope.AllAccounts {
		return true
	}
	if scope.RestrictedAccounts {
		if len(scope.AccountIDs) == 0 || !containsCRMString(scope.AccountIDs, c.SourceAccountID) {
			return false
		}
	}
	if len(scope.Industries) > 0 && !containsCRMString(scope.Industries, c.Industry) {
		return false
	}
	if scope.OnlyOwned && c.OwnerUserID != nil && *c.OwnerUserID != scope.UserID {
		return false
	}
	if scope.OwnerUserID > 0 && (c.OwnerUserID == nil || *c.OwnerUserID != scope.OwnerUserID) {
		return false
	}
	return true
}

func containsCRMString(list []string, v string) bool {
	for _, item := range list {
		if item == v {
			return true
		}
	}
	return false
}

func intersectCRMStrings(base, allowed []string) []string {
	if len(base) == 0 {
		return append([]string{}, allowed...)
	}
	out := make([]string, 0, len(base))
	for _, v := range base {
		if containsCRMString(allowed, v) {
			out = append(out, v)
		}
	}
	return out
}
