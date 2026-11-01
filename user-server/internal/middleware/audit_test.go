package middleware

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func isNumeric(s string) bool {
	if s == "" {
		return false
	}
	if _, err := strconv.Atoi(s); err != nil {
		return false
	}
	return true
}

func splitPath(path string) []string {
	if path == "" {
		return []string{}
	}
	parts := []string{}
	for _, p := range strings.Split(path, "/") {
		if p != "" {
			parts = append(parts, p)
		}
	}
	return parts
}

// TestAuditMiddleware_Enabled 测试审计中间件启用情况
func TestAuditMiddleware_Enabled(t *testing.T) {
	gin.SetMode(gin.TestMode)

	originalEnabled := DefaultAuditConfig.Enabled
	originalExcludePaths := DefaultAuditConfig.ExcludePaths
	defer func() {
		DefaultAuditConfig.Enabled = originalEnabled
		DefaultAuditConfig.ExcludePaths = originalExcludePaths
	}()

	DefaultAuditConfig.Enabled = true
	DefaultAuditConfig.ExcludePaths = []string{"/api/health"}

	w := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(w)
	req, _ := http.NewRequest("POST", "http://test.com/api/test", bytes.NewBuffer([]byte(`{"key":"value"}`)))
	ctx.Request = req

	ctx.Set("user_id", uint(1))
	ctx.Set("username", "testuser")

	middleware := AuditMiddleware()
	middleware(ctx)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}
}

// TestAuditMiddleware_NoUserInfo 测试没有用户信息的情况
func TestAuditMiddleware_NoUserInfo(t *testing.T) {
	gin.SetMode(gin.TestMode)

	originalEnabled := DefaultAuditConfig.Enabled
	originalExcludePaths := DefaultAuditConfig.ExcludePaths
	defer func() {
		DefaultAuditConfig.Enabled = originalEnabled
		DefaultAuditConfig.ExcludePaths = originalExcludePaths
	}()

	DefaultAuditConfig.Enabled = true
	DefaultAuditConfig.ExcludePaths = []string{}

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

// TestAuditMiddleware_PATCHRequest 测试 PATCH 请求
func TestAuditMiddleware_PATCHRequest(t *testing.T) {
	gin.SetMode(gin.TestMode)

	originalEnabled := DefaultAuditConfig.Enabled
	originalExcludePaths := DefaultAuditConfig.ExcludePaths
	defer func() {
		DefaultAuditConfig.Enabled = originalEnabled
		DefaultAuditConfig.ExcludePaths = originalExcludePaths
	}()

	DefaultAuditConfig.Enabled = true
	DefaultAuditConfig.ExcludePaths = []string{}

	w := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(w)
	req, _ := http.NewRequest("PATCH", "http://test.com/api/test/123", bytes.NewBuffer([]byte(`{"key":"value"}`)))
	ctx.Request = req

	ctx.Set("user_id", uint(1))
	ctx.Set("username", "testuser")

	middleware := AuditMiddleware()
	middleware(ctx)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}
}

// TestSanitizeMap_Nested 测试嵌套 map 的敏感字段清理
func TestSanitizeMap_Nested(t *testing.T) {
	input := map[string]any{
		"name":     "John",
		"password": "secret123",
		"profile": map[string]any{
			"email": "john@example.com",
			"token": "abc123",
		},
	}

	result := sanitizeMap(input)

	if result["name"] != "John" {
		t.Errorf("Expected name 'John', got %v", result["name"])
	}
	if result["password"] != "******" {
		t.Errorf("Expected password to be masked, got %v", result["password"])
	}

	profile, ok := result["profile"].(map[string]any)
	if !ok {
		t.Fatal("Expected profile to be a map")
	}
	if profile["token"] != "******" {
		t.Errorf("Expected nested token to be masked, got %v", profile["token"])
	}
	if profile["email"] != "john@example.com" {
		t.Errorf("Expected email to be preserved, got %v", profile["email"])
	}
}

// TestGetActionFromMethod_PATCH 测试 PATCH 方法的操作类型
func TestGetActionFromMethod_PATCH(t *testing.T) {
	result := getActionFromMethod("PATCH")
	if result != "update" {
		t.Errorf("Expected action 'update' for PATCH, got %s", result)
	}
}

// TestGetModuleFromPath_Team 测试 team 模块的路径解析
func TestGetModuleFromPath_Team(t *testing.T) {
	result := getModuleFromPath("/api/team/users")
	if result != "users" {
		t.Errorf("Expected module 'users' for /api/team/users, got %s", result)
	}

	// 只有域名的路径回落域名本身（此前落 "unknown"，日志里看不出是哪个模块）
	result = getModuleFromPath("/api/team")
	if result != "team" {
		t.Errorf("Expected module 'team' for /api/team, got %s", result)
	}

	result = getModuleFromPath("/api/users")
	if result != "users" {
		t.Errorf("Expected module 'users' for /api/users, got %s", result)
	}
}

