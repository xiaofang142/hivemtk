// package-level 全局登记处：报价两条腿各一份。
//
// 为什么分两格而不是"一个 QuoteRuntime 结构体一起装、一起取"：两条腿的缺件面不同，
// 而路由对两格的答复也不同 —— 生成/读那一半要 store/opps/gate/cfg/scripts，
// 发送那一半另要 approvals/open/reach/events。合成一格就只有两种答案
// （全在 / 全不在），于是"报价能生成、但发送腿没接上外发实例"这一档
// 会被报成"整个报价域没装配"，读的人去找错东西。
//
// 口径与 globalOpportunitySvc 完全一致：atomic.Pointer，装配点写、请求路径读，
// 传 nil 等于撤掉（撤掉之后端点回 503，这是本卡天然的关闸）。
package service

import (
	"sync/atomic"
)

// globalQuoteSvc 生成/读侧的全局实例。
var globalQuoteSvc atomic.Pointer[QuoteService]

// SetGlobalQuoteService 登记全局实例（装配点：internal/app/quote_wiring.go）。
func SetGlobalQuoteService(s *QuoteService) { globalQuoteSvc.Store(s) }

// GlobalQuoteService 取全局实例（可能为 nil；调用方必须判空并给出"未装配"的答复）。
func GlobalQuoteService() *QuoteService { return globalQuoteSvc.Load() }

// globalQuoteSendSvc 发送腿的全局实例。
var globalQuoteSendSvc atomic.Pointer[QuoteSendService]

// SetGlobalQuoteSendService 登记全局实例。
func SetGlobalQuoteSendService(s *QuoteSendService) { globalQuoteSendSvc.Store(s) }

// GlobalQuoteSendService 取全局实例（可能为 nil，判据同上）。
func GlobalQuoteSendService() *QuoteSendService { return globalQuoteSendSvc.Load() }
