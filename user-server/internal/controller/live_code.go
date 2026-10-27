package controller

import (
	"errors"
	"hivemtk-user/internal/dto"
	"hivemtk-user/internal/pkg/utils/response"
	"hivemtk-user/internal/reach/card/template"
	"hivemtk-user/internal/service"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
)

// LiveCodeController 活码控制器
type LiveCodeController struct {
	liveCodeService service.LiveCodeService
}

// NewLiveCodeController 创建活码控制器实例
func NewLiveCodeController(liveCodeService service.LiveCodeService) *LiveCodeController {
	return &LiveCodeController{
		liveCodeService: liveCodeService,
	}
}

// handleLiveCodeWriteError 分流活码写口的业务错误：
//   - 短链重了 → 409（不是服务故障，换一个短链即可）
//   - 域名停用/不存在 → 400（先修域名池）
//   - 其余仍走 HandleDBError（真 DB 故障保持 500、记录不存在 404）
func handleLiveCodeWriteError(ctx *gin.Context, err error, operation string) bool {
	if err == nil {
		return false
	}
	switch {
	case errors.Is(err, service.ErrLiveCodeShortLinkExists):
		response.Error(ctx, http.StatusConflict, err.Error())
		return true
	case errors.Is(err, service.ErrLiveCodeDomainUnavailable):
		response.Error(ctx, http.StatusBadRequest, err.Error())
		return true
	}
	return HandleDBError(ctx, err, operation)
}

// @Summary      创建活码
// @Tags      live-code
// @Router    /api/live-codes/create [post]
// Create 创建活码
func (c *LiveCodeController) Create(ctx *gin.Context) {
	var req dto.CreateLiveCodeRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		response.Error(ctx, http.StatusBadRequest, err.Error())
		return
	}

	liveCode, err := c.liveCodeService.Create(ctx.Request.Context(), &req)
	if handleLiveCodeWriteError(ctx, err, "创建活码") {
		return
	}

	response.Success(ctx, liveCode, "创建成功")
}

// @Summary      更新活码
// @Tags      live-code
// @Router    /api/live-codes/{id}/update [put]
// Update 更新活码
func (c *LiveCodeController) Update(ctx *gin.Context) {
	idStr := ctx.Param("id")

	var req dto.UpdateLiveCodeRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		response.Error(ctx, http.StatusBadRequest, err.Error())
		return
	}

	liveCode, err := c.liveCodeService.Update(ctx.Request.Context(), idStr, &req)
	if handleLiveCodeWriteError(ctx, err, "更新活码") {
		return
	}

	response.Success(ctx, liveCode, "更新成功")
}

// @Summary      删除活码
// @Tags      live-code
// @Router    /api/live-codes/{id}/delete [delete]
// Delete 删除活码
func (c *LiveCodeController) Delete(ctx *gin.Context) {
	idStr := ctx.Param("id")

	err := c.liveCodeService.Delete(ctx.Request.Context(), idStr)
	if HandleDBError(ctx, err, "删除活码") {
		return
	}

	response.Success(ctx, nil, "删除成功")
}

// @Summary      按 ID 获取活码详情
// @Tags      live-code
// @Router    /api/live-codes/{id} [get]
// GetByID 根据ID获取活码
func (c *LiveCodeController) GetByID(ctx *gin.Context) {
	idStr := ctx.Param("id")

	liveCode, err := c.liveCodeService.GetByID(ctx.Request.Context(), idStr)
	if HandleDBError(ctx, err, "获取活码") {
		return
	}

	response.Success(ctx, liveCode, "获取成功")
}

// @Summary      分页查询活码列表（名称/状态筛选）
// @Tags      live-code
// @Router    /api/live-codes [get]
// GetList 获取活码列表
func (c *LiveCodeController) GetList(ctx *gin.Context) {
	pageStr := ctx.DefaultQuery("page", "1")
	pageSizeStr := ctx.DefaultQuery("pageSize", "10")
	name := ctx.Query("name")
	status := ctx.Query("status")

	page, err := strconv.Atoi(pageStr)
	if err != nil || page < 1 {
		page = 1
	}

	pageSize, err := strconv.Atoi(pageSizeStr)
	if err != nil || pageSize < 1 || pageSize > 100 {
		pageSize = 10
	}

	liveCodes, total, err := c.liveCodeService.GetList(ctx.Request.Context(), page, pageSize, name, status)
	if err != nil {
		response.ErrorFromDB(ctx, err, err.Error())
		return
	}

	response.Success(ctx, dto.LiveCodeListResponse{
		List:  liveCodes,
		Total: total,
	}, "获取成功")
}