// TestGetModuleFromPath_ResourceID 资源 ID 不得被当成模块名。
// 2026-11-01 API 全量回归实测：module_stats 里长出一堆 "1413"/"5"/UUID 键，
// 按模块筛日志失效。
func TestGetModuleFromPath_ResourceID(t *testing.T) {
	cases := map[string]string{
		"/api/users/123":                             "users",
		"/api/marketing-flows/5/activate":            "marketing-flows",
		"/api/message-hub/1413/read":                 "message-hub",
		"/api/material/8cb2b6f3-ae17-4be5-9a86-d7acb7e3eaa0": "material",
		"/api/manage/order-drafts":                   "order-drafts",
		"/api/manage/order-drafts/draft_1_2/confirm": "order-drafts",
	}
	for path, want := range cases {
		if got := getModuleFromPath(path); got != want {
			t.Errorf("getModuleFromPath(%q) = %q, want %q", path, got, want)
		}
	}
}

// TestGetResourceFromPath 测试资源类型解析
func TestGetResourceFromPath(t *testing.T) {
	// 第 4 段是 ID 时，资源名回落到域名（此前返回 "123"）
	result := getResourceFromPath("/api/users/123")
	if result != "users" {
		t.Errorf("Expected resource 'users', got %s", result)
	}

	result = getResourceFromPath("/api/users")
	if result != "users" {
		t.Errorf("Expected resource 'users', got %s", result)
	}
}

// TestGetResourceFromPath_Unknown 测试未知资源类型
func TestGetResourceFromPath_Unknown(t *testing.T) {
	result := getResourceFromPath("/api")
	if result != "unknown" {
		t.Errorf("Expected resource 'unknown', got %s", result)
	}
}

// TestGetResourceIDFromPath_Numeric 测试数字 ID 解析
func TestGetResourceIDFromPath_Numeric(t *testing.T) {
	// /api/users/123 的 ID 就在第 4 段，此前判据只看第 5 段起 ⇒ 丢 ID
	result := getResourceIDFromPath("/api/users/123")
	if result != "123" {
		t.Errorf("Expected resource ID '123' for /api/users/123, got %s", result)
	}

	result = getResourceIDFromPath("/api/users/1/123")
	if result != "1" {
		t.Errorf("Expected first resource ID '1', got %s", result)
	}
}

// TestGetResourceIDFromPath_NonNumeric 非数字业务 ID（UUID/前缀 ID）也要识别
func TestGetResourceIDFromPath_NonNumeric(t *testing.T) {
	if got := getResourceIDFromPath("/api/material/8cb2b6f3-ae17-4be5-9a86-d7acb7e3eaa0"); got != "8cb2b6f3-ae17-4be5-9a86-d7acb7e3eaa0" {
		t.Errorf("UUID resource id not detected, got %q", got)
	}
	if got := getResourceIDFromPath("/api/message-hub/1413/read"); got != "1413" {
		t.Errorf("Expected '1413', got %q", got)
	}
}

// TestGetResourceIDFromPath_NoID 测试没有 ID 的情况
func TestGetResourceIDFromPath_NoID(t *testing.T) {
	result := getResourceIDFromPath("/api/users")
	if result != "" {
		t.Errorf("Expected empty resource ID, got %s", result)
	}
}

// TestIsNumeric_Valid 测试数字检测
func TestIsNumeric_Valid(t *testing.T) {
	if !isNumeric("123") {
		t.Error("Expected '123' to be numeric")
	}
	if !isNumeric("0") {
		t.Error("Expected '0' to be numeric")
	}
}

// TestIsNumeric_Invalid 测试非数字检测
func TestIsNumeric_Invalid(t *testing.T) {
	if isNumeric("abc") {
		t.Error("Expected 'abc' to be non-numeric")
	}
	if isNumeric("123a") {
		t.Error("Expected '123a' to be non-numeric")
	}
	if isNumeric("") {
		t.Error("Expected empty string to be non-numeric")
	}
}

// TestConvertToUint_VariousTypes 测试各种类型转换为 uint
func TestConvertToUint_VariousTypes(t *testing.T) {
	tests := []struct {
		input    any
		expected uint
	}{
		{uint(5), 5},
		{int(10), 10},
		{int64(15), 15},
		{float64(20.5), 20},
		{"123", 123},
		{"abc123", 123},
		{nil, 0},
		{true, 0},
	}

	for _, tt := range tests {
		t.Run("", func(t *testing.T) {
			result := convertToUint(tt.input)
			if result != tt.expected {
				t.Errorf("Expected convertToUint(%v) to be %d, got %d", tt.input, tt.expected, result)
			}
		})
	}
}

// TestConvertToString_Nil 测试 nil 转换为字符串
func TestConvertToString_Nil(t *testing.T) {
	result := convertToString(nil)
	if result != "" {
		t.Errorf("Expected empty string for nil, got %s", result)
	}
}

