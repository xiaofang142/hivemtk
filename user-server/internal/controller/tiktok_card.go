package controller

import (
	"net/http"
	"strconv"

	"hivemtk-user/internal/dto"
	"hivemtk-user/internal/pkg/utils/response"
	cardsvc "hivemtk-user/internal/service/cards"

	"github.com/gin-gonic/gin"
)

type TikTokCardController struct {
	svc cardsvc.TikTokCardService
}

// NewTikTokCardController 创建 TikTok 卡片控制器
//
// 五层架构合规：service 由 router 层注入（service.NewTikTokCardServiceWithDB），
// controller 不直接 import repository / db 包。
func NewTikTokCardController(svc cardsvc.TikTokCardService) *TikTokCardController {
	return &TikTokCardController{svc: svc}
}

func (ctrl *TikTokCardController) RegisterRoutes(router *gin.RouterGroup) {
	tiktok := router.Group("/tiktok-card")
	{
		tiktok.GET("/list", ctrl.List)
		tiktok.GET("/:id", ctrl.Get)
		tiktok.POST("", ctrl.Create)
		tiktok.PUT("/:id", ctrl.Update)
		tiktok.DELETE("/:id", ctrl.Delete)
		tiktok.POST("/generate-short-link", ctrl.GenerateShortLink)
		tiktok.GET("/stats/overall", ctrl.StatsOverall)
		tiktok.GET("/:id/stats", ctrl.Stats)
	}
}

// @Summary      查询 TikTok 卡片列表
// @Tags      tik-tok-card
// @Router    /api/tiktok-card/list [get]
func (ctrl *TikTokCardController) List(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	keyword := c.Query("keyword")
	isActiveStr := c.Query("is_active")

	req := &dto.TikTokCardListRequest{
		Page:     page,
		PageSize: pageSize,
		Keyword:  keyword,
	}
	if isActiveStr != "" {
		v, err := strconv.ParseBool(isActiveStr)
		if err == nil {
			req.IsActive = &v
		}
	}

	list, err := ctrl.svc.GetList(c.Request.Context(), req)
	if err != nil {
		response.ErrorFromDB(c, err, "获取列表失败: "+err.Error())
		return
	}
	response.Success(c, list, "获取成功")
}

// @Summary      按 ID 获取 TikTok 卡片详情
// @Tags      tik-tok-card
// @Router    /api/tiktok-card/{id} [get]
func (ctrl *TikTokCardController) Get(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		response.Error(c, http.StatusBadRequest, "无效的卡片ID")
		return
	}

	card, err := ctrl.svc.GetByID(c.Request.Context(), uint(id))
	if err != nil {
		response.Error(c, http.StatusNotFound, "卡片不存在: "+err.Error())
		return
	}
	response.Success(c, card, "获取成功")
}

// @Summary      创建 TikTok 卡片
// @Tags      tik-tok-card
// @Router    /api/tiktok-card [post]
func (ctrl *TikTokCardController) Create(c *gin.Context) {
	var req dto.TikTokCardCreateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, http.StatusBadRequest, "参数错误: "+err.Error())
		return
	}

	card, err := ctrl.svc.Create(c.Request.Context(), &req)
	if err != nil {
		response.ErrorFromDB(c, err, "创建失败: "+err.Error())
		return
	}
	response.Success(c, card, "创建成功")
}

// @Summary      更新 TikTok 卡片
// @Tags      tik-tok-card
// @Router    /api/tiktok-card/{id} [put]
func (ctrl *TikTokCardController) Update(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		response.Error(c, http.StatusBadRequest, "无效的卡片ID")
		return
	}

	var req dto.TikTokCardUpdateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, http.StatusBadRequest, "参数错误: "+err.Error())
		return
	}
	req.ID = uint(id)

	card, err := ctrl.svc.Update(c.Request.Context(), &req)
	if HandleDBError(c, err, "更新TikTok卡片") {
		return
	}
	response.Success(c, card, "更新成功")
}

// @Summary      删除 TikTok 卡片
// @Tags      tik-tok-card
// @Router    /api/tiktok-card/{id} [delete]
func (ctrl *TikTokCardController) Delete(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		response.Error(c, http.StatusBadRequest, "无效的卡片ID")
		return
	}

	if HandleDBError(c, ctrl.svc.Delete(c.Request.Context(), uint(id)), "删除TikTok卡片") {
		return
	}
	response.Success(c, nil, "删除成功")
}

// @Summary POST /api/tiktok-card/generate-short-link
// @Tags      tik-tok-card
// @Router    /api/tiktok-card/generate-short-link [post]
func (ctrl *TikTokCardController) GenerateShortLink(c *gin.Context) {
	// 入参名以**前端实际发的**为准：`user-web/src/api/tiktokCard.js` 传的是
	// `{ card_id: cardId }`（snake_case，与本仓其它卡片 DTO 一致）。
	// 这里原来绑的是 `json:"cardId"`（camelCase），于是前端这一条**永远 400**：
	// 绑定找不到 card_id，binding:"required" 直接把请求毙在门口。
	// 兄弟路由（douyin/xiaohongshu/xianyu/kuaishou）把 id 放路径上，不吃这个 body，
	// 所以只有 tiktok 这一条踩到了。
	// cardId 保留为兼容别名：第三方脚本可能已经按 camelCase 接过。
	// 注意：card_id 上**不挂** binding:"required" —— 那个标签会在绑定阶段就把
	// "只传 cardId" 的请求毙掉（找不到 card_id ⇒ 400 ⇒ 兼容分支永远走不到）。
	// 必填校验挪到绑定之后手工做，两种键名都能进到这里。
	var req struct {
		CardID    uint `json:"card_id"`
		CardIDAlt uint `json:"cardId"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, http.StatusBadRequest, "参数错误: "+err.Error())
		return
	}
	cardID := req.CardID
	if cardID == 0 {
		cardID = req.CardIDAlt
	}
	if cardID == 0 {
		response.Error(c, http.StatusBadRequest, "参数错误: card_id 必填")
		return
	}

	card, err := ctrl.svc.GenerateShortLink(c.Request.Context(), cardID)
	if HandleServiceError(c, err) {
		return
	}
	response.Success(c, gin.H{
		"short_link_url": card.ShortLinkURL,
		"short_code":     card.ShortCode,
		"card":           card,
	}, "生成成功")
}

// @Summary      查询 TikTok 卡片整体统计
// @Tags      tik-tok-card
// @Router    /api/tiktok-card/stats/overall [get]
func (ctrl *TikTokCardController) StatsOverall(c *gin.Context) {
	data, err := ctrl.svc.StatsOverall(c.Request.Context())
	if err != nil {
		response.ErrorFromDB(c, err, "获取统计失败: "+err.Error())
		return
	}
	response.Success(c, data, "获取成功")
}

// @Summary      查询指定 TikTok 卡片统计
// @Tags      tik-tok-card
// @Router    /api/tiktok-card/{id}/stats [get]
func (ctrl *TikTokCardController) Stats(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		response.Error(c, http.StatusBadRequest, "无效的卡片ID")
		return
	}

	data, err := ctrl.svc.Stats(c.Request.Context(), uint(id))
	if HandleDBError(c, err, "获取TikTok卡片统计") {
		return
	}
	response.Success(c, data, "获取成功")
}
