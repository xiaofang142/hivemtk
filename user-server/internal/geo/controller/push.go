package controller

import (
	"net/http"

	"hivemtk-user/internal/geo/service"
	"hivemtk-user/internal/pkg/utils/response"

	"github.com/gin-gonic/gin"
)

// PushController 蜘蛛推送 v2
type PushController struct {
	svc *service.PushService
}

func NewPushController(svc *service.PushService) *PushController {
	return &PushController{svc: svc}
}

// PushURLs POST /geo/push/urls
// body: {"urls": [...], "platforms": ["baidu","indexnow","google"]}
func (c *PushController) PushURLs(ctx *gin.Context) {
	var req struct {
		URLs      []string `json:"urls" binding:"required"`
		Platforms []string `json:"platforms"`
	}
	if !response.BindJSON(ctx, &req) {
		return
	}
	var err error
	var records any
	if len(req.Platforms) > 0 {
		records, err = c.svc.PushManual(ctx.Request.Context(), req.URLs, req.Platforms)
	} else {
		err = c.svc.PushURLs(ctx.Request.Context(), req.URLs)
	}
	if err != nil {
		response.Error(ctx, http.StatusInternalServerError, err.Error())
		return
	}
	response.Success(ctx, gin.H{"count": len(req.URLs), "records": records}, "ok")
}

// QuotaStatus GET /geo/push/quota
func (c *PushController) QuotaStatus(ctx *gin.Context) {
	status := c.svc.QuotaStatus()
	response.Success(ctx, status, "ok")
}

// GenerateSitemap GET /geo/push/sitemap
func (c *PushController) GenerateSitemap(ctx *gin.Context) {
	sitemap, err := c.svc.GenerateAndSubmitSitemap(ctx.Request.Context())
	if err != nil {
		response.Error(ctx, http.StatusInternalServerError, err.Error())
		return
	}
	response.Success(ctx, gin.H{"content": sitemap}, "ok")
}
