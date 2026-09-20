package utils

import "hivemtk-user/internal/pkg/i18n"

// ErrorCode 错误码类型
type ErrorCode string

const (
	ErrorCodeSuccess ErrorCode = "SUCCESS"

	ErrorCodeUnknown          ErrorCode = "UNKNOWN_1000"
	ErrorCodeInvalidParameter ErrorCode = "INVALID_PARAM_1001"
	ErrorCodeNotFound         ErrorCode = "NOT_FOUND_1002"
	ErrorCodeAlreadyExists    ErrorCode = "ALREADY_EXISTS_1003"
	ErrorCodeTimeout          ErrorCode = "TIMEOUT_1004"

	ErrorCodeUnauthorized     ErrorCode = "UNAUTHORIZED_2001"
	ErrorCodeForbidden        ErrorCode = "FORBIDDEN_2002"
	ErrorCodeTokenExpired     ErrorCode = "TOKEN_EXPIRED_2003"
	ErrorCodeTokenInvalid     ErrorCode = "TOKEN_INVALID_2004"
	ErrorCodeAPIKeyInvalid    ErrorCode = "API_KEY_INVALID_2005"
	ErrorCodeLicenseInvalid   ErrorCode = "LICENSE_INVALID_2006"
	ErrorCodePermissionDenied ErrorCode = "PERMISSION_DENIED_2007"

	ErrorCodeDatabaseError       ErrorCode = "DB_ERROR_3001"
	ErrorCodeRecordNotFound      ErrorCode = "RECORD_NOT_FOUND_3002"
	ErrorCodeDuplicateEntry      ErrorCode = "DUPLICATE_ENTRY_3003"
	ErrorCodeForeignKeyViolation ErrorCode = "FK_VIOLATION_3004"

	ErrorCodeValidation    ErrorCode = "VALIDATION_4001"
	ErrorCodeRequiredField ErrorCode = "REQUIRED_FIELD_4002"
	ErrorCodeInvalidFormat ErrorCode = "INVALID_FORMAT_4003"
	ErrorCodeInvalidRange  ErrorCode = "INVALID_RANGE_4004"

	ErrorCodeBusiness          ErrorCode = "BUSINESS_5001"
	ErrorCodeResourceLocked    ErrorCode = "RESOURCE_LOCKED_5002"
	ErrorCodeInsufficientQuota ErrorCode = "INSUFFICIENT_QUOTA_5003"
	ErrorCodeOperationFailed   ErrorCode = "OPERATION_FAILED_5004"

	ErrorCodeSystem             ErrorCode = "SYSTEM_6001"
	ErrorCodeInternalError      ErrorCode = "INTERNAL_ERROR_6002"
	ErrorCodeServiceUnavailable ErrorCode = "SERVICE_UNAVAILABLE_6003"

	ErrorCodeFileTooLarge    ErrorCode = "FILE_TOO_LARGE_7001"
	ErrorCodeInvalidFileType ErrorCode = "INVALID_FILE_TYPE_7002"
	ErrorCodeUploadFailed    ErrorCode = "UPLOAD_FAILED_7003"

	// 浏览器自动化域（批10）。两者 HTTP 同为 409，前端必须靠码分流：
	// 「Host 未连接」要开安装引导，「已有任务执行中」只要等/看监控——
	// 都按 409 处理会把忙的用户引去重装 Host（真机走 UI 时踩过这一条）。
	ErrorCodeBrowserHostOffline ErrorCode = "BROWSER_HOST_OFFLINE_8001"
	ErrorCodeBrowserTaskBusy    ErrorCode = "BROWSER_TASK_BUSY_8002"
	// 前置状态不满足（draft 没发布就执行、非执行中却暂停…）。同样落 409，但不能让
	// response.Error 按 HTTP 码折进 DUPLICATE_ENTRY_3003：那是「唯一键冲突」的意思，
	// 而这里是「你点早了」。今天前端 default 分支只看文案所以不出错，可码域一旦混用，
	// 任何按码分流的改动都会把误操作当成重复提交（同族两条 409 已经栽过一次）。
	ErrorCodeBrowserStateConflict ErrorCode = "BROWSER_STATE_CONFLICT_8003"

	// LTC 运营开关域（T-P3-06）。闸门拦截落 409，但绝不能走 response.Error(c, 409, …)：
	// 那会被 errorCodeFromHTTPCode 折成 DUPLICATE_ENTRY_3003（「重复的记录」），
	// 而闸门拦下时**一个键都没写、一条记录都没碰**——码在说一件根本没发生的事。
	// 三个码对应三种修法，合成一个就把三件事办成一件：
	//   - 没开：运营去 LTC 配置页点开关
	//   - 读坏了：点开关没用，得修存储/告警
	//   - 挂错阶段：路由写错了，是装配 bug，运营与运维都修不了
	ErrorCodeLTCStageDisabled  ErrorCode = "LTC_STAGE_DISABLED_8101"
	ErrorCodeLTCConfigDegraded ErrorCode = "LTC_CONFIG_DEGRADED_8102"
	ErrorCodeLTCStageUnknown   ErrorCode = "LTC_STAGE_UNKNOWN_8103"
)

