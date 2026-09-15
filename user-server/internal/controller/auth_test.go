package controller

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/db"
	"hivemtk-user/internal/service"

	"github.com/gin-gonic/gin"
)

func setupGinEngine() *gin.Engine {
	gin.SetMode(gin.TestMode)
	return gin.New()
}

// TestAuthController_Login_Success 测试登录成功
func TestAuthController_Login_Success(t *testing.T) {
	setupTestControllerDB(t)
	authCtrl := NewAuthController()
	router := setupGinEngine()
	router.POST("/login", authCtrl.Login)

	admin := &model.SystemUser{
		Username: "admin",
		Password: "Admin@123",
		Email:    "admin@test.com",
		Role:     "admin",
		Status:   1,
	}
	if err := db.GetDB().Create(admin).Error; err != nil {
		t.Fatalf("Create admin failed: %v", err)
	}

	loginReq := service.LoginRequest{
		Username: "admin",
		Password: "Admin@123",
	}
	body, _ := json.Marshal(loginReq)

	req, _ := http.NewRequest("POST", "/login", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status OK, got %d, body: %s", w.Code, w.Body.String())
	}

	var response map[string]any
	json.Unmarshal(w.Body.Bytes(), &response)
	if response["code"] != float64(0) {
		t.Errorf("Expected code SUCCESS, got %v", response["code"])
	}
}

// TestAuthController_Login_InvalidJSON 测试无效 JSON 请求
func TestAuthController_Login_InvalidJSON(t *testing.T) {
	setupTestControllerDB(t)
	authCtrl := NewAuthController()
	router := setupGinEngine()
	router.POST("/login", authCtrl.Login)

	req, _ := http.NewRequest("POST", "/login", bytes.NewReader([]byte("invalid-json")))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected status Bad Request, got %d", w.Code)
	}
}

// TestAuthController_Login_EmptyUsername 测试空用户名
func TestAuthController_Login_EmptyUsername(t *testing.T) {
	setupTestControllerDB(t)
	authCtrl := NewAuthController()
	router := setupGinEngine()
	router.POST("/login", authCtrl.Login)

	loginReq := service.LoginRequest{
		Username: "",
		Password: "admin123",
	}
	body, _ := json.Marshal(loginReq)

	req, _ := http.NewRequest("POST", "/login", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected status Bad Request, got %d", w.Code)
	}
}

// TestAuthController_Login_EmptyPassword 测试空密码
func TestAuthController_Login_EmptyPassword(t *testing.T) {
	setupTestControllerDB(t)
	authCtrl := NewAuthController()
	router := setupGinEngine()
	router.POST("/login", authCtrl.Login)

	loginReq := service.LoginRequest{
		Username: "admin",
		Password: "",
	}
	body, _ := json.Marshal(loginReq)

	req, _ := http.NewRequest("POST", "/login", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected status Bad Request, got %d", w.Code)
	}
}

// TestAuthController_Login_WrongCredentials 测试错误凭证
func TestAuthController_Login_WrongCredentials(t *testing.T) {
	setupTestControllerDB(t)
	authCtrl := NewAuthController()
	router := setupGinEngine()
	router.POST("/login", authCtrl.Login)

	firstUser := model.SystemUser{
		Username: "testuser",
		Password: "correctpassword",
		Status:   1,
	}
	db.GetDB().Create(&firstUser)

	loginReq := service.LoginRequest{
		Username: "testuser",
		Password: "wrongpassword",
	}
	body, _ := json.Marshal(loginReq)

	req, _ := http.NewRequest("POST", "/login", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("Expected status Unauthorized, got %d", w.Code)
	}
}

// TestAuthController_RefreshToken_Success 测试刷新令牌成功
func TestAuthController_RefreshToken_Success(t *testing.T) {
	setupTestControllerDB(t)
	authCtrl := NewAuthController()
	router := setupGinEngine()
	router.POST("/refresh", authCtrl.RefreshToken)

	jwtUtils := service.NewAuthService().JwtUtils(context.Background())
	validToken, _ := jwtUtils.GenerateToken(1, "testuser", "admin")

	req, _ := http.NewRequest("POST", "/refresh", nil)
	req.Header.Set("Authorization", "Bearer "+validToken)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status OK, got %d, body: %s", w.Code, w.Body.String())
	}
}

