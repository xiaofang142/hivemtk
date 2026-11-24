package service

import (
	"context"
	"testing"

	"hivemtk-user/internal/dto"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/db"

	"hivemtk-user/internal/pkg/testutil"

	"gorm.io/gorm"
)

func setupUserServiceTestDB(t *testing.T) *gorm.DB {
	database := testutil.NewTestDB(t,
		&model.User{},
	)
	db.SetTestDB(database)
	return database
}

func TestNewUserService(t *testing.T) {
	service := NewUserService()
	if service == nil {
		t.Error("Expected non-nil service")
	}
}

func TestUserService_RegisterUser(t *testing.T) {
	setupUserServiceTestDB(t)

	service := NewUserService()

	req := &dto.CreateUserRequest{
		Username: "testuser",
		Password: testCredential("password123"),
		Email:    "test@example.com",
		RealName: "Test User",
		Role:     "user",
	}

	response, err := service.RegisterUser(context.Background(), req)
	if err != nil {
		t.Fatalf("RegisterUser failed: %v", err)
	}

	if response == nil {
		t.Fatal("Expected non-nil response")
	}

	if response.Username != "testuser" {
		t.Errorf("Expected username 'testuser', got %s", response.Username)
	}

	if response.Email != "test@example.com" {
		t.Errorf("Expected email 'test@example.com', got %s", response.Email)
	}
}

func TestUserService_RegisterUser_DuplicateUsername(t *testing.T) {
	setupUserServiceTestDB(t)

	service := NewUserService()

	req := &dto.CreateUserRequest{
		Username: "testuser",
		Password: testCredential("password123"),
		Email:    "test@example.com",
		Role:     "user",
	}
	service.RegisterUser(context.Background(), req)

	req2 := &dto.CreateUserRequest{
		Username: "testuser",
		Password: testCredential("password456"),
		Email:    "test2@example.com",
		Role:     "user",
	}

	_, err := service.RegisterUser(context.Background(), req2)
	if err == nil {
		t.Error("Expected error for duplicate username")
	}

	if err.Error() != "用户名已存在" {
		t.Errorf("Expected '用户名已存在', got %s", err.Error())
	}
}

func TestUserService_RegisterUser_DuplicateEmail(t *testing.T) {
	setupUserServiceTestDB(t)

	service := NewUserService()

	req := &dto.CreateUserRequest{
		Username: "testuser",
		Password: testCredential("password123"),
		Email:    "test@example.com",
		Role:     "user",
	}
	service.RegisterUser(context.Background(), req)

	req2 := &dto.CreateUserRequest{
		Username: "testuser2",
		Password: testCredential("password456"),
		Email:    "test@example.com",
		Role:     "user",
	}

	_, err := service.RegisterUser(context.Background(), req2)
	if err == nil {
		t.Error("Expected error for duplicate email")
	}

	if err.Error() != "邮箱已存在" {
		t.Errorf("Expected '邮箱已存在', got %s", err.Error())
	}
}

func TestUserService_GetUser(t *testing.T) {
	setupUserServiceTestDB(t)

	service := NewUserService()

	regReq := &dto.CreateUserRequest{
		Username: "testuser",
		Password: testCredential("password123"),
		Email:    "test@example.com",
		Role:     "user",
	}
	regResp, _ := service.RegisterUser(context.Background(), regReq)

	response, err := service.GetUser(context.Background(), regResp.ID)
	if err != nil {
		t.Fatalf("GetUser failed: %v", err)
	}

	if response.Username != "testuser" {
		t.Errorf("Expected username 'testuser', got %s", response.Username)
	}
}

func TestUserService_GetUser_NotFound(t *testing.T) {
	setupUserServiceTestDB(t)

	service := NewUserService()

	_, err := service.GetUser(context.Background(), "non-existent-id")
	if err == nil {
		t.Error("Expected error for non-existent user")
	}
}

func TestUserService_GetUserByUsername(t *testing.T) {
	setupUserServiceTestDB(t)

	service := NewUserService()

	regReq := &dto.CreateUserRequest{
		Username: "testuser",
		Password: testCredential("password123"),
		Email:    "test@example.com",
		Role:     "user",
	}
	regResp, _ := service.RegisterUser(context.Background(), regReq)

	response, err := service.GetUserByUsername(context.Background(), "testuser")
	if err != nil {
		t.Fatalf("GetUserByUsername failed: %v", err)
	}

	if response.ID != regResp.ID {
		t.Errorf("Expected ID %s, got %s", regResp.ID, response.ID)
	}
}

