package sso

import (
	"context"
	"crypto/ecdsa"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
)

// OIDCConfig OIDC 提供方配置
//
// 支持两种端点获取方式：
//   - 标准 OIDC Discovery（Issuer 可公开访问 /.well-known/openid-configuration）
//   - 显式端点配置（飞书 / 钉钉 / 企微等不提供标准 Discovery 的 IdP 必填）
type OIDCConfig struct {
	Issuer        string
	ClientID      string
	ClientSecret  string
	RedirectURL   string
	Scopes        []string
	UsernameClaim string
	EmailClaim    string
	RoleClaim     string
	DefaultRole   string
	AutoProvision bool
	HTTPTimeout   time.Duration

	AuthorizationEndpoint string
	TokenEndpoint         string
	UserInfoEndpoint      string
	JWKSURI               string
}

// OIDCProvider OIDC 提供方（运行时实例）
type OIDCProvider struct {
	cfg         OIDCConfig
	mu          sync.RWMutex
	discovery   *DiscoveryDoc
	jwks        *JWKS
	lastRefresh time.Time
	http        *http.Client
}

// DiscoveryDoc OIDC Discovery 文档
type DiscoveryDoc struct {
	Issuer                string `json:"issuer"`
	AuthorizationEndpoint string `json:"authorization_endpoint"`
	TokenEndpoint         string `json:"token_endpoint"`
	UserinfoEndpoint      string `json:"userinfo_endpoint"`
	JWKSURI               string `json:"jwks_uri"`
	EndSessionEndpoint    string `json:"end_session_endpoint"`
}

// JWKS JSON Web Key Set
type JWKS struct {
	Keys []JWK `json:"keys"`
}

// JWK 单个 JWK
type JWK struct {
	Kty string `json:"kty"`
	Use string `json:"use"`
	Kid string `json:"kid"`
	Alg string `json:"alg"`
	N   string `json:"n"`
	E   string `json:"e"`
	Crv string `json:"crv"`
	X   string `json:"x"`
	Y   string `json:"y"`
}

// IDTokenClaims ID Token 声明
type IDTokenClaims struct {
	Issuer            string                 `json:"iss"`
	Subject           string                 `json:"sub"`
	Audience          AudienceClaim          `json:"aud"`
	IssuedAt          int64                  `json:"iat"`
	Expiry            int64                  `json:"exp"`
	Nonce             string                 `json:"nonce"`
	Email             string                 `json:"email"`
	EmailVerified     bool                   `json:"email_verified"`
	Name              string                 `json:"name"`
	PreferredUsername string                 `json:"preferred_username"`
	Picture           string                 `json:"picture"`
	Roles             []string               `json:"roles"`
	Groups            []string               `json:"groups"`
	Extra             map[string]interface{} `json:"-"`
}

// AudienceClaim aud 可能是 string 或 []string
type AudienceClaim []string

func (a *AudienceClaim) UnmarshalJSON(data []byte) error {
	if string(data) == "null" {
		*a = nil
		return nil
	}
	var single string
	if err := json.Unmarshal(data, &single); err == nil {
		*a = []string{single}
		return nil
	}
	var arr []string
	if err := json.Unmarshal(data, &arr); err != nil {
		return err
	}
	*a = arr
	return nil
}

// NewOIDCProvider 创建 OIDC 提供方实例（不立即拉取 discovery/jwks）
func NewOIDCProvider(cfg OIDCConfig) *OIDCProvider {
	if cfg.UsernameClaim == "" {
		cfg.UsernameClaim = "preferred_username"
	}
	if cfg.EmailClaim == "" {
		cfg.EmailClaim = "email"
	}
	if cfg.RoleClaim == "" {
		cfg.RoleClaim = "roles"
	}
	if cfg.DefaultRole == "" {
		cfg.DefaultRole = "user"
	}
	if cfg.HTTPTimeout == 0 {
		cfg.HTTPTimeout = 10 * time.Second
	}
	if len(cfg.Scopes) == 0 {
		cfg.Scopes = []string{"openid", "profile", "email"}
	}
	return &OIDCProvider{cfg: cfg}
}

