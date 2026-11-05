package router

// Swagger 路由装配（2026-11-05 二次审计：处理器已迁入 controller.SwaggerController）。

import (
	"os"

	"github.com/gin-gonic/gin"

	"hivemtk-user/internal/controller"
)

// RegisterSwaggerRoutes 注册 Swagger 文档路由
// 使用方法：在 Setup() 函数末尾调用 RegisterSwaggerRoutes(r)
// 设置环境变量 ENABLE_SWAGGER=true 启用文档
// 访问 http://localhost:8204/swagger/index.html
//
// 生成 Swagger 文档命令（在 user-server 目录执行）：
//
//	swag init -g cmd/api/main.go -o ./docs --parseDependency --parseInternal
func RegisterSwaggerRoutes(r *gin.Engine) {
	if os.Getenv("ENABLE_SWAGGER") != "true" {
		return
	}
	sc := controller.NewSwaggerController()
	r.GET("/swagger/*any", sc.UI)
	r.GET("/api/swagger.json", sc.Spec)
}
