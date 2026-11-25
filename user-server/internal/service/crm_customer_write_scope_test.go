package service

import (
	"context"
	"errors"
	"testing"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/pkg/testutil"
	"hivemtk-user/internal/repository"
)

const (
	crmWriteTenantA = uint(6201)
	crmWriteTenantB = uint(6202)
)

type crmWriteFixture struct {
	ctx        context.Context
	database   interface{}
	customers  *CRMCustomerService
	repo       repository.CustomerRepository
	permission *CustomerPermissionService
}

func setupCRMWriteFixture(t *testing.T) crmWriteFixture {
	t.Helper()
	previousMode := tenant.CurrentMode()
	tenant.SetMode(tenant.ModeOn)
	t.Cleanup(func() { tenant.SetMode(previousMode) })

	db := testutil.NewTestDB(t, &model.Customer{}, &model.ChannelAccount{}, &model.Channel{},
		&model.Contact{}, &model.Supplier{}, &model.ServiceProvider{})
	if db == nil {
		t.Fatal("真实 PostgreSQL 测试库不可达：写入权限行为不能跳过")
	}
	sysCtx := tenant.WithSystemScope(context.Background())
	channel := &model.Channel{TenantID: crmWriteTenantA, ID: 1, Name: "telegram", Code: "telegram", Type: "dm", Enabled: true}
	if err := db.WithContext(sysCtx).Create(channel).Error; err != nil {
		t.Fatalf("准备渠道夹具失败: %v", err)
	}
	accounts := []*model.ChannelAccount{
		{TenantID: crmWriteTenantA, ID: 62001, ChannelID: 1, Name: "allowed", Type: "telegram", Status: 1, IdentifyCode: "crm-write-62001", DataScope: "{}", OwnerUserIDs: model.JSONStrings{}},
		{TenantID: crmWriteTenantA, ID: 62002, ChannelID: 1, Name: "forbidden", Type: "telegram", Status: 1, IdentifyCode: "crm-write-62002", DataScope: "{}", OwnerUserIDs: model.JSONStrings{}},
		{TenantID: crmWriteTenantB, ID: 62003, ChannelID: 1, Name: "other-tenant", Type: "telegram", Status: 1, IdentifyCode: "crm-write-62003", DataScope: "{}", OwnerUserIDs: model.JSONStrings{}},
	}
	if err := db.WithContext(sysCtx).Create(&accounts).Error; err != nil {
		t.Fatalf("准备渠道账号夹具失败: %v", err)
	}
	rows := []*model.Customer{
		{TenantID: crmWriteTenantA, ID: "crm-write-a1", UnifiedID: "crm-write-a1", Name: "A1", SourceAccountID: "acct-1", Industry: "A", OwnerUserID: ptrUintCRM(7)},
		{TenantID: crmWriteTenantA, ID: "crm-write-a2", UnifiedID: "crm-write-a2", Name: "A2", SourceAccountID: "acct-1", Industry: "B", OwnerUserID: ptrUintCRM(7)},
	}
	if err := db.WithContext(sysCtx).Create(&rows).Error; err != nil {
		t.Fatalf("准备客户夹具失败: %v", err)
	}

	repo := repository.NewCRMCustomerRepository()
	writeRepo := repository.NewCustomerRepository()
	permission := &CustomerPermissionService{customers: repo}
	return crmWriteFixture{
		ctx:        tenant.WithTenantID(context.Background(), crmWriteTenantA),
		customers:  &CRMCustomerService{permission: permission, customers: writeRepo, contacts: repository.NewContactRepository(), suppliers: repository.NewSupplierRepository(), providers: repository.NewServiceProviderRepository(), accounts: repository.NewChannelAccountRepository()},
		repo:       writeRepo,
		permission: permission,
	}
}

func ptrUintCRM(v uint) *uint { return &v }

func crmWriteRestrictedEmpty() CustomerDataScope {
	return CustomerDataScope{UserID: 7, RestrictedAccounts: true}
}

