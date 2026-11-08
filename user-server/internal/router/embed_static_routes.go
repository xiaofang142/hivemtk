package router

// 用户端 SPA / embed SDK 静态路由装配（2026-11-05 二次审计：处理器已迁入 StaticController）。

import (
	"os"
	"path/filepath"

	"github.com/gin-gonic/gin"

	"hivemtk-user/internal/controller"
	"hivemtk-user/internal/middleware"
)

func setupEmbedStaticRoutes(r *gin.Engine) {
	userWebDist := os.Getenv("USER_WEB_DIST")
	if userWebDist == "" {
		candidates := []string{
			"../user-web/dist",
			"./user-web-dist",
			"./dist",
		}
		for _, c := range candidates {
			if _, err := os.Stat(c); err == nil {
				userWebDist, _ = filepath.Abs(c)
				break
			}
		}
	}

	embedDist := os.Getenv("EMBED_SDK_DIST")
	if embedDist == "" {
		candidates := []string{
			"../embed-sdk/dist",
			"./embed-sdk-dist",
		}
		for _, c := range candidates {
			if _, err := os.Stat(c); err == nil {
				embedDist, _ = filepath.Abs(c)
				break
			}
		}
	}

	if userWebDist != "" {
		if _, err := os.Stat(userWebDist); err == nil {
			// A-18（2026-11-09）：改用 StaticGuard 对齐 /files/* 的防护基线
			// （禁用扩展名 403 + nosniff/sandbox）。构建产物本无可执行内容，
			// 但防"部署目录被篡改/未来部署形态变化"的纵深成本一行装配。
			// F-4-2：守卫用构建产物专用拒绝清单（.js/.css/.svg 放行），
			// 不能复用上传面的 dangerousWebExts。
			// F-4-3：r.Static 原本 GET+HEAD 双注册，这里保持 HEAD 平价。
			assetsGuard := middleware.StaticGuard(filepath.Join(userWebDist, "assets"))
			r.GET("/assets/*filepath", assetsGuard)
			r.HEAD("/assets/*filepath", assetsGuard)
			r.StaticFile("/favicon.ico", filepath.Join(userWebDist, "favicon.ico"))
			r.StaticFile("/logo.png", filepath.Join(userWebDist, "logo.png"))

			sc := controller.NewStaticController(userWebDist)
			r.GET("/chat/embed/*path", sc.EmbedPath)
			r.GET("/chat/embed", sc.EmbedRoot)
			r.GET("/", sc.SPA)
			r.NoRoute(sc.NotFound)
			r.GET("/index.html", sc.Index)
		} else {
			sc := controller.NewStaticController("")
			r.GET("/chat/embed/*path", sc.EmbedDistMissing)
		}
	} else {
		sc := controller.NewStaticController("")
		r.GET("/chat/embed/*path", sc.DistNotConfigured)
	}

	if embedDist != "" {
		if _, err := os.Stat(embedDist); err == nil {
			// A-18：同 /assets——embed SDK 是匿名公开面，守卫不带签名层。
			// F-4-3：HEAD 与 GET 平价（embed SDK 全是 .js，F-4-2 清单必须放行）。
			embedGuard := middleware.StaticGuard(embedDist)
			r.GET("/embed/*filepath", embedGuard)
			r.HEAD("/embed/*filepath", embedGuard)
		}
	}
}
