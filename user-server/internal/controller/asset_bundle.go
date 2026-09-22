// Package controller 提供 AssetBundle（资产包）的 HTTP 路由。
//
// 方向9：资产包模式
// 文档依据：docs/企业级架构优化/资产包模式.md
//
// 设计原则：
//  1. 严格按五层架构：Controller 仅做 HTTP 适配，业务放 Service
//  2. 入参绑定 DTO，出参 JSON
//  3. 不直接持有 db / repository（全部由 service 屏蔽）
package controller

import (
	"net/http"
	"strconv"
	"strings"

	"hivemtk-user/internal/middleware"

	"github.com/gin-gonic/gin"

	"hivemtk-user/internal/dto"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/utils/logger"
	"hivemtk-user/internal/pkg/utils/response"
	"hivemtk-user/internal/service"
)

// AssetBundleController 资产包 HTTP 控制器
type AssetBundleController struct {
	svc *service.AssetBundleService
}

// NewAssetBundleController 构造控制器
func NewAssetBundleController(svc *service.AssetBundleService) *AssetBundleController {
	return &AssetBundleController{svc: svc}
}

// Register 注册路由
//
// 路由：
//
//	POST   /api/asset-bundle              创建
//	PUT    /api/asset-bundle/:id          更新
//	GET    /api/asset-bundle/:id          查询
//	GET    /api/asset-bundle/by-aid/:aid  按 AssetID 查询
//	POST   /api/asset-bundle/list         分页
//	POST   /api/asset-bundle/:id/publish  启用
//	POST   /api/asset-bundle/:id/archive  归档
//	DELETE /api/asset-bundle/:id          软删
//	POST   /api/asset-bundle/weave        Weave 织布
//	POST   /api/asset-bundle/merchant-save 商户表单保存
//	POST   /api/asset-bundle/merchant-parse/:aid 商户表单解析
//	POST   /api/asset-bundle/:id/enable   热启用（方向 D1，立即生效）
//	POST   /api/asset-bundle/:id/disable  热禁用（方向 D1，立即生效）
//	POST   /api/asset-bundle/enabled/list 查询已热启用的资产包列表
func (c *AssetBundleController) Register(rg *gin.RouterGroup) {
	g := rg.Group("/asset-bundle")

	g.GET("/:id", c.Get)
	g.GET("/by-aid/:aid", c.GetByAssetID)
	g.POST("/list", c.List)
	g.POST("/enabled/list", c.GetEnabled)

	admin := rg.Group("/asset-bundle", middleware.AdminAuthMiddleware())
	{
		admin.POST("", c.Create)
		admin.PUT("/:id", c.Update)
		admin.DELETE("/:id", c.Delete)
		admin.POST("/:id/publish", c.Publish)
		admin.POST("/:id/archive", c.Archive)
		admin.POST("/:id/enable", c.Enable)
		admin.POST("/:id/disable", c.Disable)
		admin.POST("/:id/submit-platform", c.SubmitToPlatform)
		admin.POST("/weave", c.Weave)
		admin.POST("/merchant-save", c.MerchantSave)
		admin.POST("/merchant-parse/:aid", c.MerchantParse)
	}
}

// SubmitToPlatform 将本地资产包提交平台审核上架（开发者上架链路）
func (c *AssetBundleController) SubmitToPlatform(ctx *gin.Context) {
	if rejectPlatformDisabled(ctx) {
		return
	}

	assetID := ctx.Param("id")
	if assetID == "" {
		response.Error(ctx, http.StatusBadRequest, "invalid asset id")
		return
	}
	platformAssetID, err := c.svc.SubmitToPlatform(ctx.Request.Context(), assetID)
	if err != nil {
		logger.Errorf("[asset-bundle] submit to platform failed: %v", err)
		response.ErrorFromDB(ctx, err, "提交平台失败: "+err.Error())
		return
	}
	response.Success(ctx, gin.H{"platform_asset_id": platformAssetID}, "已提交平台审核")
}

// Create 创建
func (c *AssetBundleController) Create(ctx *gin.Context) {
	var req dto.AssetBundleCreateRequest
	if !response.BindJSON(ctx, &req) {
		return
	}
	bundle := &model.AssetBundle{
		AssetID:     req.AssetID,
		Title:       req.Title,
		Description: req.Description,
		Author:      req.Author,
		Version:     req.Version,
		Scope:       model.AssetBundleScope(req.Scope),
		Industry:    req.Industry,
		Language:    req.Language,
		Tags:        req.Tags,
		Messages:    service.AssetBundleMessagesFromDTO(req.Messages),
	}
	if err := c.svc.CreateBundle(ctx.Request.Context(), bundle); err != nil {
		logger.Errorf("[asset-bundle] create failed: %v", err)
		response.ErrorFromDB(ctx, err, err.Error())
		return
	}
	response.Success(ctx, bundle, "ok")
}

