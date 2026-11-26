package controller

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"hivemtk-user/internal/middleware"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/pkg/testutil"
	"hivemtk-user/internal/repository"
	"hivemtk-user/internal/service"
)

type crmRecordPermissionStub struct {
	scope  service.CustomerDataScope
	err    error
	called uint
}

func (s *crmRecordPermissionStub) Resolve(_ context.Context, uid uint) (service.CustomerDataScope, error) {
	s.called = uid
	return s.scope, s.err
}

// 三个客户档案入口均须解析 CRM 授权，不能仅凭客户存在或管理员字符串绕过配对。
func TestCustomerRecordEndpoints_EnforceCRMPermission(t *testing.T) {
	db := testutil.NewTestDB(t, &model.Customer{}, &model.CustomerSession{})
	ctx := tenant.WithTenantID(context.Background(), 7)
	if err := db.WithContext(ctx).Create(&model.Customer{TenantID: 7, ID: "record-crm", UnifiedID: "record-crm", Name: "private-record", SourceAccountID: "12", Industry: "B"}).Error; err != nil {
		t.Fatal(err)
	}
	profile := service.NewCustomer360Service(repository.NewClueRepositoryWithDB(db))
	for _, endpoint := range []string{"detail", "360", "communications"} {
		for _, tc := range []struct {
			name       string
			scope      service.CustomerDataScope
			err        error
			status     int
			role       string
			owner      string
			resolvedBy uint
		}{
			{"denied", service.CustomerDataScope{UserID: 3, RestrictedAccounts: true, AccountIDs: []string{"11"}}, nil, http.StatusForbidden, "admin", "", 3},
			{"allowed", service.CustomerDataScope{UserID: 3, RestrictedAccounts: true, AccountIDs: []string{"12"}}, nil, http.StatusOK, "admin", "", 3},
			{"resolver-error", service.CustomerDataScope{}, errors.New("private database error"), http.StatusInternalServerError, "admin", "", 3},
			{"both-allow", service.CustomerDataScope{UserID: 3, RestrictedAccounts: true, AccountIDs: []string{"12"}}, nil, http.StatusOK, "staff", "3", 3},
			{"owner-denied", service.CustomerDataScope{UserID: 3, RestrictedAccounts: true, AccountIDs: []string{"12"}}, nil, http.StatusForbidden, "staff", "4", 0},
			{"crm-denied-after-owner", service.CustomerDataScope{UserID: 3, RestrictedAccounts: true, AccountIDs: []string{"11"}}, nil, http.StatusForbidden, "staff", "3", 3},
		} {
			t.Run(endpoint+"/"+tc.name, func(t *testing.T) {
				lookupCalled := false
				middleware.SetOwnerLookup(func(_ context.Context, table, idColumn, ownerColumn, idValue string) (string, bool, error) {
					lookupCalled = true
					if table != "customers" || idColumn != "id" || ownerColumn != "owner_agent_id" || idValue != "record-crm" {
						t.Errorf("DataScopeGuard 必须使用原有客户归属登记，实际=%s/%s/%s/%s", table, idColumn, ownerColumn, idValue)
					}
					return tc.owner, true, nil
				})
				t.Cleanup(func() { middleware.SetOwnerLookup(nil) })
				permission := &crmRecordPermissionStub{scope: tc.scope, err: tc.err}
				ctl := &Customer360Controller{customer360Service: profile, permission: permission}
				r := gin.New()
				r.Use(func(c *gin.Context) {
					c.Set("user_id", uint(3))
					c.Set("role", tc.role)
					c.Request = c.Request.WithContext(ctx)
					c.Next()
				})
				r.Use(middleware.DataScopeGuard())
				path := "/api/customer/:id"
				switch endpoint {
				case "detail":
					r.GET(path, ctl.GetCustomerDetail)
				case "360":
					path = "/api/customer/360/:id"
					r.GET(path, ctl.GetCustomer360ByID)
				default:
					path = "/api/customer/:id/communications"
					r.GET(path, ctl.GetCustomerCommunications)
				}
				w := httptest.NewRecorder()
				r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, strings.Replace(path, ":id", "record-crm", 1), nil))
				if w.Code != tc.status || permission.called != tc.resolvedBy || lookupCalled != (tc.role != "admin") {
					t.Fatalf("CRM入口错误 status=%d want=%d uid=%d body=%s", w.Code, tc.status, permission.called, w.Body.String())
				}
				if tc.status != http.StatusOK && (strings.Contains(w.Body.String(), "private-record") || strings.Contains(w.Body.String(), "private database")) {
					t.Fatalf("错误响应泄漏内部信息 %s", w.Body.String())
				}
			})
		}
	}
}
