package middleware

import (
	"errors"
	"hivemtk-user/internal/pkg/utils"
	"hivemtk-user/internal/pkg/utils/response"
	"os"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
)

// PermissionMiddleware 权限中间件
func PermissionMiddleware(permission string) gin.HandlerFunc {
	return func(c *gin.Context) {
		role, exists := c.Get("role")
		if !exists {
			response.Error(c, utils.ErrorCodeForbidden, "未找到角色信息")
			c.Abort()
			return
		}

		roleStr, ok := role.(string)
		if !ok {
			response.Error(c, utils.ErrorCodeInternalError, "角色类型异常")
			c.Abort()
			return
		}

		if !checkPermission(c, roleStr, permission) {
			response.Error(c, utils.ErrorCodeForbidden, "无权限执行此操作")
			c.Abort()
			return
		}

		c.Next()
	}
}

// AdminOnlyMiddleware 仅管理员访问中间件
func AdminOnlyMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		role, exists := c.Get("role")
		if !exists {
			response.Error(c, utils.ErrorCodeForbidden, "未找到角色信息")
			c.Abort()
			return
		}

		roleStr, ok := role.(string)
		if !ok {
			response.Error(c, utils.ErrorCodeInternalError, "角色类型异常")
			c.Abort()
			return
		}

		if roleStr != "admin" {
			response.Error(c, utils.ErrorCodeForbidden, "仅管理员可访问")
			c.Abort()
			return
		}

		c.Next()
	}
}

// TeamJWTAuthMiddleware JWT 认证中间件 - 支持团队用户
func TeamJWTAuthMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		if IsTestMode && testModeGate() {
			c.Set("user_id", uint(1))
			c.Set("license_id", "system_admin")
			c.Next()
			return
		}
		authHeader := c.GetHeader("Authorization")
		if authHeader == "" {
			response.Error(c, utils.ErrorCodeUnauthorized, "未提供认证令牌")
			c.Abort()
			return
		}

		parts := strings.SplitN(authHeader, " ", 2)
		if !(len(parts) == 2 && parts[0] == "Bearer") {
			response.Error(c, utils.ErrorCodeTokenInvalid, "认证令牌格式错误")
			c.Abort()
			return
		}

		tokenString := parts[1]

		claims, err := ParseJWTToken(tokenString)
		if err != nil {
			response.Error(c, utils.ErrorCodeUnauthorized, "无效的认证令牌")
			c.Abort()
			return
		}

		c.Set("user_id", claims["user_id"])
		c.Set("username", claims["username"])
		c.Set("role", claims["role"])

		c.Next()
	}
}

// ParseJWTToken 解析 JWT Token
func ParseJWTToken(tokenString string) (map[string]any, error) {
	jwtSecret := os.Getenv("JWT_SECRET")
	if jwtSecret == "" {
		return nil, errors.New("JWT_SECRET 未配置")
	}

	token, err := jwt.Parse(tokenString, func(token *jwt.Token) (any, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, jwt.ErrSignatureInvalid
		}
		return []byte(jwtSecret), nil
	})
	if err != nil {
		return nil, err
	}

	if !token.Valid {
		return nil, jwt.ErrSignatureInvalid
	}

	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok {
		return nil, errors.New("无效的 Token Claims")
	}

	result := make(map[string]any)
	if userID, ok := claims["user_id"]; ok {
		result["user_id"] = userID
	}
	if username, ok := claims["username"]; ok {
		result["username"] = username
	}
	if role, ok := claims["role"]; ok {
		result["role"] = role
	}

	return result, nil
}

func checkPermission(c *gin.Context, role, permission string) bool {
	checker := getPermChecker()
	if checker == nil {
		warnPortMissing("PermChecker")
		return false
	}
	return checker.CheckPermission(c.Request.Context(), role, permission)
}

// RequirePermission 快捷方法 - 检查特定权限
func RequirePermission(permission string) gin.HandlerFunc {
	return PermissionMiddleware(permission)
}

// RequireAnyPermission 快捷方法 - 检查是否有任一权限
func RequireAnyPermission(permissions ...string) gin.HandlerFunc {
	return func(c *gin.Context) {
		role, exists := c.Get("role")
		if !exists {
			response.Error(c, utils.ErrorCodeForbidden, "未找到角色信息")
			c.Abort()
			return
		}

		roleStr, ok := role.(string)
		if !ok {
			response.Error(c, utils.ErrorCodeInternalError, "角色类型异常")
			c.Abort()
			return
		}

		for _, p := range permissions {
			if checkPermission(c, roleStr, p) {
				c.Next()
				return
			}
		}

		response.Error(c, utils.ErrorCodeForbidden, "无权限执行此操作")
		c.Abort()
	}
}

// RequireAllPermissions 快捷方法 - 检查是否有所有权限
func RequireAllPermissions(permissions ...string) gin.HandlerFunc {
	return func(c *gin.Context) {
		role, exists := c.Get("role")
		if !exists {
			response.Error(c, utils.ErrorCodeForbidden, "未找到角色信息")
			c.Abort()
			return
		}

		roleStr, ok := role.(string)
		if !ok {
			response.Error(c, utils.ErrorCodeInternalError, "角色类型异常")
			c.Abort()
			return
		}

		for _, p := range permissions {
			if !checkPermission(c, roleStr, p) {
				response.Error(c, utils.ErrorCodeForbidden, "无权限执行此操作")
				c.Abort()
				return
			}
		}

		c.Next()
	}
}
