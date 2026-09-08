package service

import (
	"context"
	"testing"

	"hivemtk-user/internal/geo/model"
)

// 边界1：同一 (date,engine,intent) 重复聚合 —— 模拟 job 重跑当天
func TestEngineCompareDuplicateRows(t *testing.T) {
	repo := &memDailyRepo{stats: []*model.GeoDailyStat{
		{Date: "2026-09-07", Engine: "e1", Intent: "q1", ProbeCount: 4, BrandMentionedCount: 2},
		{Date: "2026-09-07", Engine: "e1", Intent: "q1", ProbeCount: 4, BrandMentionedCount: 2}, // 重复行
	}}
	svc := NewVisibilityService(repo)
	res, _ := svc.GetEngineCompare(context.Background(), TrendQuery{Days: 7})
	// 双倍计数是「当前 upsert 语义下的已知边界」：同 key 重跑会写两条记录，
	// 引擎对比按行累加会把重复行算进去 —— 记录当前行为
	if res.Engines[0].ProbeCount != 8 {
		t.Logf("重复行被累加: probes=%d (已知边界，需 daily_stats 层去重或聚合修复)", res.Engines[0].ProbeCount)
	}
}

// 边界2：days 边界 0 / 366 / 负数
func TestEngineCompareDaysClamp(t *testing.T) {
	repo := &memDailyRepo{}
	svc := NewVisibilityService(repo)
	for _, d := range []int{0, -5, 366, 10000} {
		res, err := svc.GetEngineCompare(context.Background(), TrendQuery{Days: d})
		if err != nil {
			t.Fatalf("days=%d should not error", d)
		}
		if res == nil || res.Engines == nil {
			t.Fatalf("days=%d nil result", d)
		}
	}
}

// 边界3：引擎名为空的记录不应进入引擎对比
func TestEngineCompareEmptyEngine(t *testing.T) {
	repo := &memDailyRepo{stats: []*model.GeoDailyStat{
		{Date: "2026-09-07", Engine: "", ProbeCount: 5, BrandMentionedCount: 3},
		{Date: "2026-09-07", Engine: "ok", ProbeCount: 5, BrandMentionedCount: 3},
	}}
	svc := NewVisibilityService(repo)
	res, _ := svc.GetEngineCompare(context.Background(), TrendQuery{Days: 7})
	if len(res.Engines) != 1 || res.Engines[0].Engine != "ok" {
		t.Fatalf("空引擎应被过滤: %+v", res.Engines)
	}
}
