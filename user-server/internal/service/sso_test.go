package service

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"io"
	"math/big"
	"net/http"
	"strings"
	"testing"
	"time"

	"hivemtk-user/internal/config"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/db"
	"hivemtk-user/internal/pkg/testutil"
	"hivemtk-user/internal/repository"

	"github.com/golang-jwt/jwt/v5"
	"gorm.io/gorm"
)

const testOIDCIssuer = "https://test-idp.example.com"

type mockOIDCTransport struct {
	priv          *rsa.PrivateKey
	tokenIssuer   string
	sub           string
	email         string
	username      string
	name          string
	roles         []string
	extra         map[string]interface{}
	exchangeError string
}

func (m *mockOIDCTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	iss := m.tokenIssuer
	if iss == "" {
		iss = testOIDCIssuer
	}
	switch req.URL.Path {
	case "/.well-known/openid-configuration":
		return m.jsonResponse(200, map[string]string{
			"issuer":                 iss,
			"authorization_endpoint": iss + "/oauth2/authorize",
			"token_endpoint":         iss + "/oauth2/token",
			"userinfo_endpoint":      iss + "/oauth2/userinfo",
			"jwks_uri":               iss + "/.well-known/jwks.json",
		})
	case "/.well-known/jwks.json":
		n := base64.RawURLEncoding.EncodeToString(m.priv.N.Bytes())
		e := base64.RawURLEncoding.EncodeToString(big.NewInt(int64(m.priv.E)).Bytes())
		return m.jsonResponse(200, map[string]any{
			"keys": []map[string]string{
				{"kty": "RSA", "use": "sig", "alg": "RS256", "kid": "test-kid-1", "n": n, "e": e},
			},
		})
	case "/oauth2/token":
		if m.exchangeError != "" {
			return m.jsonResponse(400, map[string]string{"error": m.exchangeError})
		}
		claims := jwt.MapClaims{
			"iss":                iss,
			"sub":                m.sub,
			"aud":                "test-client",
			"iat":                time.Now().Unix(),
			"exp":                time.Now().Add(time.Hour).Unix(),
			"email":              m.email,
			"email_verified":     true,
			"preferred_username": m.username,
			"name":               m.name,
			"roles":              m.roles,
		}
		for k, v := range m.extra {
			claims[k] = v
		}
		tok := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
		tok.Header["kid"] = "test-kid-1"
		signed, err := tok.SignedString(m.priv)
		if err != nil {
			return m.jsonResponse(500, map[string]string{"error": err.Error()})
		}
		return m.jsonResponse(200, map[string]any{
			"access_token": "test-access",
			"id_token":     signed,
			"token_type":   "Bearer",
			"expires_in":   3600,
		})
	default:
		return m.jsonResponse(404, map[string]string{"error": "not found: " + req.URL.Path})
	}
}

func (m *mockOIDCTransport) jsonResponse(status int, body any) (*http.Response, error) {
	b, _ := json.Marshal(body)
	return &http.Response{
		StatusCode: status,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       &respBody{data: b},
		Request:    &http.Request{},
	}, nil
}

type respBody struct {
	data []byte
	off  int
}

func (b *respBody) Read(p []byte) (int, error) {
	if b.off >= len(b.data) {
		return 0, io.EOF
	}
	n := copy(p, b.data[b.off:])
	b.off += n
	return n, nil
}
func (b *respBody) Close() error { return nil }

func setupSSOServiceTest(t *testing.T, database *gorm.DB) *mockOIDCTransport {
	t.Helper()
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("gen rsa: %v", err)
	}
	return &mockOIDCTransport{
		priv:     priv,
		sub:      "user-123",
		email:    "alice@example.com",
		username: "alice",
		name:     "Alice",
		roles:    []string{"user"},
	}
}

func ssoTestConfig(autoProvision bool) config.SSOConfig {
	return config.SSOConfig{
		Enabled: true,
		Providers: map[string]config.SSOProviderConfig{
			"generic": {
				Issuer:        testOIDCIssuer,
				ClientID:      "test-client",
				ClientSecret:  "test-secret",
				RedirectURL:   "https://hivemtk.example.com/api/sso/callback/generic",
				AutoProvision: autoProvision,
				DefaultRole:   "user",
				Scopes:        []string{"openid", "profile", "email"},
			},
		},
	}
}

