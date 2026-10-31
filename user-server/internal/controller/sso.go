package controller

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"hivemtk-user/internal/config"
	"hivemtk-user/internal/pkg/sso"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/pkg/utils/response"
	"hivemtk-user/internal/service"

	"github.com/gin-gonic/gin"
)

// SSOCookieTTL state/nonce/verifier cookie 有效期（5 分钟，与 sso 包默认一致）
//
// 它同时是参数中心 `misc.sso_cookie_ttl` 的代码兜底值，两者必须一致。
const SSOCookieTTL = 5 * time.Minute

// ssoCookieTTLProvider 由装配层注入；未注入时回落 SSOCookieTTL。
var ssoCookieTTLProvider = func() time.Duration { return SSOCookieTTL }

// getSSOCookieTTL 返回当前生效的 SSO cookie 有效期。
func getSSOCookieTTL() time.Duration { return ssoCookieTTLProvider() }

// SetSSOCookieTTLProvider 注入读取函数（装配层调用；测试可注入桩）。nil 视为不注入。
func SetSSOCookieTTLProvider(fn func() time.Duration) {
	if fn != nil {
		ssoCookieTTLProvider = fn
	}
}

// SSOController 企业 SSO 登录控制器
//
// 没有 frontend 字段：回调后要跳的地址在 SaaS 下是**每个租户各自的公网基址**
// （urls.public_base_url），构造期拍平成一份会让 B 租户的用户被 302 到 A 的站点。
type SSOController struct {
	ssoService *service.SSOService
}

// NewSSOController 创建 SSO 控制器（生产入口，注入已构建的 SSO 服务）
func NewSSOController(ssoService *service.SSOService) *SSOController {
	return &SSOController{ssoService: ssoService}
}

// providerScope 匿名入口按 tenant_code 选出租户作用域。
//
// 解不出来（没带码、码不存在）时用未绑定租户的 ctx，读到的是平台 config.yaml
// 那一档 IdP —— 登录页这时展示平台统一入口，而不是报错：带错短码的人多半还在
// 输租户名，让页面白掉只会把一个拼写问题变成一次故障报修。
func (c *SSOController) providerScope(ctx *gin.Context) context.Context {
	code := strings.TrimSpace(ctx.Query("tenant_code"))
	if code == "" {
		code = strings.TrimSpace(ctx.GetHeader("X-Tenant-Code"))
	}
	if code == "" {
		return ctx.Request.Context()
	}
	bound, err := resolvePublicTenantCtx(ctx, code)
	if err != nil {
		return ctx.Request.Context()
	}
	return bound
}

// @Summary      未启用企业登录
// @Tags      s-s-o
// @Router    /api/sso/providers [get]
// ListProviders 列出该租户已启用的 SSO 登录方式
func (c *SSOController) ListProviders(ctx *gin.Context) {
	scope := c.providerScope(ctx)
	if !c.ssoService.EnabledFor(scope) {
		response.Success(ctx, gin.H{"enabled": false, "providers": []service.ProviderInfo{}}, "未启用企业登录")
		return
	}
	response.Success(ctx, gin.H{
		"enabled":   true,
		"providers": c.ssoService.ListProvidersFor(scope),
	}, "获取登录方式成功")
}

