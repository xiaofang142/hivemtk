package controller

import (
	"strconv"

	"hivemtk-user/internal/geo/dto"
	"hivemtk-user/internal/geo/service"
	"hivemtk-user/internal/pkg/utils/response"

	"github.com/gin-gonic/gin"
)

// PlatformController GEO 平台同步发布控制器。
type PlatformController struct {
	svc *service.PlatformService
}

// NewPlatformController 构造平台同步发布控制器。
func NewPlatformController(svc *service.PlatformService) *PlatformController {
	return &PlatformController{svc: svc}
}

// @Summary 获取支持的平台列表
// @Tags      platform
// @Router    /api/geo/platform/platforms [get]
// ListPlatforms 获取支持的平台列表
// GET /geo/platform/platforms
func (c *PlatformController) ListPlatforms(ctx *gin.Context) {
	response.Success(ctx, c.svc.ListPlatforms(ctx.Request.Context()), "获取平台列表成功")
}

// @Summary      平台账号列表
// @Tags      platform
// @Router    /api/geo/platform/accounts [get]
// ListAccounts 平台账号列表
// GET /geo/platform/accounts?platform=&page=&limit=
func (c *PlatformController) ListAccounts(ctx *gin.Context) {
	page, _ := strconv.Atoi(ctx.DefaultQuery("page", "1"))
	limit, _ := strconv.Atoi(ctx.DefaultQuery("limit", "20"))
	list, total, err := c.svc.ListAccounts(ctx.Request.Context(), ctx.Query("platform"), page, limit)
	if err != nil {
		response.ErrorFromDB(ctx, err, "获取账号列表失败")
		return
	}
	response.SuccessWithPage(ctx, list, int64(page), int64(limit), total)
}

// @Summary 新增平台账号
// @Tags      platform
// @Router    /api/geo/platform/accounts [post]
// SaveAccount 新增平台账号
// POST /geo/platform/accounts
func (c *PlatformController) SaveAccount(ctx *gin.Context) {
	var req dto.SavePlatformAccountRequest
	if !response.BindJSON(ctx, &req) {
		return
	}
	account, err := c.svc.SaveAccount(ctx.Request.Context(), &req)
	if err != nil {
		response.BusinessError(ctx, err.Error())
		return
	}
	response.Success(ctx, account, "保存账号成功")
}

// @Summary 删除平台账号
// @Tags      platform
// @Router    /api/geo/platform/accounts/{id} [delete]
// DeleteAccount 删除平台账号
// DELETE /geo/platform/accounts/:id
func (c *PlatformController) DeleteAccount(ctx *gin.Context) {
	if err := c.svc.DeleteAccount(ctx.Request.Context(), ctx.Param("id")); err != nil {
		response.ErrorFromDB(ctx, err, "删除账号失败")
		return
	}
	response.Success(ctx, nil, "删除账号成功")
}

// @Summary      发布请求已处理
// @Tags      platform
// @Router    /api/geo/platform/publish [post]
// Publish 发布文章到平台
// POST /geo/platform/publish
func (c *PlatformController) Publish(ctx *gin.Context) {
	var req dto.PublishRequest
	if !response.BindJSON(ctx, &req) {
		return
	}
	result, err := c.svc.Publish(ctx.Request.Context(), &req)
	if err != nil {
		response.BusinessError(ctx, err.Error())
		return
	}
	response.Success(ctx, result, "发布请求已处理")
}

// @Summary      发布记录列表
// @Tags      platform
// @Router    /api/geo/platform/records [get]
// ListPublishRecords 发布记录列表
// GET /geo/platform/records?article_id=&platform=&page=&limit=
func (c *PlatformController) ListPublishRecords(ctx *gin.Context) {
	page, _ := strconv.Atoi(ctx.DefaultQuery("page", "1"))
	limit, _ := strconv.Atoi(ctx.DefaultQuery("limit", "20"))
	list, total, err := c.svc.ListPublishRecords(
		ctx.Request.Context(),
		ctx.Query("article_id"),
		ctx.Query("platform"),
		page, limit,
	)
	if err != nil {
		response.ErrorFromDB(ctx, err, "获取发布记录失败")
		return
	}
	response.SuccessWithPage(ctx, list, int64(page), int64(limit), total)
}
