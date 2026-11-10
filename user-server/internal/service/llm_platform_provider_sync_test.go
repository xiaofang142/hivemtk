package service

import (
	"context"
	"strings"
	"testing"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/repository"

	"gorm.io/gorm"
	"hivemtk-user/internal/pkg/testutil"
	"hivemtk-user/internal/platform"
)

// savePlatformProvidersViaRepo 仓储化后的等价入口：映射留 service，落库走
// PlatformLLMUsageRepository（savePlatformProviders 已按 L3 铁律下沉删除）。
// 价目映射缺省 nil = 单价快照列落 0（价目拉取失败的同格降级）。
func savePlatformProvidersViaRepo(db *gorm.DB, rows []platform.PlatformLLMProvider) (int, error) {
	sysCtx := tenant.WithSystemScope(context.Background())
	return repository.NewPlatformLLMUsageRepositoryWithDB(db).UpsertProviders(sysCtx, platformProviderRowsToModels(rows, nil))
}

// 回归钉死 TENANT_MODE=on 时「平台 provider 同步」落库被隔离 fail-closed 拦截的缺陷
// （生产日志实证，2026-10-22）：
//
//	[llm-provider] 平台 provider 落库失败 platform-sensenova: [tenant] 表 llm_providers 需要租户上下文但 ctx 未绑定（create）；多租户模式下拒绝执行，请经中间件/TenantRunner/行自证提供租户
//	[llm-provider] 平台下发 provider 同步完成：0 条
//
// 后果：平台下发的 provider 永远同步不到商户端 ⇒ source=platform 的 provider
// 不存在 ⇒ 平台垫资的 LLM 通道对所有租户不可用（新商户赠送的 token 无法消耗）。
//
// 同一缺陷类在 dispatcher 路径已修（dispatcher_persistence.go 的 systemScopeCtx() +
// dispatcher_persistence_system_scope_test.go）。本次是同一类缺陷在 sync 路径上的遗漏。
//
// 关键不变量：**隔离闸本身不得被放宽** —— 无租户 ctx 的裸写仍须 fail-closed；
// 放宽的只是「装配层写库点」这一个位置，走 tenant.WithSystemScope 唯一豁免通道。
func TestSavePlatformProviders_OnModeWithoutTenantCtx(t *testing.T) {
	database := testutil.NewTestDB(t, &model.LLMProvider{})
	tenant.SetMode(tenant.ModeOn)
	t.Cleanup(func() { tenant.SetMode(tenant.ModeOff) })

	// 隔离闸仍在：无租户 ctx 的裸写必须 fail-closed（本修复不得放宽隔离本身）
	if err := database.WithContext(context.Background()).
		Create(&model.LLMProvider{Name: "bare-write-probe", Model: "m"}).Error; err == nil {
		t.Fatal("on 模式无租户 ctx 的裸写必须 fail-closed（隔离不能被本修复放宽）")
	}

	// 装配层写库（system scope）必须成功
	n, err := savePlatformProvidersViaRepo(database, []platform.PlatformLLMProvider{
		{ProviderName: "platform-sensenova", DisplayName: "SenseNova（平台垫资）",
			Model: "deepseek-v4-flash", APIType: "openai", Vendor: "sensenova", PlanCode: "starter"},
	})
	if err != nil {
		t.Fatalf("装配期（无租户 ctx）同步平台 provider 必须在 system scope 下成功: %v", err)
	}
	if n != 1 {
		t.Fatalf("应同步 1 条平台 provider，实际 %d", n)
	}

	var got model.LLMProvider
	if err := database.WithContext(tenant.WithSystemScope(context.Background())).
		Where("name = ?", "platform-sensenova").First(&got).Error; err != nil {
		t.Fatalf("平台 provider 未落库: %v", err)
	}
	if got.Source != model.LLMSourcePlatform {
		t.Fatalf("落库行的 source 应为 platform，实际 %q", got.Source)
	}
	// 归属默认租户：dispatcher 的 LoadProvidersFromDB 只装 tenant_id=1，
	// 写到别的租户上会变成「写进去但读不出来」（provider 永远不可见）。
	if got.TenantID != tenant.DefaultTenantID {
		t.Fatalf("落库行应归属默认租户 %d，实际 %d（否则 dispatcher 读不出来）",
			tenant.DefaultTenantID, got.TenantID)
	}
	if got.PlanCode != "starter" {
		t.Fatalf("落库行的 plan_code 应为 starter（决定计价单价），实际 %q", got.PlanCode)
	}
}