func crmWriteAllowedScope() CustomerDataScope {
	return CustomerDataScope{UserID: 7, RestrictedAccounts: true, AccountIDs: []string{"acct-1"}}
}

func crmWriteOwnerScope() CustomerDataScope {
	return CustomerDataScope{UserID: 7, RestrictedAccounts: true, AccountIDs: []string{"acct-1"}, OnlyOwned: true}
}

// 空授权不能创建客户：RestrictedAccounts=true 且 AccountIDs 为空必须拒绝全部写入。
func TestCRMCustomerWrite_EmptyAccountScopeCannotCreate(t *testing.T) {
	f := setupCRMWriteFixture(t)
	_, err := f.customers.Create(f.ctx, crmWriteRestrictedEmpty(), CRMCustomerSave{
		Name: "x", SourceAccountID: "acct-1", Industry: "A", OwnerUserID: 7,
	})
	if !errors.Is(err, ErrCRMCustomerForbidden) {
		t.Fatalf("空授权创建应拒绝，期望 ErrCRMCustomerForbidden，实际 err=%v", err)
	}
}

// OnlyOwned 不能通过 ownerID=0 绕过归属限定。
func TestCRMCustomerWrite_OwnerScopeRejectsZeroOwner(t *testing.T) {
	f := setupCRMWriteFixture(t)
	_, err := f.customers.Create(f.ctx, crmWriteOwnerScope(), CRMCustomerSave{
		Name: "x", SourceAccountID: "acct-1", Industry: "A", OwnerUserID: 0,
	})
	if !errors.Is(err, ErrCRMCustomerForbidden) {
		t.Fatalf("OnlyOwned 下 owner=0 应拒绝，期望 ErrCRMCustomerForbidden，实际 err=%v", err)
	}
}

// Update 按最终写入值校验：传空账号/行业不能绕过权限校验并落库更宽松的值。
func TestCRMCustomerWrite_UpdateUsesFinalValues(t *testing.T) {
	f := setupCRMWriteFixture(t)
	scope := crmWriteAllowedScope()
	// 受限账号集合只有 acct-1，传 acct-2 必须拒绝。
	_, err := f.customers.Update(f.ctx, scope, "crm-write-a1", CRMCustomerSave{
		Name: "changed", SourceAccountID: "acct-2", Industry: "A", OwnerUserID: 7,
	})
	if !errors.Is(err, ErrCRMCustomerForbidden) {
		t.Fatalf("更新为未授权账号应拒绝，实际 err=%v", err)
	}
}

// 合法客户挂未授权渠道账号必须拒绝。
func TestCRMEntityWrite_UnauthorizedAccountRejected(t *testing.T) {
	f := setupCRMWriteFixture(t)
	for name, create := range map[string]func(context.Context, CustomerDataScope, CRMEntitySave) (any, error){
		"contact": func(ctx context.Context, s CustomerDataScope, in CRMEntitySave) (any, error) {
			return f.customers.CreateContact(ctx, s, in)
		},
		"supplier": func(ctx context.Context, s CustomerDataScope, in CRMEntitySave) (any, error) {
			return f.customers.CreateSupplier(ctx, s, in)
		},
		"service_provider": func(ctx context.Context, s CustomerDataScope, in CRMEntitySave) (any, error) {
			return f.customers.CreateServiceProvider(ctx, s, in)
		},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := create(f.ctx, crmWriteAllowedScope(), CRMEntitySave{
				Name: "x", CustomerID: "crm-write-a1", ChannelAccountID: ptrUintCRM(62002),
			})
			if !errors.Is(err, ErrCRMCustomerForbidden) {
				t.Fatalf("挂未授权账号应拒绝，实际 err=%v", err)
			}
		})
	}
}

