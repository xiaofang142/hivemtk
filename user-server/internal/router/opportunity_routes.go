// opportunity_routes.go T-P4-04：商机 HTTP 出口的挂载点。
//
// 单独一个文件（而不是塞进 router.go）与 human_task/order_draft 同一理由：
// "这套端点挂在哪个鉴权组下"必须只有一处可查。塞进 router.go 的那一侧很快会出现
// 第二个块 —— 到那时"商机写入口要不要鉴权"这个问题就有两个答案了。
//
// 本文件只做 URL → Controller 的映射，不写任何响应（判据见 opportunity_routes_test.go
// 的 TestOpportunityRoutes_RouterFileHasNoInlineHandler：那条静态锁读的就是这里）。
package router

import (
	"github.com/gin-gonic/gin"

	"hivemtk-user/internal/controller"
	"hivemtk-user/internal/pkg/utils/logger"
	"hivemtk-user/internal/service"
)

// setupOpportunityRoutes 在**已鉴权**的 /api 组下挂八条端点。
//
// 位置约束与 setupHumanTaskRoutes 同一条：gin 的 Use 只对手之后注册的路由生效，
// 挂早了就是匿名可访问 —— 而这里挂着的是能改写金额与状态机的入口。
//
// 服务实例取自全局登记处（装配点 app.InitOpportunityRuntime，在路由之前）。
// 取到 nil 不是错误，也不在这里拦挂载：路由照挂，五个要读库的端点各回 503，
// /rules 照答（机器规则与 DB 句柄无关）。"底座没装配"与"这套 API 不存在"是两件事，
// 前者要在网关日志里看得见，后者才是该 404 的。
func setupOpportunityRoutes(auth *gin.RouterGroup) {
	svc := service.GlobalOpportunityService()
	controller.NewOpportunityController(svc).RegisterRoutes(auth)
	if svc == nil {
		logger.Infof("[Router] opportunity 路由已挂载，但未装配服务 ⇒ 读口与写口全部回 503（检查 app.InitOpportunityRuntime 是否在路由之前跑过）")
		return
	}
	logger.Infof("[Router] opportunity 商机 API 已连通（底座可用=%v）", svc.Available())
}
