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
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"hivemtk-user/internal/middleware"

	"github.com/gin-gonic/gin"

	"hivemtk-user/internal/dto"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/errhttp"
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

// @Summary      已提交平台审核
// @Tags      asset-bundle
// @Router    /api/asset-bundle/{id}/submit-platform [post]
// SubmitToPlatform 将本地资产包提交平台审核上架（开发者上架链路）
//
// 路由参数 :id 的语义与兄弟路由（publish/archive/enable/disable）保持一致：资产包主键 id。
// 前端 Playground 传的是 bundle.id（数字主键），而 SubmitToPlatform 需要的是 asset_id
// （业务 slug），历史上这里直接把 :id 当 asset_id 用，导致数字主键永远查不到资产包、
// 上架链路 100% 报 "record not found"。这里显式做一次主键→asset_id 归一化。
func (c *AssetBundleController) SubmitToPlatform(ctx *gin.Context) {
	if rejectPlatformDisabled(ctx) {
		return
	}

	rawID := ctx.Param("id")
	if rawID == "" {
		response.Error(ctx, http.StatusBadRequest, "invalid asset id")
		return
	}
	assetID, err := c.resolveAssetID(ctx, rawID)
	if err != nil {
		response.ErrorFromDB(ctx, err, "提交平台失败: "+err.Error())
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

// assetBundleWriteError 统一资产包写操作的错误出口：
// 记录不存在 → 404 + 具体对象文案（仓储只回 gorm.ErrRecordNotFound 的英文原文，
// 直接透出会变成「record not found」）；其余交给 ErrorFromDB 按错误类型分派。
func assetBundleWriteError(ctx *gin.Context, err error, notFoundMsg, op string) {
	if errhttp.IsNotFoundError(err) {
		response.NotFound(ctx, notFoundMsg)
		return
	}
	response.ErrorFromDB(ctx, err, op)
}

func assetBundleNotFoundMsg(id int64) string {
	return "资产包不存在（id=" + strconv.FormatInt(id, 10) + "）"
}

// resolveAssetID 把路由参数归一化成 asset_id：
//   - 数字主键（前端 bundle.id）→ 查库取该资产包的 asset_id
//   - 非数字（历史调用方直接传 asset_id slug）→ 原样返回
func (c *AssetBundleController) resolveAssetID(ctx *gin.Context, rawID string) (string, error) {
	bundleID, parseErr := strconv.ParseInt(rawID, 10, 64)
	if parseErr != nil {
		// 非数字：按 asset_id slug 兼容处理。
		return rawID, nil
	}
	bundle, err := c.svc.GetBundle(ctx.Request.Context(), bundleID)
	if err != nil {
		return "", err
	}
	if bundle == nil {
		return "", fmt.Errorf("资产包不存在: %d", bundleID)
	}
	return bundle.AssetID, nil
}

// @Summary      创建资产包
// @Tags      asset-bundle
// @Router    /api/asset-bundle [post]
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
		response.ErrorFromDB(ctx, err, "操作失败，请稍后重试")
		return
	}
	response.Success(ctx, bundle, "ok")
}

// @Summary      更新资产包
// @Tags      asset-bundle
// @Router    /api/asset-bundle/{id} [put]
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
		assetBundleWriteError(ctx, err, assetBundleNotFoundMsg(id), "更新资产包失败")
		return
	}
	// W-5：Update 只回写 DTO 携带的列（tenant_id 等身份列不在更新白名单内），
	// 直接回显请求 DTO 构造的模型会让 tenant_id 恒为 0。更新成功后按主键回读，
	// 让响应反映库内真相；回读失败不推翻已成功的更新，降级回显原模型并告警。
	if updated, getErr := c.svc.GetBundle(ctx.Request.Context(), id); getErr != nil || updated == nil {
		logger.Warnf("[asset-bundle] update succeeded but re-read failed: id=%d err=%v", id, getErr)
	} else {
		bundle = updated
	}
	response.Success(ctx, bundle, "ok")
}

