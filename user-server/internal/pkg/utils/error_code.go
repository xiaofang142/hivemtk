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

	// LTC 运营开关域。闸门拦截落 409，但绝不能走 response.Error(c, 409, …)：
	// 那会被 errorCodeFromHTTPCode 折成 DUPLICATE_ENTRY_3003（「重复的记录」），
	// 而闸门拦下时**一个键都没写、一条记录都没碰**——码在说一件根本没发生的事。
	// 三个码对应三种修法，合成一个就把三件事办成一件：
	//   - 没开：运营去 LTC 配置页点开关
	//   - 读坏了：点开关没用，得修存储/告警
	//   - 挂错阶段：路由写错了，是装配 bug，运营与运维都修不了
	ErrorCodeLTCStageDisabled  ErrorCode = "LTC_STAGE_DISABLED_8101"
	ErrorCodeLTCConfigDegraded ErrorCode = "LTC_CONFIG_DEGRADED_8102"
	ErrorCodeLTCStageUnknown   ErrorCode = "LTC_STAGE_UNKNOWN_8103"

	// 消息撤回域。撤回失败落 409 的三条原因彼此修法不同，此前一律走
	// response.Error(c, 409, …) 被折成 DUPLICATE_ENTRY_3003（「重复的记录」）——
	// 可撤回一条消息既没写重复键，也和"重复"无关，前端按码分流时会被带偏：
	//   - 渠道没有撤回通道：去平台后台手工删（≠ 再点一次）
	//   - 已过平台窗口：只能人工处理，重试无用
	//   - 端口未装配：运维/装配问题（这一条走 503，不在此列）
	ErrorCodeMessageRecallUnsupported   ErrorCode = "MESSAGE_RECALL_UNSUPPORTED_8201"
	ErrorCodeMessageRecallWindowExpired ErrorCode = "MESSAGE_RECALL_WINDOW_EXPIRED_8202"

	// 状态机冲突域。409 此前一律折成 DUPLICATE_ENTRY_3003（「重复的记录」），
	// 但"这条已经在跑/已经被确认/已经归档"既不是重复键也不是重复提交：
	//   - WORKFLOW：执行已结束，再 stop 一次没有意义（刷新即可）
	//   - ORDER_DRAFT：草稿已被别人确认/取消/过期（刷新列表即可）
	ErrorCodeWorkflowStateConflict   ErrorCode = "WORKFLOW_STATE_CONFLICT_8301"
	ErrorCodeOrderDraftStateConflict ErrorCode = "ORDER_DRAFT_STATE_CONFLICT_8302"

	// WhatsApp 个人版扫码登录已随 gowa 下线（W-3）。410 而不是 500：
	// "曾经有、永久移走"要让老前端去换 Cloud API，而不是以为服务挂了在重试。
	ErrorCodeWhatsappLoginRetired ErrorCode = "WHATSAPP_LOGIN_RETIRED_8401"

	// 上游/渠道失败（502）。此前 502 没有专属码，按 HTTP 反查落到 UNKNOWN_1000，
	// 前端无法把"渠道挂了/凭据错"与"未知错误"分开。
	ErrorCodeUpstreamFailure ErrorCode = "UPSTREAM_FAILURE_5005"

	// 终态下线域（N-5）：已下线端点的 410 墓碑响应。此前 response.Error(c, 410, …)
	// 走 int 反向映射落 UNKNOWN_1000（"未知错误"）——老客户端拿到的码在说一件
	// 没发生过的事，监控/按码分流都无法把"地址该换了"从"服务器坏了"里分出来。
	// 码名带 LEGACY 前缀而不是复用 NOT_FOUND_1002：410 是"曾经存在、永久移走"，
	// 与"从未存在"的修法不同（前者改调用方地址，后者查路由表）。
	ErrorCodeLegacyGone ErrorCode = "LEGACY_GONE_4100"
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

	ErrorCodeMessageRecallUnsupported:   {Code: ErrorCodeMessageRecallUnsupported, HTTPCode: 409, Message: "该渠道不支持主动撤回"},
	ErrorCodeMessageRecallWindowExpired: {Code: ErrorCodeMessageRecallWindowExpired, HTTPCode: 409, Message: "已超过平台撤回窗口"},

	ErrorCodeWorkflowStateConflict:   {Code: ErrorCodeWorkflowStateConflict, HTTPCode: 409, Message: "工作流状态不满足该操作"},
	ErrorCodeOrderDraftStateConflict: {Code: ErrorCodeOrderDraftStateConflict, HTTPCode: 409, Message: "订单草稿状态不满足该操作"},
	ErrorCodeWhatsappLoginRetired:    {Code: ErrorCodeWhatsappLoginRetired, HTTPCode: 410, Message: "WhatsApp 个人版扫码登录已下线"},
	ErrorCodeUpstreamFailure:         {Code: ErrorCodeUpstreamFailure, HTTPCode: 502, Message: "上游渠道调用失败"},

	ErrorCodeLegacyGone: {Code: ErrorCodeLegacyGone, HTTPCode: 410, Message: "该接口已废弃"},
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
