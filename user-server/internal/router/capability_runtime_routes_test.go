package router

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"testing"
	"time"

	"hivemtk-user/internal/capregistry"
	"hivemtk-user/internal/middleware"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/pkg/testutil"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

const capabilityResolveURL = "/api/v1/capabilities/resolve"
const capabilityCellsURL = "/api/v1/capabilities/cells"

func capabilityRuntimeEngine(role string, evidence *capregistry.CellEvidence, db *gorm.DB) *gin.Engine {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	auth := engine.Group("/api", func(c *gin.Context) {
		// 模拟 JWT 写入身份，继续走实际租管守卫和路由装配。
		if role != "" {
			c.Set("role", role)
			ctx := tenant.WithTenantID(c.Request.Context(), 71)
			if evidence != nil {
				ctx = capregistry.WithCellEvidence(ctx, *evidence)
			}
			c.Request = c.Request.WithContext(ctx)
		}
		c.Next()
	})
	setupCapabilityRuntimeRoutes(auth, auth.Group("", middleware.AdminAuthMiddleware()), db)
	return engine
}

func capabilityRequest(t *testing.T, engine *gin.Engine, path string, wantStatus int) json.RawMessage {
	t.Helper()
	w := httptest.NewRecorder()
	engine.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
	if w.Code != wantStatus {
		t.Fatalf("%s 预期 %d，实际 %d：%s", path, wantStatus, w.Code, w.Body.String())
	}
	var body struct{ Data json.RawMessage }
	if wantStatus == http.StatusOK {
		if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
	}
	return body.Data
}

func capabilityCellURL(product, action, carrier string) string {
	q := url.Values{"shop_id": {"merchant-123"}, "product": {product}, "region": {"CN"}, "action": {action}, "carrier": {carrier},
		"tenant_id": {"98"}, "production_allowed": {"true"}, "version": {"ignored-query"}}
	return capabilityResolveURL + "?" + q.Encode()
}

// 精确格选项和解析走租管守卫，普通用户仍可读原诊断视图。
func TestCapabilityAdminRoutes_GuardAndRegisteredOptions(t *testing.T) {
	resolve := capabilityCellURL("douyin_shop", "order.address.change", "api_adapter")
	for _, tc := range []struct {
		role string
		want int
	}{{"", 401}, {"agent", 403}, {"admin", 200}} {
		engine := capabilityRuntimeEngine(tc.role, nil, nil)
		capabilityRequest(t, engine, resolve, tc.want)
		data := capabilityRequest(t, engine, capabilityCellsURL, tc.want)
		if tc.role == "admin" {
			var options []struct{ Product, Region, Action, Carrier string }
			if err := json.Unmarshal(data, &options); err != nil || len(options) != len(capregistry.CellDefinitions()) {
				t.Fatalf("选项应只来自完整注册表：%s err=%v", data, err)
			}
			for _, option := range options {
				if _, ok := capregistry.FindCellDefinition(option.Product, option.Region, option.Action, option.Carrier); !ok {
					t.Fatalf("未登记选项：%+v", option)
				}
			}
			capabilityRequest(t, engine, capabilityCellURL("douyin", "order.address.change", "api_adapter"), 400)
		}
	}
	db := testutil.NewTestDB(t)
	data := capabilityRequest(t, capabilityRuntimeEngine("agent", nil, db), "/api/capabilities", 200)
	if strings.Contains(string(data), "required_scopes") || strings.Contains(string(data), "shop_id") {
		t.Fatalf("普通诊断泄漏租管摘要：%s", data)
	}
}

