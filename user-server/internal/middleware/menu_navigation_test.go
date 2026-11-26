package middleware

import (
	"context"
	"net/http"
	"testing"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/service"
)

type navigationGrantRepo struct{ keys []string }

func (r *navigationGrantRepo) CountAll(context.Context) (int64, error) {
	return int64(len(r.keys)), nil
}
func (r *navigationGrantRepo) ListKeysByRole(context.Context, string) ([]string, error) {
	return r.keys, nil
}
func (r *navigationGrantRepo) ReplaceRoleKeys(context.Context, string, []string) error { return nil }
func (r *navigationGrantRepo) BulkInsert(context.Context, []*model.RoleMenuPermission) error {
	return nil
}

// 页内入口沿用菜单授权上界：即使存量表误存 admin 叶子，普通角色实打接口仍为 403。
func TestMenuNavigation_CRMAndLeadEntryPointsRequireAdmin(t *testing.T) {
	svc := service.NewMenuServiceWithRepo(&navigationGrantRepo{keys: []string{"crmMaster", "leadGovernance"}})
	withProvider(t, svc.GrantProvider)
	cases := []struct{ method, path string }{
		{"GET", "/api/manage/crm/customers"}, {"GET", "/api/manage/crm/contacts"},
		{"GET", "/api/manage/crm/suppliers"}, {"GET", "/api/manage/crm/service-providers"},
		{"POST", "/api/manage/crm/dedupe/scan"}, {"GET", "/api/manage/crm/merge/audit"},
		{"POST", "/api/manage/crm/import"}, {"GET", "/api/manage/crm/export"},
		{"GET", "/api/manage/crm/360/1"}, {"GET", "/api/manage/crm/cross-industry"},
		{"GET", "/api/manage/leads/rules"}, {"POST", "/api/manage/leads/rules"},
		{"GET", "/api/manage/leads/notify-routes"}, {"GET", "/api/manage/leads/messages"},
		{"GET", "/api/manage/leads/customers"}, {"GET", "/api/manage/leads/judgements/7/hits"},
		{"GET", "/api/manage/lead-mining-agents"}, {"GET", "/api/manage/lead-mining-agents/1"},
	}
	for _, tc := range cases {
		for _, role := range []string{model.SystemRoleCodeCustomerService, model.SystemRoleCodeStaff, model.SystemRoleCodeAdmin} {
			status, body, handled := runGuard(t, tc.method, tc.path, role)
			if role == model.SystemRoleCodeAdmin {
				if !handled || status == http.StatusForbidden {
					t.Errorf("admin 应通过 %s %s 的授权门禁，实际 %d %s", tc.method, tc.path, status, body)
				}
			} else if status != http.StatusForbidden || handled {
				t.Errorf("%s 对 %s %s 应为 403 且不执行业务，实际 %d %s", role, tc.method, tc.path, status, body)
			}
			if got := ModuleOwnerOf(tc.path); got != model.ModuleCustomer {
				t.Errorf("%s 应归客户模块，实际 %q", tc.path, got)
			}
		}
	}
}

// 业务叶子换目录后，真实能力面和 HTTP 模块归属仍读同一 key，存量越界行不能放行。
func TestMenuNavigation_CapabilitySurfaceAndModuleOwners(t *testing.T) {
	cases := []struct{ path, key, module string }{
		{"/api/material/list", "systemMaterialLibrary", model.ModuleCore},
		{"/api/office-hours", "automationCenter", model.ModuleCore},
		{"/api/geo/decision/report", "geoDecisionReport", model.ModuleGEO},
		{"/api/geo/decision/tasks", "geoDecisionReport", model.ModuleGEO},
		{"/api/reach/pipelines", "reachPipeline", model.ModuleReachAuto},
		{"/api/quick-replies", "quickReply", model.ModuleInbox},
		{"/api/manage/crm/customers", "crmMaster", model.ModuleCustomer},
		{"/api/manage/crm/merge/audit", "crmMaster", model.ModuleCustomer},
		{"/api/manage/leads/rules", "leadGovernance", model.ModuleCustomer},
		{"/api/manage/leads/judgements/7/hits", "leadGovernance", model.ModuleCustomer},
		{"/api/manage/lead-mining-agents", "leadGovernance", model.ModuleCustomer},
		{"/api/v1/commerce/profiles", "commerceOperations", model.ModuleReachSocial},
	}
	repo := &navigationGrantRepo{keys: []string{"systemMaterialLibrary", "automationCenter", "geoDecisionReport", "reachPipeline", "quickReply", "crmMaster", "leadGovernance", "commerceOperations"}}
	svc := service.NewMenuServiceWithRepo(repo)
	withProvider(t, svc.GrantProvider)
	for _, tc := range cases {
		t.Run(tc.key+tc.path, func(t *testing.T) {
			found := false
			for _, key := range CapabilityOwnerKeysFor(tc.path) {
				found = found || key == tc.key
			}
			if !found {
				t.Fatalf("%s 的能力面应包含 %s，实际 %v", tc.path, tc.key, CapabilityOwnerKeysFor(tc.path))
			}
			if got := ModuleOwnerOf(tc.path); got != tc.module {
				t.Errorf("%s 的 HTTP 模块应为 %s，实际 %s", tc.path, tc.module, got)
			}
			if got := model.OwnerOfMenuKey(tc.key); got != tc.module {
				t.Errorf("%s 的菜单模块应为 %s，实际 %s", tc.key, tc.module, got)
			}
			for _, role := range []string{model.SystemRoleCodeCustomerService, model.SystemRoleCodeStaff} {
				wantAllowed := role == model.SystemRoleCodeCustomerService && tc.key == "quickReply"
				matched, allowed := CapabilityAllows(context.Background(), role, tc.path)
				if !matched || allowed != wantAllowed {
					t.Errorf("%s 对 %s 能力面应 matched=true allowed=%t，实际 %t/%t", role, tc.path, wantAllowed, matched, allowed)
				}
			}
		})
	}
}

// 菜单模块与 HTTP 模块同时执行开关：补上的业务路径不能只登记归属而不实际拦截。
func TestMenuNavigation_ModuleGuardRejectsClosedBusinessModules(t *testing.T) {
	inMultiTenantMode(t)
	cases := []struct{ path, module string }{
		{"/api/manage/crm/customers", model.ModuleCustomer},
		{"/api/manage/leads/judgements/7/hits", model.ModuleCustomer},
		{"/api/manage/lead-mining-agents", model.ModuleCustomer},
		{"/api/lead-mining/analyze", model.ModuleCustomer},
		{"/api/v1/commerce/profiles", model.ModuleReachSocial},
		{"/api/v1/commerce/operations/7/submit", model.ModuleReachSocial},
	}
	for _, tc := range cases {
		t.Run(tc.path, func(t *testing.T) {
			withChecker(t, fakeChecker{enabled: map[string]bool{}})
			if !runThroughPath(GuardModulePath(), tc.path) {
				t.Fatalf("关闭 %s 后必须阻止 %s 执行业务", tc.module, tc.path)
			}
			withChecker(t, fakeChecker{enabled: map[string]bool{tc.module: true}})
			if runThroughPath(GuardModulePath(), tc.path) {
				t.Fatalf("开通 %s 后应允许 %s 通过模块闸", tc.module, tc.path)
			}
		})
	}
}
