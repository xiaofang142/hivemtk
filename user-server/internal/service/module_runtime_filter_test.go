package service

import (
	"context"
	"testing"
	"time"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"

	"hivemtk-user/internal/event"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/pkg/testutil"
	"hivemtk-user/internal/repository"
)

// FR-5.6 运行时模块闸（派发时刻动态过滤）的用例。
//
// 与 module_gate_test.go 那组互补：那组测「闸的数据对不对」（per-tenant
// 路由闸），这里测**实例级并集判定 + fail-open 教义 + 三个派发点入口**。
//
// 共用前提：进程单例（AllowWorker 的落点）缓存必须在用例开头清掉 ——
// 它的 per-tenant 快照可能来自同包其它用例（refreshProcessGate 刷过），
// 不清会把上一用例的开通状态串进本用例的并集判定。

// newRuntimeGate 造「空模块镜像」的测试面：返回显式仓储的闸实例，并清
// 进程单例（AllowWorker 的落点）缓存。
//
// 必须**带模型**建连接（同 module_gate_tenant_test.go）：只有这条路径会
// DropTable+AutoMigrate+tenancyidx.Normalize（把模型标签的单列唯一索引复合成
// (tenant_id, module_key)）+ 注册隔离 callback。少了 Normalize，UpsertMany 的
// 复合 ON CONFLICT 会 42P10 退回"先查后写"，而慢路径按 module_key **单列**
// 查改 —— 两个租户的同一模块行会被并成一行，多租户用例静默失真。
func newRuntimeGate(t *testing.T) (*ModuleGateService, repository.TenantModuleRepository) {
	t.Helper()
	database := testutil.NewTestDB(t, &model.TenantModule{}, &model.ModuleSyncLog{}, &model.ModuleOrder{})
	sys := tenant.WithSystemScope(context.Background())
	truncate := func() {
		for _, m := range []any{&model.TenantModule{}, &model.ModuleSyncLog{}, &model.ModuleOrder{}} {
			if err := database.Session(&gorm.Session{AllowGlobalUpdate: true}).
				WithContext(sys).Delete(m).Error; err != nil {
				t.Fatalf("清空 %T: %v", m, err)
			}
		}
	}
	truncate()
	// 用例结束必须清行：模块闸有"表里有行=数据存在"的空态判定，本文件留下的
	// 行会被同包其它用例读到（与 module_gate_test.go newModuleDB 同一条纪律）。
	t.Cleanup(truncate)
	repo := repository.NewTenantModuleRepositoryWithDB(database)
	gate := NewModuleGateServiceWithRepo(repo)
	NewModuleGateService().Invalidate()
	t.Cleanup(func() {
		NewModuleGateService().Invalidate()
		model.SetModuleEnabledFilter(nil)
	})
	return gate, repo
}

// brokenDB 造一个「必然查询失败」的连接：Open 不 ping（DisableAutomaticPing），
// 随后直接关闭底层 sql 句柄 —— 之后任何查询都得到 "sql: database is closed"。
// 用独立连接而不是关闭共享测试连接：后者会把同包其它用例的连接池一起毒死。
func brokenDB(t *testing.T) *gorm.DB {
	t.Helper()
	gdb, err := gorm.Open(
		postgres.Open("host=127.0.0.1 port=1 user=none password=none dbname=none sslmode=disable"),
		&gorm.Config{DisableAutomaticPing: true},
	)
	if err != nil {
		t.Fatalf("打开坏连接（不应失败：未启用 ping）: %v", err)
	}
	sqlDB, err := gdb.DB()
	if err != nil {
		t.Fatalf("取底层句柄: %v", err)
	}
	if err := sqlDB.Close(); err != nil {
		t.Fatalf("关闭底层句柄: %v", err)
	}
	return gdb
}

// ---------------------------------------------------------- (a) 未映射恒放行

