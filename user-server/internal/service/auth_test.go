package service

import (
	"context"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/db"
	"testing"
	"time"

	"hivemtk-user/internal/pkg/testutil"

	"gorm.io/gorm"
)

func setupAuthServiceTestDB(t *testing.T) *gorm.DB {
	database := testutil.NewTestDB(t,
		&model.SystemUser{},
	)
	db.SetTestDB(database)
	return database
}

func setupAuthService(t *testing.T) *AuthService {
	setupAuthServiceTestDB(t)
	return NewAuthService()
}

// TestNewAuthService 测试创建服务实例
func TestNewAuthService(t *testing.T) {
	service := NewAuthService()
	if service == nil {
		t.Fatal("Expected service instance, got nil")
	}
	if service.jwtUtils == nil {
		t.Error("Expected jwtUtils to be initialized")
	}
}

// TestAuthService_JwtUtils 测试获取 JWT 工具实例
func TestAuthService_JwtUtils(t *testing.T) {
	service := NewAuthService()
	jwtUtils := service.JwtUtils(context.Background())
	if jwtUtils == nil {
		t.Error("Expected jwtUtils to be returned")
	}
}

// TestAuthService_Login_Success 测试已存在用户登录
// 注意：登录功能在 安全修复后不再自动注册超管，需先通过 InitSetup 创建用户
func TestAuthService_Login_Success(t *testing.T) {
	service := setupAuthService(t)

	database := db.GetDB()
	adminUser := &model.SystemUser{
		Username: "admin",
		Password: "admin123",
		Email:    "admin@example.com",
		RealName: "Admin",
		Role:     "admin",
		Status:   1,
	}
	if err := database.Create(adminUser).Error; err != nil {
		t.Fatalf("Create admin failed: %v", err)
	}

	req := &LoginRequest{
		Username: "admin",
		Password: "admin123",
	}

	response, err := service.Login(context.Background(), req)
	if err != nil {
		t.Fatalf("Login failed: %v", err)
	}

	if response == nil {
		t.Fatal("Expected response to be returned")
	}
	if response.Token == "" {
		t.Error("Expected token to be generated")
	}
	if response.User == nil {
		t.Fatal("Expected user info to be returned")
	}
	if response.User.Username != "admin" {
		t.Errorf("Expected username 'admin', got '%s'", response.User.Username)
	}
	if response.User.Role != "admin" {
		t.Errorf("Expected role 'admin', got '%s'", response.User.Role)
	}
}

// TestAuthService_Login_ExistingUser 测试已存在用户登录
func TestAuthService_Login_ExistingUser(t *testing.T) {
	service := setupAuthService(t)

	firstUserReq := &LoginRequest{
		Username: "admin",
		Password: "admin123",
	}
	_, _ = service.Login(context.Background(), firstUserReq)

	database := db.GetDB()
	secondUser := &model.SystemUser{
		Username: "testuser",
		Password: "password123",
		Email:    "test@example.com",
		RealName: "Test User",
		Role:     "user",
		Status:   1,
	}
	database.Create(secondUser)

	loginReq := &LoginRequest{
		Username: "testuser",
		Password: "password123",
	}

	response, err := service.Login(context.Background(), loginReq)
	if err != nil {
		t.Fatalf("Login failed: %v", err)
	}

	if response.User.Username != "testuser" {
		t.Errorf("Expected username 'testuser', got '%s'", response.User.Username)
	}
}

// TestAuthService_Login_WrongPassword 测试密码错误
func TestAuthService_Login_WrongPassword(t *testing.T) {
	service := setupAuthService(t)

	_, _ = service.Login(context.Background(), &LoginRequest{Username: "admin", Password: "admin123"})

	req := &LoginRequest{
		Username: "admin",
		Password: "wrongpassword",
	}

	_, err := service.Login(context.Background(), req)
	if err == nil {
		t.Error("Expected error for wrong password")
	}
}

// TestAuthService_Login_DisabledUser 测试被禁用用户登录
func TestAuthService_Login_DisabledUser(t *testing.T) {
	service := setupAuthService(t)

	_, _ = service.Login(context.Background(), &LoginRequest{Username: "admin", Password: "admin123"})

	database := db.GetDB()
	disabledUser := &model.SystemUser{
		Username: "disabled",
		Password: "password123",
		Email:    "disabled@example.com",
		RealName: "Disabled User",
		Role:     "user",
		Status:   0,
	}
	model.HashSystemUserPassword(disabledUser)
	database.Create(disabledUser)

	req := &LoginRequest{
		Username: "disabled",
		Password: "password123",
	}

	_, err := service.Login(context.Background(), req)
	if err == nil {
		t.Error("Expected error for disabled user")
	}
}

// TestAuthService_Login_NonExistentUser 测试不存在的用户
func TestAuthService_Login_NonExistentUser(t *testing.T) {
	service := setupAuthService(t)

	_, _ = service.Login(context.Background(), &LoginRequest{Username: "admin", Password: "admin123"})

	req := &LoginRequest{
		Username: "nonexistent",
		Password: "password123",
	}

	_, err := service.Login(context.Background(), req)
	if err == nil {
		t.Error("Expected error for non-existent user")
	}
}

// TestAuthService_RefreshToken 测试刷新令牌
func TestAuthService_RefreshToken(t *testing.T) {
	service := setupAuthService(t)

	_, _ = service.Login(context.Background(), &LoginRequest{Username: "admin", Password: "admin123"})

	jwtUtils := service.JwtUtils(context.Background())
	token, err := jwtUtils.GenerateToken(1, "admin", "admin")
	if err != nil {
		t.Fatalf("Failed to generate token: %v", err)
	}

	newToken, err := service.RefreshToken(context.Background(), token)
	if err != nil {
		t.Fatalf("RefreshToken failed: %v", err)
	}
	if newToken == "" {
		t.Error("Expected new token to be returned")
	}
}

