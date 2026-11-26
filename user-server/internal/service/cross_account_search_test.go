package service

import (
	"context"
	"errors"
	"testing"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/repository"
)

type fakeCrossAccountSearchRepo struct {
	rows        []repository.CrossAccountSearchRow
	total       int64
	err         error
	gotTID      uint
	gotKeyword  string
	gotIndustry string
	gotIDs      []string
	gotAll      bool
	gotPage     int
	gotPageSize int
}

func (f *fakeCrossAccountSearchRepo) SearchCustomers(_ context.Context, tid uint, keyword, industry string, allowedAccountIDs []string, allAccounts bool, page, pageSize int) ([]repository.CrossAccountSearchRow, int64, error) {
	f.gotTID = tid
	f.gotKeyword = keyword
	f.gotIndustry = industry
	f.gotIDs = append([]string(nil), allowedAccountIDs...)
	f.gotAll = allAccounts
	f.gotPage = page
	f.gotPageSize = pageSize
	if f.err != nil {
		return nil, 0, f.err
	}
	out := make([]repository.CrossAccountSearchRow, len(f.rows))
	copy(out, f.rows)
	return out, f.total, nil
}

func TestCrossAccountSearchService_SearchCustomers_AdminAllAccounts(t *testing.T) {
	repo := &fakeCrossAccountSearchRepo{
		rows:  []repository.CrossAccountSearchRow{{ID: "c1", Name: "Alice", Phone: "13800138000", Email: "alice@example.com"}},
		total: 1,
	}
	scopeSvc := NewBusinessScopeServiceWithReaders(
		fakeBusinessScopeAuthReader{},
		fakeBusinessScopeUserReader{user: &model.SystemUser{ID: 1, Role: model.SystemUserRoleAdmin, DataScope: model.DataScopeAll}},
	)
	svc := NewCrossAccountSearchServiceWithRepo(repo, scopeSvc)

	rows, total, scope, err := svc.SearchCustomers(context.Background(), 10, 1, "alice", "", 1, 20)
	if err != nil {
		t.Fatalf("SearchCustomers: %v", err)
	}
	if total != 1 || len(rows) != 1 {
		t.Fatalf("unexpected result total=%d rows=%d", total, len(rows))
	}
	if !scope.AllAccounts || scope.Restricted || scope.AuthorizedAccountCount != 0 {
		t.Fatalf("unexpected scope: %+v", scope)
	}
	if !repo.gotAll || len(repo.gotIDs) != 0 {
		t.Fatalf("admin should query all accounts, gotAll=%v ids=%v", repo.gotAll, repo.gotIDs)
	}
	if rows[0].Phone != "138****8000" {
		t.Fatalf("phone should be masked, got %q", rows[0].Phone)
	}
	if rows[0].Email != "a***@example.com" {
		t.Fatalf("email should be masked, got %q", rows[0].Email)
	}
}

func TestCrossAccountSearchService_SearchCustomers_RestrictedNoAuthReturnsEmpty(t *testing.T) {
	repo := &fakeCrossAccountSearchRepo{
		rows:  []repository.CrossAccountSearchRow{{ID: "leak", Name: "should-not-return"}},
		total: 99,
	}
	scopeSvc := NewBusinessScopeServiceWithReaders(
		fakeBusinessScopeAuthReader{},
		fakeBusinessScopeUserReader{user: &model.SystemUser{ID: 8, Role: "staff", DataScope: model.DataScopeSelf}},
	)
	svc := NewCrossAccountSearchServiceWithRepo(repo, scopeSvc)

	rows, total, scope, err := svc.SearchCustomers(context.Background(), 10, 8, "secret", "", 1, 20)
	if err != nil {
		t.Fatalf("SearchCustomers: %v", err)
	}
	if total != 0 || len(rows) != 0 {
		t.Fatalf("no-auth user must get empty result, got total=%d rows=%d", total, len(rows))
	}
	if scope.AllAccounts || !scope.Restricted || scope.AuthorizedAccountCount != 0 {
		t.Fatalf("unexpected scope: %+v", scope)
	}
	if repo.gotTID != 0 || repo.gotKeyword != "" {
		t.Fatalf("repo should not be called when restricted user has no authorized accounts, got tid=%d keyword=%q", repo.gotTID, repo.gotKeyword)
	}
}

