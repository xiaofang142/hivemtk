// order_webhook_payment_wiring_test.go T-P7-02：回款腿进入回调这条路的**那一次交接**。
//
// 分工：载荷怎么翻成入参、翻错了报什么，由 order_webhook_payment_test.go 判（它注入一个假 sink，
// 为的是看清中间那一格）。本文件只判一件事，而那一件事恰恰是假 sink 判不到的：
// **生产上有没有人把真的回款腿递给这条回调**。
//
// 为什么这一条必须存在（M33 那一课的第三个版本）：`SetOrderPaymentSink` 是一格导出方法，
// 测试里人人会调，而生产路径上只要没人调，症状就是"webhook 收了单、正文里说回款腿未装配"——
// 一个 503，看上去像"这套系统还没开账单域"，而不是"装配点漏了一行"。
// 全仓 NewIntegrationService 只有一个生产调用点（controller 的构造函数），
// 所以交接只能发生在那一处：构造函数读全局。这条用例钉的就是那次读。
//
// 两格各挡一种坏法：
// ① 全局有实例 ⇒ 构造函数把它带上，回调真的往库里记了一行钱（断言打在落库那一行）；
// ② 全局是空的 ⇒ 不许把一个 typed-nil 的 *PaymentService 塞进接口格子。
// 今天它不会崩（PaymentService 每格都写了 nil 接收者守卫，所以两格的错误文案**一样**），
// 但"接口值非 nil"就是"sink 存在"这件事在这一层的唯一答案 —— 一旦它是假的，
// 下一处用 `if s.payments != nil` 判"有没有腿"的代码就会把 503 记成 500。
// 所以 ② 判的是接缝上的那格**真是 nil**（白盒：本包读得到字段），外加"钱一行都没记"。
package service

import (
	"context"
	"strings"
	"testing"

	"gorm.io/gorm"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/repository"
)

// webhookPaymentBody 一次"渠道说这笔钱到了"的最小载荷（bill_id 由渠道带回，见契约文档）。
func webhookPaymentBody(billID, channelRef string) map[string]any {
	return map[string]any{
		"order_id": "ORD-PAY-WIRING",
		"status":   "paid",
		"payment": map[string]any{
			"bill_id":     billID,
			"channel_ref": channelRef,
			"amount":      120.0,
		},
	}
}

func seedWiringBill(t *testing.T, database *gorm.DB, billID string) {
	t.Helper()
	err := repository.NewBillRepositoryWithDB(database).Create(context.Background(), &model.Bill{
		ID: billID, QuoteID: "QT-" + billID, QuoteRowID: billID + "_1", OpportunityID: "opp_" + billID,
		Status: model.BillStatusOpen, Amount: 300, Currency: model.BillCurrencyDefault,
	})
	if err != nil {
		t.Fatalf("造账单行失败：%v", err)
	}
}

func TestNewIntegrationServiceTakesThePaymentLegFromTheGlobal(t *testing.T) {
	database := setupIntegrationServiceTestDB(t)
	prev := GlobalPaymentService()
	t.Cleanup(func() { SetGlobalPaymentService(prev) })
	SetGlobalPaymentService(NewPaymentService(
		repository.NewPaymentRepositoryWithDB(database),
		repository.NewBillRepositoryWithDB(database),
	))

	const billID = "b_wiring_hook"
	seedWiringBill(t, database, billID)

	svc := NewIntegrationService()
	if svc.payments == nil {
		t.Fatal("全局里明明有回款腿，构造函数却没带上：/api 之外这条回调永远记不到钱")
	}
	res, err := svc.UpsertOrderFromWebhook(context.Background(), "wiring-platform", "ORD-PAY-WIRING", "paid",
		webhookPaymentBody(billID, "ref_wiring_hook_1"))
	if err != nil {
		t.Fatalf("装配点给过实例之后回调仍失败：%v", err)
	}
	if !res.PaymentPresent || res.Payment == nil {
		t.Fatalf("响应里说没带钱（PaymentPresent=%v Payment=%v）", res.PaymentPresent, res.Payment)
	}
	// 断言打在**落库那一行**：接口递进去了但接的是另一把库，上面那两格照样是对的。
	var n int64
	if err := database.Model(&model.Payment{}).Where("bill_id = ?", billID).Count(&n).Error; err != nil {
		t.Fatalf("数回款行失败：%v", err)
	}
	if n != 1 {
		t.Errorf("payments 里有 %d 行，期望 1（钱要通过**构造函数给的这条腿**进库）", n)
	}
	var stored model.Bill
	if err := database.First(&stored, "id = ?", billID).Error; err != nil {
		t.Fatalf("读回账单失败：%v", err)
	}
	if stored.Status != model.BillStatusPartial {
		t.Errorf("库里账单状态=%s，期望 %s", stored.Status, model.BillStatusPartial)
	}
}

// TestNewIntegrationServiceDoesNotWrapATypedNilSink 全局为空时不许产出"非 nil 的接口值"。
func TestNewIntegrationServiceDoesNotWrapATypedNilSink(t *testing.T) {
	database := setupIntegrationServiceTestDB(t)
	prev := GlobalPaymentService()
	t.Cleanup(func() { SetGlobalPaymentService(prev) })
	SetGlobalPaymentService(nil)

	const billID = "b_wiring_nil"
	seedWiringBill(t, database, billID)

	svc := NewIntegrationService()
	if svc.payments != nil {
		t.Errorf("全局为空却带上了一个非 nil 的 sink（typed-nil）：%#v —— 『有没有腿』这一格从此没有可信答案", svc.payments)
	}
	if _, err := svc.UpsertOrderFromWebhook(context.Background(), "wiring-platform", "ORD-PAY-WIRING", "paid",
		webhookPaymentBody(billID, "ref_wiring_nil_1")); err == nil {
		t.Fatal("没有回款腿却入账成功：载荷带了 payment 格子，这一格绝不能被静默丢掉")
	} else if !strings.Contains(err.Error(), "未装配") {
		t.Errorf("错误文本=%v，期望说的是『未装配』而不是别的失败", err)
	}
	var n int64
	if err := database.Model(&model.Payment{}).Count(&n).Error; err != nil {
		t.Fatalf("数回款行失败：%v", err)
	}
	if n != 0 {
		t.Errorf("无腿状态下 payments 有 %d 行", n)
	}
	// 镜像仍然要写：钱记不上是回款腿的事，订单收没收到是我方另一件事（判据同 order_webhook_payment_test.go）。
	var m int64
	if err := database.Model(&model.ExternalOrder{}).Where("platform = ?", "wiring-platform").Count(&m).Error; err != nil {
		t.Fatalf("数镜像失败：%v", err)
	}
	if m != 1 {
		t.Errorf("无腿状态下订单镜像有 %d 行，期望 1", m)
	}
}
