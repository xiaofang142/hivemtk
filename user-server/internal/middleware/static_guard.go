package middleware

// 静态构建产物托管守卫（A-18 收口，2026-11-09）。
//
// 背景：/assets（user-web/dist/assets）与 /embed（embed-sdk/dist）此前用裸 r.Static
// 发布，与 /files/* 的 FilesGuard 相比缺"危险扩展名 403 + nosniff/sandbox"两层防护。
// 定性：两目录都是**构建产物**而非用户上传目录，内容不可被请求者注入，非 XSS 上传链；
// 对齐基线防的是"部署目录被篡改/未来部署形态变化（如把可写目录也指过来）"这一纵深，
// 成本一行装配，故统一。
//
// 与 FilesGuard 的差异：**没有签名凭证层**——SPA 资产与 embed SDK 本来就是匿名
// 公开面（浏览器直接 <script src> 引用，加签名会打断加载），防护只保留
// FilesGuard 的第 1、2 层。且参数名不同：r.Static 的通配是 *filepath，与本守卫一致。

import (
	"net/http"
	"path/filepath"

	"github.com/gin-gonic/gin"

	"hivemtk-user/internal/storage"
)

// StaticGuard 发布静态构建产物目录：危险扩展名 403 + nosniff + CSP sandbox。
// rel param 与 r.Static 的 ":*filepath" 参数名对应（gin 通配参数默认名 filepath）。
func StaticGuard(baseDir string) gin.HandlerFunc {
	base := filepath.Clean(baseDir)
	return func(c *gin.Context) {
		rel := c.Param("filepath")
		if storage.IsDangerousWebExt(filepath.Ext(rel)) {
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
