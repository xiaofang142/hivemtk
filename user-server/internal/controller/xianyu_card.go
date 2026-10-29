package controller

import (
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"

	"hivemtk-user/internal/dto"
	"hivemtk-user/internal/pkg/utils/response"
	cardpkg "hivemtk-user/internal/reach/card"
	cardsvc "hivemtk-user/internal/service/cards"

	"github.com/gin-gonic/gin"
)

// XianyuCardController 闲鱼卡片控制器
type XianyuCardController struct {
	service  cardsvc.XianyuCardService
	statsSvc cardsvc.XianyuCardStatsService
}

// NewXianyuCardController 创建闲鱼卡片控制器实例
// statsSvc 在构造时注入，避免 Controller 方法内直接访问数据库（架构违规）
func NewXianyuCardController(service cardsvc.XianyuCardService, statsSvc cardsvc.XianyuCardStatsService) *XianyuCardController {
	return &XianyuCardController{
		service:  service,
		statsSvc: statsSvc,
	}
}

// @Summary      创建闲鱼卡片
// @Tags      xianyu-card
// @Router    /api/xianyu/create [post]
// Create 创建闲鱼卡片
func (c *XianyuCardController) Create(ctx *gin.Context) {
	var req dto.XianyuCardCreateRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		response.Error(ctx, http.StatusBadRequest, response.ErrInvalidParams, err.Error())
		return
	}

	card, err := c.service.Create(ctx.Request.Context(), &req)
	if HandleDBError(ctx, err, "创建闲鱼卡片") {
		return
	}

	response.Success(ctx, card, response.ErrSuccess)
}

// @Summary      更新闲鱼卡片
// @Tags      xianyu-card
// @Router    /api/xianyu-card/{id} [put]
// Update 更新闲鱼卡片
func (c *XianyuCardController) Update(ctx *gin.Context) {
	var req dto.XianyuCardUpdateRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		response.Error(ctx, http.StatusBadRequest, response.ErrInvalidParams, err.Error())
		return
	}

	card, err := c.service.Update(ctx.Request.Context(), &req)
	if HandleDBError(ctx, err, "更新闲鱼卡片") {
		return
	}

	response.Success(ctx, card, response.ErrUpdateSuccess)
}

// @Summary      删除闲鱼卡片
// @Tags      xianyu-card
// @Router    /api/xianyu-card/{id} [delete]
// Delete 删除闲鱼卡片
func (c *XianyuCardController) Delete(ctx *gin.Context) {
	idStr := ctx.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 64)
	if err != nil {
		response.Error(ctx, http.StatusBadRequest, response.ErrInvalidIDFormat, err.Error())
		return
	}

	if HandleDBError(ctx, c.service.Delete(ctx.Request.Context(), uint(id)), "删除闲鱼卡片") {
		return
	}

	response.Success(ctx, gin.H{"id": id}, response.ErrDeleteSuccess)
}

// @Summary      根据ID获取闲鱼卡片
// @Tags      xianyu-card
// @Router    /api/xianyu-card/{id} [get]
// GetByID 根据ID获取闲鱼卡片
func (c *XianyuCardController) GetByID(ctx *gin.Context) {
	idStr := ctx.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 64)
	if err != nil {
		response.Error(ctx, http.StatusBadRequest, response.ErrInvalidIDFormat, err.Error())
		return
	}

	card, err := c.service.GetByID(ctx.Request.Context(), uint(id))
	if HandleDBError(ctx, err, "获取闲鱼卡片") {
		return
	}

	response.Success(ctx, card, response.ErrGetSuccess)
}

// @Summary      获取闲鱼卡片列表
// @Tags      xianyu-card
// @Router    /api/xianyu-card/list [get]
// GetList 获取闲鱼卡片列表
func (c *XianyuCardController) GetList(ctx *gin.Context) {
	var req dto.XianyuCardListRequest
	if err := ctx.ShouldBindQuery(&req); err != nil {
		response.Error(ctx, http.StatusBadRequest, response.ErrInvalidParams, err.Error())
		return
	}

	if req.Page == 0 {
		req.Page = 1
	}
	if req.PageSize == 0 {
		req.PageSize = 10
	}

	cards, err := c.service.GetList(ctx.Request.Context(), &req)
	if err != nil {
		response.ErrorFromDB(ctx, err, response.ErrGetListFailed, err.Error())
		return
	}

	response.Success(ctx, cards, response.ErrGetSuccess)
}

// @Summary      生成短链
// @Tags      xianyu-card
// @Router    /api/xianyu/{id}/generate-short-link [post]
// GenerateShortLink 生成短链
func (c *XianyuCardController) GenerateShortLink(ctx *gin.Context) {
	idStr := ctx.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 64)
	if err != nil {
		response.Error(ctx, http.StatusBadRequest, response.ErrInvalidIDFormat, err.Error())
		return
	}

	card, err := c.service.GetByIDWithRefresh(ctx.Request.Context(), uint(id))
	if HandleDBError(ctx, err, "获取闲鱼卡片") {
		return
	}

	response.Success(ctx, gin.H{
		"short_link_url": card.ShortLinkURL,
		"short_code":     card.ShortCode,
	}, response.ErrSuccess)
}

