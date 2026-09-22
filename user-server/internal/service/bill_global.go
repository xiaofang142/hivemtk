// package-level 全局登记处：账单派生腿一份。
//
// 为什么只有一格（报价那边是两格）：报价的生成腿与发送腿缺件面不同（发送腿另要审批、
// 待办出口、外发实例、事件表），503 之外还要能回答"能不能读、能不能发"两件不同的事。
// 账单今天只有一条腿——确认成交并开一张应收；读账单属 T-P7-03 的视图，
// 改账单状态属 T-P7-02 的回款累计，两条都还没有实现，先不为它们留格子。
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
