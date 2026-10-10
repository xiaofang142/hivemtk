package response

// 错误消息常量
// 用于统一 API 响应中的错误消息文本
const (
	ErrSuccess          = "操作成功"
	ErrInvalidParams    = "无效的请求参数"
	ErrInvalidIDFormat  = "无效的 ID 格式"
	ErrIDMismatch       = "ID 不一致"
	ErrResourceNotFound = "资源不存在"

	ErrCreateFailed  = "创建失败"
	ErrCreateSuccess = "创建成功"

	ErrUpdateFailed  = "更新失败"
	ErrUpdateSuccess = "更新成功"

	ErrDeleteFailed  = "删除失败"
	ErrDeleteSuccess = "删除成功"

	ErrGetFailed     = "获取失败"
	ErrGetSuccess    = "获取成功"
	ErrGetListFailed = "获取列表失败"

	ErrUnauthorized     = "未授权访问"
	ErrPermissionDenied = "权限不足"

	ErrBusinessError = "业务处理失败"
)

// 卡片管理相关错误消息

// 短链管理相关错误消息

// 用户管理相关错误消息

// 系统配置相关错误消息
const (
	ErrSystemAlreadyInitialized = "系统已初始化，禁止重复创建超管"
)
