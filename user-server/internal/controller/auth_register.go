package controller

import (
	"hivemtk-user/internal/pkg/utils/response"
	"hivemtk-user/internal/service"

	"github.com/gin-gonic/gin"
)

// auth_register.go 自助注册 + 公开站点配置
//
// 背景：注册 API 曾整体下线（authorization_routes_gone_test 钉死 404），
// enable_register 沦为无消费点的死配置。本文件把自助注册接回来：
//   - POST /api/auth/register —— 受 system_config.enable_register 开关 gate，
//     关闭时 403；走 BruteForceGuard 防灌号
//   - GET  /api/public/config —— 未登录可取的最小公开配置（站点名/是否开放注册），
//     前端据此渲染注册入口
//
// 服务层复用 AuthService.Register（角色 user、唯一性/强度校验、注册即签发 JWT）。

// PublicConfig 未登录可取的最小站点配置
//
// @Summary      公开站点配置
// @Description  仅暴露站点名与注册开关，不含任何敏感字段
// @Tags         Auth
// @Produce      json
// @Router       /api/public/config [get]
func (c *AuthController) PublicConfig(ctx *gin.Context) {
	cfg, err := service.NewSystemConfigService().GetConfig(ctx.Request.Context())
	if err != nil {
		// 读失败按保守值返回：站点名空、注册关闭——前端隐藏入口而非误开放
		response.Success(ctx, gin.H{"site_name": "", "enable_register": false}, "ok")
		return
	}
	response.Success(ctx, gin.H{
		"site_name":       cfg.Name,
		"enable_register": cfg.EnableRegister,
	}, "ok")
}
