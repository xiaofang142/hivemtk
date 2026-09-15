package controller

import (
	"net/http"

	"hivemtk-user/internal/geo/service"
	"hivemtk-user/internal/pkg/utils/response"

	"github.com/gin-gonic/gin"
)

// IndexTrackerController 收录追踪 + AI 引用 v2
type IndexTrackerController struct {
	svc *service.IndexTrackerService
}

func NewIndexTrackerController(svc *service.IndexTrackerService) *IndexTrackerController {
	return &IndexTrackerController{svc: svc}
}

// VerifyFull POST /geo/index-tracking/verify/:article_id
func (c *IndexTrackerController) VerifyFull(ctx *gin.Context) {
	articleID := ctx.Param("article_id")
	standing, err := c.svc.VerifyArticleFull(ctx.Request.Context(), articleID)
	if err != nil {
		response.Error(ctx, http.StatusInternalServerError, err.Error())
		return
	}
	response.Success(ctx, standing, "ok")
}

// FunnelStats GET /geo/index-tracking/funnel
func (c *IndexTrackerController) FunnelStats(ctx *gin.Context) {
	fs, err := c.svc.FunnelStats(ctx.Request.Context())
	if err != nil {
		response.Error(ctx, http.StatusInternalServerError, err.Error())
		return
	}
	response.Success(ctx, fs, "ok")
}

// ManualVerifyAll POST /geo/index-tracking/verify-all（触发 cron）
func (c *IndexTrackerController) ManualVerifyAll(ctx *gin.Context) {
	err := c.svc.AutoVerifyCron(ctx.Request.Context())
	if err != nil {
		response.Error(ctx, http.StatusInternalServerError, err.Error())
		return
	}
	response.Success(ctx, gin.H{"started": true}, "ok")
}
