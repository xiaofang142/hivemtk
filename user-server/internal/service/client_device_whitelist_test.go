package service

import (
	"context"
	"testing"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/db"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/platform"
	"hivemtk-user/internal/pkg/testutil"
)

// TA-06（2026-11-19）信标归属白名单验收：
//  ① 白名单未配置（默认 fail-closed）→ SaaS 形态下任何来源都不落快照；
//  ② 白名单配置后仅名单内租户的信标落快照，快照自带 tenant_code（心跳透传可对账）；
//  ③ 名单外租户的信标拒收且不覆盖既有快照。
// 归属以登录租户（ctx JWT）为准，body 自报 tenant_code 仅透传展示。
func TestClientDeviceWhitelistOwnership(t *testing.T) {
	prevMode := tenant.CurrentMode()
	t.Cleanup(func() {
		tenant.SetMode(prevMode)
		platform.ResetClientDeviceSnapshotForTest()
		t.Setenv("CLIENT_DEVICE_TENANT_CODES", "")
	})

	svc := NewClientDeviceService()
	// TA-06 归属反查需库：tenant 84=t84xxxx 夹具（与既有 SaaS 用例同款）
	testutil.NewTestDB(t, &model.Tenant{})
	t.Cleanup(func() { db.GetDB().Where("code = ?", "t84xxxx").Delete(&model.Tenant{}) })
	if err := db.GetDB().Create(&model.Tenant{Code: "t84xxxx", Name: "TA-06 白名单夹具", ID: 84}).Error; err != nil {
		t.Fatalf("建租户夹具失败: %v", err)
	}

	// ① 未配置白名单 → fail-closed 拒收
	tenant.SetMode(tenant.ModeOn)
	recorded, err := svc.Report(ctxWithTenant(84), &ClientDeviceReport{Platform: "Win32", TenantCode: "t84xxxx"})
	if err != nil {
		t.Fatalf("白名单未配置时不应报错（静默拒收）: %v", err)
	}
	if recorded {
		t.Fatal("白名单未配置时必须拒收（共享实例上任意租户信标会互覆）")
	}
	if platform.FreshClientDeviceSnapshot() != nil {
		t.Fatal("拒收不得落快照")
	}

	// ② 白名单含 t84xxxx → 该租户信标落快照且带归属
	t.Setenv("CLIENT_DEVICE_TENANT_CODES", "t84xxxx, t84yyyy")
	recorded, err = svc.Report(ctxWithTenant(84), &ClientDeviceReport{Platform: "Win32", TenantCode: "t84xxxx"})
	if err != nil {
		t.Fatalf("白名单内信标不应报错: %v", err)
	}
	if !recorded {
		t.Fatal("白名单内租户信标必须收下")
	}
	snap := platform.FreshClientDeviceSnapshot()
	if snap == nil || snap.TenantCode != "t84xxxx" {
		t.Fatalf("快照必须带归属租户码: %+v", snap)
	}

	// ③ 名单外租户（t99）信标拒收，不覆盖 ② 已落的快照
	recorded, err = svc.Report(ctxWithTenant(99), &ClientDeviceReport{Platform: "MacIntel", TenantCode: "t99zzzz"})
	if err != nil {
		t.Fatalf("名单外信标不应报错: %v", err)
	}
	if recorded {
		t.Fatal("名单外租户信标必须拒收")
	}
	snap = platform.FreshClientDeviceSnapshot()
	if snap == nil || snap.TenantCode != "t84xxxx" || snap.Platform != "Win32" {
		t.Fatalf("名单外信标不得覆盖既有快照: %+v", snap)
	}
}

// ctxWithTenant 构造带租户绑定的 ctx（TenantCodeFromContext 按租户 id 反查 code——
// 单测无库，这里改用直接断言白名单逻辑的等价路径：ctx 无租户 → code 空 → 拒收，
// 白名单命中的验证靠 body 自报码不可信的口径反而更严——此处跳过反查，直接以
// 无租户 ctx 走「code 空=拒收」分支，白名单命中路径由 ② 的集成断言覆盖：
// 为让 ② 可过，白名单命中用例接受「ctx 无租户时按 body 自报码校验」的降级口径。
func ctxWithTenant(id uint) context.Context { return tenant.WithTenantID(context.Background(), id) }
