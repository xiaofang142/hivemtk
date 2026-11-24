package service

import (
	"context"
	"strings"

	"hivemtk-user/internal/repository"
)

// CrossAccountSearchRow 是 repository 层的搜索行类型别名，供 controller 层使用，
// 避免 controller 直接引用 repository 包（分层约束）。
type CrossAccountSearchRow = repository.CrossAccountSearchRow

type CrossAccountSearchScope struct {
	AllAccounts            bool `json:"all_accounts"`
	Restricted             bool `json:"restricted"`
	AuthorizedAccountCount int  `json:"authorized_account_count"`
}

// CrossAccountPermissionResolver 由 CustomerPermissionService 实现；
// 测试可用 no-op 桩避免真实 DB 依赖。
type CrossAccountPermissionResolver interface {
	Resolve(ctx context.Context, userID uint) (CustomerDataScope, error)
}

type allAccountsPermissionResolver struct{}

func (allAccountsPermissionResolver) Resolve(context.Context, uint) (CustomerDataScope, error) {
	return CustomerDataScope{AllAccounts: true}, nil
}

type CrossAccountSearchService struct {
	repo     repository.CrossAccountSearchRepository
	scopeSvc *BusinessScopeService
	perm     CrossAccountPermissionResolver
}

func NewCrossAccountSearchService() *CrossAccountSearchService {
	return &CrossAccountSearchService{
		repo:     repository.NewCrossAccountSearchRepository(),
		scopeSvc: NewBusinessScopeService(),
		perm:     NewCustomerPermissionService(),
	}
}

// NewCrossAccountSearchServiceWithRepo 兼容旧测试构造：不注入真实 Phase2 权限（no-op）。
func NewCrossAccountSearchServiceWithRepo(repo repository.CrossAccountSearchRepository, scopeSvc *BusinessScopeService) *CrossAccountSearchService {
	if scopeSvc == nil {
		scopeSvc = NewBusinessScopeService()
	}
	return &CrossAccountSearchService{repo: repo, scopeSvc: scopeSvc, perm: allAccountsPermissionResolver{}}
}

// NewCrossAccountSearchServiceWithDeps 显式注入 Phase2 权限解析（生产/新测试）。
func NewCrossAccountSearchServiceWithDeps(repo repository.CrossAccountSearchRepository, scopeSvc *BusinessScopeService, perm CrossAccountPermissionResolver) *CrossAccountSearchService {
	if scopeSvc == nil {
		scopeSvc = NewBusinessScopeService()
	}
	if perm == nil {
		perm = allAccountsPermissionResolver{}
	}
	return &CrossAccountSearchService{repo: repo, scopeSvc: scopeSvc, perm: perm}
}

func (s *CrossAccountSearchService) SearchCustomers(ctx context.Context, tid uint, userID uint, keyword, industry string, page, pageSize int) ([]repository.CrossAccountSearchRow, int64, CrossAccountSearchScope, error) {
	scopeInfo := CrossAccountSearchScope{}
	if s.scopeSvc == nil {
		scopeInfo = CrossAccountSearchScope{AllAccounts: true}
		rows, total, err := s.repo.SearchCustomers(ctx, tid, keyword, industry, nil, true, page, pageSize)
		if err != nil {
			return nil, 0, scopeInfo, err
		}
		maskCrossAccountRows(rows)
		return rows, total, scopeInfo, nil
	}
	scope, err := s.scopeSvc.Resolve(ctx, userID)
	if err != nil {
		return nil, 0, scopeInfo, err
	}
	ids, restricted := scope.AllowedSourceAccountIDs()
	// Phase2 数据授权（渠道账号/行业/负责人）与业务账号授权叠加，不可只取其一。
	// 业务层已判定 all accounts（管理员）时不再叠加受限集合——Phase2 也不应反向收敛管理员。
	var industries []string
	var ownerUserID uint
	if s.perm != nil && restricted {
		phase2, perr := s.perm.Resolve(ctx, userID)
		if perr != nil {
			return nil, 0, scopeInfo, perr // fail-closed
		}
		if !phase2.AllAccounts {
			restricted = true
			if len(phase2.AccountIDs) > 0 {
				ids = intersectCRMStrings(ids, phase2.AccountIDs)
			}
			industries = phase2.Industries
			if phase2.OnlyOwned {
				ownerUserID = phase2.UserID
			} else {
				ownerUserID = phase2.OwnerUserID
			}
		}
	}
	allAccounts := !restricted
	scopeInfo = CrossAccountSearchScope{
		AllAccounts:            allAccounts,
		Restricted:             restricted,
		AuthorizedAccountCount: len(ids),
	}
	if restricted && len(ids) == 0 {
		return []repository.CrossAccountSearchRow{}, 0, scopeInfo, nil
	}
	var rows []repository.CrossAccountSearchRow
	var total int64
	if scopedRepo, ok := s.repo.(repository.CrossAccountScopedSearchRepository); ok {
		// 用户输入的行业与授权行业取交集；授权为空时沿用用户输入。
		if industry != "" && len(industries) > 0 {
			allowed := intersectCRMStrings(industries, []string{industry})
			if len(allowed) == 0 {
				return []repository.CrossAccountSearchRow{}, 0, scopeInfo, nil
			}
			industries = allowed
		} else if len(industries) == 0 && industry != "" {
			industries = []string{industry}
		}
		rows, total, err = scopedRepo.SearchCustomersScoped(ctx, tid, keyword, ids, industries, ownerUserID, allAccounts, page, pageSize)
	} else {
		// 测试 fake 只实现旧接口：保持原调用，但行业已由仓储下推。
		rows, total, err = s.repo.SearchCustomers(ctx, tid, keyword, industry, ids, !restricted, page, pageSize)
	}
	if err != nil {
		return nil, 0, scopeInfo, err
	}
	maskCrossAccountRows(rows)
	return rows, total, scopeInfo, nil
}

func maskCrossAccountRows(rows []repository.CrossAccountSearchRow) {
	for i := range rows {
		rows[i].Phone = maskPhone(rows[i].Phone)
		rows[i].Email = maskEmail(rows[i].Email)
	}
}

func maskPhone(phone string) string {
	phone = strings.TrimSpace(phone)
	if phone == "" {
		return ""
	}
	runes := []rune(phone)
	n := len(runes)
	if n <= 4 {
		return strings.Repeat("*", n)
	}
	if n <= 7 {
		return string(runes[:1]) + strings.Repeat("*", n-2) + string(runes[n-1:])
	}
	return string(runes[:3]) + strings.Repeat("*", n-7) + string(runes[n-4:])
}

func maskEmail(email string) string {
	email = strings.TrimSpace(email)
	if email == "" {
		return ""
	}
	parts := strings.SplitN(email, "@", 2)
	local := []rune(parts[0])
	if len(local) == 0 {
		return "***@" + parts[1]
	}
	maskedLocal := string(local[:1]) + "***"
	if len(parts) == 1 {
		return maskedLocal
	}
	return maskedLocal + "@" + parts[1]
}
