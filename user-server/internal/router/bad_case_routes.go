// bad_case_routes.go T-P8-03：G-2 Bad Case 闭环的路由挂载点。
//
// 单独一个文件的理由与 human_task_routes.go 同一条：让"这套读写口挂在哪个组下"
// 只有一处可查。评测集导出与打标如果被人另开一组路径，就会出现两条"这批样本判过了"
// 的口径，而这条链的下游是基线分数。
package router

import (
	"github.com/gin-gonic/gin"

	"hivemtk-user/internal/controller"
	"hivemtk-user/internal/pkg/utils/logger"
	"hivemtk-user/internal/service"
)

// setupBadCaseRoutes 在**已鉴权**的 /api 组下挂八条端点（同 setupHumanTaskRoutes 的位置约束：
// gin 的 Use 只对其后注册的路由生效，挂早了就是匿名可访问 —— 而打标是要追责的写操作）。
//
// 服务实例取自全局登记处（装配点 app.InitBadCaseRuntime，在 BuildSmartOrchestrator 之前）。
// 取到 nil 不是错误：路由照挂，每个请求回 503 —— 本卡的"关闸"就是不给它底座。
func setupBadCaseRoutes(auth *gin.RouterGroup) {
	svc := service.GlobalBadCaseService()
	controller.NewBadCaseController(svc).RegisterRoutes(auth)
	if svc == nil {
		logger.Infof("[Router] bad_case 路由已挂载，但未装配服务 ⇒ 全部端点回 503（检查 app.InitBadCaseRuntime 是否在路由之前跑过）")
		return
	}
	logger.Infof("[Router] bad_case 闭环 API 已连通（底座可用=%v）", svc.Available())
}
