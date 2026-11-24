package service

import (
	"context"
	"strings"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/repository"
)

// BusinessAccountScope describes which business accounts a user may access.
// PR-5/D-03,D-06: business-account authorization is independent from employee
// account identity; query chains must filter by this scope.
type BusinessAccountScope struct {
	UserID         uint
	Role           string
	DataScope      string
	AllAccounts    bool
	Authorizations []model.BusinessAccountAuthorization
}

// Allows reports whether the scope permits access to channel+accountID.
// An empty channel or accountID is treated as an unscoped record and is only
// allowed when the caller has all-account access; restricted users need an
// explicit authorization.
func (s BusinessAccountScope) Allows(channel, accountID string) bool {
	if s.AllAccounts {
		return true
	}
	channel = strings.TrimSpace(channel)
	accountID = strings.TrimSpace(accountID)
	if accountID == "" {
		return false
	}
	for _, a := range s.Authorizations {
		if strings.TrimSpace(a.AccountID) != accountID {
			continue
		}
		if strings.TrimSpace(a.Channel) == "" || channel == "" || strings.EqualFold(strings.TrimSpace(a.Channel), channel) {
			return true
		}
	}
	return false
}

// AllowedSourceAccountIDs returns source account IDs for query IN filters.
// The second return value is false when all accounts are allowed.
func (s BusinessAccountScope) AllowedSourceAccountIDs() ([]string, bool) {
	if s.AllAccounts {
		return nil, false
	}
	seen := make(map[string]struct{}, len(s.Authorizations))
	ids := make([]string, 0, len(s.Authorizations))
	for _, a := range s.Authorizations {
		id := strings.TrimSpace(a.AccountID)
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		ids = append(ids, id)
	}
	return ids, true
}

// BusinessScopeUserReader is the minimal user lookup needed by scope resolution.
type BusinessScopeUserReader interface {
	GetByID(ctx context.Context, id uint) (*model.SystemUser, error)
}

// BusinessScopeAuthReader is the minimal auth lookup needed by scope resolution.
type BusinessScopeAuthReader interface {
	ListByUser(ctx context.Context, userID uint) ([]model.BusinessAccountAuthorization, error)
}

// BusinessScopeService resolves business-account scope for CRM queries.
type BusinessScopeService struct {
	authRepo BusinessScopeAuthReader
	userRepo BusinessScopeUserReader
}

// NewBusinessScopeService constructs a service with production repositories.
func NewBusinessScopeService() *BusinessScopeService {
	return &BusinessScopeService{
		authRepo: repository.NewBusinessAccountAuthRepository(),
		userRepo: repository.NewSystemUserRepository(),
	}
}

// NewBusinessScopeServiceWithReaders constructs a service with injected readers for tests.
func NewBusinessScopeServiceWithReaders(authRepo BusinessScopeAuthReader, userRepo BusinessScopeUserReader) *BusinessScopeService {
	return &BusinessScopeService{authRepo: authRepo, userRepo: userRepo}
}

// Resolve computes the business-account scope for userID.
// Admin role or data_scope=all gets full access; other users get explicitly
// authorized business accounts only.
func (s *BusinessScopeService) Resolve(ctx context.Context, userID uint) (BusinessAccountScope, error) {
	scope := BusinessAccountScope{UserID: userID}
	if userID == 0 {
		return scope, nil
	}
	if s.userRepo != nil {
		u, err := s.userRepo.GetByID(ctx, userID)
		if err != nil {
			return scope, err
		}
		if u != nil {
			scope.Role = u.Role
			scope.DataScope = u.DataScope
			if u.Role == model.SystemUserRoleAdmin || u.DataScope == model.DataScopeAll {
				scope.AllAccounts = true
				return scope, nil
			}
		}
	}
	if s.authRepo == nil {
		return scope, nil
	}
	auths, err := s.authRepo.ListByUser(ctx, userID)
	if err != nil {
		return scope, err
	}
	scope.Authorizations = auths
	return scope, nil
}

// CustomerSourceAccountClause returns a WHERE fragment for source_account_id.
// ok=false means all accounts allowed; ok=true with empty args means deny all.
func (s BusinessAccountScope) CustomerSourceAccountClause() (clause string, args []any, restricted bool) {
	if s.AllAccounts {
		return "", nil, false
	}
	ids, _ := s.AllowedSourceAccountIDs()
	if len(ids) == 0 {
		return "1 = 0", nil, true
	}
	anyIDs := make([]any, 0, len(ids))
	for _, id := range ids {
		anyIDs = append(anyIDs, id)
	}
	return "source_account_id IN ?", []any{anyIDs}, true
}