// TestRuntimeGateUnmappedWorkerAlwaysAllows 未映射的 worker 名 / 空 key
// 一律放行（教义 ③ fail-open-on-unknown），且**不打 DB**（无 DB 环境也必须真）。
func TestRuntimeGateUnmappedWorkerAlwaysAllows(t *testing.T) {
	ctx := context.Background()
	for _, name := range []string{"", "sop_scheduler", "feedback_learning", "some_future_worker"} {
		if !AllowWorker(ctx, name) {
			t.Errorf("未映射的 worker %q 应恒放行（fail-open-on-unknown），实际被拦", name)
		}
	}
	// 空 key 的工具过滤是 no-op：原样返回
	tools := []AgentToolDef{{Name: "a"}, {Name: "b"}}
	if got := FilterToolsByKey(ctx, "", tools); len(got) != 2 {
		t.Errorf("FilterToolsByKey(\"\") 应原样返回 2 个工具，实际 %d", len(got))
	}
}

// ---------------------------------------------------------- (b) 关停 → 停跑

// TestRuntimeGateDisabledModuleStopsWorker 模块对**任何**租户都不可用
// （禁用 + 过期两种不可用形态并存）→ AnyTenantEnabled=false → AllowWorker=false。
func TestRuntimeGateDisabledModuleStopsWorker(t *testing.T) {
	gate, repo := newRuntimeGate(t)
	ctx := context.Background()
	now := time.Now()
	expired := now.Add(-time.Hour)

	rows := []*model.TenantModule{
		// 租户 1：显式禁用
		{TenantID: 1, ModuleKey: model.ModuleGEO, Status: model.TenantModuleDisabled, ActivatedAt: now},
		// 租户 2：status=active 但已过期（TenantModuleUsable=false 的另一半形态）
		{TenantID: 2, ModuleKey: model.ModuleGEO, Status: model.TenantModuleActive, ActivatedAt: expired, ExpiresAt: &expired},
	}
	if err := repo.UpsertMany(tenant.WithSystemScope(ctx), rows); err != nil {
		t.Fatalf("写入镜像: %v", err)
	}

	if gate.AnyTenantEnabled(ctx, model.ModuleGEO) {
		t.Error("全实例禁用+过期的模块应判关（AnyTenantEnabled 不得放行）")
	}
	if AllowWorker(ctx, model.ModuleGEO) {
		t.Error("模块关停后 AllowWorker(模块 key) 应为 false")
	}
	if AllowWorker(ctx, "sov_refresh") {
		t.Error("GEO 关停后 AllowWorker(\"sov_refresh\")（worker 名映射路径）应为 false")
	}
	// 未映射入口不受影响
	if !AllowWorker(ctx, "") || !AllowWorker(ctx, "sop_scheduler") {
		t.Error("空 key / 未映射 worker 在关停场景下仍应放行")
	}
}

// ---------------------------------------------------------- (c) 任一租户开通 → 跑

