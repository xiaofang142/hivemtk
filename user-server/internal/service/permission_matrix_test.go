package service

import (
	"context"
	"testing"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/testutil"

	"gorm.io/gorm"
)

const d13Matrix = `{"customer_service":["cards.*","clues.view"],"staff":["cards.view"],"viewer":[]}`

func setupD13(t *testing.T, matrix string) (*ConfigParamService, *gorm.DB) {
	t.Helper()
	db := testutil.NewTestDB(t, &model.ConfigParam{})
	svc := NewConfigParamService()
	if err := SeedConfigParams(context.Background()); err != nil {
		t.Skipf("seed 不可用: %v", err)
	}
	if matrix != "" {
		if err := db.Exec(`UPDATE config_params SET param_value = ? WHERE param_group = 'permission' AND key = 'role_permissions_json'`, matrix).Error; err != nil {
			t.Fatal(err)
		}
	}
	svc.invalidate("permission", "role_permissions_json")

	prev := GlobalConfigParam()
	SetGlobalForTest(svc)
	t.Cleanup(func() {
		if prev != nil {
			SetGlobalForTest(prev)
		}
	})
	return svc, db
}

// 配置生效：cs 通配命中 cards.edit、精确命中 clues.view
func TestD13_MatrixOverridesBuiltin(t *testing.T) {
	ps := &PermissionService{}
	_, _ = setupD13(t, d13Matrix)
	if !ps.CheckPermission(context.Background(), SystemUserRoleCustomerService, "cards.edit") {
		t.Error("cards.* 应命中 cards.edit")
	}
	if !ps.CheckPermission(context.Background(), SystemUserRoleCustomerService, "clues.view") {
		t.Error("clues.view 应命中")
	}
	if ps.CheckPermission(context.Background(), SystemUserRoleCustomerService, "autoreply.edit") {
		t.Error("配置表未授 autoreply 应拒绝（覆盖内置的 autoreply.*）")
	}
}

// fail-closed：role 不在配置表 → 拒绝（不回退内置）
func TestD13_MissingRoleFailClosed(t *testing.T) {
	ps := &PermissionService{}
	_, _ = setupD13(t, d13Matrix)

	if !ps.CheckPermission(context.Background(), SystemUserRoleStaff, "cards.view") {
		t.Error("staff cards.view 应命中")
	}
	if ps.CheckPermission(context.Background(), "manager", "cards.view") {
		t.Error("manager 不在配置表应 fail-closed 拒绝")
	}
}

// admin 短路不受配置影响（防自锁）
func TestD13_AdminAlwaysAllowed(t *testing.T) {
	_, _ = setupD13(t, d13Matrix)
	ps := &PermissionService{}
	if !ps.CheckPermission(context.Background(), SystemUserRoleAdmin, "anything.at.all") {
		t.Error("admin 应恒全权")
	}
}

// 坏 JSON → 回退内置（fail-safe）
func TestD13_BadJSONFallsBack(t *testing.T) {
	_, _ = setupD13(t, "{broken")
	ps := &PermissionService{}
	if !ps.CheckPermission(context.Background(), SystemUserRoleCustomerService, "cards.edit") {
		t.Error("坏 JSON 应回退内置（customer_service 有 cards.*）")
	}
}

// 安全：非 admin 的 "*" 被 strip
func TestD13_WildcardStripped(t *testing.T) {
	_, _ = setupD13(t, `{"staff":["*"]}`)
	ps := &PermissionService{}
	if ps.CheckPermission(context.Background(), SystemUserRoleStaff, "anything.at.all") {
		t.Error("非 admin 的 '*' 应被剥除")
	}
}

// TTL：改配置 60s 后生效（注入时钟模式验证组重拉）
func TestD13_TTLReloadAfterUpdate(t *testing.T) {
	startMatrix := `{"customer_service":["cards.*","shortlinks.*","clues.*","autoreply.*"],"staff":["cards.view"]}`
	svc, db := setupD13(t, startMatrix)
	now := time.Now()
	svc.nowFn = func() time.Time { return now }
	ps := &PermissionService{}

	if !ps.CheckPermission(context.Background(), SystemUserRoleCustomerService, "autoreply.edit") {
		t.Fatal("前置：默认矩阵 cs 应有 autoreply.*")
	}

	db.Exec(`UPDATE config_params SET param_value = '{"customer_service":["cards.*"]}' WHERE param_group = 'permission' AND key = 'role_permissions_json'`)

	if !ps.CheckPermission(context.Background(), SystemUserRoleCustomerService, "autoreply.edit") {
		t.Error("未过期应仍读旧矩阵（autoreply.edit=true）")
	}
	now = now.Add(61 * time.Second)
	if ps.CheckPermission(context.Background(), SystemUserRoleCustomerService, "autoreply.edit") {
		t.Error("TTL 过期后应读到新矩阵（收权生效）")
	}
}