func TestUserService_GetUserByUsername_NotFound(t *testing.T) {
	setupUserServiceTestDB(t)

	service := NewUserService()

	_, err := service.GetUserByUsername(context.Background(), "nonexistent")
	if err == nil {
		t.Error("Expected error for non-existent username")
	}
}

func TestUserService_GetUserList(t *testing.T) {
	setupUserServiceTestDB(t)

	service := NewUserService()

	for i := 0; i < 5; i++ {
		req := &dto.CreateUserRequest{
			Username: "user" + string(rune('0'+i)),
			Password: testCredential("password123"),
			Email:    "user" + string(rune('0'+i)) + "@example.com",
			Role:     "user",
		}
		service.RegisterUser(context.Background(), req)
	}

	response, err := service.GetUserList(context.Background(), 1, 10)
	if err != nil {
		t.Fatalf("GetUserList failed: %v", err)
	}

	if response.Total != 5 {
		t.Errorf("Expected total 5, got %d", response.Total)
	}

	if len(response.Users) != 5 {
		t.Errorf("Expected 5 users, got %d", len(response.Users))
	}
}

func TestUserService_DeleteUser(t *testing.T) {
	setupUserServiceTestDB(t)

	service := NewUserService()

	regReq := &dto.CreateUserRequest{
		Username: "testuser",
		Password: testCredential("password123"),
		Email:    "test@example.com",
		Role:     "user",
	}
	regResp, _ := service.RegisterUser(context.Background(), regReq)

	err := service.DeleteUser(context.Background(), regResp.ID)
	if err != nil {
		t.Fatalf("DeleteUser failed: %v", err)
	}

	_, err = service.GetUser(context.Background(), regResp.ID)
	if err == nil {
		t.Error("Expected error after delete")
	}
}

func TestUserService_UpdateUser(t *testing.T) {
	setupUserServiceTestDB(t)

	service := NewUserService()

	regReq := &dto.CreateUserRequest{
		Username: "testuser",
		Password: testCredential("password123"),
		Email:    "test@example.com",
		Role:     "user",
	}
	regResp, _ := service.RegisterUser(context.Background(), regReq)

	status := 1
	updateReq := &dto.UpdateUserRequest{
		RealName: "Updated Name",
		Phone:    "13812345678",
		Status:   &status,
	}

	response, err := service.UpdateUser(context.Background(), regResp.ID, updateReq)
	if err != nil {
		t.Fatalf("UpdateUser failed: %v", err)
	}

	if response.RealName != "Updated Name" {
		t.Errorf("Expected RealName 'Updated Name', got %s", response.RealName)
	}

	if response.Phone != "13812345678" {
		t.Errorf("Expected Phone '13812345678', got %s", response.Phone)
	}
}

func TestUserService_UpdateUser_DuplicateUsername(t *testing.T) {
	setupUserServiceTestDB(t)

	service := NewUserService()

	regReq1 := &dto.CreateUserRequest{
		Username: "testuser1",
		Password: testCredential("password123"),
		Email:    "test1@example.com",
		Role:     "user",
	}
	_, _ = service.RegisterUser(context.Background(), regReq1)

	regReq2 := &dto.CreateUserRequest{
		Username: "testuser2",
		Password: testCredential("password123"),
		Email:    "test2@example.com",
		Role:     "user",
	}
	regResp2, _ := service.RegisterUser(context.Background(), regReq2)

	updateReq := &dto.UpdateUserRequest{
		Username: "testuser1",
	}

	_, err := service.UpdateUser(context.Background(), regResp2.ID, updateReq)
	if err == nil {
		t.Error("Expected error for duplicate username")
	}

	if err.Error() != "用户名已存在" {
		t.Errorf("Expected '用户名已存在', got %s", err.Error())
	}
}

func TestUserService_UpdatePassword(t *testing.T) {
	setupUserServiceTestDB(t)

	service := NewUserService()

	regReq := &dto.CreateUserRequest{
		Username: "testuser",
		Password: testCredential("oldpassword123"),
		Email:    "test@example.com",
		Role:     "user",
	}
	regResp, _ := service.RegisterUser(context.Background(), regReq)

	updateReq := &dto.UpdatePasswordRequest{
		OldPassword: testCredential("oldpassword123"),
		NewPassword: testCredential("newpassword123"),
	}

	err := service.UpdatePassword(context.Background(), regResp.ID, updateReq)
	if err != nil {
		t.Fatalf("UpdatePassword failed: %v", err)
	}

	loginReq := &dto.LoginRequest{
		Username: "testuser",
		Password: testCredential("newpassword123"),
	}
	loginResp, err := service.Login(context.Background(), loginReq)
	if err != nil {
		t.Fatalf("Login with new password failed: %v", err)
	}

	if loginResp == nil {
		t.Error("Expected successful login with new password")
	}
}

