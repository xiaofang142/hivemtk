// opportunity_wiring.go 商机竖的装配层（新规划任务清单 T-P4-04）。
//
// 职责与 human_task/order_draft 的装配层同一份契约，且只有一件：把"有没有 DB 句柄"
// 翻译成一个可注入的服务并登记到全局，让路由（现取全局，决定回 200 还是 503）与
// 未来的生产者（P7 的回款完成 → MarkWonByCollection）在同一时刻看到同一份。
//
// 为什么必须在 BuildSmartOrchestrator 之前：本卡还没有编排器侧的消费方，但装配顺序
// 一旦排在它之后就改不动了 —— 届时任何"按全局服务决定挂不挂钩子"的代码都会静默
// 拿到 nil，而那种 nil 与"故意不挂"在日志里长得一模一样。
//
// 为什么本竖不加旗子（与 FF_LTC_ORDER_DRAFT_DB 不同）：旗子守的是"改变既有时序或换存储介质"
// 的失败面；本卡写的是一张新表的新行，不改动任何既有读写路径。真正的关闸更彻底且已经在结构里：
// 不装配 ⇒ 全局服务为 nil ⇒ 五个读口回 503，而 /rules 仍答 —— 契约面与数据面各自独立成数。
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
		logger.Warnf("[opportunity] ⚠️ 无 DB 句柄 ⇒ 商机底座不装配：/api/opportunity/* 的读口与写口全部回 503" +
			"（/rules 仍答，机器规则不碰库）")
		return nil
	}
	svc := service.NewOpportunityService(repository.NewOpportunityRepositoryWithDB(db))
	service.SetGlobalOpportunityService(svc)
	logger.Infof("[opportunity] ✅ 商机底座已装配（表 opportunities）：状态机边表 + 乐观锁 CAS + 派生赢率；" +
		"写 won 只有回款完成一条路，且不经 HTTP")
	return svc
}
