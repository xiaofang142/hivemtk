package repository

import (
	"context"
	"testing"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/pkg/testutil"
)

const (
	crmScopeTenantA = uint(6201)
	crmScopeTenantB = uint(6202)
)

func setupCRMCustomerScopeFixture(t *testing.T) context.Context {
	t.Helper()
	previousMode := tenant.CurrentMode()
	tenant.SetMode(tenant.ModeOn)
	t.Cleanup(func() { tenant.SetMode(previousMode) })

	database := testutil.NewTestDB(t, &model.Customer{})
	if database == nil {
		t.Fatal("真实 PostgreSQL 测试库不可达：CRM scope 仓储行为不能跳过")
	}
	rows := []*model.Customer{
		{TenantID: crmScopeTenantA, ID: "crm-scope-a-1", UnifiedID: "crm-scope-a-1", Name: "A1", SourceAccountID: "acct-1", Industry: "A"},
		{TenantID: crmScopeTenantA, ID: "crm-scope-a-2", UnifiedID: "crm-scope-a-2", Name: "A2", SourceAccountID: "acct-1", Industry: "A"},
		{TenantID: crmScopeTenantA, ID: "crm-scope-b-1", UnifiedID: "crm-scope-b-1", Name: "B1", SourceAccountID: "acct-1", Industry: "B"},
		{TenantID: crmScopeTenantA, ID: "crm-scope-other-account", UnifiedID: "crm-scope-other-account", Name: "OtherAccount", SourceAccountID: "acct-2", Industry: "A"},
		{TenantID: crmScopeTenantB, ID: "crm-scope-other-tenant", UnifiedID: "crm-scope-other-tenant", Name: "OtherTenant", SourceAccountID: "acct-1", Industry: "A"},
	}
	if err := database.WithContext(tenant.WithSystemScope(context.Background())).Create(&rows).Error; err != nil {
		t.Fatalf("准备 CRM scope 夹具失败: %v", err)
	}
	return tenant.WithTenantID(context.Background(), crmScopeTenantA)
}

// TestCRMCustomerRepository_ListScoped_EmptyIndustriesWithoutDenialIsUnrestricted
// 普通空行业切片仍表示行业不受限，不能把既有调用误判为拒绝全部。
func TestCRMCustomerRepository_ListScoped_EmptyIndustriesWithoutDenialIsUnrestricted(t *testing.T) {
	ctx := setupCRMCustomerScopeFixture(t)
	repo := NewCRMCustomerRepository()
	filter := CustomerScopeFilter{
		RestrictedAccounts: true,
		SourceAccountIDs:   []string{"acct-1"},
		Page:               1,
		PageSize:           2,
	}

	rows, total, err := repo.ListScoped(ctx, filter)
	if err != nil || len(rows) != 2 || total != 3 {
		t.Fatalf("空行业且无拒绝态应返回账号内 A+B，实际 rows=%d,total=%d,err=%v", len(rows), total, err)
	}
	filter.Page = 2
	rows, total, err = repo.ListScoped(ctx, filter)
	if err != nil || len(rows) != 1 || total != 3 {
		t.Fatalf("空行业且无拒绝态第二页应保持 total=3，实际 rows=%d,total=%d,err=%v", len(rows), total, err)
	}
}

// TestCRMCustomerRepository_ListScoped_IndustryFilterPaginatesWithStableTotal
// 普通行业过滤继续按分页返回，并且每一页总数一致。
func TestCRMCustomerRepository_ListScoped_IndustryFilterPaginatesWithStableTotal(t *testing.T) {
	ctx := setupCRMCustomerScopeFixture(t)
	repo := NewCRMCustomerRepository()
	filter := CustomerScopeFilter{
		RestrictedAccounts: true,
		SourceAccountIDs:   []string{"acct-1"},
		Industries:         []string{"A"},
		Page:               1,
		PageSize:           1,
	}

	for _, page := range []int{1, 2} {
		filter.Page = page
		rows, total, err := repo.ListScoped(ctx, filter)
		if err != nil || len(rows) != 1 || total != 2 || rows[0].Industry != "A" {
			t.Fatalf("行业 A 第 %d 页异常 rows=%+v,total=%d,err=%v", page, rows, total, err)
		}
	}
}

// TestCRMCustomerRepository_ListScoped_DenyAllReturnsEmptyCountAndRows
// 显式拒绝态必须让 count 与分页 rows 同时为空，不能退化为无行业过滤。
func TestCRMCustomerRepository_ListScoped_DenyAllReturnsEmptyCountAndRows(t *testing.T) {
	ctx := setupCRMCustomerScopeFixture(t)
	repo := NewCRMCustomerRepository()
	filter := CustomerScopeFilter{
		RestrictedAccounts: true,
		SourceAccountIDs:   []string{"acct-1"},
		Industries:         []string{"A"},
		DenyAll:            true,
		PageSize:           1,
	}

	for _, page := range []int{1, 2} {
		filter.Page = page
		rows, total, err := repo.ListScoped(ctx, filter)
		if err != nil || len(rows) != 0 || total != 0 {
			t.Fatalf("DenyAll 第 %d 页应 rows=0,total=0，实际 rows=%d,total=%d,err=%v", page, len(rows), total, err)
		}
	}
}

// TestCRMCustomerRepository_ListScoped_RestrictedEmptyAccountsStillDenies
// 既有“账号受限但授权账号为空”拒绝语义保持不变。
func TestCRMCustomerRepository_ListScoped_RestrictedEmptyAccountsStillDenies(t *testing.T) {
	ctx := setupCRMCustomerScopeFixture(t)
	repo := NewCRMCustomerRepository()
	filter := CustomerScopeFilter{RestrictedAccounts: true, PageSize: 1}

	for _, page := range []int{1, 2} {
		filter.Page = page
		rows, total, err := repo.ListScoped(ctx, filter)
		if err != nil || len(rows) != 0 || total != 0 {
			t.Fatalf("空账号授权第 %d 页应拒绝全部，实际 rows=%d,total=%d,err=%v", page, len(rows), total, err)
		}
	}
}
