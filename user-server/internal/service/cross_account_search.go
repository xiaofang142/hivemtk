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

type CrossAccountSearchService struct {
	repo     repository.CrossAccountSearchRepository
	scopeSvc *BusinessScopeService
}

func NewCrossAccountSearchService() *CrossAccountSearchService {
	return &CrossAccountSearchService{
		repo:     repository.NewCrossAccountSearchRepository(),
		scopeSvc: NewBusinessScopeService(),
	}
}

func NewCrossAccountSearchServiceWithRepo(repo repository.CrossAccountSearchRepository, scopeSvc *BusinessScopeService) *CrossAccountSearchService {
	if scopeSvc == nil {
		scopeSvc = NewBusinessScopeService()
	}
	return &CrossAccountSearchService{repo: repo, scopeSvc: scopeSvc}
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
	scopeInfo = CrossAccountSearchScope{
		AllAccounts:            !restricted,
		Restricted:             restricted,
		AuthorizedAccountCount: len(ids),
	}
	if restricted && len(ids) == 0 {
		return []repository.CrossAccountSearchRow{}, 0, scopeInfo, nil
	}
	rows, total, err := s.repo.SearchCustomers(ctx, tid, keyword, industry, ids, !restricted, page, pageSize)
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