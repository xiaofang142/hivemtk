package rbac

// matrix.go 生成矩阵的结构化出参（/api/manage/rbac-matrix 与 /api/auth/my-permissions）。
// 数据全部来自 permissions_gen.go 的生成常量与角色映射——单一事实源仍是 registry.yaml。

// RoleDef / PermissionDef / StateDef 矩阵出参（labels 见 docs/registry/generated/api-vocabulary.md）。
type RoleDef struct {
	ID string `json:"id"`
}

type PermissionDef struct {
	ID    string   `json:"id"`
	Roles []string `json:"roles"`
}

type StateDef struct {
	ID string `json:"id"`
}

// Matrix 返回角色清单 + 权限点→角色映射 + API Key 状态机。
func Matrix() (roles []RoleDef, permissions []PermissionDef, states []StateDef) {
	seen := map[string]bool{}
	roleList := make([]RoleDef, 0, len(RoleAllPermissions))
	for rid := range RoleAllPermissions {
		if !seen[rid] {
			seen[rid] = true
			roleList = append(roleList, RoleDef{ID: rid})
		}
	}
	roles = roleList

	permSeen := map[string]bool{}
	permissions = make([]PermissionDef, 0)
	for rid, perms := range RoleAllPermissions {
		for pid := range perms {
			if permSeen[pid] {
				continue
			}
			permSeen[pid] = true
			holder := make([]string, 0, 3)
			for r2, ps := range RoleAllPermissions {
				if ps[pid] {
					holder = append(holder, r2)
				}
			}
			permissions = append(permissions, PermissionDef{ID: pid, Roles: holder})
		}
		_ = rid
	}

	states = []StateDef{
		{ID: APIKeyStateActive},
		{ID: APIKeyStateDisabled},
		{ID: APIKeyStateSuspended},
		{ID: APIKeyStateRevoked},
	}
	return roles, permissions, states
}

// PermissionCatalog 权限点目录（生成层全量 id，管理端编辑覆盖层的合法性依据）。
func PermissionCatalog() []string {
	_, permissions, _ := Matrix()
	out := make([]string, 0, len(permissions))
	for _, p := range permissions {
		out = append(out, p.ID)
	}
	return out
}

// RBACRoleFor 系统角色 → 矩阵角色（admin→super_admin 等；未知角色 ok=false）。
func RBACRoleFor(systemRole string) (string, bool) {
	switch systemRole {
	case "admin":
		return "super_admin", true
	case "team_admin":
		return "team_admin", true
	case "user":
		return "member", true
	}
	return "", false
}

// PermissionsFor 某矩阵角色持有的全部权限点（my-permissions 出参）。
func PermissionsFor(rbacRole string) []string {
	perms, ok := RoleAllPermissions[rbacRole]
	if !ok {
		return []string{}
	}
	out := make([]string, 0, len(perms))
	for pid := range perms {
		out = append(out, pid)
	}
	return out
}
