package service

import (
	"context"
	cryptorand "crypto/rand"
	"errors"
	"fmt"
	"strings"
	"time"

	"hivemtk-user/internal/config"
	bizerr "hivemtk-user/internal/domain/errors"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/pkg/utils"
	"hivemtk-user/internal/pkg/utils/bcrypt"
	"hivemtk-user/internal/pkg/utils/fingerprint"
	"hivemtk-user/internal/pkg/utils/logger"
	"hivemtk-user/internal/platform"
	"hivemtk-user/internal/repository"
	"hivemtk-user/internal/system/install"

	"gorm.io/gorm"
)

// LoginRequest 登录请求
//
// 登录通道双轨（specs/001-saas-multi-tenant T031，contracts/api.md）：
//   - username：租户内唯一，跨租户可同名 → on 模式歧义时报 4108 提示改用邮箱
//   - email：跨租户全局唯一（global_uniq_system_users_email 索引兜底），消解上述歧义
//
// 两者必须至少提供一个（binding required_without 双向约束）；
// 离线模式（TENANT_MODE=off）两条通道语义一致。
type LoginRequest struct {
	Username   string `json:"username" binding:"required_without=Email"`
	Email      string `json:"email" binding:"required_without=Username"`
	Password   string `json:"password" binding:"required"`
	TenantCode string `json:"tenant_code,omitempty"` // 租户短码（on 模式可选，提供则按租户 scope 查用户名）
}

// LoginResponse 登录响应
type LoginResponse struct {
	Token     string              `json:"token,omitempty"`
	User      *SystemUserResponse `json:"user,omitempty"`
	Expires   int64               `json:"expires,omitempty"`
	NeedMFA   bool                `json:"need_mfa,omitempty"`
	TempToken string              `json:"temp_token,omitempty"`
	// 租户归属（off 模式省略，存量前端零适配）
	TenantID   uint   `json:"tenant_id,omitempty"`
	TenantCode string `json:"tenant_code,omitempty"`
}

