package controller

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/repository"
	"hivemtk-user/internal/service"
)

type fakeCrossAccountSearchService struct {
	rows    []repository.CrossAccountSearchRow
	total   int64
	scope   service.CrossAccountSearchScope
	err     error
	gotTID  uint
	gotUID  uint
	gotKw   string
	gotInd  string
	gotPage int
	gotSize int
}

func (f *fakeCrossAccountSearchService) SearchCustomers(_ context.Context, tid uint, userID uint, keyword, industry string, page, pageSize int) ([]repository.CrossAccountSearchRow, int64, service.CrossAccountSearchScope, error) {
	f.gotTID = tid
	f.gotUID = userID
	f.gotKw = keyword
	f.gotInd = industry
	f.gotPage = page
	f.gotSize = pageSize
	if f.err != nil {
		return nil, 0, service.CrossAccountSearchScope{}, f.err
	}
	return f.rows, f.total, f.scope, nil
}

func TestCrossAccountSearchController_SearchCustomers_Success(t *testing.T) {
	gin.SetMode(gin.TestMode)
	fakeSvc := &fakeCrossAccountSearchService{
		rows:  []repository.CrossAccountSearchRow{{ID: "c1", Name: "Alice", Phone: "138****8000", Email: "a***@example.com", SourceChannel: "wechat", SourceAccountID: "wx-1"}},
		total: 1,
		scope: service.CrossAccountSearchScope{AllAccounts: false, Restricted: true, AuthorizedAccountCount: 2},
	}
	ctrl := NewCrossAccountSearchControllerWithService(fakeSvc)
	var audited bool
	ctrl.audit = func(_ *gin.Context, tid, uid uint, uname, keyword, industry string, page, pageSize int, total int64, scope service.CrossAccountSearchScope) {
		audited = true
		if tid != 100 || uid != 7 || uname != "u7" || keyword != "alice" || industry != "retail" || page != 2 || pageSize != 10 || total != 1 {
			t.Fatalf("unexpected audit args tid=%d uid=%d uname=%q kw=%q ind=%q page=%d size=%d total=%d", tid, uid, uname, keyword, industry, page, pageSize, total)
		}
		if !scope.Restricted || scope.AllAccounts || scope.AuthorizedAccountCount != 2 {
			t.Fatalf("unexpected audit scope %+v", scope)
		}
	}

	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Request = c.Request.WithContext(tenant.WithTenantID(c.Request.Context(), 100))
		c.Set("user_id", uint(7))
		c.Set("username", "u7")
		c.Next()
	})
	ctrl.RegisterRoutes(r.Group("/api"))
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/cross-account-search/customers?keyword=alice&industry=retail&page=2&page_size=10", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	var resp struct {
		Code int `json:"code"`
		Data struct {
			List  []repository.CrossAccountSearchRow `json:"list"`
			Total int64                             `json:"total"`
			Scope service.CrossAccountSearchScope   `json:"scope"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v body=%s", err, w.Body.String())
	}
	if resp.Code != 0 || resp.Data.Total != 1 || len(resp.Data.List) != 1 {
		t.Fatalf("unexpected resp: %+v", resp)
	}
	if !resp.Data.Scope.Restricted || resp.Data.Scope.AllAccounts || resp.Data.Scope.AuthorizedAccountCount != 2 {
		t.Fatalf("unexpected scope: %+v", resp.Data.Scope)
	}
	if !audited {
		t.Fatalf("expected audit to be called")
	}
	if fakeSvc.gotTID != 100 || fakeSvc.gotUID != 7 || fakeSvc.gotKw != "alice" || fakeSvc.gotInd != "retail" || fakeSvc.gotPage != 2 || fakeSvc.gotSize != 10 {
		t.Fatalf("unexpected svc args: %+v", fakeSvc)
	}
}

func TestCrossAccountSearchController_SearchCustomers_MissingTenant(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctrl := NewCrossAccountSearchControllerWithService(&fakeCrossAccountSearchService{})
	ctrl.audit = func(*gin.Context, uint, uint, string, string, string, int, int, int64, service.CrossAccountSearchScope) {}

	r := gin.New()
	ctrl.RegisterRoutes(r.Group("/api"))
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/cross-account-search/customers?keyword=x", nil)
	r.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d body=%s", w.Code, w.Body.String())
	}
}