// Update 更新
func (c *AssetBundleController) Update(ctx *gin.Context) {
	id, err := strconv.ParseInt(ctx.Param("id"), 10, 64)
	if err != nil {
		response.Error(ctx, http.StatusBadRequest, "invalid id")
		return
	}
	var req dto.AssetBundleUpdateRequest
	if !response.BindJSON(ctx, &req) {
		return
	}
	req.ID = id
	bundle := &model.AssetBundle{
		ID:          req.ID,
		Title:       req.Title,
		Description: req.Description,
		Author:      req.Author,
		Version:     req.Version,
		Scope:       model.AssetBundleScope(req.Scope),
		Industry:    req.Industry,
		Language:    req.Language,
		Tags:        req.Tags,
		Messages:    service.AssetBundleMessagesFromDTO(req.Messages),
		Status:      model.AssetBundleStatus(req.Status),
		CoverImage:  req.CoverImage,
	}
	if err := c.svc.UpdateBundle(ctx.Request.Context(), bundle); err != nil {
		logger.Errorf("[asset-bundle] update failed: %v", err)
		response.ErrorFromDB(ctx, err, err.Error())
		return
	}
	response.Success(ctx, bundle, "ok")
}

// Get 按 ID 查
func (c *AssetBundleController) Get(ctx *gin.Context) {
	id, err := strconv.ParseInt(ctx.Param("id"), 10, 64)
	if err != nil {
		response.Error(ctx, http.StatusBadRequest, "invalid id")
		return
	}
	bundle, err := c.svc.GetBundle(ctx.Request.Context(), id)
	if err != nil {
		response.Error(ctx, http.StatusNotFound, err.Error())
		return
	}
	response.Success(ctx, bundle, "ok")
}

// GetByAssetID 按业务键查
func (c *AssetBundleController) GetByAssetID(ctx *gin.Context) {
	aid := ctx.Param("aid")
	bundle, err := c.svc.GetBundleByAssetID(ctx.Request.Context(), aid)
	if err != nil {
		response.Error(ctx, http.StatusNotFound, err.Error())
		return
	}
	response.Success(ctx, bundle, "ok")
}

// List 分页
func (c *AssetBundleController) List(ctx *gin.Context) {
	var req dto.AssetBundleListRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		if err2 := ctx.ShouldBindQuery(&req); err2 != nil {
			response.Error(ctx, http.StatusBadRequest, err.Error())
			return
		}
	}
	list, total, err := c.svc.ListBundlesWithParams(ctx.Request.Context(),
		req.Keyword, req.Author, req.Industry, req.Language, req.Scope, assetBundleStatusToInt(model.AssetBundleStatus(req.Status)), strings.Join(req.Tags, ","), req.Page, req.Size)
	if err != nil {
		response.ErrorFromDB(ctx, err, err.Error())
		return
	}
	response.Success(ctx, dto.AssetBundleListResponse{
		List: service.FromAssetBundleModelList(list), Total: total, Page: req.Page, Size: req.Size,
	}, "ok")
}

// Publish 启用
func (c *AssetBundleController) Publish(ctx *gin.Context) {
	id, err := strconv.ParseInt(ctx.Param("id"), 10, 64)
	if err != nil {
		response.Error(ctx, http.StatusBadRequest, "invalid id")
		return
	}
	if err := c.svc.PublishBundle(ctx.Request.Context(), id); err != nil {
		response.ErrorFromDB(ctx, err, err.Error())
		return
	}
	response.Success(ctx, "ok", "ok")
}

// Archive 归档
func (c *AssetBundleController) Archive(ctx *gin.Context) {
	id, err := strconv.ParseInt(ctx.Param("id"), 10, 64)
	if err != nil {
		response.Error(ctx, http.StatusBadRequest, "invalid id")
		return
	}
	if err := c.svc.ArchiveBundle(ctx.Request.Context(), id); err != nil {
		response.ErrorFromDB(ctx, err, err.Error())
		return
	}
	response.Success(ctx, "ok", "ok")
}

// Enable 热启用资产包（方向 D1，立即生效，无需重启）
func (c *AssetBundleController) Enable(ctx *gin.Context) {
	id, err := strconv.ParseInt(ctx.Param("id"), 10, 64)
	if err != nil {
		response.Error(ctx, http.StatusBadRequest, "invalid id")
		return
	}
	bundle, err := c.svc.EnableBundle(ctx.Request.Context(), id)
	if err != nil {
		logger.Errorf("[asset-bundle] enable failed: %v", err)
		response.ErrorFromDB(ctx, err, err.Error())
		return
	}
	response.Success(ctx, bundle, "enabled")
}

// Disable 热禁用资产包（方向 D1，立即生效，无需重启）
func (c *AssetBundleController) Disable(ctx *gin.Context) {
	id, err := strconv.ParseInt(ctx.Param("id"), 10, 64)
	if err != nil {
		response.Error(ctx, http.StatusBadRequest, "invalid id")
		return
	}
	bundle, err := c.svc.DisableBundle(ctx.Request.Context(), id)
	if err != nil {
		logger.Errorf("[asset-bundle] disable failed: %v", err)
		response.ErrorFromDB(ctx, err, err.Error())
		return
	}
	response.Success(ctx, bundle, "disabled")
}

