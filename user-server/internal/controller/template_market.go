package controller

import (
	"errors"
	"strconv"

	"github.com/gin-gonic/gin"
	bizerr "hivemtk-user/internal/domain/errors"
	"hivemtk-user/internal/pkg/utils/logger"
	"hivemtk-user/internal/pkg/utils/response"
	"hivemtk-user/internal/service"
)

// TemplateMarketController 模板/技能市场（F2 最小可用版，specs/013）。
//
// 只读橱窗档：目录浏览 + 分类 + 单条详情，不含支付/发布/审核闭环。
// 三个端点都是 public 档（未登录可逛），对齐 module-market/list 的公开语义——
// 目录条目只回元数据（名称/分类/简介/来源/可用性），不含任何个人或租户数据。
type TemplateMarketController struct {
	svc *service.TemplateMarketService
}

// NewTemplateMarketController 构造控制器（进程内目录仓储）。
func NewTemplateMarketController() *TemplateMarketController {
	return &TemplateMarketController{svc: service.NewTemplateMarketService()}
}

// List GET /api/template-market/list?kind=&category=&keyword=&page=&page_size=
// @Tags      template-market
// @Router    /api/template-market/list [get]
func (c *TemplateMarketController) List(ctx *gin.Context) {
	page, _ := strconv.Atoi(ctx.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(ctx.DefaultQuery("page_size", "50"))
	if pageSize < 1 || pageSize > 200 {
		pageSize = 50
	}
	list, err := c.svc.List(ctx.Request.Context(), service.MarketQuery{
		Kind:     ctx.Query("kind"),
		Category: ctx.Query("category"),
		Keyword:  ctx.Query("keyword"),
	})
	if err != nil {
		logger.Errorf("template market service error: %v", err)
		response.ErrorWithBusinessCode(ctx, bizerr.CodeInternal, "操作失败，请稍后重试", gin.H{})
		return
	}
	start := (page - 1) * pageSize
	if start < 0 {
		start = 0
	}
	if start > len(list) {
		start = len(list)
	}
	end := start + pageSize
	if end > len(list) {
		end = len(list)
	}
	response.Success(ctx, gin.H{"list": list[start:end], "total": len(list)}, "ok")
}

// Categories GET /api/template-market/categories
// @Tags      template-market
// @Router    /api/template-market/categories [get]
func (c *TemplateMarketController) Categories(ctx *gin.Context) {
	cats, err := c.svc.Categories(ctx.Request.Context())
	if err != nil {
		logger.Errorf("template market service error: %v", err)
		response.ErrorWithBusinessCode(ctx, bizerr.CodeInternal, "操作失败，请稍后重试", gin.H{})
		return
	}
	response.Success(ctx, cats, "ok")
}

// Detail GET /api/template-market/detail/:id
// @Tags      template-market
// @Router    /api/template-market/detail/{id} [get]
func (c *TemplateMarketController) Detail(ctx *gin.Context) {
	item, err := c.svc.Get(ctx.Request.Context(), ctx.Param("id"))
	if errors.Is(err, service.ErrMarketEntryNotFound) {
		response.ErrorWithBusinessCode(ctx, bizerr.CodeNotFound, "模板/技能不存在", gin.H{})
		return
	}
	if err != nil {
		logger.Errorf("template market service error: %v", err)
		response.ErrorWithBusinessCode(ctx, bizerr.CodeInternal, "操作失败，请稍后重试", gin.H{})
		return
	}
	response.Success(ctx, item, "ok")
}
