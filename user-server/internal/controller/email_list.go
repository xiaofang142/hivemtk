package controller

import (
	"bytes"
	"hivemtk-user/internal/dto"
	email "hivemtk-user/internal/email/service"
	"hivemtk-user/internal/pkg/errhttp"
	"hivemtk-user/internal/pkg/utils/response"
	"image"
	"image/png"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// EmailListController 列表控制器
type EmailListController struct {
	svc *email.EmailListService
}

// NewEmailListController 创建列表控制器实例（服务由装配点注入）
func NewEmailListController(svc *email.EmailListService) *EmailListController {
	return &EmailListController{svc: svc}
}

// CreateEmailList 创建列表
func (c *EmailListController) CreateEmailList(ctx *gin.Context) {
	var req dto.CreateEmailListRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		response.Error(ctx, http.StatusBadRequest, "无效的列表 ID（需 UUID）")
		return
	}

	total, err := c.svc.CreateEmailList(ctx.Request.Context(), req.Subject, req.Content, strings.Join(req.Attachments, ","))
	if err != nil {
		response.ErrorFromDB(ctx, err, "创建邮件列表失败")
		return
	}
	response.Success(ctx, map[string]int64{
		"total": total,
	}, "success")
}

// @Summary      获取列表列表
// @Tags      email-list
// @Router    /api/email/list [get]
// GetEmailListList 获取列表列表
func (c *EmailListController) GetEmailListList(ctx *gin.Context) {
	var req dto.GetEmailListRequest
	if err := ctx.ShouldBindQuery(&req); err != nil {
		response.Error(ctx, http.StatusBadRequest, "无效的列表 ID（需 UUID）")
		return
	}
	if req.Page <= 0 {
		req.Page = 1
	}
	if req.PageSize <= 0 {
		req.PageSize = 20
	}
	resp, err := c.svc.GetEmailListListDTO(ctx.Request.Context(), req.Page, req.PageSize)
	if err != nil {
		response.ErrorFromDB(ctx, err, "获取邮件列表失败")
		return
	}
	response.Success(ctx, resp, "success")
}

// @Summary      获取列表详情
// @Tags      email-list
// @Router    /api/email/lists/{id} [get]
// GetEmailListDetail 获取列表详情
func (c *EmailListController) GetEmailListDetail(ctx *gin.Context) {
	var req struct {
		ID string `uri:"id" binding:"required"`
	}
	if err := ctx.ShouldBindUri(&req); err != nil {
		response.Error(ctx, http.StatusBadRequest, "无效的列表 ID（需 UUID）")
		return
	}
	listID, err := uuid.Parse(req.ID)
	if err != nil {
		response.Error(ctx, http.StatusBadRequest, "无效的列表 ID（需 UUID）")
		return
	}

	resp, err := c.svc.GetEmailListByIDDTO(ctx.Request.Context(), listID)
	if err != nil {
		response.ErrorFromDB(ctx, err, "邮件列表不存在")
		return
	}
	response.Success(ctx, resp, "success")
}

// UpdateEmailList 更新列表
func (c *EmailListController) UpdateEmailList(ctx *gin.Context) {
	var req dto.UpdateEmailListRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		response.Error(ctx, http.StatusBadRequest, "无效的列表 ID（需 UUID）")
		return
	}

	if _, err := uuid.Parse(req.ID); err != nil {
		response.Error(ctx, http.StatusBadRequest, "无效的列表 ID（需 UUID）")
		return
	}

	if err := c.svc.UpdateEmailListDTO(ctx.Request.Context(), req); err != nil {
		response.ErrorFromDB(ctx, err, "更新邮件列表失败")
		return
	}
	response.Success(ctx, nil, "success")
}

// @Summary      删除列表
// @Tags      email-list
// @Router    /api/email/list/{id} [delete]
// DeleteEmailList 删除列表
func (c *EmailListController) DeleteEmailList(ctx *gin.Context) {
	var req struct {
		ID string `uri:"id" binding:"required"`
	}
	if err := ctx.ShouldBindUri(&req); err != nil {
		response.Error(ctx, http.StatusBadRequest, "无效的列表 ID（需 UUID）")
		return
	}

	listID, err := uuid.Parse(req.ID)
	if err != nil {
		response.Error(ctx, http.StatusBadRequest, "无效的列表 ID（需 UUID）")
		return
	}

	if err = c.svc.DeleteEmailList(ctx.Request.Context(), listID); err != nil {
		if errhttp.IsNotFoundError(err) {
			response.Error(ctx, http.StatusNotFound, "邮件列表不存在")
			return
		}
		errhttp.HandleDBError(ctx, err, "删除邮件列表失败")
		return
	}
	response.Success(ctx, nil, "success")
}

func (c *EmailListController) TraceEmail(ctx *gin.Context) {
	img := image.NewRGBA(image.Rect(0, 0, 1, 1))

	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		ctx.AbortWithStatus(500)
		return
	}

	var req struct {
		TraceID string `uri:"trace_id" binding:"required"`
	}
	if err := ctx.ShouldBindUri(&req); err != nil {
		ctx.Data(200, "image/png", buf.Bytes())
		return
	}

	traceID, err := uuid.Parse(req.TraceID)
	if err != nil {
		ctx.Data(200, "image/png", buf.Bytes())
		return
	}

	err = c.svc.UpdateEmailListReadInfo(ctx.Request.Context(), traceID)
	if err != nil {
		ctx.Data(200, "image/png", buf.Bytes())
		return
	}

	ctx.Data(200, "image/png", buf.Bytes())
}

// @Summary      获取邮件列表关联的追踪事件（基于 JobsID 查询，返回 JSON）
// @Tags      email-list
// @Router    /api/email/lists/{id}/tracking [get]
// GetTracking 获取邮件列表关联的追踪事件（基于 JobsID 查询，返回 JSON）
func (c *EmailListController) GetTracking(ctx *gin.Context) {
	var req struct {
		ID   string `uri:"id" binding:"required"`
		Page int    `form:"page"`
		Size int    `form:"page_size"`
	}
	if err := ctx.ShouldBindUri(&req); err != nil {
		response.Error(ctx, http.StatusBadRequest, "无效的列表 ID（需 UUID）")
		return
	}
	if req.Page <= 0 {
		req.Page = 1
	}
	if req.Size <= 0 {
		req.Size = 50
	}
	listID, err := uuid.Parse(req.ID)
	if err != nil {
		response.Error(ctx, http.StatusBadRequest, "无效的列表 ID（需 UUID）")
		return
	}
	events, total, err := c.svc.GetTrackingEvents(ctx.Request.Context(), listID, req.Page, req.Size)
	if err != nil {
		response.ErrorFromDB(ctx, err, "邮件列表不存在")
		return
	}
	response.Success(ctx, map[string]interface{}{
		"list":  events,
		"total": total,
	}, "success")
}
