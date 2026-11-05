package controller

import (
	"net/http"
	"strconv"
	"time"

	"hivemtk-user/internal/pkg/utils/response"
	"hivemtk-user/internal/service"

	"github.com/gin-gonic/gin"
)

// RagHealthController RAG 健康度控制器
type RagHealthController struct {
	svc *service.RagHealthService
}

// NewRagHealthController 创建控制器
func NewRagHealthController(svc *service.RagHealthService) *RagHealthController {
	return &RagHealthController{svc: svc}
}

// GetHealth godoc
// @Summary      RAG 系统健康度评分
// @Description  30 秒内复用缓存；窗口可通过 window_seconds 自定义
// @Tags         RAG Health
// @Produce      json
// @Param        window_seconds  query  int  false  "评估窗口（秒），默认 3600"
// @Success      200  {object}  service.RagHealthReport
// @Router       /api/rag/health [get]
func (c *RagHealthController) GetHealth(ctx *gin.Context) {
	if c.svc == nil {
		response.Error(ctx, http.StatusServiceUnavailable, "RAG 健康度服务未初始化")
		return
	}
	windowSec, _ := strconv.Atoi(ctx.DefaultQuery("window_seconds", "0"))
	window := time.Duration(windowSec) * time.Second
	report, err := c.svc.GetHealthCached(ctx.Request.Context(), window)
	if err != nil {
		response.ErrorFromDB(ctx, err, "健康度评估失败，请稍后重试")
		return
	}
	response.Success(ctx, report, "ok")
}

// RefreshHealth godoc
// @Summary      强制刷新健康度评分
// @Description  不使用 30 秒缓存，立即重新计算
// @Tags         RAG Health
// @Produce      json
// @Param        window_seconds  query  int  false  "评估窗口（秒），默认 3600"
// @Success      200  {object}  service.RagHealthReport
// @Router       /api/rag/health/refresh [get]
func (c *RagHealthController) RefreshHealth(ctx *gin.Context) {
	if c.svc == nil {
		response.Error(ctx, http.StatusServiceUnavailable, "RAG 健康度服务未初始化")
		return
	}
	windowSec, _ := strconv.Atoi(ctx.DefaultQuery("window_seconds", "0"))
	window := time.Duration(windowSec) * time.Second
	report, err := c.svc.GetHealth(ctx.Request.Context(), window)
	if err != nil {
		response.ErrorFromDB(ctx, err, "健康度评估失败，请稍后重试")
		return
	}
	response.Success(ctx, report, "ok")
}

// RegisterRoutes 注册路由
func (c *RagHealthController) RegisterRoutes(auth *gin.RouterGroup) {
	group := auth.Group("/rag/health")
	{
		group.GET("", c.GetHealth)
		group.GET("/refresh", c.RefreshHealth)
	}
}
