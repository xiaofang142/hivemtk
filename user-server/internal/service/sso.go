package service

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"hivemtk-user/internal/config"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/sso"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/pkg/utils"
	"hivemtk-user/internal/pkg/utils/logger"
	"hivemtk-user/internal/repository"

	"gorm.io/gorm"
)

// SSO 业务错误（语义错误，可转 4xx / 特定提示）
var (
	ErrSSONotEnabled       = errors.New("sso: 未启用企业登录")
	ErrSSOProviderNotFound = errors.New("sso: 未找到该登录方式")
	ErrSSOMissingCode      = errors.New("sso: 缺少授权码")
	ErrSSOUserNotBound     = errors.New("sso: 该账号未绑定本地用户，请联系管理员")
	ErrSSOUserDisabled     = errors.New("sso: 关联用户已被禁用")
	ErrSSOInvalidState     = errors.New("sso: 登录状态校验失败，请重新发起登录")
)

// ProviderInfo 已启用的 SSO 提供方信息（登录页展示）
type ProviderInfo struct {
	Name        string `json:"name"`
	DisplayName string `json:"display_name"`
	LoginURL    string `json:"login_url"`
}

// SSOLoginResult SSO 登录结果（与 LoginResponse 对齐，前端可直接存储 token）
type SSOLoginResult struct {
	Token     string              `json:"token"`
	User      *SystemUserResponse `json:"user"`
	Expires   int64               `json:"expires"`
	Provider  string              `json:"provider"`
	IsNewUser bool                `json:"is_new_user"`
}

// SSOService 企业 SSO 登录服务
//
// 两套适配器并存（R5-4，SaaS 多租户）：
//   - adapters：从平台 config.yaml 的 sso 段构建，是"平台统一 IdP"那一档，也是
//     租户没自配时的回落；
//   - tenantCache：按租户缓存自配的 IdP（system_config_kv 的 sso.providers）。
//
// 之所以两份而不是合并成一份：登录页与回调都必须按**发起方所属租户**取 IdP，
// 用平台那一份去校验 A 租户的 ID Token 等于拿 A 的授权码去验 B 的签名。
type SSOService struct {
	enabled      bool
	jwtUtils     *utils.JWTUtils
	userRepo     repository.SystemUserRepository
	identityRepo repository.SSOIdentityRepository
	adapters     map[string]sso.Adapter

	kv         repository.SystemConfigKVRepository
	httpClient *http.Client

	tenantMu    sync.RWMutex
	tenantCache map[uint]*tenantSSOEntry
}

// tenantSSOEntry 一个租户已构建好的适配器集合 + 构建时用的那份 JSON 原文。
//
// 拿原文比对做失效判据：KV 没有变更事件，而每次读都是一条 SELECT；
// 原文没变就复用，变了（含明文→密文的透明升级）才重建。
type tenantSSOEntry struct {
	raw      string
	adapters map[string]sso.Adapter
}

// NewSSOService 生产入口：仓储由装配点注入。
//
// 刻意**不接收 *gorm.DB**（五层架构 §3.2：Service 直访 db 被阻断）。调用方写：
//
//	service.NewSSOService(
//	    repository.NewSSOIdentityRepository(db),
//	    repository.NewSystemConfigKVRepositoryWithDB(db),
//	)
//
// 这样"哪个连接用在哪"只出现在装配点一处，service 自己既不握句柄、也不决定连接。
func NewSSOService(identityRepo repository.SSOIdentityRepository, kv repository.SystemConfigKVRepository) *SSOService {
	svc := NewSSOServiceWithRepos(
		config.GetAppConfig().SSO,
		repository.NewSystemUserRepository(),
		identityRepo,
	)
	svc.kv = kv
	return svc
}

// NewSSOServiceWithRepos 注入依赖构建（便于测试）
//
// 不带 KV：只认平台 config.yaml 那一档。租户自配那一份要由装配层用
// NewSSOServiceWithKV 显式接上，测试里传 nil 就是在测回落行为。
func NewSSOServiceWithRepos(cfg config.SSOConfig, userRepo repository.SystemUserRepository, identityRepo repository.SSOIdentityRepository) *SSOService {
	return NewSSOServiceWithKV(cfg, userRepo, identityRepo, nil)
}