// TestAuthController_RefreshToken_MissingToken 测试缺少令牌
func TestAuthController_RefreshToken_MissingToken(t *testing.T) {
	setupTestControllerDB(t)
	authCtrl := NewAuthController()
	router := setupGinEngine()
	router.POST("/refresh", authCtrl.RefreshToken)

	req, _ := http.NewRequest("POST", "/refresh", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("Expected status Unauthorized, got %d", w.Code)
	}
}

// TestAuthController_RefreshToken_InvalidToken 测试无效令牌
func TestAuthController_RefreshToken_InvalidToken(t *testing.T) {
	setupTestControllerDB(t)
	authCtrl := NewAuthController()
	router := setupGinEngine()
	router.POST("/refresh", authCtrl.RefreshToken)

	req, _ := http.NewRequest("POST", "/refresh", nil)
	req.Header.Set("Authorization", "Bearer invalid-token")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("Expected status Unauthorized, got %d", w.Code)
	}
}

// TestAuthController_GetCurrentUser_Success 测试获取当前用户成功
func TestAuthController_GetCurrentUser_Success(t *testing.T) {
	setupTestControllerDB(t)
	authCtrl := NewAuthController()
	router := setupGinEngine()

	user := model.SystemUser{
		Username: "testuser",
		Email:    "test@example.com",
		Role:     "user",
		Status:   1,
	}
	db.GetDB().Create(&user)

	router.GET("/user", func(c *gin.Context) {
		c.Set("user_id", user.ID)
		authCtrl.GetCurrentUser(c)
	})

	req, _ := http.NewRequest("GET", "/user", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status OK, got %d, body: %s", w.Code, w.Body.String())
	}
}

// TestAuthController_GetCurrentUser_MissingUserID 测试缺少用户 ID
func TestAuthController_GetCurrentUser_MissingUserID(t *testing.T) {
	setupTestControllerDB(t)
	authCtrl := NewAuthController()
	router := setupGinEngine()
	router.GET("/user", authCtrl.GetCurrentUser)

	req, _ := http.NewRequest("GET", "/user", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("Expected status Unauthorized, got %d", w.Code)
	}
}

// seedRegularUser 播种「初始超管」（占住 id=1）+ 一个普通用户，返回该普通用户。
//
// 为什么必须先占住 id=1：service.ChangePassword 与 SystemUserService.ResetPassword
// 对 id=1（初始超管）有系统级保护 —— 一律拒绝改密（ErrInitialAdminProtected）。
// 而本文件原先的用例把普通用户建成表里的第一行，它必然拿到 id=1，
// 于是：
//   - *_Success 用例被守卫拦成 400 → 长期失败；
//   - *_WrongOldPassword 用例恰好也期望 400 → "因错误原因通过"，实际根本没验到原密码分支。
//
// 这里显式占位，让被测用户稳定落在 id≠1，两条路径才真正被覆盖。
func seedRegularUser(t *testing.T, username, password string) model.SystemUser {
	t.Helper()
	database := db.GetDB()
	if err := database.Create(&model.SystemUser{
		Username: "initial_admin",
		Password: "seed-password-not-used",
		Status:   1,
	}).Error; err != nil {
		t.Fatalf("播种初始超管失败: %v", err)
	}
	u := model.SystemUser{Username: username, Password: password, Status: 1}
	if err := database.Create(&u).Error; err != nil {
		t.Fatalf("创建普通用户失败: %v", err)
	}
	if u.ID == 1 {
		t.Fatalf("普通用户不应拿到 id=1（初始超管受保护），实际 id=%d", u.ID)
	}
	return u
}

// TestAuthController_ChangePassword_Success 测试修改密码成功
func TestAuthController_ChangePassword_Success(t *testing.T) {
	setupTestControllerDB(t)
	authCtrl := NewAuthController()
	router := setupGinEngine()

	user := seedRegularUser(t, "testuser", "oldpassword123")

	changeReq := service.ChangePasswordRequest{
		OldPassword: "oldpassword123",
		NewPassword: "Hv7mKp2LnQ",
	}
	body, _ := json.Marshal(changeReq)

	router.PUT("/password", func(c *gin.Context) {
		c.Set("user_id", user.ID)
		authCtrl.ChangePassword(c)
	})

	req, _ := http.NewRequest("PUT", "/password", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status OK, got %d, body: %s", w.Code, w.Body.String())
	}
}

