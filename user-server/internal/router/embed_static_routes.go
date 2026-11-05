package router

// 用户端 SPA / embed SDK 静态路由装配（2026-11-05 二次审计：处理器已迁入 StaticController）。

import (
	"os"
	"path/filepath"

	"github.com/gin-gonic/gin"

	"hivemtk-user/internal/controller"
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
			r.Static("/assets", filepath.Join(userWebDist, "assets"))
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
			r.Static("/embed", embedDist)
		}
	}
}
