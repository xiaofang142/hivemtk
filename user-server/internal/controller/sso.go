package controller

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"hivemtk-user/internal/config"
	"hivemtk-user/internal/pkg/sso"
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
type SSOController struct {
	ssoService *service.SSOService
	frontend   string
}

// NewSSOController 创建 SSO 控制器（生产入口，注入已构建的 SSO 服务）
func NewSSOController(ssoService *service.SSOService) *SSOController {
	return &SSOController{
		ssoService: ssoService,
		frontend:   config.GetPublicBaseURL(),
	}
}

// ListProviders 列出已启用的 SSO 登录方式
func (c *SSOController) ListProviders(ctx *gin.Context) {
	if !c.ssoService.Enabled() {
		response.Success(ctx, gin.H{"enabled": false, "providers": []service.ProviderInfo{}}, "未启用企业登录")
		return
	}
	response.Success(ctx, gin.H{
		"enabled":   true,
		"providers": c.ssoService.ListProviders(),
	}, "获取登录方式成功")
}

// Login 发起 SSO 登录：生成 state + nonce + PKCE verifier，重定向到 IdP
func (c *SSOController) Login(ctx *gin.Context) {
	if !c.ssoService.Enabled() {
		response.Error(ctx, http.StatusForbidden, service.ErrSSONotEnabled.Error())
		return
	}
	provider := ctx.Param("provider")
	adapter, ok := c.ssoService.Adapter(provider)
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

	oidc := adapter.OIDC()
	if err := oidc.EnsureFresh(ctx.Request.Context()); err != nil {
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

	result, err := c.ssoService.HandleCallback(ctx.Request.Context(), provider, ctx.Query("code"), verifier)
	clearSSOCookies(ctx)
	if err != nil {
		c.callbackError(ctx, err)
		return
	}

	if ctx.Query("format") != "json" {
		target := c.resolveRedirectTarget(ctx)
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

func (c *SSOController) resolveRedirectTarget(ctx *gin.Context) string {
	if r := strings.TrimSpace(ctx.Query("redirect")); r != "" {
		if strings.HasPrefix(r, "/") && !strings.HasPrefix(r, "//") {
			return r
		}
		return ""
	}
	return strings.TrimRight(c.frontend, "/")
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
}