// TestAuthController_ChangePassword_InitialAdminProtected 回归：初始超管（id=1）禁止自助改密。
//
// 这是一条系统级安全约束（防止初始超管密码被改后无法登录）。此前**没有任何测试覆盖**它，
// 一旦被后续改动无声移除也不会有人发现。本用例同时校验状态码与"密码确实没被改"。
func TestAuthController_ChangePassword_InitialAdminProtected(t *testing.T) {
	setupTestControllerDB(t)
	authCtrl := NewAuthController()
	router := setupGinEngine()

	const originalPwd = "admin-original-pwd"
	admin := model.SystemUser{Username: "admin", Password: originalPwd, Status: 1}
	if err := db.GetDB().Create(&admin).Error; err != nil {
		t.Fatalf("创建初始超管失败: %v", err)
	}
	if admin.ID != 1 {
		t.Fatalf("本用例前提是初始超管 id=1，实际 id=%d", admin.ID)
	}

	changeReq := service.ChangePasswordRequest{
		OldPassword: originalPwd,
		NewPassword: "N3wSecur3Pwd!",
	}
	body, _ := json.Marshal(changeReq)

	router.PUT("/password", func(c *gin.Context) {
		c.Set("user_id", admin.ID)
		authCtrl.ChangePassword(c)
	})

	req, _ := http.NewRequest("PUT", "/password", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("初始超管自助改密应被拒绝(400)，实际 %d，body: %s", w.Code, w.Body.String())
	}

	var after model.SystemUser
	if err := db.GetDB().First(&after, admin.ID).Error; err != nil {
		t.Fatalf("回查初始超管失败: %v", err)
	}
	if !service.CheckPassword(&after, originalPwd) {
		t.Error("初始超管密码已被改动 —— 系统级保护失效")
	}
}

// TestAuthController_ChangePassword_WrongOldPassword 测试错误原密码
func TestAuthController_ChangePassword_WrongOldPassword(t *testing.T) {
	setupTestControllerDB(t)
	authCtrl := NewAuthController()
	router := setupGinEngine()

	user := seedRegularUser(t, "testuser", "correctpassword")

	changeReq := service.ChangePasswordRequest{
		OldPassword: "wrongpassword",
		NewPassword: "newpassword",
	}
	body, _ := json.Marshal(changeReq)

	router.PUT("/password", func(c *gin.Context) {
		c.Set("user_id", user.ID)
		authCtrl.ChangePassword(c)
	})

	req, _ := http.NewRequest("PUT", "/password", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected status Bad Request, got %d", w.Code)
	}
}

// TestAuthController_ChangePassword_InvalidJSON 测试无效 JSON
func TestAuthController_ChangePassword_InvalidJSON(t *testing.T) {
	setupTestControllerDB(t)
	authCtrl := NewAuthController()
	router := setupGinEngine()

	router.PUT("/password", func(c *gin.Context) {
		c.Set("user_id", uint(1))
		authCtrl.ChangePassword(c)
	})

	req, _ := http.NewRequest("PUT", "/password", bytes.NewReader([]byte("invalid-json")))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected status Bad Request, got %d", w.Code)
	}
}

// TestSystemUserController_GetUsers_Success 测试获取用户列表成功
func TestSystemUserController_GetUsers_Success(t *testing.T) {
	setupTestControllerDB(t)
	ctrl := NewSystemUserController()
	router := setupGinEngine()
	router.GET("/users", ctrl.GetUsers)

	user := model.SystemUser{
		Username: "testuser",
		Email:    "test@example.com",
		Status:   1,
	}
	db.GetDB().Create(&user)

	req, _ := http.NewRequest("GET", "/users?page=1&page_size=10", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status OK, got %d, body: %s", w.Code, w.Body.String())
	}
}

// TestSystemUserController_GetUser_Success 测试获取用户详情成功
func TestSystemUserController_GetUser_Success(t *testing.T) {
	setupTestControllerDB(t)
	ctrl := NewSystemUserController()
	router := setupGinEngine()

	user := model.SystemUser{
		Username: "testuser",
		Email:    "test@example.com",
		Status:   1,
	}
	db.GetDB().Create(&user)

	router.GET("/users/:id", ctrl.GetUser)

	req, _ := http.NewRequest("GET", "/users/1", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status OK, got %d, body: %s", w.Code, w.Body.String())
	}
}