// NewSSOServiceWithKV 注入租户 KV 仓储构建（R5-4 生产/测试入口）。
func NewSSOServiceWithKV(cfg config.SSOConfig, userRepo repository.SystemUserRepository, identityRepo repository.SSOIdentityRepository, kv repository.SystemConfigKVRepository) *SSOService {
	svc := &SSOService{
		enabled:      cfg.Enabled,
		jwtUtils:     utils.NewJWTUtils(utils.DefaultJWTConfig),
		userRepo:     userRepo,
		identityRepo: identityRepo,
		adapters:     map[string]sso.Adapter{},
		kv:           kv,
		tenantCache:  map[uint]*tenantSSOEntry{},
	}
	// 按名字排序再建：map 遍历顺序随机，出错时"哪个 provider 先报错"会跟着抖，
	// 而运维是拿日志比对来定位那一行坏配置的。
	for _, name := range sortedProviderNames(cfg.Providers) {
		pcfg := cfg.Providers[name]
		if pcfg.ClientID == "" {
			continue
		}
		adapter, err := sso.NewAdapter(name, oidcConfigFrom(pcfg))
		if err != nil {
			logger.Errorf("[SSO] 构建 provider %q 适配器失败: %v", name, err)
			continue
		}
		svc.adapters[name] = adapter
	}
	return svc
}

