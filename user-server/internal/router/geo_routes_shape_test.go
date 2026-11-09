package router

import (
	"os"
	"strings"
	"testing"
)

// MD-07 形状守卫：关键词挖掘组的三个构造点已迁出 Router（进 internal/app
// geo_wiring.go 的 InitGeoKeywordMiningRuntime），源文本里不得再出现；
// runtime 装配调用必须在。守卫的是"Router 零就地构造"这条迁移后形状，
// 不是行为——行为由 TestGeoRoutesAllModuleGated 与 geo/service 包用例钉。
func TestGeoRoutesKeywordMiningGroupNotConstructedInRouter(t *testing.T) {
	src, err := os.ReadFile("geo_routes.go")
	if err != nil {
		t.Fatalf("读 geo_routes.go 失败：%v", err)
	}
	text := string(src)
	for _, banned := range []string{
		"geoservice.NewKeywordMiningService(",
		"georepo.NewGeoSeedTermRepositoryWithDB(",
		"georepo.NewGeoDictRepositoryWithDB(",
	} {
		if strings.Contains(text, banned) {
			t.Errorf("geo_routes.go 仍就地构造 %s（应迁 app.InitGeoKeywordMiningRuntime）", banned)
		}
	}
	if !strings.Contains(text, "app.InitGeoKeywordMiningRuntime(gormDB)") {
		t.Error("geo_routes.go 应调 app.InitGeoKeywordMiningRuntime(gormDB)")
	}
}
