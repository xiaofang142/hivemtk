package middleware

import (
	"bufio"
	"bytes"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
)

type testResponseWriter struct {
	*httptest.ResponseRecorder
	size int
}

func (m *testResponseWriter) CloseNotify() <-chan bool {
	return nil
}

func (m *testResponseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	return nil, nil, nil
}

func (m *testResponseWriter) Pusher() (pusher http.Pusher) {
	return nil
}

func (m *testResponseWriter) Status() int {
	return m.ResponseRecorder.Code
}

func (m *testResponseWriter) Size() int {
	return m.size
}

func (m *testResponseWriter) WriteHeaderNow() {
}

func (m *testResponseWriter) Written() bool {
	return true
}

func TestJWTAuthMiddleware(t *testing.T) {
	gin.SetMode(gin.TestMode)

	t.Run("Test mode enabled", func(t *testing.T) {
		IsTestMode = true
		defer func() { IsTestMode = false }()

		w := httptest.NewRecorder()
		ctx, _ := gin.CreateTestContext(w)
		req, _ := http.NewRequest("GET", "http://test.com/test", nil)
		ctx.Request = req

		middleware := JWTAuthMiddleware()
		middleware(ctx)

		userID, exists := ctx.Get("user_id")
		if !exists {
			t.Error("Expected user_id to be set in context")
		}
		if userID != uint(1) {
			t.Errorf("Expected user_id 1, got %v", userID)
		}

		licenseID, exists := ctx.Get("license_id")
		if !exists || licenseID != "system_admin" {
			t.Errorf("Expected license_id 'system_admin', got %v", licenseID)
		}
	})

	t.Run("Missing Authorization header", func(t *testing.T) {
		IsTestMode = false
		w := httptest.NewRecorder()
		ctx, _ := gin.CreateTestContext(w)
		req, _ := http.NewRequest("GET", "http://test.com/test", nil)
		ctx.Request = req

		middleware := JWTAuthMiddleware()
		middleware(ctx)

		if w.Code != http.StatusUnauthorized {
			t.Errorf("Expected status 401, got %d", w.Code)
		}
	})

	t.Run("Invalid Authorization format", func(t *testing.T) {
		w := httptest.NewRecorder()
		ctx, _ := gin.CreateTestContext(w)
		req, _ := http.NewRequest("GET", "http://test.com/test", nil)
		req.Header.Set("Authorization", "InvalidFormat")
		ctx.Request = req

		middleware := JWTAuthMiddleware()
		middleware(ctx)

		if w.Code != http.StatusUnauthorized {
			t.Errorf("Expected status 401, got %d", w.Code)
		}
	})
}

func TestAdminAuthMiddleware(t *testing.T) {
	gin.SetMode(gin.TestMode)

	t.Run("Test mode enabled", func(t *testing.T) {
		IsTestMode = true
		defer func() { IsTestMode = false }()

		w := httptest.NewRecorder()
		ctx, _ := gin.CreateTestContext(w)
		req, _ := http.NewRequest("GET", "http://test.com/test", nil)
		ctx.Request = req

		middleware := AdminAuthMiddleware()
		middleware(ctx)

		if w.Code != http.StatusOK {
			t.Errorf("Expected status 200 in test mode, got %d", w.Code)
		}
	})

	t.Run("Missing role in context", func(t *testing.T) {
		IsTestMode = false
		w := httptest.NewRecorder()
		ctx, _ := gin.CreateTestContext(w)
		req, _ := http.NewRequest("GET", "http://test.com/test", nil)
		ctx.Request = req

		middleware := AdminAuthMiddleware()
		middleware(ctx)

		if w.Code != http.StatusUnauthorized {
			t.Errorf("Expected status 401, got %d", w.Code)
		}
	})

	t.Run("Non-admin role", func(t *testing.T) {
		w := httptest.NewRecorder()
		ctx, _ := gin.CreateTestContext(w)
		req, _ := http.NewRequest("GET", "http://test.com/test", nil)
		ctx.Request = req
		ctx.Set("role", "user")

		middleware := AdminAuthMiddleware()
		middleware(ctx)

		if w.Code != http.StatusForbidden {
			t.Errorf("Expected status 403, got %d", w.Code)
		}
	})

	t.Run("Admin role", func(t *testing.T) {
		w := httptest.NewRecorder()
		ctx, _ := gin.CreateTestContext(w)
		req, _ := http.NewRequest("GET", "http://test.com/test", nil)
		ctx.Request = req
		ctx.Set("role", "admin")

		middleware := AdminAuthMiddleware()
		middleware(ctx)

		if w.Code != http.StatusOK {
			t.Errorf("Expected status 200, got %d", w.Code)
		}
	})
}

