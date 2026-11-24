package service

import (
	"context"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/repository"
)

// LeadScopedListService 配置人员权限内的消息/客户列表（FR-010）。
//
// 复用 Phase 2 ChannelResolver/fail-closed 口径与 Phase 3 CustomerPermissionService：
// 数据权限在 Service 层叠加，不改原 RBAC 表、不碰 RoutePolicyGuard / TenantMiddleware
// 挂载顺序。userID=0 或权限解析失败一律返回空列表（拒绝全部），绝不 fail-open。
type LeadScopedListService struct {
	perm       *CustomerPermissionService
	judgements repository.LeadMiningJudgementRepository
	customers  repository.CRMCustomerRepository
}

func NewLeadScopedListService() *LeadScopedListService {
	return &LeadScopedListService{
		perm:       NewCustomerPermissionService(),
		judgements: repository.NewLeadMiningJudgementRepository(),
		customers:  repository.NewCRMCustomerRepository(),
	}
}

// ListMessages 按可见账号范围返回判定/消息记录。
func (s *LeadScopedListService) ListMessages(ctx context.Context, userID uint, page, limit int, platform, leadRole string, matched *bool) ([]model.LeadMiningJudgement, int64, error) {
	if s == nil || s.judgements == nil {
		return nil, 0, ErrLeadNotInitialized
	}
	scope, err := s.perm.Resolve(ctx, userID)
	if err != nil {
		return nil, 0, err // fail-closed：权限解析失败不返回任何数据
	}
	if scope.AllAccounts {
		return s.judgements.List(ctx, page, limit, platform, "", leadRole, matched)
	}
	if scope.RestrictedAccounts && len(scope.AccountIDs) == 0 {
		return []model.LeadMiningJudgement{}, 0, nil
	}
	return s.judgements.ListScoped(ctx, scope.AccountIDs, page, limit, platform, leadRole, matched)
}

// ListCustomers 按可见账号 + 行业 + 负责人范围返回客户。
func (s *LeadScopedListService) ListCustomers(ctx context.Context, userID uint, page, limit int, keyword string) ([]*model.Customer, int64, error) {
	if s == nil || s.customers == nil {
		return nil, 0, ErrLeadNotInitialized
	}
	scope, err := s.perm.Resolve(ctx, userID)
	if err != nil {
		return nil, 0, err
	}
	filter := repository.CustomerScopeFilter{
		SourceAccountIDs:   scope.AccountIDs,
		RestrictedAccounts: scope.RestrictedAccounts && !scope.AllAccounts,
		Industries:         scope.Industries,
		OwnerUserID:        scope.OwnerUserID,
		Keyword:            keyword,
		ExcludeMerged:      true,
		Page:               page,
		PageSize:           limit,
	}
	if scope.OnlyOwned && userID > 0 {
		filter.OwnerUserID = userID
	}
	return s.customers.ListScoped(ctx, filter)
}

// ErrLeadNotInitialized 权限/仓储未装配：fail-closed。
var ErrLeadNotInitialized = errLeadNotInitialized("线索列表服务未装配")

type errLeadNotInitialized string

func (e errLeadNotInitialized) Error() string { return string(e) }
