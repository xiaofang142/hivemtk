package controller

import (
	"errors"
	"net/http"
	"strings"

	"hivemtk-user/internal/geo/repository"
	"hivemtk-user/internal/geo/service"
	"hivemtk-user/internal/pkg/utils/response"

	"github.com/gin-gonic/gin"
)

// SiteController 静态站部署 v2
type SiteController struct {
	deploySvc  *service.SiteDeployService
	pushSvc    *service.PushService
	configRepo repository.GeoConfigRepository
}

func NewSiteController(deploySvc *service.SiteDeployService, pushSvc *service.PushService, configRepo repository.GeoConfigRepository) *SiteController {
	return &SiteController{deploySvc: deploySvc, pushSvc: pushSvc, configRepo: configRepo}
}

// @Summary      导出站点为 Hugo 静态站
// @Tags      site
// @Router    /api/geo/site/export [post]
// ExportToHugo POST /geo/site/export
func (c *SiteController) ExportToHugo(ctx *gin.Context) {
	n, err := c.deploySvc.ExportToHugo(ctx.Request.Context())
	if err != nil {
		response.Error(ctx, http.StatusInternalServerError, err.Error())
		return
	}
	response.Success(ctx, gin.H{"exported": n}, "ok")
}

// @Summary      触发站点部署
// @Tags      site
// @Router    /api/geo/site/deploy [post]
// TriggerDeploy POST /geo/site/deploy
func (c *SiteController) TriggerDeploy(ctx *gin.Context) {
	url, err := c.deploySvc.TriggerDeploy(ctx.Request.Context())
	if err != nil {
		if errors.Is(err, service.ErrSiteConfigInvalid) {
			response.Error(ctx, http.StatusBadRequest, err.Error())
			return
		}
		response.Error(ctx, http.StatusInternalServerError, err.Error())
		return
	}
	response.Success(ctx, gin.H{"site_url": url}, "ok")
}

// @Summary      一键全链路：导出→部署→推送
// @Tags      site
// @Router    /api/geo/site/full-pipeline [post]
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
		if errors.Is(err, service.ErrSiteConfigInvalid) {
			response.Error(ctx, http.StatusBadRequest, err.Error())
			return
		}
		response.Error(ctx, http.StatusInternalServerError, "deploy: "+err.Error())
		return
	}
	response.Success(ctx, gin.H{"exported": n, "site_url": url}, "ok")
}

// @Summary      预览 llms.txt 内容
// @Tags      site
// @Router    /api/geo/site/llms-txt-preview [get]
// LlmsTxtPreview GET /geo/site/llms-txt-preview?domain=example.com
func (c *SiteController) LlmsTxtPreview(ctx *gin.Context) {
	domain := ctx.DefaultQuery("domain", "example.com")
	brand := "Brand"
	if cfg, err := c.configRepo.Get(ctx.Request.Context()); err == nil && cfg != nil {
		if b := strings.TrimSpace(cfg.BrandName); b != "" {
			brand = b
		}
	}
	response.Success(ctx, gin.H{"content": service.GenerateLLMsTxt(brand, domain, nil)}, "ok")
}

// @Summary      预览 robots.txt 内容
// @Tags      site
// @Router    /api/geo/site/robots-preview [get]
// RobotsTxtPreview GET /geo/site/robots-preview
func (c *SiteController) RobotsTxtPreview(ctx *gin.Context) {
	response.Success(ctx, gin.H{"content": service.GenerateRobots()}, "ok")
}
