package service

import (
	"testing"
)

func TestRequireRole(t *testing.T) {
	tests := []struct {
		name         string
		operatorRole string
		allowed      []string
		wantErr      bool
	}{
		{name: "admin 满足 admin", operatorRole: "admin", allowed: []string{"admin"}, wantErr: false},
		{name: "admin 满足 manager", operatorRole: "admin", allowed: []string{"manager", "admin"}, wantErr: false},
		{name: "manager 满足 manager", operatorRole: "manager", allowed: []string{"manager"}, wantErr: false},
		{name: "manager 不满足 admin-only", operatorRole: "manager", allowed: []string{"admin"}, wantErr: true},
		{name: "viewer 不满足 admin/manager", operatorRole: "viewer", allowed: []string{"admin", "manager"}, wantErr: true},
		{name: "空角色拒绝", operatorRole: "", allowed: []string{"admin", "manager", "viewer"}, wantErr: true},
		{name: "未知角色拒绝", operatorRole: "unknown", allowed: []string{"admin", "manager", "viewer"}, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := RequireRole(tt.operatorRole, tt.allowed...)
			gotErr := err != nil
			if gotErr != tt.wantErr {
				t.Errorf("RequireRole(%q, %v) error = %v, wantErr = %v", tt.operatorRole, tt.allowed, err, tt.wantErr)
			}
		})
	}
}

// TestRequireAdmin 测试仅 admin
func TestRequireAdmin(t *testing.T) {
	if err := RequireAdmin("admin"); err != nil {
		t.Errorf("admin should pass, got %v", err)
	}
	if err := RequireAdmin("manager"); err == nil {
		t.Errorf("manager should not pass RequireAdmin")
	}
	if err := RequireAdmin("viewer"); err == nil {
		t.Errorf("viewer should not pass RequireAdmin")
	}
	if err := RequireAdmin(""); err == nil {
		t.Errorf("empty role should not pass RequireAdmin")
	}
}

// TestRequireNotViewer 测试非 viewer
func TestRequireNotViewer(t *testing.T) {
	if err := RequireNotViewer("admin"); err != nil {
		t.Errorf("admin should pass, got %v", err)
	}
	if err := RequireNotViewer("manager"); err != nil {
		t.Errorf("manager should pass, got %v", err)
	}
	if err := RequireNotViewer("viewer"); err == nil {
		t.Errorf("viewer should not pass")
	}
}

// TestIsWriteRole 测试角色读写权限判定
func TestIsWriteRole(t *testing.T) {
	if !IsWriteRole("admin") {
		t.Error("admin should be write role")
	}
	if !IsWriteRole("customer_service") {
		t.Error("customer_service should be write role")
	}
	if IsWriteRole("manager") {
		t.Error("legacy manager should NOT be write role")
	}
	if IsWriteRole("staff") {
		t.Error("staff should NOT be write role")
	}
	if IsWriteRole("viewer") {
		t.Error("viewer should NOT be write role")
	}
}

// TestIsReadOnlyRole 测试只读角色
func TestIsReadOnlyRole(t *testing.T) {
	if !IsReadOnlyRole("viewer") {
		t.Error("viewer should be read-only")
	}
	if IsReadOnlyRole("admin") {
		t.Error("admin should NOT be read-only")
	}
	if IsReadOnlyRole("manager") {
		t.Error("manager should NOT be read-only")
	}
}

// TestAssertCanOperateSystemUser 测试 SystemUser 操作权限
// 单租户迁移后 team_users 已合并进 system_users，此处覆盖生产函数 AssertCanOperateSystemUser。
func TestAssertCanOperateSystemUser(t *testing.T) {
	tests := []struct {
		name         string
		operatorID   uint
		operatorRole string
		action       string
		targetID     uint
		wantErr      bool
	}{
		{name: "admin create", operatorID: 1, operatorRole: "admin", action: "create", targetID: 0, wantErr: false},
		{name: "admin delete other", operatorID: 1, operatorRole: "admin", action: "delete", targetID: 5, wantErr: false},
		{name: "admin reset_password other", operatorID: 1, operatorRole: "admin", action: "reset_password", targetID: 5, wantErr: false},
		{name: "admin update other", operatorID: 1, operatorRole: "admin", action: "update", targetID: 5, wantErr: false},

		// A-23：manager 为死词（system_users.role 白名单无 manager，无签发链恒假）——
		// 死分支已删，manager 角色一切动作一律拒绝（fail-closed 语义不变）
		{name: "manager update 拒绝（A-23 死词）", operatorID: 1, operatorRole: "manager", action: "update", targetID: 5, wantErr: true},
		{name: "manager create 拒绝", operatorID: 1, operatorRole: "manager", action: "create", targetID: 0, wantErr: true},
		{name: "manager delete 拒绝", operatorID: 1, operatorRole: "manager", action: "delete", targetID: 5, wantErr: true},
		{name: "manager reset 拒绝", operatorID: 1, operatorRole: "manager", action: "reset_password", targetID: 5, wantErr: true},

		{name: "customer_service update other", operatorID: 1, operatorRole: "customer_service", action: "update", targetID: 5, wantErr: false},
		{name: "customer_service create 拒绝", operatorID: 1, operatorRole: "customer_service", action: "create", targetID: 0, wantErr: true},
		{name: "customer_service delete 拒绝", operatorID: 1, operatorRole: "customer_service", action: "delete", targetID: 5, wantErr: true},

		{name: "staff update 拒绝", operatorID: 1, operatorRole: "staff", action: "update", targetID: 5, wantErr: true},
		{name: "viewer create 拒绝", operatorID: 1, operatorRole: "viewer", action: "create", targetID: 0, wantErr: true},
		{name: "viewer update 拒绝", operatorID: 1, operatorRole: "viewer", action: "update", targetID: 5, wantErr: true},

		{name: "空角色拒绝", operatorID: 1, operatorRole: "", action: "create", targetID: 0, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := AssertCanOperateSystemUser(tt.operatorID, tt.operatorRole, tt.action, tt.targetID)
			gotErr := err != nil
			if gotErr != tt.wantErr {
				t.Errorf("AssertCanOperateSystemUser(%d, %q, %q, %d) error = %v, wantErr = %v",
					tt.operatorID, tt.operatorRole, tt.action, tt.targetID, err, tt.wantErr)
			}
		})
	}
}

// TestIsValidSystemUserRoleCode 测试 system_user 角色合法性校验
func TestIsValidSystemUserRoleCode(t *testing.T) {
	valid := []string{"admin", "customer_service", "staff"}
	for _, r := range valid {
		if !IsValidSystemUserRoleCode(r) {
			t.Errorf("role %q should be valid", r)
		}
	}
	invalid := []string{"", "super_admin", "manager", "viewer", "operator", "sales", "user"}
	for _, r := range invalid {
		if IsValidSystemUserRoleCode(r) {
			t.Errorf("role %q should NOT be valid", r)
		}
	}
}
