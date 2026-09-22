// bill_routes.go T-P7-01：账单 HTTP 出口的挂载点。
//
// 单独一个文件（与 quote / opportunity / human_task 同一理由）：
// "这套端点挂在哪个鉴权组下"必须只有一处可查。塞进 router.go 的那一侧很快会出现第二个块，
// 到那时"开一张应收要不要鉴权"这个问题就有两个答案了 —— 而这里挂着的是能改变报价生命周期、
// 并在财务台账上长出一行应收的入口。
//
// 本文件只做 URL → Controller 的映射，不写任何响应（判据见 bill_routes_test.go 的
// TestBillRoutes_RouterFileHasNoInlineHandler，那条静态锁读的就是这里）。
package router

import (
	"github.com/gin-gonic/gin"

	"hivemtk-user/internal/controller"
	"hivemtk-user/internal/pkg/utils/logger"
	"hivemtk-user/internal/service"
)

// setupBillRoutes 在**已鉴权**的 /api 组下挂那一条端点。
//
// 位置约束与 setupQuoteRoutes 同一条：gin 的 Use 只对手之后注册的路由生效，
// 挂早了就是匿名可开应收。
//
// 服务实例取自全局登记处（装配点 app.InitBillRuntime，在路由之前）。取到 nil 不是错误：
// 路由照挂、端点回 503 —— "底座没装配"与"这套 API 不存在"是两件事，
// 前者要在网关日志里看得见，后者才是该 404 的。
func setupBillRoutes(auth *gin.RouterGroup) {
	derive := service.GlobalBillService()
	controller.NewBillController(derive).RegisterRoutes(auth)
	if derive == nil {
		logger.Infof("[Router] bill 路由已挂载，但派生腿未装配 ⇒ /api/bill 回 503" +
			"（检查 app.InitBillRuntime 是否在路由之前跑过）")
		return
	}
	logger.Infof("[Router] bill 账单 API 已连通（可派生=%v）", derive.Available())
}