func (p *OIDCProvider) httpClient() *http.Client {
	if p.http != nil {
		return p.http
	}
	return &http.Client{Timeout: p.cfg.HTTPTimeout}
}

func (p *OIDCProvider) refreshDiscovery(ctx context.Context) error {
	wellKnownURL := strings.TrimRight(p.cfg.Issuer, "/") + "/.well-known/openid-configuration"
	req, err := http.NewRequestWithContext(ctx, "GET", wellKnownURL, nil)
	if err != nil {
		return fmt.Errorf("sso: build discovery request: %w", err)
	}
	client := p.httpClient()
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("sso: discovery request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != 200 {
		return fmt.Errorf("sso: discovery returned status %d", resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("sso: read discovery body: %w", err)
	}
	var doc DiscoveryDoc
	if err := json.Unmarshal(body, &doc); err != nil {
		return fmt.Errorf("sso: parse discovery: %w", err)
	}
	if doc.Issuer != p.cfg.Issuer {
		return fmt.Errorf("sso: issuer mismatch: got %q expected %q", doc.Issuer, p.cfg.Issuer)
	}
	p.mu.Lock()
	p.discovery = &doc
	p.lastRefresh = time.Now()
	p.mu.Unlock()
	return nil
}

func (p *OIDCProvider) loadExplicitEndpoints() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.discovery = &DiscoveryDoc{
		Issuer:                p.cfg.Issuer,
		AuthorizationEndpoint: p.cfg.AuthorizationEndpoint,
		TokenEndpoint:         p.cfg.TokenEndpoint,
		UserinfoEndpoint:      p.cfg.UserInfoEndpoint,
		JWKSURI:               p.cfg.JWKSURI,
	}
	p.lastRefresh = time.Now()
}

func (p *OIDCProvider) refreshJWKS(ctx context.Context) error {
	p.mu.RLock()
	jwksURI := ""
	if p.discovery != nil {
		jwksURI = p.discovery.JWKSURI
	}
	p.mu.RUnlock()
	if jwksURI == "" {
		if p.cfg.JWKSURI != "" {
			jwksURI = p.cfg.JWKSURI
		} else {
			jwksURI = strings.TrimRight(p.cfg.Issuer, "/") + "/.well-known/jwks.json"
		}
	}
	req, err := http.NewRequestWithContext(ctx, "GET", jwksURI, nil)
	if err != nil {
		return fmt.Errorf("sso: build jwks request: %w", err)
	}
	client := p.httpClient()
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("sso: jwks request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != 200 {
		return fmt.Errorf("sso: jwks returned status %d", resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("sso: read jwks body: %w", err)
	}
	var jwks JWKS
	if err := json.Unmarshal(body, &jwks); err != nil {
		return fmt.Errorf("sso: parse jwks: %w", err)
	}
	p.mu.Lock()
	p.jwks = &jwks
	p.mu.Unlock()
	return nil
}

func (p *OIDCProvider) ensureFresh(ctx context.Context) error {
	p.mu.RLock()
	stale := time.Since(p.lastRefresh) > 5*time.Minute
	hasDiscovery := p.discovery != nil
	hasJWKS := p.jwks != nil
	explicit := p.cfg.AuthorizationEndpoint != "" || p.cfg.TokenEndpoint != ""
	p.mu.RUnlock()
	if !hasDiscovery || stale {
		if explicit {
			p.loadExplicitEndpoints()
		} else {
			if err := p.refreshDiscovery(ctx); err != nil {
				return err
			}
		}
	}
	if !hasJWKS || stale {
		if p.cfg.JWKSURI != "" || !explicit {
			if err := p.refreshJWKS(ctx); err != nil {
				return err
			}
		}
	}
	return nil
}

// LoginHandler 启动登录（生成 state + nonce + PKCE code_challenge，重定向到 IdP）
func (p *OIDCProvider) LoginHandler() gin.HandlerFunc {
	return func(c *gin.Context) {
		if err := p.ensureFresh(c.Request.Context()); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "SSO discovery failed: " + err.Error()})
			return
		}

		state := randString(32)
		nonce := randString(32)
		codeVerifier := randString(64)
		codeChallenge := pkceS256(codeVerifier)

		setSecureCookie(c, "sso_state", state, 5*time.Minute)
		setSecureCookie(c, "sso_nonce", nonce, 5*time.Minute)
		setSecureCookie(c, "sso_verifier", codeVerifier, 5*time.Minute)

		authURL, err := url.Parse(p.discovery.AuthorizationEndpoint)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "invalid authorization endpoint"})
			return
		}
		q := authURL.Query()
		q.Set("response_type", "code")
		q.Set("client_id", p.cfg.ClientID)
		q.Set("redirect_uri", p.cfg.RedirectURL)
		q.Set("scope", strings.Join(p.cfg.Scopes, " "))
		q.Set("state", state)
		q.Set("nonce", nonce)
		q.Set("code_challenge", codeChallenge)
		q.Set("code_challenge_method", "S256")
		authURL.RawQuery = q.Encode()

		c.Redirect(http.StatusFound, authURL.String())
	}
}