// 合法客户挂他租户/不存在的账号必须拒绝。
func TestCRMEntityWrite_CrossTenantAndMissingAccountRejected(t *testing.T) {
	f := setupCRMWriteFixture(t)
	for _, accountID := range []uint{62003, 99999} {
		_, err := f.customers.CreateContact(f.ctx, crmWriteAllowedScope(), CRMEntitySave{
			Name: "x", CustomerID: "crm-write-a1", ChannelAccountID: ptrUintCRM(accountID),
		})
		if !errors.Is(err, ErrCRMCustomerForbidden) {
			t.Fatalf("挂他租户/不存在账号(%d)应拒绝，实际 err=%v", accountID, err)
		}
	}
}

// UpdateEntity 改挂账号指针到未授权账号必须拒绝，旧对象不变。
func TestCRMEntityWrite_UpdateRejectsUnauthorizedAccountPointer(t *testing.T) {
	f := setupCRMWriteFixture(t)
	row := &model.Contact{TenantID: crmWriteTenantA, Name: "old", CustomerID: "crm-write-a1", ChannelAccountID: ptrUintCRM(62001)}
	if err := f.customers.contacts.Create(f.ctx, row); err != nil {
		t.Fatalf("准备实体夹具失败: %v", err)
	}
	_, err := f.customers.UpdateEntity(f.ctx, crmWriteAllowedScope(), CRMEntityContact, row.ID, CRMEntitySave{
		Name: "changed", CustomerID: "crm-write-a1", ChannelAccountID: ptrUintCRM(62002),
	})
	if !errors.Is(err, ErrCRMCustomerForbidden) {
		t.Fatalf("改挂未授权账号应拒绝，实际 err=%v", err)
	}
	got, err := f.customers.contacts.GetByID(f.ctx, row.ID)
	if err != nil || got.Name != "old" || got.ChannelAccountID == nil || *got.ChannelAccountID != 62001 {
		t.Fatalf("拒绝后原对象应不变，实际 row=%+v err=%v", got, err)
	}
}

// 合法账号 + 合法行业 + 合法负责人应正常创建；管理员不受行业/负责人范围限制。
func TestCRMCustomerWrite_AllowedCreateAndAdmin(t *testing.T) {
	f := setupCRMWriteFixture(t)
	row, err := f.customers.Create(f.ctx, crmWriteAllowedScope(), CRMCustomerSave{
		Name: "legal", SourceAccountID: "acct-1", Industry: "A", OwnerUserID: 7,
	})
	if err != nil || row == nil || row.SourceAccountID != "acct-1" {
		t.Fatalf("合法创建应成功，实际 row=%+v err=%v", row, err)
	}
	admin := CustomerDataScope{UserID: 9, AllAccounts: true}
	row2, err := f.customers.Create(f.ctx, admin, CRMCustomerSave{
		Name: "admin", SourceAccountID: "acct-2", Industry: "B", OwnerUserID: 99,
	})
	if err != nil || row2 == nil {
		t.Fatalf("管理员创建应成功，实际 err=%v", err)
	}
}

// 跨行业请求被拒，且原客户不变。
func TestCRMCustomerWrite_DisallowedIndustryRejected(t *testing.T) {
	f := setupCRMWriteFixture(t)
	scope := CustomerDataScope{UserID: 7, RestrictedAccounts: true, AccountIDs: []string{"acct-1"}, Industries: []string{"A"}}
	_, err := f.customers.Update(f.ctx, scope, "crm-write-a1", CRMCustomerSave{
		Name: "changed", SourceAccountID: "acct-1", Industry: "B", OwnerUserID: 7,
	})
	if !errors.Is(err, ErrCRMCustomerForbidden) {
		t.Fatalf("超范围行业更新应拒绝，实际 err=%v", err)
	}
	got, err := f.repo.GetByID(f.ctx, "crm-write-a1")
	if err != nil || got.Name != "A1" || got.Industry != "A" {
		t.Fatalf("拒绝后原客户应不变，实际 row=%+v err=%v", got, err)
	}
}

