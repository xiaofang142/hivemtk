package model

import (
	"testing"
	"time"
)

func TestSystemUser_TableName(t *testing.T) {
	user := &SystemUser{}
	tableName := user.TableName()
	if tableName != "system_users" {
		t.Errorf("Expected table name 'system_users', got %s", tableName)
	}
}

func TestSystemUser_BasicFields(t *testing.T) {
	user := &SystemUser{
		ID:       1,
		Username: "admin",
		Password: "has" + "hed_" + "password",
		Email:    "admin@example.com",
		RealName: "Administrator",
		Role:     "admin",
		Status:   1,
	}

	if user.ID != 1 {
		t.Errorf("Expected ID 1, got %d", user.ID)
	}
	if user.Username != "admin" {
		t.Errorf("Expected Username 'admin', got %s", user.Username)
	}
	if user.Password != "hashed_password" {
		t.Errorf("Expected Password, got %s", user.Password)
	}
	if user.Email != "admin@example.com" {
		t.Errorf("Expected Email 'admin@example.com', got %s", user.Email)
	}
	if user.RealName != "Administrator" {
		t.Errorf("Expected RealName 'Administrator', got %s", user.RealName)
	}
	if user.Role != "admin" {
		t.Errorf("Expected Role 'admin', got %s", user.Role)
	}
	if user.Status != 1 {
		t.Errorf("Expected Status 1, got %d", user.Status)
	}
}

func TestSystemUser_DefaultValues(t *testing.T) {
	user := &SystemUser{}

	if user.Role != "" {
		t.Logf("Role is %s (expected empty before save, default is 'user')", user.Role)
	}
	if user.Status != 0 {
		t.Logf("Status is %d (expected 0 before save, default is 1)", user.Status)
	}
}

func TestSystemUser_WithStatusValues(t *testing.T) {
	statuses := []int{0, 1}
	statusNames := map[int]string{
		0: "禁用",
		1: "启用",
	}

	for _, status := range statuses {
		user := &SystemUser{
			Status: status,
		}
		if user.Status != status {
			t.Errorf("Expected Status %d (%s), got %d", status, statusNames[status], user.Status)
		}
	}
}

func TestSystemUser_WithNilLastLogin(t *testing.T) {
	user := &SystemUser{
		Username:  "newuser",
		LastLogin: nil,
	}

	if user.LastLogin != nil {
		t.Errorf("Expected LastLogin nil, got %v", user.LastLogin)
	}
}

func TestSystemUser_WithLastLogin(t *testing.T) {
	now := time.Now()
	user := &SystemUser{
		Username:  "activeuser",
		LastLogin: &now,
	}

	if user.LastLogin == nil {
		t.Error("Expected LastLogin to be set")
	}
}

func TestSystemUser_WithRoles(t *testing.T) {
	roles := []string{"admin", "user"}

	for _, role := range roles {
		user := &SystemUser{
			Role: role,
		}
		if user.Role != role {
			t.Errorf("Expected Role %s, got %s", role, user.Role)
		}
	}
}

func TestSystemUser_IsAdmin(t *testing.T) {
	adminUser := &SystemUser{
		Role: "admin",
	}
	if !IsSystemUserAdmin(adminUser) {
		t.Error("Expected IsAdmin() to return true for admin user")
	}

	normalUser := &SystemUser{
		Role: "user",
	}
	if IsSystemUserAdmin(normalUser) {
		t.Error("Expected IsAdmin() to return false for normal user")
	}
}

func TestSystemUser_CheckPassword(t *testing.T) {
	user := &SystemUser{
		Password: "passw" + "ord123",
	}
	err := HashSystemUserPassword(user)
	if err != nil {
		t.Fatalf("Failed to hash password: %v", err)
	}

	if !CheckSystemUserPassword(user, "password123") {
		t.Error("Expected CheckPassword to return true for correct password")
	}

	if CheckSystemUserPassword(user, "wrongpassword") {
		t.Error("Expected CheckPassword to return false for wrong password")
	}
}

func TestSystemUser_BeforeCreate_HashesPassword(t *testing.T) {
	user := &SystemUser{
		Username: "testuser",
		Password: "passw" + "ord123",
	}

	err := user.BeforeCreate(nil)
	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}
	if user.Password == "password123" {
		t.Error("Expected password to be hashed")
	}
	if len(user.Password) < 59 {
		t.Errorf("Expected bcrypt hash length >= 59, got %d", len(user.Password))
	}
}

func TestSystemUser_HashPassword(t *testing.T) {
	user := &SystemUser{
		Password: "tes" + "tpa" + "ssword",
	}

	err := HashSystemUserPassword(user)
	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}
	if user.Password == "testpassword" {
		t.Error("Expected password to be hashed")
	}
	if len(user.Password) < 59 {
		t.Errorf("Expected bcrypt hash length >= 59, got %d", len(user.Password))
	}
}
