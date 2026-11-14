package controller

// SPA/静态资源与 404/405 控制器（2026-11-05 二次审计：从 router 迁入）。

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/gin-gonic/gin"

	"hivemtk-user/internal/pkg/utils/response"
)

// StaticController 用户端 SPA 与 embed 落地页。
type StaticController struct {
	userWebDist string
}

func NewStaticController(userWebDist string) *StaticController {
	return &StaticController{userWebDist: userWebDist}
}

// EmbedPath GET /chat/embed/*path
func (s *StaticController) EmbedPath(c *gin.Context) {
	path := c.Param("path")
	if path != "" && !strings.HasPrefix(path, "#") {
		// embed=1 是**服务端能看见的**放行标记：302 之后浏览器加载的是
		// 路径 "/"（hash 只到客户端），而点击劫持护栏是挂在文档响应上的
		// ——不把这个标记带过去，浮标所在的第三方 iframe 会被 SAMEORIGIN 挡掉。
		c.Redirect(http.StatusFound, "/?embed=1#/chat/embed/"+strings.TrimPrefix(path, "/"))
		return
	}
	serveSPAWithFramePolicy(c, s.userWebDist, true)
}

// EmbedRoot GET /chat/embed
func (s *StaticController) EmbedRoot(c *gin.Context) {
	c.Redirect(http.StatusFound, "/?embed=1#/chat/embed/default")
}

// SPA GET /
func (s *StaticController) SPA(c *gin.Context) {
	serveSPAWithFramePolicy(c, s.userWebDist, c.Query("embed") == "1")
}

// Index GET /index.html
func (s *StaticController) Index(c *gin.Context) {
	serveSPAWithFramePolicy(c, s.userWebDist, c.Query("embed") == "1")
}

// NotFound NoRoute：API/文件/静态前缀回 JSON 404，其余回落 SPA。
func (s *StaticController) NotFound(c *gin.Context) {
	path := c.Request.URL.Path
	if strings.HasPrefix(path, "/api/") {
		response.Envelope(c, http.StatusNotFound, gin.H{"error": "not found"})
		return
	}
	if strings.HasPrefix(path, "/files/") {
		response.Envelope(c, http.StatusNotFound, gin.H{"error": "file not found", "path": path})
		return
	}
	if strings.HasPrefix(path, "/assets/") ||
		strings.HasPrefix(path, "/embed/") ||
		strings.HasPrefix(path, "/static/") {
		response.Envelope(c, http.StatusNotFound, gin.H{"error": "not found"})
		return
	}
	serveSPAWithFramePolicy(c, s.userWebDist, c.Query("embed") == "1")
}

// PWARootFiles NoRoute 兜底：PWA 五件套（/sw.js、/version.json、/manifest.webmanifest、
// /favicon.svg 与 /workbox-*.js 运行时 chunk）按 no-cache 直服，其余交回 SPA 回落。
//
// 背景（2026-11-13 O 轮）：这五个路径落 NoRoute 后被 SPA 回落成 index.html（200 HTML）
// ⇒ SW 注册 MIME 校验失败 ⇒ 整套 PWA 静默死亡。五件套统一 Cache-Control: no-cache，
// 与 nginx 静态站口径一致。Clean+Join 防目录穿越；文件不存在（未构建产物）则交回
// SPA 回落，保持既有行为不变。
// （架构审计轮：从 router 内联闭包迁入——[L2] 门禁禁 router 内联处理器。）
func (s *StaticController) PWARootFiles(c *gin.Context) {
	p := c.Request.URL.Path
	pwaRootFiles := map[string]bool{
		"/sw.js":                true,
		"/version.json":         true,
		"/manifest.webmanifest": true,
		"/favicon.svg":          true,
	}
	if !pwaRootFiles[p] &&
		!(strings.HasPrefix(p, "/workbox-") && strings.HasSuffix(p, ".js")) {
		s.NotFound(c)
		return
	}
	f := filepath.Join(s.userWebDist, filepath.Clean(p))
	if fi, err := os.Stat(f); err != nil || fi.IsDir() {
		s.NotFound(c)
		return
	}
	c.Header("Cache-Control", "no-cache")
	c.File(f)
}

// EmbedDistMissing user-web dist 路径配置了但不存在。
func (s *StaticController) EmbedDistMissing(c *gin.Context) {
	response.Envelope(c, http.StatusNotFound, gin.H{
		"error":   "user-web dist not found",
		"message": "请先构建 user-web: cd ../user-web && npm run build，并把 dist 放到能被 user-server 找到的位置。可通过环境变量 USER_WEB_DIST 指定。",
		"path":    c.Param("path"),
	})
}

// DistNotConfigured 未配置 user-web dist。
func (s *StaticController) DistNotConfigured(c *gin.Context) {
	response.Envelope(c, http.StatusNotFound, gin.H{
		"error": "user-web dist not configured",
	})
}

// MethodNotAllowed 405（r.NoMethod）。
func (s *StaticController) MethodNotAllowed(c *gin.Context) {
	response.Envelope(c, http.StatusMethodNotAllowed, gin.H{
		"code":    "METHOD_NOT_ALLOWED_405",
		"message": "请求方法不被支持",
		"method":  c.Request.Method,
		"path":    c.Request.URL.Path,
	})
}

func serveSPA(c *gin.Context, distDir string) {
	indexPath := filepath.Join(distDir, "index.html")
	if _, err := os.Stat(indexPath); err != nil {
		response.Envelope(c, http.StatusNotFound, gin.H{"error": "index.html not found", "path": indexPath})
		return
	}
	c.File(indexPath)
}

// serveSPAWithFramePolicy 交付 SPA 文档时下发点击劫持护栏（A13）。
//
// 为什么必须是**响应头**：`index.html` 的 meta CSP 里不能带 frame-ancestors
// （浏览器按规范忽略 meta 交付的这一条，见 user-web/index.html:20-26 的注释），
// 因此"meta 里写了"从不等于"有防护"。
//
// 两档策略 —— 只有客服浮标（/chat/embed 的落地页）需要对第三方放行：
//   - 普通页面：`X-Frame-Options: SAMEORIGIN` + `Content-Security-Policy: frame-ancestors 'self'`
//     （两条都发：XFO 给老浏览器，CSP 是现代实现；两者同时出现时浏览器取交集，
//     都要求同源，语义一致）；
//   - embeddable（`?embed=1`）：只发 `frame-ancestors *`，**不发 XFO**——
//     XFO 没有"全放行"的标准值（ALLOWALL 是 Chrome 私有），发了就等于把
//     /chat/embed 又关回去。
//
// `?embed=1` 由 /chat/embed 的 302 自己带上（见 EmbedPath）：那段 302 之后浏览器
// 加载的是路径 `/`，服务端看不到原始 hash ⇒ 不把标记带过去，浮标在第三方站点里
// 会被 SAMEORIGIN 挡掉。安全边界：任何页面都能用 `?embed=1` 嵌我们的 SPA。
// 接受这条的理由是 embed 模式本来就是公开可嵌的访客界面；要收得更紧，就得给
// embed 单独出一份 shell 文档（独立任务，属路由语义改动）。
func serveSPAWithFramePolicy(c *gin.Context, distDir string, embeddable bool) {
	if embeddable {
		c.Header("Content-Security-Policy", "frame-ancestors *")
	} else {
		c.Header("X-Frame-Options", "SAMEORIGIN")
		c.Header("Content-Security-Policy", "frame-ancestors 'self'")
	}
	serveSPA(c, distDir)
}
