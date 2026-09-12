//go:build ignore
// +build ignore

package service

import (
	"context"
	"testing"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/utils"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"gorm.io/gorm"
)

// MockSystemUserRepository 模拟 SystemUserRepository
type MockSystemUserRepository struct {
	mock.Mock
}

func (m *MockSystemUserRepository) Create(ctx context.Context, u *model.SystemUser) error {
	args := m.Called(ctx, u)
	return args.Error(0)
}

func (m *MockSystemUserRepository) Update(ctx context.Context, u *model.SystemUser) error {
	args := m.Called(ctx, u)
	return args.Error(0)
}

func (m *MockSystemUserRepository) Delete(ctx context.Context, id uint) error {
	args := m.Called(ctx, id)
	return args.Error(0)
}

func (m *MockSystemUserRepository) GetByID(ctx context.Context, id uint) (*model.SystemUser, error) {
	args := m.Called(ctx, id)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*model.SystemUser), args.Error(1)
}

func (m *MockSystemUserRepository) GetByUsername(ctx context.Context, username string) (*model.SystemUser, error) {
	args := m.Called(ctx, username)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*model.SystemUser), args.Error(1)
}

func (m *MockSystemUserRepository) GetByEmail(ctx context.Context, email string) (*model.SystemUser, error) {
	args := m.Called(ctx, email)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*model.SystemUser), args.Error(1)
}

func (m *MockSystemUserRepository) List(ctx context.Context, page, pageSize int) ([]*model.SystemUser, int64, error) {
	args := m.Called(ctx, page, pageSize)
	return args.Get(0).([]*model.SystemUser), args.Get(1).(int64), args.Error(2)
}

func (m *MockSystemUserRepository) SearchUsers(ctx context.Context, keyword string, offset, limit int) ([]*model.SystemUser, int64, error) {
	args := m.Called(ctx, keyword, offset, limit)
	return args.Get(0).([]*model.SystemUser), args.Get(1).(int64), args.Error(2)
}

func (m *MockSystemUserRepository) Count(ctx context.Context) (int64, error) {
	args := m.Called(ctx)
	return args.Get(0).(int64), args.Error(1)
}

func (m *MockSystemUserRepository) UsernameExists(ctx context.Context, username string, excludeID uint) (bool, error) {
	args := m.Called(ctx, username, excludeID)
	return args.Bool(0), args.Error(1)
}

func (m *MockSystemUserRepository) EmailExists(ctx context.Context, email string, excludeID uint) (bool, error) {
	args := m.Called(ctx, email, excludeID)
	return args.Bool(0), args.Error(1)
}

func (m *MockSystemUserRepository) GetFirstAdminUsername(ctx context.Context) (string, error) {
	args := m.Called(ctx)
	return args.String(0), args.Error(1)
}

func (m *MockSystemUserRepository) GetUpdatedAt(ctx context.Context, userID uint) (*time.Time, error) {
	args := m.Called(ctx, userID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*time.Time), args.Error(1)
}

func (m *MockSystemUserRepository) ListByRole(ctx context.Context, role string, page, size int) ([]*model.SystemUser, int64, error) {
	args := m.Called(ctx, role, page, size)
	return args.Get(0).([]*model.SystemUser), args.Get(1).(int64), args.Error(2)
}

func (m *MockSystemUserRepository) CountByRole(ctx context.Context, role string) (int64, error) {
	args := m.Called(ctx, role)
	return args.Get(0).(int64), args.Error(1)
}

func (m *MockSystemUserRepository) CountAdmins(ctx context.Context) (int64, error) {
	args := m.Called(ctx)
	return args.Get(0).(int64), args.Error(1)
}

func (m *MockSystemUserRepository) DeleteSafe(ctx context.Context, id uint) error {
	args := m.Called(ctx, id)
	return args.Error(0)
}

func (m *MockSystemUserRepository) SetEnabled(ctx context.Context, id uint, enabled bool) error {
	args := m.Called(ctx, id, enabled)
	return args.Error(0)
}

func (m *MockSystemUserRepository) CountEnabledAdmins(ctx context.Context) (int64, error) {
	args := m.Called(ctx)
	return args.Get(0).(int64), args.Error(1)
}

func (m *MockSystemUserRepository) UpdatePassword(ctx context.Context, id uint, hashedPassword string) error {
	args := m.Called(ctx, id, hashedPassword)
	return args.Error(0)
}

// TestAuthService_Login_UserNotFound 测试用户不存在
func TestAuthService_Login_UserNotFound(t *testing.T) {
	mockRepo := new(MockSystemUserRepository)
	jwtUtils := utils.NewJWTUtils(utils.DefaultJWTConfig)

	authService := &AuthService{
		jwtUtils:       jwtUtils,
		systemUserRepo: mockRepo,
	}

	ctx := context.Background()

	mockRepo.On("GetByUsername", ctx, "nonexistent").Return(nil, gorm.ErrRecordNotFound)

	req := &LoginRequest{
		Username: "nonexistent",
		Password: "password",
	}

	resp, err := authService.Login(ctx, req)

	assert.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "用户名或密码错误")
	mockRepo.AssertExpectations(t)
}

