package service

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"hivemtk-user/internal/config"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/db"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/pkg/testutil"

	"gorm.io/gorm"
)

// staleSyncStub 造一个"商户级"平台：active_keys 只含基础模块，不含付费模块。
//
// 这正是线上后台周期 runner 看到的形态 —— 它没有租户绑定，sync 请求带
// ?tenant_code=（空串），平台按商户级行下发名单。
func setupStaleScopeStub(t *testing.T) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/merchant-api/modules/sync":
			_ = json.NewEncoder(w).Encode(map[string]any{"code": 200, "msg": "", "data": map[string]any{
				"merchant_key": "MKE2E",
				"active_keys":  []string{"core"},
				"menu_keys":    []string{},
				"catalog": []map[string]any{
					{"module_key": "core", "name": "core", "category": "base",
						"owned": true, "owned_status": "active", "owned_source": "default_base", "menu_keys": []string{}},
				},
			}})
		case "/merchant-api/orders":
			_ = json.NewEncoder(w).Encode(map[string]any{"code": 200, "msg": "", "data": map[string]any{
				"total": 0, "list": []map[string]any{},
			}})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)

	withPlatformEndpointCfg(t, &config.PlatformConfig{APIURL: srv.URL, Secret: "test-secret"})
}

func setupStaleScopeDB(t *testing.T) *gorm.DB {
	t.Helper()
	database := testutil.NewTestDB(t,
		&model.Tenant{}, &model.TenantModule{}, &model.ModuleSyncLog{}, &model.ModuleOrder{},
		&model.TenantSubscriptionMirror{})
	db.SetTestDB(database)
	t.Cleanup(func() {
		sysCtx := tenant.WithSystemScope(context.Background())
		for _, m := range []any{&model.Tenant{}, &model.TenantModule{}, &model.ModuleSyncLog{}, &model.ModuleOrder{}, &model.TenantSubscriptionMirror{}} {
			if err := database.Session(&gorm.Session{AllowGlobalUpdate: true}).WithContext(sysCtx).Delete(m).Error; err != nil {
				t.Logf("清理 %T 失败: %v", m, err)
			}
		}
	})
	return database
}

// TestSync_MerchantLevelRound_MustNotDisableTenantPaidModules 钉住「商户级那轮
// sync 不得把租户级已开通的付费模块标成 disabled」。
//
// 线上症状（2026-10-22）：m1 买过 reach-auto 并已开通（平台端 t578fub 名下
// reach-auto=active），用户端「我的模块」一度显示已开通，但市场页却显示未拥有。
// 根因：后台周期 runner 没有租户绑定 ⇒ sync 请求带 tenant_code=（空串）⇒ 平台
// 按**商户级** active_keys 下发（不含 reach-auto）⇒ 用户端 stale 判定
// （"本地存在但平台不再下发 ⇒ 标 disabled"）拿这份**商户级**名单去遍历
// **全租户**的行 ⇒ 把租户级已开通的付费模块误标为 disabled。
//
// 正确的判据：stale 判定只在**本轮同步覆盖到的租户**范围内做；商户级那轮
// 无权判定任何租户行的存废。
func TestSync_MerchantLevelRound_MustNotDisableTenantPaidModules(t *testing.T) {
	withTenantMode(t, tenant.ModeOn)
	t.Setenv("PLATFORM_ENABLED", "true")
	t.Setenv("PLATFORM_MERCHANT_KEY", "MKE2E")

	database := setupStaleScopeDB(t)
	setupStaleScopeStub(t)

	tn := &model.Tenant{Code: "tSTALE1", Name: "租户一"}
	if err := database.Create(tn).Error; err != nil {
		t.Fatalf("写入租户行: %v", err)
	}
	other := &model.Tenant{Code: "tSTALE2", Name: "租户二"}
	if err := database.Create(other).Error; err != nil {
		t.Fatalf("写入租户行: %v", err)
	}

	// 租户一已开通一个付费模块（平台端该租户名下有这行）
	tenantCtx := tenant.OnlyTenant(context.Background(), tn.ID)
	if err := database.WithContext(tenantCtx).Create(&model.TenantModule{
		ModuleKey:  "reach-auto",
		Status:     model.TenantModuleActive,
		Source:     model.TenantModuleSourcePurchase,
		PriceCents: 100,
	}).Error; err != nil {
		t.Fatalf("写入租户一已开通的付费模块: %v", err)
	}

	// 后台周期 runner：无租户绑定（=商户级那轮）
	svc := NewModuleCatalogService()
	if _, err := svc.Sync(context.Background()); err != nil {
		t.Fatalf("商户级 Sync 失败: %v", err)
	}

	var got model.TenantModule
	if err := database.WithContext(tenantCtx).
		Where("module_key = ?", "reach-auto").First(&got).Error; err != nil {
		t.Fatalf("回查租户一的模块行失败: %v", err)
	}
	if got.Status != model.TenantModuleActive {
		t.Errorf("商户级那轮 sync 不得把租户级已开通的付费模块标成 %s（实际 %s）："+
			"stale 判据必须按租户作用域，不能用商户级 active_keys 遍历全租户的行",
			model.TenantModuleDisabled, got.Status)
	}
}