// 只返回白名单字段。外租户证据不显示，模拟证据只提供观察信息。
func TestCapabilityAdminRoutes_ExactSafeProjection(t *testing.T) {
	url := capabilityCellURL("douyin_shop", "order.address.change", "api_adapter")
	expires := time.Now().Add(time.Hour)
	for _, tc := range []struct {
		name, state, reason, next        string
		owner                            string
		tenantID                         uint
		revoked, expired, observed, noDB bool
	}{
		{name: "存储不可用", state: "unconfigured", reason: "capability_storage_unavailable", next: "verify_live_evidence", noDB: true},
		{name: "缺证据", state: "unconfigured", reason: "shop_evidence_missing", next: "configure_connection"},
		{name: "外租户", state: "unconfigured", reason: "shop_evidence_missing", next: "configure_connection", tenantID: 98},
		{name: "撤权", state: "reauth", reason: "permission_revoked", next: "reconnect", tenantID: 71, revoked: true},
		{name: "模拟", state: "ready", reason: "state_preflight_required", owner: "merchant_operator", next: "review_action_state", tenantID: 71, observed: true},
		{name: "过期", state: "reauth", reason: "evidence_expired", next: "reconnect", tenantID: 71, expired: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var db *gorm.DB
			if !tc.noDB {
				db = testutil.NewTestDB(t, &model.MerchantConnection{}, &model.CapabilityEvidence{})
			}
			if tc.tenantID != 0 {
				exp := expires
				if tc.expired {
					exp = time.Now().Add(-time.Hour)
				}
				conn := model.MerchantConnection{TenantID: tc.tenantID, Provider: "douyin_shop", ShopID: "merchant-123", Version: 1, CredentialVersion: 1, ConsentVersion: 1, CredentialContract: "local-only", CredentialCiphertext: "never-return", Scopes: []string{"order.address.change"}, ExpiresAt: &exp, Revoked: tc.revoked}
				if err := db.Create(&conn).Error; err != nil {
					t.Fatal(err)
				}
				e := model.CapabilityEvidence{TenantID: tc.tenantID, ShopID: conn.ShopID, Product: conn.Provider, Region: "CN", Action: "order.address.change", Carrier: "api_adapter", ConnectionID: conn.ID, ConnectionVersion: 1, CredentialVersion: 1, ConsentVersion: 1, ContractVersion: "api-v1", DocumentationVersion: "design-cell-v1", ExpiresAt: exp, Scopes: conn.Scopes, Documentation: true, Implementation: true, Simulated: true}
				if err := db.Create(&e).Error; err != nil {
					t.Fatal(err)
				}
			}
			data := capabilityRequest(t, capabilityRuntimeEngine("admin", nil, db), url, 200)
			var got map[string]any
			if err := json.Unmarshal(data, &got); err != nil {
				t.Fatal(err)
			}
			deadline, _ := got["valid_until"].(string)
			validUntil, err := time.Parse(time.RFC3339Nano, deadline)
			if err != nil || !validUntil.After(time.Now()) || validUntil.After(time.Now().Add(5*time.Minute)) {
				t.Fatalf("摘要有效期无效：%q (%v)", deadline, err)
			}
			owner := tc.owner
			if owner == "" {
				owner = "tenant_admin"
			}
			want := map[string]any{
				"tenant_id": float64(71), "shop_id": "merchant-123", "product": "douyin_shop", "region": "CN", "action": "order.address.change", "carrier": "api_adapter",
				"execution_mode": "api_adapter", "connection_state": tc.state, "status": "blocked", "production_allowed": false,
				"required_scopes": []any{"order.address.change"}, "contract_version": "api-v1", "documentation_version": "design-cell-v1",
				"state_limits": []any{"platform_preflight_required"}, "reason_code": tc.reason, "owner": owner, "next_step": tc.next,
				"valid_until": deadline,
			}
			if tc.state != "unconfigured" {
				want["observed_scopes"] = []any{"order.address.change"}
			}
			if tc.observed {
				want["observed_version"] = "api-v1"
				want["observed_documentation_version"] = "design-cell-v1"
			}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("解析摘要与白名单不符：得到 %s，预期 %+v", data, want)
			}
		})
	}
}

// 工作台与平台托管是责任信息，不能通过精确格授予自动提交。
func TestCapabilityAdminRoutes_ExternalActionResponsibilities(t *testing.T) {
	for _, tc := range []struct{ product, action, mode, owner, next string }{
		{"jd_pop", "aftersale.write", "official_ui", "merchant_operator", "use_official_workbench"},
		{"vipshop", "refund.observe", "platform_managed", "platform", "observe_platform_result"},
	} {
		cell, found := capregistry.FindCellDefinition(tc.product, "CN", tc.action, tc.mode)
		if !found {
			t.Fatalf("职责格未登记：%+v", tc)
		}
		expires := time.Now().Add(time.Minute)
		evidence := &capregistry.CellEvidence{
			TenantID: 71, ShopID: "merchant-123", Product: tc.product, Region: "CN", Action: tc.action, Carrier: tc.mode,
			ConnectionState: capregistry.ConnectionReady, Version: cell.ContractVersion, DocumentationVersion: cell.DocumentationVersion,
			ExpiresAt: &expires, StatePreflightPassed: true,
			Evidence: []capregistry.EvidenceRecord{{Kind: capregistry.EvidenceDocumentation}, {Kind: capregistry.EvidenceImplementation}, {Kind: capregistry.EvidenceRuntime}},
		}
		for _, fixture := range []*capregistry.CellEvidence{nil, evidence} {
			engine := capabilityRuntimeEngine("admin", fixture, nil)
			data := capabilityRequest(t, engine, capabilityCellURL(tc.product, tc.action, tc.mode), 200)
			var got map[string]any
			if err := json.Unmarshal(data, &got); err != nil {
				t.Fatal(err)
			}
			if got["execution_mode"] != tc.mode || got["owner"] != tc.owner || got["next_step"] != tc.next || got["status"] != "blocked" || got["production_allowed"] != false {
				t.Fatalf("责任或裁决与登记模式不符：%s", data)
			}
			if _, found := got["observed_version"]; found {
				t.Fatalf("未持久装载的观察版本不得显示：%s", data)
			}
		}
	}
}
