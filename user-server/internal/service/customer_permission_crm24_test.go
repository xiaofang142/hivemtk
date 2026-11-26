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

func TestCustomerPermission_MalformedIndustryRestrictionFailsClosed(t *testing.T) {
	industries, valid := parseVisibleIndustries(`{"industries":"not-an-array"}`)
	if valid || industries != nil {
		t.Fatalf("malformed industry restriction must be invalid and deny-by-caller, got industries=%v valid=%v", industries, valid)
	}

	industries, valid = parseVisibleIndustries("")
	if !valid || industries != nil {
		t.Fatalf("explicitly unrestricted empty restriction must remain valid nil, got industries=%v valid=%v", industries, valid)
	}
}

func TestCustomerPermission_SourceReferenceUsesMappedStableAccountIDs(t *testing.T) {
	scope := CustomerDataScope{
		UserID:             7,
		RestrictedAccounts: true,
		AccountIDs:         []string{"62001"},
		AccountRules: []CustomerAccountRule{{
			AccountID:        "62001",
			SourceAccountIDs: []string{"62001", "current-code"},
			Industries:       []string{"A"},
		}},
	}
	svc := &CustomerPermissionService{}

	if !svc.CanAccess(scope, &model.Customer{SourceAccountID: "current-code", Industry: "A"}) {
		t.Fatal("uniquely mapped transient source reference should be allowed")
	}
	if svc.CanAccess(scope, &model.Customer{SourceAccountID: "expired-old-code", Industry: "A"}) {
		t.Fatal("expired or unknown source reference must be denied")
	}
}

func TestCRMExportAndDedupeScopeFilterPreservesAccountRules(t *testing.T) {
	scope := CustomerDataScope{
		UserID:             7,
		RestrictedAccounts: true,
		AccountIDs:         []string{"62001", "62002"},
		AccountRules: []CustomerAccountRule{
			{AccountID: "62001", SourceAccountIDs: []string{"62001"}, Industries: []string{"A"}},
			{AccountID: "62002", SourceAccountIDs: []string{"62002"}, Industries: []string{"B"}},
		},
	}
	filter := (&CustomerPermissionService{}).ToFilter(scope, repository.CustomerScopeFilter{})
	if len(filter.AccountRules) != 2 {
		t.Fatalf("导出/去重共用过滤器必须保留全部账号配对，实际=%+v", filter.AccountRules)
	}
}

type crmPermissionAccountStub struct {
	repository.ChannelAccountRepository
	accounts []model.ChannelAccount
	refs     map[uint][]string
	err      error
}

func (s *crmPermissionAccountStub) Available() bool { return true }
func (s *crmPermissionAccountStub) GetByID(ctx context.Context, tid, id uint) (*model.ChannelAccount, error) {
	rows, err := s.GetByIDs(ctx, tid, []uint{id})
	if err != nil || len(rows) == 0 {
		return nil, err
	}
	return &rows[0], nil
}
func (s *crmPermissionAccountStub) GetByIDs(_ context.Context, tid uint, ids []uint) ([]model.ChannelAccount, error) {
	if s.err != nil {
		return nil, s.err
	}
	var out []model.ChannelAccount
	for _, a := range s.accounts {
		for _, id := range ids {
			if a.ID == id && a.TenantID == tid {
				out = append(out, a)
				break
			}
		}
	}
	return out, nil
}
func (s *crmPermissionAccountStub) GetByIdentifyCode(_ context.Context, code string) (*model.ChannelAccount, error) {
	if s.err != nil {
		return nil, s.err
	}
	for _, a := range s.accounts {
		if containsCRMString(s.refs[a.ID], code) {
			return &a, nil
		}
	}
	return nil, nil
}
func (s *crmPermissionAccountStub) GetCRMSourceReferences(_ context.Context, tid uint, ids []uint) (map[uint][]string, error) {
	accounts, err := s.GetByIDs(context.Background(), tid, ids)
	if err != nil {
		return nil, err
	}
	out := map[uint][]string{}
	for _, a := range accounts {
		out[a.ID] = s.refs[a.ID]
	}
	return out, nil
}

