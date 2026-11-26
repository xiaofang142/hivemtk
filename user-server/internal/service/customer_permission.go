package service

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"strconv"
	"strings"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/repository"
)

// CustomerDataScope 客户列表/导出/详情的数据权限范围。
// 四个维度为 AND 叠加：可见账号 ∩ 可见行业 ∩ 负责人 ∩ 登录身份。
// CustomerAccountRule 保留单条授权的账号 × 行业 × 角色配对关系。
// 存在 AccountRules 时，判定以配对为准，禁止把不同账号的行业/角色组合成全局集合后交叉放行。
type CustomerAccountRule struct {
	AccountID        string   `json:"account_id"`
	SourceAccountIDs []string `json:"-"`                    // 本次权限解析验证过的来源引用，不作为持久 ACL
	Industries       []string `json:"industries,omitempty"` // 空表示不限行业
	Role             string   `json:"role,omitempty"`
	OwnerUserID      uint     `json:"owner_user_id,omitempty"` // >0 表示该账号仅查看此负责人
}

type CustomerDataScope struct {
	UserID             uint     `json:"user_id"`
	AllAccounts        bool     `json:"all_accounts"`
	RestrictedAccounts bool     `json:"restricted_accounts"`
	AccountIDs         []string `json:"account_ids"`
	Industries         []string `json:"industries"`
	// AccountRules 为逐账号授权配对；零值表示旧 scope（按全局集合语义兼容）。
	AccountRules []CustomerAccountRule `json:"account_rules,omitempty"`
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
	scope := CustomerDataScope{UserID: userID, RestrictedAccounts: true}
	if userID == 0 {
		scope.RestrictedAccounts = true
		return scope, nil
	}
	tid := tenant.WriteTenantID(ctx)
	if tid == 0 {
		return scope, errors.New("缺少租户上下文，无法解析客户权限")
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
	bizIDs, _ := bizScope.AllowedSourceAccountIDs()
	auths, err := s.authRepo.ListByUser(ctx, userID)
	if err != nil {
		// fail-closed：同上，授权表读取失败不得退化为不过滤。
		scope.RestrictedAccounts = true
		return scope, err
	}
	if len(auths) == 0 && len(bizIDs) == 0 {
		return scope, nil
	}
	if s.accountRepo == nil || !s.accountRepo.Available() {
		return scope, repository.ErrChannelStoreUnavailable
	}
	channelIDs := make([]uint, 0, len(auths)+len(bizIDs))
	businessIDs := make(map[string]uint)
	for _, ref := range bizIDs {
		if id, err := strconv.ParseUint(ref, 10, strconv.IntSize); err == nil {
			if id > 0 && strconv.FormatUint(id, 10) == ref {
				businessIDs[ref] = uint(id)
				channelIDs = append(channelIDs, uint(id))
			}
			continue
		}
		account, err := s.accountRepo.GetByIdentifyCode(ctx, ref)
		if errors.Is(err, repository.ErrAmbiguousIdentifyCode) {
			continue
		}
		if err != nil {
			return scope, err
		}
		if account != nil && account.TenantID == tid {
			businessIDs[ref] = account.ID
			channelIDs = append(channelIDs, account.ID)
		}
	}
	for _, auth := range auths {
		if auth.ChannelAccountID > 0 {
			channelIDs = append(channelIDs, auth.ChannelAccountID)
		}
	}
	accounts, err := s.accountRepo.GetByIDs(ctx, tid, channelIDs)
	if err != nil {
		return scope, err
	}
	refs := make(map[uint][]string, len(accounts))
	for _, account := range accounts {
		refs[account.ID] = []string{strconv.FormatUint(uint64(account.ID), 10)}
	}
	if reader, ok := s.accountRepo.(repository.CRMAccountReferenceReader); ok {
		refs, err = reader.GetCRMSourceReferences(ctx, tid, channelIDs)
		if err != nil {
			return scope, err
		}
	}
	var rules []CustomerAccountRule
	seen := make(map[string]bool)
	addRule := func(id uint, inds []string, role string) {
		idRef := strconv.FormatUint(uint64(id), 10)
		if !containsCRMString(refs[id], idRef) {
			return
		}
		rules = append(rules, CustomerAccountRule{AccountID: idRef, SourceAccountIDs: refs[id], Industries: inds, Role: role})
		seen[idRef] = true
	}
	for _, ref := range bizIDs {
		if id := businessIDs[ref]; id > 0 && containsCRMString(refs[id], ref) {
			addRule(id, nil, "viewer")
		}
	}
	for _, auth := range auths {
		inds, valid := parseVisibleIndustries(auth.VisibleIndustryScope)
		role := strings.ToLower(strings.TrimSpace(auth.Role))
		if !valid || (role != "owner" && role != "viewer") {
			continue
		}
		addRule(auth.ChannelAccountID, inds, role)
	}
	for id := range seen {
		scope.AccountIDs = append(scope.AccountIDs, id)
	}
	sort.Strings(scope.AccountIDs)
	scope.AccountRules = rules
	return scope, nil
}

func parseVisibleIndustries(raw string) ([]string, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, true
	}
	var arr []string
	if strings.HasPrefix(raw, "[") {
		if json.Unmarshal([]byte(raw), &arr) != nil {
			return nil, false
		}
	} else {
		var obj struct {
			Industries []string `json:"industries"`
		}
		if !strings.HasPrefix(raw, "{") || json.Unmarshal([]byte(raw), &obj) != nil || obj.Industries == nil {
			return nil, false
		}
		arr = obj.Industries
	}
	for _, industry := range arr {
		if strings.TrimSpace(industry) == "" {
			return nil, false
		}
	}
	return arr, true
}

