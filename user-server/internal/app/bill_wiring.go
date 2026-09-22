// bill_wiring.go 账单派生腿的装配层（新规划任务清单 T-P7-01）。
//
// 一句话职责：把"有没有 DB 句柄"翻译成一台可注入的派生服务并登记到全局，
// 让路由（现取全局，决定回 200 还是 503）在同一时刻看到同一份。
// 形状与 InitQuoteRuntime 逐条相同，只有两处是本竖特有的，写在下面对应的注释里。
//
// 为什么必须有装配点而不是"等 T-P7-02 一起做"：`accepted` 这个值从 T-P6-01 起就在报价的
// 值域里，而全仓非测试代码没有任何一处写它（实测见 service/bill.go 文件头）。
// 这条腿不装进生产启动路径，"客户接了"这件事在库里就**永远不会发生**，
// 于是整个回款域（P7-02 入账、P7-03 催收、P7-04 赢单）没有起点。
//
// 为什么本竖不加旗子（与 T-P3-02 的审批运行时不同）：旗子守的是"改变既有时序或换存储介质"
// 的失败面；本卡写的是一张新表的新行，不改动任何既有读写路径。真正的关闸更彻底且已经在结构里：
// 不装配 ⇒ 全局为 nil ⇒ /api/bill 回 503。
//
// 与报价腿的顺序关系（本文件最需要出声的一处）：派生腿**读**报价仓储但**不**复用
// InitQuoteRuntime 建出来的那台服务 —— 报价服务的方法集里没有 ListLines/UpdateStatus 的
// 组合出口，而账单这条腿要的恰好是那三个读口加一个状态跃迁（判据见 service/bill.go 的
// billQuoteStore）。各建各的仓储实例是这一族的既有口径（无状态，共享反而把两个装配点的
// 缺件面搅在一起）。
package app

import (
	"hivemtk-user/internal/pkg/utils/logger"
	"hivemtk-user/internal/repository"
	"hivemtk-user/internal/service"

	"gorm.io/gorm"
)

// InitBillRuntime 装配账单派生腿并登记为全局实例，可重复调用（后写覆盖）。
//
// 返回 true 的含义是"两把句柄都接上了、服务 Available()"，不是"能开出应收"：
// 后者取决于库里有没有一版已发出的报价，那是业务前提不是装配。
//
// db == nil 时必须把全局**清空**：Init 可被重复调用（测试与灰度重启都是真实路径），
// 不清会让端点继续对着上一份实例回 200，而日志同时写着"未装配"。
func InitBillRuntime(db *gorm.DB) bool {
	if db == nil {
		service.SetGlobalBillService(nil)
		logger.Warnf("[bill] ⚠️ 无 DB 句柄 ⇒ 账单派生腿不装配：/api/bill 回 503，" +
			"且报价的 accepted 这一格在库里没有任何写入口（回款域没有起点）")
		return false
	}

	// 两把句柄都走显式注入：全局句柄版（NewBillRepository()）刻意不产出，
	// 判据与"引用键必须多实例不撞"同族 —— 装配顺序决定的隐式句柄是本仓最难查的一类错因。
	svc := service.NewBillService(
		repository.NewBillRepositoryWithDB(db),
		repository.NewQuoteRepositoryWithDB(db),
	)
	service.SetGlobalBillService(svc)

	ok := svc.Available()
	if !ok {
		// 半装配比不装配更坏：不装配是清一色 503 一眼看得出来，半装配是"路由挂了、
		// 服务在、每条请求都失败"，而失败原因散在两把句柄里。所以这一条必须是 Warn 且点名。
		logger.Warnf("[bill] ❌ 派生腿已登记但 Available() 为假 ⇒ /api/bill 会回 503，检查账单/报价两把句柄")
		return false
	}
	logger.Infof("[bill] ✅ 派生腿已装配：账单金额只取自该版行项目合计，入参里带不进金额")
	return true
}