// TestAuthService_GetCurrentUser 测试获取当前用户信息
func TestAuthService_GetCurrentUser(t *testing.T) {
	service := setupAuthService(t)

	database := db.GetDB()
	adminUser := &model.SystemUser{
		Username: "admin",
		Password: "admin123",
		Email:    "admin@example.com",
		RealName: "Admin",
		Role:     "admin",
		Status:   1,
	}
	if err := database.Create(adminUser).Error; err != nil {
		t.Fatalf("Create admin failed: %v", err)
	}

	userInfo, err := service.GetCurrentUser(context.Background(), adminUser.ID)
	if err != nil {
		t.Fatalf("GetCurrentUser failed: %v", err)
	}

	if userInfo == nil {
		t.Fatal("Expected user info to be returned")
	}
	if userInfo.Username != "admin" {
		t.Errorf("Expected username 'admin', got '%s'", userInfo.Username)
	}
}

// TestAuthService_GetCurrentUser_NonExistent 测试获取不存在的用户
func TestAuthService_GetCurrentUser_NonExistent(t *testing.T) {
	service := setupAuthService(t)

	_, err := service.GetCurrentUser(context.Background(), 999)
	if err == nil {
		t.Error("Expected error for non-existent user")
	}
}

// TestAuthService_ChangePassword 测试修改密码
func TestAuthService_ChangePassword(t *testing.T) {
	service := setupAuthService(t)

	database := db.GetDB()
	// 先占住 id=1：初始超管受系统级保护禁止改密，被测用户必须落在 id≠1
	seedInitialAdmin(t, database)
	adminUser := &model.SystemUser{
		Username: "admin",
		Password: "admin123",
		Email:    "admin@example.com",
		RealName: "Admin",
		Role:     "admin",
		Status:   1,
	}
	if err := database.Create(adminUser).Error; err != nil {
		t.Fatalf("Create admin failed: %v", err)
	}

	req := &ChangePasswordRequest{
		OldPassword: "admin123",
		NewPassword: "N3wSecur3Pwd!",
	}

	err := service.ChangePassword(context.Background(), adminUser.ID, req)
	if err != nil {
		t.Fatalf("ChangePassword failed: %v", err)
	}

	loginReq := &LoginRequest{
		Username: "admin",
		Password: "N3wSecur3Pwd!",
	}
	response, err := service.Login(context.Background(), loginReq)
	if err != nil {
		t.Fatalf("Login with new password failed: %v", err)
	}
	if response == nil {
		t.Error("Expected to login successfully with new password")
	}
}

// TestAuthService_ChangePassword_WrongOldPassword 测试旧密码错误
func TestAuthService_ChangePassword_WrongOldPassword(t *testing.T) {
	service := setupAuthService(t)

	_, _ = service.Login(context.Background(), &LoginRequest{Username: "admin", Password: "admin123"})

	req := &ChangePasswordRequest{
		OldPassword: "wrongpassword",
		NewPassword: "newpassword123",
	}

	err := service.ChangePassword(context.Background(), 1, req)
	if err == nil {
		t.Error("Expected error for wrong old password")
	}
}

// TestAuthService_ChangePassword_NonExistentUser 测试修改不存在的用户密码
func TestAuthService_ChangePassword_NonExistentUser(t *testing.T) {
	service := setupAuthService(t)

	req := &ChangePasswordRequest{
		OldPassword: "oldpassword",
		NewPassword: "newpassword",
	}

	err := service.ChangePassword(context.Background(), 999, req)
	if err == nil {
		t.Error("Expected error for non-existent user")
	}
}

// TestAuthService_toUserResponse 测试用户响应转换
func TestAuthService_toUserResponse(t *testing.T) {
	service := setupAuthService(t)

	now := time.Now()
	user := &model.SystemUser{
		ID:        1,
		Username:  "testuser",
		Email:     "test@example.com",
		RealName:  "Test User",
		Role:      "admin",
		Status:    1,
		LastLogin: &now,
	}

	response := service.toUserResponse(context.Background(), user)
	if response == nil {
		t.Fatal("Expected response to be returned")
	}
	if response.ID != 1 {
		t.Errorf("Expected ID 1, got %d", response.ID)
	}
	if response.Username != "testuser" {
		t.Errorf("Expected username 'testuser', got '%s'", response.Username)
	}
	if response.Role != "admin" {
		t.Errorf("Expected role 'admin', got '%s'", response.Role)
	}
}

// TestAuthService_loginWithUser 测试完成登录流程
func TestAuthService_loginWithUser(t *testing.T) {
	service := setupAuthService(t)

	now := time.Now()
	user := &model.SystemUser{
		ID:        1,
		Username:  "testuser",
		Password:  "hashedpassword",
		Email:     "test@example.com",
		RealName:  "Test User",
		Role:      "admin",
		Status:    1,
		LastLogin: &now,
	}

	response, err := service.loginWithUser(context.Background(), user)
	if err != nil {
		t.Fatalf("loginWithUser failed: %v", err)
	}

	if response == nil {
		t.Fatal("Expected response to be returned")
	}
	if response.Token == "" {
		t.Error("Expected token to be generated")
	}
	if response.User == nil {
		t.Error("Expected user info to be populated")
	}
}