// GetEnabled 查询已热启用的资产包列表
func (c *AssetBundleController) GetEnabled(ctx *gin.Context) {
	list, err := c.svc.GetEnabledBundles(ctx.Request.Context())
	if err != nil {
		response.ErrorFromDB(ctx, err, err.Error())
		return
	}
	response.Success(ctx, gin.H{"list": list, "total": len(list)}, "ok")
}

// Delete 软删
func (c *AssetBundleController) Delete(ctx *gin.Context) {
	id, err := strconv.ParseInt(ctx.Param("id"), 10, 64)
	if err != nil {
		response.Error(ctx, http.StatusBadRequest, "invalid id")
		return
	}
	if err := c.svc.DeleteBundle(ctx.Request.Context(), id); err != nil {
		response.ErrorFromDB(ctx, err, err.Error())
		return
	}
	response.Success(ctx, "ok", "ok")
}

// Weave 织布算法
func (c *AssetBundleController) Weave(ctx *gin.Context) {
	var req dto.WeaveRequest
	if !response.BindJSON(ctx, &req) {
		return
	}
	in := service.WeaveInput{
		UserQuery:    req.UserQuery,
		ChatHistory:  service.AssetBundleMessagesFromDTO(req.ChatHistory),
		MerchantVars: req.MerchantVars,
		Sandbox:      req.Sandbox,
	}
	for _, d := range req.RAGDocs {
		in.RAGDocs = append(in.RAGDocs, service.RAGDocument{
			ID: d.ID, Title: d.Title, Content: d.Content, Score: d.Score, Source: d.Source,
		})
	}
	if req.Options != nil {
		in.Options = service.WeaveOptions{
			RAGPosition:         service.RAGInsertPosition(req.Options.RAGPosition),
			MaxHistoryMessages:  req.Options.MaxHistoryMessages,
			StripFewShotJSON:    req.Options.StripFewShotJSON,
			IncludeMerchantVars: req.Options.IncludeMerchantVars,
		}
	}
	msgs, err := c.svc.WeaveForRequest(ctx.Request.Context(), req.AssetID, req.UserQuery, &in)
	if err != nil {
		response.ErrorFromDB(ctx, err, err.Error())
		return
	}
	stats := dto.WeaveStats{
		AssetMessages:    len(in.Asset.Messages),
		RAGMessages:      0,
		HistoryMessages:  len(in.ChatHistory),
		FinalTotal:       len(msgs),
		StrippedFewShots: 0,
	}
	if len(req.RAGDocs) > 0 {
		stats.RAGMessages = 1
	}
	response.Success(ctx, dto.WeaveResponse{
		Messages:     service.AssetBundleMessagesToDTO(msgs),
		ResultLength: len(msgs),
		Stats:        stats,
	}, "ok")
}

// MerchantSave 商户低代码表单保存（把表单反向翻译成标准 messages 数组）
func (c *AssetBundleController) MerchantSave(ctx *gin.Context) {
	var req dto.MerchantFormSaveRequest
	if !response.BindJSON(ctx, &req) {
		return
	}
	bundle, err := service.BuildBundleFromMerchantForm(req)
	if err != nil {
		response.ErrorFromDB(ctx, err, err.Error())
		return
	}
	existing, _ := c.svc.GetBundleByAssetID(ctx.Request.Context(), req.AssetID)
	if existing != nil {
		bundle.ID = existing.ID
		if err := c.svc.UpdateBundle(ctx.Request.Context(), bundle); err != nil {
			response.ErrorFromDB(ctx, err, err.Error())
			return
		}
	} else {
		if err := c.svc.CreateBundle(ctx.Request.Context(), bundle); err != nil {
			response.ErrorFromDB(ctx, err, err.Error())
			return
		}
	}
	response.Success(ctx, bundle, "ok")
}

// MerchantParse 解析 messages 数组为商户表单（前端回显用）
func (c *AssetBundleController) MerchantParse(ctx *gin.Context) {
	aid := ctx.Param("aid")
	bundle, err := c.svc.GetBundleByAssetID(ctx.Request.Context(), aid)
	if err != nil {
		response.Error(ctx, http.StatusNotFound, err.Error())
		return
	}
	resp := service.ParseBundleToMerchantForm(bundle)
	response.Success(ctx, resp, "ok")
}

func assetBundleStatusToInt(s model.AssetBundleStatus) int {
	switch s {
	case model.AssetBundleStatusDraft:
		return 1
	case model.AssetBundleStatusActive:
		return 2
	case model.AssetBundleStatusInactive:
		return 3
	case model.AssetBundleStatusArchived:
		return 4
	default:
		return 0
	}
}
