package service

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/pkg/testutil"
	"hivemtk-user/internal/repository"
)

const importScopeTenant = uint(6501)

func setupImportScopeFixture(t *testing.T) (*CustomerImportJobService, context.Context, string) {
	t.Helper()
	previousMode := tenant.CurrentMode()
	tenant.SetMode(tenant.ModeOn)
	t.Cleanup(func() { tenant.SetMode(previousMode) })

	db := testutil.NewTestDB(t, &model.Customer{}, &model.CustomerImportJob{})
	if db == nil {
		t.Fatal("真实 PostgreSQL 测试库不可达：导入作业权限不能跳过")
	}
	dir := t.TempDir()
	t.Setenv("IMPORT_JOB_DIR", dir)
	ctx := tenant.WithTenantID(context.Background(), importScopeTenant)
	svc := &CustomerImportJobService{
		repo:       repository.NewCustomerImportJobRepository(),
		custRepo:   repository.NewCustomerRepository(),
		permission: NewCustomerPermissionService(),
	}
	return svc, ctx, dir
}

func importScopeCreateJob(t *testing.T, svc *CustomerImportJobService, ctx context.Context, dir, rows string, creator uint) string {
	t.Helper()
	path := filepath.Join(dir, "scope.csv")
	if err := os.WriteFile(path, []byte(rows), 0600); err != nil {
		t.Fatalf("写导入文件失败: %v", err)
	}
	jobID := "imp_scope_" + strings.ReplaceAll(rows, "\n", "_")[:8]
	job := &model.CustomerImportJob{
		TenantID: importScopeTenant, JobID: jobID, Status: "prechecked",
		FilePath: path, TotalRows: 1, DedupStrategy: "skip", MappingJSON: "{}",
		CreatedBy: &creator,
	}
	if err := svc.repo.Create(ctx, job); err != nil {
		t.Fatalf("准备作业失败: %v", err)
	}
	return jobID
}

// 非创建者不能执行/读取/下载他人作业。
func TestImportScope_OtherActorCannotAccess(t *testing.T) {
	svc, ctx, dir := setupImportScopeFixture(t)
	jobID := importScopeCreateJob(t, svc, ctx, dir, "name,source_account_id\nA,acct-a\n", 7)
	scope := CustomerDataScope{UserID: 8, RestrictedAccounts: true, AccountIDs: []string{"acct-a"}}
	if _, err := svc.ExecuteScoped(ctx, scope, 8, jobID); !errors.Is(err, ErrImportJobForbidden) {
		t.Fatalf("他人作业执行应拒绝，实际 err=%v", err)
	}
	if _, err := svc.ErrorReportScoped(ctx, scope, 8, jobID); !errors.Is(err, ErrImportJobForbidden) {
		t.Fatalf("他人作业报告应拒绝，实际 err=%v", err)
	}
	if _, _, err := svc.ErrorReportCSVScoped(ctx, scope, 8, jobID); !errors.Is(err, ErrImportJobForbidden) {
		t.Fatalf("他人作业下载应拒绝，实际 err=%v", err)
	}
}

// 空授权不能执行作业，且不能创建客户（越权行记为失败）。
func TestImportScope_EmptyScopeRejects(t *testing.T) {
	svc, ctx, dir := setupImportScopeFixture(t)
	jobID := importScopeCreateJob(t, svc, ctx, dir, "name,source_account_id\nB,acct-b\n", 7)
	scope := CustomerDataScope{UserID: 7, RestrictedAccounts: true}
	if _, err := svc.ExecuteScoped(ctx, scope, 7, jobID); !errors.Is(err, ErrImportJobForbidden) {
		t.Fatalf("空授权执行应拒绝，实际 err=%v", err)
	}
}

// 受限员工执行自己的作业：越权账号行被拒，合法账号行正常导入。
func TestImportScope_RowAccountBoundary(t *testing.T) {
	svc, ctx, dir := setupImportScopeFixture(t)
	path := filepath.Join(dir, "mixed.csv")
	if err := os.WriteFile(path, []byte("name,phone,source_account_id\nAllowed,13000000001,acct-a\nForbidden,13000000002,acct-b\n"), 0600); err != nil {
		t.Fatalf("写文件失败: %v", err)
	}
	jobID := "imp_mixed_scope"
	if err := svc.repo.Create(ctx, &model.CustomerImportJob{
		TenantID: importScopeTenant, JobID: jobID, Status: "prechecked", FilePath: path,
		TotalRows: 2, DedupStrategy: "skip", MappingJSON: "{}", CreatedBy: ptrUintCRM(7),
	}); err != nil {
		t.Fatalf("准备作业失败: %v", err)
	}
	scope := CustomerDataScope{UserID: 7, RestrictedAccounts: true, AccountIDs: []string{"acct-a"}}
	report, err := svc.ExecuteScoped(ctx, scope, 7, jobID)
	if err != nil {
		t.Fatalf("执行应成功（越权行计失败），实际 err=%v", err)
	}
	if report.SuccessRows != 1 || report.FailedRows != 1 {
		t.Fatalf("期望 1 成功 1 失败，实际 success=%d failed=%d errs=%+v", report.SuccessRows, report.FailedRows, report.Errors)
	}
}
