package router

import (
	"net/http"
	"os"
	"path/filepath"

	"hivemtk-user/internal/pkg/utils/logger"
	"hivemtk-user/internal/storage"

	"github.com/gin-gonic/gin"
)

// RegisterFilesRoute 把本地上传目录挂到 /files/*。
//
// 抽成独立函数是为了让它可测："托管哪一棵树"这件事单独就能错，而 Setup 要拉起整个应用
// （见 TestRegisterFilesRouteServesTheUploadWritersRoot；装配腿由 TestSetupRegistersFilesRoute 守着）。
//
// 托管根取自 storage.LocalSource() ——与上传写入方（controller/upload.go）、邮件外发侧三方
// 同一份解析，兜底链 STORAGE_LOCAL_BASE_DIR → ./uploads。此前这里自己读一遍 STORAGE_LOCAL_BASE_DIR，
// 而上传侧的链中间还夹着一档 UPLOAD_DIR ⇒ "只配 UPLOAD_DIR"的部署把文件写进 $UPLOAD_DIR、
// 却从 ./uploads 对外公开，公开链接逐个 404（第三十八轮把那一档连同这里的手抄一起退役）。
// 取第一个返回值：第三个（附件子目录）与第二个（公网前缀）都不是本路由的坐标系，
// 路由路径按 /files/* 硬编码，改 STORAGE_LOCAL_PUBLIC_URL 只会改外链，不会改这里挂的路径。
//
// /files 走守卫版（同源可执行扩展名 403 + nosniff/sandbox）而非裸 r.Static，
// 堵素材库/渠道媒体任意扩展名落盘后的同源直出。
func RegisterFilesRoute(r *gin.Engine) {
	uploadDir, _, _ := storage.LocalSource()
	_ = os.MkdirAll(uploadDir, 0o750)
	r.GET("/files/*filepath", serveUploadsGuarded(uploadDir))
	logger.Infof("[Router] static file server registered (guarded): /files -> %s", uploadDir)
}

// serveUploadsGuarded 以 r.Static 的替代实现发布本地上传目录（/files/*），
// 叠加两层防护：
//
//  1. 命中"同源可执行/可渲染"扩展名清单（storage.IsDangerousWebExt，与落盘
//     中和层同一清单）→ 403。落盘层（LocalDriver.generatePath）已把新文件
//     中和为 .bin，此处兜底**历史已落盘**的 .html/.svg 与绕过驱动直接放入
//     目录的文件。
//  2. X-Content-Type-Options: nosniff + Content-Security-Policy: sandbox，
//     封掉旧浏览器内容嗅探与万一命中文档类型的脚本执行/同源能力。
//
// 路径处理：c.Param 取到的相对段先 filepath.Clean("/"+rel) 归一（消化 ..
// 与多重斜杠）再 Join 到 baseDir；http.ServeFile 对含 ".." 的请求段自带二次
// 拒绝——双保险防穿越，与第七轮 platform 存储驱动的修法同构。
func serveUploadsGuarded(baseDir string) gin.HandlerFunc {
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
		c.Header("X-Content-Type-Options", "nosniff")
		c.Header("Content-Security-Policy", "sandbox")
		c.File(filepath.Join(base, filepath.Clean("/"+rel)))
	}
}
