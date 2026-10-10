package rbac

import (
	"testing"

	"hivemtk-user/internal/model"
)

// permissions_gen_test.go 生成层的语义护栏（生成物新鲜度由 CI 的
// `python scripts/gen-registry.py --check` 钉；这里钉"生成物与消费方一致"）。

// TestAPIKeyStates_SyncWithModel rbac 生成的状态字面量必须与 model 层常量同值
//（两处都消费：guard 判定用 model 常量，P1 权限面用 rbac 常量——漂移即状态机分裂）。
func TestAPIKeyStates_SyncWithModel(t *testing.T) {
	pairs := map[string]string{
		APIKeyStateActive:    model.MCPKeyStatusActive,
		APIKeyStateDisabled:  model.MCPKeyStatusDisabled,
		APIKeyStateSuspended: model.MCPKeyStatusSuspended,
		APIKeyStateRevoked:   model.MCPKeyStatusRevoked,
	}
	for gen, m := range pairs {
		if gen != m {
			t.Errorf("API Key 状态漂移：rbac=%q model=%q（registry.yaml 改了没同步两处？）", gen, m)
		}
	}
}

// TestRoleMatrix_BasicInvariants 角色矩阵的底线判据：
//   - 超级管理员持有全部权限点（需求 3 的权限面）；
//   - api_key.reveal 绝不下发到 member（明文查看是敏感位，backlog P1-3）；
//   - team.manage（跨团队）只有超级管理员。
func TestRoleMatrix_BasicInvariants(t *testing.T) {
	for _, p := range []string{
		PermissionCustomerRead, PermissionCustomerWrite, PermissionReachSend,
		PermissionApiKeyReveal, PermissionAuditExport, PermissionTeamManage,
	} {
		if !RoleHasPermission("super_admin", p) {
			t.Errorf("super_admin 应持有 %s", p)
		}
	}
	if RoleHasPermission("member", PermissionApiKeyReveal) {
		t.Error("member 不得持有 api_key.reveal（明文查看是敏感权限）")
	}
	if RoleHasPermission("team_admin", PermissionTeamManage) {
		t.Error("跨团队 team.manage 只有超级管理员（需求 3 口径）")
	}
	// 未知角色一律拒绝（fail-closed）
	if RoleHasPermission("ghost", PermissionCustomerRead) {
		t.Error("未知角色应 fail-closed")
	}
}