// SystemUserResponse 系统用户响应
type SystemUserResponse struct {
	ID          uint       `json:"id"`
	Username    string     `json:"username"`
	Email       string     `json:"email"`
	Phone       string     `json:"phone"`
	RealName    string     `json:"real_name"`
	Role        string     `json:"role"`
	Status      int        `json:"status"`
	LastLogin   *time.Time `json:"last_login"`
	LastLoginAt *time.Time `json:"last_login_at"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
}

// RegisterResponse 注册响应
type RegisterResponse struct {
	Token string              `json:"token,omitempty"`
	User  *SystemUserResponse `json:"user,omitempty"`
}

// ChangePasswordRequest 修改密码请求
type ChangePasswordRequest struct {
	OldPassword string `json:"old_password" binding:"required"`
	NewPassword string `json:"new_password" binding:"required,min=8"`
}

// RegisterRequest 用户自助注册请求
type RegisterRequest struct {
	Username string `json:"username" binding:"required,min=3,max=32"`
	Password string `json:"password" binding:"required,min=8,max=64"`
	Email    string `json:"email" binding:"required,email"`
	Phone    string `json:"phone,omitempty"`
	RealName string `json:"real_name,omitempty"`
	// TenantCode 加入哪个租户（on 模式必填：自助注册只在租户内加人，
	// 不创建新租户；开户走 /api/tenant/register）
	TenantCode string `json:"tenant_code,omitempty"`

	// ---- 开户登记（平台监管责任）----
	// 需求原文：租户开户必须收集「电话，邮箱，公司名称，负责人等」。
	// 走 /api/auth/register 这条路（单机部署的默认注册入口）时同样必须收 ——
	// 否则用户从自助注册进来的商户在平台端就是一条查不到人的记录。
	CompanyName string `json:"company_name"`
	LicenseNo   string `json:"license_no"`
	Address     string `json:"address"`
	// RegulatoryAck 用户已勾选监管告知
	RegulatoryAck bool `json:"regulatory_ack"`
	// AgreementVersion 用户同意的协议版本
	AgreementVersion string `json:"agreement_version"`
}

// AuthService 认证服务
type AuthService struct {
	jwtUtils       *utils.JWTUtils
	systemUserRepo repository.SystemUserRepository
}

// NewAuthService 创建认证服务实例
func NewAuthService() *AuthService {
	return &AuthService{
		jwtUtils:       utils.NewJWTUtils(utils.DefaultJWTConfig),
		systemUserRepo: repository.NewSystemUserRepository(),
	}
}

// JwtUtils 获取 JWT 工具实例（用于测试）
func (s *AuthService) JwtUtils(ctx context.Context) *utils.JWTUtils {
	return s.jwtUtils
}

// Login 用户登录
//
// 严格规则（修复）：
//  1. 不再"系统无用户 → 自动注册为超管"——该机制绕过 InitGuard，
//     导致系统未完成初始化即可创建超管，摧毁安全模型。
//  2. 必须先有用户（由 InitSetup 创建）才能登录。
//  3. 用户名/密码错误一律返回"用户名或密码错误"（防枚举）。
//  4. 用户被禁用直接拒绝（明确反馈）。
//  5. 密码用 bcrypt 验证。
//  6. 阶段 1：team_users 已被合并到 system_users，仅查 system_users。
//  7. SaaS 多租户（T031）：邮箱全局通道优先；用户名跨租户歧义时不猜，报 4108。
func (s *AuthService) Login(ctx context.Context, req *LoginRequest) (*LoginResponse, error) {
	user, err := s.resolveLoginIdentity(ctx, req)
	if err != nil {
		return nil, err
	}

	if user.Status != 1 || !user.Enabled {
		return nil, errors.New("用户已被禁用")
	}

	if !CheckPassword(user, req.Password) {
		return nil, errors.New("用户名或密码错误")
	}

	return s.loginWithUser(ctx, user)
}

// resolveLoginIdentity 解析登录身份（tenant_code 定向通道优先，其次邮箱，最后用户名）
//
// 登录发生在租户未知之前，故查询一律走仓储的 SystemScope 变体，
// 租户由命中的用户行自身携带，绝不由 ctx 假定。
func (s *AuthService) resolveLoginIdentity(ctx context.Context, req *LoginRequest) (*model.SystemUser, error) {
	// tenant_code 定向登录通道（on 模式，契约 contracts/api.md）：
	// 显式给出租户短码时优先于无方向通道，短码本身不是秘密（非防枚举对象），
	// 用户名/密码仍是。短码不存在 → 4106（与其他租户错误码同一暴露口径）。
	if code := strings.TrimSpace(req.TenantCode); code != "" && tenant.IsMultiTenant() {
		t, err := repository.NewTenantRepository().GetByCode(ctx, code)
		if err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil, bizerr.New(bizerr.CodeTenantNotFound, "租户不存在或代码错误")
			}
			logger.Error(err, "按短码查询租户失败")
			return nil, errors.New("登录失败，请稍后重试")
		}

		// 邮箱通道：先做全局解析（邮箱跨租户全局唯一），再校验归属租户；
		// 归属不符一律回"用户名或密码错误"，不暴露"该邮箱属于另一租户"。
		if email := strings.TrimSpace(req.Email); email != "" {
			user, err := s.systemUserRepo.ResolveByEmailGlobal(ctx, email)
			if err != nil {
				if errors.Is(err, gorm.ErrRecordNotFound) {
					return nil, errors.New("用户名或密码错误")
				}
				logger.Error(err, "按邮箱查询用户失败")
				return nil, errors.New("登录失败，请稍后重试")
			}
			if user.TenantID != t.ID {
				return nil, errors.New("用户名或密码错误")
			}
			return user, nil
		}

		// 用户名通道：租户内唯一即可定位，无跨租户歧义问题（4108 不适用）
		user, err := s.systemUserRepo.FindByUsernameInTenant(ctx, t.ID, strings.TrimSpace(req.Username))
		if err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil, errors.New("用户名或密码错误")
			}
			logger.Error(err, "按租户内用户名查询用户失败")
			return nil, errors.New("登录失败，请稍后重试")
		}
		return user, nil
	}

	if email := strings.TrimSpace(req.Email); email != "" {
		user, err := s.systemUserRepo.ResolveByEmailGlobal(ctx, email)
		if err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil, errors.New("用户名或密码错误")
			}
			logger.Error(err, "按邮箱查询用户失败")
			return nil, errors.New("登录失败，请稍后重试")
		}
		return user, nil
	}

	candidates, err := s.systemUserRepo.FindByUsernameAnyTenant(ctx, req.Username, 2)
	if err != nil {
		logger.Error(err, "按用户名查询用户失败")
		return nil, errors.New("登录失败，请稍后重试")
	}
	switch len(candidates) {
	case 0:
		return nil, errors.New("用户名或密码错误")
	case 1:
		return candidates[0], nil
	default:
		// 跨租户同名：用户名无法唯一定位租户，不猜（防登录到错误租户）
		return nil, bizerr.New(bizerr.CodeTenantAmbiguousUsername,
			"该用户名在多个租户中存在，请改用邮箱登录")
	}
}

// bindUserTenant 把已解析出的用户行归属租户挂到 ctx 上（off 模式直通）。
// 0 值遗留行回落默认租户，与 tokenForUser 的签发口径一致。
func bindUserTenant(ctx context.Context, user *model.SystemUser) context.Context {
	if !tenant.IsMultiTenant() || user == nil {
		return ctx
	}
	tid := user.TenantID
	if tid == 0 {
		tid = tenant.DefaultTenantID
	}
	return tenant.OnlyTenant(ctx, tid)
}

// tokenForUser 按用户实际归属租户签发 JWT（specs/001-saas-multi-tenant T031）
//
// 与 GenerateToken 的唯一差异是 tenantID：dataScope/dept/team 仍传空与 0，
// 保持既有行级权限语义不变。off 模式用户行 tenant_id 恒为 1（存量回填值），
// 为 0 的遗留行回落到默认租户，避免签出 tenant_id=0 的越权令牌。
func (s *AuthService) tokenForUser(user *model.SystemUser) (string, error) {
	tid := user.TenantID
	if tid == 0 {
		tid = tenant.DefaultTenantID
	}
	return s.jwtUtils.GenerateTokenForTenant(user.ID, user.Username, user.Role, "", 0, 0, tid)
}

// loginTenant 解析登录用户的租户状态：off 模式不查租户表（存量部署 tenants 可为空）
//
// on 模式返回 (code, err)：租户不存在 4106、已停用 4107（登录受限、数据保留）。
func (s *AuthService) loginTenant(ctx context.Context, user *model.SystemUser) (uint, string, error) {
	if !tenant.IsMultiTenant() {
		tid := user.TenantID
		if tid == 0 {
			tid = tenant.DefaultTenantID
		}
		return tid, "", nil
	}
	tid := user.TenantID
	if tid == 0 {
		tid = tenant.DefaultTenantID
	}
	tenantRepo := repository.NewTenantRepository()
	t, err := tenantRepo.GetByID(ctx, tid)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return 0, "", bizerr.New(bizerr.CodeTenantNotFound, "租户不存在")
		}
		logger.Error(err, "查询租户失败")
		return 0, "", errors.New("登录失败，请稍后重试")
	}
	if !model.TenantIsActive(t) {
		return 0, "", bizerr.New(bizerr.CodeTenantSuspended, "租户已停用，请联系平台运营")
	}
	return t.ID, t.Code, nil
}

func (s *AuthService) loginWithUser(ctx context.Context, user *model.SystemUser) (*LoginResponse, error) {
	// 身份已按全局通道解析出来，后续所有以 user_id 为主键的租户表读写
	// （user_mfa / system_users.last_login / login_events）都必须挂在用户
	// 实际归属租户上下文下：on 模式无租户绑定时隔离闸 fail-closed，错误被
	// 吞掉就会把"MFA 查询失败"当成"MFA 未启用"直接放行。
	ctx = bindUserTenant(ctx, user)

	mfaSvc := NewMFAService()
	mfaEnabled, err := mfaSvc.IsMFAEnabled(ctx, user.ID)
	if err != nil {
		logger.Errorf("MFA 状态查询失败: %v", err)
		// fail-closed：查不出 MFA 状态时绝不能当"未启用"处理（绕过二次验证）。
		return nil, errors.New("登录失败，请稍后重试")
	}
	if mfaEnabled {
		tempToken, err := mfaSvc.IssueTempToken(ctx, user.ID, user.Username, user.Role)
		if err != nil {
			return nil, errors.New("登录失败，请稍后重试")
		}
		return &LoginResponse{
			NeedMFA:   true,
			TempToken: tempToken,
		}, nil
	}

	// 租户停用拦截放在密码校验之后：未通过认证的调用方不应探测到租户状态
	tenantID, tenantCode, err := s.loginTenant(ctx, user)
	if err != nil {
		return nil, err
	}

	token, err := s.tokenForUser(user)
	if err != nil {
		logger.Error(err, "生成JWT令牌失败")
		return nil, errors.New("登录失败，请稍后重试")
	}

	now := time.Now()
	user.LastLogin = &now
	if err := s.systemUserRepo.Update(ctx, user); err != nil {
		logger.Error(err, "更新用户最后登录时间失败")
	}

	response := &LoginResponse{
		Token:   token,
		User:    s.toUserResponse(ctx, user),
		Expires: time.Now().Add(time.Hour * time.Duration(utils.DefaultJWTConfig.ExpiresHours)).Unix(),
	}
	if tenant.IsMultiTenant() {
		response.TenantID = tenantID
		response.TenantCode = tenantCode
	}

	return response, nil
}

// RefreshToken 续期令牌。
//
// 走 utils.RefreshToken 时**只把旧令牌里的 claims 原样
// 抄进新令牌**，全程不回源——后果是被禁用/被删除/被降权的账号只要每 24h 刷一次，
// 就能带着过期的 role/data_scope 无限续期，"24 小时暴露面"实际是"永久暴露面"。
// 现在改为：吊销位判定 → 回源查账号 → 状态守卫 → 用**当前**库内角色/数据范围重新签发。
func (s *AuthService) RefreshToken(ctx context.Context, tokenString string) (string, error) {
	if utils.IsJWTBlacklisted(tokenString) {
		return "", errors.New("token 已失效")
	}
	claims, err := s.jwtUtils.ParseToken(tokenString)
	if err != nil {
		return "", err
	}
	if utils.IsTokenRevoked(ctx, claims.UserID, claims.IssuedAt) {
		return "", errors.New("账号状态已变更，请重新登录")
	}

	user, err := s.systemUserRepo.GetByID(ctx, claims.UserID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return "", errors.New("账号不存在或已被删除，请重新登录")
		}
		logger.Error(err, "刷新令牌时查询用户失败")
		return "", errors.New("刷新令牌失败，请稍后重试")
	}
	if user.Status != 1 || !user.Enabled {
		return "", errors.New("用户已被禁用")
	}

	// 刷新必须保留用户的真实租户：GenerateTokenWithScope 硬编码默认租户（tenant_id=1），
	// 会让非默认租户用户在刷新后"掉进"1 号租户（越权读默认租户数据）。
	// 与登录签发（tokenForUser）同口径：0 值遗留行回落默认租户，避免签出 tenant_id=0。
	tid := user.TenantID
	if tid == 0 {
		tid = tenant.DefaultTenantID
	}
	newToken, err := s.jwtUtils.GenerateTokenForTenant(
		user.ID, user.Username, user.Role, user.DataScope, user.DepartmentID, user.TeamID, tid)
	if err != nil {
		logger.Error(err, "刷新令牌签发失败")
		return "", errors.New("刷新令牌失败，请稍后重试")
	}
	// 一次性轮换：旧令牌立即拉黑，防止"刷新后新旧两把令牌并行可用"
	utils.BlacklistJWT(tokenString)

	return newToken, nil
}

// Logout 登出：仅拉黑当前这一把令牌。
//
// 刻意不做"按用户全端吊销"：RevokeUserTokens 会把同账号其它设备/浏览器的会话一并踢掉，
// 属于"登出=登出所有设备"的产品语义变更，未经拍板不单方面改变（第十八轮登记）。
// 按用户维度的即时吊销只挂在禁用/删除/改角色/改密这类安全事件上。
func (s *AuthService) Logout(ctx context.Context, tokenString string) error {
	utils.BlacklistJWT(tokenString)
	return nil
}

// GetCurrentUser 获取当前用户信息
func (s *AuthService) GetCurrentUser(ctx context.Context, userID uint) (*SystemUserResponse, error) {
	user, err := s.systemUserRepo.GetByID(ctx, userID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errors.New("用户不存在")
		}
		logger.Error(err, "查询用户失败")
		return nil, errors.New("获取用户信息失败")
	}

	return s.toUserResponse(ctx, user), nil
}

// initialAdminID 初始超管的固定用户 ID（system_init 初始化向导创建的第一个账号）。
// 该账号的密码受系统级保护：所有改密入口（自助改密/管理员重置/邮件重置）均拒绝。
const initialAdminID uint = 1

// ErrInitialAdminProtected 初始超管账号（id=1）受保护，禁止自助改密
var ErrInitialAdminProtected = errors.New("初始超管账号的密码不允许通过此入口修改（系统级保护）")

// ChangePassword 修改密码
// 强制走密码策略校验 + 记录改密留痕
// 保护：初始超管（id=1）密码不允许通过任何入口修改，防止密码被改后无法登录
func (s *AuthService) ChangePassword(ctx context.Context, userID uint, req *ChangePasswordRequest) error {
	if userID == initialAdminID {
		return ErrInitialAdminProtected
	}
	user, err := s.systemUserRepo.GetByID(ctx, userID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return errors.New("用户不存在")
		}
		logger.Error(err, "查询用户失败")
		return errors.New("修改密码失败")
	}

	if !CheckPassword(user, req.OldPassword) {
		return errors.New("原密码不正确")
	}

	policySvc := NewPasswordPolicyService()
	if err := policySvc.ValidatePassword(ctx, req.NewPassword, userID); err != nil {
		return err
	}

	hashed, err := HashPassword(req.NewPassword)
	if err != nil {
		logger.Error(err, "密码加密失败")
		return errors.New("修改密码失败")
	}
	user.Password = hashed

	if err := s.systemUserRepo.Update(ctx, user); err != nil {
		logger.Error(err, "保存用户失败")
		return errors.New("修改密码失败")
	}

	if err := policySvc.RecordPasswordHistory(ctx, userID, req.NewPassword, model.PasswordSourceChangePassword); err != nil {
		logger.Errorf("记录密码历史失败（不影响改密流程）: %v", err)
	}

	// 改密成功即按用户维度吊销：controller 已经拉黑了"手上这把"令牌（说明本流程的
	// 既定语义就是要求重新登录），但同账号在别处签发的令牌此前仍能活到自然过期——
	// 密码被爆破/泄露后用户改密自救，攻击者会话不该继续存在。
	utils.RevokeUserTokens(ctx, userID)

	return nil
}

// IssueTokenForUserID 按用户 ID 签发携带租户的 JWT（MFA 二次验证等只有 userID 的路径）
//
// MFA 临时令牌不携带租户归属，故终令牌在此处按用户行解析，
// 避免 MFA 登录用户在 SaaS 模式下丢失 tenant_id claim（T031 收口）。
func (s *AuthService) IssueTokenForUserID(ctx context.Context, userID uint) (string, error) {
	user, err := s.systemUserRepo.GetByID(tenant.WithSystemScope(ctx), userID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return "", errors.New("用户不存在")
		}
		logger.Error(err, "查询用户失败")
		return "", errors.New("签发令牌失败")
	}
	return s.tokenForUser(user)
}

// IssueTokenWithTenant 按用户 ID 签发携带租户的 JWT，并回带租户归属（MFA 登录第二步用）
//
// IssueTokenForUserID 的扩展：MFA 校验成功后的响应此前只有 token + 精简 user
// （id/username/role），前端拿不到 tenant_id/tenant_code，与普通登录响应字段不对齐
// —— SaaS 模式下 MFA 用户登录后落库的租户归属是空的。
// 租户口径与 loginWithUser 完全一致（复用 loginTenant）：off 模式不查租户表
// （存量部署 tenants 可为空），on 模式校验租户存在且启用（4106/4107）。
func (s *AuthService) IssueTokenWithTenant(ctx context.Context, userID uint) (string, uint, string, error) {
	user, err := s.systemUserRepo.GetByID(tenant.WithSystemScope(ctx), userID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return "", 0, "", errors.New("用户不存在")
		}
		logger.Error(err, "查询用户失败")
		return "", 0, "", errors.New("签发令牌失败")
	}
	tenantID, tenantCode, err := s.loginTenant(ctx, user)
	if err != nil {
		return "", 0, "", err
	}
	token, err := s.tokenForUser(user)
	if err != nil {
		logger.Error(err, "生成JWT令牌失败")
		return "", 0, "", errors.New("签发令牌失败")
	}
	return token, tenantID, tenantCode, nil
}

func (s *AuthService) toUserResponse(ctx context.Context, user *model.SystemUser) *SystemUserResponse {
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

// ErrUsernameExists 用户名已存在
var ErrUsernameExists = errors.New("用户名已存在")

// ErrEmailExists 邮箱已被使用
var ErrEmailExists = errors.New("邮箱已被使用")

// Register 用户自主注册
//
// 设计要点（plan v3.1 自助服务）：
//   - 默认角色为 user（普通用户），非 admin
//   - 严格校验：username/password/email 唯一性 + 强度
//   - 注册成功直接签发 JWT，避免二次登录
//   - 自助注册不应绕过 InitGuard：若系统尚未初始化，直接拒绝
//
// 返回 LoginResponse 与登录流程结构一致，便于上层复用。
func (s *AuthService) Register(ctx context.Context, req *RegisterRequest) (*LoginResponse, error) {
	// 自助注册只在租户内加人：on 模式没有租户绑定就落库 = 隔离闸 fail-closed，
	// 与其让用户看到一条库错误，不如在入口说清楚缺什么。
	tenantID, boundTenant := tenant.FromContext(ctx)
	if tenant.IsMultiTenant() && !boundTenant {
		return nil, errors.New("多租户模式下注册必须提供有效的租户短码")
	}

	username := strings.TrimSpace(req.Username)
	email := strings.TrimSpace(req.Email)
	password := req.Password
	if username == "" || email == "" || password == "" {
		return nil, errors.New("用户名/邮箱/密码均不能为空")
	}

	if err := validateUsername(username); err != nil {
		return nil, err
	}
	if err := validatePassword(password); err != nil {
		return nil, err
	}
	if err := validateEmail(email); err != nil {
		return nil, err
	}

	// 用户名租户内唯一（callback 按 ctx 限定）；邮箱跨租户全局唯一
	// （global_uniq_system_users_email 索引）——按租户查重会放过别的租户
	// 已占用的邮箱，最终撞唯一索引变成 500。
	if exists, _ := s.systemUserRepo.UsernameExists(ctx, username, 0); exists {
		return nil, ErrUsernameExists
	}
	if exists, _ := s.systemUserRepo.EmailExistsGlobal(ctx, email); exists {
		return nil, ErrEmailExists
	}

	// 开户登记必填校验（与自助开户同一口径）
	if err := validateSelfRegisterFields(*req); err != nil {
		return nil, err
	}

	user := &model.SystemUser{
		TenantID: tenantID,
		Username: username,
		Password: password,
		Email:    email,
		Phone:    strings.TrimSpace(req.Phone),
		RealName: strings.TrimSpace(req.RealName),
		Role:     model.SystemUserRoleUser,
		Status:   1,
		Enabled:  true,
	}
	if err := s.systemUserRepo.Create(ctx, user); err != nil {
		logger.Error(err, "Register 创建用户失败: "+username)
		return nil, errors.New("注册失败: " + err.Error())
	}

	if user.Email != "" {
		emailSvc := NewEmailServiceAuto()
		welcomeBody := "欢迎加入 HiveMTK！您的账号已成功创建。"
		if _, err := emailSvc.Send(ctx, 0, user.Email, "欢迎加入 HiveMTK", welcomeBody, nil); err != nil {
			logger.Ctx(ctx).Warn().Err(err).Str("email", user.Email).Msg("Register 欢迎邮件发送失败")
		}
	}

	token, err := s.tokenForUser(user)
	if err != nil {
		logger.Error(err, "Register 生成 JWT 失败: "+username)
		return nil, errors.New("注册成功但签发令牌失败，请登录")
	}

	now := time.Now()
	user.LastLogin = &now
	if err := s.systemUserRepo.Update(ctx, user); err != nil {
		logger.Error(err, "Register 更新最后登录时间失败")
	}

	// 开户登记上报平台（失败不回滚注册，理由见 TenantService.SubmitRegistrationToPlatform）
	if err := s.submitSelfRegisterToPlatform(ctx, user, *req); err != nil {
		logger.Warnf("[auth] 自助注册的平台登记上报失败（不影响注册本身）: %v", err)
	}

	logger.Info("Register 用户注册成功: " + username)
	resp := &LoginResponse{
		Token:   token,
		User:    s.toUserResponse(ctx, user),
		Expires: time.Now().Add(time.Hour * time.Duration(utils.DefaultJWTConfig.ExpiresHours)).Unix(),
	}
	// 租户归属随响应下发（on 模式）：自助注册是「加入已有商户」，租户由
	// req.TenantCode 解析而来（controller 的 resolvePublicTenantCtx 已绑 ctx）。
	// 不下发的话前端拿不到 tenant_code ⇒ 注册成功后无法把租户代码记下来，
	// 用户下次登录填不对就进不来。与 loginWithUser / MFA 第二步同口径。
	if tenant.IsMultiTenant() {
		tid := user.TenantID
		if tid == 0 {
			tid = tenant.DefaultTenantID
		}
		resp.TenantID = tid
		if t, err := repository.NewTenantRepository().GetByID(ctx, tid); err == nil {
			resp.TenantCode = t.Code
		} else {
			logger.Warnf("[auth] 自助注册后读取租户短码失败 tenant_id=%d: %v", tid, err)
		}
	}
	return resp, nil
}

// CheckPassword 校验 SystemUser 密码
func CheckPassword(u *model.SystemUser, password string) bool {
	return model.CheckSystemUserPassword(u, password)
}

// HashPassword 对明文密码进行 bcrypt 哈希
func HashPassword(password string) (string, error) {
	return bcrypt.HashPassword(password)
}

// ErrAdminAlreadyInitialized 首个超管只能铸一次。
// 文案与 response.ErrSystemAlreadyInitialized 一致，便于控制层原样透出并映射 403。
var ErrAdminAlreadyInitialized = errors.New("系统已初始化，禁止重复创建超管")

// assertFirstAdminPossible 判定"此刻还允许铸首个超管"。
//
// 两道依据，任一为真即拒绝：
//  1. install.lock 已标记 initialized——最快路径，也是安装向导走完的正常状态。
//  2. 库里已存在 role=admin 的账号——真相源。lock 只是缓存：它可能被删、写坏，
//     而 install.GetStatus 在"文件不可用 + 数据库探针不可用"时会把这种实例读成
//     NOT_INSTALLED（探针未装配时返回空串，与"库里真没超管"同形），
//     只看第 1 条就会给匿名建号留一个窗口。
//
// 计数查询本身失败也拒绝：无法证明"还没有超管"时，不放行建超管。
func (s *AuthService) assertFirstAdminPossible(ctx context.Context) error {
	if install.GetStatus().Initialized {
		return ErrAdminAlreadyInitialized
	}
	admins, err := s.systemUserRepo.CountAdmins(ctx)
	if err != nil {
		return fmt.Errorf("核对既有超管账号失败，已拒绝创建: %w", err)
	}
	if admins > 0 {
		return ErrAdminAlreadyInitialized
	}
	return nil
}

// InitAdmin 初始化系统首个超管（公开，无 JWT）
//
// 系统用户统一 plan v3.1 §3.2：
//   - 调用方必须传入 username/password，email 选填（不再读 config 默认值）
//   - 密码强度：至少 8 位，含大小写字母 + 数字
//   - username 唯一性、email 唯一性
//   - "禁止重复初始化"在本方法开头断言（见 assertFirstAdminPossible），不依赖任何路由层守卫；
//     任何把本方法挂到公开前缀上的新入口都自动带上同一道门，不会再出现"某个别名忘了判"
//   - 创建后写 install.lock（AdminUsername + Initialized=true），作为"已初始化"标记
//
// 失败语义：
//   - 已有超管 / lock 已 initialized → ErrAdminAlreadyInitialized（调用方映射为 403）
//   - 超管计数查询失败 → 直接拒绝（fail-closed：宁可安装者重试一次，不放过第二个匿名超管）
//   - 密码强度不达标 → 复用 service.validatePassword 返回的错误
//   - username/email 冲突 → 透传 repo 错误
//
// 校验与落库拆成 prepareAdminForInit / insertPreparedAdmin 两步（对本方法而言
// 仍然连续调用，语义与拆分前一致）：on 模式必须在两步之间先落 tenants 行、
// 拿到新租户 ID 再写超管行，见 InitAdminWithRegistration 的 P0 注释。
func (s *AuthService) InitAdmin(ctx context.Context, username, password, email string) error {
	user, err := s.prepareAdminForInit(ctx, username, password, email)
	if err != nil {
		return err
	}
	return s.insertPreparedAdmin(ctx, user)
}

// initReadOnlyCtx 初始化流程中"全局口径只读 + 落库后回填"所用的 ctx。
//
// on 模式的公开初始化入口（router.go 的 public 组没有 TenantMiddleware）ctx 无
// 租户绑定，直连会被隔离 callback fail-closed。而这里覆盖的几条读写口径本来
// 就是全局的 —— 超管"全库只能铸一次"、用户名/邮箱唯一性跨租户、回填按刚建号
// 的用户名定位 —— 挂租户作用域反而会把别的租户的超管/同名账号漏掉。因此 on
// 模式显式挂 SystemScope（系统入口的唯一豁免通道）；off 模式原样返回，
// 直通路径逐字节不变。
func initReadOnlyCtx(ctx context.Context) context.Context {
	if tenant.IsMultiTenant() {
		return tenant.WithSystemScope(ctx)
	}
	return ctx
}

// prepareAdminForInit 校验并构造待插入的超管行（不落库、不写 install.lock）
//
// 从 InitAdmin 拆出的原因：on 模式要先知道新租户 ID 才能给超管行显式写
// TenantID，而 tenants 行必须建在**全部校验通过之后** —— 否则一个弱密码请求
// 就会在 on 模式留下一条永远配不上超管的孤儿登记行。判定顺序与拆分前逐条一致
// （先断言可建 → 字段校验 → 唯一性），既有桩测试的调用序列不变。
func (s *AuthService) prepareAdminForInit(ctx context.Context, username, password, email string) (*model.SystemUser, error) {
	guardCtx := initReadOnlyCtx(ctx)
	if err := s.assertFirstAdminPossible(guardCtx); err != nil {
		return nil, err
	}

	username = strings.TrimSpace(username)
	email = strings.TrimSpace(email)
	if username == "" || password == "" {
		return nil, errors.New("username/password 均不能为空")
	}

	if err := validatePassword(password); err != nil {
		return nil, err
	}

	if email != "" {
		if err := validateEmail(email); err != nil {
			return nil, err
		}
	}

	if err := validateUsername(username); err != nil {
		return nil, err
	}

	if exists, _ := s.systemUserRepo.UsernameExists(guardCtx, username, 0); exists {
		return nil, errors.New("用户名已存在")
	}
	if email != "" {
		if exists, _ := s.systemUserRepo.EmailExists(guardCtx, email, 0); exists {
			return nil, errors.New("邮箱已被使用")
		}
	}

	return &model.SystemUser{
		Username: username,
		Password: password,
		Email:    email,
		Role:     model.SystemUserRoleAdmin,
		Status:   1,
		Enabled:  true,
	}, nil
}

// insertPreparedAdmin 落超管行并写 install.lock。
//
// 顺序不可换：INSERT 必须先成功，"已初始化"标记才有意义 —— 建号失败而 lock
// 已写，实例会被自己的失败态锁死（只能手删 lock 才能重装）。
func (s *AuthService) insertPreparedAdmin(ctx context.Context, user *model.SystemUser) error {
	if err := s.systemUserRepo.Create(ctx, user); err != nil {
		logger.Error(err, "InitAdmin 创建超管失败")
		// %w 而不是字符串拼接：错误文案不变，但调用方仍能用 errors.Is 认出底层原因。
		return fmt.Errorf("创建超管失败: %w", err)
	}

	if err := install.MarkAdminInitialized(user.Username); err != nil {
		logger.Error(err, "InitAdmin 写 install.lock 失败")
	}

	logger.Info("InitAdmin 超管创建成功: " + user.Username)
	return nil
}

// ── 自助注册的监管登记 ────────────────────────────────────────────────

// validateSelfRegisterFields 校验自助注册的监管必需项
//
// 口径与 TenantService.validateRegulatoryFields 一致，刻意保持同一套判据：
// 两条注册入口（SaaS 自助开户 / 单机自助注册）如果要求不同，
// 用户会从"要求低的入口"绕过去，监管登记就形同虚设。
//
// 003 T012 口径收敛：公司/负责人/电话/**地址** 四项必填，邮箱/执照号选填
// （邮箱在本入口仍是登录凭据（binding required），这里只是不再把它算进
// 监管必填集；"联系不到人"的缺口由地址补上——电话失联时地址是唯一线索）。
func validateSelfRegisterFields(req RegisterRequest) error {
	var missing []string
	if strings.TrimSpace(req.CompanyName) == "" {
		missing = append(missing, "公司名称")
	}
	if strings.TrimSpace(req.RealName) == "" {
		missing = append(missing, "负责人姓名")
	}
	if strings.TrimSpace(req.Phone) == "" {
		missing = append(missing, "联系电话")
	}
	if strings.TrimSpace(req.Address) == "" {
		missing = append(missing, "联系地址")
	}
	if len(missing) > 0 {
		return bizerr.New(bizerr.CodeModuleRegistrationIncomplete,
			"开户信息不完整，缺少："+strings.Join(missing, "、"))
	}
	if !tenantPhonePattern.MatchString(strings.TrimSpace(req.Phone)) {
		return bizerr.New(bizerr.CodeModuleRegistrationIncomplete, "联系电话格式不正确")
	}
	if !req.RegulatoryAck {
		return bizerr.New(bizerr.CodeModuleRegistrationIncomplete,
			"Please read and confirm the platform regulatory notice first")
	}
	return nil
}

// submitSelfRegisterToPlatform 把自助注册的登记信息上报平台
//
// 离线部署（PLATFORM_ENABLED=false）：一个出站请求都不发，只在日志里记一笔。
// 这样"离线独立部署"与"SaaS 注册"走同一段代码，离线用户不会被卡在注册上，
// 平台端也就不会收到一堆 `reg_mode=offline` 的空登记。
func (s *AuthService) submitSelfRegisterToPlatform(ctx context.Context, u *model.SystemUser, req RegisterRequest) error {
	if !config.PlatformEnabled() {
		return nil
	}
	agreementVersion := strings.TrimSpace(req.AgreementVersion)
	if agreementVersion == "" {
		agreementVersion = serviceCurrentAgreementVersion
	}
	// merchant_key 与 install_id 是两个不同语义的标识，**不可混填**：
	// 前者是平台上的商业身份（模块授权/订单挂在它上面），
	// 后者是安装标识（心跳上报与 platform_installs 用的是它）。
	// 混填会让平台端无法把开户登记与心跳记录关联起来 —— 详见 tenant.go 里同处的注释。
	_, err := platform.NewModuleClient().RegisterTenant(ctx, platform.TenantRegistrationPayload{
		MerchantKey:      platform.GetMerchantKey(),
		InstallID:        currentInstallID(),
		CompanyName:      strings.TrimSpace(req.CompanyName),
		LegalPerson:      strings.TrimSpace(req.RealName),
		Phone:            strings.TrimSpace(req.Phone),
		Email:            u.Email,
		LicenseNo:        strings.TrimSpace(req.LicenseNo),
		Address:          strings.TrimSpace(req.Address),
		RegMode:          model.TenantRegModeSaaS,
		AgreementVersion: agreementVersion,
		RegulatoryAck:    true,
	})
	return err
}

// ============================================================
// 离线部署的负责人初始化 + 开户登记（任务三 · 三次审核轮 L10）
//
// ============================================================
// 需求原文：「注意离线独立部署情况，**也要有负责人初始化**，也要收集信息」。
//
// 这句话此前被漏掉的原因很具体：注册页有公司名/负责人/电话/邮箱（做了），
// 而离线用户**根本不走注册** —— 他们装完系统看到的是 /setup 初始化向导，
// 那条路径当时把四项全当选填。结果是：需求里唯一被点名的人群，
// 恰恰是唯一完全绕过监管信息收集的人群。
//
// 处置：
//  1. InitAdminRequest 的四项改为必填（控制器 binding 已收紧）；
//  2. InitAdminWithRegistration 建超管时同时写一条 tenants 行（reg_mode=offline），
//     并把负责人姓名/电话写进超管本人（登录后自己的资料里就能看到，不用另找入口）；
//  3. platform_sync=skipped —— 离线部署本来就连不上平台，
//     标成 failed 会让平台端出现一堆无法处理的红单。
// ============================================================

// ValidateOfflineRegistration 校验离线初始化的开户登记必填项
//
// 口径与另两条注册入口一致（SaaS 自助开户 / 单机自助注册），003 T012 收敛为：
// 公司名（或负责人所属组织）、负责人姓名、电话、**地址** 四项缺一不可；
// 邮箱/执照号选填（地址是电话失联时唯一剩下的定位线索，顶替邮箱的必填位）。
func (s *AuthService) ValidateOfflineRegistration(companyName, realName, phone, email, address string, ack bool) error {
	var missing []string
	if strings.TrimSpace(companyName) == "" {
		missing = append(missing, "公司/组织名称")
	}
	if strings.TrimSpace(realName) == "" {
		missing = append(missing, "负责人姓名")
	}
	if strings.TrimSpace(phone) == "" {
		missing = append(missing, "联系电话")
	}
	if strings.TrimSpace(address) == "" {
		missing = append(missing, "联系地址")
	}
	if len(missing) > 0 {
		return fmt.Errorf("开户信息不完整，缺少：%s", strings.Join(missing, "、"))
	}
	if !tenantPhonePattern.MatchString(strings.TrimSpace(phone)) {
		return errors.New("联系电话格式不正确")
	}
	if !ack {
		return errors.New("请先阅读并确认平台监管责任告知")
	}
	return nil
}

// InitAdminWithRegistration 初始化超管并落一条离线租户登记
//
// on / off 两种形态的落库顺序刻意不同（P0：TENANT_MODE=on 的初始化向导 400 修复）：
//
//	on：  全部校验 → 建 tenants 行（拿真实租户 ID）→ 显式 user.TenantID=该 ID
//	      → INSERT → install.lock → 回填负责人信息。
//	      public 组没有 TenantMiddleware，此处 ctx 无租户绑定，system_users 的
//	      INSERT 若不带显式 TenantID 会被 fillCreateForTenant fail-closed 拒绝；
//	      显式值走"行自证"放行路径 —— 与 SaaS 开户 CreateWithAdmin 同一模式
//	      （见 repository/tenant.go 的注释）。登记行失败即中止：没有租户归属的
//	      超管在 on 模式是悬空行，后续所有按租户隔离的写入都落不到它头上。
//	off：  全部校验 → INSERT → install.lock → 回填 → 建 tenants 行。
//	      顺序逐条一致；登记失败**不回滚超管**（留痕缺失可补，
//	      把已能登录的人挡在门外不可逆）。
//
// clientIP/userAgent：003 T010 静默合规采集 —— 服务端视角落 tenants 三列
// （init_ip / init_ua(sha256) / init_device_fingerprint），不做用户可见提示。
func (s *AuthService) InitAdminWithRegistration(ctx context.Context,
	username, password, email, realName, phone, companyName, licenseNo, address string,
	regulatoryAck bool, agreementVersion, clientIP, userAgent string) error {

	companyName = strings.TrimSpace(companyName)
	if companyName == "" {
		companyName = strings.TrimSpace(realName) + " 的私有部署"
	}
	if strings.TrimSpace(agreementVersion) == "" {
		agreementVersion = serviceCurrentAgreementVersion
	}

	// 先过全部校验再落任何一行：on 模式若先建 tenants 行、之后才发现密码太弱，
	// 就会留下一条永远配不上超管的孤儿登记行。
	user, err := s.prepareAdminForInit(ctx, username, password, email)
	if err != nil {
		return err
	}

	// 租户登记行：离线部署的唯一监管留痕
	t := &model.Tenant{
		Code:        "local-" + shortRandomKey(8),
		Name:        companyName,
		Status:      model.TenantStatusActive,
		Plan:        "free",
		CompanyName: companyName,
		LegalPerson: strings.TrimSpace(realName),
		Phone:       strings.TrimSpace(phone),
		Email:       strings.ToLower(strings.TrimSpace(email)),
		LicenseNo:   strings.TrimSpace(licenseNo),
		Address:     strings.TrimSpace(address),
		// reg_mode 按 TENANT_MODE 实际返回（003 T012）：on 模式的 /setup 初始化
		// 是云端 SaaS 形态的开户，不再是"离线独立部署"——硬编码 offline 会让
		// 平台端把云端首租户误记成离线商户。
		RegMode:          model.TenantRegModeOffline,
		PlatformSync:     model.TenantSyncSkipped,
		AgreementVersion: agreementVersion,
		RegulatoryAck:    regulatoryAck,
	}
	// 003 T010 静默合规采集：UA 只存 sha256 hex，指纹由 UA+IP 派生
	t.InitIP = strings.TrimSpace(clientIP)
	t.InitUA = fingerprint.HashHex(userAgent)
	t.InitDeviceFingerprint = fingerprint.FromClient(userAgent, clientIP)

	onMode := tenant.IsMultiTenant()
	if onMode {
		t.RegMode = model.TenantRegModeSaaS
		// P0：先落租户行拿 ID（tenants 非租户表，任何 ctx 都能写）。
		// 失败即中止 —— 此刻超管还没建，不存在"抛下无租户超管"的半态。
		if err := repository.NewTenantRepository().Create(ctx, t); err != nil {
			return fmt.Errorf("离线租户登记写入失败，已中止超管创建（on 模式超管必须有租户归属）: %w", err)
		}
		// 显式归属必须发生在 INSERT 之前：fillCreateForTenant 见到非零值即放行
		user.TenantID = t.ID
	}

	// 超管本人：负责人姓名与电话落在他自己的资料上（不用另找入口就能看到）
	if err := s.insertPreparedAdmin(ctx, user); err != nil {
		if onMode {
			// 校验都过了还写失败 = 库异常。租户行会留在库里（TenantRepository
			// 没有 Delete 口），重试会再建一条 —— 与"校验期失败"不同，这条
			// 只在库写入异常时发生，宁可留痕也不静默吞。
			logger.Errorf("[init-admin] on 模式超管创建失败，已留下先行写入的租户登记行（company=%s）：%v",
				t.CompanyName, err)
		}
		return err
	}
	s.backfillInitAdminProfile(ctx, user.Username, realName, phone)

	if !onMode {
		if err := repository.NewTenantRepository().Create(ctx, t); err != nil {
			// 登记失败**不回滚超管**：用户已经能登录了，为一条合规留痕
			// 把人挡在门外，代价远大于收益。留痕缺失是可补的，系统不可用是不可逆的。
			logger.Errorf("[init-admin] 离线租户登记写入失败（不影响超管创建）：%v", err)
			return nil
		}
	}
	logger.Infof("[init-admin] 开户登记完成（reg_mode=%s, tenant 行 ID=%d, on=%v）：company=%s legal_person=%s",
		t.RegMode, t.ID, onMode, t.CompanyName, t.LegalPerson)

	// 003 T013 开户赠送：失败仅 Warn 不阻断初始化（赠送可后补，开户不可逆）
	if t.ID != 0 {
		provisionTenantStarterKit(ctx, t.ID, companyName, repository.NewStarterKitRepositoryDefault())
		ProvisionTenantModules(ctx, t.ID)
	}
	return nil
}

// backfillInitAdminProfile 超管建号后回填负责人姓名/电话：失败只记日志
//
// （账号已可用，为两个非关键字段回滚账号是本末倒置）。
// on 模式挂 SystemScope（initReadOnlyCtx）：回填按刚建号的用户名定位，
// 口径是全局的，无租户绑定的 ctx 直连会 fail-closed。
func (s *AuthService) backfillInitAdminProfile(ctx context.Context, username, realName, phone string) {
	if strings.TrimSpace(realName) == "" && strings.TrimSpace(phone) == "" {
		return
	}
	repo := s.systemUserRepo
	lookup := initReadOnlyCtx(ctx)
	existing, err := repo.GetByUsername(lookup, username)
	if err != nil || existing == nil {
		logger.Warnf("[init-admin] 回填负责人信息失败（不影响创建）：%v", err)
		return
	}
	if realName != "" {
		existing.RealName = strings.TrimSpace(realName)
	}
	if phone != "" {
		existing.Phone = strings.TrimSpace(phone)
	}
	if err := repo.Update(lookup, existing); err != nil {
		logger.Warnf("[init-admin] 回填负责人信息失败（不影响创建）：%v", err)
	}
}

// shortRandomKey 生成短随机串（离线租户短码用）
func shortRandomKey(n int) string {
	const charset = "abcdefghijklmnopqrstuvwxyz0123456789"
	b := make([]byte, n)
	if _, err := cryptorand.Read(b); err != nil {
		for i := range b {
			b[i] = charset[i%len(charset)]
		}
	}
	out := make([]byte, n)
	for i, v := range b {
		out[i] = charset[int(v)%len(charset)]
	}
	return string(out)
}
