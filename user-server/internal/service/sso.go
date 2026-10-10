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
	"time"

	"hivemtk-user/internal/config"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/sso"
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
type SSOService struct {
	enabled      bool
	jwtUtils     *utils.JWTUtils
	userRepo     repository.SystemUserRepository
	identityRepo repository.SSOIdentityRepository
	adapters     map[string]sso.Adapter
}

// NewSSOService 从全局配置构建 SSO 服务（生产入口）
func NewSSOService(db *gorm.DB) *SSOService {
	return NewSSOServiceWithRepos(
		config.GetAppConfig().SSO,
		repository.NewSystemUserRepository(),
		repository.NewSSOIdentityRepository(db),
	)
}

// NewSSOServiceWithRepos 注入依赖构建（便于测试）
func NewSSOServiceWithRepos(cfg config.SSOConfig, userRepo repository.SystemUserRepository, identityRepo repository.SSOIdentityRepository) *SSOService {
	svc := &SSOService{
		enabled:      cfg.Enabled,
		jwtUtils:     utils.NewJWTUtils(utils.DefaultJWTConfig),
		userRepo:     userRepo,
		identityRepo: identityRepo,
		adapters:     map[string]sso.Adapter{},
	}
	for name, pcfg := range cfg.Providers {
		if pcfg.ClientID == "" {
			continue
		}
		oidcCfg := sso.OIDCConfig{
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
		adapter, err := sso.NewAdapter(name, oidcCfg)
		if err != nil {
			logger.Errorf("[SSO] 构建 provider %q 适配器失败: %v", name, err)
			continue
		}
		svc.adapters[name] = adapter
	}
	return svc
}

// Enabled 返回 SSO 是否启用
func (s *SSOService) Enabled() bool { return s.enabled }

// Adapter 获取指定 provider 的适配器
func (s *SSOService) Adapter(provider string) (sso.Adapter, bool) {
	a, ok := s.adapters[provider]
	return a, ok
}

// SetProviderHTTPClient 注入 HTTP 客户端到所有 provider（测试用 mock；生产不调用）
func (s *SSOService) SetProviderHTTPClient(client *http.Client) {
	for _, a := range s.adapters {
		a.OIDC().SetHTTPClient(client)
	}
}

// ListProviders 列出已启用的 SSO 提供方（稳定排序）
func (s *SSOService) ListProviders() []ProviderInfo {
	out := make([]ProviderInfo, 0, len(s.adapters))
	for name, a := range s.adapters {
		out = append(out, ProviderInfo{
			Name:        name,
			DisplayName: a.DisplayName(),
			LoginURL:    "/api/sso/login/" + name,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// HandleCallback 处理 IdP 回调：
//  1. 交换授权码 → token
//  2. 验证 ID Token（签名 / issuer / audience / exp）
//  3. 归一化 claims → 关联本地用户（或自动创建）
//  4. 签发本地 JWT
func (s *SSOService) HandleCallback(ctx context.Context, provider, code, verifier string) (*SSOLoginResult, error) {
	if !s.enabled {
		return nil, ErrSSONotEnabled
	}
	adapter, ok := s.adapters[provider]
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

	user, isNew, err := s.resolveUser(ctx, nu)
	if err != nil {
		return nil, err
	}

	// 多租户 P0：SSO 签发同样带齐 scope 三件（与登录/刷新/MFA 同口径）。
	token, err := s.jwtUtils.GenerateTokenWithScope(
		user.ID, user.Username, user.Role, user.DataScope, user.DepartmentID, user.TeamID)
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

func (s *SSOService) resolveUser(ctx context.Context, nu *sso.NormalizedUser) (*model.SystemUser, bool, error) {
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

	cfg := s.adapters[nu.Provider].OIDC().Config()
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
		if exists, _ := s.userRepo.EmailExists(ctx, email, 0); exists {
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
