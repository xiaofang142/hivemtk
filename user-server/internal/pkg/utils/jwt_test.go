package utils

import (
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

func TestDefaultJWTConfig(t *testing.T) {
	if DefaultJWTConfig.SecretKey == "" {
		t.Error("Expected non-empty SecretKey")
	}
	if DefaultJWTConfig.ExpiresHours != 24 {
		t.Errorf("Expected ExpiresHours to be 24, got %d", DefaultJWTConfig.ExpiresHours)
	}
	if DefaultJWTConfig.Issuer != "marketing-system" {
		t.Errorf("Expected Issuer to be 'marketing-system', got %s", DefaultJWTConfig.Issuer)
	}
}

func TestNewJWTUtils(t *testing.T) {
	config := JWTConfig{
		SecretKey:    "test" + "-secret",
		ExpiresHours: 12,
		Issuer:       "test-issuer",
	}

	jwt := NewJWTUtils(config)
	if jwt == nil {
		t.Fatal("NewJWTUtils returned nil")
	}
	if jwt.config.SecretKey != "test-secret" {
		t.Errorf("Expected SecretKey 'test-secret', got %s", jwt.config.SecretKey)
	}
}

func TestJWTUtils_GenerateToken(t *testing.T) {
	jwt := NewJWTUtils(DefaultJWTConfig)

	token, err := jwt.GenerateToken(1, "testuser", "admin")
	if err != nil {
		t.Fatalf("GenerateToken failed: %v", err)
	}
	if token == "" {
		t.Error("Expected non-empty token")
	}
}

func TestJWTUtils_ParseToken(t *testing.T) {
	jwt := NewJWTUtils(DefaultJWTConfig)

	tokenStr, err := jwt.GenerateToken(1, "testuser", "admin")
	if err != nil {
		t.Fatalf("GenerateToken failed: %v", err)
	}

	claims, err := jwt.ParseToken(tokenStr)
	if err != nil {
		t.Fatalf("ParseToken failed: %v", err)
	}

	if claims.UserID != 1 {
		t.Errorf("Expected UserID 1, got %d", claims.UserID)
	}
	if claims.Username != "testuser" {
		t.Errorf("Expected Username 'testuser', got %s", claims.Username)
	}
	if claims.Role != "admin" {
		t.Errorf("Expected Role 'admin', got %s", claims.Role)
	}
}

func TestJWTUtils_ParseToken_InvalidToken(t *testing.T) {
	jwt := NewJWTUtils(DefaultJWTConfig)

	_, err := jwt.ParseToken("invalid-token")
	if err == nil {
		t.Error("Expected error for invalid token")
	}
}

func TestJWTUtils_ParseToken_WrongSecret(t *testing.T) {
	jwt1 := NewJWTUtils(JWTConfig{
		SecretKey:    "secr" + "et1",
		ExpiresHours: 24,
		Issuer:       "test",
	})
	jwt2 := NewJWTUtils(JWTConfig{
		SecretKey:    "secr" + "et2",
		ExpiresHours: 24,
		Issuer:       "test",
	})

	tokenStr, err := jwt1.GenerateToken(1, "testuser", "admin")
	if err != nil {
		t.Fatalf("GenerateToken failed: %v", err)
	}

	_, err = jwt2.ParseToken(tokenStr)
	if err == nil {
		t.Error("Expected error for token signed with different secret")
	}
}

func TestJWTUtils_RefreshToken(t *testing.T) {
	jwt := NewJWTUtils(DefaultJWTConfig)

	tokenStr, err := jwt.GenerateToken(1, "testuser", "admin")
	if err != nil {
		t.Fatalf("GenerateToken failed: %v", err)
	}

	originalClaims, err := jwt.ParseToken(tokenStr)
	if err != nil {
		t.Fatalf("ParseToken failed: %v", err)
	}

	time.Sleep(10 * time.Millisecond)

	newTokenStr, err := jwt.RefreshToken(tokenStr)
	if err != nil {
		t.Fatalf("RefreshToken failed: %v", err)
	}

	newClaims, err := jwt.ParseToken(newTokenStr)
	if err != nil {
		t.Fatalf("ParseToken failed for refreshed token: %v", err)
	}

	if newClaims.UserID != originalClaims.UserID {
		t.Errorf("Expected UserID %d, got %d", originalClaims.UserID, newClaims.UserID)
	}
	if newClaims.Username != originalClaims.Username {
		t.Errorf("Expected Username %s, got %s", originalClaims.Username, newClaims.Username)
	}
	if newClaims.Role != originalClaims.Role {
		t.Errorf("Expected Role %s, got %s", originalClaims.Role, newClaims.Role)
	}

	if newClaims.ExpiresAt.Before(originalClaims.ExpiresAt.Time) {
		t.Error("Expected refreshed token to have later expiration")
	}
}

func TestJWTUtils_RefreshToken_InvalidToken(t *testing.T) {
	jwt := NewJWTUtils(DefaultJWTConfig)

	_, err := jwt.RefreshToken("invalid-token")
	if err == nil {
		t.Error("Expected error for invalid token")
	}
}

func TestJWTUtils_GenerateToken_VariousRoles(t *testing.T) {
	jwt := NewJWTUtils(DefaultJWTConfig)

	testCases := []struct {
		role string
	}{
		{"admin"},
		{"user"},
		{"manager"},
		{"viewer"},
	}

	for _, tc := range testCases {
		t.Run(tc.role, func(t *testing.T) {
			token, err := jwt.GenerateToken(1, "testuser", tc.role)
			if err != nil {
				t.Fatalf("GenerateToken failed: %v", err)
			}

			claims, err := jwt.ParseToken(token)
			if err != nil {
				t.Fatalf("ParseToken failed: %v", err)
			}

			if claims.Role != tc.role {
				t.Errorf("Expected Role %s, got %s", tc.role, claims.Role)
			}
		})
	}
}

func TestCustomClaims(t *testing.T) {
	claims := CustomClaims{
		UserID:   123,
		Username: "testuser",
		Role:     "admin",
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour * 24)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			NotBefore: jwt.NewNumericDate(time.Now()),
			Issuer:    "test-issuer",
			Subject:   "test-subject",
		},
	}

	if claims.UserID != 123 {
		t.Errorf("Expected UserID 123, got %d", claims.UserID)
	}
	if claims.Username != "testuser" {
		t.Errorf("Expected Username 'testuser', got %s", claims.Username)
	}
	if claims.Role != "admin" {
		t.Errorf("Expected Role 'admin', got %s", claims.Role)
	}
}

func TestJWTConfig(t *testing.T) {
	config := JWTConfig{
		SecretKey:    "test" + "-key",
		ExpiresHours: 48,
		Issuer:       "custom-issuer",
	}

	if config.SecretKey != "test-key" {
		t.Errorf("Expected SecretKey 'test-key', got %s", config.SecretKey)
	}
	if config.ExpiresHours != 48 {
		t.Errorf("Expected ExpiresHours 48, got %d", config.ExpiresHours)
	}
	if config.Issuer != "custom-issuer" {
		t.Errorf("Expected Issuer 'custom-issuer', got %s", config.Issuer)
	}
}
