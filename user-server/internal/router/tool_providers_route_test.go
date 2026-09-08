package router

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"hivemtk-user/internal/aiagent/agent/tooluse"
	"hivemtk-user/internal/app"

	"github.com/gin-gonic/gin"
)

type providersRouteMockProvider struct {
	name  string
	tools []tooluse.Tool
}

func (p *providersRouteMockProvider) Name() string                   { return p.name }
func (p *providersRouteMockProvider) Category() tooluse.ToolCategory { return tooluse.CategoryBusiness }
func (p *providersRouteMockProvider) Description() string            { return "mock provider for route testing" }
func (p *providersRouteMockProvider) Provide(ctx tooluse.ProviderContext) ([]tooluse.Tool, error) {
	return p.tools, nil
}

func TestSetup_ToolProvidersRouteRegistered(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	auth := r.Group("/api")
	setupToolDebugRoutes(auth)

	routes := r.Routes()
	routeSet := make(map[string]bool)
	for _, route := range routes {
		routeSet[route.Method+"-"+route.Path] = true
	}
	if !routeSet["GET-/api/agent/tools/providers"] {
		t.Error("route GET-/api/agent/tools/providers not registered")
	}
}

func TestHandleToolProviders_HTTP_WithoutInit(t *testing.T) {
	gin.SetMode(gin.TestMode)
	orig := app.GetGlobalProviderRegistry()
	app.SetGlobalProviderRegistryForTest(nil)
	defer app.SetGlobalProviderRegistryForTest(orig)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("GET", "/api/agent/tools/providers", nil)

	handleToolProviders(c)

	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503", w.Code)
	}
	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("body not JSON: %v", err)
	}
	// 信封已归一为 {code, message}（code!=0 表示失败），不再有 success 布尔
	if code, ok := resp["code"].(string); !ok || code == "SUCCESS" {
		t.Errorf("code should be a non-SUCCESS error code, got %v", resp["code"])
	}
}

func TestHandleToolProviders_HTTP_WithProviderRegistry(t *testing.T) {
	gin.SetMode(gin.TestMode)

	providerRegistry := tooluse.NewProviderRegistry()
	_ = providerRegistry.RegisterProvider(&providersRouteMockProvider{
		name:  "testp",
		tools: []tooluse.Tool{newMockEchoTool("testp.toola"), newMockEchoTool("testp.toolb")},
	})
	_, _ = providerRegistry.RegisterAll(
		tooluse.ProviderContext{Config: tooluse.ProviderConfig{Enabled: true}},
		tooluse.NewToolRegistry(),
	)

	orig := app.GetGlobalProviderRegistry()
	app.SetGlobalProviderRegistryForTest(providerRegistry)
	defer app.SetGlobalProviderRegistryForTest(orig)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("GET", "/api/agent/tools/providers", nil)

	handleToolProviders(c)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}
	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("body not JSON: %v; body=%s", err, w.Body.String())
	}

	if resp["code"] != float64(0) {
		t.Errorf("code should be 0; body=%s", w.Body.String())
	}
	data, _ := resp["data"].(map[string]any)
	if data == nil {
		t.Fatalf("missing data; body=%s", w.Body.String())
	}
	if int(data["total_providers"].(float64)) != 1 {
		t.Errorf("total_providers = %v, want 1", data["total_providers"])
	}
	results := data["results"].([]any)
	if len(results) != 1 {
		t.Fatalf("results len = %d, want 1", len(results))
	}
	first := results[0].(map[string]any)
	if first["provider_name"] != "testp" {
		t.Errorf("provider_name = %v, want testp", first["provider_name"])
	}
}
