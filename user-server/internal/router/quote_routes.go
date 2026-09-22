// quote_routes.go T-P6-03：报价 HTTP 出口的挂载点。
//
// 单独一个文件（与 opportunity / human_task / order_draft 同一理由）：
// "这套端点挂在哪个鉴权组下"必须只有一处可查。塞进 router.go 的那一侧很快会出现第二个块，
// 到那时"发送要不要鉴权"这个问题就有两个答案了 —— 而这里挂着的是能把一版报价送到
// 客户手上的入口。
//
// 本文件只做 URL → Controller 的映射，不写任何响应（判据见 quote_routes_test.go 的
// TestQuoteRoutes_RouterFileHasNoInlineHandler，那条静态锁读的就是这里）。
package router

import (
	"github.com/gin-gonic/gin"

	"hivemtk-user/internal/controller"
	"hivemtk-user/internal/pkg/utils/logger"
	"hivemtk-user/internal/service"
)

// setupQuoteRoutes 在**已鉴权**的 /api 组下挂四条端点。
//
// 位置约束与 setupOpportunityRoutes 同一条：gin 的 Use 只对手之后注册的路由生效，
// 挂早了就是匿名可访问 —— 而发送那一条对外发的是客户。
//
// 两个服务实例取自全局登记处（装配点 app.InitQuoteRuntime，在路由之前）。取到 nil 不是错误：
// 路由照挂，四条端点各回 503 —— "底座没装配"与"这套 API 不存在"是两件事，
// 前者要在网关日志里看得见，后者才是该 404 的。
//
// 生成腿与发送腿是**两个**全局实例而不是一个：它们的缺件面不同（发送腿多七个句柄），
// 而 503 之外还要能回答"能不能读、能不能发"这两件不同的事。
func setupQuoteRoutes(auth *gin.RouterGroup) {
	reads := service.GlobalQuoteService()
	sends := service.GlobalQuoteSendService()
	controller.NewQuoteController(reads, sends).RegisterRoutes(auth)
	if reads == nil || sends == nil {
		logger.Infof("[Router] quote 路由已挂载，但两条腿未全部装配（生成=%v 发送=%v）⇒ /api/quote/* 全部回 503"+
			"（检查 app.InitQuoteRuntime 是否在路由之前跑过）", reads != nil, sends != nil)
		return
	}
	logger.Infof("[Router] quote 报价 API 已连通（可生成=%v 可发送=%v）", reads.Available(), sends.Available())
}
