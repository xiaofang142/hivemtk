// sales_workbench_wiring.go 销售工作台读侧的装配层（项11a）。
//
// 为什么需要这一层：SalesWorkbenchService 交付时全仓构造点为 0、五个 Set* 注入点
// 一个都没被调过 —— 它是"实现了但没人调"的教科书样本。GetOverview 一次聚合
// 待办/漏斗/排行/产能，而这些数据分散在草稿竖（草稿待办、旅程、跟进）与事件流
// （销售业绩）两处，装配期不把依赖递进去，端点就只能回一堆 null。
//
// 依赖取自两处，各有出处：
//   - stats / tagger 是无状态单例，直接 new（stats 只读 sales_events，读侧零写入）；
//   - draft / journey / followup 从草稿竖的运行时**借**（OrderDraftServiceForHTTP /
//     JourneyFollowUpForHTTP），因为它们的权威数据是进程内内存，本进程唯一的写入方
//     就是草稿竖（触发器排跟进、建草稿、推阶段）。工作台自建一份 = 待办和漏斗永远是
//     空的，而"空"与"没有"在概览里长得一模一样。
//
// 五层归属：装配在这里（app 层），业务聚合在 service，参数与响应在 controller。
package app

import (
	"context"
	"sync"

	"hivemtk-user/internal/pkg/utils/logger"
	"hivemtk-user/internal/service"
)

var (
	salesWorkbenchMu  sync.Mutex
	salesWorkbenchSvc *service.SalesWorkbenchService
)

// SalesWorkbenchServiceForHTTP 返回装配好的工作台服务（HTTP 装配层与装配先后顺序无关）。
// 未装配时返回 nil：路由照挂，控制器回 503 —— 这是"没装配"的诚实形态，
// 与 order-draft 端点同口径。
func SalesWorkbenchServiceForHTTP() *service.SalesWorkbenchService {
	salesWorkbenchMu.Lock()
	defer salesWorkbenchMu.Unlock()
	return salesWorkbenchSvc
}

// InitSalesWorkbenchRuntime 装配销售工作台服务并存为全局单例。幂等（重复调用覆盖）。
//
// 与 InitOrderDraftRuntime 的关系：调用方（router.Setup）先装配草稿竖、再装配工作台，
// 因为工作台要借它的服务。顺序颠倒也不会崩 —— 借不到就是 nil，概览少几块而已，
// 不会 panic；但日志会把"借了没借到"喊出来，免得端点静默退化。
func InitSalesWorkbenchRuntime() *service.SalesWorkbenchService {
	ctx := context.Background()
	wb := service.NewSalesWorkbenchService()

	wb.SetStats(ctx, service.NewSalesEventStatsService())
	wb.SetTagger(ctx, service.NewAITagger())

	draft := OrderDraftServiceForHTTP()
	journey, followup := JourneyFollowUpForHTTP()
	wb.SetDraft(ctx, draft)
	wb.SetJourney(ctx, journey)
	wb.SetFollowUp(ctx, followup)

	salesWorkbenchMu.Lock()
	salesWorkbenchSvc = wb
	salesWorkbenchMu.Unlock()

	if draft == nil || journey == nil || followup == nil {
		logger.Warnf("[sales-workbench] ⚠️ 未借到草稿竖的依赖（draft=%v journey=%v followup=%v）"+
			" ⇒ 概览里的草稿待办/漏斗/跟进板块为空；检查 %s 与 app.InitOrderDraftRuntime 是否在本函数之前跑过",
			draft != nil, journey != nil, followup != nil, OrderDraftFlagEnv)
	}
	logger.Infof("[sales-workbench] ✅ 工作台读侧已装配（草稿=%v）", draft != nil)
	return wb
}
