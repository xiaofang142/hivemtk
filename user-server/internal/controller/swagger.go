package controller

// Swagger 文档控制器（2026-11-05 二次审计：从 router 迁入）。

import (
	"net"
	"net/http"

	"github.com/gin-gonic/gin"
	swaggerFiles "github.com/swaggo/files"
	ginSwagger "github.com/swaggo/gin-swagger"

	"hivemtk-user/internal/pkg/utils/response"

	// 触发生成包 docs 的 init()，将 swagger 规范注册到 swaggo 全局注册表，
	// 供 ginSwagger.WrapHandler 读取。
	_ "hivemtk-user/docs"
)

// SwaggerController Swagger 文档。
type SwaggerController struct{}

func NewSwaggerController() *SwaggerController { return &SwaggerController{} }

// UI swagger 页面（仅本机访问）。
func (s *SwaggerController) UI(c *gin.Context) {
	if !isLocalRequest(c.ClientIP()) {
		response.Envelope(c, http.StatusForbidden, gin.H{"error": "swagger is only accessible from localhost"})
		return
	}
	ginSwagger.WrapHandler(swaggerFiles.Handler)(c)
}

// Spec swagger.json（仅本机访问）。
func (s *SwaggerController) Spec(c *gin.Context) {
	if !isLocalRequest(c.ClientIP()) {
		response.Envelope(c, http.StatusForbidden, gin.H{"error": "swagger is only accessible from localhost"})
		return
	}
	c.File("./docs/swagger.json")
}

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
