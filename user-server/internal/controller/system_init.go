package controller

import (
	"net/http"

	"hivemtk-user/internal/middleware"
	"hivemtk-user/internal/pkg/utils/response"

	"github.com/gin-gonic/gin"
)

// SystemInitController 系统初始化控制器
//
// 开源版说明（hivemtk 已全面开源）：
//   - 不再有授权码（LicenseKey）/ 授权流程，移除原 init-license 步骤。
//   - 初始化流程：创建超管（POST /api/system/init-admin，由 AuthController 提供）
//     -> POST /api/system/init-complete 标记初始化完成。
//   - 不再强制"新账号首次登录改密"，初始化即完成。
//
// 提供 2 个公开端点：
//   - GET  /api/system/init-status   获取初始化状态
//   - POST /api/system/init-complete 完成初始化（写 install.lock.initialized=true）
//
// 注意：POST /api/system/init-admin 由 AuthController.InitAdmin 提供（见 admin_routes.go），
// 本控制器不再承担创建超管职责；原 SystemInitController.InitAdmin / reportInstall /
// deviceFingerprint 为路由切换后的死代码，已删除。
type SystemInitController struct{}

// NewSystemInitController 创建系统初始化控制器
func NewSystemInitController() *SystemInitController {
	return &SystemInitController{}
}

// GetInitStatus 公开：获取系统初始化状态
// @Summary 获取系统初始化状态
// @Description 返回系统状态机（NOT_INSTALLED/HAS_ADMIN/INITIALIZED）
// @Tags 系统初始化
// @Produce json
// @Success 200 {object} map[string]interface{} "系统初始化状态"
// @Router /api/system/init-status [get]
func (c *SystemInitController) GetInitStatus(ctx *gin.Context) {
	checker := middleware.GetLicenseChecker()
	if checker == nil {
		response.Success(ctx, gin.H{
			"state":       "NOT_INSTALLED",
			"initialized": false,
			"has_admin":   false,
			"version":     "unknown",
		}, "授权检查器未初始化")
		return
	}
	status := checker.GetInitStatus()
	response.Success(ctx, status, "ok")
}

// InitAdminRequest 创建超管请求
// 开源版：手机号、邮箱、姓名均为选填（初始化时上报平台，作为商户联系信息）。
type InitAdminRequest struct {
	Username     string `json:"username" binding:"required,min=3,max=20"`
	Password     string `json:"password" binding:"required,min=8"`
	Email        string `json:"email" binding:"omitempty,email"`
	RealName     string `json:"real_name"`
	ContactPhone string `json:"contact_phone"`
}

// InitComplete 公开：完成初始化向导
// @Summary 完成系统初始化向导
// @Description 标记初始化向导完成（写 install.lock.initialized=true，使 state 从 HAS_ADMIN 推进到 INITIALIZED）
// @Tags 系统初始化
// @Produce json
// @Success 200 {object} object{message=string}
// @Failure 400 {object} object{message=string}
// @Router /api/system/init-complete [post]
func (c *SystemInitController) InitComplete(ctx *gin.Context) {
	checker := middleware.GetLicenseChecker()
	if checker == nil {
		response.Error(ctx, http.StatusServiceUnavailable, "授权检查器未初始化")
		return
	}
	if !checker.HasInstallLockAdmin() {
		response.Error(ctx, http.StatusBadRequest, "请先创建超管")
		return
	}

	adminUsername := checker.GetAdminUsername()
	if err := checker.SetAdminInit(adminUsername); err != nil {
		response.ErrorFromDB(ctx, err, "标记初始化状态失败: "+err.Error())
		return
	}

	response.Success(ctx, gin.H{
		"message":        "系统已初始化完成，请使用超管账号登录",
		"initialized":    true,
		"admin_username": adminUsername,
		"next_action":    "login",
	}, "初始化完成")
}