func TestOptionalAuthMiddleware(t *testing.T) {
	gin.SetMode(gin.TestMode)

	t.Run("No Authorization header", func(t *testing.T) {
		w := httptest.NewRecorder()
		ctx, _ := gin.CreateTestContext(w)
		req, _ := http.NewRequest("GET", "http://test.com/test", nil)
		ctx.Request = req

		middleware := OptionalAuthMiddleware()
		middleware(ctx)

		if w.Code != http.StatusOK {
			t.Errorf("Expected status 200, got %d", w.Code)
		}
	})

	t.Run("Invalid Bearer token", func(t *testing.T) {
		w := httptest.NewRecorder()
		ctx, _ := gin.CreateTestContext(w)
		req, _ := http.NewRequest("GET", "http://test.com/test", nil)
		req.Header.Set("Authorization", "Bearer invalid")
		ctx.Request = req

		middleware := OptionalAuthMiddleware()
		middleware(ctx)

		if w.Code != http.StatusOK {
			t.Errorf("Expected status 200, got %d", w.Code)
		}
	})
}

func TestParseJWTToken(t *testing.T) {
	os.Setenv("JWT_SECRET", "test-secret-key")
	defer os.Unsetenv("JWT_SECRET")

	t.Run("JWT_SECRET not configured", func(t *testing.T) {
		os.Unsetenv("JWT_SECRET")
		defer os.Setenv("JWT_SECRET", "test-secret-key")

		_, err := ParseJWTToken("any-token")
		if err == nil {
			t.Error("Expected error when JWT_SECRET not configured")
		}
		if err.Error() != "JWT_SECRET 未配置" {
			t.Errorf("Expected 'JWT_SECRET 未配置' error, got %v", err)
		}
	})

	t.Run("Invalid token format", func(t *testing.T) {
		_, err := ParseJWTToken("invalid-token")
		if err == nil {
			t.Error("Expected error for invalid token")
		}
	})

	t.Run("Valid token", func(t *testing.T) {
		claims := jwt.MapClaims{
			"user_id":  float64(123),
			"username": "testuser",
			"role":     "user",
			"exp":      time.Now().Add(time.Hour).Unix(),
		}
		token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
		tokenString, err := token.SignedString([]byte("test-secret-key"))
		if err != nil {
			t.Fatalf("Failed to generate token: %v", err)
		}

		result, err := ParseJWTToken(tokenString)
		if err != nil {
			t.Fatalf("ParseJWTToken() error = %v", err)
		}
		if result == nil {
			t.Fatal("Expected non-nil result")
		}
		if result["user_id"].(float64) != 123 {
			t.Errorf("Expected user_id 123, got %v", result["user_id"])
		}
		if result["user_id"] != "test-merchant" {

		}
		if result["username"] != "testuser" {
			t.Errorf("Expected username 'testuser', got %v", result["username"])
		}
		if result["role"] != "user" {
			t.Errorf("Expected role 'user', got %v", result["role"])
		}
	})

	t.Run("Expired token", func(t *testing.T) {
		claims := jwt.MapClaims{
			"user_id":  float64(123),
			"username": "testuser",
			"role":     "user",
			"exp":      time.Now().Add(-time.Hour).Unix(),
		}
		token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
		tokenString, err := token.SignedString([]byte("test-secret-key"))
		if err != nil {
			t.Fatalf("Failed to generate token: %v", err)
		}

		_, err = ParseJWTToken(tokenString)
		if err == nil {
			t.Error("Expected error for expired token")
		}
	})

	t.Run("Token with wrong signature", func(t *testing.T) {
		claims := jwt.MapClaims{
			"user_id": float64(123),
			"exp":     time.Now().Add(time.Hour).Unix(),
		}
		token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
		tokenString, err := token.SignedString([]byte("wrong-secret"))
		if err != nil {
			t.Fatalf("Failed to generate token: %v", err)
		}

		_, err = ParseJWTToken(tokenString)
		if err == nil {
			t.Error("Expected error for token with wrong signature")
		}
	})
}

