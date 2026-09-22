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

// setupBillRoutes 在**已鉴权**的 /api 组下挂那三条端点（一条写、两条读）。
//
// 位置约束与 setupQuoteRoutes 同一条：gin 的 Use 只对手之后注册的路由生效，
// 挂早了就是匿名可开应收、匿名可读别人的账。
//
// 两把服务实例各自取自全局登记处（装配点 app.InitBillRuntime 与 app.InitPaymentRuntime，
// 都在路由之前）。取到 nil 不是错误：路由照挂、对应那一条腿的端点回 503 ——
// "底座没装配"与"这套 API 不存在"是两件事，前者要在网关日志里看得见，后者才是该 404 的。
//
// 为什么两条腿分开读、又一起挂：它们由两个装配点装，可以一有一无（只装派生 = 开得出单、
// 读不到已收）。如果合成一次判断（derive==nil 就不挂读口），症状会变成
// "GET 404 而 POST 503"——同一份缺件在网关日志里留下两种答案，而 404 那句是假的。
func setupBillRoutes(auth *gin.RouterGroup) {
	derive := service.GlobalBillService()
	read := service.GlobalPaymentService()
	controller.NewBillController(derive, read).RegisterRoutes(auth)
	if derive == nil || read == nil {
		var missing []string
		if derive == nil {
			missing = append(missing, "派生腿 app.InitBillRuntime")
		}
		if read == nil {
			missing = append(missing, "对账读腿 app.InitPaymentRuntime")
		}
		logger.Infof("[Router] bill 路由已挂载，但缺 %v ⇒ 对应端点回 503（检查装配点是否在路由之前跑过）", missing)
		return
	}
	logger.Infof("[Router] bill 账单 API 已连通（可派生=%v，可对账=%v）", derive.Available(), read.Available())
}
