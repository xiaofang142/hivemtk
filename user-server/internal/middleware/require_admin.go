package middleware

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// RequireAdminMiddleware 要求角色为 admin 的中间件
// 必须在 JWTAuthMiddleware 之后使用
//
// 流程：
//  1. 从 context 读取 role（由 JWTAuthMiddleware 写入）
//  2. 校验 role == "admin"
//  3. 不通过则 403
//
// 与 middleware/jwt.go 中 AdminAuthMiddleware 的差异：
//   - 响应字段统一为 {code, message}（前端协议一致）
//   - 不修改 ctx，仅在失败时 Abort
//   - 测试模式短路逻辑沿用全局 IsTestMode（不重复声明）
func RequireAdminMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		if IsTestMode && testModeGate() {
			c.Next()
			return
		}
		role, exists := c.Get("role")
		if !exists {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"code":    401,
				"message": "未授权，请先登录",
			})
			return
		}
		roleStr, ok := role.(string)
		if !ok || roleStr != "admin" {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{
				"code":    403,
				"message": "权限不足，需要超管权限",
			})
			return
		}
		c.Next()
	}
}
