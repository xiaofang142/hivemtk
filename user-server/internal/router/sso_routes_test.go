package router

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"hivemtk-user/internal/config"
	"hivemtk-user/internal/model"
	dbutil "hivemtk-user/internal/pkg/db"
	"hivemtk-user/internal/pkg/testutil"

	"github.com/gin-gonic/gin"
)

func newSSOTestRouter(t *testing.T, cfg *config.AppConfig) *gin.Engine {
	t.Helper()
	database := testutil.NewTestDB(t, &model.SystemUser{}, &model.SSOIdentity{})
	dbutil.SetTestDB(database)
	t.Cleanup(func() { dbutil.SetTestDB(nil) })

	config.SetAppConfig(cfg)
	t.Cleanup(func() { config.SetAppConfig(nil) })

	gin.SetMode(gin.TestMode)
	r := gin.New()
	public := r.Group("/api")
	setupSSORoutes(public, database)
	return r
}

func doSSORequest(r http.Handler, target string, cookies ...*http.Cookie) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, target, nil)
	for _, c := range cookies {
		req.AddCookie(c)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func ssoConfigWithProvider(name string, pcfg config.SSOProviderConfig) *config.AppConfig {
	return &config.AppConfig{SSO: config.SSOConfig{
		Enabled:   true,
		Providers: map[string]config.SSOProviderConfig{name: pcfg},
	}}
}

// TestSSORoutes_Registered 验证 SSO 路由已注册（与 Setup 全量路由集成）
func TestSSORoutes_Registered(t *testing.T) {
	database := testutil.NewTestDB(t)
	dbutil.SetTestDB(database)
	defer dbutil.SetTestDB(nil)

	gin.SetMode(gin.TestMode)
	r := gin.New()
	Setup(r, database)

	expected := map[string]bool{
		"/api/sso/providers":          false,
		"/api/sso/login/:provider":    false,
		"/api/sso/callback/:provider": false,
	}
	for _, route := range r.Routes() {
		if _, ok := expected[route.Path]; ok {
			expected[route.Path] = true
		}
	}
	for path, found := range expected {
		if !found {
			t.Errorf("Expected SSO route %s to be registered", path)
		}
	}
}

// TestSSORoute_ListProviders_Disabled SSO 关闭时返回 enabled=false 与空 provider 列表
func TestSSORoute_ListProviders_Disabled(t *testing.T) {
	r := newSSOTestRouter(t, &config.AppConfig{SSO: config.SSOConfig{Enabled: false}})
	w := doSSORequest(r, "/api/sso/providers")
	if w.Code != http.StatusOK {
		t.Fatalf("status: got %d want 200", w.Code)
	}

	var body struct {
		// 统一响应信封的成功码是 **int 0**（见 response.Success 的实现注释：
		// "规范: code 用 int 0 (不是 string \"SUCCESS\")，与 CLAUDE.md 架构规范一致"）。
		// 此处原声明为 string 并断言 "SUCCESS"，属陈旧约定，会让该用例恒失败。
		Code    int    `json:"code"`
		Message string `json:"message"`
		Data    struct {
			Enabled   bool  `json:"enabled"`
			Providers []any `json:"providers"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.Code != 0 {
		t.Errorf("code: got %d want 0", body.Code)
	}
	if body.Data.Enabled {
		t.Error("expected enabled=false when SSO disabled")
	}
	if len(body.Data.Providers) != 0 {
		t.Errorf("expected empty providers, got %d", len(body.Data.Providers))
	}
}

// TestSSORoute_ListProviders_Enabled SSO 开启时返回已启用 provider 列表（稳定排序）
func TestSSORoute_ListProviders_Enabled(t *testing.T) {
	r := newSSOTestRouter(t, &config.AppConfig{SSO: config.SSOConfig{
		Enabled: true,
		Providers: map[string]config.SSOProviderConfig{
			"wecom":  {ClientID: "cid-w", Issuer: "https://wecom.example.com"},
			"feishu": {ClientID: "cid-f", Issuer: "https://feishu.example.com"},
		},
	}})
	w := doSSORequest(r, "/api/sso/providers")
	if w.Code != http.StatusOK {
		t.Fatalf("status: got %d want 200", w.Code)
	}

	var body struct {
		Data struct {
			Enabled   bool `json:"enabled"`
			Providers []struct {
				Name        string `json:"name"`
				DisplayName string `json:"display_name"`
				LoginURL    string `json:"login_url"`
			} `json:"providers"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if !body.Data.Enabled {
		t.Error("expected enabled=true when SSO enabled")
	}
	if len(body.Data.Providers) != 2 {
		t.Fatalf("expected 2 providers, got %d", len(body.Data.Providers))
	}
	if body.Data.Providers[0].Name != "feishu" || body.Data.Providers[0].DisplayName != "飞书" {
		t.Errorf("providers[0]: got %+v", body.Data.Providers[0])
	}
	if body.Data.Providers[0].LoginURL != "/api/sso/login/feishu" {
		t.Errorf("providers[0].login_url: got %q", body.Data.Providers[0].LoginURL)
	}
	if body.Data.Providers[1].Name != "wecom" || body.Data.Providers[1].DisplayName != "企业微信" {
		t.Errorf("providers[1]: got %+v", body.Data.Providers[1])
	}
}

// TestSSORoute_Login_UnknownProvider 未知 provider 返回 404
func TestSSORoute_Login_UnknownProvider(t *testing.T) {
	r := newSSOTestRouter(t, &config.AppConfig{SSO: config.SSOConfig{Enabled: true}})
	w := doSSORequest(r, "/api/sso/login/unknown")
	if w.Code != http.StatusNotFound {
		t.Fatalf("status: got %d want 404", w.Code)
	}
}

// TestSSORoute_Login_Disabled SSO 关闭时发起登录返回 403
func TestSSORoute_Login_Disabled(t *testing.T) {
	r := newSSOTestRouter(t, &config.AppConfig{SSO: config.SSOConfig{Enabled: false}})
	w := doSSORequest(r, "/api/sso/login/generic")
	if w.Code != http.StatusForbidden {
		t.Fatalf("status: got %d want 403", w.Code)
	}
}

// TestSSORoute_Login_Redirect 已配置 provider 发起登录 302 到 IdP 授权端点
//
// 使用显式 authorization_endpoint，全程无网络请求。
func TestSSORoute_Login_Redirect(t *testing.T) {
	cfg := ssoConfigWithProvider("generic", config.SSOProviderConfig{
		Issuer:                "https://idp.example.com",
		ClientID:              "client-1",
		RedirectURL:           "https://hivemtk.example.com/api/sso/callback/generic",
		AuthorizationEndpoint: "https://idp.example.com/oauth2/authorize",
	})
	r := newSSOTestRouter(t, cfg)
	w := doSSORequest(r, "/api/sso/login/generic")
	if w.Code != http.StatusFound {
		t.Fatalf("status: got %d want 302", w.Code)
	}

	loc := w.Header().Get("Location")
	if loc == "" {
		t.Fatal("expected Location header on redirect")
	}
	u, err := url.Parse(loc)
	if err != nil {
		t.Fatalf("parse location %q: %v", loc, err)
	}
	if u.Scheme != "https" || u.Host != "idp.example.com" {
		t.Errorf("auth url host: got %s://%s", u.Scheme, u.Host)
	}
	q := u.Query()
	if q.Get("response_type") != "code" {
		t.Errorf("response_type: got %q", q.Get("response_type"))
	}
	if q.Get("client_id") != "client-1" {
		t.Errorf("client_id: got %q", q.Get("client_id"))
	}
	if q.Get("redirect_uri") != "https://hivemtk.example.com/api/sso/callback/generic" {
		t.Errorf("redirect_uri: got %q", q.Get("redirect_uri"))
	}
	if q.Get("scope") == "" {
		t.Error("expected scope parameter")
	}
	if q.Get("state") == "" {
		t.Error("expected state parameter (anti-CSRF)")
	}
	if q.Get("nonce") == "" {
		t.Error("expected nonce parameter")
	}
	if q.Get("code_challenge") == "" {
		t.Error("expected code_challenge parameter (PKCE)")
	}
	if q.Get("code_challenge_method") != "S256" {
		t.Errorf("code_challenge_method: got %q want S256", q.Get("code_challenge_method"))
	}

	state := q.Get("state")
	cookies := w.Result().Cookies()
	stateCookie := ""
	for _, c := range cookies {
		if c.Name == "sso_state" {
			stateCookie = c.Value
		}
	}
	if stateCookie == "" {
		t.Error("expected sso_state cookie to be set")
	} else if stateCookie != state {
		t.Errorf("sso_state cookie %q != state %q", stateCookie, state)
	}
}

// TestSSORoute_Callback_MissingState 回调缺 state 参数 → 400
func TestSSORoute_Callback_MissingState(t *testing.T) {
	r := newSSOTestRouter(t, ssoConfigWithProvider("generic", config.SSOProviderConfig{
		ClientID: "c", Issuer: "https://idp.example.com",
	}))
	w := doSSORequest(r, "/api/sso/callback/generic?code=abc")
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status: got %d want 400", w.Code)
	}
}

// TestSSORoute_Callback_NoStateCookie 带 state 参数但无对应 cookie → 400（防 CSRF）
func TestSSORoute_Callback_NoStateCookie(t *testing.T) {
	r := newSSOTestRouter(t, ssoConfigWithProvider("generic", config.SSOProviderConfig{
		ClientID: "c", Issuer: "https://idp.example.com",
	}))
	w := doSSORequest(r, "/api/sso/callback/generic?state=abc&code=xyz")
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status: got %d want 400", w.Code)
	}
}

// TestSSORoute_Callback_StateMismatch cookie state 与 query state 不一致 → 400
func TestSSORoute_Callback_StateMismatch(t *testing.T) {
	r := newSSOTestRouter(t, ssoConfigWithProvider("generic", config.SSOProviderConfig{
		ClientID: "c", Issuer: "https://idp.example.com",
	}))
	cookies := []*http.Cookie{
		{Name: "sso_state", Value: "expected", Path: "/"},
	}
	w := doSSORequest(r, "/api/sso/callback/generic?state=other&code=xyz", cookies...)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status: got %d want 400", w.Code)
	}
}

// TestSSORoute_Callback_MissingCode state 校验通过但缺 code → 400（语义错误）
func TestSSORoute_Callback_MissingCode(t *testing.T) {
	r := newSSOTestRouter(t, ssoConfigWithProvider("generic", config.SSOProviderConfig{
		ClientID: "c", Issuer: "https://idp.example.com",
	}))
	cookies := []*http.Cookie{
		{Name: "sso_state", Value: "expected", Path: "/"},
	}
	w := doSSORequest(r, "/api/sso/callback/generic?state=expected", cookies...)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status: got %d want 400", w.Code)
	}
}
