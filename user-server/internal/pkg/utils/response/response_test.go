package response

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"hivemtk-user/internal/pkg/i18n"
	"hivemtk-user/internal/pkg/utils"

	bizerr "hivemtk-user/internal/domain/errors"

	"github.com/gin-gonic/gin"
)

func setupTestContext() (*gin.Context, *httptest.ResponseRecorder) {
	w := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(w)
	req, _ := http.NewRequest("GET", "http://test.com", nil)
	ctx.Request = req
	return ctx, w
}

func TestSuccess(t *testing.T) {
	ctx, w := setupTestContext()

	data := map[string]string{"key": "value"}
	Success(ctx, data, "success")

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}
}

func TestError(t *testing.T) {
	ctx, w := setupTestContext()

	Error(ctx, utils.ErrorCodeInvalidParameter, "bad request")

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected status 400, got %d", w.Code)
	}
}

func TestErrorWithDetails(t *testing.T) {
	ctx, w := setupTestContext()

	Error(ctx, utils.ErrorCodeInvalidParameter, "bad request", "detail info")

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected status 400, got %d", w.Code)
	}
}

func TestErrorWithLog(t *testing.T) {
	ctx, w := setupTestContext()

	ErrorWithLog(ctx, utils.ErrorCodeInvalidParameter, "bad request")

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected status 400, got %d", w.Code)
	}
}

func TestValidationError(t *testing.T) {
	ctx, w := setupTestContext()

	ValidationError(ctx, "validation error", "field is required")

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected status 400, got %d", w.Code)
	}
}

func TestDatabaseError(t *testing.T) {
	ctx, w := setupTestContext()

	DatabaseError(ctx, "db error")

	if w.Code != http.StatusInternalServerError {
		t.Errorf("Expected status 500, got %d", w.Code)
	}
}

func TestBusinessError(t *testing.T) {
	ctx, w := setupTestContext()

	BusinessError(ctx, "business error", "invalid state")

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected status 400, got %d", w.Code)
	}
}

func TestAuthError(t *testing.T) {
	ctx, w := setupTestContext()

	AuthError(ctx, "unauthorized")

	if w.Code != http.StatusUnauthorized {
		t.Errorf("Expected status 401, got %d", w.Code)
	}
}

func TestSuccessWithList(t *testing.T) {
	ctx, w := setupTestContext()

	data := []map[string]string{{"key": "value"}}
	SuccessWithList(ctx, data, 100)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}
}

func TestSuccessWithPage(t *testing.T) {
	ctx, w := setupTestContext()

	data := []map[string]string{{"key": "value"}}
	SuccessWithPage(ctx, data, 1, 10, 100)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}
}

func TestErrorWithCode(t *testing.T) {
	ctx, w := setupTestContext()

	ErrorWithCode(ctx, utils.ErrorCodeInvalidParameter, http.StatusBadRequest, "custom error")

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected status 400, got %d", w.Code)
	}
}

func TestNotFoundError(t *testing.T) {
	ctx, w := setupTestContext()

	NotFoundError(ctx, "user")

	if w.Code != http.StatusNotFound {
		t.Errorf("Expected status 404, got %d", w.Code)
	}
}

func TestNotFoundErrorEmptyResource(t *testing.T) {
	ctx, w := setupTestContext()

	NotFoundError(ctx, "")

	if w.Code != http.StatusNotFound {
		t.Errorf("Expected status 404, got %d", w.Code)
	}
}

func TestInvalidParameterError(t *testing.T) {
	ctx, w := setupTestContext()

	InvalidParameterError(ctx, "email", "邮箱格式不正确")

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected status 400, got %d", w.Code)
	}
}

func TestOperationFailedError(t *testing.T) {
	ctx, w := setupTestContext()

	OperationFailedError(ctx, "创建订单")

	if w.Code != http.StatusInternalServerError {
		t.Errorf("Expected status 500, got %d", w.Code)
	}
}

func TestOperationFailedErrorEmptyOperation(t *testing.T) {
	ctx, w := setupTestContext()

	OperationFailedError(ctx, "")

	if w.Code != http.StatusInternalServerError {
		t.Errorf("Expected status 500, got %d", w.Code)
	}
}

