package service

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"hivemtk-user/internal/aiagent/llm"
	"hivemtk-user/internal/config"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/pkg/testutil"
	"hivemtk-user/internal/platform"
	"hivemtk-user/internal/repository"

	"gorm.io/gorm"
)

// PLT-01 刀1 端到端判据（真库）：平台 provider 调用后的本地成本三列
// （input/output/total cost cents）非 0、与套餐单价快照整数换算逐分对齐、
// 写入时点快照不随读侧漂移、join 失败如实降级 0、trace 幂等不重复落账。
//
// 链路：dispatcher 整数计价（llm 包侧用例钉）→ 事件携带整数分 →
// InstallPlatformUsageSink 消费落 platform_llm_usage_records。
// 本文件把后两段（+sync 落价、daily 聚合 round-trip、off 零出域）钉在真 PG 上。

// costE2EFixture 装 sink + resolver 两条装配 seam（与 cmd/api/main.go 同一入口），
// 返回真库句柄。cleanup 摘掉全局 seam，不污染同包其他用例。
func costE2EFixture(t *testing.T) *gorm.DB {
	t.Helper()
	database := testutil.NewTestDB(t, &model.LLMProvider{}, &model.PlatformLLMUsageRecord{})
	InstallPlatformUsageSink()
	t.Cleanup(func() {
		llm.SetPlatformUsageSink(nil)
		llm.SetPlatformPlanPriceResolver(nil)
	})
	return database
}

// seedPricedPlatformProvider 模拟 sync 的落库结果：平台 provider 行 + 套餐价快照。
func seedPricedPlatformProvider(t *testing.T, db *gorm.DB, name, planCode string, inCents, outCents int64) {
	t.Helper()
	sysCtx := tenant.WithSystemScope(context.Background())
	n, err := repository.NewPlatformLLMUsageRepositoryWithDB(db).UpsertProviders(sysCtx,
		platformProviderRowsToModels([]platform.PlatformLLMProvider{
			{ProviderName: name, DisplayName: name, Model: "m", APIType: "openai", PlanCode: planCode},
		}, map[string]platform.PlatformLLMPlan{
			planCode: {PlanCode: planCode, InputPricePer1kCents: inCents, OutputPricePer1kCents: outCents},
		}))
	if err != nil || n != 1 {
		t.Fatalf("seed 平台 provider 失败: n=%d err=%v", n, err)
	}
}

func platformUsageRow(t *testing.T, db *gorm.DB, requestID string) *model.PlatformLLMUsageRecord {
	t.Helper()
	var rec model.PlatformLLMUsageRecord
	if err := db.WithContext(context.Background()).
		Where("request_id = ?", requestID).First(&rec).Error; err != nil {
		t.Fatalf("读用量明细 %s: %v", requestID, err)
	}
	return &rec
}