// 合法账号指针挂接应成功，清空指针不视为越权。
func TestCRMEntityWrite_AllowedAccountAndClear(t *testing.T) {
	f := setupCRMWriteFixture(t)
	admin := CustomerDataScope{UserID: 9, AllAccounts: true}
	scope := CustomerDataScope{UserID: 7, RestrictedAccounts: true, AccountIDs: []string{"acct-1", "62001"}}
	cust, err := f.customers.Create(f.ctx, admin, CRMCustomerSave{Name: "owner-cust", SourceAccountID: "acct-1", Industry: "A", OwnerUserID: 7})
	if err != nil {
		t.Fatalf("准备客户夹具失败: %v", err)
	}
	if _, err := f.permission.GetByIDScoped(f.ctx, scope, cust.ID); err != nil {
		t.Fatalf("受限员工应能读取授权客户，实际 err=%v", err)
	}
	row, err := f.customers.CreateContact(f.ctx, admin, CRMEntitySave{
		Name: "legal", CustomerID: cust.ID, ChannelAccountID: ptrUintCRM(62001),
	})
	if err != nil || row == nil || row.ChannelAccountID == nil || *row.ChannelAccountID != 62001 {
		t.Fatalf("授权账号挂接应成功，实际 row=%+v err=%v", row, err)
	}
	if _, err := f.customers.CreateSupplier(f.ctx, admin, CRMEntitySave{
		Name: "no-account", CustomerID: cust.ID, ChannelAccountID: nil,
	}); err != nil {
		t.Fatalf("未提交账号指针不应被拒，实际 err=%v", err)
	}
}

func TestCRMAccountRulePairAllowsWrite(t *testing.T) {
	svc := &CustomerPermissionService{}
	cases := []struct {
		name     string
		scope    CustomerDataScope
		account  string
		industry string
		owner    uint
		want     bool
	}{
		{
			name: "matching_pair",
			scope: CustomerDataScope{UserID: 7, AccountRules: []CustomerAccountRule{
				{AccountID: "62001", Industries: []string{"A"}},
			}},
			account: "62001", industry: "A", owner: 9, want: true,
		},
		{
			name: "cross_industry_denied",
			scope: CustomerDataScope{UserID: 7, AccountRules: []CustomerAccountRule{
				{AccountID: "62001", Industries: []string{"A"}},
			}},
			account: "62001", industry: "B", owner: 9, want: false,
		},
		{
			name: "owner_role_match",
			scope: CustomerDataScope{UserID: 7, AccountRules: []CustomerAccountRule{
				{AccountID: "62001", Role: "owner"},
			}},
			account: "62001", industry: "A", owner: 7, want: true,
		},
		{
			name: "owner_role_mismatch",
			scope: CustomerDataScope{UserID: 7, AccountRules: []CustomerAccountRule{
				{AccountID: "62001", Role: "owner"},
			}},
			account: "62001", industry: "A", owner: 8, want: false,
		},
		{
			name: "explicit_owner_match",
			scope: CustomerDataScope{UserID: 7, AccountRules: []CustomerAccountRule{
				{AccountID: "62001", OwnerUserID: 9},
			}},
			account: "62001", industry: "A", owner: 9, want: true,
		},
		{
			name: "explicit_owner_mismatch",
			scope: CustomerDataScope{UserID: 7, AccountRules: []CustomerAccountRule{
				{AccountID: "62001", OwnerUserID: 9},
			}},
			account: "62001", industry: "A", owner: 8, want: false,
		},
		{
			name: "rule_cannot_widen_restricted_accounts",
			scope: CustomerDataScope{UserID: 7, RestrictedAccounts: true, AccountIDs: []string{"acct-1"},
				AccountRules: []CustomerAccountRule{
					{AccountID: "62001", Industries: []string{"A"}},
				}},
			account: "62001", industry: "A", owner: 9, want: false,
		},
		{
			name: "admin_bypass",
			scope: CustomerDataScope{UserID: 9, AllAccounts: true, AccountRules: []CustomerAccountRule{
				{AccountID: "62001", Industries: []string{"A"}, Role: "owner"},
			}},
			account: "62002", industry: "B", owner: 99, want: true,
		},
		{
			name:    "empty_restricted_denied",
			scope:   CustomerDataScope{UserID: 7, RestrictedAccounts: true},
			account: "62001", industry: "A", owner: 7, want: false,
		},
		{
			name:    "zero_owner_only_owned_denied",
			scope:   CustomerDataScope{UserID: 7, RestrictedAccounts: true, AccountIDs: []string{"62001"}, OnlyOwned: true},
			account: "62001", industry: "A", owner: 0, want: false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := svc.AllowsWrite(tc.scope, tc.account, tc.industry, tc.owner); got != tc.want {
				t.Fatalf("AllowsWrite()=%v, want %v; scope=%+v account=%q industry=%q owner=%d", got, tc.want, tc.scope, tc.account, tc.industry, tc.owner)
			}
		})
	}
}