func TestFileUploadError(t *testing.T) {
	ctx, w := setupTestContext()

	FileUploadError(ctx, utils.ErrorCodeUploadFailed, "文件上传失败")

	if w.Code != http.StatusInternalServerError {
		t.Errorf("Expected status 500, got %d", w.Code)
	}
}

func TestFileUploadErrorEmptyCode(t *testing.T) {
	ctx, w := setupTestContext()

	FileUploadError(ctx, "", "文件上传失败")

	if w.Code != http.StatusInternalServerError {
		t.Errorf("Expected status 500, got %d", w.Code)
	}
}

// TestError_EmptyMessage tests Error with empty message to trigger default message lookup
func TestError_EmptyMessage(t *testing.T) {
	ctx, w := setupTestContext()

	Error(ctx, utils.ErrorCodeInvalidParameter, "")

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected status 400, got %d", w.Code)
	}
}

// TestErrorWithLog_WithDetails tests ErrorWithLog with details to cover the details branch
func TestErrorWithLog_WithDetails(t *testing.T) {
	ctx, w := setupTestContext()

	ErrorWithLog(ctx, utils.ErrorCodeInvalidParameter, "error with details", "detail info")

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected status 400, got %d", w.Code)
	}
}

func TestError_IntCode(t *testing.T) {
	ctx, w := setupTestContext()

	Error(ctx, http.StatusServiceUnavailable, "service unavailable")

	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("Expected status 503, got %d", w.Code)
	}
}

func TestError_UnknownCode(t *testing.T) {
	ctx, w := setupTestContext()

	Error(ctx, "unknown_type", "unknown error")

	if w.Code != http.StatusInternalServerError {
		t.Errorf("Expected status 500, got %d", w.Code)
	}
}

func TestError_WithDetailData(t *testing.T) {
	ctx, w := setupTestContext()

	detailData := map[string]string{"field": "email", "reason": "invalid format"}
	Error(ctx, utils.ErrorCodeInvalidParameter, "bad request", detailData)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected status 400, got %d", w.Code)
	}
}

// TestErrorCodeFromHTTPCode tests all branches in errorCodeFromHTTPCode
func TestErrorCodeFromHTTPCode(t *testing.T) {
	tests := []struct {
		httpCode     int
		expectedCode utils.ErrorCode
		name         string
	}{
		{http.StatusBadRequest, utils.ErrorCodeInvalidParameter, "400"},
		{http.StatusUnauthorized, utils.ErrorCodeUnauthorized, "401"},
		{http.StatusForbidden, utils.ErrorCodeForbidden, "403"},
		{http.StatusNotFound, utils.ErrorCodeNotFound, "404"},
		{http.StatusConflict, utils.ErrorCodeDuplicateEntry, "409"},
		{http.StatusGone, utils.ErrorCodeLegacyGone, "410"},
		{http.StatusTooManyRequests, utils.ErrorCodeInsufficientQuota, "429"},
		{http.StatusInternalServerError, utils.ErrorCodeInternalError, "500"},
		{http.StatusServiceUnavailable, utils.ErrorCodeServiceUnavailable, "503"},
		{http.StatusGatewayTimeout, utils.ErrorCodeUnknown, "default"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := errorCodeFromHTTPCode(tt.httpCode)
			if result != tt.expectedCode {
				t.Errorf("errorCodeFromHTTPCode(%d) = %v, want %v", tt.httpCode, result, tt.expectedCode)
			}
		})
	}
}

// TestError_BusinessCodeDoesNotPanicWriter 钉住这一族的回归：业务错误码（10001 这类
// 5 位数）不是合法的 HTTP 状态码，直传 gin 会 panic("invalid WriteHeader code 10001")，
// 整条请求拿不到响应体。修后必须降级成一条可读的 500。
func TestError_BusinessCodeDoesNotPanicWriter(t *testing.T) {
	ctx, w := setupTestContext()

	Error(ctx, 10001, "用户不存在")

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("越界业务码应降级为 500, got %d", w.Code)
	}
	var body Response
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("响应体必须是合法 JSON, err=%v body=%q", err, w.Body.String())
	}
	if got, ok := body.Code.(string); !ok || got != string(utils.ErrorCodeUnknown) {
		t.Errorf("code = %#v, want %q（业务码不得冒充 HTTP 状态码语义）", body.Code, string(utils.ErrorCodeUnknown))
	}
}

