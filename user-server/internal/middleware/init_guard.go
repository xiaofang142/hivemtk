package middleware

import (
	"hivemtk-user/internal/pkg/utils/logger"

	"github.com/gin-gonic/gin"
)

// InitGuard 系统初始化守卫（开源版）
//
// 行为：
//   - 系统 NOT_INSTALLED：返回 INIT_REQUIRED，前端跳 /setup
//   - 系统 INITIALIZED：放行
//   - 白名单路由（init-*/login/health 等）直接放行
//
// 本守卫只看一件事：install.lock 里 initialized 是否为真。
// hivemtk 已全面开源，没有授权态可拦，也不再强制新账号改密。
func InitGuard() gin.HandlerFunc {
	return func(c *gin.Context) {
		path := c.FullPath()
		if isInitWhitelist(path) {
			c.Next()
			return
		}
		checker := GetInstallStatus()
		if checker == nil {
			c.Next()
			return
		}
		status := checker.GetInitStatus()
		if status == nil {
			c.Next()
			return
		}

		if !status.Initialized {
			logger.Info("InitGuard: 未初始化 path=" + path)
			c.AbortWithStatusJSON(200, gin.H{
				"code":     "INIT_REQUIRED",
				"message":  "系统未初始化",
				"redirect": "/setup",
				"data":     status,
			})
			return
		}

		c.Next()
	}
}

func isInitWhitelist(path string) bool {
	whitelist := map[string]bool{
		"/api/system/init-status":          true,
		"/api/system/info":                 true,
		"/api/system/init-admin":           true,
		"/api/system/init-complete":        true,
		"/api/auth/login":                  true,
		"/api/auth/refresh-token":          true,
		"/api/auth/change-password":        true,
		"/api/auth/current-user":           true,
		"/api/system/create-default-admin": true,
		"/s/:code":                         true,
		"/l/:code":                         true,
		"/health":                          true,
		"/healthz":                         true,
		"/readyz":                          true,
	}
	return whitelist[path]
}