func sortedProviderNames(m map[string]config.SSOProviderConfig) []string {
	names := make([]string, 0, len(m))
	for n := range m {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

func oidcConfigFrom(pcfg config.SSOProviderConfig) sso.OIDCConfig {
	return sso.OIDCConfig{
		Issuer:                pcfg.Issuer,
		ClientID:              pcfg.ClientID,
		ClientSecret:          pcfg.ClientSecret,
		RedirectURL:           pcfg.RedirectURL,
		AutoProvision:         pcfg.AutoProvision,
		DefaultRole:           pcfg.DefaultRole,
		Scopes:                pcfg.Scopes,
		AuthorizationEndpoint: pcfg.AuthorizationEndpoint,
		TokenEndpoint:         pcfg.TokenEndpoint,
		UserInfoEndpoint:      pcfg.UserInfoEndpoint,
		JWKSURI:               pcfg.JWKSURI,
	}
}

// buildAdapters 把一组 IdP 配置编译成适配器；单个 provider 构建失败只跳过它，
// 不让整个租户的登录页因为一行坏配置而全挂。
func (s *SSOService) buildAdapters(providers []TenantSSOProvider) map[string]sso.Adapter {
	out := make(map[string]sso.Adapter, len(providers))
	for _, p := range providers {
		if strings.TrimSpace(p.ClientID) == "" {
			continue
		}
		adapter, err := sso.NewAdapter(p.Name, sso.OIDCConfig{
			Issuer:                p.Issuer,
			ClientID:              p.ClientID,
			ClientSecret:          p.ClientSecret,
			RedirectURL:           p.RedirectURL,
			AutoProvision:         p.AutoProvision,
			DefaultRole:           p.DefaultRole,
			Scopes:                p.Scopes,
			AuthorizationEndpoint: p.AuthorizationEndpoint,
			TokenEndpoint:         p.TokenEndpoint,
			UserInfoEndpoint:      p.UserInfoEndpoint,
			JWKSURI:               p.JWKSURI,
		})
		if err != nil {
			logger.Errorf("[SSO] 构建租户 provider %q 适配器失败: %v", p.Name, err)
			continue
		}
		if s.httpClient != nil {
			adapter.OIDC().SetHTTPClient(s.httpClient)
		}
		out[p.Name] = adapter
	}
	return out
}

// adaptersFor 返回 ctx 所属租户可用的适配器集合，第二个返回值表示它是否来自租户自配。
//
// 回落顺序：租户 KV 有值且能建出至少一个适配器 → 用它；KV 没装配/没绑租户/
// 读失败/JSON 坏了/一条都建不出来 → 平台 config.yaml 那一份。
// "坏 JSON 回落平台"是有意的：把登录页打死只会让人去查数据库，而查数据库的人
// 不知道页面为什么白；日志里留一条 WARN 足够定位。
func (s *SSOService) adaptersFor(ctx context.Context) (map[string]sso.Adapter, bool) {
	if s.kv == nil {
		return s.adapters, false
	}
	raw, err := s.kv.Get(ctx, kvSSOProviders)
	if err != nil {
		if !errors.Is(err, repository.ErrKVTenantContextMissing) && !errors.Is(err, repository.ErrKVStoreUnavailable) {
			logger.Warnf("[SSO] 读租户 IdP 配置失败，回落平台配置: %v", err)
		}
		return s.adapters, false
	}
	if strings.TrimSpace(raw) == "" {
		return s.adapters, false
	}
	tid, ok := tenant.FromContext(ctx)
	if !ok {
		tid = tenant.DefaultTenantID
	}

	s.tenantMu.RLock()
	entry := s.tenantCache[tid]
	s.tenantMu.RUnlock()
	if entry != nil && entry.raw == raw {
		return entry.adapters, len(entry.adapters) > 0
	}

	providers, derr := decodeTenantSSOProviders(raw)
	if derr != nil {
		logger.Warnf("[SSO] 租户 %d 的 IdP 配置解析失败，回落平台配置: %v", tid, derr)
		return s.adapters, false
	}
	adapters := s.buildAdapters(providers)
	s.tenantMu.Lock()
	s.tenantCache[tid] = &tenantSSOEntry{raw: raw, adapters: adapters}
	s.tenantMu.Unlock()
	if len(adapters) == 0 {
		return s.adapters, false
	}
	return adapters, true
}

// EnabledFor 该租户是否可用企业登录：自配了 IdP 即视为开，否则看平台开关。
func (s *SSOService) EnabledFor(ctx context.Context) bool {
	if _, fromTenant := s.adaptersFor(ctx); fromTenant {
		return true
	}
	return s.enabled
}

// AdapterFor 取该租户可用的指定 provider 适配器。
func (s *SSOService) AdapterFor(ctx context.Context, provider string) (sso.Adapter, bool) {
	adapters, _ := s.adaptersFor(ctx)
	a, ok := adapters[provider]
	return a, ok
}

// ListProvidersFor 列出该租户可见的已启用登录方式（稳定排序）。
func (s *SSOService) ListProvidersFor(ctx context.Context) []ProviderInfo {
	adapters, _ := s.adaptersFor(ctx)
	out := make([]ProviderInfo, 0, len(adapters))
	for name, a := range adapters {
		out = append(out, ProviderInfo{
			Name:        name,
			DisplayName: a.DisplayName(),
			LoginURL:    "/api/sso/login/" + name,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// TenantProviders 读本租户自配的 IdP 原文（admin 回显口用，调用方负责打码）。
//
// 用 s.kv 而不是包级构造函数：句柄在建服务时已经定了，读的时候再取全局句柄
// 会出现"登录页读 A 库、参数页读 B 库"这种只在多库部署才暴露的错。
func (s *SSOService) TenantProviders(ctx context.Context) ([]TenantSSOProvider, error) {
	if s.kv == nil {
		return nil, repository.ErrKVStoreUnavailable
	}
	return loadTenantSSOProvidersWithRepo(ctx, s.kv)
}

// SaveTenantProviders 写入本租户的 IdP 配置并让缓存立刻换代。
//
// 缓存按 KV 原文比对失效，写完不主动清也能在下一次读时重建；但"改完刷新页面没变"
// 是这个功能最难自查的一种坏，所以写侧显式删掉该租户那一格。
func (s *SSOService) SaveTenantProviders(ctx context.Context, providers []TenantSSOProvider) error {
	if s.kv == nil {
		return repository.ErrKVStoreUnavailable
	}
	err := saveTenantSSOProvidersWithRepo(ctx, s.kv, providers)
	s.forgetTenant(ctx)
	return err
}

func (s *SSOService) forgetTenant(ctx context.Context) {
	tid, ok := tenant.FromContext(ctx)
	if !ok {
		tid = tenant.DefaultTenantID
	}
	s.tenantMu.Lock()
	delete(s.tenantCache, tid)
	s.tenantMu.Unlock()
}

// SetProviderHTTPClient 注入 HTTP 客户端到所有 provider（测试用 mock；生产不调用）。
//
// 存到字段上再套给已建好的适配器：租户适配器是**懒构建**的，只遍历现有 map 的话，
// 测试里注入的 mock 客户端永远套不到租户那一档，OIDC 发现会去打真网络。
func (s *SSOService) SetProviderHTTPClient(client *http.Client) {
	s.tenantMu.Lock()
	s.httpClient = client
	cached := make([]map[string]sso.Adapter, 0, len(s.tenantCache))
	for _, entry := range s.tenantCache {
		cached = append(cached, entry.adapters)
	}
	s.tenantMu.Unlock()
	for _, a := range s.adapters {
		a.OIDC().SetHTTPClient(client)
	}
	for _, m := range cached {
		for _, a := range m {
			a.OIDC().SetHTTPClient(client)
		}
	}
}

// HandleCallback 处理 IdP 回调：
//  1. 交换授权码 → token
//  2. 验证 ID Token（签名 / issuer / audience / exp）
//  3. 归一化 claims → 关联本地用户（或自动创建）
//  4. 签发本地 JWT
//
// ctx 必须已绑定发起方所属租户（controller 在匿名入口用 tenant_code 解析过）：
// IdP 配置、身份绑定、用户行读写都按这个租户走，换租户等于换一套签名密钥。
func (s *SSOService) HandleCallback(ctx context.Context, provider, code, verifier string) (*SSOLoginResult, error) {
	if !s.EnabledFor(ctx) {
		return nil, ErrSSONotEnabled
	}
	adapter, ok := s.AdapterFor(ctx, provider)
	if !ok {
		return nil, ErrSSOProviderNotFound
	}
	if code == "" {
		return nil, ErrSSOMissingCode
	}

	oidc := adapter.OIDC()
	tok, err := oidc.ExchangeCode(ctx, code, verifier)
	if err != nil {
		return nil, fmt.Errorf("sso: 令牌交换失败: %w", err)
	}
	claims, err := oidc.VerifyIDToken(ctx, tok.IDToken)
	if err != nil {
		return nil, fmt.Errorf("sso: ID Token 校验失败: %w", err)
	}
	nu := adapter.MapClaims(claims)
	if nu.Subject == "" {
		return nil, errors.New("sso: 无法从 ID Token 解析主体标识")
	}

	user, isNew, err := s.resolveUser(ctx, adapter, nu)
	if err != nil {
		return nil, err
	}

	// 租户归属取用户行实际 tenant_id（T031：SaaS 模式下 SSO 登录同样需带租户 claim）
	tid := user.TenantID
	if tid == 0 {
		tid = tenant.DefaultTenantID
	}
	token, err := s.jwtUtils.GenerateTokenForTenant(user.ID, user.Username, user.Role, "", 0, 0, tid)
	if err != nil {
		return nil, fmt.Errorf("sso: 签发本地令牌失败: %w", err)
	}

	now := time.Now()
	user.LastLogin = &now
	if err := s.userRepo.Update(ctx, user); err != nil {
		logger.Error(err, "[SSO] 更新用户最后登录时间失败")
	}

	return &SSOLoginResult{
		Token:     token,
		User:      toSystemUserResponse(user),
		Expires:   time.Now().Add(time.Hour * time.Duration(utils.DefaultJWTConfig.ExpiresHours)).Unix(),
		Provider:  provider,
		IsNewUser: isNew,
	}, nil
}

// resolveUser 把 IdP 主体映射到本地用户。
//
// adapter 由调用方按租户解析后传进来，不再回查 s.adapters：那张表只有平台
// config.yaml 那一档，租户自配的 provider 名在里面查不到，`[...]` 直接取下标
// 会拿到零值接口并在 .OIDC() 上 panic —— 表现是租户一登录就 500。
func (s *SSOService) resolveUser(ctx context.Context, adapter sso.Adapter, nu *sso.NormalizedUser) (*model.SystemUser, bool, error) {
	identity, err := s.identityRepo.GetByProviderSubject(ctx, nu.Provider, nu.Subject)
	if err == nil {
		user, err := s.userRepo.GetByID(ctx, identity.UserID)
		if err != nil {
			return nil, false, errors.New("sso: 绑定的本地用户不存在，请联系管理员")
		}
		if user.Status != 1 || !user.Enabled {
			return nil, false, ErrSSOUserDisabled
		}
		return user, false, nil
	}
	if !errors.Is(err, repository.ErrSSOIdentityNotFound) {
		return nil, false, fmt.Errorf("sso: 查询身份绑定失败: %w", err)
	}

	cfg := adapter.OIDC().Config()
	if cfg.AutoProvision {
		user, err := s.provisionUser(ctx, nu, cfg)
		if err != nil {
			return nil, false, err
		}
		return user, true, nil
	}

	if nu.Email != "" {
		user, err := s.userRepo.GetByEmail(ctx, nu.Email)
		if err == nil {
			if user.Status != 1 || !user.Enabled {
				return nil, false, ErrSSOUserDisabled
			}
			if err := s.bindIdentity(ctx, nu.Provider, nu.Subject, user.ID); err != nil {
				return nil, false, err
			}
			return user, false, nil
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, false, fmt.Errorf("sso: 按邮箱查询用户失败: %w", err)
		}
	}
	return nil, false, ErrSSOUserNotBound
}

func (s *SSOService) provisionUser(ctx context.Context, nu *sso.NormalizedUser, cfg sso.OIDCConfig) (*model.SystemUser, error) {
	role := nu.Role
	if role == "" {
		role = cfg.DefaultRole
	}
	if !model.IsValidSystemUserRole(role) {
		role = model.SystemUserRoleUser
	}

	username := s.uniqueUsername(ctx, nu.Username, nu.Email)
	email := nu.Email
	if email != "" {
		// 邮箱唯一性是跨租户全局的（global_uniq_system_users_email 部分唯一索引）。
		// 按当前租户查重会漏掉别的租户已占用的同一邮箱，落库时才被唯一索引
		// 拒绝（SQLSTATE 23505）→ 自动开户失败。这里必须走全局查重。
		if exists, _ := s.userRepo.EmailExistsGlobal(ctx, email); exists {
			email = ""
		}
	}

	user := &model.SystemUser{
		Username: username,
		Password: randomPassword(),
		Email:    email,
		RealName: nu.RealName,
		Role:     role,
		Status:   1,
		Enabled:  true,
	}
	if err := s.userRepo.Create(ctx, user); err != nil {
		return nil, fmt.Errorf("sso: 自动创建用户失败: %w", err)
	}
	if err := s.bindIdentity(ctx, nu.Provider, nu.Subject, user.ID); err != nil {
		return nil, err
	}
	logger.Infof("[SSO] 自动创建用户 %q（provider=%s, subject=%s）", user.Username, nu.Provider, nu.Subject)
	return user, nil
}

func (s *SSOService) bindIdentity(ctx context.Context, provider, subject string, userID uint) error {
	identity := &model.SSOIdentity{
		Provider: provider,
		Subject:  subject,
		UserID:   userID,
	}
	if err := s.identityRepo.Create(ctx, identity); err != nil {
		return fmt.Errorf("sso: 绑定身份失败: %w", err)
	}
	return nil
}

func (s *SSOService) uniqueUsername(ctx context.Context, base, email string) string {
	name := strings.TrimSpace(base)
	if name == "" {
		name = email
	}
	if name == "" {
		name = "sso_user"
	}
	if exists, _ := s.userRepo.UsernameExists(ctx, name, 0); !exists {
		return name
	}
	for i := 0; i < 10; i++ {
		candidate := fmt.Sprintf("%s_%s", name, randomSuffix(3))
		if len(candidate) > 50 {
			candidate = candidate[:50]
		}
		if exists, _ := s.userRepo.UsernameExists(ctx, candidate, 0); !exists {
			return candidate
		}
	}
	return fmt.Sprintf("%s_%d", name, time.Now().UnixNano()%100000)
}

func randomPassword() string {
	b := make([]byte, 32)
	_, _ = rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}

func randomSuffix(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return fmt.Sprintf("%x", b)
}

func toSystemUserResponse(user *model.SystemUser) *SystemUserResponse {
	return &SystemUserResponse{
		ID:          user.ID,
		Username:    user.Username,
		Email:       user.Email,
		Phone:       user.Phone,
		RealName:    user.RealName,
		Role:        user.Role,
		Status:      user.Status,
		LastLogin:   user.LastLogin,
		LastLoginAt: user.LastLogin,
		CreatedAt:   user.CreatedAt,
		UpdatedAt:   user.UpdatedAt,
	}
}
