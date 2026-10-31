package controller

import (
	"hivemtk-user/internal/config"
	"hivemtk-user/internal/pkg/utils/response"

	"github.com/gin-gonic/gin"
)

// AdminConfigController 管理员配置控制器
//
// 管理员配置只承载 UI 行为（登录页是否展示默认账号提示、自动登录开关等），
// 超管密码唯一来源是 system_users.password（bcrypt 哈希），由 InitAdmin 流程写入，
// 不暴露给任何 API。
type AdminConfigController struct{}

// NewAdminConfigController 创建管理员配置控制器
func NewAdminConfigController() *AdminConfigController {
	return &AdminConfigController{}
}

// @Summary 获取管理员配置
// @Tags      admin-config
// @Router    /api/admin/config [get]
// GetAdminConfig 获取管理员配置
//
// 返回非敏感的 UI 行为配置（前端用于登录页/自动登录的开关）。
// 不再返回任何密码字段。
func (c *AdminConfigController) GetAdminConfig(ctx *gin.Context) {
	adminConfig := config.GetAdminConfig()

	configInfo := map[string]any{
		"login": map[string]any{
			"show_default_credentials": adminConfig.Login.ShowDefaultCredentials,
			"default_credentials_hint": adminConfig.Login.DefaultCredentialsHint,
		},
		"auto_login": map[string]any{
			"enabled":           adminConfig.AutoLogin.Enabled,
			"use_default_admin": adminConfig.AutoLogin.UseDefaultAdmin,
		},
	}

	response.Success(ctx, configInfo, "获取管理员配置成功")
}
