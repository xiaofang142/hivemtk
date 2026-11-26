package service

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/repository"
)

// 本文件用授权仓储替身验证 LEAD-01 的配置闭环：租户归属、空授权拒绝、
// 账号×行业/角色配对保留、保存回读与撤销立即影响权限解析。

type authConfigStoreStub struct {
	rows        []model.ChannelAccountAuthorization
	replaceCall int
	revokeCall  int
	lastTenant  uint
	lastUser    uint
	lastAccount uint
	lastRows    []model.ChannelAccountAuthorization
	listErr     error
}

func (s *authConfigStoreStub) ListByUser(_ context.Context, userID uint) ([]model.ChannelAccountAuthorization, error) {
	if s.listErr != nil {
		return nil, s.listErr
	}
	out := make([]model.ChannelAccountAuthorization, 0, len(s.rows))
	for _, r := range s.rows {
		if r.UserID == userID {
			out = append(out, r)
		}
	}
	return out, nil
}

func (s *authConfigStoreStub) Replace(_ context.Context, tenantID, userID uint, rows []model.ChannelAccountAuthorization) error {
	s.replaceCall++
	s.lastTenant, s.lastUser = tenantID, userID
	s.lastRows = append([]model.ChannelAccountAuthorization(nil), rows...)
	kept := make([]model.ChannelAccountAuthorization, 0, len(s.rows))
	for _, r := range s.rows {
		if r.TenantID != tenantID || r.UserID != userID {
			kept = append(kept, r)
		}
	}
	s.rows = append(kept, rows...)
	return nil
}

func (s *authConfigStoreStub) Revoke(_ context.Context, tenantID, userID, accountID uint) error {
	s.revokeCall++
	s.lastTenant, s.lastUser, s.lastAccount = tenantID, userID, accountID
	kept := make([]model.ChannelAccountAuthorization, 0, len(s.rows))
	for _, r := range s.rows {
		if r.TenantID == tenantID && r.UserID == userID && r.ChannelAccountID == accountID {
			continue
		}
		kept = append(kept, r)
	}
	s.rows = kept
	return nil
}

type authConfigUserStub struct {
	users map[uint]*model.SystemUser
}

func (s *authConfigUserStub) GetByID(_ context.Context, id uint) (*model.SystemUser, error) {
	u := s.users[id]
	if u == nil {
		return nil, errors.New("not found")
	}
	return u, nil
}

type authConfigAccountStub struct {
	accounts map[uint]*model.ChannelAccount
	all      []model.ChannelAccount
}

func (s *authConfigAccountStub) Available() bool { return true }

func (s *authConfigAccountStub) GetByID(_ context.Context, tenantID, id uint) (*model.ChannelAccount, error) {
	acc := s.accounts[id]
	if acc == nil || acc.TenantID != tenantID {
		return nil, errors.New("not found")
	}
	return acc, nil
}

func (s *authConfigAccountStub) List(_ context.Context, opts repository.ChannelAccountListOptions) ([]model.ChannelAccount, error) {
	out := make([]model.ChannelAccount, 0, len(s.all))
	for _, a := range s.all {
		if a.TenantID == opts.TenantID {
			out = append(out, a)
		}
	}
	return out, nil
}

func TestAccountAuthorizationService_SaveAndReadbackPreservesPairs(t *testing.T) {
	store := &authConfigStoreStub{}
	svc := NewAccountAuthorizationServiceWithDeps(
		&authConfigAccountStub{accounts: map[uint]*model.ChannelAccount{
			11: {ID: 11, TenantID: 7, Name: "A"},
			12: {ID: 12, TenantID: 7, Name: "B"},
		}},
		&authConfigUserStub{users: map[uint]*model.SystemUser{3: {ID: 3, TenantID: 7, Enabled: true}}},
		store,
	)
	ctx := tenant.WithTenantID(context.Background(), 7)
	pairs := []AccountAuthorizationPair{
		{AccountID: 11, Industries: []string{"制造"}, Role: "owner"},
		{AccountID: 12, Industries: []string{"食品"}, Role: "viewer"},
	}
	saved, err := svc.Save(ctx, 7, 3, pairs)
	if err != nil {
		t.Fatalf("保存授权失败: %v", err)
	}
	if len(saved) != 2 {
		t.Fatalf("期望保留 2 条账号×行业/角色配对，实际 %d", len(saved))
	}
	if saved[0].ChannelAccountID != 11 || saved[0].Role != "owner" || len(saved[0].VisibleIndustryScope) == 0 ||
		saved[1].ChannelAccountID != 12 || saved[1].Role != "viewer" {
		t.Fatalf("配对被合并或丢失: %+v", saved)
	}
	readback, err := svc.List(ctx, 7, 3)
	if err != nil || len(readback) != 2 {
		t.Fatalf("回读应为 2 条，实际 len=%d err=%v", len(readback), err)
	}
	// 配对不能被合并成全局行业集合。
	if !reflect.DeepEqual(readback[0].VisibleIndustryScope, saved[0].VisibleIndustryScope) ||
		!reflect.DeepEqual(readback[1].VisibleIndustryScope, saved[1].VisibleIndustryScope) {
		t.Fatalf("回读配对与保存不一致: saved=%+v readback=%+v", saved, readback)
	}
}