// @Summary      渲染卡片分享页面（统一卡片聊天页，含联系客服按钮）
// @Tags      xianyu-card
// @Router    /share/xianyu/{id} [get]
// SharePage 渲染卡片分享页面（统一卡片聊天页，含联系客服按钮）
func (c *XianyuCardController) SharePage(ctx *gin.Context) {
	idStr := ctx.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		ctx.String(http.StatusBadRequest, "无效的卡片ID")
		return
	}
	renderCardChatPage(ctx, c.service.GenerateCardChatPage, uint(id), buildBaseURL(ctx))
}

// xianyuActivityReport 是卡片页三条埋点上报的请求体。
//
// ip/user_agent/referer 与模板现有语义保持一致；请求体为空时回落到连接/请求头里的值。
type xianyuActivityReport struct {
	IP        string `json:"ip"`
	UserAgent string `json:"user_agent"`
	Referer   string `json:"referer"`
}

// @Summary      记录闲鱼卡片点击
// @Tags      xianyu-card
// @Router    /api/xianyu/{id}/click [post]
// RecordClick 记录闲鱼卡片点击（公网写口，必须携带卡片渲染时签发的埋点凭证）
func (c *XianyuCardController) RecordClick(ctx *gin.Context) {
	c.recordXianyuActivity(ctx, "click")
}

// @Summary      记录闲鱼卡片分享
// @Tags      xianyu-card
// @Router    /api/xianyu/{id}/share [post]
// RecordShare 记录闲鱼卡片分享（公网写口，必须携带卡片渲染时签发的埋点凭证）
func (c *XianyuCardController) RecordShare(ctx *gin.Context) {
	c.recordXianyuActivity(ctx, "share")
}

// @Summary      记录闲鱼卡片浏览
// @Tags      xianyu-card
// @Router    /api/xianyu/{id}/view [post]
// RecordView 记录闲鱼卡片浏览（公网写口，必须携带卡片渲染时签发的埋点凭证）
func (c *XianyuCardController) RecordView(ctx *gin.Context) {
	c.recordXianyuActivity(ctx, "view")
}

// recordXianyuActivity 是三条埋点写口的公共实现。
//
// 鉴权判据：请求头里的凭证必须由服务端按 :id 签发且未过期（见 reach/card/token.go）。
// 缺凭证 401、凭证与 id 不匹配/被篡改/过期 403——没有任何一条能在匿名状态下写库。
func (c *XianyuCardController) recordXianyuActivity(ctx *gin.Context, activityType string) {
	id, err := strconv.ParseUint(ctx.Param("id"), 10, 64)
	if err != nil || id == 0 {
		response.Error(ctx, http.StatusBadRequest, response.ErrInvalidIDFormat, "无效的卡片ID")
		return
	}

	token := strings.TrimSpace(ctx.GetHeader(cardpkg.XianyuReportTokenHeader))
	if token == "" {
		response.Error(ctx, http.StatusUnauthorized, "缺少卡片访问凭证")
		return
	}
	if err := cardpkg.VerifyXianyuReportToken(uint(id), token); err != nil {
		response.Error(ctx, http.StatusForbidden, "卡片访问凭证无效")
		return
	}

	var req xianyuActivityReport
	if err := ctx.ShouldBindJSON(&req); err != nil && !errors.Is(err, io.EOF) {
		response.Error(ctx, http.StatusBadRequest, response.ErrInvalidParams, err.Error())
		return
	}

	ip := strings.TrimSpace(req.IP)
	if ip == "" {
		ip = ctx.ClientIP()
	}
	userAgent := req.UserAgent
	if userAgent == "" {
		userAgent = ctx.GetHeader("User-Agent")
	}
	referer := req.Referer
	if referer == "" {
		referer = ctx.GetHeader("Referer")
	}
	// 列宽 IP 45 / user_agent 500 / referer 500：公网请求体不可信，先截断再落库。
	ip = truncateField(ip, 45)
	userAgent = truncateField(userAgent, 500)
	referer = truncateField(referer, 500)

	var recordErr error
	switch activityType {
	case "click":
		recordErr = c.statsSvc.RecordClick(ctx.Request.Context(), uint(id), ip, userAgent, referer)
	case "share":
		recordErr = c.statsSvc.RecordShare(ctx.Request.Context(), uint(id), ip, userAgent, referer)
	case "view":
		recordErr = c.statsSvc.RecordView(ctx.Request.Context(), uint(id), ip, userAgent, referer)
	default:
		response.Error(ctx, http.StatusBadRequest, response.ErrInvalidParams, "未知的上报类型")
		return
	}
	if recordErr != nil {
		response.ErrorFromDB(ctx, recordErr, "记录卡片活动失败")
		return
	}

	response.Success(ctx, gin.H{"activity": activityType}, response.ErrSuccess)
}

// truncateField 按字节上限截断公网请求体字段，避免超长值把埋点表写失败。
func truncateField(value string, max int) string {
	if len(value) <= max {
		return value
	}
	return value[:max]
}
