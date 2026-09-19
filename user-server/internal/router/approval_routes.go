// approval_routes.go 审批 API 的路由挂载点（T-P3-04）。
//
// 与 human_task_routes.go 分两个文件，是因为两者的**关闸方式**不同：待办底座没有旗子
// （db==nil 就是它的关闸），而审批运行时受 FF_LTC_APPROVAL_RESUME 控制。
// 混在一个文件里，"off 档这两个端点该回什么"就会只剩一处注释来回答。
package router

import (
	"github.com/gin-gonic/gin"

	"hivemtk-user/internal/app"
	"hivemtk-user/internal/controller"
	"hivemtk-user/internal/pkg/utils/logger"
	"hivemtk-user/internal/service"
)

// setupApprovalRoutes 在**已鉴权**的 /api 组下挂审批端点（同 setupHumanTaskRoutes：
// gin 的 Use 只对手之后注册的路由生效，挂早了就是匿名可访问）。
//
// 服务实例取自全局登记处（装配点 app.InitApprovalRuntime）。取到 nil 不是错误：
// 路由照挂、每个请求回 503 —— 裁决口的"关闸"就是不给它底座，而不是让按钮能点。
func setupApprovalRoutes(auth *gin.RouterGroup) {
	svc := service.GlobalApprovalRequestService()
	controller.NewApprovalController(svc).RegisterRoutes(auth)
	if svc == nil {
		logger.Infof("[Router] approval 路由已挂载，但未装配审批运行时 ⇒ 全部端点回 503（检查 %s 与 app.InitApprovalRuntime 是否在路由之前跑过）",
			app.ApprovalResumeFlagEnv)
		return
	}
	logger.Infof("[Router] approval 审批裁决 API 已连通（底座可用=%v）", svc.Available())
}
