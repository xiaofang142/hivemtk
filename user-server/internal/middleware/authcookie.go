package middleware

// HttpOnly 认证 Cookie（2026-11-17 审计 P2-7 收口）。
//
// 背景：四个前端一直把 JWT 存 localStorage，任意 XSS 即可窃取 24h 令牌。
// 缓解面已有（全站 DOMPurify、CSP 无 unsafe-eval、URL 传 token 面本轮清零），
// 但持久化面只有 HttpOnly Cookie 能真正关死：JS 读不到，XSS 只能"借用会话"，
// 不能偷走令牌离线重放。
//
// 口径：
//   - 双通道兼容：Authorization 头优先（桥接扩展/脚本客户端不动的唯一原因），
//     缺头时回落 Cookie——同源 Web 端在无头场景（刷新页面后内存令牌丢失）由
//     Cookie 无感续期，脚本客户端不感知。
//   - SameSite=Strict：API 与前端同源部署（反代同域），Strict 不影响同源请求；
//     跨站请求一律不携带，CSRF 面同步收窄。
//   - Secure：默认 false——生产链路是 FRP/反代 TLS 卸载（后端 HTTP，Request.TLS
//     恒为 nil），写死 true 会直接断掉生产登录。需要端到端 HTTPS 的部署显式设
//     AUTH_COOKIE_SECURE=1。
//   - Max-Age 与 JWT TTL 对齐（86400，代码常量，无 env 开关——见 jwt.go 头注）。
//   - 值为完整 JWT，进出都走 ParseToken/黑名单/吊销水位线，与头通道同一套校验，
//     不新增信任面。

import (
	"net/http"
	"os"

	"github.com/gin-gonic/gin"
)

// AuthCookieName 认证 Cookie 名。改名=全站登出，轻易别动。
const AuthCookieName = "hmtk_token"

// authCookieMaxAge 与 JWT 有效期（jwt.go 的 24h 硬编码）保持一致：86400 秒。
const authCookieMaxAge = 86400

func authCookieSecure() bool {
	return os.Getenv("AUTH_COOKIE_SECURE") == "1"
}

// SetAuthCookie 登录/换发成功后写入 HttpOnly 会话 Cookie。
func SetAuthCookie(c *gin.Context, token string) {
	http.SetCookie(c.Writer, &http.Cookie{
		Name:     AuthCookieName,
		Value:    token,
		Path:     "/",
		MaxAge:   authCookieMaxAge,
		HttpOnly: true,
		Secure:   authCookieSecure(),
		SameSite: http.SameSiteStrictMode,
	})
}

// ClearAuthCookie 登出时撤销 Cookie（MaxAge=-1 即删除）。
func ClearAuthCookie(c *gin.Context) {
	http.SetCookie(c.Writer, &http.Cookie{
		Name:     AuthCookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   authCookieSecure(),
		SameSite: http.SameSiteStrictMode,
	})
}

// AuthCookieToken 从 Cookie 取认证令牌；无 Cookie 或值为空返回空串。
func AuthCookieToken(c *gin.Context) string {
	v, err := c.Cookie(AuthCookieName)
	if err != nil {
		return ""
	}
	return v
}