// TestError_OutOfRangeCodesAllDegraded 覆盖合法 HTTP 域 [100,999] 之外的两侧边界，
// 含文档旧段（10000-19999）与实际存在的 5 位健康码 50301。
func TestError_OutOfRangeCodesAllDegraded(t *testing.T) {
	for _, code := range []int{0, 1, 99, 1000, 5001, 10001, 19999, 50301, -1} {
		ctx, w := setupTestContext()
		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("Error(%d) panic: %v", code, r)
				}
			}()
			Error(ctx, code, "x")
		}()
		if w.Code != http.StatusInternalServerError {
			t.Errorf("Error(%d) 应降级为 500, got %d", code, w.Code)
		}
	}
}

// TestError_ValidHTTPStatusUnchanged 是上一枚的极性对：合法域内的状态码必须逐字保持
// 原行为（加校验不得把正常通道一起改掉）。
func TestError_ValidHTTPStatusUnchanged(t *testing.T) {
	for _, code := range []int{http.StatusBadRequest, http.StatusUnauthorized, http.StatusForbidden,
		http.StatusNotFound, http.StatusConflict, http.StatusUnprocessableEntity,
		http.StatusTooManyRequests, http.StatusInternalServerError, http.StatusServiceUnavailable,
		http.StatusGatewayTimeout, 999} {
		ctx, w := setupTestContext()
		Error(ctx, code, "x")
		if w.Code != code {
			t.Errorf("Error(%d) 状态码应原样透传, got %d", code, w.Code)
		}
	}
}

// TestResponsePath_RequestlessContextDoesNotPanic 钉住写响应这条路的第二个引信：
// gin 的 GetHeader 会直取 c.Request.Header，而 gin.CreateTestContext 造出的 Context
// 不带 Request（本仓 104 处测试用它）。localeOf 早先只判 c != nil，于是在
// 「根本没有请求」的上下文上以段错误收场——本地化失败应当回退中文，而不是打断响应。
func TestResponsePath_RequestlessContextDoesNotPanic(t *testing.T) {
	mustBeRequestless := func(ctx *gin.Context) {
		t.Helper()
		if ctx.Request != nil {
			t.Fatalf("夹具前提不成立：该 Context 已带 Request，测不到 nil 分支")
		}
	}

	t.Run("Error", func(t *testing.T) {
		w := httptest.NewRecorder()
		ctx, _ := gin.CreateTestContext(w)
		mustBeRequestless(ctx)
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("Error 在 nil Request 上下文上 panic: %v", r)
			}
		}()
		Error(ctx, http.StatusBadRequest, "参数错误")
		if w.Code != http.StatusBadRequest {
			t.Errorf("状态码应原样透传, got %d", w.Code)
		}
	})

	t.Run("Success", func(t *testing.T) {
		w := httptest.NewRecorder()
		ctx, _ := gin.CreateTestContext(w)
		mustBeRequestless(ctx)
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("Success 在 nil Request 上下文上 panic: %v", r)
			}
		}()
		Success(ctx, gin.H{"k": "v"}, "ok")
		if w.Code != http.StatusOK {
			t.Errorf("got %d, want 200", w.Code)
		}
	})

	t.Run("ErrorWithBusinessCode", func(t *testing.T) {
		w := httptest.NewRecorder()
		ctx, _ := gin.CreateTestContext(w)
		mustBeRequestless(ctx)
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("ErrorWithBusinessCode 在 nil Request 上下文上 panic: %v", r)
			}
		}()
		ErrorWithBusinessCode(ctx, 6001, "资产不存在")
		var body Response
		if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
			t.Fatalf("响应体必须是合法 JSON, err=%v body=%q", err, w.Body.String())
		}
		if got, ok := body.Code.(float64); !ok || int(got) != 6001 {
			t.Errorf("code = %#v, want 6001（业务码走响应体）", body.Code)
		}
	})

	t.Run("LocaleOf", func(t *testing.T) {
		w := httptest.NewRecorder()
		ctx, _ := gin.CreateTestContext(w)
		mustBeRequestless(ctx)
		if got := LocaleOf(ctx); got != i18n.ZH {
			t.Errorf("LocaleOf = %v, want 缺省中文", got)
		}
	})
}

