package controller

import (
	"hivemtk-user/internal/pkg/utils/response"
	"hivemtk-user/internal/rbac"

	"github.com/gin-gonic/gin"
)

// rbac_matrix.go RBAC 矩阵与我的权限端点（backlog P1-2 的服务端一半，2026-10-11）。
//
//   - GET /api/manage/rbac-matrix（平台管理员）：只读矩阵——前端"权限矩阵"页的数据源；
//   - GET /api/auth/my-permissions（任意登录用户）：当前身份的 RBAC 角色与权限点清单，
//     前端按钮级显隐（P1-2）以此为准（服务端仍是唯一执法者，前端只做显隐）。

// GetRBACMatrix GET /api/manage/rbac-matrix
func GetRBACMatrix(ctx *gin.Context) {
	roles, permissions, states := rbac.Matrix()
	response.Success(ctx, gin.H{
		"roles":          roles,
		"permissions":    permissions,
		"api_key_states": states,
	}, "ok")
}

// GetMyPermissions GET /api/auth/my-permissions
func GetMyPermissions(ctx *gin.Context) {
	role, _ := ctx.Get("role")
	r, _ := role.(string)
	rbacRole, ok := rbac.RBACRoleFor(r)
	if !ok {
		// 未知角色（如存量 customer_service）：fail-closed，空权限清单
		response.Success(ctx, gin.H{
			"role":        r,
			"rbac_role":   "",
			"permissions": []string{},
		}, "ok")
		return
	}
	response.Success(ctx, gin.H{
		"role":        r,
		"rbac_role":   rbacRole,
		"permissions": rbac.PermissionsFor(rbacRole),
	}, "ok")
}