func crmPermissionAccountsForTest() *crmPermissionAccountStub {
	return &crmPermissionAccountStub{
		accounts: []model.ChannelAccount{{ID: 11, TenantID: 7}, {ID: 12, TenantID: 7}},
		refs:     map[uint][]string{11: {"11", "code-a"}, 12: {"12"}},
	}
}

// 真实 Resolve 不能把 owner 条件/行业扩散到其他授权；稳定账号键中不得混入识别码。
func TestCustomerPermission_ResolvePreservesGrantUnionAndRejectsOrphans(t *testing.T) {
	ctx := tenant.WithTenantID(context.Background(), 7)
	store := &authConfigStoreStub{rows: []model.ChannelAccountAuthorization{
		{UserID: 3, ChannelAccountID: 11, Role: "owner", VisibleIndustryScope: `["A"]`},
		{UserID: 3, ChannelAccountID: 12, Role: "viewer", VisibleIndustryScope: `["B"]`},
		{UserID: 3, ChannelAccountID: 12, Role: "viewer", VisibleIndustryScope: `["C"]`},
		{UserID: 3, ChannelAccountID: 99, Role: "viewer", VisibleIndustryScope: `[]`},
	}}
	svc := &CustomerPermissionService{authRepo: store, accountRepo: crmPermissionAccountsForTest(), scopeSvc: NewBusinessScopeServiceWithReaders(nil, nil)}
	scope, err := svc.Resolve(ctx, 3)
	if err != nil || scope.OnlyOwned || len(scope.Industries) != 0 || !reflect.DeepEqual(scope.AccountIDs, []string{"11", "12"}) || len(scope.AccountRules) != 3 {
		t.Fatalf("授权应保留三条配对且只含有效数字主键，实际=%+v err=%v", scope, err)
	}
	other := uint(4)
	for _, tc := range []struct {
		source, industry string
		want             bool
	}{
		{"code-a", "A", false}, {"12", "B", true}, {"12", "C", true}, {"12", "A", false}, {"99", "Z", false},
	} {
		if got := svc.CanAccess(scope, &model.Customer{SourceAccountID: tc.source, Industry: tc.industry, OwnerUserID: &other}); got != tc.want {
			t.Errorf("%s/%s 期望=%t 实际=%t", tc.source, tc.industry, tc.want, got)
		}
	}
}

// 旧业务授权与 Phase2 授权仍取并集，不得被另一条授权错误收窄。
func TestCustomerPermission_ResolveBusinessUnionAndInvalidGrant(t *testing.T) {
	ctx := tenant.WithTenantID(context.Background(), 7)
	svc := &CustomerPermissionService{
		authRepo: &authConfigStoreStub{rows: []model.ChannelAccountAuthorization{
			{UserID: 3, ChannelAccountID: 12, Role: "viewer", VisibleIndustryScope: `{"unexpected":true}`},
		}}, accountRepo: crmPermissionAccountsForTest(),
		scopeSvc: NewBusinessScopeServiceWithReaders(fakeBusinessScopeAuthReader{auths: []model.BusinessAccountAuthorization{{AccountID: "code-a"}, {AccountID: "unknown"}, {AccountID: "00012"}}}, nil),
	}
	scope, err := svc.Resolve(ctx, 3)
	if err != nil || !reflect.DeepEqual(scope.AccountIDs, []string{"11"}) || !svc.CanAccess(scope, &model.Customer{SourceAccountID: "11", Industry: "any"}) || svc.CanAccess(scope, &model.Customer{SourceAccountID: "12"}) {
		t.Fatalf("业务授权仅允许唯一解析账号，异常行业不得变全量，实际=%+v err=%v", scope, err)
	}
	svc.accountRepo = &crmPermissionAccountStub{err: errors.New("account store unavailable")}
	scope, err = svc.Resolve(ctx, 3)
	if err == nil || !scope.RestrictedAccounts || len(scope.AccountIDs) != 0 || len(scope.AccountRules) != 0 {
		t.Fatalf("账号查询失败必须返回错误和拒绝态，实际=%+v err=%v", scope, err)
	}
}