// @Summary      按 ID 查
// @Tags      asset-bundle
// @Router    /api/asset-bundle/{id} [get]
// Get 按 ID 查
func (c *AssetBundleController) Get(ctx *gin.Context) {
	id, err := strconv.ParseInt(ctx.Param("id"), 10, 64)
	if err != nil {
		response.Error(ctx, http.StatusBadRequest, "invalid id")
		return
	}
	bundle, err := c.svc.GetBundle(ctx.Request.Context(), id)
	if err != nil {
		if errhttp.IsNotFoundError(err) {
			response.NotFound(ctx, "资产包不存在（id="+strconv.FormatInt(id, 10)+"）")
			return
		}
		response.ErrorFromDB(ctx, err, "查询资产包失败")
		return
	}
	response.Success(ctx, bundle, "ok")
}

// @Summary      按业务键查
// @Tags      asset-bundle
// @Router    /api/asset-bundle/by-aid/{aid} [get]
// GetByAssetID 按业务键查
func (c *AssetBundleController) GetByAssetID(ctx *gin.Context) {
	aid := ctx.Param("aid")
	bundle, err := c.svc.GetBundleByAssetID(ctx.Request.Context(), aid)
	if err != nil {
		if errhttp.IsNotFoundError(err) {
			response.NotFound(ctx, "资产包不存在（asset_id="+aid+"）")
			return
		}
		response.ErrorFromDB(ctx, err, "查询资产包失败")
		return
	}
	response.Success(ctx, bundle, "ok")
}

// @Summary      分页
// @Tags      asset-bundle
// @Router    /api/asset-bundle/list [post]
// List 分页
func (c *AssetBundleController) List(ctx *gin.Context) {
	var req dto.AssetBundleListRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		if err2 := ctx.ShouldBindQuery(&req); err2 != nil {
			response.Error(ctx, http.StatusBadRequest, err.Error())
			return
		}
	}
	// 仓储会静默归一化分页（page<=0→1、size<=0→20、size>200→200）。
	// 原样回显请求值会出现「size:0 却返回 20 条」，前端分页器据此算错页数，
	// 因此这里用同一规则归一化后再回显。
	page, size := req.Page, req.Size
	if page <= 0 {
		page = 1
	}
	if size <= 0 {
		size = 20
	}
	if size > 200 {
		size = 200
	}
	list, total, err := c.svc.ListBundlesWithParams(ctx.Request.Context(),
		req.Keyword, req.Author, req.Industry, req.Language, req.Scope, assetBundleStatusToInt(model.AssetBundleStatus(req.Status)), strings.Join(req.Tags, ","), page, size)
	if err != nil {
		response.ErrorFromDB(ctx, err, "操作失败，请稍后重试")
		return
	}
	response.Success(ctx, dto.AssetBundleListResponse{
		List: service.FromAssetBundleModelList(list), Total: total, Page: page, Size: size,
	}, "ok")
}

// @Summary      启用
// @Tags      asset-bundle
// @Router    /api/asset-bundle/{id}/publish [post]
// Publish 启用
func (c *AssetBundleController) Publish(ctx *gin.Context) {
	id, err := strconv.ParseInt(ctx.Param("id"), 10, 64)
	if err != nil {
		response.Error(ctx, http.StatusBadRequest, "invalid id")
		return
	}
	if err := c.svc.PublishBundle(ctx.Request.Context(), id); err != nil {
		assetBundleWriteError(ctx, err, assetBundleNotFoundMsg(id), "启用资产包失败")
		return
	}
	response.Success(ctx, "ok", "ok")
}

// @Summary      归档
// @Tags      asset-bundle
// @Router    /api/asset-bundle/{id}/archive [post]
// Archive 归档
func (c *AssetBundleController) Archive(ctx *gin.Context) {
	id, err := strconv.ParseInt(ctx.Param("id"), 10, 64)
	if err != nil {
		response.Error(ctx, http.StatusBadRequest, "invalid id")
		return
	}
	if err := c.svc.ArchiveBundle(ctx.Request.Context(), id); err != nil {
		assetBundleWriteError(ctx, err, assetBundleNotFoundMsg(id), "归档资产包失败")
		return
	}
	response.Success(ctx, "ok", "ok")
}

