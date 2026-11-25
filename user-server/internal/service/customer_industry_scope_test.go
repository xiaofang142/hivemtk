package service

import (
	"context"
	"testing"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/pkg/testutil"
	"hivemtk-user/internal/repository"
)

const (
	crmIndustryTenantA = uint(6101)
	crmIndustryTenantB = uint(6102)
)

type crmIndustryScopeFixture struct {
	ctx        context.Context
	permission *CustomerPermissionService
	customers  *CRMCustomerService
	imports    *CustomerImportExportService
	crm360     *CRM360Service
}

func setupCRMIndustryScopeFixture(t *testing.T) crmIndustryScopeFixture {
	t.Helper()
	previousMode := tenant.CurrentMode()
	tenant.SetMode(tenant.ModeOn)
	t.Cleanup(func() { tenant.SetMode(previousMode) })

	database := testutil.NewTestDB(t, &model.Customer{})
	if database == nil {
		t.Fatal("真实 PostgreSQL 测试库不可达：行业权限行为不能跳过")
	}
	rows := []*model.Customer{
		{TenantID: crmIndustryTenantA, ID: "crm-ind-a-1", UnifiedID: "crm-ind-a-1", Name: "A1", SourceAccountID: "acct-1", Industry: "A"},
		{TenantID: crmIndustryTenantA, ID: "crm-ind-a-2", UnifiedID: "crm-ind-a-2", Name: "A2", SourceAccountID: "acct-1", Industry: "A"},
		{TenantID: crmIndustryTenantA, ID: "crm-ind-b-1", UnifiedID: "crm-ind-b-1", Name: "B1", SourceAccountID: "acct-1", Industry: "B"},
		{TenantID: crmIndustryTenantA, ID: "crm-ind-other-account", UnifiedID: "crm-ind-other-account", Name: "OtherAccount", SourceAccountID: "acct-2", Industry: "A"},
		{TenantID: crmIndustryTenantB, ID: "crm-ind-other-tenant", UnifiedID: "crm-ind-other-tenant", Name: "OtherTenant", SourceAccountID: "acct-1", Industry: "A"},
	}
	if err := database.WithContext(tenant.WithSystemScope(context.Background())).Create(&rows).Error; err != nil {
		t.Fatalf("准备客户行业夹具失败: %v", err)
	}

	repo := repository.NewCRMCustomerRepository()
	permission := &CustomerPermissionService{customers: repo}
	return crmIndustryScopeFixture{
		ctx:        tenant.WithTenantID(context.Background(), crmIndustryTenantA),
		permission: permission,
		customers:  &CRMCustomerService{permission: permission},
		imports:    &CustomerImportExportService{permission: permission, scopeRepo: repo},
		crm360:     &CRM360Service{permission: permission},
	}
}

func crmIndustryAScope() CustomerDataScope {
	return CustomerDataScope{
		UserID:             7,
		RestrictedAccounts: true,
		AccountIDs:         []string{"acct-1"},
		Industries:         []string{"A"},
	}
}

// TestCRMCustomerList_EmptyIndustryIntersection_ReturnsNoRowsAndZeroTotal
// 仅授权行业 A 的员工请求 B，列表第一页和第二页都必须为空且总数为零。
func TestCRMCustomerList_EmptyIndustryIntersection_ReturnsNoRowsAndZeroTotal(t *testing.T) {
	fixture := setupCRMIndustryScopeFixture(t)
	for _, page := range []int{1, 2} {
		rows, total, err := fixture.customers.List(fixture.ctx, crmIndustryAScope(), "", "B", 0, page, 1)
		if err != nil {
			t.Fatalf("第 %d 页查询失败: %v", page, err)
		}
		if len(rows) != 0 || total != 0 {
			t.Fatalf("A→B 第 %d 页应 rows=0,total=0，实际 rows=%d,total=%d", page, len(rows), total)
		}
	}
}