// TestRuntimeGateEnabledForAnyTenant 任一租户（这里是租户 2）开通 →
// 实例级判定为 true，且与路由闸的 per-tenant 判定并存互不污染。
//
// 走 on 模式（同 module_gate_tenant_test.go）：只有隔离 callback 生效时，
// "租户 1 没开通 / 租户 2 开通"才是一组真实可分辨的数据；off 模式单表无
// 隔离，两个租户的行会读成一锅。
func TestRuntimeGateEnabledForAnyTenant(t *testing.T) {
	withTenantMode(t, tenant.ModeOn)
	database := testutil.NewTestDB(t, &model.TenantModule{}, &model.ModuleSyncLog{}, &model.ModuleOrder{})
	clearModuleTablesOnMode(t, database)

	repo := repository.NewTenantModuleRepositoryWithDB(database)
	ctx1 := tenant.WithTenantID(context.Background(), 1)
	ctx2 := tenant.WithTenantID(context.Background(), 2)
	now := time.Now()
	if err := repo.UpsertMany(ctx1, []*model.TenantModule{
		{ModuleKey: model.ModuleGEO, Status: model.TenantModuleDisabled, ActivatedAt: now},
	}); err != nil {
		t.Fatalf("写入租户 1（禁用）: %v", err)
	}
	if err := repo.UpsertMany(ctx2, []*model.TenantModule{
		{ModuleKey: model.ModuleGEO, Status: model.TenantModuleActive, ActivatedAt: now},
	}); err != nil {
		t.Fatalf("写入租户 2（开通）: %v", err)
	}

	gate := NewModuleGateServiceWithRepo(repo)
	NewModuleGateService().Invalidate()
	t.Cleanup(func() {
		NewModuleGateService().Invalidate()
		model.SetModuleEnabledFilter(nil)
	})

	bg := context.Background()
	// 无租户绑定（后台 worker 形态）
	if !gate.AnyTenantEnabled(bg, model.ModuleGEO) {
		t.Error("任一租户开通即应为 true（实例级并集）")
	}
	// 绑在"没开通"的租户 1 上也必须 true：判定不看 ctx 租户
	if !gate.AnyTenantEnabled(ctx1, model.ModuleGEO) {
		t.Error("绑定未开通租户的 ctx 也应得到并集结果 true")
	}
	if !AllowWorker(bg, model.ModuleGEO) || !AllowWorker(bg, "keyword_pipeline") {
		t.Error("任一租户开通后 AllowWorker（含 worker 名路径）应放行")
	}
	// 对照：路由闸（per-tenant）同数据下按各自租户判 —— 两种口径并存是刻意的
	gate.Invalidate()
	if gate.IsEnabled(ctx1, model.ModuleGEO) {
		t.Error("路由闸仍应按租户 1 判关（per-tenant 口径不许被并集口径污染）")
	}
	if !gate.IsEnabled(ctx2, model.ModuleGEO) {
		t.Error("路由闸应按租户 2 判开通（隔离读没生效则本用例测不到并集语义）")
	}
}

// ---------------------------------------------------------- (d) DB 错误 → fail-open

// TestRuntimeGateFailOpenOnDBError DB 不可用 → AnyTenantEnabled 返回 true。
//
// 这是本闸与路由闸（fail-closed）**相反**的教义：后台任务被瞬时 DB 错误
// 停掉比多跑一轮贵得多（见 module_runtime_filter.go 文件头 ②）。
func TestRuntimeGateFailOpenOnDBError(t *testing.T) {
	repo := repository.NewTenantModuleRepositoryWithDB(brokenDB(t))
	gate := NewModuleGateServiceWithRepo(repo)
	// 有缓存也必须走 DB 路径（本实例缓存为空）
	if !gate.AnyTenantEnabled(context.Background(), model.ModuleGEO) {
		t.Error("DB 错误时应 fail-open 放行，实际被拦（会把后台任务静默停掉）")
	}
	if !gate.AnyTenantEnabled(context.Background(), model.ModuleCustomer) {
		t.Error("基础模块在 DB 错误时也应 fail-open 放行")
	}
	// 未初始化仓储（装配早期 / 测试无 DB）同样 fail-open
	bare := NewModuleGateServiceWithRepo(nil)
	if !bare.AnyTenantEnabled(context.Background(), model.ModuleGEO) {
		t.Error("仓储未注入时应 fail-open 放行，而不是 panic 或拦死")
	}
}

// ---------------------------------------------------------- (e) 订阅包装

