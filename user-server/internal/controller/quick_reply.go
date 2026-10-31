package controller

import (
	"errors"

	"hivemtk-user/internal/pkg/utils/response"
	"hivemtk-user/internal/service"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
)

// QuickReplyController 快捷回复控制器
type QuickReplyController struct {
	replyService *service.QuickReplyService
}

// NewQuickReplyController 创建快捷回复控制器实例
func NewQuickReplyController() *QuickReplyController {
	return &QuickReplyController{
		replyService: service.NewQuickReplyService(),
	}
}

// replyQuickReplyError 快捷回复域统一错误出口：不存在 → 404，其余走通用 DB 映射。
func replyQuickReplyError(ctx *gin.Context, err error, op string) {
	if errors.Is(err, service.ErrQuickReplyNotFound) {
		response.NotFound(ctx, err.Error())
		return
	}
	response.ErrorFromDB(ctx, err, op)
}

// @Summary      查询快捷回复列表
// @Tags      quick-reply
// @Router    /api/quick-replies [get]
// GetReplies 获取快捷回复列表
func (c *QuickReplyController) GetReplies(ctx *gin.Context) {
	category := ctx.Query("category")
	replies, err := c.replyService.GetReplies(ctx.Request.Context(), category)
	if err != nil {
		response.ErrorFromDB(ctx, err, "获取快捷回复失败")
		return
	}

	response.Success(ctx, replies, "获取成功")
}

// @Summary      创建快捷回复
// @Tags      quick-reply
// @Router    /api/quick-replies [post]
// CreateReply 创建快捷回复
func (c *QuickReplyController) CreateReply(ctx *gin.Context) {
	createdBy := getUserIDFromContext(ctx)

	var req service.CreateReplyRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		response.Error(ctx, http.StatusBadRequest, "请求参数错误：category、title、content 均为必填，channel / sort_order / is_public 可选")
		return
	}

	reply, err := c.replyService.CreateReply(ctx.Request.Context(), createdBy, &req)
	if err != nil {
		replyQuickReplyError(ctx, err, "创建快捷回复失败")
		return
	}

	response.Success(ctx, reply, "创建成功")
}

// @Summary      更新快捷回复
// @Tags      quick-reply
// @Router    /api/quick-replies/{id} [put]
// UpdateReply 更新快捷回复
func (c *QuickReplyController) UpdateReply(ctx *gin.Context) {
	idStr := ctx.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		response.Error(ctx, http.StatusBadRequest, "无效的快捷回复ID")
		return
	}

	var req service.CreateReplyRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		response.Error(ctx, http.StatusBadRequest, "请求参数错误：category、title、content 均为必填，channel / sort_order / is_public 可选")
		return
	}

	reply, err := c.replyService.UpdateReply(ctx.Request.Context(), uint(id), &req)
	if err != nil {
		replyQuickReplyError(ctx, err, "更新快捷回复失败")
		return
	}

	response.Success(ctx, reply, "更新成功")
}

// @Summary      删除快捷回复
// @Tags      quick-reply
// @Router    /api/quick-replies/{id} [delete]
// DeleteReply 删除快捷回复
func (c *QuickReplyController) DeleteReply(ctx *gin.Context) {
	idStr := ctx.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		response.Error(ctx, http.StatusBadRequest, "无效的快捷回复ID")
		return
	}

	if err := c.replyService.DeleteReply(ctx.Request.Context(), uint(id)); err != nil {
		replyQuickReplyError(ctx, err, "删除快捷回复失败")
		return
	}

	response.Success(ctx, nil, "删除成功")
}

// @Summary      查询快捷回复分类列表
// @Tags      quick-reply
// @Router    /api/quick-replies/categories [get]
// GetReplyCategories 获取快捷回复分类
func (c *QuickReplyController) GetReplyCategories(ctx *gin.Context) {
	categories, err := c.replyService.GetCategories(ctx.Request.Context())
	if err != nil {
		response.ErrorFromDB(ctx, err, "获取快捷回复分类失败")
		return
	}

	response.Success(ctx, categories, "获取成功")
}