// TestSavePlatformProviders_ExplicitTenantIDSelfAsserts 钉死「显式 tenant_id」这条守卫。
//
// 生产红因是**行自证**缺失：原文构造 LLMProvider 时不写 TenantID，
// 于是隔离 callback 的 fillOneForTenant 走 !bound 分支、行上又没有显式
// tenant_id 可自证 ⇒ tenantGuardFail ⇒
//
//	[tenant] 表 llm_providers 需要租户上下文但 ctx 未绑定（create）
//
// 本用例把守卫摘掉（模拟原文：不写 TenantID），必须红成**原来那句**；
// 断言的是"落库行必须带显式 tenant_id"这个不变量，而不是某条具体实现。
func TestSavePlatformProviders_ExplicitTenantIDSelfAsserts(t *testing.T) {
	database := testutil.NewTestDB(t, &model.LLMProvider{})
	tenant.SetMode(tenant.ModeOn)
	t.Cleanup(func() { tenant.SetMode(tenant.ModeOff) })

	// 模拟原文（不写 TenantID）：在无租户 ctx 下必须被隔离 fail-closed 拒绝，
	// 报错原文即生产日志那一句。
	row := model.LLMProvider{Name: "no-tenant-id-probe", Model: "m", Source: model.LLMSourcePlatform}
	err := database.WithContext(context.Background()).Create(&row).Error
	if err == nil {
		t.Fatal("不写 tenant_id 的装配期写入必须被隔离 fail-closed 拒绝（否则就重现不了生产红因）")
	}
	if !strings.Contains(err.Error(), "需要租户上下文但 ctx 未绑定") {
		t.Fatalf("红因文案应与生产日志一致（需要租户上下文但 ctx 未绑定），实际 %q", err.Error())
	}

	// 带显式 tenant_id（行自证）：同一 ctx 下必须放行
	ok := model.LLMProvider{
		TenantID: tenant.DefaultTenantID,
		Name:     "with-tenant-id-probe", Model: "m", Source: model.LLMSourcePlatform,
	}
	if err := database.WithContext(context.Background()).Create(&ok).Error; err != nil {
		t.Fatalf("带显式 tenant_id 的行自证写入应放行: %v", err)
	}
	// 回读同样要能定位到该行：带 tenant ctx 的读是隔离注入路径
	var got model.LLMProvider
	if err := database.WithContext(tenant.WithTenantID(context.Background(), tenant.DefaultTenantID)).
		Where("name = ?", "with-tenant-id-probe").First(&got).Error; err != nil {
		t.Fatal(err)
	}
	if got.TenantID != tenant.DefaultTenantID {
		t.Fatalf("行自证写入的 tenant_id 应为 %d，实际 %d", tenant.DefaultTenantID, got.TenantID)
	}
}

// TestSavePlatformProviders_OnConflictTargetMatchesIndex 钉死冲突目标形状：
// tenancyidx.Normalize 把租户表的唯一索引复合化为 (tenant_id, name)，
// 写 ON CONFLICT (name) 会与库里实际索引匹配不上 → 42P10
// 「there is no unique or exclusion constraint matching the ON CONFLICT specification」。
//
// 这条是上面那条的**第二道**拦路虎：只修 system scope 不修冲突目标，落库照样失败
// （生产实测该错误）。回归时两条一起钉。
func TestSavePlatformProviders_OnConflictTargetMatchesIndex(t *testing.T) {
	database := testutil.NewTestDB(t, &model.LLMProvider{})
	tenant.SetMode(tenant.ModeOn)
	t.Cleanup(func() { tenant.SetMode(tenant.ModeOff) })

	row := platform.PlatformLLMProvider{
		ProviderName: "platform-conflict-probe", DisplayName: "冲突目标探针",
		Model: "m", APIType: "openai",
	}
	if _, err := savePlatformProvidersViaRepo(database, []platform.PlatformLLMProvider{row}); err != nil {
		t.Fatalf("首次落库失败: %v", err)
	}
	// 第二次：走冲突分支（DO UPDATE）。冲突目标不匹配索引时这里会报 42P10。
	row.DisplayName = "改名了"
	if _, err := savePlatformProvidersViaRepo(database, []platform.PlatformLLMProvider{row}); err != nil {
		t.Fatalf("冲突分支失败（ON CONFLICT 目标与实际索引形状不匹配）: %v", err)
	}

	var count int64
	if err := database.WithContext(tenant.WithSystemScope(context.Background())).
		Model(&model.LLMProvider{}).Where("name = ?", "platform-conflict-probe").Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("重复同步应只有 1 行，实际 %d 行", count)
	}
	var got model.LLMProvider
	if err := database.WithContext(tenant.WithSystemScope(context.Background())).
		Where("name = ?", "platform-conflict-probe").First(&got).Error; err != nil {
		t.Fatal(err)
	}
	if got.DisplayName != "改名了" {
		t.Fatalf("冲突分支应更新元信息，实际 display_name=%q", got.DisplayName)
	}
}

// TestSavePlatformProviders_KeepsMerchantAPIKey 钉死「不动商户自己填的 api_key」：
// 平台下发的是元信息，**不含 Key**。若同步把商户自带的密钥清了，
// provider 从 platform 转回 self 就废了（商户要重新去厂商后台抄密钥）。
func TestSavePlatformProviders_KeepsMerchantAPIKey(t *testing.T) {
	database := testutil.NewTestDB(t, &model.LLMProvider{})
	tenant.SetMode(tenant.ModeOn)
	t.Cleanup(func() { tenant.SetMode(tenant.ModeOff) })

	// 商户自带的 provider（source=self，自带 Key）
	if err := database.WithContext(tenant.WithSystemScope(context.Background())).
		Create(&model.LLMProvider{
			TenantID: tenant.DefaultTenantID,
			Name:     "merchant-own",
			Model:    "own-model",
			APIKey:   "sk-merchant-own-secret",
			Source:   model.LLMSourceSelf,
			Enabled:  true,
		}).Error; err != nil {
		t.Fatalf("seed 商户自带 provider 失败: %v", err)
	}

	// 平台下发同名 provider（不含 Key）
	if _, err := savePlatformProvidersViaRepo(database, []platform.PlatformLLMProvider{
		{ProviderName: "merchant-own", Model: "platform-model", APIType: "openai"},
	}); err != nil {
		t.Fatalf("同步失败: %v", err)
	}

	var got model.LLMProvider
	if err := database.WithContext(tenant.WithSystemScope(context.Background())).
		Where("name = ?", "merchant-own").First(&got).Error; err != nil {
		t.Fatal(err)
	}
	if got.APIKey != "sk-merchant-own-secret" {
		t.Fatalf("同步不得清空商户自带的 api_key，实际 %q", got.APIKey)
	}
	if got.Source != model.LLMSourcePlatform {
		t.Fatalf("同名下发后 source 应更新为 platform，实际 %q", got.Source)
	}
}
