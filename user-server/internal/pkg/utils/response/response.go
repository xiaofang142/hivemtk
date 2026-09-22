package response

import (
	"errors"
	"net/http"
	"strings"

	"hivemtk-user/internal/pkg/i18n"
	"hivemtk-user/internal/pkg/utils"
	"hivemtk-user/internal/pkg/utils/logger"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// LocaleOf 从 gin 上下文解析请求语言（由 LocaleMiddleware 注入，缺省回退中文）。
// 业务层据此把返回的提示/消息本地化为对应语言。导出供 controller 复用。
func LocaleOf(c *gin.Context) i18n.Locale {
	return localeOf(c)
}

func localeOf(c *gin.Context) i18n.Locale {
	if c != nil {
		if v, ok := c.Get("locale"); ok {
			if l, ok := v.(i18n.Locale); ok {
				return l
			}
		}
		if h := c.GetHeader("X-Locale"); h != "" {
			return i18n.Parse(h)
		}
		if h := c.GetHeader("Accept-Language"); h != "" {
			return i18n.ParseAcceptLanguage(h)
		}
	}
	return i18n.ZH
}

// Response 统一响应结构
type Response struct {
	Code    any    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

// Success 成功响应
// 规范: code 用 int 0 (不是 string "SUCCESS")，与 CLAUDE.md 架构规范一致
func Success(c *gin.Context, data any, message string) {
	c.JSON(http.StatusOK, Response{
		Code:    0,
		Message: i18n.Localize(localeOf(c), message),
		Data:    data,
	})
}

// Accepted 以 202 返回"已受理、结论未定"的成功响应，body 与 Success 同构（code=0 + data）。
//
// 为什么单列一个出口而不是让调用方自己写状态码：本仓的控制器一律禁止 c.JSON
// （check-architecture.sh 的 [L3] 那条），而 202 与 200 的差别不是"另一个状态码"，
// 是"这一次调用没有给出终态"。报价发送是本仓第一个用到这一档的动作：
// 审批没结论时它既不是成功也不是失败，回 200 会被前端渲染成"已发送"，
// 回 409 会被读成"你的请求被拒了"，两种都是错的结论。
func Accepted(c *gin.Context, data any, message string) {
	c.JSON(http.StatusAccepted, Response{
		Code:    0,
		Message: i18n.Localize(localeOf(c), message),
		Data:    data,
	})
}

// SuccessWithList 成功响应（带分页列表）
// data 字段返回 {list, total} 结构，便于前端统一解析 res.list 与 res.total
func SuccessWithList(c *gin.Context, data any, total int64) {
	c.JSON(http.StatusOK, gin.H{
		"code":    0,
		"message": i18n.T(localeOf(c), "success"),
		"data": gin.H{
			"list":  data,
			"total": total,
		},
	})
}

// SuccessWithPage 成功响应（带分页信息）
// data 字段返回 {list, total, page, page_size} 结构，便于前端统一解析
func SuccessWithPage(c *gin.Context, data any, page, pageSize int64, total int64) {
	c.JSON(http.StatusOK, gin.H{
		"code":    0,
		"message": i18n.T(localeOf(c), "success"),
		"data": gin.H{
			"list":      data,
			"total":     total,
			"page":      page,
			"page_size": pageSize,
		},
	})
}

// Error 错误响应 - 支持 ErrorCode 和 int 两种类型
func Error(c *gin.Context, code any, message string, data ...any) {
	var httpCode int
	var errorCode any

	switch v := code.(type) {
	case utils.ErrorCode:
		config := utils.GetErrorCodeConfig(v)
		httpCode = config.HTTPCode
		errorCode = v
		if message == "" {
			message = config.LocalizedMessage(localeOf(c))
		}
	case int:
		httpCode = v
		errorCode = errorCodeFromHTTPCode(v)
	default:
		httpCode = http.StatusInternalServerError
		errorCode = utils.ErrorCodeUnknown
	}

	resp := Response{
		Code:    errorCode,
		Message: i18n.Localize(localeOf(c), message),
		Data:    nil,
	}

	if len(data) > 0 {
		resp.Data = data[0]
	}

	c.JSON(httpCode, resp)
}

// ErrorFromDB 依据错误类型返回恰当的 HTTP 状态码，避免把可预期错误误报为 500：
//   - gorm.ErrRecordNotFound -> 404（资源不存在）
//   - utils.ErrInvalidInput  -> 400（输入校验失败）
//   - 其它                    -> 500（保留原始消息便于排查）
//
// 用于替换 controller 中散落的 `response.Error(c, 500, err.Error())`，
// 让「操作一个已不存在的资源」或「缺必填字段」返回正确的语义状态码。
func ErrorFromDB(c *gin.Context, err error, fallbackMsg string, data ...any) {
	if err == nil {
		return
	}
	msg := err.Error()
	if errors.Is(err, gorm.ErrRecordNotFound) {
		NotFound(c, fallbackMsg)
		return
	}
	if errors.Is(err, utils.ErrInvalidInput) {
		Error(c, http.StatusBadRequest, fallbackMsg, data...)
		return
	}
	if errors.Is(err, utils.ErrUnauthorized) {
		Error(c, http.StatusUnauthorized, fallbackMsg, data...)
		return
	}
	if errors.Is(err, utils.ErrForbidden) {
		Error(c, http.StatusForbidden, fallbackMsg, data...)
		return
	}
	if strings.Contains(msg, "record not found") || strings.Contains(msg, "not found") || strings.Contains(msg, "不存在") {
		NotFound(c, fallbackMsg)
		return
	}
	if strings.Contains(msg, "does not belong to current user") || strings.Contains(msg, "无权限") || strings.Contains(msg, "forbidden") {
		Error(c, http.StatusForbidden, fallbackMsg, data...)
		return
	}
	if strings.Contains(msg, "invalid input") ||
		strings.Contains(msg, "非法") ||
		strings.Contains(msg, "参数") ||
		strings.Contains(msg, "不能为空") ||
		strings.Contains(msg, "缺失") ||
		strings.Contains(msg, "required") ||
		strings.Contains(msg, "validation") ||
		strings.Contains(msg, "至少需要") ||
		strings.Contains(msg, "缺 ") ||
		strings.Contains(msg, "取值") ||
		strings.Contains(msg, "格式") ||
		strings.Contains(msg, "校验") ||
		strings.Contains(msg, "不符合") {
		Error(c, http.StatusBadRequest, fallbackMsg, data...)
		return
	}
	if fallbackMsg == "" {
		fallbackMsg = msg
	}
	Error(c, http.StatusInternalServerError, fallbackMsg, data...)
}

func errorCodeFromHTTPCode(code int) utils.ErrorCode {
	switch code {
	case http.StatusBadRequest:
		return utils.ErrorCodeInvalidParameter
	case http.StatusUnauthorized:
		return utils.ErrorCodeUnauthorized
	case http.StatusForbidden:
		return utils.ErrorCodeForbidden
	case http.StatusNotFound:
		return utils.ErrorCodeNotFound
	case http.StatusConflict:
		return utils.ErrorCodeDuplicateEntry
	case http.StatusTooManyRequests:
		return utils.ErrorCodeInsufficientQuota
	case http.StatusInternalServerError:
		return utils.ErrorCodeInternalError
	case http.StatusServiceUnavailable:
		return utils.ErrorCodeServiceUnavailable
	default:
		return utils.ErrorCodeUnknown
	}
}

// ErrorWithCode 错误响应（指定 HTTP 状态码）
func ErrorWithCode(c *gin.Context, errorCode utils.ErrorCode, httpCode int, message string) {
	c.JSON(httpCode, Response{
		Code:    errorCode,
		Message: i18n.Localize(localeOf(c), message),
		Data:    nil,
	})
}

// ErrorWithLog 错误响应并记录日志
func ErrorWithLog(c *gin.Context, errorCode utils.ErrorCode, message string, details ...string) {
	detail := ""
	if len(details) > 0 {
		detail = details[0]
	}
	_ = utils.LogErrorWithRequest(c, utils.NewAppError(utils.ErrorTypeSystem, message, utils.GetHTTPCode(errorCode), detail))

	Error(c, errorCode, message)
}

// ValidationError 参数验证错误响应
func ValidationError(c *gin.Context, message string, details ...string) {
	detail := ""
	if len(details) > 0 {
		detail = details[0]
	}
	Error(c, utils.ErrorCodeValidation, message, detail)
}

// DatabaseError 数据库错误响应
func DatabaseError(c *gin.Context, details ...string) {
	detail := ""
	if len(details) > 0 {
		detail = details[0]
	}
	Error(c, utils.ErrorCodeDatabaseError, i18n.T(localeOf(c), "db_operation_failed"), detail)
}

// BusinessError 业务错误响应
func BusinessError(c *gin.Context, message string, details ...string) {
	detail := ""
	if len(details) > 0 {
		detail = details[0]
	}
	Error(c, utils.ErrorCodeBusiness, message, detail)
}

// ErrorWithBusinessCode 以 HTTP 200 返回业务错误码响应。
//
// 适用场景：业务错误码（如 5001/6001）只能放在响应体 code 字段，
// 不能作为 HTTP 状态码（gin 会 panic: invalid WriteHeader code）。
// 前端按响应体 code 判断成功/失败，与平台返回约定一致。
//
// 对应架构规范：controller 应使用 response.* 统一响应，禁止直接 c.JSON。
//
// 可选 data 参数：传入非 nil 值（如 gin.H{}）时会在响应体中携带 data 字段，
// 避免 data:null 占位符；不传则 data 字段被 omitempty 省略。
func ErrorWithBusinessCode(c *gin.Context, code int, message string, data ...any) {
	var d any
	if len(data) > 0 {
		d = data[0]
	}
	c.JSON(http.StatusOK, Response{
		Code:    code,
		Message: i18n.Localize(localeOf(c), message),
		Data:    d,
	})
}

// AuthError 认证错误响应
func AuthError(c *gin.Context, message string) {
	Error(c, utils.ErrorCodeUnauthorized, message)
}

// NotFoundError 资源不存在错误响应
func NotFoundError(c *gin.Context, resource string) {
	message := utils.GetUserMessageLoc(utils.ErrorCodeNotFound, localeOf(c))
	if resource != "" {
		message = i18n.T(localeOf(c), "resource_not_exist", resource)
	}
	Error(c, utils.ErrorCodeNotFound, message)
}

// NotFound 以 404 状态码响应记录不存在的错误
func NotFound(c *gin.Context, msg string) {
	if msg == "" {
		msg = i18n.T(localeOf(c), "record_not_found")
	}
	Error(c, utils.ErrorCodeNotFound, msg)
}

// InvalidParameterError 参数无效错误响应
func InvalidParameterError(c *gin.Context, field string, message string) {
	Error(c, utils.ErrorCodeInvalidParameter, message, gin.H{"field": field})
}

// OperationFailedError 操作失败错误响应
func OperationFailedError(c *gin.Context, operation string) {
	message := i18n.T(localeOf(c), "operation_failed", operation)
	Error(c, utils.ErrorCodeOperationFailed, message)
}

// FileUploadError 文件上传错误响应
func FileUploadError(c *gin.Context, errorCode utils.ErrorCode, message string) {
	if errorCode == "" {
		errorCode = utils.ErrorCodeUploadFailed
	}
	Error(c, errorCode, message)
}

// BindJSON 安全地绑定 JSON 请求体到 obj。
//
// 设计动机：
//   - 历史 controller 普遍 `if err := c.ShouldBindJSON(&req); err != nil {
//     response.Error(c, 400, "参数错误: "+err.Error())`，将
//     GORM/validator 内部错误细节（字段名、JSON tag、类型约束）泄露给客户端，
//     攻击者可借此推断数据模型结构，便于构造后续攻击。
//   - 内部错误用 logger.Errorf 记录，客户端只看到通用 "无效的请求参数"。
//
// 用法：
//
//	if !response.BindJSON(c, &req) {
//	    return
//	}
//
// 返回 true 表示绑定成功，false 表示已写入 400 响应、调用方应直接 return。
func BindJSON(c *gin.Context, obj any) bool {
	if err := c.ShouldBindJSON(obj); err != nil {
		logger.Errorf("BindJSON failed on %s %s: %v", c.Request.Method, c.Request.URL.Path, err)
		Error(c, http.StatusBadRequest, i18n.T(localeOf(c), "invalid_params"))
		return false
	}
	return true
}

// BindQuery 安全地绑定 query string 到 obj（同 BindJSON 的安全模式）。
func BindQuery(c *gin.Context, obj any) bool {
	if err := c.ShouldBindQuery(obj); err != nil {
		logger.Errorf("BindQuery failed on %s %s: %v", c.Request.Method, c.Request.URL.Path, err)
		Error(c, http.StatusBadRequest, i18n.T(localeOf(c), "invalid_params"))
		return false
	}
	return true
}