// TestSync_TenantRound_StillDisablesRevokedModule 正向用例：撤销语义不能被
// 上面那条「商户级无权判定」改没了 —— 租户自己的那轮同步里，平台不再下发
// 的模块仍必须被显式失效（否则「撤销」永远不生效）。
//
// 判据的完整形状：**按租户作用域**。商户级那轮不判；租户级那轮只判该租户。
func TestSync_TenantRound_StillDisablesRevokedModule(t *testing.T) {
	withTenantMode(t, tenant.ModeOn)
	t.Setenv("PLATFORM_ENABLED", "true")
	t.Setenv("PLATFORM_MERCHANT_KEY", "MKE2E")

	database := setupStaleScopeDB(t)
	setupTenantScopeStub(t, []string{"core"}) // 平台只下发 core

	tn := &model.Tenant{Code: "tSTALE3", Name: "租户三"}
	if err := database.Create(tn).Error; err != nil {
		t.Fatalf("写入租户行: %v", err)
	}
	tenantCtx := tenant.OnlyTenant(context.Background(), tn.ID)
	if err := database.WithContext(tenantCtx).Create(&model.TenantModule{
		ModuleKey:  "reach-auto",
		Status:     model.TenantModuleActive,
		Source:     model.TenantModuleSourcePurchase,
		PriceCents: 100,
	}).Error; err != nil {
		t.Fatalf("写入租户三已开通的付费模块: %v", err)
	}

	svc := NewModuleCatalogService()
	if _, err := svc.Sync(tenantCtx); err != nil {
		t.Fatalf("租户级 Sync 失败: %v", err)
	}

	var got model.TenantModule
	if err := database.WithContext(tenantCtx).
		Where("module_key = ?", "reach-auto").First(&got).Error; err != nil {
		t.Fatalf("回查失败: %v", err)
	}
	if got.Status != model.TenantModuleDisabled {
		t.Errorf("平台已不再下发的模块应被显式失效（%s），实际 %s：撤销语义不能被"+
			"「商户级无权判定」这条改没了", model.TenantModuleDisabled, got.Status)
	}
}

// setupTenantScopeStub 造一个按租户下发的平台桩（active_keys 可配）。
func setupTenantScopeStub(t *testing.T, activeKeys []string) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/merchant-api/modules/sync":
			catalog := []map[string]any{}
			for _, k := range activeKeys {
				catalog = append(catalog, map[string]any{
					"module_key": k, "name": k, "category": "base",
					"owned": true, "owned_status": "active", "owned_source": "default_base",
					"menu_keys": []string{},
				})
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"code": 200, "msg": "", "data": map[string]any{
				"merchant_key": "MKE2E",
				"active_keys":  activeKeys,
				"menu_keys":    []string{},
				"catalog":      catalog,
			}})
		case "/merchant-api/orders":
			_ = json.NewEncoder(w).Encode(map[string]any{"code": 200, "msg": "", "data": map[string]any{
				"total": 0, "list": []map[string]any{},
			}})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)

	withPlatformEndpointCfg(t, &config.PlatformConfig{APIURL: srv.URL, Secret: "test-secret"})
}

