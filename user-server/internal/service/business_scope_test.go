package service

import (
	"context"
	"errors"
	"testing"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/repository"
)

type fakeBusinessScopeUserReader struct {
	user *model.SystemUser
	err  error
}

func (f fakeBusinessScopeUserReader) GetByID(_ context.Context, _ uint) (*model.SystemUser, error) {
	return f.user, f.err
}

type fakeBusinessScopeAuthReader struct {
	auths []model.BusinessAccountAuthorization
	err   error
}

func (f fakeBusinessScopeAuthReader) ListByUser(_ context.Context, _ uint) ([]model.BusinessAccountAuthorization, error) {
	return f.auths, f.err
}

func TestBusinessScopeService_ResolveAdminAllAccounts(t *testing.T) {
	svc := NewBusinessScopeServiceWithReaders(
		fakeBusinessScopeAuthReader{auths: []model.BusinessAccountAuthorization{{AccountID: "acc-1"}}},
		fakeBusinessScopeUserReader{user: &model.SystemUser{ID: 1, Role: model.SystemUserRoleAdmin, DataScope: model.DataScopeAll}},
	)
	scope, err := svc.Resolve(context.Background(), 1)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if !scope.AllAccounts {
		t.Fatalf("admin should have all accounts")
	}
	if !scope.Allows("wechat", "any-account") {
		t.Fatalf("admin scope should allow any account")
	}
}

func TestBusinessScopeService_ResolveDataScopeAll(t *testing.T) {
	svc := NewBusinessScopeServiceWithReaders(
		fakeBusinessScopeAuthReader{},
		fakeBusinessScopeUserReader{user: &model.SystemUser{ID: 2, Role: "staff", DataScope: model.DataScopeAll}},
	)
	scope, err := svc.Resolve(context.Background(), 2)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if !scope.AllAccounts {
		t.Fatalf("data_scope=all should allow all accounts")
	}
}

func TestBusinessScopeService_ResolveRestrictedUser(t *testing.T) {
	svc := NewBusinessScopeServiceWithReaders(
		fakeBusinessScopeAuthReader{auths: []model.BusinessAccountAuthorization{
			{UserID: 7, Channel: "wechat", AccountID: "wx-1"},
			{UserID: 7, Channel: "douyin", AccountID: "dy-1"},
		}},
		fakeBusinessScopeUserReader{user: &model.SystemUser{ID: 7, Role: "staff", DataScope: model.DataScopeSelf}},
	)
	scope, err := svc.Resolve(context.Background(), 7)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if scope.AllAccounts {
		t.Fatalf("restricted user should not have all accounts")
	}
	if !scope.Allows("wechat", "wx-1") {
		t.Fatalf("authorized account should be allowed")
	}
	if scope.Allows("wechat", "wx-other") {
		t.Fatalf("unauthorized account should be denied")
	}
	if scope.Allows("", "") {
		t.Fatalf("empty account should be denied for restricted user")
	}
	ids, restricted := scope.AllowedSourceAccountIDs()
	if !restricted {
		t.Fatalf("expected restricted scope")
	}
	if len(ids) != 2 {
		t.Fatalf("expected 2 allowed account IDs, got %d", len(ids))
	}
}

func TestBusinessScopeService_ResolveRestrictedUserNoAuths(t *testing.T) {
	svc := NewBusinessScopeServiceWithReaders(
		fakeBusinessScopeAuthReader{},
		fakeBusinessScopeUserReader{user: &model.SystemUser{ID: 8, Role: "staff", DataScope: model.DataScopeSelf}},
	)
	scope, err := svc.Resolve(context.Background(), 8)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if scope.AllAccounts {
		t.Fatalf("no-auth user should not have all accounts")
	}
	if scope.Allows("wechat", "wx-1") {
		t.Fatalf("no-auth user must not access unauthorized account")
	}
	ids, restricted := scope.AllowedSourceAccountIDs()
	if !restricted || len(ids) != 0 {
		t.Fatalf("expected restricted empty scope, restricted=%v ids=%v", restricted, ids)
	}
}

func TestBusinessScopeService_ResolveUserLookupError(t *testing.T) {
	wantErr := errors.New("db down")
	svc := NewBusinessScopeServiceWithReaders(fakeBusinessScopeAuthReader{}, fakeBusinessScopeUserReader{err: wantErr})
	_, err := svc.Resolve(context.Background(), 9)
	if !errors.Is(err, wantErr) {
		t.Fatalf("expected user lookup error, got %v", err)
	}
}

