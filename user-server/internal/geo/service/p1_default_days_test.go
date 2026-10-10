package service

import (
	"context"
	"testing"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/testutil"
	"hivemtk-user/internal/repository"
	"hivemtk-user/internal/service"
)

// TestDefaultTrendDaysReadsParamCenter 直连参数的行为测试。
//
// geo.default_visibility_days 不走 provider seam，而是 internal/geo/service 直接调
// service.GlobalConfigParam()——那个包早已 import internal/service（llm.go、
// keyword_pipeline.go），没有环可成。所以这一组用例钉的是"读取函数真的会去读参数中心"，
// 而不是某个注入口有没有被顶掉。
func TestDefaultTrendDaysReadsParamCenter(t *testing.T) {
	database := testutil.NewTestDBOrSkip(t, &model.ConfigParam{}, &model.ConfigParamAuditLog{})
	if database == nil {
		t.Skip("no DB")
	}
	if err := service.SeedConfigParams(context.Background(), database); err != nil {
		t.Fatalf("SeedConfigParams 失败：%v", err)
	}
	svc := service.NewConfigParamService(database)
	service.SetGlobalForTest(svc)
	t.Cleanup(func() { service.SetGlobalForTest(nil) })

	ctx := context.Background()

	// 种子默认 30 应与改造前的写死值逐字节一致。
	if got := defaultTrendDays(ctx); got != 30 {
		t.Errorf("种子默认下得到 %d, 期望 30", got)
	}

	if err := svc.UpdateValue(ctx, "geo", "default_visibility_days", "90", 1); err != nil {
		t.Fatalf("UpdateValue 失败：%v", err)
	}
	// 参数服务按 group.key 缓存 60s，换个冷缓存才读得到新值。
	service.SetGlobalForTest(service.NewConfigParamService(database))
	if got := defaultTrendDays(ctx); got != 90 {
		t.Errorf("改库为 90 后得到 %d", got)
	}

	// 守卫：非正数回落 30 而不是透传——透传 0 会让下面的窗口计算拿到 0 天。
	// 走 repository 而不是 service.UpdateValue：后者有 min/max 守卫（min=1），
	// 而守卫要防的恰是"库里已经有坏值"这种情形——历史数据、直接改库、绕过 service 写。
	repo := repository.NewConfigParamRepository(database)
	if err := repo.UpdateValue(ctx, "geo", "default_visibility_days", "0", 1); err != nil {
		t.Fatalf("repo.UpdateValue 失败：%v", err)
	}
	service.SetGlobalForTest(service.NewConfigParamService(database))
	if got := defaultTrendDays(ctx); got != 30 {
		t.Errorf("库里是 0 时应回落 30, 得到 %d", got)
	}
}

// TestDefaultTrendDaysWithoutParamCenter 无参数中心时回落 30，不 panic。
func TestDefaultTrendDaysWithoutParamCenter(t *testing.T) {
	service.SetGlobalForTest(nil)
	t.Cleanup(func() { service.SetGlobalForTest(nil) })
	if got := defaultTrendDays(context.Background()); got != 30 {
		t.Errorf("无参数中心时应回落 30, 得到 %d", got)
	}
	// nil ctx 也不能 panic：GetTrend 的调用链上出现过 ctx 未传的情况。
	if got := defaultTrendDays(nil); got != 30 { //nolint:staticcheck // 故意传 nil 验证守卫
		t.Errorf("nil ctx 时应回落 30, 得到 %d", got)
	}
}
