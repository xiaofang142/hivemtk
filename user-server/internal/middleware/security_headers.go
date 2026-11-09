package middleware

import "github.com/gin-gonic/gin"

// SecurityHeaders 全局安全响应头（AUD-08 审计整改）：
//
//   - CSP 取 default-src 'self' 兜底，script/style/img/connect 各显式收紧；
//     /files 守卫（files.go）与静态产物守卫（static_guard.go）的 CSP sandbox、
//     embed 面板路由（controller/static.go）的 frame-ancestors 均在 handler 内
//     用 c.Header（gin Set 语义）覆盖本值，中间件先于 handler 执行，更严/专用
//     语义不变；
//   - 不设 X-Frame-Options：embed-sdk 面板走 controller/static.go 的
//     frame-ancestors * 分支（该分支不设 XFO），全局 XFO=SAMEORIGIN 会误伤
//     跨域嵌入；现代浏览器以 CSP frame-ancestors 为准，XFO 缺省不构成敞口；
//   - Referrer-Policy 取 strict-origin-when-cross-origin（不泄漏全路径给外域）。
func SecurityHeaders() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("Content-Security-Policy",
			"default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; "+
				"img-src 'self' data:; connect-src 'self'; frame-ancestors 'self'; "+
				"base-uri 'self'; form-action 'self'")
		c.Header("X-Content-Type-Options", "nosniff")
		c.Header("Referrer-Policy", "strict-origin-when-cross-origin")
		c.Next()
	}
}
