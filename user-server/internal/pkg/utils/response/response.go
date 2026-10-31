package response

import (
	"errors"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
	"hivemtk-user/internal/pkg/i18n"
	"hivemtk-user/internal/pkg/utils"
	"hivemtk-user/internal/pkg/utils/logger"
	"net/http"
	"strings"

	bizerr "hivemtk-user/internal/domain/errors"
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
		// gin 的 GetHeader 直取 c.Request.Header，请求位为空时是段错误而不是空串。
		// 写响应这条路上确实会出现没有 Request 的上下文（测试与手构的 Context），
		// 而本地化失败的正确后果是回退中文，不是把整条响应打断。
		if c.Request == nil {
			return i18n.ZH
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
		// 业务错误码（如 10001/5001）不是合法的 HTTP 状态码，直传 c.JSON 会打到
		// gin 的 "invalid WriteHeader code" panic，整条请求拿不到响应体。
		// 这里降级为 500 并出声，让写错的调用方可诊断，而不是静默断连。
		// 业务码应当经 ErrorWithBusinessCode 走响应体 code 字段。
		if v < 100 || v > 999 {
			logger.Errorf("response.Error received non-HTTP status code %d; business codes must go through ErrorWithBusinessCode", v)
			httpCode = http.StatusInternalServerError
			errorCode = utils.ErrorCodeUnknown
			break
		}
		httpCode = v
		errorCode = errorCodeFromHTTPCode(v)
	default:
		httpCode = http.StatusInternalServerError
		errorCode = utils.ErrorCodeUnknown
	}

	// 入参校验错误的通用文案（「无效的请求参数」）会把真正的字段原因埋在 data 里，
	// 前端只能看到一句无从下手的话（A20 一类）。data[0] 是绑定/校验错误字符串时，
	// 把它并入 message —— 绑定错误只含字段名与 tag，不含敏感值。
	if len(data) > 0 {
		if detail, ok := data[0].(string); ok && detail != "" &&
			(message == ErrInvalidParams || message == i18n.T(localeOf(c), "invalid_params")) {
			message = message + ": " + detail
		}
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
	// 上游/渠道失败**不是**"记录不存在"（与 errhttp.isNotFoundError 同一条判据）。
	//
	// 下面那条按文本判定的分支里含 "not found"/"不存在"，而渠道原文常自带这些词
	// —— 阿里云短信回 "Specified access key is not found." ——
	// 于是"密钥填错了"会落成 404「记录不存在」，把操作者引去查记录而不是改密钥。
	// 错误自己声明了它是上游失败（bizerr 5xxx）时，走 502 而不是 404。
	//
	// 消息用 BizError 的完整串（Message + cause）：cause 里有渠道给的真因
	// （"Specified access key is not found."），只取 Message 会把真因丢掉。
	// 不用 fallbackMsg：fallbackMsg 只有"发送草稿失败"这种操作名，
	// 操作者还得再翻日志才知道是密钥错了还是渠道挂了。
	// 4xxx = 客户端侧业务校验失败（参数不合法 / 状态不允许 / 当前做不到）⇒ 400。
	// 不能落到最后那句 500：这类提示是写给用户照做的，报成服务端故障既误导又招重试。
	// 与 IsUpstreamFailure（5xxx 平台类 ⇒ 502）分工见 domain/errors/codes.go。
	if bizerr.IsClientError(err) {
		msg := fallbackMsg
		var be *bizerr.BizError
		if errors.As(err, &be) && be.Error() != "" {
			msg = be.Error()
		}
		Error(c, http.StatusBadRequest, msg, data...)
		return
	}
	if bizerr.IsUpstreamFailure(err) {
		msg := fallbackMsg
		var be *bizerr.BizError
		if errors.As(err, &be) && be.Error() != "" {
			msg = be.Error()
		}
		Error(c, http.StatusBadGateway, msg, data...)
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
	// 唯一键冲突（PostgreSQL 23505）：回 409，且**不回传 SQL 原文**。
	// 原文含表名/约束名（`duplicate key value violates unique constraint
	// "idx_ai_agents_agent_code"`），既是内部实现细节，也让前端只能看到英文 SQL。
	// 调用方给的中文 fallbackMsg（"创建失败"）已足够定位是哪个操作撞了唯一键。
	if strings.Contains(msg, "SQLSTATE 23505") || strings.Contains(msg, "duplicate key value violates unique constraint") {
		if fallbackMsg == "" {
			fallbackMsg = "数据重复，已存在相同的记录"
		}
		Error(c, http.StatusConflict, fallbackMsg)
		return
	}
	// 终态：未落入任何业务/上游/文本分派分支的错误 = 真正的未知 500。
	// 原始错误（可含 SQLSTATE/表名/约束名/驱动消息）只进服务端日志，对外回操作名。
	// 三个历史泄漏面在此一并封死（S-29，2026-10-31 审计）：
	//   - fallbackMsg 为空时曾把 err.Error() 顶出去；
	//   - 87 处调用点直接把 err.Error() 当 fallbackMsg 传入（== msg 判定一并拦住）；
	//   - data 通道在终态丢弃（部分调用点把 err.Error() 塞在 data 里）。
	// 400/404/409/502 分支的业务文案不受影响——那些分支的错误已有明确分类，
	// 文本本就是写给用户照做的。
	if fallbackMsg == "" || fallbackMsg == msg {
		logger.Errorf("ErrorFromDB 未知 500 已掩码（原文仅入日志）: %s", msg)
		fallbackMsg = "操作失败，请稍后重试"
	}
	data = nil // 终态一律不带 data：部分调用点把 err.Error() 塞在 data 里，无条件丢弃才封得死
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
	case http.StatusGone:
		// 410 = 曾经存在、永久移走（墓碑路由）。与 404 的修法不同：
		// 前者改调用方地址，后者查路由表，不能都叫"资源不存在"。
		return utils.ErrorCodeLegacyGone
	case http.StatusTooManyRequests:
		return utils.ErrorCodeInsufficientQuota
	case http.StatusInternalServerError:
		return utils.ErrorCodeInternalError
	case http.StatusServiceUnavailable:
		return utils.ErrorCodeServiceUnavailable
	case http.StatusBadGateway:
		// 502 是"上游/渠道失败"（bizerr 5xxx 平台类），不是未知错误：
		// 前端要能把它与 UNKNOWN_1000 分开（重试/换凭据 vs 报障）。
		return utils.ErrorCodeUpstreamFailure
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
		// 绑定错误只含字段名/tag，不含敏感值：把它带给调用方，才能知道"哪个字段不合格"，
		// 否则所有入参错误都只剩一句「无效的请求参数」（A20）。
		Error(c, http.StatusBadRequest, i18n.T(localeOf(c), "invalid_params")+": "+err.Error())
		return false
	}
	return true
}

// BindQuery 安全地绑定 query string 到 obj（同 BindJSON 的安全模式）。
func BindQuery(c *gin.Context, obj any) bool {
	if err := c.ShouldBindQuery(obj); err != nil {
		logger.Errorf("BindQuery failed on %s %s: %v", c.Request.Method, c.Request.URL.Path, err)
		Error(c, http.StatusBadRequest, i18n.T(localeOf(c), "invalid_params")+": "+err.Error())
		return false
	}
	return true
}