func TestCRMEntityWriteAccountRulePairIsApplied(t *testing.T) {
	f := setupCRMWriteFixture(t)
	admin := CustomerDataScope{UserID: 9, AllAccounts: true}
	parent := func(t *testing.T, owner uint) *model.Customer {
		t.Helper()
		row, err := f.customers.Create(f.ctx, admin, CRMCustomerSave{
			Name: "pair-parent", SourceAccountID: "acct-1", Industry: "A", OwnerUserID: owner,
		})
		if err != nil {
			t.Fatalf("准备配对客户夹具失败: %v", err)
		}
		return row
	}

	t.Run("matching_account_industry_pair_allowed", func(t *testing.T) {
		cust := parent(t, 7)
		scope := CustomerDataScope{
			UserID: 7, RestrictedAccounts: true, AccountIDs: []string{"acct-1", "62001"},
			AccountRules: []CustomerAccountRule{
				{AccountID: "acct-1", Industries: []string{"A"}},
				{AccountID: "62001", Industries: []string{"A"}},
			},
		}
		row, err := f.customers.CreateContact(f.ctx, scope, CRMEntitySave{
			Name: "pair-ok", CustomerID: cust.ID, ChannelAccountID: ptrUintCRM(62001),
		})
		if err != nil || row == nil {
			t.Fatalf("账号与行业配对一致应可挂接，实际 row=%+v err=%v", row, err)
		}
	})

	t.Run("cross_industry_pair_denied_even_if_account_id_listed", func(t *testing.T) {
		cust := parent(t, 7)
		scope := CustomerDataScope{
			UserID: 7, RestrictedAccounts: true, AccountIDs: []string{"acct-1", "62001"},
			AccountRules: []CustomerAccountRule{
				{AccountID: "acct-1", Industries: []string{"A"}},
				{AccountID: "62001", Industries: []string{"B"}},
			},
		}
		_, err := f.customers.CreateContact(f.ctx, scope, CRMEntitySave{
			Name: "pair-bad-industry", CustomerID: cust.ID, ChannelAccountID: ptrUintCRM(62001),
		})
		if !errors.Is(err, ErrCRMCustomerForbidden) {
			t.Fatalf("账号×行业配对不符必须拒绝，实际 err=%v", err)
		}
	})

	t.Run("owner_role_uses_customer_owner", func(t *testing.T) {
		cust := parent(t, 8)
		scope := CustomerDataScope{
			UserID: 7, RestrictedAccounts: true, AccountIDs: []string{"acct-1", "62001"},
			AccountRules: []CustomerAccountRule{
				{AccountID: "acct-1"},
				{AccountID: "62001", Role: "owner"},
			},
		}
		_, err := f.customers.CreateContact(f.ctx, scope, CRMEntitySave{
			Name: "pair-bad-owner", CustomerID: cust.ID, ChannelAccountID: ptrUintCRM(62001),
		})
		if !errors.Is(err, ErrCRMCustomerForbidden) {
			t.Fatalf("owner 角色必须按目标客户负责人判定，实际 err=%v", err)
		}
	})

	t.Run("explicit_owner_mismatch_denied", func(t *testing.T) {
		cust := parent(t, 8)
		scope := CustomerDataScope{
			UserID: 7, RestrictedAccounts: true, AccountIDs: []string{"acct-1", "62001"},
			AccountRules: []CustomerAccountRule{
				{AccountID: "acct-1"},
				{AccountID: "62001", OwnerUserID: 9},
			},
		}
		_, err := f.customers.CreateContact(f.ctx, scope, CRMEntitySave{
			Name: "pair-bad-explicit-owner", CustomerID: cust.ID, ChannelAccountID: ptrUintCRM(62001),
		})
		if !errors.Is(err, ErrCRMCustomerForbidden) {
			t.Fatalf("显式负责人配对不符必须拒绝，实际 err=%v", err)
		}
	})

	t.Run("explicit_owner_match_allowed", func(t *testing.T) {
		cust := parent(t, 9)
		scope := CustomerDataScope{
			UserID: 7, RestrictedAccounts: true, AccountIDs: []string{"acct-1", "62001"},
			AccountRules: []CustomerAccountRule{
				{AccountID: "acct-1"},
				{AccountID: "62001", OwnerUserID: 9},
			},
		}
		_, err := f.customers.CreateContact(f.ctx, scope, CRMEntitySave{
			Name: "pair-explicit-owner-ok", CustomerID: cust.ID, ChannelAccountID: ptrUintCRM(62001),
		})
		if err != nil {
			t.Fatalf("显式负责人配对一致应允许，实际 err=%v", err)
		}
	})

	t.Run("admin_bypass", func(t *testing.T) {
		cust := parent(t, 7)
		_, err := f.customers.CreateContact(f.ctx, admin, CRMEntitySave{
			Name: "pair-admin", CustomerID: cust.ID, ChannelAccountID: ptrUintCRM(62002),
		})
		if err != nil {
			t.Fatalf("管理员不受 AccountRules 收敛，实际 err=%v", err)
		}
	})
}

