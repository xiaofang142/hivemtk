package controller

import (
	"errors"
	"net/http"

	"hivemtk-user/internal/middleware"
	"hivemtk-user/internal/pkg/utils/logger"
	"hivemtk-user/internal/pkg/utils/response"
	"hivemtk-user/internal/service"

	"github.com/gin-gonic/gin"
)

// SelfServiceController 用户自助服务控制器
type SelfServiceController struct {
	passwordResetService *service.PasswordResetService
}

// NewSelfServiceController 创建自助服务控制器
func NewSelfServiceController(passwordResetService *service.PasswordResetService) *SelfServiceController {
	return &SelfServiceController{
		passwordResetService: passwordResetService,
	}
}

// ForgotPassword godoc
// @Summary      忘记密码
// @Description  发送密码重置邮件
// @Tags         Public
// @Accept       json
// @Produce      json
// @Param        body  body  service.RequestPasswordResetRequest  true  "邮箱请求"
// @Success      200   {object}  response.Response  "邮件已发送"
// @Failure      400   {object}  response.Response  "参数错误"
// @Router       /api/public/forgot-password [post]
func (c *SelfServiceController) ForgotPassword(ctx *gin.Context) {
	var req service.RequestPasswordResetRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		response.Error(ctx, http.StatusBadRequest, response.ErrInvalidParams, err.Error())
		return
	}
	if err := c.passwordResetService.RequestPasswordReset(ctx.Request.Context(), &req); err != nil {
		logger.Ctx(ctx.Request.Context()).Error().Err(err).Str("email", req.Email).Msg("failed to process forgot password request")
		response.Error(ctx, http.StatusBadRequest, err.Error())
		return
	}
	response.Success(ctx, nil, "if the email exists, a reset link has been sent")
}

// ResetPassword godoc
// @Summary      重置密码
// @Description  使用令牌重置密码
// @Tags         Public
// @Accept       json
// @Produce      json
// @Param        body  body  service.ResetPasswordRequest  true  "重置请求"
// @Success      200   {object}  response.Response  "密码重置成功"
// @Failure      400   {object}  response.Response  "参数错误或令牌无效"
// @Router       /api/public/reset-password [post]
func (c *SelfServiceController) ResetPassword(ctx *gin.Context) {
	var req service.ResetPasswordRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		response.Error(ctx, http.StatusBadRequest, response.ErrInvalidParams, err.Error())
		return
	}
	if err := c.passwordResetService.ResetPassword(ctx.Request.Context(), &req); err != nil {
		// 路由已挂 BruteForceGuard("reset-password")：仅"令牌无效/过期"计入爆破计数；
		// 新密码不符合策略是持票用户自身错误，不参与 IP 锁定。
		if errors.Is(err, service.ErrInvalidResetToken) {
			middleware.RecordBruteForceFailure(ctx, "reset-password")
		}
		logger.Ctx(ctx.Request.Context()).Error().Err(err).Msg("failed to reset password")
		response.Error(ctx, http.StatusBadRequest, err.Error())
		return
	}
	middleware.ClearBruteForceFailure(ctx, "reset-password")
	response.Success(ctx, nil, "password has been reset successfully")
}