// TestCRM360CrossIndustrySearch_EmptyIndustryIntersection_ReturnsNoRows
// 跨行业检索不能把空交集复活为授权行业 A；第一页和第二页都必须为空。
func TestCRM360CrossIndustrySearch_EmptyIndustryIntersection_ReturnsNoRows(t *testing.T) {
	fixture := setupCRMIndustryScopeFixture(t)
	for _, page := range []int{1, 2} {
		result, err := fixture.crm360.CrossIndustrySearch(fixture.ctx, crmIndustryTenantA, crmIndustryAScope(), "", "B", page, 1)
		if err != nil {
			t.Fatalf("第 %d 页跨行业检索失败: %v", page, err)
		}
		if result == nil || len(result.Items) != 0 || len(result.CrossTab) != 0 || result.Total != 0 {
			t.Fatalf("A→B 第 %d 页应 items=0,cross_tab=0,total=0，实际 %+v", page, result)
		}
	}
}

// TestCRMExportByQuery_EmptyIndustryIntersection_ReturnsNoRows
// 导出与列表共享拒绝态，A→B 必须导出空结果且总数为零。
func TestCRMExportByQuery_EmptyIndustryIntersection_ReturnsNoRows(t *testing.T) {
	fixture := setupCRMIndustryScopeFixture(t)
	result, err := fixture.imports.ExportByQuery(fixture.ctx, crmIndustryAScope(), "", "B", 0, "tester")
	if err != nil {
		t.Fatalf("A→B 导出失败: %v", err)
	}
	if result == nil || len(result.Rows) != 0 || result.Total != 0 || !result.SensitiveMasked {
		t.Fatalf("A→B 导出应 rows=0,total=0,masked=true，实际 %+v", result)
	}
}

// TestCRMIndustryScope_MatchingIndustryPositive A→A 在三个入口保持正常分页与计数。
func TestCRMIndustryScope_MatchingIndustryPositive(t *testing.T) {
	fixture := setupCRMIndustryScopeFixture(t)
	scope := crmIndustryAScope()

	for _, page := range []int{1, 2} {
		rows, total, err := fixture.customers.List(fixture.ctx, scope, "", "A", 0, page, 1)
		if err != nil || len(rows) != 1 || total != 2 || rows[0].Industry != "A" {
			t.Fatalf("列表 A→A 第 %d 页异常 rows=%+v,total=%d,err=%v", page, rows, total, err)
		}
		result, err := fixture.crm360.CrossIndustrySearch(fixture.ctx, crmIndustryTenantA, scope, "", "A", page, 1)
		if err != nil || result == nil || len(result.Items) != 1 || result.Total != 2 || result.Items[0].Industry != "A" {
			t.Fatalf("检索 A→A 第 %d 页异常 result=%+v,err=%v", page, result, err)
		}
	}

	exported, err := fixture.imports.ExportByQuery(fixture.ctx, scope, "", "A", 0, "tester")
	if err != nil || exported == nil || len(exported.Rows) != 2 || exported.Total != 2 {
		t.Fatalf("导出 A→A 异常 result=%+v,err=%v", exported, err)
	}
	for _, row := range exported.Rows {
		if row.Industry != "A" {
			t.Fatalf("导出 A→A 出现其他行业: %+v", row)
		}
	}
}

// TestCRMIndustryScope_PartialIntersectionPositive 部分交集在真实仓储中只返回共同允许的行业。
func TestCRMIndustryScope_PartialIntersectionPositive(t *testing.T) {
	fixture := setupCRMIndustryScopeFixture(t)
	scope := CustomerDataScope{
		UserID:             10,
		RestrictedAccounts: true,
		AccountIDs:         []string{"acct-1"},
		Industries:         []string{"A", "B"},
	}

	rows, total, err := fixture.permission.ListScoped(fixture.ctx, scope, repository.CustomerScopeFilter{
		Industries: []string{"B", "C"},
		Page:       1,
		PageSize:   20,
	})
	if err != nil || len(rows) != 1 || total != 1 || rows[0].Industry != "B" {
		t.Fatalf("部分交集 [A,B]∩[B,C] 应只返回 B，实际 rows=%+v,total=%d,err=%v", rows, total, err)
	}
}

