package controller

import (
	"hivemtk-user/internal/dto"
	email "hivemtk-user/internal/email/service"
	"hivemtk-user/internal/pkg/utils/response"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// EmailDraftController 草稿控制器
type EmailDraftController struct {
	svc *email.EmailDraftService
}

// NewEmailDraftController 创建草稿控制器实例
func NewEmailDraftController() *EmailDraftController {
	return &EmailDraftController{svc: email.NewEmailDraftService()}
}

// @Summary      创建邮件草稿
// @Tags      email-draft
// @Router    /api/email/drafts [post]
// CreateEmailDraft 创建草稿
func (c *EmailDraftController) CreateEmailDraft(ctx *gin.Context) {
	var req dto.CreateEmailDraftRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		response.Error(ctx, http.StatusBadRequest, "参数错误")
		return
	}

	resp, err := c.svc.CreateEmailDraftDTO(ctx.Request.Context(), req)
	if err != nil {
		response.ErrorFromDB(ctx, err, "创建邮件草稿失败")
		return
	}
	response.Success(ctx, resp, "创建成功")
}

// @Summary      查询邮件草稿列表
// @Tags      email-draft
// @Router    /api/email/drafts [get]
// GetEmailDraftList 获取草稿列表
func (c *EmailDraftController) GetEmailDraftList(ctx *gin.Context) {
	resp, err := c.svc.GetEmailDraftListDTO(ctx.Request.Context())
	if err != nil {
		response.ErrorFromDB(ctx, err, "获取邮件草稿列表失败")
		return
	}
	response.Success(ctx, resp, "获取成功")
}

// @Summary      查询邮件草稿详情
// @Tags      email-draft
// @Router    /api/email/drafts/{id} [get]
// GetEmailDraftDetail 获取草稿详情
func (c *EmailDraftController) GetEmailDraftDetail(ctx *gin.Context) {
	var req struct {
		ID string `uri:"id" binding:"required"`
	}
	if err := ctx.ShouldBindUri(&req); err != nil {
		response.Error(ctx, http.StatusBadRequest, "参数错误")
		return
	}

	draftID, err := uuid.Parse(req.ID)
	if err != nil {
		response.Error(ctx, http.StatusBadRequest, "无效的 ID 格式")
		return
	}

	resp, err := c.svc.GetEmailDraftByIDDTO(ctx.Request.Context(), draftID)
	if err != nil {
		if isNotFoundError(err) {
			response.Error(ctx, http.StatusNotFound, "草稿不存在")
			return
		}
		response.ErrorFromDB(ctx, err, "邮件草稿不存在")
		return
	}
	response.Success(ctx, resp, "获取成功")
}

// @Summary      更新邮件草稿
// @Tags      email-draft
// @Router    /api/email/drafts/{id} [put]
// UpdateEmailDraft 更新草稿
// PUT /api/email/drafts/:id
//
// 草稿 id 来自**路径参数**（与同资源的 GET / DELETE 一致）。
// 之前只认 body 里的 id（dto.UpdateEmailDraftRequest.ID），而前端
// `updateDraft(id, data)` 走的是 `PUT /api/email/drafts/${id}` ——
// id 在路径上、body 里没有 ⇒ 后端拿到空串 ⇒ 恒 400「无效的 ID 格式」
// ⇒ **邮件草稿的编辑页永远存不进去**。
// body 里的 id 仍作兜底（老调用方可能两处都带），但两者不一致时以路径为准。
func (c *EmailDraftController) UpdateEmailDraft(ctx *gin.Context) {
	var req dto.UpdateEmailDraftRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		response.Error(ctx, http.StatusBadRequest, "参数错误")
		return
	}

	if pathID := ctx.Param("id"); pathID != "" {
		if req.ID != "" && req.ID != pathID {
			response.Error(ctx, http.StatusBadRequest, "路径参数与请求体中的草稿 ID 不一致")
			return
		}
		req.ID = pathID
	}

	if _, err := uuid.Parse(req.ID); err != nil {
		response.Error(ctx, http.StatusBadRequest, "无效的 ID 格式")
		return
	}

	if err := c.svc.UpdateEmailDraftDTO(ctx.Request.Context(), req); err != nil {
		response.ErrorFromDB(ctx, err, "更新邮件草稿失败")
		return
	}
	response.Success(ctx, nil, "更新成功")
}

// @Summary      删除邮件草稿
// @Tags      email-draft
// @Router    /api/email/drafts/{id} [delete]
// DeleteEmailDraft 删除草稿
func (c *EmailDraftController) DeleteEmailDraft(ctx *gin.Context) {
	var req struct {
		ID string `uri:"id" binding:"required"`
	}
	if err := ctx.ShouldBindUri(&req); err != nil {
		response.Error(ctx, http.StatusBadRequest, "参数错误")
		return
	}

	draftID, err := uuid.Parse(req.ID)
	if err != nil {
		response.Error(ctx, http.StatusBadRequest, "无效的 ID 格式")
		return
	}

	if err = c.svc.DeleteEmailDraft(ctx.Request.Context(), draftID); err != nil {
		response.ErrorFromDB(ctx, err, "删除邮件草稿失败")
		return
	}
	response.Success(ctx, nil, "删除成功")
}