// @Summary      获取活码统计（浏览/点击等）
// @Tags      live-code
// @Router    /api/live-codes/{id}/stats [get]
// GetStats 获取活码统计
func (c *LiveCodeController) GetStats(ctx *gin.Context) {
	idStr := ctx.Param("id")

	stats, err := c.liveCodeService.GetStats(ctx.Request.Context(), idStr)
	if HandleDBError(ctx, err, "获取活码统计") {
		return
	}

	response.Success(ctx, stats, "获取成功")
}

// @Summary      生成成功
// @Tags      live-code
// @Router    /api/live-codes/{id}/qrcodes/create [post]
// GenerateQRCode 生成活码二维码
func (c *LiveCodeController) GenerateQRCode(ctx *gin.Context) {
	idStr := ctx.Param("id")

	var req dto.GenerateQRCodeRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		response.Error(ctx, http.StatusBadRequest, err.Error())
		return
	}

	qrCode, err := c.liveCodeService.GenerateQRCode(ctx.Request.Context(), idStr, &req)
	if HandleDBError(ctx, err, "生成活码二维码") {
		return
	}

	response.Success(ctx, qrCode, "生成成功")
}

// @Summary      获取活码二维码列表
// @Tags      live-code
// @Router    /api/live-codes/{id}/qrcodes [get]
// GetQRCodes 获取活码二维码列表
func (c *LiveCodeController) GetQRCodes(ctx *gin.Context) {
	idStr := ctx.Param("id")

	qrCodes, err := c.liveCodeService.GetQRCodes(ctx.Request.Context(), idStr)
	if HandleDBError(ctx, err, "获取活码二维码列表") {
		return
	}

	response.Success(ctx, qrCodes, "获取成功")
}

// @Summary      获取活码二维码扫码统计
// @Tags      live-code
// @Router    /api/live-codes/qrcodes/{qr_id}/stats [get]
// GetQRStats 获取活码二维码统计
func (c *LiveCodeController) GetQRStats(ctx *gin.Context) {
	qrIDStr := ctx.Param("qr_id")

	stats, err := c.liveCodeService.GetQRStats(ctx.Request.Context(), qrIDStr)
	if HandleDBError(ctx, err, "获取活码二维码统计") {
		return
	}

	response.Success(ctx, stats, "获取成功")
}

// @Summary      分享成功
// @Tags      live-code
// @Router    /api/live-codes/{id}/share [post]
// Share 分享活码
func (c *LiveCodeController) Share(ctx *gin.Context) {
	idStr := ctx.Param("id")

	req := dto.ShareLiveCodeRequest{
		IPAddress: ctx.ClientIP(),
		UserAgent: ctx.GetHeader("User-Agent"),
	}

	shareResponse, err := c.liveCodeService.Share(ctx.Request.Context(), idStr, &req)
	if HandleServiceError(ctx, err) {
		return
	}

	response.Success(ctx, shareResponse, "分享成功")
}

// @Summary      活码短链重定向
// @Tags      live-code
// @Router    /api/l/{code} [get]
// RedirectLiveCode 活码短链重定向
func (c *LiveCodeController) RedirectLiveCode(ctx *gin.Context) {
	code := ctx.Param("code")
	if code == "" {
		ctx.String(http.StatusBadRequest, "无效的短链")
		return
	}

	liveCode, err := c.liveCodeService.GetByShortLink(ctx.Request.Context(), code)
	if err != nil {
		ctx.String(http.StatusNotFound, "活码不存在")
		return
	}

	if liveCode.Status != 1 {
		ctx.String(http.StatusNotFound, "活码已停用")
		return
	}

	req := &dto.ShareLiveCodeRequest{
		IPAddress: ctx.ClientIP(),
		UserAgent: ctx.GetHeader("User-Agent"),
	}

	response, err := c.liveCodeService.Share(ctx.Request.Context(), liveCode.ID, req)
	if err != nil {
		ctx.String(http.StatusInternalServerError, "分享失败: %v", err)
		return
	}

	userAgent := strings.ToLower(ctx.GetHeader("User-Agent"))

	if strings.Contains(userAgent, "micromessenger") {
		ctx.Redirect(http.StatusMovedPermanently, response.EntryLink)
		return
	}

	ctx.Redirect(http.StatusMovedPermanently, response.LandingLink)
}