func newTestSSOService(t *testing.T, database *gorm.DB, cfg config.SSOConfig, mt *mockOIDCTransport) *SSOService {
	t.Helper()
	svc := NewSSOServiceWithRepos(
		cfg,
		repository.NewSystemUserRepository(),
		repository.NewSSOIdentityRepository(database),
	)
	svc.SetProviderHTTPClient(&http.Client{Transport: mt})
	return svc
}

func TestNewSSOService_SkipsProvidersWithoutClientID(t *testing.T) {
	database := testutil.NewTestDB(t,
		&model.SystemUser{},
		&model.SSOIdentity{},
	)
	db.SetTestDB(database)
	defer db.SetTestDB(nil)

	cfg := config.SSOConfig{
		Enabled: true,
		Providers: map[string]config.SSOProviderConfig{
			"feishu":   {Issuer: "https://x", ClientID: ""},
			"dingtalk": {Issuer: "https://x", ClientID: "cid"},
		},
	}
	svc := NewSSOServiceWithRepos(cfg, repository.NewSystemUserRepository(), repository.NewSSOIdentityRepository(database))
	// 无 KV 的服务实例 + 未绑定租户的 ctx = 平台 config.yaml 那一档，
	// 这三条断言量的就是"租户化之后平台档还在"。
	bg := context.Background()
	if _, ok := svc.AdapterFor(bg, "feishu"); ok {
		t.Error("provider without client_id should not be enabled")
	}
	if _, ok := svc.AdapterFor(bg, "dingtalk"); !ok {
		t.Error("provider with client_id should be enabled")
	}
}

func TestSSOService_EnabledFlag(t *testing.T) {
	database := testutil.NewTestDB(t)
	db.SetTestDB(database)
	defer db.SetTestDB(nil)

	off := NewSSOServiceWithRepos(config.SSOConfig{Enabled: false}, repository.NewSystemUserRepository(), repository.NewSSOIdentityRepository(database))
	if off.EnabledFor(context.Background()) {
		t.Error("Expected EnabledFor() == false")
	}
	on := NewSSOServiceWithRepos(config.SSOConfig{Enabled: true}, repository.NewSystemUserRepository(), repository.NewSSOIdentityRepository(database))
	if !on.EnabledFor(context.Background()) {
		t.Error("Expected EnabledFor() == true")
	}
}

func TestSSOService_HandleCallback_AutoProvision(t *testing.T) {
	database := testutil.NewTestDB(t,
		&model.SystemUser{},
		&model.SSOIdentity{},
	)
	db.SetTestDB(database)
	defer db.SetTestDB(nil)

	mt := setupSSOServiceTest(t, database)
	svc := newTestSSOService(t, database, ssoTestConfig(true), mt)

	result, err := svc.HandleCallback(context.Background(), "generic", "test-code", "test-verifier")
	if err != nil {
		t.Fatalf("HandleCallback: %v", err)
	}
	if result.Token == "" {
		t.Fatal("expected JWT token")
	}
	if !result.IsNewUser {
		t.Error("expected is_new_user == true for first login")
	}
	if result.User == nil || result.User.Username != "alice" {
		t.Errorf("expected username alice, got %+v", result.User)
	}

	var users []model.SystemUser
	if err := database.Where("username = ?", "alice").Find(&users).Error; err != nil || len(users) != 1 {
		t.Fatalf("expected 1 provisioned user, got %d (err=%v)", len(users), err)
	}
	if users[0].Role != "user" {
		t.Errorf("role: got %q", users[0].Role)
	}
	if users[0].Password == "" || users[0].Password == "alice" {
		t.Error("SSO user should have a hashed random password")
	}

	var identities []model.SSOIdentity
	if err := database.Where("provider = ? AND subject = ?", "generic", "user-123").Find(&identities).Error; err != nil || len(identities) != 1 {
		t.Fatalf("expected 1 sso identity, got %d (err=%v)", len(identities), err)
	}
}

