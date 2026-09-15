package controller

import (
	"net/http"

	"hivemtk-user/internal/geo/service"
	"hivemtk-user/internal/pkg/utils/response"

	"github.com/gin-gonic/gin"
)

// SiteController 静态站部署 v2
type SiteController struct {
	deploySvc *service.SiteDeployService
	pushSvc   *service.PushService
}

func NewSiteController(deploySvc *service.SiteDeployService, pushSvc *service.PushService) *SiteController {
	return &SiteController{deploySvc: deploySvc, pushSvc: pushSvc}
}

// ExportToHugo POST /geo/site/export
func (c *SiteController) ExportToHugo(ctx *gin.Context) {
	n, err := c.deploySvc.ExportToHugo(ctx.Request.Context())
	if err != nil {
		response.Error(ctx, http.StatusInternalServerError, err.Error())
		return
	}
	response.Success(ctx, gin.H{"exported": n}, "ok")
}

// TriggerDeploy POST /geo/site/deploy
func (c *SiteController) TriggerDeploy(ctx *gin.Context) {
	url, err := c.deploySvc.TriggerDeploy(ctx.Request.Context())
	if err != nil {
		response.Error(ctx, http.StatusInternalServerError, err.Error())
		return
	}
	response.Success(ctx, gin.H{"site_url": url}, "ok")
}

// FullPipeline POST /geo/site/full-pipeline
// Export → Deploy → Push 一键全链路
func (c *SiteController) FullPipeline(ctx *gin.Context) {
	n, err := c.deploySvc.ExportToHugo(ctx.Request.Context())
	if err != nil {
		response.Error(ctx, http.StatusInternalServerError, "export: "+err.Error())
		return
	}
	url, err := c.deploySvc.TriggerDeploy(ctx.Request.Context())
	if err != nil {
		response.Error(ctx, http.StatusInternalServerError, "deploy: "+err.Error())
		return
	}
	response.Success(ctx, gin.H{"exported": n, "site_url": url}, "ok")
}

// LlmsTxtPreview GET /geo/site/llms-txt-preview
func (c *SiteController) LlmsTxtPreview(ctx *gin.Context) {
	ctx.Header("Content-Type", "text/markdown")
	ctx.String(http.StatusOK, service.GenerateLLMsTxt("Brand", "example.com", nil))
}

// RobotsTxtPreview GET /geo/site/robots-preview
func (c *SiteController) RobotsTxtPreview(ctx *gin.Context) {
	ctx.Header("Content-Type", "text/plain")
	ctx.String(http.StatusOK, service.GenerateRobots())
}