// @Summary      渲染活码页面
// @Tags      live-code
// @Router    /api/livecode/{id} [get]
// RenderLiveCodePage 渲染活码页面
func (c *LiveCodeController) RenderLiveCodePage(ctx *gin.Context) {
	idStr := ctx.Param("id")

	liveCode, err := c.liveCodeService.GetByID(ctx.Request.Context(), idStr)
	if err != nil {
		ctx.String(http.StatusNotFound, "活码不存在")
		return
	}

	stats, err := c.liveCodeService.GetStats(ctx.Request.Context(), idStr)
	if err != nil {
		ctx.String(http.StatusInternalServerError, "获取活码统计失败")
		return
	}

	qrCodes, err := c.liveCodeService.GetQRCodes(ctx.Request.Context(), idStr)
	if err != nil {
		ctx.String(http.StatusInternalServerError, "获取二维码列表失败")
		return
	}

	templateData := &template.LiveCodeTemplateData{
		ID:          liveCode.ID,
		Title:       liveCode.Name,
		Description: "点击跳转到目标页面",
		ImageURL:    liveCode.ImageURL,
		EntryURL:    liveCode.EntryURL,
		LandingURL:  liveCode.LandingURL,
		ShowStats:   true,
		ShowQR:      len(qrCodes) > 0,
		TotalClicks: stats.TotalClicks,
		TodayClicks: stats.TodayClicks,
		QRCount:     len(qrCodes),
	}

	if len(qrCodes) > 0 {
		templateData.QRImageURL = qrCodes[0].QRImageURL
	}

	templateService := template.NewTemplateService("internal/template")

	html, err := templateService.GenerateLiveCodePage(templateData)
	if err != nil {
		ctx.String(http.StatusInternalServerError, "生成页面失败")
		return
	}

	ctx.Header("Content-Type", "text/html; charset=utf-8")
	ctx.String(http.StatusOK, html)
}

// @Summary      记录成功
// @Tags      live-code
// @Router    /api/livecode/{id}/click [post]
// RecordClick 记录点击统计
func (c *LiveCodeController) RecordClick(ctx *gin.Context) {
	idStr := ctx.Param("id")

	var req struct {
		UserAgent string `json:"user_agent"`
		Referrer  string `json:"referrer"`
	}

	if err := ctx.ShouldBindJSON(&req); err != nil {
		response.Error(ctx, 400, "请求参数错误")
		return
	}

	err := c.liveCodeService.RecordClick(ctx.Request.Context(), idStr, ctx.ClientIP(), req.UserAgent, req.Referrer)
	if err != nil {
		response.ErrorFromDB(ctx, err, "记录点击统计失败")
		return
	}

	response.Success(ctx, nil, "记录成功")
}

// @Summary      删除活码二维码
// @Tags      live-code
// @Router    /api/live-codes/qrcodes/{qr_id}/delete [delete]
// DeleteLiveCodeQR 删除活码二维码
func (c *LiveCodeController) DeleteLiveCodeQR(ctx *gin.Context) {
	idStr := ctx.Param("qr_id")
	if idStr == "" {
		response.Error(ctx, 400, "二维码ID不能为空")
		return
	}

	err := c.liveCodeService.DeleteQRCode(ctx.Request.Context(), idStr)
	if err != nil {
		if isNotFoundError(err) {
			response.Error(ctx, http.StatusNotFound, err.Error())
			return
		}
		response.ErrorFromDB(ctx, err, "删除二维码失败", err.Error())
		return
	}

	response.Success(ctx, nil, "删除成功")
}

// @Summary      更新活码二维码
// @Tags      live-code
// @Router    /api/live-codes/qrcodes/{qr_id}/update [put]
// UpdateLiveCodeQR 更新活码二维码
func (c *LiveCodeController) UpdateLiveCodeQR(ctx *gin.Context) {
	idStr := ctx.Param("qr_id")
	if idStr == "" {
		response.Error(ctx, 400, "二维码ID不能为空")
		return
	}

	var req dto.UpdateLiveCodeQRRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		response.Error(ctx, 400, "请求参数错误", err.Error())
		return
	}

	err := c.liveCodeService.UpdateQRCode(ctx.Request.Context(), idStr, &req)
	if HandleDBError(ctx, err, "更新二维码") {
		return
	}

	response.Success(ctx, nil, "更新成功")
}