// TestAuthService_Login_DisabledUser 测试禁用用户登录
func TestAuthService_Login_DisabledUser(t *testing.T) {
	mockRepo := new(MockSystemUserRepository)
	jwtUtils := utils.NewJWTUtils(utils.DefaultJWTConfig)

	authService := &AuthService{
		jwtUtils:       jwtUtils,
		systemUserRepo: mockRepo,
	}

	ctx := context.Background()
	user := &model.SystemUser{
		Model:    gorm.Model{ID: 1},
		Username: "disabled",
		Password: "$2a$10$dummyhash",
		Status:   0,
		Enabled:  false,
	}

	mockRepo.On("GetByUsername", ctx, "disabled").Return(user, nil)

	req := &LoginRequest{
		Username: "disabled",
		Password: "password",
	}

	resp, err := authService.Login(ctx, req)

	assert.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "用户已被禁用")
	mockRepo.AssertExpectations(t)
}

// TestAuthService_RefreshToken 测试刷新令牌
func TestAuthService_RefreshToken(t *testing.T) {
	jwtUtils := utils.NewJWTUtils(utils.DefaultJWTConfig)
	mockRepo := new(MockSystemUserRepository)

	authService := &AuthService{
		jwtUtils:       jwtUtils,
		systemUserRepo: mockRepo,
	}

	ctx := context.Background()
	user := &model.SystemUser{
		Model:    gorm.Model{ID: 1},
		Username: "testuser",
		Role:     "user",
	}

	token, err := jwtUtils.GenerateToken(user.ID, user.Username, user.Role)
	assert.NoError(t, err)

	newToken, err := authService.RefreshToken(ctx, token)

	assert.NoError(t, err)
	assert.NotEmpty(t, newToken)
}

// TestAuthService_GetCurrentUser_Success 测试获取当前用户成功
func TestAuthService_GetCurrentUser_Success(t *testing.T) {
	mockRepo := new(MockSystemUserRepository)
	jwtUtils := utils.NewJWTUtils(utils.DefaultJWTConfig)

	authService := &AuthService{
		jwtUtils:       jwtUtils,
		systemUserRepo: mockRepo,
	}

	ctx := context.Background()
	user := &model.SystemUser{
		Model:    gorm.Model{ID: 1},
		Username: "testuser",
		Email:    "test@example.com",
		Role:     "user",
		Status:   1,
		Enabled:  true,
	}

	mockRepo.On("GetByID", ctx, uint(1)).Return(user, nil)

	resp, err := authService.GetCurrentUser(ctx, 1)

	assert.NoError(t, err)
	assert.NotNil(t, resp)
	assert.Equal(t, "testuser", resp.Username)
	assert.Equal(t, "test@example.com", resp.Email)
	mockRepo.AssertExpectations(t)
}

// TestAuthService_GetCurrentUser_NotFound 测试获取不存在的用户
func TestAuthService_GetCurrentUser_NotFound(t *testing.T) {
	mockRepo := new(MockSystemUserRepository)
	jwtUtils := utils.NewJWTUtils(utils.DefaultJWTConfig)

	authService := &AuthService{
		jwtUtils:       jwtUtils,
		systemUserRepo: mockRepo,
	}

	ctx := context.Background()

	mockRepo.On("GetByID", ctx, uint(999)).Return(nil, gorm.ErrRecordNotFound)

	resp, err := authService.GetCurrentUser(ctx, 999)

	assert.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "用户不存在")
	mockRepo.AssertExpectations(t)
}

// TestAuthService_ChangePassword_UserNotFound 测试修改密码时用户不存在
func TestAuthService_ChangePassword_UserNotFound(t *testing.T) {
	mockRepo := new(MockSystemUserRepository)
	jwtUtils := utils.NewJWTUtils(utils.DefaultJWTConfig)

	authService := &AuthService{
		jwtUtils:       jwtUtils,
		systemUserRepo: mockRepo,
	}

	ctx := context.Background()

	mockRepo.On("GetByID", ctx, uint(999)).Return(nil, gorm.ErrRecordNotFound)

	req := &ChangePasswordRequest{
		OldPassword: "oldpassword",
		NewPassword: "newpassword",
	}

	err := authService.ChangePassword(ctx, 999, req)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "用户不存在")
	mockRepo.AssertExpectations(t)
}