// 无效限制不能被解释成不限行业；显式空数组仍保留既有不限语义。
func TestCustomerPermission_IndustryRestrictionShape(t *testing.T) {
	for _, raw := range []string{`null`, `{}`, `{"industries":null}`, `[null]`, `[""]`, `[" "]`, `["A",null]`, `{"industries":[null]}`, `42`, `"A"`, `[1]`} {
		if _, valid := parseVisibleIndustries(raw); valid {
			t.Errorf("无效行业限制必须拒绝，实际接受=%s", raw)
		}
	}
	for _, raw := range []string{``, `[]`, `{"industries":[]}`, `["A"]`, `{"industries":["A"]}`} {
		if _, valid := parseVisibleIndustries(raw); !valid {
			t.Errorf("合法行业限制应接受，实际拒绝=%s", raw)
		}
	}
}

// 请求条件不能复活空授权，owner 的零身份在 SQL 与按行校验中必须同样拒绝。
func TestCustomerPermission_EmptyGrantAndZeroOwnerFailClosed(t *testing.T) {
	svc := &CustomerPermissionService{}
	scope := CustomerDataScope{UserID: 3, RestrictedAccounts: true}
	filter := svc.ToFilter(scope, repository.CustomerScopeFilter{SourceAccountIDs: []string{"11"}})
	if !filter.DenyAll && len(filter.SourceAccountIDs) > 0 {
		t.Fatalf("请求账号不能将空授权复活，实际=%+v", filter)
	}
	zero := uint(0)
	for _, scope := range []CustomerDataScope{
		{OnlyOwned: true},
		{AccountRules: []CustomerAccountRule{{AccountID: "11", Role: "owner"}}},
	} {
		if svc.CanAccess(scope, &model.Customer{SourceAccountID: "11", OwnerUserID: &zero}) {
			t.Errorf("owner 零身份不得放行 scope=%+v", scope)
		}
	}
}

// 同一账号的旧业务授权与新授权取并集，撤销后重新解析立即收窄。
func TestCustomerPermission_BusinessAndPhase2SameAccountUnion(t *testing.T) {
	store := &authConfigStoreStub{rows: []model.ChannelAccountAuthorization{{UserID: 3, ChannelAccountID: 11, Role: "owner", VisibleIndustryScope: `["A"]`}}}
	svc := &CustomerPermissionService{authRepo: store, accountRepo: crmPermissionAccountsForTest(), scopeSvc: NewBusinessScopeServiceWithReaders(fakeBusinessScopeAuthReader{auths: []model.BusinessAccountAuthorization{{AccountID: "11"}}}, nil)}
	ctx := tenant.WithTenantID(context.Background(), 7)
	scope, err := svc.Resolve(ctx, 3)
	if err != nil || !svc.AllowsWrite(scope, "code-a", "B", 4) {
		t.Fatalf("旧业务全账号授权必须保留并集，实际=%+v err=%v", scope, err)
	}
	svc.scopeSvc = NewBusinessScopeServiceWithReaders(nil, nil)
	scope, err = svc.Resolve(ctx, 3)
	if err != nil || svc.AllowsWrite(scope, "code-a", "B", 4) || !svc.AllowsWrite(scope, "code-a", "A", 3) {
		t.Fatalf("撤销旧授权后应只剩 owner/A，实际=%+v err=%v", scope, err)
	}
}

// 多租户开启时，即使用户读仓返回管理员也不能绕过缺租户拒绝。
func TestCustomerPermission_MissingTenantFailsClosed(t *testing.T) {
	mode := tenant.CurrentMode()
	tenant.SetMode(tenant.ModeOn)
	t.Cleanup(func() { tenant.SetMode(mode) })
	svc := &CustomerPermissionService{scopeSvc: NewBusinessScopeServiceWithReaders(nil, fakeBusinessScopeUserReader{user: &model.SystemUser{Role: model.SystemUserRoleAdmin}})}
	scope, err := svc.Resolve(context.Background(), 3)
	if err == nil || scope.AllAccounts || !scope.RestrictedAccounts {
		t.Fatalf("缺租户必须拒绝，实际=%+v err=%v", scope, err)
	}
}