// TestPlatformUsageCostSnapshot_IntegerCentsAndIdempotent 主判据：
// 成本三列逐分对齐 + 写时快照不漂移 + join 失败降级 + trace 幂等。
func TestPlatformUsageCostSnapshot_IntegerCentsAndIdempotent(t *testing.T) {
	db := costE2EFixture(t)
	ctx := context.Background()

	// 快照：输入 7 分/1k、输出 70 分/1k。
	seedPricedPlatformProvider(t, db, "plat-e2e", "plan-snap", 7, 70)

	// 1500×7/1000 = 10.5 → 10 分；200×70/1000 = 14 分。整数截断逐分对齐。
	emitPlatformUsageForTest(t, llm.PlatformUsageEvent{
		Provider: "plat-e2e", Model: "m", PlanCode: "plan-snap",
		Scenario: "sop_reply", TraceID: "trace-snap-1", TenantID: 1,
		PromptTokens: 1500, CompletionTokens: 200,
		InputCostCents: 10, OutputCostCents: 14,
	})
	rec := platformUsageRow(t, db, "trace-snap-1")
	if rec.InputCostCents != 10 || rec.OutputCostCents != 14 || rec.TotalCostCents != 24 {
		t.Fatalf("成本三列应逐分对齐 in=10 out=14 total=24，实际 in=%d out=%d total=%d",
			rec.InputCostCents, rec.OutputCostCents, rec.TotalCostCents)
	}

	// 幂等：同一 trace 重复 sink（重试/重放形态）不重复落账。
	emitPlatformUsageForTest(t, llm.PlatformUsageEvent{
		Provider: "plat-e2e", Model: "m", PlanCode: "plan-snap",
		TraceID: "trace-snap-1", TenantID: 1,
		PromptTokens: 1500, CompletionTokens: 200,
		InputCostCents: 10, OutputCostCents: 14,
	})
	var count int64
	if err := db.WithContext(ctx).Model(&model.PlatformLLMUsageRecord{}).
		Where("request_id = ?", "trace-snap-1").Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("重复 sink 必须仍只有 1 行（trace_id 唯一索引幂等），实际 %d", count)
	}

	// 改价后历史行不变（写时快照，读侧不重算不回填）：
	// 平台调价 → 下一趟 sync 把新价落到镜像行 → 只有**新**调用按新价落列。
	if err := db.WithContext(tenant.WithSystemScope(ctx)).Model(&model.LLMProvider{}).
		Where("name = ?", "plat-e2e").
		Updates(map[string]any{"input_price_per_1k_cents": 9, "output_price_per_1k_cents": 90}).Error; err != nil {
		t.Fatal(err)
	}
	emitPlatformUsageForTest(t, llm.PlatformUsageEvent{
		Provider: "plat-e2e", Model: "m", PlanCode: "plan-snap",
		TraceID: "trace-snap-2", TenantID: 1,
		PromptTokens: 1500, CompletionTokens: 200,
		InputCostCents: 13, OutputCostCents: 18, // 1500×9/1000=13.5→13；200×90/1000=18
	})
	rec2 := platformUsageRow(t, db, "trace-snap-2")
	if rec2.InputCostCents != 13 || rec2.OutputCostCents != 18 {
		t.Fatalf("改价后新行应按新快照落列 in=13 out=18，实际 in=%d out=%d",
			rec2.InputCostCents, rec2.OutputCostCents)
	}
	after := platformUsageRow(t, db, "trace-snap-1")
	if after.InputCostCents != 10 || after.OutputCostCents != 14 || after.TotalCostCents != 24 {
		t.Fatalf("改价后历史行不得漂移（写时快照口径），实际 in=%d out=%d total=%d",
			after.InputCostCents, after.OutputCostCents, after.TotalCostCents)
	}

	// join 失败降级格：套餐下架/不可见 → 镜像行无价（0）→ 事件 0 分 → 如实落 0，
	// 不报错、不编价。
	emitPlatformUsageForTest(t, llm.PlatformUsageEvent{
		Provider: "plat-e2e", Model: "m", PlanCode: "plan-delisted",
		TraceID: "trace-snap-3", TenantID: 1,
		PromptTokens: 1500, CompletionTokens: 200,
	})
	rec3 := platformUsageRow(t, db, "trace-snap-3")
	if rec3.InputCostCents != 0 || rec3.OutputCostCents != 0 || rec3.TotalCostCents != 0 {
		t.Fatalf("无价调用应如实降级 0，实际 in=%d out=%d total=%d",
			rec3.InputCostCents, rec3.OutputCostCents, rec3.TotalCostCents)
	}
}

// TestPlatformPlanPriceResolver_ReadsLocalMirror resolver 读本地镜像价列：
// 按 plan_code 命中、未同步/plan_code 空 → (0,0)；DB 行缺失 → (0,0) 不报错。
func TestPlatformPlanPriceResolver_ReadsLocalMirror(t *testing.T) {
	db := costE2EFixture(t)
	seedPricedPlatformProvider(t, db, "plat-res", "plan-res", 33, 66)

	in, out := platformPlanPriceResolver("plan-res")
	if in != 33 || out != 66 {
		t.Fatalf("resolver 应读到快照 (33,66)，实际 (%d,%d)", in, out)
	}
	if in, out := platformPlanPriceResolver("plan-unknown"); in != 0 || out != 0 {
		t.Fatalf("查不到的 plan 应降级 (0,0)，实际 (%d,%d)", in, out)
	}
	if in, out := platformPlanPriceResolver("  "); in != 0 || out != 0 {
		t.Fatalf("空 plan_code 应降级 (0,0)，实际 (%d,%d)", in, out)
	}
}

