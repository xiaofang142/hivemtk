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

const followupScopeTenant = uint(6601)

func setupFollowupScopeFixture(t *testing.T) (*FollowupRecordService, context.Context) {
	t.Helper()
	previousMode := tenant.CurrentMode()
	tenant.SetMode(tenant.ModeOn)
	t.Cleanup(func() { tenant.SetMode(previousMode) })

	db := testutil.NewTestDB(t, &model.Customer{}, &model.FollowupRecord{})
	if db == nil {
		t.Fatal("真实 PostgreSQL 测试库不可达：跟进权限不能跳过")
	}
	ctx := tenant.WithTenantID(context.Background(), followupScopeTenant)
	sysCtx := tenant.WithSystemScope(context.Background())
	customers := []*model.Customer{
		{TenantID: followupScopeTenant, ID: "fu-a", UnifiedID: "fu-a", Name: "A", SourceAccountID: "acct-a", Industry: "A", OwnerUserID: ptrUintCRM(7)},
		{TenantID: followupScopeTenant, ID: "fu-b", UnifiedID: "fu-b", Name: "B", SourceAccountID: "acct-b", Industry: "B", OwnerUserID: ptrUintCRM(8)},
	}
	if err := db.WithContext(sysCtx).Create(&customers).Error; err != nil {
		t.Fatalf("准备客户失败: %v", err)
	}
	records := []*model.FollowupRecord{
		{TenantID: followupScopeTenant, RecordID: "fr-a", CustomerID: "fu-a", OwnerID: 7, Content: "A"},
		{TenantID: followupScopeTenant, RecordID: "fr-b", CustomerID: "fu-b", OwnerID: 8, Content: "B"},
	}
	if err := db.WithContext(sysCtx).Create(&records).Error; err != nil {
		t.Fatalf("准备跟进失败: %v", err)
	}
	return &FollowupRecordService{
		repo:       repository.NewFollowupRecordRepository(),
		permission: NewCustomerPermissionService(),
	}, ctx
}

func followupScopeA() CustomerDataScope {
	return CustomerDataScope{UserID: 7, RestrictedAccounts: true, AccountIDs: []string{"acct-a"}, Industries: []string{"A"}}
}

// 列表必须按客户数据范围过滤：员工 A 不能通过 owner=0 拿到员工 B 的记录，总数不含隐藏行。
func TestFollowupScope_ListRestricted(t *testing.T) {
	svc, ctx := setupFollowupScopeFixture(t)
	items, total, err := svc.ListScoped(ctx, followupScopeA(), "", "", 0, 1, 20)
	if err != nil {
		t.Fatalf("列表失败: %v", err)
	}
	if total != 1 || len(items) != 1 || items[0].RecordID != "fr-a" {
		t.Fatalf("应仅见 fr-a，实际 total=%d items=%+v", total, items)
	}
}

// 创建时关联未授权客户必须拒绝。
func TestFollowupScope_CreateForbiddenCustomer(t *testing.T) {
	svc, ctx := setupFollowupScopeFixture(t)
	err := svc.CreateScoped(ctx, followupScopeA(), &model.FollowupRecord{
		CustomerID: "fu-b", OwnerID: 7, Content: "x", EvidenceJSON: "[]",
	})
	if !errors.Is(err, ErrCRMCustomerForbidden) {
		t.Fatalf("关联未授权客户应拒绝，实际 err=%v", err)
	}
}

// 改挂到未授权客户必须拒绝，旧记录不变。
func TestFollowupScope_UpdateRebindForbidden(t *testing.T) {
	svc, ctx := setupFollowupScopeFixture(t)
	err := svc.UpdateScoped(ctx, followupScopeA(), &model.FollowupRecord{
		RecordID: "fr-a", CustomerID: "fu-b", OwnerID: 7, Content: "changed", EvidenceJSON: "[]",
	})
	if !errors.Is(err, ErrCRMCustomerForbidden) {
		t.Fatalf("改挂未授权客户应拒绝，实际 err=%v", err)
	}
	got, err := svc.repo.GetByRecordID(ctx, "fr-a")
	if err != nil || got.CustomerID != "fu-a" || got.Content != "A" {
		t.Fatalf("拒绝后旧记录应不变，实际 got=%+v err=%v", got, err)
	}
}

// 合法客户写入应成功。
func TestFollowupScope_CreateAllowed(t *testing.T) {
	svc, ctx := setupFollowupScopeFixture(t)
	err := svc.CreateScoped(ctx, followupScopeA(), &model.FollowupRecord{
		CustomerID: "fu-a", OwnerID: 7, Content: "ok", EvidenceJSON: "[]",
	})
	if err != nil {
		t.Fatalf("合法创建应成功，实际 err=%v", err)
	}
}
