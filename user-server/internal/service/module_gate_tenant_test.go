package service

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"gorm.io/gorm"

	"hivemtk-user/internal/config"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/pkg/testutil"
	"hivemtk-user/internal/repository"
)

// 模块闸的租户语义测试（per-tenant 缓存 / 空态单口径 / 激活绑下单租户）。
//
// 与 module_gate_test.go 那组互补：那组测"闸的数据对不对"，这里测
// **闸按哪个租户判**、以及**激活落在哪个租户** —— 这两件事任一错了，
// 表现都是"租户 A 的钱买了租户 B 的能力"（或反过来白送），同样静默。

// clearModuleTablesOnMode on 模式下清空模块三表。
//
// 无绑定的删除会被隔离 callback fail-closed，必须挂 SystemScope。
// 调用顺序约定：先 withTenantMode（其还原 cleanup 先注册、后执行），
// 再本函数 —— 于是本 cleanup 跑的时候 mode 仍是 on。
func clearModuleTablesOnMode(t *testing.T, database *gorm.DB) {
	t.Helper()
	t.Cleanup(func() {
		for _, m := range []any{&model.TenantModule{}, &model.ModuleSyncLog{}, &model.ModuleOrder{}} {
			if err := database.Session(&gorm.Session{AllowGlobalUpdate: true}).
				WithContext(tenant.WithSystemScope(context.Background())).
				Delete(m).Error; err != nil {
				t.Logf("清理 %T 失败: %v", m, err)
			}
		}
	})
}

// TestModuleGateTwoTenantCacheIsolation 两租户缓存隔离：A 的开通状态绝不喂给 B。
//
// 修复前的失效链：闸只有**一份**全局 cache + 一个 bootstrapped —— 每个请求把
// 自己租户的行灌进同一份缓存，被下一个请求覆盖；在 gateCacheTTL（60s）内，
// 租户 B 会被拿租户 A 的开通集判定（付费闸对 B 静默失效或反向误拦）。
// 这里用三个租户钉住三个面：
//
//	A（默认租户，有 geo）/ B（有 reach-link）→ 双向交替判定，谁都不许串；
//	C（空镜像）→ base-only 判定（B-2 空态口径），且不许被 A/B 的行"带活"。
//
// 注意：必须经**带模型**的 NewTestDB 建连接 —— 只有那条路径会注册隔离
// callback 与租户登记表；不传模型的连接在 on 模式下等于没开隔离，用例会假绿。
func TestModuleGateTwoTenantCacheIsolation(t *testing.T) {
	withTenantMode(t, tenant.ModeOn)
	database := testutil.NewTestDB(t, &model.TenantModule{}, &model.ModuleSyncLog{}, &model.ModuleOrder{})
	clearModuleTablesOnMode(t, database)

	repo := repository.NewTenantModuleRepositoryWithDB(database)
	ctxA := tenant.WithTenantID(context.Background(), 1) // 默认租户
	ctxB := tenant.WithTenantID(context.Background(), 2)
	ctxC := tenant.WithTenantID(context.Background(), 3) // 空镜像租户

	now := time.Now()
	// A：基础包 + 已购 geo
	rowsA := append(baseTenantModuleRows(now), &model.TenantModule{
		ModuleKey: model.ModuleGEO, Status: model.TenantModuleActive,
		Source: model.TenantModuleSourcePurchase, ActivatedAt: now,
	})
	if err := repo.UpsertMany(ctxA, rowsA); err != nil {
		t.Fatalf("写入租户 1 镜像: %v", err)
	}
	// B：基础包 + 已购 reach-link（与 A 的付费模块错开，串缓存必然暴露）
	rowsB := append(baseTenantModuleRows(now), &model.TenantModule{
		ModuleKey: model.ModuleReachLink, Status: model.TenantModuleActive,
		Source: model.TenantModuleSourcePurchase, ActivatedAt: now,
	})
	if err := repo.UpsertMany(ctxB, rowsB); err != nil {
		t.Fatalf("写入租户 2 镜像: %v", err)
	}
	// C：不写任何行（on 模式 2+ 租户的典型空镜像）

	gate := NewModuleGateServiceWithRepo(repo)
	t.Cleanup(func() { model.SetModuleEnabledFilter(nil) })

	// ---- A 先载入 ----
	if !gate.IsEnabled(ctxA, model.ModuleGEO) {
		t.Fatal("租户 1 应看到自己已购的 geo")
	}
	if gate.IsEnabled(ctxA, model.ModuleReachLink) {
		t.Fatal("租户 1 不该看到租户 2 的 reach-link")
	}

	// ---- B 判定：不许拿到 A 的状态，且自己已购的要放行 ----
	if gate.IsEnabled(ctxB, model.ModuleGEO) {
		t.Fatal("租户 2 被拿租户 1 的开通集判定：跨租户串闸（全局 cache 回归）")
	}
	if !gate.IsEnabled(ctxB, model.ModuleReachLink) {
		t.Fatal("租户 2 应看到自己已购的 reach-link")
	}
	if !gate.IsEnabled(ctxB, model.ModuleCore) {
		t.Fatal("租户 2 的基础模块应放行")
	}

	// ---- C（空镜像）：base-only 口径，不许被 A/B 的行"带活" ----
	if !gate.IsEnabled(ctxC, model.ModuleCore) {
		t.Error("空镜像租户的基础模块应放行")
	}
	if gate.IsEnabled(ctxC, model.ModuleGEO) || gate.IsEnabled(ctxC, model.ModuleReachLink) {
		t.Error("空镜像租户被判成了 A/B 的开通集：base-only 口径失效")
	}
	if !gate.IsEnabled(ctxC, "") {
		t.Error("空 moduleKey（未标注路由）应放行")
	}
	if keys := gate.EnabledKeys(ctxC); len(keys) != len(model.BaseModuleKeys()) {
		t.Errorf("空镜像租户 EnabledKeys 应恰为基础包（%d 个），实际 %d 个: %v",
			len(model.BaseModuleKeys()), len(keys), keys)
	}

	// ---- 回读 A/B：C/B 的载入不许把 A 的条目覆盖掉 ----
	if !gate.IsEnabled(ctxA, model.ModuleGEO) {
		t.Error("租户 2/3 的载入覆盖了租户 1 的缓存条目（全局 cache 回归）")
	}
	if keys := gate.EnabledKeys(ctxA); !hasKey(keys, model.ModuleGEO) || hasKey(keys, model.ModuleReachLink) {
		t.Error("租户 1 的 EnabledKeys 串了租户 2 的 reach-link")
	}
	if !gate.IsEnabled(ctxB, model.ModuleReachLink) {
		t.Error("租户 1 的回读覆盖了租户 2 的缓存条目（全局 cache 回归）")
	}
}

