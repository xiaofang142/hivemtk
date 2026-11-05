// security_overview.go 安全总览端点（R39-K3 overview 腿的 HTTP 面）。
package controller

import (
	"net/http"

	"hivemtk-user/internal/pkg/utils/response"
	"hivemtk-user/internal/service"

	"github.com/gin-gonic/gin"
)

// SecurityOverviewController 安全总览控制器
type SecurityOverviewController struct {
	svc *service.SecurityOverviewService
}

// NewSecurityOverviewController 创建控制器
func NewSecurityOverviewController(svc *service.SecurityOverviewService) *SecurityOverviewController {
	return &SecurityOverviewController{svc: svc}
}

// Overview godoc
// @Summary      安全总览
// @Description  聚合登录事件(24h)/open 告警分布/最近审计成绩与弱密码结论/JWT 401 聚合(近7天)
// @Tags         Security
// @Produce      json
// @Success      200  {object}  service.SecurityOverview
// @Router       /api/security/overview [get]
func (c *SecurityOverviewController) Overview(ctx *gin.Context) {
	if c.svc == nil {
		response.Error(ctx, http.StatusServiceUnavailable, "安全总览服务未初始化")
		return
	}
	out, err := c.svc.Overview(ctx.Request.Context())
	if err != nil {
		response.ErrorFromDB(ctx, err, "聚合安全总览失败，请稍后重试")
		return
	}
	response.Success(ctx, out, "ok")
}