// TestErrorFromDB_ClassifiedBranchesMaskRawError（S-36）：4xx/502 分类分支
// 同样不能把 err.Error() 带出——无论它藏在 fallback 还是 data 里。
func TestErrorFromDB_ClassifiedBranchesMaskRawError(t *testing.T) {
	const raw = `relation "secret_table" does not exist (SQLSTATE 42P01)`

	cases := []struct {
		name       string
		err        error
		fallback   string
		data       []any
		wantStatus int
	}{
		{
			name:       "not found：原文拼进 fallback",
			err:        errors.New("record not found: " + raw),
			fallback:   "查询失败: " + raw,
			wantStatus: http.StatusNotFound,
		},
		{
			name:       "invalid input：原文塞进 data",
			err:        errors.New("invalid input: " + raw),
			fallback:   "参数错误",
			data:       []any{raw},
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "conflict：原文既进 fallback 又进 data",
			err:        errors.New("duplicate key value violates unique constraint " + raw),
			fallback:   "创建失败: " + raw,
			data:       []any{gin.H{"detail": raw}},
			wantStatus: http.StatusConflict,
		},
		{
			name:       "forbidden：data 夹带原文",
			err:        errors.New("forbidden: " + raw),
			fallback:   "无权限",
			data:       []any{raw},
			wantStatus: http.StatusForbidden,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c, w := setupTestContext()
			ErrorFromDB(c, tc.err, tc.fallback, tc.data...)

			if w.Code != tc.wantStatus {
				t.Fatalf("状态码 = %d，期望 %d，body=%s", w.Code, tc.wantStatus, w.Body.String())
			}
			body := w.Body.String()
			if strings.Contains(body, "secret_table") || strings.Contains(body, "SQLSTATE") {
				t.Errorf("分类分支泄漏原始错误: %s", body)
			}
		})
	}
}
func TestErrorFromDB_TerminalMasksRawError(t *testing.T) {
	raw := `pq: syntax error at or near "SELEC" (SQLSTATE 42601), relation "bills_x"` // 表名+SQLSTATE，泄露即肉眼可见

	cases := []struct {
		name        string
		fallback    string
		data        []any
		wantBlocked bool // true = 响应体不允许出现原文
	}{
		{"空 fallback", "", nil, true},
		{"原文当 fallback", raw, nil, true},
		{"原文塞 data", "导出失败", []any{raw}, true},
		{"业务文案不受影响", "导出任务提交成功但状态刷新失败", nil, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request = httptest.NewRequest("POST", "/t", nil)

			err := errors.New(raw)
			ErrorFromDB(c, err, tc.fallback, tc.data...)

			if w.Code != http.StatusInternalServerError {
				t.Fatalf("未知错误应落 500，得到 %d", w.Code)
			}
			body := w.Body.String()
			if tc.wantBlocked && strings.Contains(body, "SQLSTATE") {
				t.Errorf("原文泄漏出境: %s", body)
			}
			if !tc.wantBlocked && !strings.Contains(body, tc.fallback) {
				t.Errorf("业务文案被误掩: %s", body)
			}
		})
	}
}

// TestErrorFromDB_UpstreamKeepsControlledReasonDropsRawData（S-36）：
// 502 分支保留 BizError 里写给用户的原因，但 data 中的原始错误仍要丢弃。
func TestErrorFromDB_UpstreamKeepsControlledReasonDropsRawData(t *testing.T) {
	const raw = `dial tcp 10.0.0.9:443: connection refused`
	c, w := setupTestContext()

	err := bizerr.Wrap(bizerr.CodePlatformUnavail, "渠道不可达或凭据无效", errors.New(raw))
	ErrorFromDB(c, err, "发送失败: "+raw, raw)

	if w.Code != http.StatusBadGateway {
		t.Fatalf("上游失败应 502，实际 %d，body=%s", w.Code, w.Body.String())
	}
	body := w.Body.String()
	if strings.Contains(body, raw) || strings.Contains(body, "10.0.0.9") {
		t.Errorf("上游分支泄漏原始连接错误: %s", body)
	}
	if !strings.Contains(body, "渠道不可达") {
		t.Errorf("上游分支应保留受控原因，body=%s", body)
	}
}