// ToFilter 把权限范围叠加到调用方过滤条件上。
func (s *CustomerPermissionService) ToFilter(scope CustomerDataScope, base repository.CustomerScopeFilter) repository.CustomerScopeFilter {
	if base.DenyAll {
		return base
	}
	if !scope.AllAccounts {
		base.RestrictedAccounts = scope.RestrictedAccounts || base.RestrictedAccounts
		allowedRefs := append([]string{}, scope.AccountIDs...)
		if len(scope.AccountRules) > 0 {
			allowedRefs = nil
			base.AccountRules = make([]repository.CustomerAccountScopeRule, 0, len(scope.AccountRules))
			for _, rule := range scope.AccountRules {
				if scope.RestrictedAccounts && !containsCRMString(scope.AccountIDs, rule.AccountID) {
					continue
				}
				refs := rule.SourceAccountIDs
				if len(refs) == 0 {
					refs = []string{rule.AccountID}
				}
				owner := rule.OwnerUserID
				if strings.EqualFold(rule.Role, "owner") {
					if scope.UserID == 0 || (owner > 0 && owner != scope.UserID) {
						continue
					}
					owner = scope.UserID
				}
				base.AccountRules = append(base.AccountRules, repository.CustomerAccountScopeRule{
					SourceAccountIDs: refs, Industries: rule.Industries, OwnerUserID: owner,
				})
				allowedRefs = append(allowedRefs, refs...)
			}
			if len(base.AccountRules) == 0 {
				base.DenyAll = true
			}
		}
		if scope.RestrictedAccounts && len(allowedRefs) == 0 {
			base.DenyAll = true
		}
		if scope.RestrictedAccounts && len(base.SourceAccountIDs) == 0 {
			base.SourceAccountIDs = allowedRefs
		} else if len(allowedRefs) > 0 {
			base.SourceAccountIDs = intersectCRMStrings(base.SourceAccountIDs, allowedRefs)
			if len(base.SourceAccountIDs) == 0 {
				base.DenyAll = true
			}
		}
		if len(scope.Industries) > 0 {
			if len(base.Industries) == 0 {
				base.Industries = append([]string{}, scope.Industries...)
			} else {
				base.Industries = intersectCRMStrings(base.Industries, scope.Industries)
				if len(base.Industries) == 0 {
					base.DenyAll = true
				}
			}
		}
	}
	owner := scope.OwnerUserID
	if scope.OnlyOwned {
		owner = scope.UserID
		if owner == 0 {
			base.DenyAll = true
		}
	}
	if owner > 0 {
		if base.OwnerUserID > 0 && base.OwnerUserID != owner {
			base.DenyAll = true
		}
		base.OwnerUserID = owner
	}
	return base
}

