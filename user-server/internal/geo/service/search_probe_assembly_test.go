// search_probe_assembly_test.go 探针引擎装配的仓储注入契约（PG 定向，8217）。
//
// AV-04c 批次3：NewEngineProbesFromDB 由 *gorm.DB 形参改收
// mainrepo.LLMProviderRepository（ListEnabled）+ GeoConfigRepository（GetBrandProfile）。
// 这里锁定装配语义：enabled 过滤、sort_order 轮转序、空 model/本地端点排除、
// 品牌提示词两列投影，以及 NewEngineProbesForContext 的租户门。
package service

import (
	"context"
	"testing"

	"hivemtk-user/internal/geo/model"
	"hivemtk-user/internal/geo/repository"
	hivemodel "hivemtk-user/internal/model"
	pkgdb "hivemtk-user/internal/pkg/db"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/pkg/testutil"
	mainrepo "hivemtk-user/internal/repository"

	"gorm.io/gorm"
)

func probeAssemblySetupDB(t *testing.T) *gorm.DB {
	t.Helper()
	db := testutil.NewTestDB(t, &model.GeoConfig{}, &hivemodel.LLMProvider{})
	if db == nil {
		t.Fatal("测试库不可达：探针装配契约判不了（不 Skip，跳过等于没锁）")
	}
	return db
}

// testGlobalDBSwap 换挂/还原全局句柄（NewEngineProbesForContext 的无参构造走全局）。
func testGlobalDBSwap(t *testing.T, db *gorm.DB) *gorm.DB {
	t.Helper()
	prev := pkgdb.GetDB()
	pkgdb.SetTestDB(db)
	return prev
}

func cloudProvider(name, model_, baseURL string, sort int, enabled bool) *hivemodel.LLMProvider {
	return &hivemodel.LLMProvider{Name: name, Model: model_, BaseURL: baseURL,
		SortOrder: sort, Enabled: enabled, Source: hivemodel.LLMSourceSelf}
}

func TestNewEngineProbesFromDB_OrderAndFilters(t *testing.T) {
	db := probeAssemblySetupDB(t)
	ctx := context.Background()

	if err := db.WithContext(ctx).Create(&model.GeoConfig{
		BrandName:        "蜂巢",
		BrandDescription: "AI 工作流平台",
	}).Error; err != nil {
		t.Fatalf("创建 geo_config 失败: %v", err)
	}
	for _, p := range []*hivemodel.LLMProvider{
		cloudProvider("sort1-云", "m-1", "https://api1.example.com", 1, true),   // 唯一有效 → 第 1
		cloudProvider("sort5-云", "m-5", "https://api5.example.com", 5, true),   // 有效 → 第 2
		cloudProvider("sort0-空model", "", "https://api0.example.com", 0, true), // 空 model → 排除
		cloudProvider("sort2-本地", "m-2", "http://127.0.0.1:9999", 2, true),     // 本地端点 → 排除
		cloudProvider("sort9-停用", "m-9", "https://api9.example.com", 9, false), // enabled=false → 排除
	} {
		if err := db.WithContext(ctx).Create(p).Error; err != nil {
			t.Fatalf("创建 provider %s 失败: %v", p.Name, err)
		}
	}

	probes := NewEngineProbesFromDB(ctx,
		mainrepo.NewLLMProviderRepositoryWithDB(db),
		repository.NewGeoConfigRepositoryWithDB(db))
	if len(probes) != 2 {
		names := make([]string, 0, len(probes))
		for _, p := range probes {
			names = append(names, p.Name())
		}
		t.Fatalf("应装配 2 个云端探针（空 model/本地/停用排除），实际 %v", names)
	}
	if probes[0].Name() != "sort1-云" || probes[1].Name() != "sort5-云" {
		t.Fatalf("应按 sort_order ASC 轮转，实际 %s, %s", probes[0].Name(), probes[1].Name())
	}

	ep, ok := probes[0].(*llmEndpointProbe)
	if !ok {
		t.Fatalf("云端 provider 应装配为 llmEndpointProbe，实际 %T", probes[0])
	}
	if ep.brandHint != "蜂巢：AI 工作流平台" {
		t.Fatalf("品牌提示词应为两列投影拼接，实际 %q", ep.brandHint)
	}
}