// @Summary      热启用资产包（方向 D1，立即生效，无需重启）
// @Tags      asset-bundle
// @Router    /api/asset-bundle/{id}/enable [post]
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
		assetBundleWriteError(ctx, err, assetBundleNotFoundMsg(id), "热启用资产包失败")
		return
	}
	response.Success(ctx, bundle, "enabled")
}

// @Summary      热禁用资产包（方向 D1，立即生效，无需重启）
// @Tags      asset-bundle
// @Router    /api/asset-bundle/{id}/disable [post]
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
		assetBundleWriteError(ctx, err, assetBundleNotFoundMsg(id), "热禁用资产包失败")
		return
	}
	response.Success(ctx, bundle, "disabled")
}

// @Summary      查询已热启用的资产包列表
// @Tags      asset-bundle
// @Router    /api/asset-bundle/enabled/list [post]
// GetEnabled 查询已热启用的资产包列表
func (c *AssetBundleController) GetEnabled(ctx *gin.Context) {
	list, err := c.svc.GetEnabledBundles(ctx.Request.Context())
	if err != nil {
		response.ErrorFromDB(ctx, err, "操作失败，请稍后重试")
		return
	}
	response.Success(ctx, gin.H{"list": list, "total": len(list)}, "ok")
}

// @Summary      软删
// @Tags      asset-bundle
// @Router    /api/asset-bundle/{id} [delete]
// Delete 软删
func (c *AssetBundleController) Delete(ctx *gin.Context) {
	id, err := strconv.ParseInt(ctx.Param("id"), 10, 64)
	if err != nil {
		response.Error(ctx, http.StatusBadRequest, "invalid id")
		return
	}
	if err := c.svc.DeleteBundle(ctx.Request.Context(), id); err != nil {
		assetBundleWriteError(ctx, err, assetBundleNotFoundMsg(id), "删除资产包失败")
		return
	}
	response.Success(ctx, "ok", "ok")
}

// @Summary      织布算法
// @Tags      asset-bundle
// @Router    /api/asset-bundle/weave [post]
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
		response.ErrorFromDB(ctx, err, "操作失败，请稍后重试")
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

// @Summary      商户低代码表单保存（把表单反向翻译成标准 messages 数组）
// @Tags      asset-bundle
// @Router    /api/asset-bundle/merchant-save [post]
// MerchantSave 商户低代码表单保存（把表单反向翻译成标准 messages 数组）
func (c *AssetBundleController) MerchantSave(ctx *gin.Context) {
	var req dto.MerchantFormSaveRequest
	if !response.BindJSON(ctx, &req) {
		return
	}
	bundle, err := service.BuildBundleFromMerchantForm(req)
	if err != nil {
		response.ErrorFromDB(ctx, err, "操作失败，请稍后重试")
		return
	}
	existing, _ := c.svc.GetBundleByAssetID(ctx.Request.Context(), req.AssetID)
	if existing != nil {
		bundle.ID = existing.ID
		if err := c.svc.UpdateBundle(ctx.Request.Context(), bundle); err != nil {
			response.ErrorFromDB(ctx, err, "操作失败，请稍后重试")
			return
		}
	} else {
		if err := c.svc.CreateBundle(ctx.Request.Context(), bundle); err != nil {
			response.ErrorFromDB(ctx, err, "操作失败，请稍后重试")
			return
		}
	}
	response.Success(ctx, bundle, "ok")
}

// @Summary      解析 messages 数组为商户表单（前端回显用）
// @Tags      asset-bundle
// @Router    /api/asset-bundle/merchant-parse/{aid} [post]
// MerchantParse 解析 messages 数组为商户表单（前端回显用）
func (c *AssetBundleController) MerchantParse(ctx *gin.Context) {
	aid := ctx.Param("aid")
	bundle, err := c.svc.GetBundleByAssetID(ctx.Request.Context(), aid)
	if err != nil {
		if errhttp.IsNotFoundError(err) {
			response.NotFound(ctx, "资产包不存在（asset_id="+aid+"）")
			return
		}
		response.ErrorFromDB(ctx, err, "解析资产包失败")
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
