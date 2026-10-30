// Package controller T5 资产市场商店化控制器：
// 商户侧（评分/举报/版本列表）+ 平台审核后台（队列/approve/reject/suspend/举报处理/月账）。
package controller

import (
	"strconv"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/pkg/utils/response"
	"hivemtk-user/internal/service"

	"github.com/gin-gonic/gin"
)

// jsonRaw 请求体里的 JSON 原文（政策/权限声明）
type jsonRaw = []byte

// userClaimID 从 JWT 中间件落下的 user_id 取值；未登录场景返回 0
func userClaimID(c *gin.Context) int64 {
	if v, ok := c.Get("user_id"); ok {
		switch n := v.(type) {
		case uint:
			return int64(n)
		case int:
			return int64(n)
		case int64:
			return n
		}
	}
	return 0
}

// tenantIDOf 租户 ID（on 模式从 ctx，off 模式默认 1）
func tenantIDOf(c *gin.Context) uint {
	if id, ok := tenant.FromContext(c.Request.Context()); ok {
		return id
	}
	return 1
}

// AssetStoreController 商店化域控制器
type AssetStoreController struct {
	svc *service.AssetStoreService
}

// NewAssetStoreController 构造
func NewAssetStoreController(svc *service.AssetStoreService) *AssetStoreController {
	return &AssetStoreController{svc: svc}
}

func storeFail(c *gin.Context, err error) {
	response.Error(c, 400, err.Error())
}

func listingIDParam(c *gin.Context) (int64, bool) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		response.Error(c, 400, "非法 listing id")
		return 0, false
	}
	return id, true
}

// RegisterMerchantRoutes 商户侧路由（挂 /v1/asset-store）
func (ctrl *AssetStoreController) RegisterMerchantRoutes(g *gin.RouterGroup) {
	g.POST("/listings", ctrl.CreateListing)
	g.POST("/listings/:id/submit", ctrl.Submit)
	g.POST("/listings/:id/transition", ctrl.Transition)
	g.GET("/listings/:id/versions", ctrl.Versions)
	g.GET("/listings/:id/status", ctrl.Status)
	g.POST("/rate", ctrl.Rate)
	g.POST("/report", ctrl.Report)
}

// RegisterAdminRoutes 平台审核后台路由（挂 /v1/asset-admin，平台管理员中间件在外层）
func (ctrl *AssetStoreController) RegisterAdminRoutes(g *gin.RouterGroup) {
	g.GET("/review-queue", ctrl.ReviewQueue)
	g.POST("/listings/:id/approve", ctrl.Approve)
	g.POST("/listings/:id/reject", ctrl.Reject)
	g.POST("/listings/:id/suspend", ctrl.Suspend)
	g.GET("/reports", ctrl.Reports)
	g.POST("/reports/:id/handle", ctrl.HandleReport)
	g.GET("/attribution", ctrl.Attribution)
}

// @Summary      创建资产上架条目
// @Tags      asset-store
// @Router    /api/v1/asset-store/listings [post]
// CreateListing POST /listings
func (ctrl *AssetStoreController) CreateListing(c *gin.Context) {
	var req struct {
		AssetCode   string  `json:"asset_code" binding:"required"`
		AssetType   string  `json:"asset_type" binding:"required"`
		Name        string  `json:"name" binding:"required"`
		Industry    string  `json:"industry"`
		Summary     string  `json:"summary"`
		Policy      jsonRaw `json:"policy_declaration"`
		Permissions jsonRaw `json:"permission_declarations"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, 400, "参数错误: "+err.Error())
		return
	}
	l, err := ctrl.svc.CreateListing(c.Request.Context(), tenantIDOf(c), uint(userClaimID(c)), req.AssetCode, req.AssetType, req.Name, req.Industry, req.Summary, req.Policy, req.Permissions)
	if err != nil {
		storeFail(c, err)
		return
	}
	response.Success(c, l, "ok")
}

// @Summary      提交资产上架审核
// @Tags      asset-store
// @Router    /api/v1/asset-store/listings/{id}/submit [post]
// Submit POST /listings/:id/submit
func (ctrl *AssetStoreController) Submit(c *gin.Context) {
	id, ok := listingIDParam(c)
	if !ok {
		return
	}
	if err := ctrl.svc.SubmitListing(c.Request.Context(), tenantIDOf(c), id); err != nil {
		storeFail(c, err)
		return
	}
	response.Success(c, gin.H{"status": model.AssetListingSubmitted}, "ok")
}

// @Summary      资产上架状态流转
// @Tags      asset-store
// @Router    /api/v1/asset-store/listings/{id}/transition [post]
// Transition POST /listings/:id/transition（贡献者侧仅 suspended→draft 退回等合法链路）
func (ctrl *AssetStoreController) Transition(c *gin.Context) {
	id, ok := listingIDParam(c)
	if !ok {
		return
	}
	var req struct {
		To string `json:"to" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, 400, "参数错误: "+err.Error())
		return
	}
	if err := ctrl.svc.TransitionListing(c.Request.Context(), tenantIDOf(c), id, req.To, 0, ""); err != nil {
		storeFail(c, err)
		return
	}
	response.Success(c, gin.H{"status": req.To}, "ok")
}