// TestSync_TenantRound_LandsPaidModuleOnRequestingTenant 钉住「收费模块的开通
// 落点必须是本轮同步的那个租户」。
//
// 线上症状（2026-10-22）：m1 与 m2 都买过 reach-auto。平台的 active_keys 早已
// 按租户分开下发（m1 拿到的名单含 reach-auto），但用户端落点归并用的是
// purchaseTenants 的「首见即最新」判据 —— 跨租户订单里**最新那一笔**的租户
// 赢了，于是 m1 的那轮 sync 把 reach-auto 写到了 **m2 名下**，m1 自己的行
// 停在 disabled：m1 市场页显示未拥有，而 m2 白得一个模块。
//
// 判据：平台端 active_keys 已经是「本租户的名单」⇒ 落点就该是本轮租户；
// 只有商户级那轮（无租户绑定）才需要从订单反查落点。
func TestSync_TenantRound_LandsPaidModuleOnRequestingTenant(t *testing.T) {
	withTenantMode(t, tenant.ModeOn)
	t.Setenv("PLATFORM_ENABLED", "true")
	t.Setenv("PLATFORM_MERCHANT_KEY", "MKE2E")

	database := setupStaleScopeDB(t)
	setupTenantScopeStub(t, []string{"core", "reach-auto"})

	tenantA := &model.Tenant{Code: "tLAND1", Name: "租户甲"}
	if err := database.Create(tenantA).Error; err != nil {
		t.Fatalf("写入租户行: %v", err)
	}
	tenantB := &model.Tenant{Code: "tLAND2", Name: "租户乙"}
	if err := database.Create(tenantB).Error; err != nil {
		t.Fatalf("写入租户行: %v", err)
	}

	// 两个租户都买过 reach-auto，**乙的订单更新**（首见即最新会选中乙）
	writeOrder := func(tn *model.Tenant, orderNo string, at time.Time) {
		items := `[{"item_key":"reach-auto","quantity":1,"snapshot":"{\"price_cents\":100}","item_name":"营销自动化","item_type":"module","amount_cents":100,"unit_price_cents":100}]`
		ord := &model.ModuleOrder{
			ID: "id-" + orderNo, OrderNo: orderNo, ClientOrderNo: orderNo, TenantID: tn.ID,
			OrderType: "module", Status: model.ModuleOrderFulfilled,
			TotalAmountCents: 100, Currency: "CNY", Items: items,
			CreatedAt: at, UpdatedAt: at,
		}
		if err := database.WithContext(tenant.WithSystemScope(context.Background())).Create(ord).Error; err != nil {
			t.Fatalf("写入订单 %s: %v", orderNo, err)
		}
	}
	base := time.Now().Add(-time.Hour)
	writeOrder(tenantA, "ORD-A-OLD", base)
	writeOrder(tenantB, "ORD-B-NEW", time.Now()) // 更新 ⇒ 首见即最新会选中乙

	svc := NewModuleCatalogService()
	ctxA := tenant.OnlyTenant(context.Background(), tenantA.ID)
	if _, err := svc.Sync(ctxA); err != nil {
		t.Fatalf("租户甲 Sync 失败: %v", err)
	}

	// 甲名下必须有 reach-auto 且为 active
	var gotA model.TenantModule
	if err := database.WithContext(ctxA).
		Where("module_key = ?", "reach-auto").First(&gotA).Error; err != nil {
		t.Fatalf("租户甲名下应有 reach-auto 的开通行（平台名单已含它）: %v", err)
	}
	if gotA.Status != model.TenantModuleActive {
		t.Errorf("租户甲的 reach-auto 应为 active，实际 %s", gotA.Status)
	}

	// 乙名下**不应**因为甲这轮 sync 多出 reach-auto
	var nB int64
	if err := database.WithContext(tenant.OnlyTenant(context.Background(), tenantB.ID)).
		Model(&model.TenantModule{}).Where("module_key = ?", "reach-auto").Count(&nB).Error; err != nil {
		t.Fatalf("统计租户乙的模块行失败: %v", err)
	}
	if nB > 0 {
		t.Errorf("租户甲这轮 sync 不该往租户乙名下写 reach-auto（实写 %d 行）："+
			"落点必须是本轮同步的租户，不是订单里最新那一笔的租户", nB)
	}
}

