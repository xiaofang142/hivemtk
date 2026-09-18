package router

import (
	"net"
	"net/http"
	"os"

	"github.com/gin-gonic/gin"
	swaggerFiles "github.com/swaggo/files"
	ginSwagger "github.com/swaggo/gin-swagger"

	// 触发生成包 docs 的 init()，将 swagger 规范注册到 swaggo 全局注册表，
	// 供 ginSwagger.WrapHandler 读取。
	_ "hivemtk-user/docs"
)

func isLocalRequest(ip string) bool {
	if ip == "127.0.0.1" || ip == "::1" || ip == "localhost" {
		return true
	}
	parsedIP := net.ParseIP(ip)
	if parsedIP == nil {
		return false
	}
	return parsedIP.IsLoopback()
}

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

	r.GET("/swagger/*any", func(c *gin.Context) {
		if !isLocalRequest(c.ClientIP()) {
			c.JSON(http.StatusForbidden, gin.H{"error": "swagger is only accessible from localhost"})
			return
		}
		ginSwagger.WrapHandler(swaggerFiles.Handler)(c)
	})

	r.GET("/api/swagger.json", func(c *gin.Context) {
		if !isLocalRequest(c.ClientIP()) {
			c.JSON(http.StatusForbidden, gin.H{"error": "swagger is only accessible from localhost"})
			return
		}
		c.File("./docs/swagger.json")
	})
}
