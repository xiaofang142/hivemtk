package controller

import (
	"context"
	"os"
	"strings"

	"hivemtk-user/internal/config"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tenant"
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

// EnvSiteTenantCode 站点默认租户短码。
//
// 语义：匿名访问公开页面（登录页的站点名、是否开放注册）时，站点自己的归属租户
// 是谁。SaaS 部署下每个站点由一个商户持有（官网域名由「示例科技有限公司」
// 持有，code=tz3rmdj），公开页要读的就是**它的**站点名与注册开关。
const EnvSiteTenantCode = "SITE_TENANT_CODE"

// resolvePublicTenantCtx 公开入口的租户归属：站点配置与注册开关都是租户数据，
// 匿名请求没有 JWT 可依，只能由请求显式携带短码确定。
//
//   - off 模式：显式绑定默认租户，语义与旧版本等价；
//   - on 模式：`tenant_code`（query/body）或 X-Tenant-Code 头解析租户并钉死
//     绑定。解析不出来就返回错误——绝不在匿名请求上回落到默认租户，
//     否则 S 租户的公开页会读到默认租户的站点名/注册开关。
//
// 站点兜底（EnvSiteTenantCode）：请求**没带**短码时，用部署期配置的站点短码。
//
// 为什么需要这一层：注册是**新商户自助开户**（POST /api/tenant/register），
// 开户时人还没有任何租户短码 —— 让"注册入口可见"依赖"已有租户短码"是死结：
// 匿名访客在登录页既没有短码也没有 JWT，公开配置只能回保守值
// enable_register=false ⇒ 前端隐藏注册入口 ⇒ 线上 SaaS 登录页看不到注册链接
// （2026-10-22 线上实测）。
//
// 边界：兜底**只在请求没给短码时**生效。显式给了短码就以请求为准 ——
// S 租户看到的必须是 S 租户的站点名与注册开关，不能被站点兜底覆盖。
// 站点短码配错（查不到租户）时按保守值回，注册入口不假开。
func resolvePublicTenantCtx(c *gin.Context, code string) (context.Context, error) {
	rctx := c.Request.Context()
	if !tenant.IsMultiTenant() {
		return tenant.ContextWithDefault(rctx), nil
	}
	code = strings.TrimSpace(code)
	if code == "" {
		code = strings.TrimSpace(c.GetHeader("X-Tenant-Code"))
	}
	if code == "" {
		code = strings.TrimSpace(os.Getenv(EnvSiteTenantCode))
	}
	id, err := service.NewTenantService().ResolvePublicTenantID(rctx, code)
	if err != nil {
		return rctx, err
	}
	return tenant.OnlyTenant(rctx, id), nil
}

// PublicConfig 未登录可取的最小站点配置
//
// @Summary      公开站点配置
// @Description  仅暴露站点名、注册开关、租户模式与云端版入口，不含任何敏感字段
// @Tags         Auth
// @Produce      json
// @Router       /api/public/config [get]
func (c *AuthController) PublicConfig(ctx *gin.Context) {
	// tenant_mode 与系统配置无关，两个分支都带上：前端据此决定是否渲染
	// 租户短码登录框（on 模式才显示），且读配置失败也不影响该判定。
	tenantMode := tenant.IsMultiTenant()
	// edition/cloud_url 同理（003 T003）：edition 是 TENANT_MODE 的语义映射
	// （前端不该猜开关名）；cloud_url 任何形态都要给可渲染值（离线回落占位常量）。
	edition := "opensource"
	if tenantMode {
		edition = "saas"
	}
	// reg_mode 是 TENANT_MODE 的**注册口径**映射（003 T012）：与 edition 同源、
	// 但回答的是另一个问题——"这个实例允许怎么开户"。缺这个键时
	// scripts/api_verify_full.py 的 B1.3 会红，而前端只能用 tenant_mode 布尔值
	// 自己推断（推断口径一旦与后端不一致，就会在云端实例上渲染出"离线部署"文案）。
	regMode := model.TenantRegModeOffline
	if tenantMode {
		regMode = model.TenantRegModeSaaS
	}
	requestCtx, err := resolvePublicTenantCtx(ctx, ctx.Query("tenant_code"))
	if err != nil {
		// 租户未知（on 模式未带短码）按保守值返回：站点名空、注册关闭——
		// 前端隐藏入口而非误开放，也绝不拿默认租户的配置冒充本租户。
		response.Success(ctx, gin.H{
			"site_name": "", "enable_register": false, "tenant_mode": tenantMode,
			"edition": edition, "reg_mode": regMode, "cloud_url": config.CloudURLFor(requestCtx),
		}, "ok")
		return
	}
	cfg, err := service.NewSystemConfigService().GetConfig(requestCtx)
	if err != nil {
		// 读失败按保守值返回：站点名空、注册关闭——前端隐藏入口而非误开放
		response.Success(ctx, gin.H{
			"site_name": "", "enable_register": false, "tenant_mode": tenantMode,
			"edition": edition, "reg_mode": regMode, "cloud_url": config.CloudURLFor(requestCtx),
		}, "ok")
		return
	}
	response.Success(ctx, gin.H{
		"site_name":       cfg.Name,
		"enable_register": cfg.EnableRegister,
		"tenant_mode":     tenantMode,
		"edition":         edition,
		"reg_mode":        regMode,
		"cloud_url":       config.CloudURLFor(requestCtx),
	}, "ok")
}
