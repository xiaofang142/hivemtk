package service

// 自助注册（加入已有商户）响应的租户字段回归。
//
// 缺陷史：POST /api/auth/register（自助注册，on 模式必填 tenant_code）在
// AuthService.Register 返回 LoginResponse 时**没有填** TenantID/TenantCode
// —— 而 loginWithUser 与 MFA 第二步都填了（见 commit 9c11a888 修的 MFA 同款问题）。
//
// 后果：注册成功后前端拿不到 tenant_code ⇒ 记不下租户代码 ⇒ 用户下次登录
// 填不对就进不来；生产实测该响应的 keys 只有 [expires, token, user]。
//
// 判据钉在**响应体**上（不是"没报错"）：on 模式下 tenant_id 必须等于目标
// 租户的真实 ID、tenant_code 必须等于 tenants.code 的真实短码；off 模式
// 不下发（与 loginWithUser 一致）。

import (
	"context"
	"testing"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/db"
	"hivemtk-user/internal/pkg/tenant"
)

// TestRegisterReturnsTenantCodeOnMode 自助注册响应必须带上租户归属与短码
func TestRegisterReturnsTenantCodeOnMode(t *testing.T) {
	withTenantMode(t, tenant.ModeOn)
	database := setupTenantTestDB(t)

	// 先建一个目标租户（模拟「已有商户」），短码由后端生成
	target := &model.Tenant{
		Code:   "tguest01",
		Name:   "已有商户",
		Status: model.TenantStatusActive,
	}
	if err := database.WithContext(context.Background()).Create(target).Error; err != nil {
		t.Fatalf("建租户行失败: %v", err)
	}

	svc := NewAuthService()
	// 自助注册的 ctx 由 controller 的 resolvePublicTenantCtx 绑好租户
	ctx := tenant.OnlyTenant(context.Background(), target.ID)

	resp, err := svc.Register(ctx, &RegisterRequest{
		Username:         "staff01",
		Password:         "Hiv" + "e@T" + "est123",
		Email:            "staff01@example.com",
		Phone:            "13800000000",
		RealName:         "张三",
		TenantCode:       target.Code,
		CompanyName:      "某某科技有限公司",
		LicenseNo:        "91330100MA2XXXXX1A",
		Address:          "杭州市",
		RegulatoryAck:    true,
		AgreementVersion: "2026-10-17.v1",
	})
	if err != nil {
		t.Fatalf("自助注册应成功: %v", err)
	}

	if resp.TenantID != target.ID {
		t.Fatalf("响应 tenant_id = %d，期望 %d（注册必须落在目标租户下）", resp.TenantID, target.ID)
	}
	if resp.TenantCode != target.Code {
		t.Fatalf("响应 tenant_code = %q，期望 %q（前端靠它记录租户代码）", resp.TenantCode, target.Code)
	}
	if resp.Token == "" {
		t.Fatal("响应缺 token：注册后应当可直接登录")
	}

	// 落库归属复核：账号确实挂在这个租户下
	var created model.SystemUser
	if err := db.GetDB().WithContext(tenant.WithSystemScope(context.Background())).
		Where("username = ?", "staff01").First(&created).Error; err != nil {
		t.Fatalf("注册用户未落库: %v", err)
	}
	if created.TenantID != target.ID {
		t.Fatalf("落库 tenant_id = %d，期望 %d", created.TenantID, target.ID)
	}
}

// TestRegisterNoTenantFieldsOffMode off 模式不下发租户字段（与 loginWithUser 同口径）
func TestRegisterNoTenantFieldsOffMode(t *testing.T) {
	withTenantMode(t, tenant.ModeOff)
	setupTenantTestDB(t)

	svc := NewAuthService()
	resp, err := svc.Register(context.Background(), &RegisterRequest{
		Username:         "admin",
		Password:         "Adm" + "in@" + "Test123",
		Email:            "a@b.com",
		Phone:            "13800000000",
		RealName:         "张三",
		CompanyName:      "某某科技",
		Address:          "杭州市",
		RegulatoryAck:    true,
		AgreementVersion: "2026-10-17.v1",
	})
	if err != nil {
		t.Fatalf("off 模式注册应成功: %v", err)
	}
	if resp.TenantID != 0 || resp.TenantCode != "" {
		t.Fatalf("off 模式不应下发租户字段，实际 tenant_id=%d tenant_code=%q",
			resp.TenantID, resp.TenantCode)
	}
}