// TestSyncPlatformProvidersLandsPlanPrices sync 落价：Providers()×Plans() 按
// plan_code 关联写镜像价列；join 不上的 plan 价列 0（不编价）。
func TestSyncPlatformProvidersLandsPlanPrices(t *testing.T) {
	database := testutil.NewTestDB(t, &model.LLMProvider{})
	withTenantMode(t, tenant.ModeOn)
	t.Cleanup(func() { tenant.SetMode(tenant.ModeOff) })
	t.Setenv("PLATFORM_ENABLED", "true")
	t.Setenv("PLATFORM_MERCHANT_KEY", "MKE2E")

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/merchant-api/llm/providers":
			_ = encodeOK(w, map[string]any{"list": []map[string]any{
				{"provider_name": "plat-a", "display_name": "A", "model": "m1",
					"api_type": "openai", "vendor": "v", "plan_code": "plan-live"},
				{"provider_name": "plat-b", "display_name": "B", "model": "m2",
					"api_type": "openai", "vendor": "v", "plan_code": "plan-delisted"},
			}})
		case "/merchant-api/llm/plans":
			_ = encodeOK(w, map[string]any{"list": []map[string]any{
				// plan-live 可见；plan-delisted 已下架（列表里没有）→ join 不上
				{"plan_code": "plan-live", "name": "L", "input_price_per_1k_cents": 5,
					"output_price_per_1k_cents": 55, "price_cents": 1000},
			}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	withPlatformEndpointCfg(t, &config.PlatformConfig{APIURL: srv.URL, Secret: "test-secret"})

	n, err := SyncPlatformProviders(context.Background())
	if err != nil {
		t.Fatalf("sync 失败: %v", err)
	}
	if n != 2 {
		t.Fatalf("应同步 2 条 provider，实际 %d", n)
	}
	sysCtx := tenant.WithSystemScope(context.Background())
	var a, b model.LLMProvider
	if err := database.WithContext(sysCtx).Where("name = ?", "plat-a").First(&a).Error; err != nil {
		t.Fatal(err)
	}
	if a.InputPricePer1kCents != 5 || a.OutputPricePer1kCents != 55 {
		t.Fatalf("plan-live 应带上套餐价 (5,55)，实际 (%d,%d)", a.InputPricePer1kCents, a.OutputPricePer1kCents)
	}
	if err := database.WithContext(sysCtx).Where("name = ?", "plat-b").First(&b).Error; err != nil {
		t.Fatal(err)
	}
	if b.InputPricePer1kCents != 0 || b.OutputPricePer1kCents != 0 {
		t.Fatalf("join 不上的 plan 价列必须 0（不编价），实际 (%d,%d)",
			b.InputPricePer1kCents, b.OutputPricePer1kCents)
	}
}

// TestSyncPlatformProviders_OffModeZeroEgress off 模式零出域（zero_egress 同款）：
// 关态下 sync/sink 都不得发出任何平台请求 —— 即使 PlatformCfg 里装着可达地址。
func TestSyncPlatformProviders_OffModeZeroEgress(t *testing.T) {
	var got int64
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt64(&got, 1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":200,"msg":"ok","data":{}}`))
	}))
	defer target.Close()

	t.Setenv("PLATFORM_ENABLED", "false")
	t.Setenv("PLATFORM_MERCHANT_KEY", "MKE2E")
	withPlatformEndpointCfg(t, &config.PlatformConfig{APIURL: target.URL, Secret: "s"})
	// withPlatformEndpointCfg 只在未显式设置时才置 true；这里显式钉死关态
	t.Setenv("PLATFORM_ENABLED", "false")

	costE2EFixture(t)
	if _, err := SyncPlatformProviders(context.Background()); err != nil {
		t.Fatalf("关态 sync 应空跑不报错: %v", err)
	}
	emitPlatformUsageForTest(t, llm.PlatformUsageEvent{
		Provider: "plat-x", TraceID: "trace-off-1", TenantID: 1,
		PromptTokens: 100, CompletionTokens: 100,
	})
	if n := atomic.LoadInt64(&got); n != 0 {
		t.Fatalf("关态必须零出站请求（sync+sink 共 %d 次）", n)
	}
}