// TestSystemUserController_GetUser_InvalidID 测试无效用户 ID
func TestSystemUserController_GetUser_InvalidID(t *testing.T) {
	setupTestControllerDB(t)
	ctrl := NewSystemUserController()
	router := setupGinEngine()
	router.GET("/users/:id", ctrl.GetUser)

	req, _ := http.NewRequest("GET", "/users/invalid", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected status Bad Request, got %d", w.Code)
	}
}

// TestSystemUserController_CreateUser_Success 测试创建用户成功
func TestSystemUserController_CreateUser_Success(t *testing.T) {
	setupTestControllerDB(t)
	ctrl := NewSystemUserController()
	router := setupGinEngine()
	router.POST("/users", ctrl.CreateUser)

	createReq := service.CreateUserRequest{
		Username: "newuser",
		Password: "Password@123",
		Email:    "newuser@example.com",
		RealName: "New User",
		Role:     "user",
		Status:   1,
	}
	body, _ := json.Marshal(createReq)

	req, _ := http.NewRequest("POST", "/users", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status OK, got %d, body: %s", w.Code, w.Body.String())
	}
}

// TestSystemUserController_UpdateUser_Success 测试更新用户成功
func TestSystemUserController_UpdateUser_Success(t *testing.T) {
	setupTestControllerDB(t)
	ctrl := NewSystemUserController()
	router := setupGinEngine()

	user := model.SystemUser{
		Username: "testuser",
		Email:    "test@example.com",
		Status:   1,
	}
	db.GetDB().Create(&user)

	router.PUT("/users/:id", ctrl.UpdateUser)

	updateReq := service.UpdateUserRequest{
		RealName: "Updated Name",
	}
	body, _ := json.Marshal(updateReq)

	req, _ := http.NewRequest("PUT", "/users/1", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status OK, got %d, body: %s", w.Code, w.Body.String())
	}
}

// TestSystemUserController_DeleteUser_Success 测试删除用户成功
func TestSystemUserController_DeleteUser_Success(t *testing.T) {
	setupTestControllerDB(t)
	ctrl := NewSystemUserController()
	router := setupGinEngine()

	user := model.SystemUser{
		Username: "testuser",
		Email:    "test@example.com",
		Status:   1,
	}
	db.GetDB().Create(&user)

	router.Use(func(c *gin.Context) {
		c.Set("user_id", uint(999))
		c.Next()
	})
	router.DELETE("/users/:id", ctrl.DeleteUser)

	req, _ := http.NewRequest("DELETE", "/users/1", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status OK, got %d, body: %s", w.Code, w.Body.String())
	}
}

// TestSystemUserController_ResetPassword_Success 测试重置密码成功
func TestSystemUserController_ResetPassword_Success(t *testing.T) {
	setupTestControllerDB(t)
	ctrl := NewSystemUserController()
	router := setupGinEngine()

	user := seedRegularUser(t, "testuser", "oldpassword")

	router.POST("/users/:id/reset-password", ctrl.ResetPassword)

	// 必须满足密码策略（含大写字母等），否则会被 400 拦在策略校验而非被测路径上
	resetReq := map[string]string{"password": "Hv7mKp2LnQ"}
	body, _ := json.Marshal(resetReq)

	req, _ := http.NewRequest("POST", fmt.Sprintf("/users/%d/reset-password", user.ID), bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status OK, got %d, body: %s", w.Code, w.Body.String())
	}
}

// TestSystemUserController_CreateDefaultAdmin 测试创建默认管理员
// 现行为：与 AuthController.InitAdmin 一致，强制调用方在请求体传 username/password/email。
func TestSystemUserController_CreateDefaultAdmin(t *testing.T) {
	setupTestControllerDB(t)
	ctrl := NewSystemUserController()
	router := setupGinEngine()
	router.POST("/admin/init", ctrl.CreateDefaultAdmin)

	createReq := map[string]string{
		"username": "admin",
		"password": "Admin@123456",
		"email":    "admin@test.com",
	}
	body, _ := json.Marshal(createReq)

	req, _ := http.NewRequest("POST", "/admin/init", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status OK, got %d, body: %s", w.Code, w.Body.String())
	}
}