// TestConvertToString_String 测试字符串转换
func TestConvertToString_String(t *testing.T) {
	result := convertToString("hello")
	if result != "hello" {
		t.Errorf("Expected 'hello', got %s", result)
	}
}

// TestConvertToString_NonString 测试非字符串类型转换
func TestConvertToString_NonString(t *testing.T) {
	result := convertToString(123)
	if result != "" {
		t.Errorf("Expected empty string for non-string, got %s", result)
	}
}

// TestSplitPath 测试路径分割
func TestSplitPath(t *testing.T) {
	result := splitPath("/api/users/123")
	expected := []string{"api", "users", "123"}
	if len(result) != len(expected) {
		t.Fatalf("Expected %d parts, got %d", len(expected), len(result))
	}
	for i, part := range result {
		if part != expected[i] {
			t.Errorf("Expected part %d to be '%s', got '%s'", i, expected[i], part)
		}
	}
}

// TestSplitPath_Empty 测试空路径分割
func TestSplitPath_Empty(t *testing.T) {
	result := splitPath("")
	if len(result) != 0 {
		t.Errorf("Expected empty result for empty path, got %v", result)
	}
}

// TestSplitPath_LeadingSlash 测试带前导斜杠的路径
func TestSplitPath_LeadingSlash(t *testing.T) {
	result := splitPath("/api/test")
	if len(result) != 2 {
		t.Errorf("Expected 2 parts, got %d", len(result))
	}
}

// TestIsSensitiveField 测试敏感字段检测
func TestIsSensitiveField_Extended(t *testing.T) {
	sensitiveFields := []string{"password", "token", "secret", "old_password", "new_password"}
	for _, field := range sensitiveFields {
		if !isSensitiveField(field) {
			t.Errorf("Expected '%s' to be sensitive", field)
		}
	}

	nonSensitiveFields := []string{"name", "email", "phone"}
	for _, field := range nonSensitiveFields {
		if isSensitiveField(field) {
			t.Errorf("Expected '%s' to not be sensitive", field)
		}
	}
}

// TestLogLogin 测试登录日志记录
func TestLogLogin(t *testing.T) {
	LogLogin(1, "testuser", "127.0.0.1", "test-agent")
	t.Log("LogLogin completed without panic")
}

// TestLogLogout 测试登出日志记录
func TestLogLogout(t *testing.T) {
	LogLogout(1, "testuser", "127.0.0.1")
	t.Log("LogLogout completed without panic")
}

// TestLogCustom 测试自定义日志记录
func TestLogCustom(t *testing.T) {
	LogCustom(1, "testuser", "test_action", "test_module", "test_resource", "123", map[string]string{"key": "value"})
	t.Log("LogCustom completed without panic")
}

// TestDataChangeMiddleware 测试数据变更中间件
func TestDataChangeMiddleware_PUT(t *testing.T) {
	gin.SetMode(gin.TestMode)

	w := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(w)
	req, _ := http.NewRequest("PUT", "http://test.com/api/test", bytes.NewBuffer([]byte(`{"key":"value"}`)))
	ctx.Request = req

	middleware := DataChangeMiddleware("test_module", "test_resource")
	middleware(ctx)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}
}

func TestDataChangeMiddleware_DELETE(t *testing.T) {
	gin.SetMode(gin.TestMode)

	w := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(w)
	req, _ := http.NewRequest("DELETE", "http://test.com/api/test/123", nil)
	ctx.Request = req

	middleware := DataChangeMiddleware("test_module", "test_resource")
	middleware(ctx)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}
}

func TestDataChangeMiddleware_POST(t *testing.T) {
	gin.SetMode(gin.TestMode)

	w := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(w)
	req, _ := http.NewRequest("POST", "http://test.com/api/test", bytes.NewBuffer([]byte(`{"key":"value"}`)))
	ctx.Request = req

	middleware := DataChangeMiddleware("test_module", "test_resource")
	middleware(ctx)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}
}

// TestAuditResponseWriter_WriteString 测试 WriteString 方法
func TestAuditResponseWriter_WriteString(t *testing.T) {
	baseWriter := httptest.NewRecorder()
	w := &auditResponseWriter{
		ResponseWriter: &testResponseWriter{ResponseRecorder: baseWriter},
		body:           &bytes.Buffer{},
	}

	n, err := w.WriteString("test string")
	if err != nil {
		t.Errorf("WriteString failed: %v", err)
	}
	if n != 11 {
		t.Errorf("Expected 11 bytes written, got %d", n)
	}
	if w.body.String() != "test string" {
		t.Errorf("Expected buffer 'test string', got %s", w.body.String())
	}
}

// TestSaveAuditBatch_Empty 测试空批次保存
func TestSaveAuditBatch_Empty(t *testing.T) {

	var logs []*AuditEntry
	NewAuditManager().saveAuditBatch(logs)
	t.Log("saveAuditBatch with empty slice completed without panic")
}