func hasKey(m map[string]struct{}, k string) bool {
	_, ok := m[k]
	return ok
}

// TestModuleSyncActivatesPaidModuleOnOrderingTenant 激活必须绑**下单租户**（Fix C）。
//
// 事故形态：租户 2 在平台下单 → 平台确认收款 → 用户端同步把 owned 模块写进
// tenant_modules。写用的是 SystemScope ctx（列默认值 1）→ 行落**租户 1**：
// 租户 2 的购买永远不激活（4301），租户 1 反而白得一个付费模块 —— 钱是
// 租户 2 付的，能力是租户 1 拿的。
//
// 本用例走**真实链路**（桩平台：目录下发 + 订单状态确认），断言三件事：
//  1. reach-link 行只落在 tenant_id=2，且带 1 条不落 tenant_id=1；
//  2. 租户 2 同时拿到基础模块行（否则 boot=true 后基础能力会被判关）；
//  3. 闸判定：租户 2 见激活、租户 1 不见（同一实例交替读，缓存不许串）。
//
// TestModuleSyncExemptsGiftRowsFromStaleDisable 0 元赠送行不参与 stale 判定。
//
// 事故形态（003 oracle P1-3）：平台对 0 元模块的 owned 下发口径未两端定死时，
// 平台没把某 0 元模块算进 active_keys（存量商户升级前、离线期落的 gift 行），
// 下一轮同步的 stale-disable 会把免费能力整批误关 —— 而这些行从未被购买，
// 用户也无法通过任何界面恢复。
//
// 钉住两件事：gift 来源的 active 行不被失效；purchase 来源的行照常失效
// （撤销通道本身不能被这个豁免弄丢）。
func TestModuleSyncExemptsGiftRowsFromStaleDisable(t *testing.T) {
	withTenantMode(t, tenant.ModeOn)
	t.Setenv("PLATFORM_ENABLED", "true")
	t.Setenv("PLATFORM_MERCHANT_KEY", "MKE2E")

	// ---- 桩平台：只下发基础包（geo/approval 不在 active_keys 里）----
	catalog := make([]map[string]any, 0, len(model.BaseModuleKeys()))
	for _, k := range model.BaseModuleKeys() {
		catalog = append(catalog, map[string]any{
			"module_key": k, "name": k, "category": "base",
			"owned": true, "owned_status": "active", "owned_source": "default_base",
			"menu_keys": model.ModuleMenuBindings[k],
		})
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/merchant-api/modules/sync":
			_ = json.NewEncoder(w).Encode(map[string]any{"code": 200, "msg": "", "data": map[string]any{
				"merchant_key": "MKE2E",
				"active_keys":  model.BaseModuleKeys(),
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
	defer srv.Close()

	withPlatformEndpointCfg(t, &config.PlatformConfig{APIURL: srv.URL, Secret: "test-secret"})

	database := testutil.NewTestDB(t, &model.TenantModule{}, &model.ModuleSyncLog{}, &model.ModuleOrder{})
	clearModuleTablesOnMode(t, database)

	repo := repository.NewTenantModuleRepositoryWithDB(database)
	orderRepo := repository.NewModuleOrderRepositoryWithDB(database)
	sysCtx := tenant.WithSystemScope(context.Background())
	now := time.Now()
	if err := repo.UpsertMany(sysCtx, []*model.TenantModule{
		{ModuleKey: model.ModuleGEO, Status: model.TenantModuleActive,
			Source: model.TenantModuleSourceGift, ActivatedAt: now},
		{ModuleKey: model.ModuleApproval, Status: model.TenantModuleActive,
			Source: model.TenantModuleSourcePurchase, ActivatedAt: now},
	}); err != nil {
		t.Fatalf("写入 gift/purchase 行: %v", err)
	}

	svc := NewModuleCatalogServiceWithRepos(repo, orderRepo)
	// stale 判定只在「本轮有该租户专属名单」时做（module_catalog.go 的
	// reqTenantBound 门：商户级名单无权判定别人存废）⇒ ctx 必须绑请求租户。
	// 这两行写的是默认租户（未显式设 TenantID ⇒ 落 tenant 1），故绑 1。
	// a315e471 引入 reqTenantBound 时没同步更新本用例，导致 stale 判定整段没跑。
	reqCtx := tenant.WithTenantID(context.Background(), tenant.DefaultTenantID)
	if _, err := svc.Sync(reqCtx); err != nil {
		t.Fatalf("Sync 失败: %v", err)
	}

	geo, err := repo.Get(sysCtx, model.ModuleGEO)
	if err != nil || geo == nil {
		t.Fatalf("读 geo 行失败: geo=%v err=%v", geo, err)
	}
	if geo.Status != model.TenantModuleActive {
		t.Errorf("gift 行被 stale-disable 误伤：geo 状态应保持 active，实际 %q", geo.Status)
	}
	ap, err := repo.Get(sysCtx, model.ModuleApproval)
	if err != nil || ap == nil {
		t.Fatalf("读 approval 行失败: ap=%v err=%v", ap, err)
	}
	if ap.Status != model.TenantModuleDisabled {
		t.Errorf("purchase 行应照常被 stale-disable：approval 状态应为 disabled，实际 %q", ap.Status)
	}
}

func TestModuleSyncActivatesPaidModuleOnOrderingTenant(t *testing.T) {
	withTenantMode(t, tenant.ModeOn)
	t.Setenv("PLATFORM_ENABLED", "true")
	t.Setenv("PLATFORM_MERCHANT_KEY", "MKE2E")
	const orderNo = "MOD-T2-ACTIVATE-1"

	// ---- 桩平台：目录下发（基础包 + 已 owned 的 reach-link）+ 订单已履约 ----
	catalog := make([]map[string]any, 0, len(model.BaseModuleKeys())+1)
	for _, k := range model.BaseModuleKeys() {
		catalog = append(catalog, map[string]any{
			"module_key": k, "name": k, "category": "base",
			"owned": true, "owned_status": "active", "owned_source": "default_base",
			"menu_keys": model.ModuleMenuBindings[k],
		})
	}
	catalog = append(catalog, map[string]any{
		"module_key": model.ModuleReachLink, "name": "短链活码", "category": "paid",
		"price_cents": 38000, "owned": true, "owned_status": "active",
		"owned_source": "purchase", "menu_keys": model.ModuleMenuBindings[model.ModuleReachLink],
	})
	activeKeys := append([]string{}, model.BaseModuleKeys()...)
	activeKeys = append(activeKeys, model.ModuleReachLink)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/merchant-api/modules/sync":
			_ = json.NewEncoder(w).Encode(map[string]any{"code": 200, "msg": "", "data": map[string]any{
				"merchant_key": "MKE2E",
				"active_keys":  activeKeys,
				"menu_keys":    []string{},
				"catalog":      catalog,
			}})
		case "/merchant-api/orders":
			_ = json.NewEncoder(w).Encode(map[string]any{"code": 200, "msg": "", "data": map[string]any{
				"total": 1,
				"list":  []map[string]any{{"order_no": orderNo, "status": "fulfilled"}},
			}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	withPlatformEndpointCfg(t, &config.PlatformConfig{APIURL: srv.URL, Secret: "test-secret"})

	database := testutil.NewTestDB(t, &model.TenantModule{}, &model.ModuleSyncLog{}, &model.ModuleOrder{})
	clearModuleTablesOnMode(t, database)

	repo := repository.NewTenantModuleRepositoryWithDB(database)
	orderRepo := repository.NewModuleOrderRepositoryWithDB(database)

	// ---- 租户 2 下单（订单行带着下单租户的 tenant_id，与生产下单路径同形）----
	ordCtx := tenant.WithTenantID(context.Background(), 2)
	items, _ := json.Marshal([]map[string]any{{
		"item_type": "module", "item_key": model.ModuleReachLink,
		"item_name": "短链活码", "unit_price_cents": 38000,
		"quantity": 1, "amount_cents": 38000,
	}})
	if err := orderRepo.Upsert(ordCtx, &model.ModuleOrder{
		ID: "ord-t2-activate-1", TenantID: 2,
		OrderNo: orderNo, ClientOrderNo: "CLI-T2-ACTIVATE-1",
		OrderType: "module", Status: model.ModuleOrderPendingPayment,
		TotalAmountCents: 38000, Currency: "CNY", Items: string(items),
	}); err != nil {
		t.Fatalf("写入租户 2 的订单: %v", err)
	}

	// ---- 真实同步链路：状态确认（pending→fulfilled）→ 落点归并 → 激活 ----
	svc := NewModuleCatalogServiceWithRepos(repo, orderRepo)
	res, err := svc.Sync(context.Background())
	if err != nil {
		t.Fatalf("Sync 失败: %v", err)
	}
	if res.Degraded {
		t.Fatalf("不应降级: %s", res.Message)
	}

	// 1. reach-link 只落租户 2
	sysCtx := tenant.WithSystemScope(context.Background())
	rows, err := repo.ListAll(sysCtx)
	if err != nil {
		t.Fatalf("读全量镜像: %v", err)
	}
	rlTenants := map[uint]int{}
	baseTenants := map[uint]int{}
	for _, r := range rows {
		switch r.ModuleKey {
		case model.ModuleReachLink:
			rlTenants[r.TenantID]++
		default:
			if hasKey(baseModuleKeySet, r.ModuleKey) {
				baseTenants[r.TenantID]++
			}
		}
	}
	if rlTenants[2] != 1 {
		t.Errorf("reach-link 应恰有 1 行落在下单租户 2，实际 tenant_id=2 的行数 %d（全量分布 %v）",
			rlTenants[2], rlTenants)
	}
	if n := rlTenants[tenant.DefaultTenantID]; n != 0 {
		t.Errorf("默认租户白得了租户 2 购买的 reach-link（%d 行）：激活未绑下单租户", n)
	}
	if len(rlTenants) != 1 {
		t.Errorf("reach-link 的租户分布应只有 {2}，实际 %v", rlTenants)
	}

	// 2. 租户 2 同时拿到基础模块行（有行的租户必须有基础包）
	if baseTenants[2] != len(model.BaseModuleKeys()) {
		t.Errorf("租户 2 应补齐 %d 个基础模块行，实际 %d",
			len(model.BaseModuleKeys()), baseTenants[2])
	}
	if baseTenants[tenant.DefaultTenantID] != len(model.BaseModuleKeys()) {
		t.Errorf("默认租户应有 %d 个基础模块行，实际 %d",
			len(model.BaseModuleKeys()), baseTenants[tenant.DefaultTenantID])
	}

	// 3. 订单状态确实被刷新到 fulfilled（激活判据的上游）
	if ord, err := orderRepo.GetByOrderNo(sysCtx, orderNo); err != nil || ord == nil {
		t.Fatalf("读回落的订单失败: ord=%v err=%v", ord, err)
	} else if ord.Status != model.ModuleOrderFulfilled {
		t.Errorf("订单状态应刷新为 fulfilled，实际 %q", ord.Status)
	} else if ord.TenantID != 2 {
		t.Errorf("订单归属租户应在同步后保持 2，实际 %d", ord.TenantID)
	}

	// 4. 闸判定：同一实例交替读两个租户 —— 租户 2 见激活、租户 1 不见
	gate := NewModuleGateServiceWithRepo(repo)
	t.Cleanup(func() { model.SetModuleEnabledFilter(nil) })
	ctx1 := tenant.WithTenantID(context.Background(), 1)
	ctx2 := tenant.WithTenantID(context.Background(), 2)

	if !gate.IsEnabled(ctx2, model.ModuleReachLink) {
		t.Error("下单租户 2 的购买未在自己的闸里生效（激活没落对租户）")
	}
	if !gate.IsEnabled(ctx2, model.ModuleCore) {
		t.Error("租户 2 的基础模块应放行（激活必须连带基础包）")
	}
	if gate.IsEnabled(ctx1, model.ModuleReachLink) {
		t.Error("默认租户 1 的闸看到了租户 2 购买的模块：白送")
	}
	if !gate.IsEnabled(ctx1, model.ModuleCore) {
		t.Error("默认租户的基础模块应放行")
	}
	if keys := gate.EnabledKeys(ctx2); !hasKey(keys, model.ModuleReachLink) {
		t.Error("租户 2 的 EnabledKeys 应含 reach-link（菜单过滤同口径）")
	}
}

// TestModuleSyncProvisionsFreeModulesForRequestingTenant 无任何下单记录的租户
// 也必须拿到「基础包 + 0 元赠送模块」（线上事故：新注册商户 tenant_modules 恒空，
// 除基础外的页面一律 4301，菜单只剩骨架）。
//
// 修复前的失效链：Sync 的落点归并只给「本地有已付款订单」的租户建分组，
// 其余行全部落默认租户；于是**刚注册、还没买过任何东西的租户**拿不到任何行，
// 模块闸按其空镜像口径只放行基础包 —— 0 元模块（平台已按 gift 下发）也被判关。
//
// 钉住三件事：
//  1. 请求租户（18）拿到 base + 0 元模块，且赠送行来源标 gift；
//  2. 收费且未下单的模块（browser-automation）不落该租户（不白送）；
//  3. 默认租户的既有行为不变（商户级已 owned 行仍全量落默认租户）。
func TestModuleSyncProvisionsFreeModulesForRequestingTenant(t *testing.T) {
	withTenantMode(t, tenant.ModeOn)
	t.Setenv("PLATFORM_ENABLED", "true")
	t.Setenv("PLATFORM_MERCHANT_KEY", "MKE2E")
	const reqTenantID uint = 18

	catalog := make([]map[string]any, 0, len(model.BaseModuleKeys())+2)
	for _, k := range model.BaseModuleKeys() {
		catalog = append(catalog, map[string]any{
			"module_key": k, "name": k, "category": "base",
			"owned": true, "owned_status": "active", "owned_source": "default_base",
			"menu_keys": model.ModuleMenuBindings[k],
		})
	}
	// 0 元付费模块：平台视为赠送（owned=true，owned_source 显式给 gift）
	//
	// 2026-10-30 起 16 个付费模块都是 0 元（只有浏览器自动化收费），
	// 「0 元 ⇒ gift」是主路径；这里用 geo 显式压一遍，确保平台下发
	// owned_source=gift 时用户端落 source=gift 而不是 purchase。
	//
	// owned_source 必须显式给 "gift"：平台端对「0 元且已授予」的模块就是
	// 这么下发的（owned_flag=true 且 owned_source 空 ⇒ 平台补 "gift"，
	// 见 platform-server 的 MerchantMarket 构造）。桩漏了它，用户端
	// sourceOf() 就会按 paid 类目回落成 "purchase" ⇒ 断言看到的是错的值。
	catalog = append(catalog, map[string]any{
		"module_key": model.ModuleGEO, "name": "GEO 智能优化", "category": "paid",
		"price_cents": 0, "owned": true, "owned_status": "active",
		"owned_source": "gift",
		"menu_keys":    model.ModuleMenuBindings[model.ModuleGEO],
	})
	// 收费模块：平台**商户级**已 owned，但本租户没下单 ⇒ 平台按租户下发时
	// 它的 owned 状态是 disabled（线上实测：tenant 18 的 browser-automation
	// 就是 source=purchase / status=disabled），**不在本租户的 active_keys 里**。
	//
	// 桩若把它写成 owned=true+active_status=active，就会把它塞进 active_keys
	// ⇒ 用户端按「本轮租户的名单」把它落给请求租户（a315e471 修的正是这条：
	// 落点用本轮租户，不再用订单反查）⇒ 变成白送。
	// 浏览器自动化保持 ¥1（100 分）买断；测试只关心「收费」这个性质，不关心具体金额。
	catalog = append(catalog, map[string]any{
		"module_key": model.ModuleBrowserAutomation, "name": "浏览器自动化", "category": "paid",
		"price_cents": 100, "owned": false, "owned_status": "",
		"owned_source": "", "menu_keys": model.ModuleMenuBindings[model.ModuleBrowserAutomation],
	})
	// active_keys = 本租户**已授予**的名单（平台端口径：owned=true 的模块）。
	// 本租户已授予：7 个 base + geo（0 元 gift）。browser-automation 未购买 ⇒ 不在内。
	activeKeys := append([]string{}, model.BaseModuleKeys()...)
	activeKeys = append(activeKeys, model.ModuleGEO)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/merchant-api/modules/sync":
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
	defer srv.Close()

	withPlatformEndpointCfg(t, &config.PlatformConfig{APIURL: srv.URL, Secret: "test-secret"})

	database := testutil.NewTestDB(t, &model.TenantModule{}, &model.ModuleSyncLog{}, &model.ModuleOrder{})
	clearModuleTablesOnMode(t, database)
	repo := repository.NewTenantModuleRepositoryWithDB(database)
	orderRepo := repository.NewModuleOrderRepositoryWithDB(database)

	// 与「新商户登录后在页面里点同步」/ 开户后异步同步同形：ctx 绑请求租户，无本地订单
	// 存量默认租户的 0 元模块可能停在 disabled（2026-10-30 线上事故：
	// reach-social 被旧逻辑按下单租户分到别处，tenant 1 保留 disabled 行）。
	// 另一个已存在租户（34）只有基础行，也必须被补上 0 元模块。
	sysSeedCtx := tenant.WithSystemScope(context.Background())
	if err := repo.UpsertMany(sysSeedCtx, []*model.TenantModule{{
		ModuleKey: model.ModuleGEO, Status: model.TenantModuleDisabled,
		Source: model.TenantModuleSourcePurchase, Category: model.ModuleCategoryPaid,
		PriceCents: 100,
	}}); err != nil {
		t.Fatalf("写入默认租户 disabled 0 元模块: %v", err)
	}
	if err := repo.UpsertMany(tenant.OnlyTenant(sysSeedCtx, 34), []*model.TenantModule{{
		ModuleKey: model.ModuleCore, Status: model.TenantModuleActive,
		Source: model.TenantModuleSourceDefaultBase, Category: model.ModuleCategoryBase,
	}}); err != nil {
		t.Fatalf("写入租户 34 基础行: %v", err)
	}

	reqCtx := tenant.WithTenantID(context.Background(), reqTenantID)
	svc := NewModuleCatalogServiceWithRepos(repo, orderRepo)
	if _, err := svc.Sync(reqCtx); err != nil {
		t.Fatalf("Sync 失败: %v", err)
	}

	sysCtx := tenant.WithSystemScope(context.Background())
	rows, err := repo.ListAll(sysCtx)
	if err != nil {
		t.Fatalf("读全量镜像: %v", err)
	}
	baseSet := baseModuleKeySet
	perTenant := map[uint]map[string]*model.TenantModule{}
	baseCount := map[uint]int{}
	for _, r := range rows {
		if perTenant[r.TenantID] == nil {
			perTenant[r.TenantID] = map[string]*model.TenantModule{}
		}
		perTenant[r.TenantID][r.ModuleKey] = r
		if hasKey(baseSet, r.ModuleKey) {
			baseCount[r.TenantID]++
		}
	}

	// 1. 请求租户拿到完整基础包 + 0 元模块
	if got := baseCount[reqTenantID]; got != len(model.BaseModuleKeys()) {
		t.Errorf("租户 %d 应落 %d 个基础模块行，实际 %d（分布 %v）",
			reqTenantID, len(model.BaseModuleKeys()), got, baseCount)
	}
	geo := perTenant[reqTenantID][model.ModuleGEO]
	if geo == nil {
		t.Fatalf("租户 %d 未拿到 0 元模块 geo：空镜像回归（%v）", reqTenantID, perTenant[reqTenantID])
	}
	if geo.Source != model.TenantModuleSourceGift {
		t.Errorf("赠送行来源应为 gift，实际 %q", geo.Source)
	}
	if geo.Status != model.TenantModuleActive {
		t.Errorf("赠送行状态应为 active，实际 %q", geo.Status)
	}

	// 2. 收费且本租户未下单的模块不许白送
	if r := perTenant[reqTenantID][model.ModuleBrowserAutomation]; r != nil {
		t.Errorf("租户 %d 未下单却拿到了收费模块 browser-automation（source=%s）", reqTenantID, r.Source)
	}

	// 3. 默认租户行为不变：商户级 owned 行照旧全量落默认租户
	if got := baseCount[tenant.DefaultTenantID]; got != len(model.BaseModuleKeys()) {
		t.Errorf("默认租户应有 %d 个基础行，实际 %d", len(model.BaseModuleKeys()), got)
	}
	// browser-automation 不在默认租户名下：平台按**租户**下发 owned 状态，
	// 本租户没购买 ⇒ owned=false ⇒ 不在 active_keys ⇒ 谁都不该拿到它
	// （线上实测：默认租户 1 就没有 browser-automation 行，只有买过的租户有）。
	// 旧断言要求"默认租户保留商户级 owned 行"——那是平台端改用按租户下发
	// owned 之前的旧口径，与生产实际不符。
	if r := perTenant[tenant.DefaultTenantID][model.ModuleBrowserAutomation]; r != nil {
		t.Errorf("默认租户不该有 browser-automation 行（该租户未购买，source=%s）", r.Source)
	}

	// 3.1 存量 disabled 的 0 元模块必须被本轮同步改回 active；其他已存在租户
	// 也必须补到 0 元模块（而不是只有发起同步的租户拿到）。
	if r := perTenant[tenant.DefaultTenantID][model.ModuleGEO]; r == nil || r.Status != model.TenantModuleActive {
		t.Errorf("默认租户的存量 disabled 0 元模块应被激活，实际 %+v", r)
	}
	if r := perTenant[34][model.ModuleGEO]; r == nil || r.Status != model.TenantModuleActive {
		t.Errorf("已存在租户 34 应补到 0 元模块 geo，实际 %+v", r)
	}
	if r := perTenant[34][model.ModuleBrowserAutomation]; r != nil {
		t.Errorf("租户 34 未下单却拿到了收费模块 browser-automation（source=%s）", r.Source)
	}

	// 4. 闸：赠送模块对请求租户放行，收费模块仍拦
	gate := NewModuleGateServiceWithRepo(repo)
	t.Cleanup(func() { model.SetModuleEnabledFilter(nil) })
	if !gate.IsEnabled(reqCtx, model.ModuleGEO) {
		t.Error("租户的 0 元赠送模块应放行")
	}
	if gate.IsEnabled(reqCtx, model.ModuleBrowserAutomation) {
		t.Error("未下单的收费模块不应放行")
	}
	if !gate.IsEnabled(reqCtx, model.ModuleCore) {
		t.Error("请求租户的基础模块应放行")
	}
}
