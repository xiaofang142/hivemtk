package router

import (
	"hivemtk-user/internal/controller"
	"hivemtk-user/internal/service"

	"github.com/gin-gonic/gin"
)

// setupChannelRoutes 渠道域路由映射（仅映射 + 依赖装配，无内联闭包）。
// 服务装配沿用 router.Setup 既有的构造注入口径，租户上下文由 auth 组的
// TenantMiddleware 提供，缺租户在 Controller 层 fail-closed。
func setupChannelRoutes(auth *gin.RouterGroup) {
	channelCtl := controller.NewChannelController(service.NewChannelMasterService())
	accountCtl := controller.NewChannelAccountController(service.NewChannelAccountService())
	identifyCtl := controller.NewChannelIdentifyCodeController(service.NewChannelIdentifyService())
	// T054：共享对话上下文只读口（两智能体交叉引用展示数据源）。
	ctxCtl := controller.NewConversationContextController(service.NewConversationContextService())

	// 渠道主数据
	auth.GET("/channels", channelCtl.List)
	auth.GET("/channels/options", channelCtl.Options)
	auth.GET("/channels/:id", channelCtl.Get)
	auth.POST("/channels", channelCtl.Create)
	auth.PUT("/channels/:id", channelCtl.Update)
	auth.DELETE("/channels/:id", channelCtl.Delete)

	// 渠道账号（列表/下拉统一走 ListOptions）
	auth.GET("/channel-accounts", accountCtl.List)
	auth.GET("/channel-accounts/options", accountCtl.Options)
	auth.GET("/channel-accounts/:id", accountCtl.Get)
	auth.POST("/channel-accounts", accountCtl.Create)
	auth.PUT("/channel-accounts/:id", accountCtl.Update)
	auth.DELETE("/channel-accounts/:id", accountCtl.Delete)

	// 共享对话上下文 / 交叉引用（T054 前端展示）
	auth.GET("/conversation-context", ctxCtl.Get)

	// 识别码（T016 只双写，不改读路径）
	auth.GET("/identify-codes", identifyCtl.List)
	auth.GET("/identify-codes/:channel_id", identifyCtl.Get)
	auth.POST("/identify-codes/:channel_id/rotate", identifyCtl.Rotate)
}