// TestAuthService_InitAdmin_EmptyUsername 测试初始化管理员时用户名为空
func TestAuthService_InitAdmin_EmptyUsername(t *testing.T) {
	mockRepo := new(MockSystemUserRepository)
	jwtUtils := utils.NewJWTUtils(utils.DefaultJWTConfig)

	authService := &AuthService{
		jwtUtils:       jwtUtils,
		systemUserRepo: mockRepo,
	}

	ctx := context.Background()

	err := authService.InitAdmin(ctx, "", "Password123!", "test@example.com")

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "不能为空")
}

// TestAuthService_InitAdmin_WeakPassword 测试初始化管理员时密码强度不足
func TestAuthService_InitAdmin_WeakPassword(t *testing.T) {
	mockRepo := new(MockSystemUserRepository)
	jwtUtils := utils.NewJWTUtils(utils.DefaultJWTConfig)

	authService := &AuthService{
		jwtUtils:       jwtUtils,
		systemUserRepo: mockRepo,
	}

	ctx := context.Background()

	err := authService.InitAdmin(ctx, "admin", "weak", "test@example.com")

	assert.Error(t, err)
}

// TestAuthService_InitAdmin_InvalidEmail 测试初始化管理员时邮箱格式错误
func TestAuthService_InitAdmin_InvalidEmail(t *testing.T) {
	mockRepo := new(MockSystemUserRepository)
	jwtUtils := utils.NewJWTUtils(utils.DefaultJWTConfig)

	authService := &AuthService{
		jwtUtils:       jwtUtils,
		systemUserRepo: mockRepo,
	}

	ctx := context.Background()

	err := authService.InitAdmin(ctx, "admin", "Password123!", "invalid-email")

	assert.Error(t, err)
}

// TestAuthService_InitAdmin_UsernameExists 测试初始化管理员时用户名已存在
func TestAuthService_InitAdmin_UsernameExists(t *testing.T) {
	mockRepo := new(MockSystemUserRepository)
	jwtUtils := utils.NewJWTUtils(utils.DefaultJWTConfig)

	authService := &AuthService{
		jwtUtils:       jwtUtils,
		systemUserRepo: mockRepo,
	}

	ctx := context.Background()

	mockRepo.On("UsernameExists", ctx, "existinguser", uint(0)).Return(true, nil)

	err := authService.InitAdmin(ctx, "existinguser", "Password123!", "new@example.com")

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "用户名已存在")
	mockRepo.AssertExpectations(t)
}

// TestAuthService_InitAdmin_EmailExists 测试初始化管理员时邮箱已存在
func TestAuthService_InitAdmin_EmailExists(t *testing.T) {
	mockRepo := new(MockSystemUserRepository)
	jwtUtils := utils.NewJWTUtils(utils.DefaultJWTConfig)

	authService := &AuthService{
		jwtUtils:       jwtUtils,
		systemUserRepo: mockRepo,
	}

	ctx := context.Background()

	mockRepo.On("UsernameExists", ctx, "newuser", uint(0)).Return(false, nil)
	mockRepo.On("EmailExists", ctx, "existing@example.com", uint(0)).Return(true, nil)

	err := authService.InitAdmin(ctx, "newuser", "Password123!", "existing@example.com")

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "邮箱已被使用")
	mockRepo.AssertExpectations(t)
}

// TestAuthService_InitAdmin_Success 测试初始化管理员成功
func TestAuthService_InitAdmin_Success(t *testing.T) {
	mockRepo := new(MockSystemUserRepository)
	jwtUtils := utils.NewJWTUtils(utils.DefaultJWTConfig)

	authService := &AuthService{
		jwtUtils:       jwtUtils,
		systemUserRepo: mockRepo,
	}

	ctx := context.Background()

	mockRepo.On("UsernameExists", ctx, "newadmin", uint(0)).Return(false, nil)
	mockRepo.On("EmailExists", ctx, "admin@example.com", uint(0)).Return(false, nil)
	mockRepo.On("Create", ctx, mock.AnythingOfType("*model.SystemUser")).Return(nil)

	err := authService.InitAdmin(ctx, "newadmin", "Password123!", "admin@example.com")

	assert.NoError(t, err)
	mockRepo.AssertExpectations(t)
}

// TestAuthService_CheckPassword 测试密码校验
func TestAuthService_CheckPassword(t *testing.T) {
	user := &model.SystemUser{
		Password: "$2a$10$dummyhash",
	}

	result := CheckPassword(user, "anypassword")

	assert.False(t, result)
}

// TestAuthService_HashPassword 测试密码哈希
func TestAuthService_HashPassword(t *testing.T) {
	password := "TestPassword123!"

	hashed, err := HashPassword(password)

	assert.NoError(t, err)
	assert.NotEmpty(t, hashed)
	assert.NotEqual(t, password, hashed)
}

// TestNewAuthService 测试创建认证服务实例
func TestNewAuthService(t *testing.T) {
	service := NewAuthService()

	assert.NotNil(t, service)
	assert.NotNil(t, service.jwtUtils)
	assert.NotNil(t, service.systemUserRepo)
}
