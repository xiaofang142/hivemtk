// human_task_wiring.go 统一待办竖的装配层（新规划任务清单 T-P3-03）。
//
// 职责只有一件：把"有没有 DB 句柄"这个事实翻译成一个可注入的服务，登记到全局，
// 让两个消费方在同一时刻看到同一份 —— 路由（在 router.go 里现取全局，决定回 200 还是 503）
// 与编排器（在 BuildSmartOrchestrator 里挂生产者，决定转人工时投不投待办）。
//
// 为什么这一卡**不加旗子**（与 FF_LTC_ORDER_DRAFT_DB / FF_LTC_APPROVAL_RESUME 不同）：
// 那两把旗子守的是"业务数据换存储介质"与"流程挂起/恢复改变时序"，出错面是数据与状态机；
// 本竖写的是**一张新表的新行**，不改动任何既有读写路径，唯一的失败是"少投一条待办"，
// 而那件事由 transferToHuman 那条 Error 日志说得出、也补得回。
// 真正的关闸已经存在且更彻底：不装配 ⇒ 全局服务为 nil ⇒ 编排器不挂生产者 ⇒
// transferToHuman 与本卡交付前逐字一致（由 TestTransferToHuman_WithoutProducerIsUnchanged 锁定）。
//
// 五层归属：全局登记与取用在这里；"投什么类别、SLA 落哪一列、幂等怎么判"全在 service。
package app

import (
	"context"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/utils/logger"
	"hivemtk-user/internal/repository"
	"hivemtk-user/internal/service"

	"gorm.io/gorm"
)

// InitHumanTaskRuntime 装配统一待办底座并登记为全局实例，可重复调用（后写覆盖）。
//
// 返回的实例与全局那份是同一个对象，返回值只给调用方做断言用；生产侧不持有它 ——
// 路由与编排器各自从全局取，避免"装配层传一份、路由又 new 一份"造成两套缓存口径。
//
// db == nil 时必须把全局**清空**而不是"什么都不做"：Init 可被重复调用（测试里尤其如此），
// 上一次装配成功的实例若留在全局里，路由就会对着一句"未装配"的告警继续回 200。
func InitHumanTaskRuntime(db *gorm.DB) *service.HumanTaskService {
	if db == nil {
		service.SetGlobalHumanTaskService(nil)
		logger.Warnf("[human-task] ⚠️ 无 DB 句柄 ⇒ 待办底座不装配：/api/human-tasks/* 全部回 503、" +
			"转人工不投待办（会话本身仍正常转人工，本卡不改变那条路径）")
		return nil
	}
	svc := service.NewHumanTaskService(
		repository.NewHumanTaskRepositoryWithDB(db),
		service.GlobalConfigParam(), // 会话首响 SLA 的动态参数；nil-safe（读不到回默认 5 分钟）
	)
	service.SetGlobalHumanTaskService(svc)
	logger.Infof("[human-task] ✅ 统一待办底座已装配（表 human_tasks）：三类 kind 共用状态机、"+
		"SLA 各占一列，会话首响取 %s.%s（缺省 %d 分钟）",
		service.HumanTaskConfigGroup, service.HumanTaskHandoffSlaKey, service.DefaultHumanTaskHandoffSlaMinutes)
	return svc
}

// humanTaskProducerFor 造出要挂到编排器上的会话待办生产者；底座未装配 ⇒ nil（不挂）。
//
// 单列成函数的理由与 orderDraftProduceFunc 同一条：让"生产用的那一份"能被测试直接调用，
// 而不是内联在装配里测不到（挂上去的闭包一旦测不到，"投不投、投几条"就只能靠真跑流量验）。
func humanTaskProducerFor() func(ctx context.Context, session *model.CustomerSession, reason string) error {
	return service.HumanTaskHandoffProducer(service.GlobalHumanTaskService())
}

// attachHumanTaskProducer 把生产者挂上编排器，返回是否挂上（false = 底座未装配）。
//
// 调用方：BuildSmartOrchestrator（本文件内，紧邻订单草稿那一次 attach）。
// 未装配时调 SetHumanTaskProducer(nil) 与不调完全等价 —— 编排器那个字段是函数类型，
// nil 就是"没人可叫"，transferToHuman 里的判空分支因此根本不进。
func attachHumanTaskProducer(o *service.SmartCSOrchestrator) bool {
	if o == nil {
		return false
	}
	produce := humanTaskProducerFor()
	o.SetHumanTaskProducer(produce)
	if produce == nil {
		logger.Infof("[human-task] 编排器未挂待办生产者（全局底座为 nil）⇒ 转人工只改会话状态，池子里不会有行")
		return false
	}
	logger.Infof("[human-task] ✅ 编排器已挂会话待办生产者（转人工即投递一条 conversation_handoff）")
	return true
}
