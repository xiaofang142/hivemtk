// geo_wiring.go GEO 关键词挖掘组的装配层（MD-07 首个热点）。
//
// 一句话职责：把 router.Setup 里就地构造的关键词挖掘三件套
// （KeywordMiningService / SeedTermService / DictService）显式收进装配层，
// Router 只收已构造依赖、不再 new。
//
// 为什么先迁这一组：MD-01 测绘里 GEO 域装配自包含、构造器全在 geo_routes.go
// 一处，关键词挖掘是其中行为最独立的一组（三个 service 都无实例内状态、
// 只持 repo + LLMAdapter），迁它不牵动探针闭包/调度/其余 GEO 组。
//
// 等价性边界（与 MD-06 同口径）：
//   - LLMAdapter 是 GetGlobalDispatcher 的无状态包装，装配点各建一份=同一全局
//     裁决来源，行为逐字相同；
//   - 三份 repo 用显式句柄版构造（全局句柄版刻意不产出），与账单/回款/催收/
//     挽回各腿同一口径：装配点拿错 db 是运行期才显形的跨库读写；
//   - SeedTerm/Dict 的全局单例版（NewDictServiceDefault 等）本刀不使用，
//     也不许在 Router 侧复活——权威实例以本函数返回的 runtime 为准。
//
// db 为 nil 时返回 nil（与 InitRecoveryQueueRuntime 同口径）：宁可 GEO 关键词
// 挖掘这组路由没有服务实例，也不用隐式全局句柄蒙混。生产路径 main 在 DB
// fail-closed 之后才进 router.Setup，nil 分支只在异常装配出现。
package app

import (
	"hivemtk-user/internal/geo/repository"
	geoservice "hivemtk-user/internal/geo/service"
	"hivemtk-user/internal/pkg/utils/logger"

	"gorm.io/gorm"
)

// GeoKeywordMiningRuntime 关键词挖掘组一次装配留下的三份服务。
type GeoKeywordMiningRuntime struct {
	KeywordMining *geoservice.KeywordMiningService
	SeedTerm      *geoservice.SeedTermService
	Dict          *geoservice.DictService
}

// InitGeoKeywordMiningRuntime 装配 GEO 关键词挖掘组（MD-07 首刀）。
func InitGeoKeywordMiningRuntime(db *gorm.DB) *GeoKeywordMiningRuntime {
	if db == nil {
		logger.Warnf("[GeoKeywordMining] ⚠️ db 为 nil ⇒ GEO 关键词挖掘组不装配")
		return nil
	}
	llmAdapter := geoservice.NewLLMAdapter()
	return &GeoKeywordMiningRuntime{
		KeywordMining: geoservice.NewKeywordMiningService(
			repository.NewGeoKeywordRepositoryWithDB(db), llmAdapter),
		SeedTerm: geoservice.NewSeedTermService(
			repository.NewGeoSeedTermRepositoryWithDB(db)),
		Dict: geoservice.NewDictService(
			repository.NewGeoDictRepositoryWithDB(db)),
	}
}
