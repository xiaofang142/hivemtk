package router

// /files/* 路由装配（2026-11-05 二次审计：处理器已迁入 middleware.FilesGuard）。

import (
	"os"

	"github.com/gin-gonic/gin"

	"hivemtk-user/internal/middleware"
	"hivemtk-user/internal/pkg/utils/logger"
	"hivemtk-user/internal/storage"
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
//
// /files 走守卫版（同源可执行扩展名 403 + 签名凭证 + nosniff/sandbox）而非裸 r.Static，
// 堵素材库/渠道媒体任意扩展名落盘后的同源直出。守卫实现见 middleware.FilesGuard。
func RegisterFilesRoute(r *gin.Engine) {
	uploadDir, _, _ := storage.LocalSource()
	_ = os.MkdirAll(uploadDir, 0o750)
	r.GET("/files/*filepath", middleware.FilesGuard(uploadDir))
	logger.Infof("[Router] static file server registered (signed): /files -> %s", uploadDir)
}
