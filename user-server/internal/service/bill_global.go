// package-level 全局登记处：账单派生腿一份。
//
// 为什么只有一格（报价那边是两格）：报价的生成腿与发送腿缺件面不同（发送腿另要审批、
// 待办出口、外发实例、事件表），503 之外还要能回答"能不能读、能不能发"两件不同的事。
// 账单这条登记处**只管派生**：读对账视图（T-P7-02 落地的 Statement 那一族）登记在
// payment_global.go 的另一格里，由 app.InitPaymentRuntime 装 —— 两把接缝分开是有意的，
// 合成一格就只能一起有或一起没有，而"派生能跑、对账读不到"是一种会真实发生的半装配
// （判据见 controller.TestBillController_LegsFailIndependently）。
//
// 口径与 globalQuoteSvc 完全一致：atomic.Pointer，装配点写、请求路径读，
// 传 nil 等于撤掉（撤掉之后端点回 503，这是本域天然的关闸）。
package service

import (
	"sync/atomic"
)

// globalBillSvc 派生腿的全局实例。
var globalBillSvc atomic.Pointer[BillService]

// SetGlobalBillService 登记全局实例（装配点：internal/app/bill_wiring.go）。
func SetGlobalBillService(s *BillService) { globalBillSvc.Store(s) }

// GlobalBillService 取全局实例（可能为 nil；调用方必须判空并给出"未装配"的答复）。
func GlobalBillService() *BillService { return globalBillSvc.Load() }