// setupSubscriptionMirrorStub 平台桩：模块目录为空，订阅段固定顺延到 30 天后。
// 用于钉住「哪一轮 sync 有权写订阅镜像」。
func setupSubscriptionMirrorStub(t *testing.T, periodEnd time.Time) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/merchant-api/modules/sync":
			_ = json.NewEncoder(w).Encode(map[string]any{"code": 200, "msg": "", "data": map[string]any{
				"merchant_key": "MKE2E",
				"active_keys":  []string{},
				"menu_keys":    []string{},
				"catalog":      []any{},
				"subscription": map[string]any{
					"status":             "active",
					"current_period_end": periodEnd.UTC().Format(time.RFC3339),
					"price_cents":        900,
				},
			}})
		case "/merchant-api/orders":
			_ = json.NewEncoder(w).Encode(map[string]any{"code": 200, "msg": "", "data": map[string]any{
				"total": 0, "list": []map[string]any{},
			}})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	withPlatformEndpointCfg(t, &config.PlatformConfig{APIURL: srv.URL, Secret: "{{SECRET_m1rr0r}}"})
}

// TestSync_MerchantLevelRound_MustNotWriteSubscriptionMirror 钉住：无租户绑定的
// 商户级同步（后台 10 分钟 runner）**不得**写订阅镜像。
//
// 线上症状（2026-11-04）：商户级那轮拿到的是 tenant_code=” 的旧订阅行，却被
// 写进 SystemScope 的默认租户镜像 —— tenant 1 真实 active 至 2027-01-17，镜像
// 却显示 trial 至当天 15:00。订阅镜像按租户一行，只有该租户自己的请求有权写。
func TestSync_MerchantLevelRound_MustNotWriteSubscriptionMirror(t *testing.T) {
	withTenantMode(t, tenant.ModeOn)
	t.Setenv("PLATFORM_ENABLED", "true")
	t.Setenv("PLATFORM_MERCHANT_KEY", "MKE2E")

	database := setupStaleScopeDB(t)
	setupSubscriptionMirrorStub(t, time.Now().Add(30*24*time.Hour))

	// 后台 runner 的上下文：SystemScope、无租户绑定。
	sysCtx := tenant.WithSystemScope(context.Background())
	if _, err := NewModuleCatalogService().Sync(sysCtx); err != nil {
		t.Fatalf("商户级 Sync 失败: %v", err)
	}

	var n int64
	if err := database.WithContext(sysCtx).Model(&model.TenantSubscriptionMirror{}).Count(&n).Error; err != nil {
		t.Fatalf("回查订阅镜像失败: %v", err)
	}
	if n != 0 {
		t.Fatalf("商户级同步不得写订阅镜像（写入了 %d 行）：tenant_code='' 的订阅段属于"+
			"商户级旧行，写进默认租户镜像会覆盖该租户真实付费状态", n)
	}
}

// TestSync_TenantRound_WritesOwnSubscriptionMirror 正向用例：带着租户绑定的
// 同步必须把平台下发的该租户订阅段写进**这一租户**的镜像行。
func TestSync_TenantRound_WritesOwnSubscriptionMirror(t *testing.T) {
	withTenantMode(t, tenant.ModeOn)
	t.Setenv("PLATFORM_ENABLED", "true")
	t.Setenv("PLATFORM_MERCHANT_KEY", "MKE2E")

	database := setupStaleScopeDB(t)
	periodEnd := time.Now().Add(30 * 24 * time.Hour).UTC().Truncate(time.Second)
	setupSubscriptionMirrorStub(t, periodEnd)

	tn := &model.Tenant{Code: "tMIRROR1", Name: "镜像租户"}
	if err := database.Create(tn).Error; err != nil {
		t.Fatalf("写入租户行: %v", err)
	}
	tenantCtx := tenant.OnlyTenant(context.Background(), tn.ID)
	if _, err := NewModuleCatalogService().Sync(tenantCtx); err != nil {
		t.Fatalf("租户级 Sync 失败: %v", err)
	}

	var row model.TenantSubscriptionMirror
	if err := database.WithContext(tenant.WithSystemScope(context.Background())).
		Where("id = ?", tn.ID).First(&row).Error; err != nil {
		t.Fatalf("该租户的订阅镜像未落库: %v", err)
	}
	if row.CurrentPeriodEnd == nil || !row.CurrentPeriodEnd.Equal(periodEnd) {
		t.Fatalf("到期时间未按平台段写回，got %v want %v", row.CurrentPeriodEnd, periodEnd)
	}
	if row.Status != model.SubscriptionStatusActive {
		t.Fatalf("status 应按本地惰性判定为 active，got %s", row.Status)
	}
}