func TestTeamJWTAuthMiddleware(t *testing.T) {
	gin.SetMode(gin.TestMode)
	os.Setenv("JWT_SECRET", "test-secret-key")
	defer os.Unsetenv("JWT_SECRET")

	t.Run("Missing Authorization header", func(t *testing.T) {
		w := httptest.NewRecorder()
		ctx, _ := gin.CreateTestContext(w)
		req, _ := http.NewRequest("GET", "http://test.com/test", nil)
		ctx.Request = req

		middleware := TeamJWTAuthMiddleware()
		middleware(ctx)

		if w.Code != http.StatusUnauthorized {
			t.Errorf("Expected status 401, got %d", w.Code)
		}
	})

	t.Run("Invalid Authorization format", func(t *testing.T) {
		w := httptest.NewRecorder()
		ctx, _ := gin.CreateTestContext(w)
		req, _ := http.NewRequest("GET", "http://test.com/test", nil)
		req.Header.Set("Authorization", "InvalidFormat")
		ctx.Request = req

		middleware := TeamJWTAuthMiddleware()
		middleware(ctx)

		if w.Code != http.StatusUnauthorized {
			t.Errorf("Expected status 401, got %d", w.Code)
		}
	})

	t.Run("Valid Bearer token", func(t *testing.T) {
		claims := jwt.MapClaims{
			"user_id":  float64(123),
			"username": "testuser",
			"role":     "admin",
			"exp":      time.Now().Add(time.Hour).Unix(),
		}
		token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
		tokenString, _ := token.SignedString([]byte("test-secret-key"))

		w := httptest.NewRecorder()
		ctx, _ := gin.CreateTestContext(w)
		req, _ := http.NewRequest("GET", "http://test.com/test", nil)
		req.Header.Set("Authorization", "Bearer "+tokenString)
		ctx.Request = req

		middleware := TeamJWTAuthMiddleware()
		middleware(ctx)

		if w.Code != http.StatusOK {
			t.Errorf("Expected status 200, got %d", w.Code)
		}

		userID, exists := ctx.Get("user_id")
		if !exists {
			t.Error("Expected user_id to be set")
		}
		if userID.(float64) != 123 {
			t.Errorf("Expected user_id 123, got %v", userID)
		}

		if !exists {
			t.Errorf("Expected user test")
		}
	})

	t.Run("Test mode enabled", func(t *testing.T) {
		IsTestMode = true
		defer func() { IsTestMode = false }()

		w := httptest.NewRecorder()
		ctx, _ := gin.CreateTestContext(w)
		req, _ := http.NewRequest("GET", "http://test.com/test", nil)
		ctx.Request = req

		middleware := TeamJWTAuthMiddleware()
		middleware(ctx)

		if w.Code != http.StatusOK {
			t.Errorf("Expected status 200 in test mode, got %d", w.Code)
		}
	})
}

func TestAdminOnlyMiddleware(t *testing.T) {
	gin.SetMode(gin.TestMode)

	t.Run("Admin role", func(t *testing.T) {
		w := httptest.NewRecorder()
		ctx, _ := gin.CreateTestContext(w)
		req, _ := http.NewRequest("GET", "http://test.com/test", nil)
		ctx.Request = req
		ctx.Set("role", "admin")

		middleware := AdminOnlyMiddleware()
		middleware(ctx)

		if w.Code != http.StatusOK {
			t.Errorf("Expected status 200, got %d", w.Code)
		}
	})
}