func TestAccountAuthorizationService_RejectsEmptyAndCrossTenant(t *testing.T) {
	svc := NewAccountAuthorizationServiceWithDeps(
		&authConfigAccountStub{accounts: map[uint]*model.ChannelAccount{
			11: {ID: 11, TenantID: 7}, 12: {ID: 12, TenantID: 8},
		}},
		&authConfigUserStub{users: map[uint]*model.SystemUser{3: {ID: 3, TenantID: 7}, 4: {ID: 4, TenantID: 8}}},
		&authConfigStoreStub{},
	)
	ctx := tenant.WithTenantID(context.Background(), 7)
	if _, err := svc.Save(ctx, 7, 3, nil); !errors.Is(err, ErrAccountAuthorizationInvalid) {
		t.Fatalf("空授权必须拒绝，实际 err=%v", err)
	}
	if _, err := svc.Save(ctx, 7, 4, []AccountAuthorizationPair{{AccountID: 11, Role: "viewer"}}); !errors.Is(err, ErrAccountAuthorizationTenantMismatch) {
		t.Fatalf("跨租户用户必须拒绝，实际 err=%v", err)
	}
	if _, err := svc.Save(ctx, 7, 3, []AccountAuthorizationPair{{AccountID: 12, Role: "viewer"}}); !errors.Is(err, ErrAccountAuthorizationTenantMismatch) {
		t.Fatalf("跨租户账号必须拒绝，实际 err=%v", err)
	}
}

func TestAccountAuthorizationService_RevokeAffectsPermissionImmediately(t *testing.T) {
	store := &authConfigStoreStub{rows: []model.ChannelAccountAuthorization{
		{TenantID: 7, UserID: 3, ChannelAccountID: 11, Role: "viewer", VisibleIndustryScope: `["制造"]`},
	}}
	svc := NewAccountAuthorizationServiceWithDeps(
		&authConfigAccountStub{accounts: map[uint]*model.ChannelAccount{11: {ID: 11, TenantID: 7, IdentifyCode: "code-a"}}},
		&authConfigUserStub{users: map[uint]*model.SystemUser{3: {ID: 3, TenantID: 7}}},
		store,
	)
	ctx := tenant.WithTenantID(context.Background(), 7)
	perm := &CustomerPermissionService{authRepo: store, accountRepo: crmPermissionAccountsForTest(), scopeSvc: NewBusinessScopeServiceWithReaders(nil, nil)}
	scopeBefore, err := perm.Resolve(ctx, 3)
	if err != nil || !scopeBefore.RestrictedAccounts || len(scopeBefore.AccountIDs) == 0 {
		t.Fatalf("撤销前受限员工应看到账号范围: scope=%+v err=%v", scopeBefore, err)
	}
	if err := svc.Revoke(ctx, 7, 3, 11); err != nil {
		t.Fatalf("撤销失败: %v", err)
	}
	scopeAfter, err := perm.Resolve(ctx, 3)
	if err != nil || !scopeAfter.RestrictedAccounts || len(scopeAfter.AccountIDs) != 0 {
		t.Fatalf("撤销后立即无账号可见，实际 scope=%+v err=%v", scopeAfter, err)
	}
}

func TestCustomerPermissionService_TwoEmployeesTwoAccountsAreIsolated(t *testing.T) {
	store := &authConfigStoreStub{rows: []model.ChannelAccountAuthorization{
		{TenantID: 7, UserID: 3, ChannelAccountID: 11, Role: "viewer", VisibleIndustryScope: `["制造"]`},
		{TenantID: 7, UserID: 4, ChannelAccountID: 12, Role: "viewer", VisibleIndustryScope: `["食品"]`},
	}}
	perm := &CustomerPermissionService{authRepo: store, accountRepo: crmPermissionAccountsForTest(), scopeSvc: NewBusinessScopeServiceWithReaders(nil, nil)}
	ctx := tenant.WithTenantID(context.Background(), 7)
	s3, err := perm.Resolve(ctx, 3)
	if err != nil {
		t.Fatalf("解析员工3失败: %v", err)
	}
	s4, err := perm.Resolve(ctx, 4)
	if err != nil {
		t.Fatalf("解析员工4失败: %v", err)
	}
	c11 := &model.Customer{SourceAccountID: "11", Industry: "制造"}
	c12 := &model.Customer{SourceAccountID: "12", Industry: "食品"}
	if !perm.CanAccess(s3, c11) || perm.CanAccess(s3, c12) {
		t.Fatalf("员工3只能访问账号11，实际 c11=%t c12=%t", perm.CanAccess(s3, c11), perm.CanAccess(s3, c12))
	}
	if perm.CanAccess(s4, c11) || !perm.CanAccess(s4, c12) {
		t.Fatalf("员工4只能访问账号12，实际 c11=%t c12=%t", perm.CanAccess(s4, c11), perm.CanAccess(s4, c12))
	}
	// 不同账号的行业不能组成全局集合后交叉放行。
	if perm.CanAccess(s3, &model.Customer{SourceAccountID: "12", Industry: "制造"}) {
		t.Fatal("禁止把他人账号+本人行业拼成授权配对")
	}
}