func TestCRMEntityWrite_UpdateRejectsAccountRulePairMismatch(t *testing.T) {
	f := setupCRMWriteFixture(t)
	admin := CustomerDataScope{UserID: 9, AllAccounts: true}
	cust, err := f.customers.Create(f.ctx, admin, CRMCustomerSave{
		Name: "update-pair-parent", SourceAccountID: "acct-1", Industry: "A", OwnerUserID: 7,
	})
	if err != nil {
		t.Fatalf("准备客户夹具失败: %v", err)
	}
	row := &model.Contact{TenantID: crmWriteTenantA, Name: "old", CustomerID: cust.ID, ChannelAccountID: ptrUintCRM(62001)}
	if err := f.customers.contacts.Create(f.ctx, row); err != nil {
		t.Fatalf("准备实体夹具失败: %v", err)
	}
	scope := CustomerDataScope{
		UserID: 7, RestrictedAccounts: true, AccountIDs: []string{"acct-1", "62001"},
		AccountRules: []CustomerAccountRule{
			{AccountID: "acct-1", Industries: []string{"A"}},
			{AccountID: "62001", Industries: []string{"B"}},
		},
	}
	_, err = f.customers.UpdateEntity(f.ctx, scope, CRMEntityContact, row.ID, CRMEntitySave{
		Name: "changed", CustomerID: cust.ID, ChannelAccountID: ptrUintCRM(62001),
	})
	if !errors.Is(err, ErrCRMCustomerForbidden) {
		t.Fatalf("改挂账号×行业配对不符必须拒绝，实际 err=%v", err)
	}
	got, err := f.customers.contacts.GetByID(f.ctx, row.ID)
	if err != nil || got.Name != "old" || got.ChannelAccountID == nil || *got.ChannelAccountID != 62001 {
		t.Fatalf("拒绝后原实体应不变，实际 row=%+v err=%v", got, err)
	}
}
