// payment_wiring.go 回款腿的装配层（新规划任务清单 T-P7-02）。
//
// 一句话职责：把"有没有 DB 句柄"翻译成一台可注入的回款服务并登记到全局，
// 让两个消费方（订单 webhook 的入账腿、/api/bill 的对账读腿）在同一时刻看到同一份。
// 形状与 InitBillRuntime 逐条相同，只有"为什么今天必须装"这一格是本竖特有的。
//
// 为什么必须有装配点而不是停在"服务层写完了"：payment.go 与 payment_global.go 落地之后，
// 全仓非测试代码里 NewPaymentService 仍然**零调用点**——那正是 M33 那一课的形状
// （服务层单测全绿而生产没人装）。缺席的后果在这条链上比账单侧更静默：
// webhook 会照收订单镜像、只在正文里说"回款腿未装配"，而读侧恒 503；
// 两者都不是崩溃，于是"钱进了库"这件事永远不发生，而 AC② 的对账正读那一格。
//
// 为什么本竖不加旗子（与 T-P3-02 的审批运行时不同）：旗子守的是"改变既有时序或换存储介质"
// 的失败面；本卡写的是两张新表（payments 与它推动的 bills.status），不改任何既有读写路径。
// 真正的关闸更彻底且已经在结构里：不装配 ⇒ 全局为 nil ⇒ 入账回 503、读单回 503。
//
// 与账单腿的顺序关系：两条腿**互不依赖**（入账读的是 bills 行而不是 BillService），
// 所以 InitPaymentRuntime 可以排在 InitBillRuntime 之前或之后。唯一必须守的是它排在
// setupBillRoutes 与集成控制器构造之前 —— 那两处都在挂载那一刻取一次全局实例，
// 读早了就是永久的 nil（判据见 router/bill_routes_test.go 的 TestBillRoutes_MountedAtTheRightPlace）。
package app

import (
	"hivemtk-user/internal/pkg/utils/logger"
	"hivemtk-user/internal/repository"
	"hivemtk-user/internal/service"

	"gorm.io/gorm"
)

// InitPaymentRuntime 装配回款腿并登记为全局实例，可重复调用（后写覆盖）。
//
// 返回 true 的含义是"两把句柄都接上了、服务 Available()"，不是"能记这笔钱"：
// 后者取决于库里有没有那张没作废的应收，那是业务前提不是装配。
//
// db == nil 时必须把全局**清空**：Init 可被重复调用（测试与灰度重启都是真实路径），
// 不清会让 webhook 继续对着上一份实例把订单镜像写成 200，而日志同时写着"未装配"。
func InitPaymentRuntime(db *gorm.DB) bool {
	if db == nil {
		service.SetGlobalPaymentService(nil)
		logger.Warnf("[payment] ⚠️ 无 DB 句柄 ⇒ 回款腿不装配：订单 webhook 收到带 payment 的推送会回 503，" +
			"且 /api/bill 的两条 GET 恒 503（回款域没有读方也没有写方）")
		return false
	}

	// 两把句柄都走显式注入：全局句柄版（NewPaymentRepository()）刻意不产出，
	// 判据与"引用键必须多实例不撞"同族 —— 装配顺序决定的隐式句柄是本仓最难查的一类错因。
	svc := service.NewPaymentService(
		repository.NewPaymentRepositoryWithDB(db),
		repository.NewBillRepositoryWithDB(db),
	)
	service.SetGlobalPaymentService(svc)

	ok := svc.Available()
	if !ok {
		// 半装配比不装配更坏：不装配是清一色 503 一眼看得出来，半装配是"路由挂了、
		// 服务在、每条请求都失败"，而失败原因散在两把句柄里。所以这一条必须是 Warn 且点名。
		logger.Warnf("[payment] ❌ 回款腿已登记但 Available() 为假 ⇒ 入账与读单都会回 503，检查回款/账单两把句柄")
		return false
	}
	logger.Infof("[payment] ✅ 回款腿已装配：账单的已收只由 payments 表求和得出，bills 上没有第二个数字；" +
		"同一流水号重投只入账一次")
	return true
}