// @Summary      发起 SSO 登录：生成 state + nonce + PKCE verifier，重定向到 IdP
// @Tags      s-s-o
// @Router    /api/sso/login/{provider} [get]
// Login 发起 SSO 登录：生成 state + nonce + PKCE verifier，重定向到 IdP
func (c *SSOController) Login(ctx *gin.Context) {
	provider := ctx.Param("provider")

	// 先定租户再取适配器：IdP 配置在 SaaS 下是按租户解析的，反过来写会用平台那一份
	// 生成授权请求、却用租户那一份校验回调，两边 issuer/client_id 不同 → 每次登录
	// 都在 token 交换处失败。
	tenantCode := strings.TrimSpace(ctx.Query("tenant_code"))
	requestCtx := ctx.Request.Context()
	if tenant.IsMultiTenant() {
		if tenantCode == "" {
			response.Error(ctx, http.StatusBadRequest, "多租户模式需携带 tenant_code")
			return
		}
		bound, err := resolvePublicTenantCtx(ctx, tenantCode)
		if err != nil {
			response.Error(ctx, http.StatusBadRequest, "无法确定租户："+err.Error())
			return
		}
		requestCtx = bound
	}
	if !c.ssoService.EnabledFor(requestCtx) {
		response.Error(ctx, http.StatusForbidden, service.ErrSSONotEnabled.Error())
		return
	}
	adapter, ok := c.ssoService.AdapterFor(requestCtx, provider)
	if !ok {
		response.Error(ctx, http.StatusNotFound, "未找到该登录方式")
		return
	}

	state := sso.RandString(32)
	nonce := sso.RandString(32)
	verifier := sso.RandString(64)

	setSSOCookie(ctx, "sso_state", state)
	setSSOCookie(ctx, "sso_nonce", nonce)
	setSSOCookie(ctx, "sso_verifier", verifier)
	if tenantCode != "" {
		setSSOCookie(ctx, "sso_tenant", tenantCode)
	}

	oidc := adapter.OIDC()
	if err := oidc.EnsureFresh(requestCtx); err != nil {
		clearSSOCookies(ctx)
		response.Error(ctx, http.StatusInternalServerError, "获取授权配置失败: "+err.Error())
		return
	}
	authURL, err := oidc.BuildAuthURL(state, nonce, verifier)
	if err != nil {
		clearSSOCookies(ctx)
		response.Error(ctx, http.StatusInternalServerError, "构建授权地址失败: "+err.Error())
		return
	}
	ctx.Redirect(http.StatusFound, authURL)
}

// @Summary 处理 IdP 回调
// @Tags      s-s-o
// @Router    /api/sso/callback/{provider} [get]
// Callback 处理 IdP 回调
func (c *SSOController) Callback(ctx *gin.Context) {
	provider := ctx.Param("provider")

	state := ctx.Query("state")
	expectedState, errState := ctx.Cookie("sso_state")
	if errState != nil || state == "" || state != expectedState {
		response.Error(ctx, http.StatusBadRequest, service.ErrSSOInvalidState.Error())
		return
	}

	verifier, _ := ctx.Cookie("sso_verifier")

	requestCtx := ctx.Request.Context()
	if tenant.IsMultiTenant() {
		tenantCode := strings.TrimSpace(ctx.Query("tenant_code"))
		if tenantCode == "" {
			tenantCode, _ = ctx.Cookie("sso_tenant")
		}
		bound, err := resolvePublicTenantCtx(ctx, tenantCode)
		if err != nil {
			clearSSOCookies(ctx)
			response.Error(ctx, http.StatusBadRequest, "无法确定租户："+err.Error())
			return
		}
		requestCtx = bound
	}

	result, err := c.ssoService.HandleCallback(requestCtx, provider, ctx.Query("code"), verifier)
	clearSSOCookies(ctx)
	if err != nil {
		c.callbackError(ctx, err)
		return
	}

	if ctx.Query("format") != "json" {
		target := c.resolveRedirectTarget(ctx, requestCtx)
		if target != "" {
			sep := "?"
			if strings.Contains(target, "?") {
				sep = "&"
			}
			ctx.Redirect(http.StatusFound, fmt.Sprintf("%s%stoken=%s", target, sep, url.QueryEscape(result.Token)))
			return
		}
	}

	response.Success(ctx, gin.H{
		"token":       result.Token,
		"user":        result.User,
		"expires":     result.Expires,
		"provider":    result.Provider,
		"is_new_user": result.IsNewUser,
	}, "登录成功")
}

// resolveRedirectTarget 回调成功后把用户送去哪儿。
//
// ?redirect= 只接受站内相对路径（`/x` 但不是 `//x`，后者是协议相对地址，会把用户
// 甩到外部域）；没带 redirect 时落到该租户的公网基址——基址按 requestCtx 解析，
// 那里已经绑好发起方租户。
func (c *SSOController) resolveRedirectTarget(ctx *gin.Context, requestCtx context.Context) string {
	if r := strings.TrimSpace(ctx.Query("redirect")); r != "" {
		if strings.HasPrefix(r, "/") && !strings.HasPrefix(r, "//") {
			return r
		}
		return ""
	}
	return strings.TrimRight(config.GetPublicBaseURLFor(requestCtx), "/")
}