func TestBusinessAccountScope_CustomerSourceAccountClause(t *testing.T) {
	all := BusinessAccountScope{AllAccounts: true}
	clause, args, restricted := all.CustomerSourceAccountClause()
	if restricted || clause != "" || args != nil {
		t.Fatalf("all scope should not restrict: clause=%q args=%v restricted=%v", clause, args, restricted)
	}

	denied := BusinessAccountScope{UserID: 8}
	clause, _, restricted = denied.CustomerSourceAccountClause()
	if !restricted || clause != "1 = 0" {
		t.Fatalf("empty restricted scope should deny all, clause=%q restricted=%v", clause, restricted)
	}

	allowed := BusinessAccountScope{Authorizations: []model.BusinessAccountAuthorization{{AccountID: "wx-1"}, {AccountID: "wx-2"}}}
	clause, args, restricted = allowed.CustomerSourceAccountClause()
	if !restricted || clause != "source_account_id IN ?" || len(args) != 1 {
		t.Fatalf("expected IN clause, got clause=%q restricted=%v args=%v", clause, restricted, args)
	}
}

// ---- PR-5 补充：越权用例 & 查询链路接线 ----

type fakeCustomerRepoScoped struct {
	listByScopeCalled bool
	allowedIDs        []string
	allAccounts       bool
	rows              []*model.Customer
	total             int64
	err               error
}

func (f *fakeCustomerRepoScoped) Create(context.Context, *model.Customer) error { return nil }
func (f *fakeCustomerRepoScoped) GetByID(context.Context, string) (*model.Customer, error) {
	return nil, nil
}
func (f *fakeCustomerRepoScoped) GetByUnifiedID(context.Context, string) (*model.Customer, error) {
	return nil, nil
}
func (f *fakeCustomerRepoScoped) GetByPhone(context.Context, string) (*model.Customer, error) {
	return nil, nil
}
func (f *fakeCustomerRepoScoped) GetByEmail(context.Context, string) (*model.Customer, error) {
	return nil, nil
}
func (f *fakeCustomerRepoScoped) GetByWechatOpenID(context.Context, string) (*model.Customer, error) {
	return nil, nil
}
func (f *fakeCustomerRepoScoped) GetByDouyinOpenID(context.Context, string) (*model.Customer, error) {
	return nil, nil
}
func (f *fakeCustomerRepoScoped) Update(context.Context, *model.Customer) error { return nil }
func (f *fakeCustomerRepoScoped) Delete(context.Context, string) error          { return nil }
func (f *fakeCustomerRepoScoped) List(context.Context, int, int, string) ([]*model.Customer, int64, error) {
	return []*model.Customer{}, 0, nil
}
func (f *fakeCustomerRepoScoped) ListKeyset(context.Context, string, int, string) ([]*model.Customer, int64, string, error) {
	return []*model.Customer{}, 0, "", nil
}
func (f *fakeCustomerRepoScoped) FindByIdentity(context.Context, string, string, string, string, string) (*model.Customer, error) {
	return nil, nil
}
func (f *fakeCustomerRepoScoped) FindByIdentityAll(context.Context, string, string, string, string, string) ([]*model.Customer, error) {
	return nil, nil
}
func (f *fakeCustomerRepoScoped) CountNotEmpty(context.Context, string) (int64, error) { return 0, nil }
func (f *fakeCustomerRepoScoped) CountMultiIdentity(context.Context) (int64, error)    { return 0, nil }
func (f *fakeCustomerRepoScoped) ListByIDs(context.Context, []string) (map[string]*model.Customer, error) {
	return map[string]*model.Customer{}, nil
}
func (f *fakeCustomerRepoScoped) GetByXiaohongshuID(context.Context, string) (*model.Customer, error) {
	return nil, nil
}
func (f *fakeCustomerRepoScoped) SearchByFilter(context.Context, repository.CustomerSearchFilter) ([]*model.Customer, int64, error) {
	return nil, 0, nil
}
func (f *fakeCustomerRepoScoped) ReassignSessionOneID(context.Context, string, string) error {
	return nil
}
func (f *fakeCustomerRepoScoped) ReassignOneID(context.Context, string, string, string) error {
	return nil
}
func (f *fakeCustomerRepoScoped) ReassignDNCOneID(context.Context, string, string) error { return nil }
func (f *fakeCustomerRepoScoped) WithTransaction(ctx context.Context, fn func(ctx context.Context) error) error {
	return fn(ctx)
}
func (f *fakeCustomerRepoScoped) LockMergeCustomers(context.Context, string, string) (*model.Customer, *model.Customer, error) {
	return nil, nil, nil
}
func (f *fakeCustomerRepoScoped) LinkConsentIdentities(context.Context, string, string) error {
	return nil
}
func (f *fakeCustomerRepoScoped) ListBySourceScope(_ context.Context, _ int, _ int, _ string, allowedIDs []string, allAccounts bool) ([]*model.Customer, int64, error) {
	f.listByScopeCalled = true
	f.allowedIDs = append([]string(nil), allowedIDs...)
	f.allAccounts = allAccounts
	if f.err != nil {
		return nil, 0, f.err
	}
	return f.rows, f.total, nil
}

var _ repository.CustomerRepository = (*fakeCustomerRepoScoped)(nil)

func TestBusinessAccountScope_AllowsCrossChannelCaseInsensitive(t *testing.T) {
	scope := BusinessAccountScope{Authorizations: []model.BusinessAccountAuthorization{{Channel: "WeChat", AccountID: "wx-1"}}}
	if !scope.Allows("wechat", "wx-1") {
		t.Fatalf("channel match should be case-insensitive")
	}
	if scope.Allows("douyin", "wx-1") {
		t.Fatalf("different channel should be denied")
	}
}