// TestRequireAdminMiddleware A-8 收口后的等价性钉子：旧 ManagerOrAdmin 门
// （role 为 admin/manager 之一放行）已删除——manager 角色无签发链（system_users.role
// 白名单 admin/user/staff/customer_service），收口为 RequireAdminMiddleware。
// 本用例钉住新门的行为：admin 放行、manager 拒绝（403）。
func TestRequireAdminMiddleware(t *testing.T) {
	gin.SetMode(gin.TestMode)
	// 关掉测试模式短路，否则 role 校验整段被跳过，测不到门本身
	prevTestMode := IsTestMode
	IsTestMode = false
	defer func() { IsTestMode = prevTestMode }()

	t.Run("Admin role passes", func(t *testing.T) {
		w := httptest.NewRecorder()
		ctx, _ := gin.CreateTestContext(w)
		req, _ := http.NewRequest("GET", "http://test.com/test", nil)
		ctx.Request = req
		ctx.Set("role", "admin")

		middleware := RequireAdminMiddleware()
		middleware(ctx)

		if w.Code != http.StatusOK {
			t.Errorf("Expected status 200, got %d", w.Code)
		}
	})

	t.Run("Manager role rejected (A-8: no issuance chain)", func(t *testing.T) {
		w := httptest.NewRecorder()
		ctx, _ := gin.CreateTestContext(w)
		req, _ := http.NewRequest("GET", "http://test.com/test", nil)
		ctx.Request = req
		ctx.Set("role", "manager")

		middleware := RequireAdminMiddleware()
		middleware(ctx)

		if w.Code != http.StatusForbidden {
			t.Errorf("Expected status 403 for manager, got %d", w.Code)
		}
	})

	t.Run("Missing role unauthorized", func(t *testing.T) {
		w := httptest.NewRecorder()
		ctx, _ := gin.CreateTestContext(w)
		req, _ := http.NewRequest("GET", "http://test.com/test", nil)
		ctx.Request = req

		middleware := RequireAdminMiddleware()
		middleware(ctx)

		if w.Code != http.StatusUnauthorized {
			t.Errorf("Expected status 401 for missing role, got %d", w.Code)
		}
	})
}

func TestRequirePermission(t *testing.T) {
	handler := RequirePermission("test.permission")
	if handler == nil {
		t.Error("Expected non-nil handler")
	}
}

func TestAuditMiddleware_ConfigDisabled(t *testing.T) {
	gin.SetMode(gin.TestMode)

	originalEnabled := DefaultAuditConfig.Enabled
	DefaultAuditConfig.Enabled = false
	defer func() { DefaultAuditConfig.Enabled = originalEnabled }()

	w := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(w)
	req, _ := http.NewRequest("POST", "http://test.com/api/test", bytes.NewBuffer([]byte(`{"key":"value"}`)))
	ctx.Request = req

	middleware := AuditMiddleware()
	middleware(ctx)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}
}

func TestAuditMiddleware_ExcludePath(t *testing.T) {
	gin.SetMode(gin.TestMode)

	w := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(w)
	req, _ := http.NewRequest("POST", "http://test.com/api/health", nil)
	ctx.Request = req

	middleware := AuditMiddleware()
	middleware(ctx)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}
}

func TestAuditMiddleware_GETRequest(t *testing.T) {
	gin.SetMode(gin.TestMode)

	w := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(w)
	req, _ := http.NewRequest("GET", "http://test.com/api/test", nil)
	ctx.Request = req

	middleware := AuditMiddleware()
	middleware(ctx)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}
}

func TestAuditResponseWriter(t *testing.T) {
	baseWriter := &testResponseWriter{ResponseRecorder: httptest.NewRecorder()}
	// MA-03：body 由 bytes.Buffer 改为有界 cappedBufferWriter（16KB cap）。
	w := &auditResponseWriter{
		ResponseWriter: baseWriter,
		body:           &cappedBufferWriter{cap: auditRespBodyCap},
	}

	n, err := w.Write([]byte("test"))
	if err != nil {
		t.Errorf("Write failed: %v", err)
	}
	if n != 4 {
		t.Errorf("Expected 4 bytes written, got %d", n)
	}
	if string(w.body.Bytes()) != "test" {
		t.Errorf("Expected buffer 'test', got %s", string(w.body.Bytes()))
	}
}

