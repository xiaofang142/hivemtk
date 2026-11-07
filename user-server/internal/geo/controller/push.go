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

// @Summary      推送 URL 到各搜索引擎平台
// @Tags      push
// @Router    /api/geo/push/urls [post]
// PushURLs POST /geo/push/urls
// body: {"urls": [...], "platforms": ["baidu","indexnow","google"]}
func (c *PushController) PushURLs(ctx *gin.Context) {
	var req struct {
		// 单次最多 2000 条（与百度批量上限一致），避免一次请求把全站 URL 塞进来。
		URLs      []string `json:"urls" binding:"required,max=2000"`
		Platforms []string `json:"platforms"`
	}
	if !response.BindJSON(ctx, &req) {
		return
	}
	if len(req.Platforms) > 0 {
		if err := c.svc.ValidatePlatforms(ctx.Request.Context(), req.Platforms); err != nil {
			response.Error(ctx, http.StatusBadRequest, err.Error())
			return
		}
	}
	var err error
	var records any
	if len(req.Platforms) > 0 {
		records, err = c.svc.PushManual(ctx.Request.Context(), req.URLs, req.Platforms)
	} else {
		err = c.svc.PushURLs(ctx.Request.Context(), req.URLs)
	}
	if err != nil {
		respondGeoConfigError(ctx, err, "URL 推送")
		return
	}
	response.Success(ctx, gin.H{"count": len(req.URLs), "records": records}, "ok")
}

// @Summary      查询推送配额状态
// @Tags      push
// @Router    /api/geo/push/quota [get]
// QuotaStatus GET /geo/push/quota
func (c *PushController) QuotaStatus(ctx *gin.Context) {
	status := c.svc.QuotaStatus(ctx.Request.Context())
	response.Success(ctx, status, "ok")
}

// @Summary      生成并提交站点 sitemap
// @Tags      push
// @Router    /api/geo/push/sitemap [get]
// GenerateSitemap GET /geo/push/sitemap
func (c *PushController) GenerateSitemap(ctx *gin.Context) {
	sitemap, err := c.svc.GenerateAndSubmitSitemap(ctx.Request.Context())
	if err != nil {
		response.Error(ctx, http.StatusInternalServerError, err.Error())
		return
	}
	response.Success(ctx, gin.H{"content": sitemap}, "ok")
}
