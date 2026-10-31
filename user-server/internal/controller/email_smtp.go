package controller

import (
	"hivemtk-user/internal/dto"
	email "hivemtk-user/internal/email/service"
	"hivemtk-user/internal/pkg/utils/response"
	"net/http"

	"github.com/gin-gonic/gin"
)

// EmailSmtpController SMTP配置控制器
type EmailSmtpController struct {
	svc *email.EmailSmtpService
}

// NewEmailSmtpController 创建SMTP配置控制器实例
func NewEmailSmtpController() *EmailSmtpController {
	return &EmailSmtpController{svc: email.NewEmailSmtpService()}
}

// @Summary      创建 SMTP 发信配置
// @Tags      email-smtp
// @Router    /api/email/smtp [post]
// CreateEmailSmtp 创建SMTP配置
func (c *EmailSmtpController) CreateEmailSmtp(ctx *gin.Context) {
	var req dto.CreateEmailSmtpRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		response.Error(ctx, http.StatusBadRequest, "请求参数错误：请检查字段类型与必填项")
		return
	}

	resp, err := c.svc.CreateEmailSmtpDTO(ctx.Request.Context(), req)
	if err != nil {
		response.ErrorFromDB(ctx, err, "创建 SMTP 配置失败")
		return
	}
	response.Success(ctx, resp, "创建成功")
}

// @Summary      查询 SMTP 发信配置列表
// @Tags      email-smtp
// @Router    /api/email/smtp [get]
// GetEmailSmtpList 获取SMTP配置列表
func (c *EmailSmtpController) GetEmailSmtpList(ctx *gin.Context) {
	resp, err := c.svc.GetEmailSmtpListDTO(ctx.Request.Context())
	if err != nil {
		response.ErrorFromDB(ctx, err, "获取 SMTP 配置列表失败")
		return
	}
	response.Success(ctx, resp, "获取成功")
}

// @Summary      按 ID 查询 SMTP 配置详情
// @Tags      email-smtp
// @Router    /api/email/smtps/{id} [get]
// GetEmailSmtp 获取SMTP配置详情
func (c *EmailSmtpController) GetEmailSmtp(ctx *gin.Context) {
	smtpIDStr := ctx.Param("id")
	if smtpIDStr == "" {
		response.Error(ctx, http.StatusBadRequest, "SMTP ID不能为空")
		return
	}

	resp, err := c.svc.GetEmailSmtpDTO(ctx.Request.Context(), smtpIDStr)
	if err != nil {
		if isNotFoundError(err) {
			response.Error(ctx, http.StatusNotFound, "SMTP配置不存在")
			return
		}
		response.ErrorFromDB(ctx, err, "SMTP 配置不存在")
		return
	}
	response.Success(ctx, resp, "获取成功")
}

// @Summary      更新 SMTP 发信配置
// @Tags      email-smtp
// @Router    /api/email/smtp/{id} [put]
// UpdateEmailSmtp 更新SMTP配置
func (c *EmailSmtpController) UpdateEmailSmtp(ctx *gin.Context) {
	var req dto.UpdateEmailSmtpRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		response.Error(ctx, http.StatusBadRequest, "请求参数错误：请检查字段类型与必填项")
		return
	}
	// 前端 PUT /email/smtp/:id 的 body 不带 id，主键只来自路径参数。
	if id := ctx.Param("id"); id != "" {
		req.ID = id
	}
	if req.ID == "" {
		response.Error(ctx, http.StatusBadRequest, "SMTP ID不能为空")
		return
	}
	if _, err := c.svc.GetEmailSmtpDTO(ctx.Request.Context(), req.ID); err != nil {
		if isNotFoundError(err) {
			response.Error(ctx, http.StatusNotFound, "SMTP配置不存在")
			return
		}
		response.ErrorFromDB(ctx, err, "更新 SMTP 配置失败")
		return
	}

	if err := c.svc.UpdateEmailSmtpDTO(ctx.Request.Context(), req); err != nil {
		response.ErrorFromDB(ctx, err, "更新 SMTP 配置失败")
		return
	}
	response.Success(ctx, nil, "更新成功")
}

// @Summary      删除 SMTP 发信配置
// @Tags      email-smtp
// @Router    /api/email/smtp/{id} [delete]
// DeleteEmailSmtp 删除SMTP配置
func (c *EmailSmtpController) DeleteEmailSmtp(ctx *gin.Context) {
	var req dto.DeleteEmailSmtpRequest
	if err := ctx.ShouldBindUri(&req); err != nil {
		response.Error(ctx, http.StatusBadRequest, "请求参数错误：请检查字段类型与必填项")
		return
	}

	if _, err := c.svc.GetEmailSmtpDTO(ctx.Request.Context(), req.ID); err != nil {
		if isNotFoundError(err) {
			response.Error(ctx, http.StatusNotFound, "SMTP配置不存在")
			return
		}
		response.ErrorFromDB(ctx, err, "删除 SMTP 配置失败")
		return
	}

	if err := c.svc.DeleteEmailSmtp(ctx.Request.Context(), req.ID); err != nil {
		response.ErrorFromDB(ctx, err, "删除 SMTP 配置失败")
		return
	}
	response.Success(ctx, nil, "删除成功")
}