func TestBusinessAccountScope_EmptyAccountDeniedForRestricted(t *testing.T) {
	scope := BusinessAccountScope{Authorizations: []model.BusinessAccountAuthorization{{AccountID: "wx-1"}}}
	if scope.Allows("", "") {
		t.Fatalf("empty account should be denied for restricted scope")
	}
}

func TestCustomerQueryService_ListCustomers_ScopedDenied(t *testing.T) {
	repo := &fakeCustomerRepoScoped{}
	svc := &CustomerQueryService{
		custRepo: repo,
		scopeSvc: NewBusinessScopeServiceWithReaders(
			fakeBusinessScopeAuthReader{}, // no auths
			fakeBusinessScopeUserReader{user: &model.SystemUser{ID: 8, Role: "staff", DataScope: model.DataScopeSelf}},
		),
	}
	rows, total := svc.ListCustomers(context.Background(), 1, 20, "", 8)
	if total != 0 || len(rows) != 0 {
		t.Fatalf("expected empty result for unauthorized user, got total=%d rows=%d", total, len(rows))
	}
	if !repo.listByScopeCalled {
		t.Fatalf("expected scoped list to be called")
	}
	if repo.allAccounts {
		t.Fatalf("restricted user should not query all accounts")
	}
	if len(repo.allowedIDs) != 0 {
		t.Fatalf("no-auth user should have no allowed IDs, got %v", repo.allowedIDs)
	}
}

func TestCustomerQueryService_ListCustomers_ScopedAllowed(t *testing.T) {
	repo := &fakeCustomerRepoScoped{
		rows:  []*model.Customer{{ID: "c1", UnifiedID: "u1", Phone: "13800138000"}},
		total: 1,
	}
	svc := &CustomerQueryService{
		custRepo: repo,
		scopeSvc: NewBusinessScopeServiceWithReaders(
			fakeBusinessScopeAuthReader{auths: []model.BusinessAccountAuthorization{{AccountID: "wx-1"}}},
			fakeBusinessScopeUserReader{user: &model.SystemUser{ID: 7, Role: "staff", DataScope: model.DataScopeSelf}},
		),
	}
	rows, total := svc.ListCustomers(context.Background(), 1, 20, "", 7)
	if total != 1 || len(rows) != 1 {
		t.Fatalf("expected 1 row, got total=%d rows=%d", total, len(rows))
	}
	if !repo.listByScopeCalled || repo.allAccounts {
		t.Fatalf("expected scoped restricted query, called=%v all=%v", repo.listByScopeCalled, repo.allAccounts)
	}
	if len(repo.allowedIDs) != 1 || repo.allowedIDs[0] != "wx-1" {
		t.Fatalf("unexpected allowed IDs: %v", repo.allowedIDs)
	}
}

func TestCustomerQueryService_ListCustomers_AdminNoScopeFilter(t *testing.T) {
	repo := &fakeCustomerRepoScoped{}
	svc := &CustomerQueryService{
		custRepo: repo,
		scopeSvc: NewBusinessScopeServiceWithReaders(
			fakeBusinessScopeAuthReader{},
			fakeBusinessScopeUserReader{user: &model.SystemUser{ID: 1, Role: model.SystemUserRoleAdmin, DataScope: model.DataScopeAll}},
		),
	}
	_, _ = svc.ListCustomers(context.Background(), 1, 20, "", 1)
	if repo.listByScopeCalled {
		t.Fatalf("admin should not use scoped repo list")
	}
}

func TestCustomerQueryService_GetCustomerByID_ScopedDenied(t *testing.T) {
	repo := &fakeCustomerRepoScoped{}
	svc := &CustomerQueryService{
		custRepo: &fakeCustomerRepoScopedGetByID{
			fakeCustomerRepoScoped: repo,
			customer:               &model.Customer{ID: "c1", SourceChannel: "wechat", SourceAccountID: "wx-other"},
		},
		scopeSvc: NewBusinessScopeServiceWithReaders(
			fakeBusinessScopeAuthReader{auths: []model.BusinessAccountAuthorization{{Channel: "wechat", AccountID: "wx-1"}}},
			fakeBusinessScopeUserReader{user: &model.SystemUser{ID: 7, Role: "staff", DataScope: model.DataScopeSelf}},
		),
	}
	got, err := svc.GetCustomerByID(context.Background(), "c1", 7)
	if err != nil {
		t.Fatalf("GetCustomerByID: %v", err)
	}
	if got != nil {
		t.Fatalf("expected nil for unauthorized customer, got %+v", got)
	}
}

type fakeCustomerRepoScopedGetByID struct {
	*fakeCustomerRepoScoped
	customer *model.Customer
}

func (f *fakeCustomerRepoScopedGetByID) GetByID(context.Context, string) (*model.Customer, error) {
	return f.customer, nil
}
