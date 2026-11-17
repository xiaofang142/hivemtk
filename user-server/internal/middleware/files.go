package middleware

// /files/* 的守卫处理器（2026-11-05 二次审计：从 router 迁入中间件层）。
// 三层防护：危险扩展名 403 + nosniff/sandbox + 签名凭证（on 模式）。

import (
	"net/http"
	"path/filepath"
	"time"

	"github.com/gin-gonic/gin"

	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/pkg/utils/logger"
	"hivemtk-user/internal/storage"
)

// FilesGuard 以 r.Static 的替代实现发布本地上传目录（/files/*），叠加三层防护：
//
//  1. 命中"同源可执行/可渲染"扩展名清单（storage.IsDangerousWebExt，与落盘
//     中和层同一清单）→ 403。落盘层（LocalDriver.generatePath）已把新文件
//     中和为 .bin，此处兜底**历史已落盘**的 .html/.svg 与绕过驱动直接放入
//     目录的文件。
//  2. X-Content-Type-Options: nosniff + Content-Security-Policy: sandbox，
//     封掉旧浏览器内容嗅探与万一命中文档类型的脚本执行/同源能力。
//  3. 访问凭证（SaaS 审计 A1 / A12，2026-10-23）：
//     on 模式下，租户归属与过期时间只写在 `?s=` 的签名 token 里，**路径里没有租户段**。
//     验不过（缺失/篡改/过期）一律 404。
//
// 路径处理：c.Param 取到的相对段先 filepath.Clean("/"+rel) 归一（消化 ..
// 与多重斜杠）再 Join 到 baseDir；http.ServeFile 对含 ".." 的请求段自带二次
// 拒绝——双保险防穿越，与第七轮 platform 存储驱动的修法同构。
func FilesGuard(baseDir string) gin.HandlerFunc {
	base := filepath.Clean(baseDir)
	return func(c *gin.Context) {
		rel := c.Param("filepath")
		if storage.IsDangerousWebExt(filepath.Ext(rel)) {
			c.JSON(http.StatusForbidden, gin.H{
				"error": "file type not allowed to serve",
				"path":  rel,
			})
			return
		}

		// off 模式（2026-11-17 审计 P3-2）：URL 由 PublicURL 签发 ?s=（默认租户归属）。
		//   - 带 ?s= ：验签，失败一律 404（与 on 模式同口径）
		//   - 无 ?s= ：存量库里的历史无签名 URL——FILES_OFF_MODE_REQUIRE_SIGN=1 时 404
		//     收口；默认放行但逐次留匿名访问审计，供消化存量后开闸。
		if !tenant.IsMultiTenant() {
			serveRel := filepath.Clean("/" + rel)
			if sig := c.Query("s"); sig != "" {
				if _, _, err := storage.VerifyFileURL(serveRel, sig, time.Now()); err != nil {
					c.JSON(http.StatusNotFound, gin.H{"error": "file not found"})
					return
				}
				serveFile(c, base, serveRel)
				return
			}
			if storage.OffModeFilesRequireSign() {
				c.JSON(http.StatusNotFound, gin.H{"error": "file not found"})
				return
			}
			logger.Warnf("[FILES] off-mode anonymous access (unsigned url): %s ip=%s", serveRel, c.ClientIP())
			serveFile(c, base, serveRel)
			return
		}

		// on 模式：签名是**唯一**凭证。三种失败一律 404（不做"验不过就放行"的分支，
		// 那正是 A1 要清掉的形状；也不用 403 —— 避免变成存在性预言机）。
		tenantID, path, err := storage.VerifyFileURL(rel, c.Query("s"), time.Now())
		if err != nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "file not found"})
			return
		}
		serveFile(c, base, filepath.Join(storage.TenantDir(tenantID), filepath.Clean("/"+path)))
	}
}

func serveFile(c *gin.Context, base, rel string) {
	c.Header("X-Content-Type-Options", "nosniff")
	c.Header("Content-Security-Policy", "sandbox")
	c.File(filepath.Join(base, rel))
}
