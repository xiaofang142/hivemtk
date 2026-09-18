package controller

import (
	"hivemtk-user/internal/dto"
	email "hivemtk-user/internal/email/service"
	"hivemtk-user/internal/pkg/utils/response"
	"net/http"

	"github.com/gin-gonic/gin"
)

type EmailSendController struct {
	svc *email.EmailSendService
}

func NewEmailSendController() *EmailSendController {
	return &EmailSendController{svc: email.NewEmailSendService()}
}

// SendEmail godoc
// @Summary      发送单封邮件
// @Description  提交一封邮件到发送队列，支持模板变量替换与排程发送
// @Tags         Email Send
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        body  body  dto.SendEmailRequest  true  "邮件发送请求"
// @Success      200   {object}  response.Response  "提交成功"
// @Failure      400   {object}  response.Response  "参数错误"
// @Router       /api/email/send [post]
func (c *EmailSendController) SendEmail(ctx *gin.Context) {
	var req dto.SendEmailRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		response.Error(ctx, http.StatusBadRequest, "参数错误："+err.Error())
		return
	}

	email, err := c.svc.SendEmail(ctx.Request.Context(), req)
	if err != nil {
		response.ErrorFromDB(ctx, err, "发送失败："+err.Error())
		return
	}

	response.Success(ctx, email, "发送成功")
}