func TestUserService_UpdatePassword_WrongOldPassword(t *testing.T) {
	setupUserServiceTestDB(t)

	service := NewUserService()

	regReq := &dto.CreateUserRequest{
		Username: "testuser",
		Password: testCredential("correctpassword"),
		Email:    "test@example.com",
		Role:     "user",
	}
	regResp, _ := service.RegisterUser(context.Background(), regReq)

	updateReq := &dto.UpdatePasswordRequest{
		OldPassword: testCredential("wrongpassword"),
		NewPassword: testCredential("newpassword"),
	}

	err := service.UpdatePassword(context.Background(), regResp.ID, updateReq)
	if err == nil {
		t.Error("Expected error for wrong old password")
	}

	if err.Error() != "原密码不正确" {
		t.Errorf("Expected '原密码不正确', got %s", err.Error())
	}
}

func TestUserService_Login(t *testing.T) {
	setupUserServiceTestDB(t)

	service := NewUserService()

	regReq := &dto.CreateUserRequest{
		Username: "testuser",
		Password: testCredential("password123"),
		Email:    "test@example.com",
		Role:     "user",
	}
	regResp, _ := service.RegisterUser(context.Background(), regReq)

	loginReq := &dto.LoginRequest{
		Username: "testuser",
		Password: testCredential("password123"),
	}

	loginResp, err := service.Login(context.Background(), loginReq)
	if err != nil {
		t.Fatalf("Login failed: %v", err)
	}

	if loginResp == nil {
		t.Fatal("Expected non-nil response")
	}

	if loginResp.User.ID != regResp.ID {
		t.Errorf("Expected user ID %s, got %s", regResp.ID, loginResp.User.ID)
	}

	if loginResp.Token == "" {
		t.Error("Expected non-empty token")
	}
}

func TestUserService_Login_WrongPassword(t *testing.T) {
	setupUserServiceTestDB(t)

	service := NewUserService()

	regReq := &dto.CreateUserRequest{
		Username: "testuser",
		Password: testCredential("password123"),
		Email:    "test@example.com",
		Role:     "user",
	}
	service.RegisterUser(context.Background(), regReq)

	loginReq := &dto.LoginRequest{
		Username: "testuser",
		Password: testCredential("wrongpassword"),
	}

	_, err := service.Login(context.Background(), loginReq)
	if err == nil {
		t.Error("Expected error for wrong password")
	}

	if err.Error() != "用户名或密码错误" {
		t.Errorf("Expected '用户名或密码错误', got %s", err.Error())
	}
}

func TestUserService_InitUser(t *testing.T) {
	setupUserServiceTestDB(t)

	service := NewUserService()

	userID, err := service.InitUser(context.Background(), "account123", 12345, "John", "Doe", "johndoe")
	if err != nil {
		t.Fatalf("InitUser failed: %v", err)
	}

	if userID == "" {
		t.Error("Expected non-empty user ID")
	}

	userID2, err := service.InitUser(context.Background(), "account123", 12345, "John", "Doe", "johndoe")
	if err != nil {
		t.Fatalf("InitUser second call failed: %v", err)
	}

	if userID != userID2 {
		t.Errorf("Expected same user ID %s, got %s", userID, userID2)
	}
}

func TestUserService_Login_DisabledUser(t *testing.T) {
	setupUserServiceTestDB(t)

	service := NewUserService()

	regReq := &dto.CreateUserRequest{
		Username: "testuser",
		Password: testCredential("password123"),
		Email:    "test@example.com",
		Role:     "user",
	}
	regResp, _ := service.RegisterUser(context.Background(), regReq)

	db.GetDB().Model(&model.User{}).Where("id = ?", regResp.ID).Update("status", 0)

	loginReq := &dto.LoginRequest{
		Username: "testuser",
		Password: testCredential("password123"),
	}

	_, err := service.Login(context.Background(), loginReq)
	if err == nil {
		t.Error("Expected error for disabled user")
	}

	if err.Error() != "账户已被禁用" {
		t.Errorf("Expected '账户已被禁用', got %s", err.Error())
	}
}
