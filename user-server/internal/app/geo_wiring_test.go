package app

import (
	"context"
	"testing"

	geomodel "hivemtk-user/internal/geo/model"
	"hivemtk-user/internal/pkg/testutil"
)

// db 为 nil 时不装配（与 InitRecoveryQueueRuntime/InitRecoveryWorker 同一口径）：
// 不用隐式全局句柄蒙混，宁可 GEO 关键词挖掘这组路由没有服务实例。
func TestInitGeoKeywordMiningRuntime_NilDBReturnsNil(t *testing.T) {
	if got := InitGeoKeywordMiningRuntime(nil); got != nil {
		t.Fatalf("db=nil 不应装配，got %v", got)
	}
}

// 显式句柄版构造：三份服务非 nil，且读路径在 PG 测试库上真实往返
// （SeedTerm.ListTerms / Dict.List 均只读，不写库不污染）。
func TestInitGeoKeywordMiningRuntime_ExplicitHandleRoundTrip(t *testing.T) {
	db := testutil.NewTestDB(t, &geomodel.GeoSeedTerm{}, &geomodel.GeoDict{})
	rt := InitGeoKeywordMiningRuntime(db)
	if rt == nil || rt.KeywordMining == nil || rt.SeedTerm == nil || rt.Dict == nil {
		t.Fatal("db 非 nil 时三份服务应齐备")
	}
	ctx := context.Background()
	if _, err := rt.SeedTerm.ListTerms(ctx); err != nil {
		t.Errorf("SeedTerm.ListTerms 经显式句柄读库失败：%v", err)
	}
	if _, err := rt.Dict.List(ctx, ""); err != nil {
		t.Errorf("Dict.List 经显式句柄读库失败：%v", err)
	}
}