// TestRuntimeGateWrapSubscriberSkipsDisabledRunsEnabled 模块关停 → handler
// 不执行且返回 nil（不是错误）；开通后恢复执行。
func TestRuntimeGateWrapSubscriberSkipsDisabledRunsEnabled(t *testing.T) {
	_, repo := newRuntimeGate(t)
	ctx := context.Background()
	now := time.Now()

	// 关停态：显式禁用行（表非空 → 不落空态兜底，测的是真·不可用判定）
	if err := repo.UpsertMany(tenant.WithSystemScope(ctx), []*model.TenantModule{
		{TenantID: 1, ModuleKey: model.ModuleGEO, Status: model.TenantModuleDisabled, ActivatedAt: now},
	}); err != nil {
		t.Fatalf("写入禁用行: %v", err)
	}

	calls := 0
	wrapped := WrapSubscriber(model.ModuleGEO, func(evt event.Event) error {
		calls++
		return nil
	})
	if err := wrapped(event.Event{Topic: "test.topic"}); err != nil {
		t.Errorf("被跳过的订阅应返回 nil（否则污染投递失败统计），实际 %v", err)
	}
	if calls != 0 {
		t.Errorf("模块关停时 handler 不应执行，实际执行 %d 次", calls)
	}
	// nil handler 透传不 panic
	if WrapSubscriber(model.ModuleGEO, nil) != nil {
		t.Error("WrapSubscriber(key, nil) 应返回 nil")
	}
	// 空 key = 纯结构透传
	pass := 0
	if err := WrapSubscriber("", func(event.Event) error { pass++; return nil })(event.Event{Topic: "t"}); err != nil || pass != 1 {
		t.Errorf("WrapSubscriber(\"\") 应透传执行（err=%v pass=%d）", err, pass)
	}

	// 开通态：同一包装恢复执行（派发时刻判定，不缓存装配期结论）
	if err := repo.UpsertMany(tenant.WithSystemScope(ctx), []*model.TenantModule{
		{TenantID: 1, ModuleKey: model.ModuleGEO, Status: model.TenantModuleActive, ActivatedAt: now},
	}); err != nil {
		t.Fatalf("写入开通行: %v", err)
	}
	if err := wrapped(event.Event{Topic: "test.topic"}); err != nil {
		t.Errorf("开通后 handler 执行不应报错: %v", err)
	}
	if calls != 1 {
		t.Errorf("开通后 handler 应执行 1 次，实际 %d 次", calls)
	}
}

// ---------------------------------------------------------- (f) OFF 模式等价

// TestRuntimeGateOfflineEquivalenceWithIsEnabled OFF 模式（bootstrapOffline
// 落地基础模块后）单租户下，实例级判定与路由闸 IsEnabled 逐 key 等价。
func TestRuntimeGateOfflineEquivalenceWithIsEnabled(t *testing.T) {
	newModuleDB(t)
	t.Setenv("PLATFORM_ENABLED", "false") // 离线：Sync 走 bootstrapOffline，不发出站请求

	repo := repository.NewTenantModuleRepositoryWithDB(rawDB(t))
	svc := NewModuleCatalogServiceWithRepos(repo, nil)
	ctx := context.Background()
	res, err := svc.Sync(ctx)
	if err != nil {
		t.Fatalf("离线 Sync 应成功: %v", err)
	}
	if res.Source != "offline_bootstrap" {
		t.Fatalf("前置应为 offline_bootstrap，实际 %q", res.Source)
	}

	gate := NewModuleGateServiceWithRepo(repo)
	gate.Invalidate()
	NewModuleGateService().Invalidate()
	t.Cleanup(func() {
		NewModuleGateService().Invalidate()
		model.SetModuleEnabledFilter(nil)
	})

	for _, k := range model.BaseModuleKeys() {
		want := gate.IsEnabled(ctx, k)
		got := gate.AnyTenantEnabled(ctx, k)
		if !want {
			t.Errorf("前置不成立：基础模块 %s 经 IsEnabled 应为 true", k)
		}
		if got != want {
			t.Errorf("OFF 模式等价性破坏：基础模块 %s IsEnabled=%v 但 AnyTenantEnabled=%v", k, want, got)
		}
	}
	for _, k := range model.PaidModuleKeys() {
		want := gate.IsEnabled(ctx, k)
		got := gate.AnyTenantEnabled(ctx, k)
		if got != want {
			t.Errorf("OFF 模式等价性破坏：收费模块 %s IsEnabled=%v 但 AnyTenantEnabled=%v", k, want, got)
		}
	}
	// worker 名路径同口径：customer/knowledge 已随基础包落地 → 对应 cron 放行
	if !AllowWorker(ctx, "customer_rfm") || !AllowWorker(ctx, "customer_session") || !AllowWorker(ctx, "rag_eval") {
		t.Error("bootstrapOffline 落地后 customer/knowledge cron 应放行")
	}
	// geo 是不是 0 元赠送，决定它的 worker 离线路径放不放行 —— 判据跟着目录走。
	//
	// 2026-10-30 起 geo 恢复为 0 元赠送模块，离线 bootstrap 会落地它 ⇒
	// 关联 worker 应放行。判据仍跟着目录走，避免下次改价时断言过期。
	geoIsGift := false
	for _, m := range model.ModuleCatalogMeta {
		if m.Key == model.ModuleGEO {
			geoIsGift = m.Category == model.ModuleCategoryPaid && m.PriceCents == 0
		}
	}
	if geoIsGift {
		if !AllowWorker(ctx, "sov_refresh") {
			t.Error("0 元赠送模块 geo 落地后其关联 worker 应放行")
		}
	} else if AllowWorker(ctx, "sov_refresh") {
		t.Error("geo 是收费模块，离线 bootstrap 不赠送它 ⇒ 关联 worker 应被拦")
	}
	// browser-automation 仍锁定（付费买断，离线不可购）→ 关联能力拦
	if AllowWorker(ctx, model.ModuleBrowserAutomation) {
		t.Error("未开通的 browser-automation 关联 worker 应被拦")
	}
}

