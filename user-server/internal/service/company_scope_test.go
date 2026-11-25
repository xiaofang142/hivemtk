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
	companyScopeTenant = uint(6301)
)

type companyScopeFixture struct {
	ctx     context.Context
	svc     *CompanyService
	perm    *CustomerPermissionService
	relRepo repository.CompanyRepository
}

func setupCompanyScopeFixture(t *testing.T) companyScopeFixture {
	t.Helper()
	previousMode := tenant.CurrentMode()
	tenant.SetMode(tenant.ModeOn)
	t.Cleanup(func() { tenant.SetMode(previousMode) })

	db := testutil.NewTestDB(t, &model.Company{}, &model.CustomerCompanyRelation{}, &model.Customer{})
	if db == nil {
		t.Fatal("真实 PostgreSQL 测试库不可达：公司数据范围行为不能跳过")
	}
	sysCtx := tenant.WithSystemScope(context.Background())
	companies := []*model.Company{
		{TenantID: companyScopeTenant, ID: 63001, Name: "CompanyA", Industry: "A", OwnerUserID: ptrUintCRM(7)},
		{TenantID: companyScopeTenant, ID: 63002, Name: "CompanyB", Industry: "B", OwnerUserID: ptrUintCRM(8)},
	}
	if err := db.WithContext(sysCtx).Create(&companies).Error; err != nil {
		t.Fatalf("准备公司夹具失败: %v", err)
	}
	customers := []*model.Customer{
		{TenantID: companyScopeTenant, ID: "co-cust-a", UnifiedID: "co-cust-a", Name: "CustA", SourceAccountID: "acct-a", Industry: "A", OwnerUserID: ptrUintCRM(7)},
		{TenantID: companyScopeTenant, ID: "co-cust-b", UnifiedID: "co-cust-b", Name: "CustB", SourceAccountID: "acct-b", Industry: "B", OwnerUserID: ptrUintCRM(8)},
	}
	if err := db.WithContext(sysCtx).Create(&customers).Error; err != nil {
		t.Fatalf("准备客户夹具失败: %v", err)
	}
	rels := []*model.CustomerCompanyRelation{
		{TenantID: companyScopeTenant, CustomerID: "co-cust-a", CompanyID: 63001, RelationType: "primary", IsPrimary: true},
		{TenantID: companyScopeTenant, CustomerID: "co-cust-b", CompanyID: 63002, RelationType: "primary", IsPrimary: true},
	}
	if err := db.WithContext(sysCtx).Create(&rels).Error; err != nil {
		t.Fatalf("准备关系夹具失败: %v", err)
	}

	perm := NewCustomerPermissionService()
	repo := repository.NewCompanyRepository()
	return companyScopeFixture{
		ctx:     tenant.WithTenantID(context.Background(), companyScopeTenant),
		svc:     &CompanyService{repo: repo, permission: perm},
		perm:    perm,
		relRepo: repo,
	}
}

func companyScopeA() CustomerDataScope {
	return CustomerDataScope{UserID: 7, RestrictedAccounts: true, AccountIDs: []string{"acct-a"}, Industries: []string{"A"}}
}

func companyScopeB() CustomerDataScope {
	return CustomerDataScope{UserID: 8, RestrictedAccounts: true, AccountIDs: []string{"acct-b"}, Industries: []string{"B"}}
}

// 受限员工只能列出其可见客户关联的公司，总数不能泄露另一员工的关系。
func TestCompanyScope_ListRestricted(t *testing.T) {
	f := setupCompanyScopeFixture(t)
	items, total, err := f.svc.ListScoped(f.ctx, companyScopeA(), "", 1, 20)
	if err != nil {
		t.Fatalf("受限员工列表失败: %v", err)
	}
	if total != 1 || len(items) != 1 || items[0].ID != 63001 {
		t.Fatalf("应只见 CompanyA，实际 total=%d items=%+v", total, items)
	}
}

// 受限员工不能按 ID 读取未授权公司。
func TestCompanyScope_GetForbidden(t *testing.T) {
	f := setupCompanyScopeFixture(t)
	if _, err := f.svc.GetByIDScoped(f.ctx, companyScopeA(), 63002); !errors.Is(err, ErrCRMCustomerForbidden) {
		t.Fatalf("读取未授权公司应拒绝，实际 err=%v", err)
	}
}

// 受限员工不能把未授权客户挂到公司关系上。
func TestCompanyScope_UpsertRelationForbiddenCustomer(t *testing.T) {
	f := setupCompanyScopeFixture(t)
	err := f.svc.UpsertRelationScoped(f.ctx, companyScopeA(), &model.CustomerCompanyRelation{
		CustomerID: "co-cust-b", CompanyID: 63001, RelationType: "primary",
	})
	if !errors.Is(err, ErrCRMCustomerForbidden) {
		t.Fatalf("关联未授权客户应拒绝，实际 err=%v", err)
	}
}

// 受限员工不能把关系挂到未授权公司。
func TestCompanyScope_UpsertRelationForbiddenCompany(t *testing.T) {
	f := setupCompanyScopeFixture(t)
	err := f.svc.UpsertRelationScoped(f.ctx, companyScopeA(), &model.CustomerCompanyRelation{
		CustomerID: "co-cust-a", CompanyID: 63002, RelationType: "primary",
	})
	if !errors.Is(err, ErrCRMCustomerForbidden) {
		t.Fatalf("关联未授权公司应拒绝，实际 err=%v", err)
	}
}

// 授权客户 + 授权公司的关系写入应成功。
func TestCompanyScope_UpsertRelationAllowed(t *testing.T) {
	f := setupCompanyScopeFixture(t)
	err := f.svc.UpsertRelationScoped(f.ctx, companyScopeA(), &model.CustomerCompanyRelation{
		CustomerID: "co-cust-a", CompanyID: 63001, RelationType: "secondary",
	})
	if err != nil {
		t.Fatalf("授权关系写入应成功，实际 err=%v", err)
	}
}

// 关系列表只能返回可见客户/公司范围内的关系。
func TestCompanyScope_ListRelationsRestricted(t *testing.T) {
	f := setupCompanyScopeFixture(t)
	rels, err := f.svc.ListRelationsByCustomerScoped(f.ctx, companyScopeA(), "co-cust-a")
	if err != nil || len(rels) != 1 || rels[0].CompanyID != 63001 {
		t.Fatalf("客户关系列表应仅见授权公司，实际 rels=%+v err=%v", rels, err)
	}
	if _, err := f.svc.ListRelationsByCustomerScoped(f.ctx, companyScopeA(), "co-cust-b"); !errors.Is(err, ErrCRMCustomerForbidden) {
		t.Fatalf("读取未授权客户的关系应拒绝，实际 err=%v", err)
	}
}