// CallbackHandler 处理 IdP 回调（用 code 换 token，验证 ID Token，返回用户信息）
func (p *OIDCProvider) CallbackHandler() gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx := c.Request.Context()
		if err := p.ensureFresh(ctx); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "SSO discovery failed: " + err.Error()})
			return
		}

		state := c.Query("state")
		expectedState, _ := c.Cookie("sso_state")
		if state == "" || state != expectedState {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid state"})
			return
		}

		code := c.Query("code")
		if code == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "missing code"})
			return
		}

		verifier, _ := c.Cookie("sso_verifier")
		tok, err := p.exchangeCode(ctx, code, verifier)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "token exchange failed: " + err.Error()})
			return
		}

		claims, err := p.verifyIDToken(ctx, tok.IDToken)
		if err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "ID token invalid: " + err.Error()})
			return
		}

		expectedNonce, _ := c.Cookie("sso_nonce")
		if expectedNonce == "" || claims.Nonce != expectedNonce {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "nonce mismatch"})
			return
		}

		clearCookie(c, "sso_state")
		clearCookie(c, "sso_nonce")
		clearCookie(c, "sso_verifier")

		_ = tok.AccessToken

		c.JSON(http.StatusOK, gin.H{
			"sub":            claims.Subject,
			"username":       mapUsername(claims, p.cfg),
			"email":          mapEmail(claims, p.cfg),
			"role":           mapRole(claims, p.cfg),
			"groups":         claims.Groups,
			"id_token":       tok.IDToken,
			"access_token":   tok.AccessToken,
			"refresh_token":  tok.RefreshToken,
			"expires_in":     tok.ExpiresIn,
			"auto_provision": p.cfg.AutoProvision,
		})
	}
}

