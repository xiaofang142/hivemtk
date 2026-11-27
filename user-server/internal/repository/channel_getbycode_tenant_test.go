package repository

// TestT03_GetByCode_SeesGlobalPresetUnderTenantScope 钉死 /api/channels/catalog 409 根因。
//
// 预设码是跨租户全局唯一的模板键（global_uniq_channel_code，tenant_id=0）。在租户
// 上下文里查 GetByCode 时，若漏了系统作用域，租户 callback 会注入 tenant_id=? 把
// tenant_id=0 的全局行过滤掉 → EnsurePresets 误判「未播种」→ 重复 INSERT 撞 23505
// global_uniq_channel_code → 线上 catalog 恒 409（2026-11-26 实证）。
// 本测试强制在 ModeOn + 租户上下文下，GetByCode 必须读到全局行。

import (
	"context"
	"testing"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/pkg/testutil"
)

func TestT03_GetByCode_SeesGlobalPresetUnderTenantScope(t *testing.T) {
	tenant.SetMode(tenant.ModeOn)
	t.Cleanup(func() {
		tenant.SetMode(tenant.ModeOff)
		tenant.ResetRegistryForTest()
	})

	db := testutil.NewTestDB(t, &model.Channel{})
	if db == nil {
		t.Fatal("测试库不可达：本测试不 Skip（跳过等于没锁 409 回归）")
	}

	const code = "t03_global_preset_code" // 非国内码，避开 edition 闸
	sysCtx := tenant.WithSystemScope(context.Background())
	// 用显式 SQL 播种 tenant_id=0 的全局行：GORM 结构体 Create 遇 default:1 标签会把
	// 零值 TenantID 省略、落列默认值 1，造不出真正的全局预设行；系统作用域下 raw
	// 守卫放行，直接 INSERT 才能钉死 tenant_id=0 这个线上真实形状。
	if err := db.WithContext(sysCtx).Exec(
		`INSERT INTO channels (tenant_id, name, code, type, enabled) VALUES (0, '全局预设', ?, 'dm', true)`, code,
	).Error; err != nil {
		t.Fatalf("播种全局预设行失败: %v", err)
	}

	repo := NewChannelRepositoryWithDB(db)
	tenantCtx := tenant.WithTenantID(context.Background(), 18)
	got, err := repo.GetByCode(tenantCtx, code)
	if err != nil {
		t.Fatalf("GetByCode 不应报错: %v", err)
	}
	if got == nil {
		t.Fatal("租户上下文下 GetByCode 必须读到 tenant_id=0 的全局预设行；返回 nil 说明漏了系统作用域（catalog 409 根因）")
	}
	if got.Code != code || got.TenantID != 0 {
		t.Fatalf("读到的不是全局预设行: %+v", got)
	}
	byID, err := repo.GetByID(tenantCtx, ChannelGlobalTenantID, got.ID)
	if err != nil || byID == nil || byID.ID != got.ID {
		t.Fatalf("租户上下文必须能按 ID 读取全局模板: row=%+v err=%v", byID, err)
	}
	rows, err := repo.List(tenantCtx, ChannelListOptions{TenantID: 18, IncludeGlobal: true})
	if err != nil {
		t.Fatalf("读取全局模板与租户渠道并集失败: %v", err)
	}
	found := false
	for _, row := range rows {
		found = found || row.ID == got.ID
	}
	if !found {
		t.Fatalf("IncludeGlobal 列表不得被租户 callback 过滤掉全局模板: %+v", rows)
	}
}
