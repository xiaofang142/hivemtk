package router

import (
	"bytes"
	"encoding/json"
	"github.com/gin-gonic/gin"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/repository"
	"hivemtk-user/internal/service"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// 真实HTTP装配验证profile契约、缺省provider和严格DTO，响应绝不包含凭证。
func TestCommerceRoutes_FailClosedContract(t *testing.T) {
	gin.SetMode(gin.TestMode)
	old := tenant.CurrentMode()
	tenant.SetMode(tenant.ModeOn)
	defer tenant.SetMode(old)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set("user_id", uint(42))
		c.Request = c.Request.WithContext(tenant.WithTenantID(c.Request.Context(), 701))
		c.Next()
	})
	mountCommerceRoutes(r.Group("/api"), service.NewCommerceService(repository.NewCommerceRepository(nil), nil))
	cases := []struct {
		method, path, body string
		want               int
	}{
		{"GET", "/api/v1/commerce/profiles", "", 200},
		{"POST", "/api/v1/commerce/operations", `{"profile":"douyin_shop_cn","shop_id":"s","order_id":1,"action":"address.change","intent_id":"i","revision":1,"tenant_id":701}`, 400},
		{"POST", "/api/v1/commerce/operations/id/submit", `{"expected_version":1,"credential":"secret"}`, 400},
		{"POST", "/api/v1/commerce/operations/id/observe", `{"expected_version":1}`, 503},
		{"POST", "/api/v1/commerce/operations/id/evidence", `{"expected_version":1,"outcome":"completed","reference":"screenshot"}`, 503},
		{"POST", "/api/v1/commerce/operations/id/review", `{"expected_version":1,"evidence_id":"e"}`, 503},
		{"GET", "/api/v1/commerce/orders?profile=douyin_shop_cn&shop_id=s&order_id=bad", "", 400},
		{"PUT", "/api/v1/commerce/policy", `{"profile":"douyin_shop_cn","shop_id":"s","owner_id":42,"write_enabled":true,"write_owner":"native","approval_mode":"single_owner","recovery_epoch":1}`, 503},
	}
	for _, tc := range cases {
		t.Run(tc.method+tc.path, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, tc.path, bytes.NewBufferString(tc.body))
			if tc.body != "" {
				req.Header.Set("Content-Type", "application/json")
			}
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)
			if w.Code != tc.want {
				t.Fatalf("HTTP状态期望%d实际%d: %s", tc.want, w.Code, w.Body.String())
			}
			if w.Header().Get("Cache-Control") != "no-store" {
				t.Fatal("缺少敏感接口禁止缓存")
			}
			if strings.Contains(w.Body.String(), "ciphertext") {
				t.Fatal("响应泄漏凭证")
			}
			if tc.path == "/api/v1/commerce/profiles" {
				var envelope struct {
					Code int `json:"code"`
					Data struct {
						Total int `json:"total"`
						List  []struct {
							Mode, LiveStatus   string
							ProviderConfigured bool `json:"provider_configured"`
						}
					} `json:"data"`
				}
				if e := json.Unmarshal(w.Body.Bytes(), &envelope); e != nil {
					t.Fatal(e)
				}
				if envelope.Code != 0 || envelope.Data.Total != 29 {
					t.Fatal("profile契约错误")
				}
				for _, p := range envelope.Data.List {
					if p.ProviderConfigured {
						t.Fatal("缺省provider错误启用")
					}
				}
			}
		})
	}
	_ = http.MethodGet
}