// ---------------------------------------------------------- 工具过滤

// TestFilterToolsByModuleConservativeMap 逐工具过滤：映射过的工具按模块
// 判定，未映射的照常展示（保守口径），零裁剪时原样返回。
func TestFilterToolsByModuleConservativeMap(t *testing.T) {
	_, repo := newRuntimeGate(t)
	ctx := context.Background()
	now := time.Now()

	tools := []AgentToolDef{
		{Name: "browser_open_task"},   // → browser-automation（收费，未开通 → 裁）
		{Name: "browser_task_status"}, // 同上（同模块只判定一次）
		{Name: "reach.wecom.send"},    // → im-wecom（收费，未开通 → 裁）
		{Name: "customer.search"},     // 未映射 → 恒展示
		{Name: "knowledge.search"},    // 未映射 → 恒展示
	}

	// 未开通：只留未映射的
	out := FilterToolsByModule(ctx, tools)
	if len(out) != 2 || out[0].Name != "customer.search" || out[1].Name != "knowledge.search" {
		t.Fatalf("未开通时应只保留未映射工具，实际 %d 个: %v", len(out), toolNames(out))
	}
	// 开通两个模块 → 全保留，且**零裁剪时返回原切片**
	if err := repo.UpsertMany(tenant.WithSystemScope(ctx), []*model.TenantModule{
		{TenantID: 1, ModuleKey: model.ModuleBrowserAutomation, Status: model.TenantModuleActive, ActivatedAt: now},
		{TenantID: 1, ModuleKey: model.ModuleIMWecom, Status: model.TenantModuleActive, ActivatedAt: now},
	}); err != nil {
		t.Fatalf("写入开通行: %v", err)
	}
	out2 := FilterToolsByModule(ctx, tools)
	if len(out2) != len(tools) {
		t.Fatalf("开通后应全保留，实际 %d/%d: %v", len(out2), len(tools), toolNames(out2))
	}
	if &out2[0] != &tools[0] {
		t.Error("零裁剪时应原样返回输入切片（避免换底层数组）")
	}

	// 单 key 过滤（FilterToolsByKey）：关停整体裁剪、开通原样
	if got := FilterToolsByKey(ctx, model.ModuleGEO, tools); got != nil {
		t.Errorf("未开通模块的 FilterToolsByKey 应返回 nil，实际 %d 个", len(got))
	}
	if got := FilterToolsByKey(ctx, model.ModuleBrowserAutomation, tools); len(got) != len(tools) {
		t.Errorf("已开通模块的 FilterToolsByKey 应原样返回，实际 %d", len(got))
	}
}

func toolNames(tools []AgentToolDef) []string {
	out := make([]string, 0, len(tools))
	for _, t := range tools {
		out = append(out, t.Name)
	}
	return out
}