func TestSanitizeMap(t *testing.T) {
	input := map[string]any{
		"name":     "John",
		"password": "secret123",
		"token":    "abc123",
		"nested": map[string]any{
			"secret": "hidden",
			"value":  "visible",
		},
	}

	result := sanitizeMap(input)

	if result["name"] != "John" {
		t.Errorf("Expected name 'John', got %v", result["name"])
	}
	if result["password"] != "******" {
		t.Errorf("Expected password to be masked, got %v", result["password"])
	}

	nested, ok := result["nested"].(map[string]any)
	if !ok {
		t.Fatal("Expected nested to be a map")
	}
	if nested["secret"] != "******" {
		t.Errorf("Expected nested secret to be masked, got %v", nested["secret"])
	}
}

func TestIsSensitiveField(t *testing.T) {
	sensitiveFields := []string{"password", "token", "secret"}
	for _, field := range sensitiveFields {
		if !isSensitiveField(field) {
			t.Errorf("Expected '%s' to be sensitive", field)
		}
	}

	nonSensitiveFields := []string{"name", "email"}
	for _, field := range nonSensitiveFields {
		if isSensitiveField(field) {
			t.Errorf("Expected '%s' to not be sensitive", field)
		}
	}
}

func TestGetActionFromMethod(t *testing.T) {
	tests := []struct {
		method   string
		expected string
	}{
		{"POST", "create"},
		{"PUT", "update"},
		{"DELETE", "delete"},
		{"GET", "GET"},
	}

	for _, tt := range tests {
		t.Run(tt.method, func(t *testing.T) {
			result := getActionFromMethod(tt.method)
			if result != tt.expected {
				t.Errorf("Expected action '%s', got %s", tt.expected, result)
			}
		})
	}
}

func TestGetModuleFromPath(t *testing.T) {
	tests := []struct {
		path     string
		expected string
	}{
		{"/api/team/users", "users"},
		{"/api/products/123", "products"}, // 资源 ID 不是模块名（2026-11-01 修复）
		{"/api", "unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			result := getModuleFromPath(tt.path)
			if result != tt.expected {
				t.Errorf("Expected module '%s', got %s", tt.expected, result)
			}
		})
	}
}

func TestGetResourceIDFromPath(t *testing.T) {
	tests := []struct {
		path     string
		expected string
	}{
		{"/api/users/123", "123"}, // 第 4 段的数字 ID 就是资源 ID（2026-11-01 修复）
		{"/api/users", ""},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			result := getResourceIDFromPath(tt.path)
			if result != tt.expected {
				t.Errorf("Expected resource ID '%s', got %s", tt.expected, result)
			}
		})
	}
}

func TestIsNumeric(t *testing.T) {
	tests := []struct {
		s        string
		expected bool
	}{
		{"123", true},
		{"abc", false},
		{"", false},
	}

	for _, tt := range tests {
		t.Run(tt.s, func(t *testing.T) {
			result := isNumeric(tt.s)
			if result != tt.expected {
				t.Errorf("Expected isNumeric('%s') to be %v, got %v", tt.s, tt.expected, result)
			}
		})
	}
}

func TestConvertToUint(t *testing.T) {
	tests := []struct {
		input    any
		expected uint
	}{
		{uint(5), 5},
		{int(10), 10},
		{nil, 0},
	}

	for _, tt := range tests {
		t.Run(fmt.Sprintf("%v", tt.input), func(t *testing.T) {
			result := convertToUint(tt.input)
			if result != tt.expected {
				t.Errorf("Expected convertToUint(%v) to be %d, got %d", tt.input, tt.expected, result)
			}
		})
	}
}

func TestConvertToString(t *testing.T) {
	tests := []struct {
		input    any
		expected string
	}{
		{"hello", "hello"},
		{nil, ""},
	}

	for _, tt := range tests {
		t.Run(fmt.Sprintf("%v", tt.input), func(t *testing.T) {
			result := convertToString(tt.input)
			if result != tt.expected {
				t.Errorf("Expected convertToString(%v) to be '%s', got '%s'", tt.input, tt.expected, result)
			}
		})
	}
}

func TestDataChangeMiddleware(t *testing.T) {
	gin.SetMode(gin.TestMode)

	w := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(w)
	req, _ := http.NewRequest("PUT", "http://test.com/api/test", nil)
	ctx.Request = req

	middleware := DataChangeMiddleware("test_module", "test_resource")
	middleware(ctx)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}
}
