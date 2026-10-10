package middleware

import (
	"net/http"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/utils/response"
	"hivemtk-user/internal/rbac"

	"github.com/gin-gonic/gin"
)

// systemRoleToRBACRole 系统角色 → 生成矩阵角色（rbac 包口径）。
var systemRoleToRBACRole = map[string]string{
	model.SystemUserRoleAdmin:     "super_admin",
	model.SystemUserRoleTeamAdmin: "team_admin",
	model.SystemUserRoleUser:      "member",
}

// RequireRBACPermission 权限点门（backlog P1-1，2026-10-11）。
//
// 与 permission.go 的 RequirePermission（PermChecker 端口，走 049 自定义角色表）
// 并存不混用：本门显式消费 docs/registry/registry.yaml 生成的 rbac 矩阵
// （RoleHasPermission），无外部 checker 依赖——部署里没有装配 PermChecker
// 也照常工作。角色映射：admin→super_admin、team_admin→team_admin、user→member；
// 未知角色 fail-closed（403）。
//
// 选型注记：P1-1 的终态是两套合一（矩阵入库 + PermChecker 消费），
// 在那之前管理面新路由用本门，避免"没配 checker 就全 403"的部署陷阱。
func RequireRBACPermission(permission string) gin.HandlerFunc {
	return func(c *gin.Context) {
		if IsTestMode && testModeGate() {
			c.Next()
			return
		}
		role, exists := c.Get("role")
		if !exists {
			response.Error(c, http.StatusUnauthorized, "未找到用户角色信息")
			c.Abort()
			return
		}
		r, _ := role.(string)
		mapped, ok := systemRoleToRBACRole[r]
		if !ok || !rbac.RoleHasPermission(mapped, permission) {
			response.Error(c, http.StatusForbidden, "权限不足，需要 "+permission)
			c.Abort()
			return
		}
		c.Next()
	}
}
