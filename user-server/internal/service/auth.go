package service

import (
	"context"
	"errors"
	"strings"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/utils"
	"hivemtk-user/internal/pkg/utils/bcrypt"
	"hivemtk-user/internal/pkg/utils/logger"
	"hivemtk-user/internal/repository"
	"hivemtk-user/internal/system/install"

	"gorm.io/gorm"
)

// LoginRequest 登录请求
type LoginRequest struct {
	Username string `json:"username" binding:"required"`
	Password string `json:"password" binding:"required"`
}

// LoginResponse 登录响应
type LoginResponse struct {
	Token     string              `json:"token,omitempty"`
	User      *SystemUserResponse `json:"user,omitempty"`
	Expires   int64               `json:"expires,omitempty"`
	NeedMFA   bool                `json:"need_mfa,omitempty"`
	TempToken string              `json:"temp_token,omitempty"`
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
//  1. 不再"系统无用户 → 自动注册为超管"——该机制绕过 InitGuard/LicenseGuard，
//     导致未绑 License 即可创建超管，摧毁安全模型。
//  2. 必须先有用户（由 InitSetup 创建）才能登录。
//  3. 用户名/密码错误一律返回"用户名或密码错误"（防枚举）。
//  4. 用户被禁用直接拒绝（明确反馈）。
//  5. 密码用 bcrypt 验证。
//  6. 阶段 1：team_users 已被合并到 system_users，仅查 system_users。
func (s *AuthService) Login(ctx context.Context, req *LoginRequest) (*LoginResponse, error) {

	user, err := s.systemUserRepo.GetByUsername(ctx, req.Username)
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		logger.Error(err, "查询用户失败")
		return nil, errors.New("登录失败，请稍后重试")
	}

	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, errors.New("用户名或密码错误")
	}

	if user.Status != 1 || !user.Enabled {
		return nil, errors.New("用户已被禁用")
	}

	if !CheckPassword(user, req.Password) {
		return nil, errors.New("用户名或密码错误")
	}

	return s.loginWithUser(ctx, user)
}

func (s *AuthService) loginWithUser(ctx context.Context, user *model.SystemUser) (*LoginResponse, error) {
	mfaSvc := NewMFAService()
	mfaEnabled, err := mfaSvc.IsMFAEnabled(ctx, user.ID)
	if err != nil {
		logger.Errorf("MFA 状态查询失败: %v", err)
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

	token, err := s.jwtUtils.GenerateToken(user.ID, user.Username, user.Role)
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

	return response, nil
}

func (s *AuthService) RefreshToken(ctx context.Context, tokenString string) (string, error) {
	if utils.IsJWTBlacklisted(tokenString) {
		return "", errors.New("token 已失效")
	}
	newToken, err := s.jwtUtils.RefreshToken(tokenString)
	if err != nil {
		return "", err
	}
	utils.BlacklistJWT(tokenString)

	return newToken, nil
}

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
// 修复：强制走密码策略校验 + 记录历史
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

	return nil
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

	if exists, _ := s.systemUserRepo.UsernameExists(ctx, username, 0); exists {
		return nil, ErrUsernameExists
	}
	if exists, _ := s.systemUserRepo.EmailExists(ctx, email, 0); exists {
		return nil, ErrEmailExists
	}

	user := &model.SystemUser{
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

	token, err := s.jwtUtils.GenerateToken(user.ID, user.Username, user.Role)
	if err != nil {
		logger.Error(err, "Register 生成 JWT 失败: "+username)
		return nil, errors.New("注册成功但签发令牌失败，请登录")
	}

	now := time.Now()
	user.LastLogin = &now
	if err := s.systemUserRepo.Update(ctx, user); err != nil {
		logger.Error(err, "Register 更新最后登录时间失败")
	}

	logger.Info("Register 用户注册成功: " + username)
	return &LoginResponse{
		Token:   token,
		User:    s.toUserResponse(ctx, user),
		Expires: time.Now().Add(time.Hour * time.Duration(utils.DefaultJWTConfig.ExpiresHours)).Unix(),
	}, nil
}

// CheckPassword 校验 SystemUser 密码
func CheckPassword(u *model.SystemUser, password string) bool {
	return model.CheckSystemUserPassword(u, password)
}

// HashPassword 对明文密码进行 bcrypt 哈希
func HashPassword(password string) (string, error) {
	return bcrypt.HashPassword(password)
}

// InitAdmin 初始化系统首个超管（公开，无 JWT）
//
// 系统用户统一 plan v3.1 §3.2：
//   - 调用方必须在请求体中传入 username/password/email（不再读 config 默认值）
//   - 密码强度：至少 8 位，含大小写字母 + 数字
//   - username 唯一性、email 唯一性（防重复初始化由路由层 install.lock 闸负责，见 admin_routes.go）
//   - 创建后写 install.lock（AdminUsername + Initialized=true），作为"已初始化"标记
//
// 失败语义：
//   - 已初始化（install.lock 存在）→ 由路由层返回 403 禁止重复创建，不到达本方法
//   - 密码强度不达标 → 复用 service.validatePassword 返回的错误
//   - username/email 冲突 → 透传 repo 错误
func (s *AuthService) InitAdmin(ctx context.Context, username, password, email string) error {
	username = strings.TrimSpace(username)
	email = strings.TrimSpace(email)
	if username == "" || password == "" || email == "" {
		return errors.New("username/password/email 均不能为空")
	}

	if err := validatePassword(password); err != nil {
		return err
	}

	if err := validateEmail(email); err != nil {
		return err
	}

	if err := validateUsername(username); err != nil {
		return err
	}

	if exists, _ := s.systemUserRepo.UsernameExists(ctx, username, 0); exists {
		return errors.New("用户名已存在")
	}
	if exists, _ := s.systemUserRepo.EmailExists(ctx, email, 0); exists {
		return errors.New("邮箱已被使用")
	}

	user := &model.SystemUser{
		Username: username,
		Password: password,
		Email:    email,
		Role:     model.SystemUserRoleAdmin,
		Status:   1,
		Enabled:  true,
	}
	if err := s.systemUserRepo.Create(ctx, user); err != nil {
		logger.Error(err, "InitAdmin 创建超管失败")
		return errors.New("创建超管失败: " + err.Error())
	}

	if err := install.MarkAdminInitialized(username); err != nil {
		logger.Error(err, "InitAdmin 写 install.lock 失败")
	}

	logger.Info("InitAdmin 超管创建成功: " + username)
	return nil
}
