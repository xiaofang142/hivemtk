package controller

import (
	"errors"

	"hivemtk-user/internal/pkg/utils/response"
	"hivemtk-user/internal/service"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
)

// SessionTagController 会话标签控制器
type SessionTagController struct {
	tagService *service.SessionTagService
}

// NewSessionTagController 创建会话标签控制器实例
func NewSessionTagController() *SessionTagController {
	return &SessionTagController{
		tagService: service.NewSessionTagService(),
	}
}

// replySessionTagError 会话标签域统一错误出口：不存在 → 404，编码重复 → 409。
func replySessionTagError(ctx *gin.Context, err error, code string, op string) {
	switch {
	case errors.Is(err, service.ErrSessionTagNotFound):
		response.NotFound(ctx, err.Error())
	case errors.Is(err, service.ErrSessionTagCodeExists):
		response.Error(ctx, http.StatusConflict, "标签编码已存在: "+code)
	default:
		response.ErrorFromDB(ctx, err, op)
	}
}

// @Summary      查询会话标签列表
// @Tags      session-tag
// @Router    /api/session-tags [get]
// GetTags 获取标签列表
func (c *SessionTagController) GetTags(ctx *gin.Context) {
	tags, err := c.tagService.GetTags(ctx.Request.Context())
	if err != nil {
		response.ErrorFromDB(ctx, err, "获取会话标签失败")
		return
	}

	response.Success(ctx, tags, "获取成功")
}

// @Summary      创建会话标签
// @Tags      session-tag
// @Router    /api/session-tags [post]
// CreateTag 创建标签
func (c *SessionTagController) CreateTag(ctx *gin.Context) {
	var req service.CreateTagRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		response.Error(ctx, http.StatusBadRequest, "请求参数错误：name 与 code 均为必填，group / color / description / sort_order 可选")
		return
	}

	tag, err := c.tagService.CreateTag(ctx.Request.Context(), &req)
	if err != nil {
		// 编码重复是 409 而不是 400：请求本身没错，是"这个编码已经有人用了"。
		// 修前这里把 PG 的 duplicate key 原文（含索引名与 SQL 片段）直接回给前端。
		replySessionTagError(ctx, err, req.Code, "创建会话标签失败")
		return
	}

	response.Success(ctx, tag, "创建成功")
}

// @Summary      更新会话标签
// @Tags      session-tag
// @Router    /api/session-tags/{id} [put]
// UpdateTag 更新标签
func (c *SessionTagController) UpdateTag(ctx *gin.Context) {
	idStr := ctx.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		response.Error(ctx, http.StatusBadRequest, "无效的标签ID")
		return
	}

	var req service.CreateTagRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		response.Error(ctx, http.StatusBadRequest, "请求参数错误：name 与 code 均为必填，group / color / description / sort_order 可选")
		return
	}

	tag, err := c.tagService.UpdateTag(ctx.Request.Context(), uint(id), &req)
	if err != nil {
		replySessionTagError(ctx, err, req.Code, "更新会话标签失败")
		return
	}

	response.Success(ctx, tag, "更新成功")
}

// @Summary      删除会话标签
// @Tags      session-tag
// @Router    /api/session-tags/{id} [delete]
// DeleteTag 删除标签
func (c *SessionTagController) DeleteTag(ctx *gin.Context) {
	idStr := ctx.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		response.Error(ctx, http.StatusBadRequest, "无效的标签ID")
		return
	}

	if err := c.tagService.DeleteTag(ctx.Request.Context(), uint(id)); err != nil {
		replySessionTagError(ctx, err, "", "删除会话标签失败")
		return
	}

	response.Success(ctx, nil, "删除成功")
}