// ErrorCodeConfig 错误码配置
type ErrorCodeConfig struct {
	Code     ErrorCode `json:"code"`
	HTTPCode int       `json:"http_code"`
	Message  string    `json:"message"`
}

var errorCodeRegistry = map[ErrorCode]ErrorCodeConfig{
	ErrorCodeSuccess: {Code: ErrorCodeSuccess, HTTPCode: 200, Message: "成功"},

	ErrorCodeUnknown:          {Code: ErrorCodeUnknown, HTTPCode: 500, Message: "未知错误"},
	ErrorCodeInvalidParameter: {Code: ErrorCodeInvalidParameter, HTTPCode: 400, Message: "参数无效"},
	ErrorCodeNotFound:         {Code: ErrorCodeNotFound, HTTPCode: 404, Message: "资源不存在"},
	ErrorCodeAlreadyExists:    {Code: ErrorCodeAlreadyExists, HTTPCode: 409, Message: "资源已存在"},
	ErrorCodeTimeout:          {Code: ErrorCodeTimeout, HTTPCode: 408, Message: "请求超时"},

	ErrorCodeUnauthorized:     {Code: ErrorCodeUnauthorized, HTTPCode: 401, Message: "未授权访问"},
	ErrorCodeForbidden:        {Code: ErrorCodeForbidden, HTTPCode: 403, Message: "拒绝访问"},
	ErrorCodeTokenExpired:     {Code: ErrorCodeTokenExpired, HTTPCode: 401, Message: "令牌已过期"},
	ErrorCodeTokenInvalid:     {Code: ErrorCodeTokenInvalid, HTTPCode: 401, Message: "令牌无效"},
	ErrorCodeAPIKeyInvalid:    {Code: ErrorCodeAPIKeyInvalid, HTTPCode: 401, Message: "API Key 无效"},
	ErrorCodeLicenseInvalid:   {Code: ErrorCodeLicenseInvalid, HTTPCode: 403, Message: "授权许可无效"},
	ErrorCodePermissionDenied: {Code: ErrorCodePermissionDenied, HTTPCode: 403, Message: "权限不足"},

	ErrorCodeDatabaseError:       {Code: ErrorCodeDatabaseError, HTTPCode: 500, Message: "数据库错误"},
	ErrorCodeRecordNotFound:      {Code: ErrorCodeRecordNotFound, HTTPCode: 404, Message: "记录不存在"},
	ErrorCodeDuplicateEntry:      {Code: ErrorCodeDuplicateEntry, HTTPCode: 409, Message: "重复的记录"},
	ErrorCodeForeignKeyViolation: {Code: ErrorCodeForeignKeyViolation, HTTPCode: 400, Message: "外键约束 violation"},

	ErrorCodeValidation:    {Code: ErrorCodeValidation, HTTPCode: 400, Message: "验证失败"},
	ErrorCodeRequiredField: {Code: ErrorCodeRequiredField, HTTPCode: 400, Message: "必填字段缺失"},
	ErrorCodeInvalidFormat: {Code: ErrorCodeInvalidFormat, HTTPCode: 400, Message: "格式无效"},
	ErrorCodeInvalidRange:  {Code: ErrorCodeInvalidRange, HTTPCode: 400, Message: "数值范围无效"},

	ErrorCodeBusiness:          {Code: ErrorCodeBusiness, HTTPCode: 400, Message: "业务错误"},
	ErrorCodeResourceLocked:    {Code: ErrorCodeResourceLocked, HTTPCode: 423, Message: "资源已锁定"},
	ErrorCodeInsufficientQuota: {Code: ErrorCodeInsufficientQuota, HTTPCode: 429, Message: "配额不足"},
	ErrorCodeOperationFailed:   {Code: ErrorCodeOperationFailed, HTTPCode: 500, Message: "操作失败"},

	ErrorCodeSystem:             {Code: ErrorCodeSystem, HTTPCode: 500, Message: "系统错误"},
	ErrorCodeInternalError:      {Code: ErrorCodeInternalError, HTTPCode: 500, Message: "内部错误"},
	ErrorCodeServiceUnavailable: {Code: ErrorCodeServiceUnavailable, HTTPCode: 503, Message: "服务不可用"},

	ErrorCodeFileTooLarge:    {Code: ErrorCodeFileTooLarge, HTTPCode: 413, Message: "文件过大"},
	ErrorCodeInvalidFileType: {Code: ErrorCodeInvalidFileType, HTTPCode: 415, Message: "文件类型不支持"},
	ErrorCodeUploadFailed:    {Code: ErrorCodeUploadFailed, HTTPCode: 500, Message: "上传失败"},

	ErrorCodeBrowserHostOffline:   {Code: ErrorCodeBrowserHostOffline, HTTPCode: 409, Message: "浏览器 Host 未连接"},
	ErrorCodeBrowserTaskBusy:      {Code: ErrorCodeBrowserTaskBusy, HTTPCode: 409, Message: "已有浏览器任务执行中"},
	ErrorCodeBrowserStateConflict: {Code: ErrorCodeBrowserStateConflict, HTTPCode: 409, Message: "任务状态不满足该操作"},

	ErrorCodeLTCStageDisabled:  {Code: ErrorCodeLTCStageDisabled, HTTPCode: 409, Message: "LTC 阶段未启用"},
	ErrorCodeLTCConfigDegraded: {Code: ErrorCodeLTCConfigDegraded, HTTPCode: 409, Message: "LTC 运营配置读不动，已按关闭处理"},
	ErrorCodeLTCStageUnknown:   {Code: ErrorCodeLTCStageUnknown, HTTPCode: 409, Message: "LTC 阶段名未注册"},
}

// GetErrorCodeConfig 获取错误码配置
func GetErrorCodeConfig(code ErrorCode) ErrorCodeConfig {
	if config, ok := errorCodeRegistry[code]; ok {
		return config
	}
	return errorCodeRegistry[ErrorCodeUnknown]
}

// GetHTTPCode 获取 HTTP 状态码
func GetHTTPCode(code ErrorCode) int {
	return GetErrorCodeConfig(code).HTTPCode
}

// LocalizedMessage 返回指定语言的错误消息（未命中时回退中文 Message）。
func (c ErrorCodeConfig) LocalizedMessage(loc i18n.Locale) string {
	return i18n.ErrorMessage(string(c.Code), loc)
}

// GetUserMessage 获取用户友好的错误消息（中文，向后兼容）
func GetUserMessage(code ErrorCode) string {
	return GetErrorCodeConfig(code).Message
}

// GetUserMessageLoc 获取指定语言的用户友好错误消息（多语言）
func GetUserMessageLoc(code ErrorCode, loc i18n.Locale) string {
	return GetErrorCodeConfig(code).LocalizedMessage(loc)
}
