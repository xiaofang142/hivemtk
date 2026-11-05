// Package controller - 智能路由 & 技能匹配
package controller

import (
	"hivemtk-user/internal/pkg/utils/response"
	"hivemtk-user/internal/service"

	"github.com/gin-gonic/gin"
)

// SmartRouterController 智能路由 API
type SmartRouterController struct {
	svc *service.SmartRouter
}

// NewSmartRouterController 创建实例（service 由装配层注入，控制器不碰 DB）
func NewSmartRouterController(svc *service.SmartRouter) *SmartRouterController {
	return &SmartRouterController{svc: svc}
}

// @Summary      无可用坐席，请降级到 round-robin
// @Tags      smart-router
// @Router    /api/smart-router/select [post]
// SelectAgent POST /api/smart-router/select
// 请求体: {"intent": "refund", "skills_needed": ["refund", "vip"]}
func (c *SmartRouterController) SelectAgent(ctx *gin.Context) {
	var req service.SmartRouteRequest
	if !response.BindJSON(ctx, &req) {
		return
	}
	result, err := c.svc.SelectAgent(ctx.Request.Context(), &req)
	if err != nil {
		response.ErrorFromDB(ctx, err, "操作失败，请稍后重试")
		return
	}
	if result == nil {

		response.Success(ctx, gin.H{"agent": nil, "message": "无可用坐席，请降级到 round-robin"}, "ok")
		return
	}
	response.Success(ctx, result, "路由完成")
}
