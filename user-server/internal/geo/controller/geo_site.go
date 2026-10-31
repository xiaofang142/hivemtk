package controller

import (
	"errors"
	"net/http"
	"strconv"

	"hivemtk-user/internal/geo/service"
	"hivemtk-user/internal/pkg/utils/response"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// GeoSiteController 站点部署配置 CRUD
type GeoSiteController struct {
	svc *service.SiteConfigService
}

func NewGeoSiteController(svc *service.SiteConfigService) *GeoSiteController {
	return &GeoSiteController{svc: svc}
}

// geoSiteRequest 只含可编辑字段；id/tenant_id/health_score/last_deploy_at 由系统维护。
type geoSiteRequest struct {
	Domain    *string `json:"domain"`
	Provider  *string `json:"provider"`
	HugoPath  *string `json:"hugo_path"`
	GitRepo   *string `json:"git_repo"`
	DeployCmd *string `json:"deploy_cmd"`
	Active    *bool   `json:"active"`
}

func (r geoSiteRequest) toInput() service.SiteConfigInput {
	return service.SiteConfigInput{
		Domain:    r.Domain,
		Provider:  r.Provider,
		HugoPath:  r.HugoPath,
		GitRepo:   r.GitRepo,
		DeployCmd: r.DeployCmd,
		Active:    r.Active,
	}
}

// @Summary      查询 GEO 站点列表
// @Tags      geo-site
// @Router    /api/geo/sites [get]
func (c *GeoSiteController) List(ctx *gin.Context) {
	page, _ := strconv.Atoi(ctx.DefaultQuery("page", "1"))
	limit, _ := strconv.Atoi(ctx.DefaultQuery("limit", "20"))
	list, total, err := c.svc.List(ctx.Request.Context(), page, limit)
	if err != nil {
		response.Error(ctx, http.StatusInternalServerError, err.Error())
		return
	}
	response.Success(ctx, gin.H{"list": list, "total": total}, "ok")
}

// @Summary GET /api/geo/sites/{id}
// @Tags      geo-site
// @Router    /api/geo/sites/{id} [get]
func (c *GeoSiteController) Get(ctx *gin.Context) {
	item, err := c.svc.Get(ctx.Request.Context(), ctx.Param("id"))
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			response.Error(ctx, http.StatusNotFound, "站点配置不存在")
			return
		}
		response.Error(ctx, http.StatusInternalServerError, err.Error())
		return
	}
	response.Success(ctx, item, "ok")
}

// @Summary      创建 GEO 站点
// @Tags      geo-site
// @Router    /api/geo/sites [post]
func (c *GeoSiteController) Create(ctx *gin.Context) {
	var req geoSiteRequest
	if !response.BindJSON(ctx, &req) {
		return
	}
	item, err := c.svc.Create(ctx.Request.Context(), req.toInput())
	if err != nil {
		response.Error(ctx, http.StatusBadRequest, err.Error())
		return
	}
	response.Success(ctx, item, "created")
}

// @Summary PUT /api/geo/sites/{id}
// @Tags      geo-site
// @Router    /api/geo/sites/{id} [put]
func (c *GeoSiteController) Update(ctx *gin.Context) {
	var req geoSiteRequest
	if !response.BindJSON(ctx, &req) {
		return
	}
	item, err := c.svc.Update(ctx.Request.Context(), ctx.Param("id"), req.toInput())
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			response.Error(ctx, http.StatusNotFound, "站点配置不存在")
			return
		}
		response.Error(ctx, http.StatusBadRequest, err.Error())
		return
	}
	response.Success(ctx, item, "updated")
}

// @Summary DELETE /api/geo/sites/{id}
// @Tags      geo-site
// @Router    /api/geo/sites/{id} [delete]
func (c *GeoSiteController) Delete(ctx *gin.Context) {
	if err := c.svc.Delete(ctx.Request.Context(), ctx.Param("id")); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			response.Error(ctx, http.StatusNotFound, "站点配置不存在")
			return
		}
		response.Error(ctx, http.StatusInternalServerError, err.Error())
		return
	}
	response.Success(ctx, nil, "deleted")
}