// @Summary      查询上架资产版本列表
// @Tags      asset-store
// @Router    /api/v1/asset-store/listings/{id}/versions [get]
// Versions GET /listings/:id/versions
func (ctrl *AssetStoreController) Versions(c *gin.Context) {
	id, ok := listingIDParam(c)
	if !ok {
		return
	}
	out, err := ctrl.svc.Versions(c.Request.Context(), tenantIDOf(c), id)
	if err != nil {
		storeFail(c, err)
		return
	}
	response.Success(c, out, "ok")
}

// @Summary      查询上架资产状态
// @Tags      asset-store
// @Router    /api/v1/asset-store/listings/{id}/status [get]
// Status GET /listings/:id/status
func (ctrl *AssetStoreController) Status(c *gin.Context) {
	id, ok := listingIDParam(c)
	if !ok {
		return
	}
	versions, err := ctrl.svc.Versions(c.Request.Context(), tenantIDOf(c), id)
	if err != nil {
		storeFail(c, err)
		return
	}
	response.Success(c, gin.H{"versions": versions}, "ok")
}

// @Summary      对已购资产评分
// @Tags      asset-store
// @Router    /api/v1/asset-store/rate [post]
// Rate POST /rate
func (ctrl *AssetStoreController) Rate(c *gin.Context) {
	var req struct {
		ListingID  int64  `json:"listing_id" binding:"required"`
		PurchaseID int64  `json:"purchase_id" binding:"required"`
		Score      int    `json:"score" binding:"required"`
		Comment    string `json:"comment"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, 400, "参数错误: "+err.Error())
		return
	}
	if err := ctrl.svc.Rate(c.Request.Context(), tenantIDOf(c), req.ListingID, userClaimID(c), req.PurchaseID, req.Score, req.Comment); err != nil {
		storeFail(c, err)
		return
	}
	response.Success(c, nil, "ok")
}

// @Summary      提交资产举报
// @Tags      asset-store
// @Router    /api/v1/asset-store/report [post]
// Report POST /report
func (ctrl *AssetStoreController) Report(c *gin.Context) {
	var req struct {
		ListingID int64  `json:"listing_id" binding:"required"`
		Category  string `json:"category" binding:"required"`
		Detail    string `json:"detail"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, 400, "参数错误: "+err.Error())
		return
	}
	rep, err := ctrl.svc.Report(c.Request.Context(), tenantIDOf(c), req.ListingID, userClaimID(c), req.Category, req.Detail)
	if err != nil {
		storeFail(c, err)
		return
	}
	response.Success(c, rep, "ok")
}

// @Summary      资产审核队列
// @Tags      asset-store
// @Router    /api/v1/asset-admin/review-queue [get]
// ReviewQueue GET /review-queue
func (ctrl *AssetStoreController) ReviewQueue(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	out, total, err := ctrl.svc.ReviewQueue(c.Request.Context(), c.Query("asset_type"), page, pageSize)
	if err != nil {
		storeFail(c, err)
		return
	}
	response.Success(c, gin.H{"items": out, "total": total}, "ok")
}

