package controller

import (
	"net/http"

	"hivemtk-user/internal/middleware"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/pkg/utils/response"
	"hivemtk-user/internal/system/install"

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
	// SaaS（tenant_mode=on）实例没有"单机未安装"概念：租户经 /register 自助
	// 开户，install.lock 只服务离线单机的 setup 向导。docker 直部的 SaaS
	// 实例从未跑过 setup，若按 lock 判定会永远 NOT_INSTALLED → 干净浏览器
	// 首访被路由守卫全部推进 /setup 而不是登录页（003 生产联调老问题）。
	// 权威口径改为：on 模式恒已初始化。
	if tenant.IsMultiTenant() {
		response.Success(ctx, gin.H{
			"state":       "INITIALIZED",
			"initialized": true,
			"has_admin":   true,
			"version":     "unknown",
		}, "ok")
		return
	}
	checker := middleware.GetInstallStatus()
	if checker == nil {
		response.Success(ctx, gin.H{
			"state":       "NOT_INSTALLED",
			"initialized": false,
			"has_admin":   false,
			"version":     "unknown",
		}, "安装态检查器未初始化")
		return
	}
	status := checker.GetInitStatus()
	response.Success(ctx, publicInitStatus(status), "ok")
}

// publicInitStatus 把安装态 DTO 收成"公开可读的那几个字段"。
//
// 这条路由挂在 public 组上（安装向导在还没有登录态时就要能问"装完了没有"），
// 所以凡是能由安装态推出的**身份类**字段都不该从这里出去：install_id 是这台实例
// 在安装平台侧的商户身份，冒用它可以伪造心跳与用量归属。进程内的心跳上报直接读
// install 包，前端也从不取这个字段（全仓 grep 无消费点），删掉不影响任何调用方。
// 判据留在这一层而不是 DTO 上：install.Status 本身还要给心跳带身份，字段不能少。
func publicInitStatus(s *install.Status) gin.H {
	if s == nil {
		return gin.H{
			"state":       "NOT_INSTALLED",
			"initialized": false,
			"has_admin":   false,
			"version":     "unknown",
		}
	}
	version := s.Version
	if version == "" {
		version = "unknown"
	}
	return gin.H{
		"state":       s.State,
		"initialized": s.Initialized,
		"has_admin":   s.HasAdmin,
		"version":     version,
	}
}

// InitAdminRequest 创建超管请求
// 开源版：手机号、邮箱、姓名均为选填（初始化时上报平台，作为商户联系信息）。
// InitAdminRequest 初始化向导的提交体。
//
// 开户登记必填集（003 T012 收敛）：CompanyName / RealName / ContactPhone /
// **Address** 四项缺一不可，Email / LicenseNo 选填（Address 顶替邮箱的必填位：
// 电话失联时地址是唯一剩下的定位线索）。必填判定在
// service.ValidateOfflineRegistration，不在 binding 层 —— gin binding 的
// 400 文案给不出「缺少：X、Y」的完整清单。
//
// CompanyName 可退化为「负责人所属组织」：个人自用也要能装，
// 但不允许**完全为空** —— 一个查不到人的经营主体在监管问询时毫无价值。
type InitAdminRequest struct {
	Username     string `json:"username" binding:"required,min=3,max=20"`
	Password     string `json:"password" binding:"required,min=8"`
	Email        string `json:"email" binding:"omitempty,email"`
	RealName     string `json:"real_name" binding:"required,min=2,max=32"`
	ContactPhone string `json:"contact_phone" binding:"required"`
	// CompanyName 组织/公司名称；为空时回落为「负责人所属组织」，仍不允许两者皆空
	CompanyName string `json:"company_name"`
	// LicenseNo 选填：法律上通常需要，但留空可事后在平台端补录
	LicenseNo string `json:"license_no"`
	// Address 开户登记必填（003 T012）
	Address string `json:"address"`
	// RegulatoryAck 用户已确认监管责任告知
	RegulatoryAck bool `json:"regulatory_ack"`
	// AgreementVersion 用户同意的协议版本（由 /api/public/config 或告知端点给出）
	AgreementVersion string `json:"agreement_version"`
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
	checker := middleware.GetInstallStatus()
	if checker == nil {
		response.Error(ctx, http.StatusServiceUnavailable, "安装态检查器未初始化")
		return
	}
	if !checker.HasInstallLockAdmin() {
		response.Error(ctx, http.StatusBadRequest, "请先创建超管")
		return
	}

	adminUsername := checker.GetAdminUsername()
	if err := checker.SetAdminInit(adminUsername); err != nil {
		response.ErrorFromDB(ctx, err, "标记初始化状态失败，请稍后重试")
		return
	}

	response.Success(ctx, gin.H{
		"message":        "系统已初始化完成，请使用超管账号登录",
		"initialized":    true,
		"admin_username": adminUsername,
		"next_action":    "login",
	}, "初始化完成")
}
