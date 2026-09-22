// package-level 全局登记处：回款腿一份。
//
// 为什么只有一格：入账（RecordPayment）与对账读（Statement / StatementsOfQuote）是同一台
// 服务的两面，缺件面完全相同（payments 与 bills 两把句柄，见 payment.go 的 Available）——
// 拆成两格只会让"半装配"这种最难查的形状多一个来源。
// 它与账单登记处（bill_global.go）分开，是因为两条腿可以分别装配：订单 webhook 的入账腿
// 在账单域没启用时也该能独立回 503，而 /api/bill 的读侧要同时看两把。
//
// 口径与 globalBillSvc 完全一致：atomic.Pointer，装配点写、请求路径读，
// 传 nil 等于撤掉（撤掉之后入账与读单都回 503，这是本域天然的关闸）。
package service

import (
	"sync/atomic"
)

// globalPaymentSvc 回款腿的全局实例。
var globalPaymentSvc atomic.Pointer[PaymentService]

// SetGlobalPaymentService 登记全局实例（装配点：internal/app/payment_wiring.go；
// 测试里用它洗掉上一用例留下的那把，再断言"未装配"那一支）。
func SetGlobalPaymentService(s *PaymentService) { globalPaymentSvc.Store(s) }

// GlobalPaymentService 取全局实例（可能为 nil；调用方必须判空并给出"未装配"的答复，
// 且**不许**把 nil 直接转成 OrderPaymentSink 接口值 —— typed-nil 会让 Available() 之外的
// 每一格都看起来"有腿"，判据见 integration.go 的 SetOrderPaymentSink 调用点）。
func GlobalPaymentService() *PaymentService { return globalPaymentSvc.Load() }
