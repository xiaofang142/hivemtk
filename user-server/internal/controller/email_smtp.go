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

// CreateEmailSmtp 创建SMTP配置
func (c *EmailSmtpController) CreateEmailSmtp(ctx *gin.Context) {
	var req dto.CreateEmailSmtpRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		response.Error(ctx, http.StatusBadRequest, err.Error())
		return
	}

	resp, err := c.svc.CreateEmailSmtpDTO(ctx.Request.Context(), req)
	if err != nil {
		response.ErrorFromDB(ctx, err, err.Error())
		return
	}
	response.Success(ctx, resp, "创建成功")
}

// GetEmailSmtpList 获取SMTP配置列表
func (c *EmailSmtpController) GetEmailSmtpList(ctx *gin.Context) {
	resp, err := c.svc.GetEmailSmtpListDTO(ctx.Request.Context())
	if err != nil {
		response.Error(ctx, http.StatusBadRequest, err.Error())
		return
	}
	response.Success(ctx, resp, "获取成功")
}

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
		response.ErrorFromDB(ctx, err, err.Error())
		return
	}
	response.Success(ctx, resp, "获取成功")
}

// UpdateEmailSmtp 更新SMTP配置
func (c *EmailSmtpController) UpdateEmailSmtp(ctx *gin.Context) {
	var req dto.UpdateEmailSmtpRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		response.Error(ctx, http.StatusBadRequest, err.Error())
		return
	}

	if err := c.svc.UpdateEmailSmtpDTO(ctx.Request.Context(), req); err != nil {
		response.ErrorFromDB(ctx, err, err.Error())
		return
	}
	response.Success(ctx, nil, "更新成功")
}

// DeleteEmailSmtp 删除SMTP配置
func (c *EmailSmtpController) DeleteEmailSmtp(ctx *gin.Context) {
	var req dto.DeleteEmailSmtpRequest
	if err := ctx.ShouldBindUri(&req); err != nil {
		response.Error(ctx, http.StatusBadRequest, err.Error())
		return
	}

	if err := c.svc.DeleteEmailSmtp(ctx.Request.Context(), req.ID); err != nil {
		response.ErrorFromDB(ctx, err, err.Error())
		return
	}
	response.Success(ctx, nil, "删除成功")
}