func TestSSOService_HandleCallback_ExistingIdentityReuse(t *testing.T) {
	database := testutil.NewTestDB(t,
		&model.SystemUser{},
		&model.SSOIdentity{},
	)
	db.SetTestDB(database)
	defer db.SetTestDB(nil)

	mt := setupSSOServiceTest(t, database)
	svc := newTestSSOService(t, database, ssoTestConfig(true), mt)

	if _, err := svc.HandleCallback(context.Background(), "generic", "test-code", "v1"); err != nil {
		t.Fatalf("first login: %v", err)
	}

	result, err := svc.HandleCallback(context.Background(), "generic", "test-code", "v2")
	if err != nil {
		t.Fatalf("second login: %v", err)
	}
	if result.IsNewUser {
		t.Error("expected is_new_user == false on second login")
	}

	var users []model.SystemUser
	database.Where("username = ?", "alice").Find(&users)
	if len(users) != 1 {
		t.Errorf("expected exactly 1 user, got %d", len(users))
	}
	var identities []model.SSOIdentity
	database.Where("provider = ? AND subject = ?", "generic", "user-123").Find(&identities)
	if len(identities) != 1 {
		t.Errorf("expected exactly 1 identity, got %d", len(identities))
	}
}

func TestSSOService_HandleCallback_NotEnabled(t *testing.T) {
	database := testutil.NewTestDB(t)
	db.SetTestDB(database)
	defer db.SetTestDB(nil)

	svc := NewSSOServiceWithRepos(config.SSOConfig{Enabled: false}, repository.NewSystemUserRepository(), repository.NewSSOIdentityRepository(database))
	if _, err := svc.HandleCallback(context.Background(), "generic", "code", "v"); err != ErrSSONotEnabled {
		t.Errorf("expected ErrSSONotEnabled, got %v", err)
	}
}

func TestSSOService_HandleCallback_ProviderNotFound(t *testing.T) {
	database := testutil.NewTestDB(t)
	db.SetTestDB(database)
	defer db.SetTestDB(nil)

	svc := NewSSOServiceWithRepos(ssoTestConfig(true), repository.NewSystemUserRepository(), repository.NewSSOIdentityRepository(database))
	if _, err := svc.HandleCallback(context.Background(), "unknown", "code", "v"); err != ErrSSOProviderNotFound {
		t.Errorf("expected ErrSSOProviderNotFound, got %v", err)
	}
}

func TestSSOService_HandleCallback_MissingCode(t *testing.T) {
	database := testutil.NewTestDB(t)
	db.SetTestDB(database)
	defer db.SetTestDB(nil)

	mt := setupSSOServiceTest(t, database)
	svc := newTestSSOService(t, database, ssoTestConfig(true), mt)
	if _, err := svc.HandleCallback(context.Background(), "generic", "", "v"); err != ErrSSOMissingCode {
		t.Errorf("expected ErrSSOMissingCode, got %v", err)
	}
}