func TestNewEngineProbesFromDB_BrandHintFallbacks(t *testing.T) {
	db := probeAssemblySetupDB(t)
	ctx := context.Background()

	// 只有名没有描述：brandHint = 名（改前 else-if 分支）。
	if err := db.WithContext(ctx).Create(&model.GeoConfig{BrandName: "蜂巢"}).Error; err != nil {
		t.Fatalf("创建 geo_config 失败: %v", err)
	}
	if err := db.WithContext(ctx).Create(cloudProvider("云", "m", "https://api.example.com", 0, true)).Error; err != nil {
		t.Fatalf("创建 provider 失败: %v", err)
	}
	probes := NewEngineProbesFromDB(ctx,
		mainrepo.NewLLMProviderRepositoryWithDB(db),
		repository.NewGeoConfigRepositoryWithDB(db))
	if len(probes) != 1 {
		t.Fatalf("应装配 1 个探针，实际 %d", len(probes))
	}
	if ep := probes[0].(*llmEndpointProbe); ep.brandHint != "蜂巢" {
		t.Fatalf("仅有名时 brandHint 应等于名，实际 %q", ep.brandHint)
	}

	// 配置读不到（空表）⇒ fail-open：brandHint 置空，探针清单照常装配。
	db2 := probeAssemblySetupDB(t)
	if err := db2.WithContext(ctx).Create(cloudProvider("云2", "m", "https://api2.example.com", 0, true)).Error; err != nil {
		t.Fatalf("创建 provider 失败: %v", err)
	}
	probes2 := NewEngineProbesFromDB(ctx,
		mainrepo.NewLLMProviderRepositoryWithDB(db2),
		repository.NewGeoConfigRepositoryWithDB(db2))
	if len(probes2) != 1 {
		t.Fatalf("配置缺失不得拦探针装配，实际 %d", len(probes2))
	}
	if ep := probes2[0].(*llmEndpointProbe); ep.brandHint != "" {
		t.Fatalf("配置读不到应 fail-open 置空提示词，实际 %q", ep.brandHint)
	}
}

func TestNewEngineProbesForContext_TenantGate(t *testing.T) {
	db := probeAssemblySetupDB(t)
	prev := testGlobalDBSwap(t, db)

	tenant.SetMode(tenant.ModeOff)
	t.Cleanup(func() {
		tenant.ResetRegistryForTest()
		tenant.SetMode(tenant.ModeOff)
		pkgdb.SetTestDB(prev)
	})
	ctx := context.Background()

	if err := db.WithContext(ctx).Create(cloudProvider("off-云", "m", "https://api.example.com", 0, true)).Error; err != nil {
		t.Fatalf("创建 provider 失败: %v", err)
	}

	// off 模式：全局句柄装配（NewEngineProbesForContext 内部仓储无参构造）。
	if probes := NewEngineProbesForContext(ctx); len(probes) != 1 {
		t.Fatalf("off 模式应装配 1 个探针，实际 %d", len(probes))
	}

	// on 模式无租户绑定：宁可无探针，也不跨租户取凭据（门语义）。
	tenant.SetMode(tenant.ModeOn)
	if probes := NewEngineProbesForContext(context.Background()); len(probes) != 0 {
		t.Fatalf("on 模式无租户 ctx 应返回空清单，实际 %d", len(probes))
	}

	// on 模式带租户 ctx：经隔离 callback 只读本租户 provider。
	const tid = uint(371)
	if err := db.WithContext(tenant.WithTenantID(ctx, tid)).Create(
		cloudProvider("tenant-云", "m", "https://api2.example.com", 0, true)).Error; err != nil {
		t.Fatalf("创建租户 provider 失败: %v", err)
	}
	probes := NewEngineProbesForContext(tenant.WithTenantID(ctx, tid))
	if len(probes) != 1 || probes[0].Name() != "tenant-云" {
		t.Fatalf("租户 ctx 应只装配本租户 provider，实际 %+v", probes)
	}
}
