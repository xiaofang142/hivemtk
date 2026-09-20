// opportunity_wiring.go 商机竖的装配层（新规划任务清单 T-P4-04 底座 + T-P4-05 转换竖）。
//
// 职责与 human_task/order_draft 的装配层同一份契约，且只有一件：把"有没有 DB 句柄"
// 翻译成一个可注入的服务并登记到全局，让路由（现取全局，决定回 200 还是 503）与
// 生产者（T-P4-05 起有了第一个：线索挖掘那条写路径）在同一时刻看到同一份。
//
// 为什么必须在路由注册之前：T-P4-04 那半边是路由现取全局，T-P4-05 这半边是 worker 现取全局，
// 两者都按"取到 nil 就退回只读/停用"运作；装配顺序一旦排到它们之后，第一次取全局的那一方
// 会拿到 nil 并把那条路径**永久**留在停用态（worker 不重试装配）。
//
// 为什么本竖不加旗子（与 FF_LTC_ORDER_DRAFT_DB 不同）：旗子守的是"改变既有时序或换存储介质"
// 的失败面；本卡写的是一张新表的新行，不改动任何既有读写路径。真正的关闸更彻底且已经在结构里：
// 不装配 ⇒ 全局服务为 nil ⇒ 五个读口回 503、挖掘侧不转商机，而 /rules 仍答 —— 契约面与数据面各自独立成数。
// 业务侧那道总闸是 ltc.config 的 opportunity 阶段，它关着时转换器照常在场但一条都不建。
package app

import (
	"hivemtk-user/internal/pkg/utils/logger"
	"hivemtk-user/internal/repository"
	"hivemtk-user/internal/service"

	"gorm.io/gorm"
)

// InitOpportunityRuntime 装配商机底座并登记为全局实例，可重复调用（后写覆盖）。
//
// 返回的实例与全局那份是同一个对象，返回值只给调用方做断言用；生产侧不持有它 ——
// 路由从全局取，避免"装配层传一份、路由又 new 一份"造成两套缓存口径。
//
// db == nil 时必须把全局**清空**而不是"什么都不做"：Init 可被重复调用（测试与灰度重启
// 都是真实路径），上一次装配成功的实例若留在全局里，路由就会对着一句"未装配"的告警
// 继续回 200 —— 那是本函数唯一会骗人的方式。
func InitOpportunityRuntime(db *gorm.DB) *service.OpportunityService {
	if db == nil {
		service.SetGlobalOpportunityService(nil)
		// 转换器一起清：它和底座是同一条竖的两半，只清一半会让挖掘那条写路径
		// 继续对着上一次的句柄写行（Available() 仍然为真，因为它自己攥着旧 DB）。
		service.SetGlobalOpportunityConverter(nil)
		logger.Warnf("[opportunity] ⚠️ 无 DB 句柄 ⇒ 商机底座不装配：/api/opportunity/* 的读口与写口全部回 503" +
			"（/rules 仍答，机器规则不碰库）；线索挖掘侧的自动转商机同步停用")
		return nil
	}
	oppRepo := repository.NewOpportunityRepositoryWithDB(db)
	svc := service.NewOpportunityService(oppRepo)
	service.SetGlobalOpportunityService(svc)

	// 线索→商机的转换竖（T-P4-05）。四个句柄都从同一个 db 出发：
	// 名单接错库的后果不是报错，是每一单都走 no_roster、owner 恒为空 ——
	// 一个看起来"配置就是这样"的形状，所以这里不给任何一句兜底。
	conv := service.NewOpportunityConvertService(
		oppRepo,
		service.GlobalLTCConfig(),
		service.NewOwnerAssigner(
			service.NewSalesEventRoster(repository.NewSalesEventRepositoryWithDB(db)), oppRepo),
		repository.NewOperationLogRepositoryWithDB(db),
	)
	service.SetGlobalOpportunityConverter(conv)

	logger.Infof("[opportunity] ✅ 商机底座已装配（表 opportunities）：状态机边表 + 乐观锁 CAS + 派生赢率；" +
		"写 won 只有回款完成一条路，且不经 HTTP")
	logger.Infof("[opportunity] ✅ 线索转商机已装配：达标线索自动建单并按在册销售（sales_events.sales_profile）分配归属；" +
		"总闸是 ltc.config 的 opportunity 阶段，关着时一条都不转")
	return svc
}