// TestDailyAggregationPlatformCostRoundTrip routing_logs → llm_usage_daily 的
// 成本 round-trip：routing 行存「整数分 ÷100」的元值（numeric(14,6) 两位小数），
// 聚合 SUM(cost)*100 必须无损回到同一整数分 —— 平台行 cost 同口径非 0。
func TestDailyAggregationPlatformCostRoundTrip(t *testing.T) {
	db := testutil.NewTestDB(t, &model.LLMRoutingLog{}, &model.LLMUsageDaily{})
	addT3Columns(t, db)
	repo := repository.NewLLMUsageReportRepository(db)
	now := time.Now()

	// dispatch 侧的落库形状：1500×7/1000=10 分 → 0.10 元；200×70/1000=14 分 → 0.14 元。
	rows := []map[string]any{
		{"tenant_id": 1, "trace_id": "rt-1", "scenario": "sop", "provider": "plat-agg", "model": "m",
			"prompt_tokens": 1500, "completion_tokens": 200, "total_tokens": 1700,
			"cost": 0.24, "prompt_cost": 0.10, "completion_cost": 0.14,
			"latency_ms": 100, "success": true, "channel": "web", "source": "platform", "created_at": now},
		{"tenant_id": 1, "trace_id": "rt-2", "scenario": "sop", "provider": "plat-agg", "model": "m",
			"prompt_tokens": 3000, "completion_tokens": 400, "total_tokens": 3400,
			"cost": 0.49, "prompt_cost": 0.21, "completion_cost": 0.28,
			"latency_ms": 120, "success": true, "channel": "web", "source": "platform", "created_at": now},
	}
	for _, r := range rows {
		if err := db.Table("llm_routing_logs").Create(r).Error; err != nil {
			t.Fatalf("seed routing_logs: %v", err)
		}
	}
	if _, err := repo.AggregateWindow(context.Background(), now.Add(-time.Hour), now.Add(time.Hour)); err != nil {
		t.Fatalf("aggregate: %v", err)
	}
	var bucket model.LLMUsageDaily
	if err := db.WithContext(context.Background()).
		Where("provider = ? AND source = ?", "plat-agg", "platform").
		First(&bucket).Error; err != nil {
		t.Fatalf("daily 平台行缺失: %v", err)
	}
	// 10+14=24 与 21+28=49；SUM(cost)*100 round-trip 后逐分对齐。
	if bucket.TotalCostCents != 24+49 {
		t.Fatalf("daily 平台行 total_cost_cents 应=73（整数分无损 round-trip），实际 %d", bucket.TotalCostCents)
	}
	if bucket.TotalCostCents == 0 {
		t.Fatal("daily 平台行 cost 非 0 判据失败")
	}
}

// emitPlatformUsageForTest 走真实的装配 seam（InstallPlatformUsageSink 注入的
// 闭包）发一条用量事件 —— 不绕过 sink 直呼 recordPlatformUsage，装配断链会红。
func emitPlatformUsageForTest(t *testing.T, e llm.PlatformUsageEvent) {
	t.Helper()
	sink := llm.PlatformUsageSink()
	if sink == nil {
		t.Fatal("sink 未注入（InstallPlatformUsageSink 未装配）")
	}
	sink(e)
}

func encodeOK(w http.ResponseWriter, data map[string]any) error {
	return json.NewEncoder(w).Encode(map[string]any{"code": 200, "msg": "", "data": data})
}