// TestCRMIndustryScope_AdminBypassesIndustryGrant 管理员请求 B 时仍只返回 B。
func TestCRMIndustryScope_AdminBypassesIndustryGrant(t *testing.T) {
	fixture := setupCRMIndustryScopeFixture(t)
	scope := CustomerDataScope{UserID: 1, AllAccounts: true, Industries: []string{"A"}}

	rows, total, err := fixture.customers.List(fixture.ctx, scope, "", "B", 0, 1, 20)
	if err != nil || len(rows) != 1 || total != 1 || rows[0].Industry != "B" {
		t.Fatalf("管理员列表 B→B 异常 rows=%+v,total=%d,err=%v", rows, total, err)
	}
	result, err := fixture.crm360.CrossIndustrySearch(fixture.ctx, crmIndustryTenantA, scope, "", "B", 1, 20)
	if err != nil || result == nil || len(result.Items) != 1 || result.Total != 1 || result.Items[0].Industry != "B" {
		t.Fatalf("管理员检索 B→B 异常 result=%+v,err=%v", result, err)
	}
	exported, err := fixture.imports.ExportByQuery(fixture.ctx, scope, "", "B", 0, "tester")
	if err != nil || exported == nil || len(exported.Rows) != 1 || exported.Total != 1 || exported.Rows[0].Industry != "B" || exported.SensitiveMasked {
		t.Fatalf("管理员导出 B→B 异常 result=%+v,err=%v", exported, err)
	}
}

// TestCRMIndustryScope_UnrestrictedIndustryPreservesRequest 账号受限但行业不受限时 B→B 正常。
func TestCRMIndustryScope_UnrestrictedIndustryPreservesRequest(t *testing.T) {
	fixture := setupCRMIndustryScopeFixture(t)
	scope := CustomerDataScope{UserID: 8, RestrictedAccounts: true, AccountIDs: []string{"acct-1"}}

	rows, total, err := fixture.customers.List(fixture.ctx, scope, "", "B", 0, 1, 20)
	if err != nil || len(rows) != 1 || total != 1 || rows[0].Industry != "B" {
		t.Fatalf("行业不受限列表 B→B 异常 rows=%+v,total=%d,err=%v", rows, total, err)
	}
	result, err := fixture.crm360.CrossIndustrySearch(fixture.ctx, crmIndustryTenantA, scope, "", "B", 1, 20)
	if err != nil || result == nil || len(result.Items) != 1 || result.Total != 1 || result.Items[0].Industry != "B" {
		t.Fatalf("行业不受限检索 B→B 异常 result=%+v,err=%v", result, err)
	}
	exported, err := fixture.imports.ExportByQuery(fixture.ctx, scope, "", "B", 0, "tester")
	if err != nil || exported == nil || len(exported.Rows) != 1 || exported.Total != 1 || exported.Rows[0].Industry != "B" {
		t.Fatalf("行业不受限导出 B→B 异常 result=%+v,err=%v", exported, err)
	}
}

// TestCRMIndustryScope_EmptyAccountDeniesAll 空账号授权继续拒绝全部，不被行业逻辑放宽。
func TestCRMIndustryScope_EmptyAccountDeniesAll(t *testing.T) {
	fixture := setupCRMIndustryScopeFixture(t)
	scope := CustomerDataScope{UserID: 9, RestrictedAccounts: true, Industries: []string{"A"}}

	rows, total, err := fixture.customers.List(fixture.ctx, scope, "", "A", 0, 1, 20)
	if err != nil || len(rows) != 0 || total != 0 {
		t.Fatalf("空账号授权列表应拒绝全部，实际 rows=%+v,total=%d,err=%v", rows, total, err)
	}
	result, err := fixture.crm360.CrossIndustrySearch(fixture.ctx, crmIndustryTenantA, scope, "", "A", 1, 20)
	if err != nil || result == nil || len(result.Items) != 0 || result.Total != 0 {
		t.Fatalf("空账号授权检索应拒绝全部，实际 result=%+v,err=%v", result, err)
	}
	exported, err := fixture.imports.ExportByQuery(fixture.ctx, scope, "", "A", 0, "tester")
	if err != nil || exported == nil || len(exported.Rows) != 0 || exported.Total != 0 {
		t.Fatalf("空账号授权导出应拒绝全部，实际 result=%+v,err=%v", exported, err)
	}
}
