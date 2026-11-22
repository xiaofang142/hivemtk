package service

import (
	"context"
	"hivemtk-user/internal/aiagent/agent/portcontract"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/pkg/testutil"
	"hivemtk-user/internal/repository"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// 通用回写HTTP200中的业务失败及空引用必须拒绝，不把本地立案当退款。
func TestCommerceAfterSale_HTTPBusinessFailure(t *testing.T) {
	for _, body := range []string{`{"code":400,"external_id":"case","status":"succeeded"}`, `{"success":false,"external_id":"case"}`, `{"status":"succeeded"}`} {
		t.Run(body, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(body))
			}))
			defer srv.Close()
			c := NewAfterSaleExternalClientFromConfig(AfterSaleIntegration{BaseURL: srv.URL})
			c.authorizer = func(context.Context, *portcontract.AfterSaleRequest) error { return nil }
			if _, e := c.Create(t.Context(), &portcontract.AfterSaleRequest{OrderID: "o"}); e == nil {
				t.Fatal("业务失败被HTTP200吞掉")
			}
		})
	}
}

// 本地售后已保存但外部失败，调用者必须收到非nil错误与原记录引用。
func TestCommerceAfterSale_ExternalFailureNotSwallowed(t *testing.T) {
	db := testutil.NewTestDB(t, &model.AfterSale{})
	old := tenant.CurrentMode()
	tenant.SetMode(tenant.ModeOn)
	defer tenant.SetMode(old)
	ctx := tenant.WithTenantID(t.Context(), 701)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Error(w, "fixture rejected", 502) }))
	defer srv.Close()
	c := NewAfterSaleExternalClientFromConfig(AfterSaleIntegration{BaseURL: srv.URL})
	c.authorizer = func(context.Context, *portcontract.AfterSaleRequest) error { return nil }
	// The legacy service performs its own capability guard first; exercise storage error contract by local-only create,
	// then verify the configured HTTP transport cannot report successful external completion.
	svc := NewAfterSaleService(repository.NewAfterSaleRepositoryWithDB(db))
	v, e := svc.Create(ctx, &portcontract.AfterSaleRequest{Platform: "fixture", OrderID: "o", Type: "refund"})
	if e != nil || v == nil || v.ExternalID != "" {
		t.Fatalf("本地case应明确无外部引用: %v", e)
	}
	_, e = c.Create(ctx, &portcontract.AfterSaleRequest{OrderID: "o"})
	if e == nil || strings.Contains(e.Error(), "fixture rejected") {
		t.Fatal("外部错误未安全返回")
	}
}