// @Summary      回显本租户自配的 IdP（client_secret 打码）
// @Tags      s-s-o
// @Router    /api/sso/config [get]
// GetTenantProviders 回显本租户自配的 IdP（GET /api/sso/config）
//
// client_secret 一律打码成 ***：把密钥渲染进 input 等于把它交给浏览器历史与
// 前端日志。前端要区分"没填"与"填过看不到"，看的就是这个标记位。
func (c *SSOController) GetTenantProviders(ctx *gin.Context) {
	providers, err := c.ssoService.TenantProviders(ctx.Request.Context())
	if err != nil {
		response.ErrorFromDB(ctx, err, "读取租户企业登录配置失败", err.Error())
		return
	}
	response.Success(ctx, gin.H{"providers": service.MaskTenantSSOClientSecrets(providers)}, "获取成功")
}

// @Summary      保存本租户自配的 IdP（*** 沿用旧密钥）
// @Tags      s-s-o
// @Router    /api/sso/config [put]
// SaveTenantProviders 保存本租户自配的 IdP（PUT /api/sso/config）
//
// 提交里 client_secret 为 *** 的那条沿用库里那份（回显打码的必然结果），
// 空串才是"清掉密钥"。少了这一步，运维改一个无关字段就会把密钥写成字面量 ***。
func (c *SSOController) SaveTenantProviders(ctx *gin.Context) {
	var body struct {
		Providers []service.TenantSSOProvider `json:"providers"`
	}
	if err := ctx.ShouldBindJSON(&body); err != nil {
		response.Error(ctx, http.StatusBadRequest, "参数错误: "+err.Error())
		return
	}
	requestCtx := ctx.Request.Context()
	existing, err := c.ssoService.TenantProviders(requestCtx)
	if err != nil {
		response.ErrorFromDB(ctx, err, "读取租户企业登录配置失败", err.Error())
		return
	}
	merged, err := service.PreserveMaskedClientSecrets(body.Providers, existing)
	if err != nil {
		response.Error(ctx, http.StatusBadRequest, err.Error())
		return
	}
	if err := c.ssoService.SaveTenantProviders(requestCtx, merged); err != nil {
		response.Error(ctx, http.StatusBadRequest, "配置不合法: "+err.Error())
		return
	}
	response.Success(ctx, gin.H{"providers": service.MaskTenantSSOClientSecrets(merged)}, "保存成功")
}

func (c *SSOController) callbackError(ctx *gin.Context, err error) {
	switch {
	case errors.Is(err, service.ErrSSONotEnabled):
		response.Error(ctx, http.StatusForbidden, err.Error())
	case errors.Is(err, service.ErrSSOProviderNotFound):
		response.Error(ctx, http.StatusNotFound, err.Error())
	case errors.Is(err, service.ErrSSOMissingCode):
		response.Error(ctx, http.StatusBadRequest, err.Error())
	case errors.Is(err, service.ErrSSOUserNotBound):
		response.Error(ctx, http.StatusForbidden, err.Error())
	case errors.Is(err, service.ErrSSOUserDisabled):
		response.Error(ctx, http.StatusForbidden, err.Error())
	case errors.Is(err, service.ErrSSOInvalidState):
		response.Error(ctx, http.StatusBadRequest, err.Error())
	default:
		response.Error(ctx, http.StatusUnauthorized, "企业登录失败: "+err.Error())
	}
}

func setSSOCookie(ctx *gin.Context, name, value string) {
	ctx.SetCookie(name, value, int(getSSOCookieTTL().Seconds()), "/", "", true, true)
}

func clearSSOCookies(ctx *gin.Context) {
	ctx.SetCookie("sso_state", "", -1, "/", "", true, true)
	ctx.SetCookie("sso_nonce", "", -1, "/", "", true, true)
	ctx.SetCookie("sso_verifier", "", -1, "/", "", true, true)
	ctx.SetCookie("sso_tenant", "", -1, "/", "", true, true)
}