// ListScoped 按权限范围读取客户列表。
func (s *CustomerPermissionService) ListScoped(ctx context.Context, scope CustomerDataScope, base repository.CustomerScopeFilter) ([]*model.Customer, int64, error) {
	return s.customers.ListScoped(ctx, s.ToFilter(scope, base))
}

// EntityAccountFilter 将客户的行业和负责人代入每条账号授权，供关联实体的 SQL 过滤复用。
func (s *CustomerPermissionService) EntityAccountFilter(scope CustomerDataScope, customer *model.Customer) repository.CRMEntityAccountFilter {
	filter := repository.CRMEntityAccountFilter{RestrictedAccounts: !scope.AllAccounts}
	if customer == nil || !s.CanAccess(scope, customer) {
		filter.RestrictedAccounts = true
		return filter
	}
	filter.AllowUnassigned = true
	ids := append([]string(nil), scope.AccountIDs...)
	for _, rule := range scope.AccountRules {
		ids = append(ids, rule.AccountID)
	}
	seen := make(map[uint]bool)
	for _, ref := range ids {
		id, err := strconv.ParseUint(ref, 10, strconv.IntSize)
		if err != nil || id == 0 || strconv.FormatUint(id, 10) != ref || seen[uint(id)] {
			continue
		}
		candidate := *customer
		candidate.SourceAccountID = ref
		if s.CanAccess(scope, &candidate) {
			seen[uint(id)] = true
			filter.AccountIDs = append(filter.AccountIDs, uint(id))
		}
	}
	return filter
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
	if scope.RestrictedAccounts && len(scope.AccountRules) == 0 {
		if len(scope.AccountIDs) == 0 || !containsCRMString(scope.AccountIDs, c.SourceAccountID) {
			return false
		}
	}
	if len(scope.Industries) > 0 && !containsCRMString(scope.Industries, c.Industry) {
		return false
	}
	// OnlyOwned：无负责人（OwnerUserID=nil）不算「我的客户」。
	// 若放行 nil，owner 角色可通过详情/360/AI/合并等按 ID 读取的入口
	// 越权访问未分配客户的资料（列表 SQL 的 owner_user_id=? 已经排除 nil）。
	if scope.OnlyOwned && (scope.UserID == 0 || c.OwnerUserID == nil || *c.OwnerUserID != scope.UserID) {
		return false
	}
	if scope.OwnerUserID > 0 && (c.OwnerUserID == nil || *c.OwnerUserID != scope.OwnerUserID) {
		return false
	}
	if len(scope.AccountRules) > 0 {
		matched := false
		for _, rule := range scope.AccountRules {
			if scope.RestrictedAccounts && !containsCRMString(scope.AccountIDs, rule.AccountID) {
				continue
			}
			refs := rule.SourceAccountIDs
			if len(refs) == 0 {
				refs = []string{rule.AccountID}
			}
			if !containsCRMString(refs, c.SourceAccountID) {
				continue
			}
			if len(rule.Industries) > 0 && !containsCRMString(rule.Industries, c.Industry) {
				continue
			}
			if strings.EqualFold(rule.Role, "owner") {
				if scope.UserID == 0 || c.OwnerUserID == nil || *c.OwnerUserID != scope.UserID {
					continue
				}
			}
			if rule.OwnerUserID > 0 && (c.OwnerUserID == nil || *c.OwnerUserID != rule.OwnerUserID) {
				continue
			}
			matched = true
			break
		}
		if !matched {
			return false
		}
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
