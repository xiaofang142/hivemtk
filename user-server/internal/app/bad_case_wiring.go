// bad_case_wiring.go G-2 Bad Case 闭环竖的装配层（新规划任务清单 T-P8-03）。
//
// 职责与 human_task_wiring.go 同一条：把"有没有 DB 句柄"翻译成一个可注入的服务，
// 登记到全局，让两个消费方在同一时刻看到同一份 ——
// 路由（现取全局，决定回 200 还是 503）与编排器（挂自动标记器，决定这一轮记不记）。
//
// 与待办同一取舍：**不加旗子**。本竖写的是"一张新表的新行"，不改动任何既有读写路径，
// 唯一的失败是"少记一条坏例"，而那件事由 Mark 失败时那行 Warn 说得出、也补得回
// （下一轮同一会话再低质还会记）。真正的关闸已经存在且更彻底：不装配 ⇒ 全局为 nil ⇒
// 编排器不挂标记器 ⇒ extractConfidence 那条路径与本卡交付前逐字一致。
package app

import (
	"hivemtk-user/internal/pkg/utils/logger"
	"hivemtk-user/internal/repository"
	"hivemtk-user/internal/service"

	"gorm.io/gorm"
)

// InitBadCaseRuntime 装配 Bad Case 底座并登记为全局实例，可重复调用（后写覆盖）。
//
// db == nil 时必须把全局**清空**而不是"什么都不做"：Init 可被重复调用（测试里尤其如此），
// 上一次装配成功的实例若留在全局里，路由就会对着一句"未装配"的告警继续回 200。
func InitBadCaseRuntime(db *gorm.DB) *service.BadCaseService {
	if db == nil {
		service.SetGlobalBadCaseService(nil)
		logger.Warnf("[bad-case] ⚠️ 无 DB 句柄 ⇒ Bad Case 底座不装配：/api/bad-cases/* 全部回 503、" +
			"低质回答不再留痕（回答本身照常发出，本卡不改变那条路径）")
		return nil
	}
	svc := service.NewBadCaseService(repository.NewBadCaseRepositoryWithDB(db))
	service.SetGlobalBadCaseService(svc)
	logger.Infof("[bad-case] ✅ Bad Case 底座已装配（表 bad_cases）：两条自动判据（低置信度 / 零命中）" +
		"+ 人工补录，打标七类目归四层，导出即盖 eval_set_id 防重导")
	return svc
}

// badCaseMarkerFor 造出要挂到编排器上的自动标记器；底座未装配 ⇒ nil（不挂）。
//
// 单列成函数的理由与 humanTaskProducerFor 同一条：让"生产用的那一份"能被测试直接调用，
// 而不是内联在装配里测不到（挂上去的闭包一旦测不到，"记不记、记几条"就只能靠真跑流量验）。
func badCaseMarkerFor() service.BadCaseMarkerFn {
	return service.BadCaseMarker(service.GlobalBadCaseService())
}

// attachBadCaseMarker 把自动标记器挂上编排器，返回是否挂上（false = 底座未装配）。
func attachBadCaseMarker(o *service.SmartCSOrchestrator) bool {
	if o == nil {
		return false
	}
	marker := badCaseMarkerFor()
	o.SetBadCaseMarker(marker)
	if marker == nil {
		logger.Infof("[bad-case] 编排器未挂自动标记器（全局底座为 nil）⇒ 低质回答不留痕，只照常回答")
		return false
	}
	logger.Infof("[bad-case] ✅ 编排器已挂 Bad Case 自动标记器（每轮回答后按两条判据判定）")
	return true
}