func TestCrossAccountSearchService_SearchCustomers_RestrictedAuthorizedFilters(t *testing.T) {
	repo := &fakeCrossAccountSearchRepo{
		rows:  []repository.CrossAccountSearchRow{{ID: "c2", Name: "Bob"}},
		total: 1,
	}
	scopeSvc := NewBusinessScopeServiceWithReaders(
		fakeBusinessScopeAuthReader{auths: []model.BusinessAccountAuthorization{{Channel: "wechat", AccountID: "wx-1"}, {Channel: "douyin", AccountID: "dy-1"}}},
		fakeBusinessScopeUserReader{user: &model.SystemUser{ID: 7, Role: "staff", DataScope: model.DataScopeSelf}},
	)
	svc := NewCrossAccountSearchServiceWithRepo(repo, scopeSvc)

	rows, total, scope, err := svc.SearchCustomers(context.Background(), 10, 7, "bob", "cross-border", 2, 50)
	if err != nil {
		t.Fatalf("SearchCustomers: %v", err)
	}
	if total != 1 || len(rows) != 1 || rows[0].ID != "c2" {
		t.Fatalf("unexpected result total=%d rows=%+v", total, rows)
	}
	if scope.AllAccounts || !scope.Restricted || scope.AuthorizedAccountCount != 2 {
		t.Fatalf("unexpected scope: %+v", scope)
	}
	if repo.gotAll {
		t.Fatalf("restricted user should not query all accounts")
	}
	if len(repo.gotIDs) != 2 {
		t.Fatalf("expected 2 authorized IDs, got %v", repo.gotIDs)
	}
	if repo.gotKeyword != "bob" || repo.gotIndustry != "cross-border" || repo.gotPage != 2 || repo.gotPageSize != 50 || repo.gotTID != 10 {
		t.Fatalf("unexpected repo args: tid=%d keyword=%q industry=%q page=%d size=%d", repo.gotTID, repo.gotKeyword, repo.gotIndustry, repo.gotPage, repo.gotPageSize)
	}
}

func TestCrossAccountSearchService_SearchCustomers_ResolveError(t *testing.T) {
	wantErr := errors.New("scope down")
	repo := &fakeCrossAccountSearchRepo{}
	scopeSvc := NewBusinessScopeServiceWithReaders(
		fakeBusinessScopeAuthReader{},
		fakeBusinessScopeUserReader{err: wantErr},
	)
	svc := NewCrossAccountSearchServiceWithRepo(repo, scopeSvc)

	_, _, _, err := svc.SearchCustomers(context.Background(), 10, 9, "", "", 1, 20)
	if !errors.Is(err, wantErr) {
		t.Fatalf("expected resolve error, got %v", err)
	}
}

func TestCrossAccountSearchService_SearchCustomers_RepoError(t *testing.T) {
	wantErr := errors.New("db down")
	repo := &fakeCrossAccountSearchRepo{err: wantErr}
	scopeSvc := NewBusinessScopeServiceWithReaders(
		fakeBusinessScopeAuthReader{},
		fakeBusinessScopeUserReader{user: &model.SystemUser{ID: 1, Role: model.SystemUserRoleAdmin, DataScope: model.DataScopeAll}},
	)
	svc := NewCrossAccountSearchServiceWithRepo(repo, scopeSvc)

	_, _, _, err := svc.SearchCustomers(context.Background(), 10, 1, "", "", 1, 20)
	if !errors.Is(err, wantErr) {
		t.Fatalf("expected repo error, got %v", err)
	}
}

func TestMaskPhoneAndEmail(t *testing.T) {
	if got := maskPhone("13800138000"); got != "138****8000" {
		t.Fatalf("unexpected phone mask: %q", got)
	}
	if got := maskPhone("1234"); got != "****" {
		t.Fatalf("unexpected short phone mask: %q", got)
	}
	if got := maskEmail("alice@example.com"); got != "a***@example.com" {
		t.Fatalf("unexpected email mask: %q", got)
	}
	if got := maskEmail(""); got != "" {
		t.Fatalf("empty email should stay empty, got %q", got)
	}
}