func TestSSOService_HandleCallback_TokenExchangeFailure(t *testing.T) {
	database := testutil.NewTestDB(t,
		&model.SystemUser{},
		&model.SSOIdentity{},
	)
	db.SetTestDB(database)
	defer db.SetTestDB(nil)

	mt := setupSSOServiceTest(t, database)
	mt.exchangeError = "invalid_grant"
	svc := newTestSSOService(t, database, ssoTestConfig(true), mt)

	_, err := svc.HandleCallback(context.Background(), "generic", "bad-code", "v")
	if err == nil {
		t.Fatal("expected error on token exchange failure")
	}
	if !strings.Contains(err.Error(), "令牌交换失败") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestSSOService_HandleCallback_EmailMatchWithoutAutoProvision(t *testing.T) {
	database := testutil.NewTestDB(t,
		&model.SystemUser{},
		&model.SSOIdentity{},
	)
	db.SetTestDB(database)
	defer db.SetTestDB(nil)

	existing := &model.SystemUser{
		Username: "alice_local",
		Password: "Passw" + "ord123",
		Email:    "alice@example.com",
		Role:     "user",
		Status:   1,
		Enabled:  true,
	}
	if err := database.Create(existing).Error; err != nil {
		t.Fatalf("seed user: %v", err)
	}

	mt := setupSSOServiceTest(t, database)
	svc := newTestSSOService(t, database, ssoTestConfig(false), mt)

	result, err := svc.HandleCallback(context.Background(), "generic", "test-code", "v")
	if err != nil {
		t.Fatalf("HandleCallback: %v", err)
	}
	if result.IsNewUser {
		t.Error("expected is_new_user == false when matching existing user")
	}
	if result.User == nil || result.User.Username != "alice_local" {
		t.Errorf("expected existing user returned, got %+v", result.User)
	}

	var identities []model.SSOIdentity
	database.Where("provider = ? AND subject = ?", "generic", "user-123").Find(&identities)
	if len(identities) != 1 || identities[0].UserID != existing.ID {
		t.Errorf("expected identity bound to existing user, got %+v", identities)
	}
}

func TestSSOService_HandleCallback_NoMatchWithoutAutoProvision(t *testing.T) {
	database := testutil.NewTestDB(t,
		&model.SystemUser{},
		&model.SSOIdentity{},
	)
	db.SetTestDB(database)
	defer db.SetTestDB(nil)

	mt := setupSSOServiceTest(t, database)
	svc := newTestSSOService(t, database, ssoTestConfig(false), mt)

	_, err := svc.HandleCallback(context.Background(), "generic", "test-code", "v")
	if err != ErrSSOUserNotBound {
		t.Errorf("expected ErrSSOUserNotBound, got %v", err)
	}
}

func TestSSOService_UniqueUsernameOnConflict(t *testing.T) {
	database := testutil.NewTestDB(t,
		&model.SystemUser{},
		&model.SSOIdentity{},
	)
	db.SetTestDB(database)
	defer db.SetTestDB(nil)

	if err := database.Create(&model.SystemUser{Username: "alice", Password: "Passw" + "ord123", Role: "user", Status: 1, Enabled: true}).Error; err != nil {
		t.Fatalf("seed: %v", err)
	}

	mt := setupSSOServiceTest(t, database)
	svc := newTestSSOService(t, database, ssoTestConfig(true), mt)

	result, err := svc.HandleCallback(context.Background(), "generic", "test-code", "v")
	if err != nil {
		t.Fatalf("HandleCallback: %v", err)
	}
	if result.User == nil || result.User.Username == "alice" {
		t.Errorf("expected a unique username on conflict, got %q", result.User.Username)
	}
}

func TestSSOService_ListProviders(t *testing.T) {
	database := testutil.NewTestDB(t)
	db.SetTestDB(database)
	defer db.SetTestDB(nil)

	cfg := config.SSOConfig{
		Enabled: true,
		Providers: map[string]config.SSOProviderConfig{
			"wecom":  {ClientID: "c1"},
			"feishu": {ClientID: "c2"},
		},
	}
	svc := NewSSOServiceWithRepos(cfg, repository.NewSystemUserRepository(), repository.NewSSOIdentityRepository(database))
	list := svc.ListProvidersFor(context.Background())
	if len(list) != 2 {
		t.Fatalf("expected 2 providers, got %d", len(list))
	}
	if list[0].Name != "feishu" || list[1].Name != "wecom" {
		t.Errorf("expected sorted providers, got %+v", list)
	}
	if list[0].DisplayName != "飞书" {
		t.Errorf("display name: got %q", list[0].DisplayName)
	}
	if list[0].LoginURL != "/api/sso/login/feishu" {
		t.Errorf("login url: got %q", list[0].LoginURL)
	}
}

// 确保 SSO 用户不能通过本地密码登录：随机密码经 BeforeCreate 加密后非明文
func TestSSOService_ProvisionedUserPasswordHashed(t *testing.T) {
	database := testutil.NewTestDB(t,
		&model.SystemUser{},
		&model.SSOIdentity{},
	)
	db.SetTestDB(database)
	defer db.SetTestDB(nil)

	mt := setupSSOServiceTest(t, database)
	svc := newTestSSOService(t, database, ssoTestConfig(true), mt)
	if _, err := svc.HandleCallback(context.Background(), "generic", "test-code", "v"); err != nil {
		t.Fatalf("HandleCallback: %v", err)
	}

	var u model.SystemUser
	if err := database.Where("username = ?", "alice").First(&u).Error; err != nil {
		t.Fatalf("query: %v", err)
	}
	if model.CheckSystemUserPassword(&u, "alice") {
		t.Error("provisioned user password should not match any plaintext")
	}
	if !strings.HasPrefix(u.Password, "$2a$") && !strings.HasPrefix(u.Password, "$2b$") {
		t.Errorf("expected bcrypt hash prefix, got %q", u.Password[:min(8, len(u.Password))])
	}
}
