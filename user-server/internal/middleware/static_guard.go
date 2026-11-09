package middleware

// 静态构建产物托管守卫（A-18 收口，2026-11-09；F-4-2 修正拦截清单）。
//
// 背景：/assets（user-web/dist/assets）与 /embed（embed-sdk/dist）此前用裸 r.Static
// 发布，与 /files/* 的 FilesGuard 相比缺"危险扩展名 403 + nosniff/sandbox"两层防护。
// 定性：两目录都是**构建产物**而非用户上传目录，内容不可被请求者注入，非 XSS 上传链；
// 对齐基线防的是"部署目录被篡改/未来部署形态变化（如把可写目录也指过来）"这一纵深，
// 成本一行装配，故统一。
//
// 与 FilesGuard 的差异：
//  1. **没有签名凭证层**——SPA 资产与 embed SDK 本来就是匿名公开面（浏览器直接
//     <script src> 引用，加签名会打断加载），防护只保留 FilesGuard 的第 1、2 层。
//  2. **拒绝清单不同**：不能复用上传面的 storage.dangerousWebExts——那份清单把
//     .js/.mjs/.svg 判为危险（针对"用户上传后被同源执行"），而 /assets 下的 .js
//     正是 SPA 的运行产物、/embed 全部是 .js。实测 dist/assets 有 408 个 .js，
//     按上传口径拦截会让生产前端整站 403（F-4-2，前轮 ego 走查跑的是 vite dev
//     未覆盖 dist 发布面，故未暴露）。构建产物面只拒绝"构建/部署流程不会产出、
//     出现即视为目录被篡改"的服务端可执行与 HTML 文档类扩展名；.js/.mjs/.css/
//     .svg 等正常产物放行，由 nosniff + CSP sandbox 兜底。

import (
	"net/http"
	"path/filepath"
	"strings"

	"github.com/gin-gonic/gin"
)

// buildAssetDeniedExts 构建产物目录中不应出现、出现即 403 的扩展名。
// 刻意不含 .js/.mjs/.css/.svg：它们是合法构建产物。
var buildAssetDeniedExts = map[string]bool{
	// 服务端可执行（部署目录被错配到 CGI/PHP 等处理器时的执行面）
	".php": true, ".php3": true, ".php4": true, ".php5": true, ".phtml": true,
	".jsp": true, ".jspx": true, ".asp": true, ".aspx": true,
	".cgi": true, ".pl": true, ".py": true, ".sh": true, ".bash": true,
	".exe": true, ".dll": true, ".bat": true, ".cmd": true, ".ps1": true,
	// HTML 文档（同源打开即执行脚本；构建产物不应把文档放进 assets 目录）
	".html": true, ".htm": true, ".htmls": true, ".xhtml": true, ".xht": true,
	// 服务器配置旁路
	".htaccess": true,
}

// StaticGuard 发布静态构建产物目录：禁用扩展名 403 + nosniff + CSP sandbox。
// rel param 与 r.Static 的 ":*filepath" 参数名对应（gin 通配参数默认名 filepath）。
// 调用方应同时注册 GET 与 HEAD（r.Static 原本两者都注册，F-4-3）。
func StaticGuard(baseDir string) gin.HandlerFunc {
	base := filepath.Clean(baseDir)
	return func(c *gin.Context) {
		rel := c.Param("filepath")
		if buildAssetDeniedExts[strings.ToLower(filepath.Ext(rel))] {
			c.JSON(http.StatusForbidden, gin.H{
				"error": "file type not allowed to serve",
			})
			return
		}
		c.Header("X-Content-Type-Options", "nosniff")
		c.Header("Content-Security-Policy", "sandbox")
		c.File(filepath.Join(base, filepath.Clean("/"+rel)))
	}
}