// TokenResponse token 端点响应
type TokenResponse struct {
	AccessToken  string `json:"access_token"`
	TokenType    string `json:"token_type"`
	IDToken      string `json:"id_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int    `json:"expires_in"`
	Scope        string `json:"scope"`
}

func (p *OIDCProvider) exchangeCode(ctx context.Context, code, verifier string) (*TokenResponse, error) {
	form := url.Values{}
	form.Set("grant_type", "authorization_code")
	form.Set("code", code)
	form.Set("redirect_uri", p.cfg.RedirectURL)
	form.Set("client_id", p.cfg.ClientID)
	if p.cfg.ClientSecret != "" {
		form.Set("client_secret", p.cfg.ClientSecret)
	}
	if verifier != "" {
		form.Set("code_verifier", verifier)
	}
	req, err := http.NewRequestWithContext(ctx, "POST", p.discovery.TokenEndpoint,
		strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	client := p.httpClient()
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("status %d: %s", resp.StatusCode, string(body))
	}
	var tr TokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&tr); err != nil {
		return nil, err
	}
	if tr.IDToken == "" {
		return nil, fmt.Errorf("missing id_token in response")
	}
	return &tr, nil
}

func (p *OIDCProvider) verifyIDToken(ctx context.Context, idToken string) (*IDTokenClaims, error) {
	parts := strings.Split(idToken, ".")
	if len(parts) != 3 {
		return nil, fmt.Errorf("malformed JWT")
	}
	headerBytes, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return nil, fmt.Errorf("decode header: %w", err)
	}
	var header struct {
		Alg string `json:"alg"`
		Kid string `json:"kid"`
		Typ string `json:"typ"`
	}
	if err := json.Unmarshal(headerBytes, &header); err != nil {
		return nil, fmt.Errorf("parse header: %w", err)
	}

	key, err := p.findKey(ctx, header.Kid)
	if err != nil {
		return nil, err
	}

	parsed, err := jwt.Parse(idToken, func(t *jwt.Token) (interface{}, error) {
		if t.Method.Alg() != header.Alg {
			return nil, fmt.Errorf("alg mismatch: %s vs %s", t.Method.Alg(), header.Alg)
		}
		return key, nil
	}, jwt.WithValidMethods([]string{"RS256", "RS384", "RS512", "ES256", "ES384", "ES512"}))
	if err != nil {
		return nil, err
	}
	if !parsed.Valid {
		return nil, fmt.Errorf("token not valid")
	}
	mc, ok := parsed.Claims.(jwt.MapClaims)
	if !ok {
		return nil, fmt.Errorf("unexpected claims type")
	}
	claims := &IDTokenClaims{Extra: map[string]interface{}{}}
	if v, ok := mc["iss"].(string); ok {
		claims.Issuer = v
	}
	if v, ok := mc["sub"].(string); ok {
		claims.Subject = v
	}
	if v, ok := mc["email"].(string); ok {
		claims.Email = v
	}
	if v, ok := mc["name"].(string); ok {
		claims.Name = v
	}
	if v, ok := mc["preferred_username"].(string); ok {
		claims.PreferredUsername = v
	}
	switch aud := mc["aud"].(type) {
	case string:
		claims.Audience = []string{aud}
	case []interface{}:
		for _, a := range aud {
			if s, ok := a.(string); ok {
				claims.Audience = append(claims.Audience, s)
			}
		}
	}
	if v, ok := mc["iat"].(float64); ok {
		claims.IssuedAt = int64(v)
	}
	if v, ok := mc["exp"].(float64); ok {
		claims.Expiry = int64(v)
	}
	if v, ok := mc["nonce"].(string); ok {
		claims.Nonce = v
	}
	if v, ok := mc["email_verified"].(bool); ok {
		claims.EmailVerified = v
	}
	if v, ok := mc["roles"].([]interface{}); ok {
		for _, r := range v {
			if s, ok := r.(string); ok {
				claims.Roles = append(claims.Roles, s)
			}
		}
	}
	if v, ok := mc["groups"].([]interface{}); ok {
		for _, g := range v {
			if s, ok := g.(string); ok {
				claims.Groups = append(claims.Groups, s)
			}
		}
	}
	for k, v := range mc {
		if k == "iss" || k == "sub" || k == "aud" || k == "exp" || k == "iat" ||
			k == "nonce" || k == "email" || k == "name" || k == "preferred_username" ||
			k == "email_verified" || k == "roles" || k == "groups" {
			continue
		}
		claims.Extra[k] = v
	}

	if claims.Issuer != p.cfg.Issuer {
		return nil, fmt.Errorf("issuer mismatch: got %q want %q", claims.Issuer, p.cfg.Issuer)
	}
	audOK := false
	for _, a := range claims.Audience {
		if a == p.cfg.ClientID {
			audOK = true
			break
		}
	}
	if !audOK {
		return nil, fmt.Errorf("audience mismatch: %v does not contain client_id %q", claims.Audience, p.cfg.ClientID)
	}
	return claims, nil
}

func (p *OIDCProvider) findKey(ctx context.Context, kid string) (interface{}, error) {
	p.mu.RLock()
	jwks := p.jwks
	p.mu.RUnlock()
	if jwks == nil {
		if err := p.refreshJWKS(ctx); err != nil {
			return nil, err
		}
		p.mu.RLock()
		jwks = p.jwks
		p.mu.RUnlock()
	}
	for _, k := range jwks.Keys {
		if k.Kid != kid {
			continue
		}
		switch k.Kty {
		case "RSA":
			return jwkToRSA(k)
		case "EC":
			return jwkToEC(k)
		default:
			return nil, fmt.Errorf("unsupported key type: %s", k.Kty)
		}
	}
	if err := p.refreshJWKS(ctx); err != nil {
		return nil, err
	}
	p.mu.RLock()
	jwks = p.jwks
	p.mu.RUnlock()
	for _, k := range jwks.Keys {
		if k.Kid != kid {
			continue
		}
		switch k.Kty {
		case "RSA":
			return jwkToRSA(k)
		case "EC":
			return jwkToEC(k)
		}
	}
	return nil, fmt.Errorf("kid %q not found in JWKS", kid)
}

func jwkToRSA(k JWK) (*rsa.PublicKey, error) {
	nb, err := base64.RawURLEncoding.DecodeString(k.N)
	if err != nil {
		return nil, fmt.Errorf("decode n: %w", err)
	}
	eb, err := base64.RawURLEncoding.DecodeString(k.E)
	if err != nil {
		return nil, fmt.Errorf("decode e: %w", err)
	}
	e := 0
	for _, b := range eb {
		e = e<<8 | int(b)
	}
	return &rsa.PublicKey{
		N: new(big.Int).SetBytes(nb),
		E: e,
	}, nil
}

func jwkToEC(k JWK) (*ecdsa.PublicKey, error) {
	xb, err := base64.RawURLEncoding.DecodeString(k.X)
	if err != nil {
		return nil, fmt.Errorf("decode x: %w", err)
	}
	yb, err := base64.RawURLEncoding.DecodeString(k.Y)
	if err != nil {
		return nil, fmt.Errorf("decode y: %w", err)
	}
	curve, err := curveFromName(k.Crv)
	if err != nil {
		return nil, err
	}
	return &ecdsa.PublicKey{
		Curve: curve,
		X:     new(big.Int).SetBytes(xb),
		Y:     new(big.Int).SetBytes(yb),
	}, nil
}

func randString(n int) string {
	b := make([]byte, n)
	_, _ = io.ReadFull(readerFunc(func(p []byte) (int, error) {
		return randRead(p)
	}), b)
	return base64.RawURLEncoding.EncodeToString(b)[:n]
}

func pkceS256(verifier string) string {
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

func mapUsername(c *IDTokenClaims, cfg OIDCConfig) string {
	if cfg.UsernameClaim != "" && c.Extra != nil {
		if v, ok := c.Extra[cfg.UsernameClaim].(string); ok && v != "" {
			return v
		}
	}
	if c.PreferredUsername != "" {
		return c.PreferredUsername
	}
	if c.Email != "" {
		return c.Email
	}
	return c.Subject
}

func mapEmail(c *IDTokenClaims, cfg OIDCConfig) string {
	if cfg.EmailClaim != "" && c.Extra != nil {
		if v, ok := c.Extra[cfg.EmailClaim].(string); ok && v != "" {
			return v
		}
	}
	if c.Email != "" {
		return c.Email
	}
	return ""
}

func mapRole(c *IDTokenClaims, cfg OIDCConfig) string {
	if len(c.Roles) > 0 {
		return c.Roles[0]
	}
	if len(c.Groups) > 0 {
		return c.Groups[0]
	}
	return cfg.DefaultRole
}

// SetHTTPClient 注入自定义 HTTP 客户端（测试用 mock，生产不调用）
func (p *OIDCProvider) SetHTTPClient(client *http.Client) {
	p.http = client
}

// Config 返回当前提供方配置副本
func (p *OIDCProvider) Config() OIDCConfig {
	return p.cfg
}

// EnsureFresh 确保 discovery/jwks 已加载（供外部在无 gin 上下文场景调用）
func (p *OIDCProvider) EnsureFresh(ctx context.Context) error {
	return p.ensureFresh(ctx)
}

// ExchangeCode 用授权码交换 token（公开方法，供 router/controller 复用）
func (p *OIDCProvider) ExchangeCode(ctx context.Context, code, verifier string) (*TokenResponse, error) {
	if err := p.ensureFresh(ctx); err != nil {
		return nil, err
	}
	return p.exchangeCode(ctx, code, verifier)
}

// VerifyIDToken 验证并解析 ID Token（公开方法，供 router/controller 复用）
func (p *OIDCProvider) VerifyIDToken(ctx context.Context, idToken string) (*IDTokenClaims, error) {
	if err := p.ensureFresh(ctx); err != nil {
		return nil, err
	}
	return p.verifyIDToken(ctx, idToken)
}

// MapUsername 映射本地用户名（公开方法）
func (p *OIDCProvider) MapUsername(c *IDTokenClaims) string {
	return mapUsername(c, p.cfg)
}

// MapEmail 映射本地邮箱（公开方法）
func (p *OIDCProvider) MapEmail(c *IDTokenClaims) string {
	return mapEmail(c, p.cfg)
}

// MapRole 映射本地角色（公开方法）
func (p *OIDCProvider) MapRole(c *IDTokenClaims) string {
	return mapRole(c, p.cfg)
}

// BuildAuthURL 构造授权 URL（PKCE S256 + state + nonce）
//
// 供 controller 在 gin 无内建 handler 的场景（多 provider 路由）复用：
//
//	state := randString(32)
//	nonce := randString(32)
//	verifier := randString(64)
//	authURL, _ := provider.BuildAuthURL(state, nonce, verifier)
func (p *OIDCProvider) BuildAuthURL(state, nonce, codeVerifier string) (string, error) {
	p.mu.RLock()
	authEp := ""
	redirect := p.cfg.RedirectURL
	if p.discovery != nil {
		authEp = p.discovery.AuthorizationEndpoint
	}
	p.mu.RUnlock()
	if authEp == "" {
		return "", fmt.Errorf("sso: authorization endpoint not loaded")
	}
	if redirect == "" {
		return "", fmt.Errorf("sso: redirect_url not configured")
	}
	authURL, err := url.Parse(authEp)
	if err != nil {
		return "", fmt.Errorf("sso: parse authorization endpoint: %w", err)
	}
	q := authURL.Query()
	q.Set("response_type", "code")
	q.Set("client_id", p.cfg.ClientID)
	q.Set("redirect_uri", redirect)
	q.Set("scope", strings.Join(p.cfg.Scopes, " "))
	q.Set("state", state)
	q.Set("nonce", nonce)
	q.Set("code_challenge", pkceS256(codeVerifier))
	q.Set("code_challenge_method", "S256")
	authURL.RawQuery = q.Encode()
	return authURL.String(), nil
}

// SetRedirectURL 动态设置回调地址（配置留空时由 controller 依据请求推导后注入）
func (p *OIDCProvider) SetRedirectURL(redirectURL string) {
	p.mu.Lock()
	p.cfg.RedirectURL = redirectURL
	p.mu.Unlock()
}

func setSecureCookie(c *gin.Context, name, value string, ttl time.Duration) {
	c.SetCookie(name, value, int(ttl.Seconds()), "/", "", true, true)
}

func clearCookie(c *gin.Context, name string) {
	c.SetCookie(name, "", -1, "/", "", true, true)
}