// @Summary      审核通过并发布资产
// @Tags      asset-store
// @Router    /api/v1/asset-admin/listings/{id}/approve [post]
// Approve POST /listings/:id/approve（经 auto_check/manual_review 前置校验由状态机把关）
func (ctrl *AssetStoreController) Approve(c *gin.Context) {
	id, ok := listingIDParam(c)
	if !ok {
		return
	}
	if err := ctrl.svc.TransitionListing(c.Request.Context(), tenantIDOf(c), id, model.AssetListingPublished, userClaimID(c), ""); err != nil {
		storeFail(c, err)
		return
	}
	response.Success(c, gin.H{"status": model.AssetListingPublished}, "ok")
}

// @Summary      审核驳回资产上架（须填理由）
// @Tags      asset-store
// @Router    /api/v1/asset-admin/listings/{id}/reject [post]
// Reject POST /listings/:id/reject（必填 reason）
func (ctrl *AssetStoreController) Reject(c *gin.Context) {
	id, ok := listingIDParam(c)
	if !ok {
		return
	}
	var req struct {
		Reason string `json:"reason" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, 400, "驳回必须填写理由")
		return
	}
	if err := ctrl.svc.TransitionListing(c.Request.Context(), tenantIDOf(c), id, model.AssetListingRejected, userClaimID(c), req.Reason); err != nil {
		storeFail(c, err)
		return
	}
	response.Success(c, gin.H{"status": model.AssetListingRejected}, "ok")
}

// @Summary      下架已发布资产
// @Tags      asset-store
// @Router    /api/v1/asset-admin/listings/{id}/suspend [post]
// Suspend POST /listings/:id/suspend
func (ctrl *AssetStoreController) Suspend(c *gin.Context) {
	id, ok := listingIDParam(c)
	if !ok {
		return
	}
	var req struct {
		Reason string `json:"reason"`
	}
	_ = c.ShouldBindJSON(&req)
	if err := ctrl.svc.TransitionListing(c.Request.Context(), tenantIDOf(c), id, model.AssetListingSuspended, userClaimID(c), req.Reason); err != nil {
		storeFail(c, err)
		return
	}
	response.Success(c, gin.H{"status": model.AssetListingSuspended}, "ok")
}

// @Summary      资产举报列表
// @Tags      asset-store
// @Router    /api/v1/asset-admin/reports [get]
// Reports GET /reports
func (ctrl *AssetStoreController) Reports(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	items, total, err := ctrl.svc.ListReports(c.Request.Context(), c.Query("status"), page, pageSize)
	if err != nil {
		storeFail(c, err)
		return
	}
	response.Success(c, gin.H{"items": items, "total": total}, "ok")
}

// @Summary      处理资产举报
// @Tags      asset-store
// @Router    /api/v1/asset-admin/reports/{id}/handle [post]
// HandleReport POST /reports/:id/handle
func (ctrl *AssetStoreController) HandleReport(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		response.Error(c, 400, "非法举报 id")
		return
	}
	var req struct {
		Confirm bool   `json:"confirm"`
		Reason  string `json:"reason"`
	}
	_ = c.ShouldBindJSON(&req)
	if err := ctrl.svc.HandleReport(c.Request.Context(), tenantIDOf(c), id, userClaimID(c), req.Confirm, req.Reason); err != nil {
		storeFail(c, err)
		return
	}
	response.Success(c, nil, "ok")
}

// @Summary      资产归因报表
// @Tags      asset-store
// @Router    /api/v1/asset-admin/attribution [get]
// Attribution GET /attribution?period=YYYY-MM
func (ctrl *AssetStoreController) Attribution(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "50"))
	out, total, err := ctrl.svc.Attribution(c.Request.Context(), c.Query("period"), page, pageSize)
	if err != nil {
		storeFail(c, err)
		return
	}
	response.Success(c, gin.H{"items": out, "total": total}, "ok")
}
