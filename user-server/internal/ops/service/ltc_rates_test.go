package service

import (
	"context"
	"testing"
	"time"

	sysmodel "hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/testutil"

	"gorm.io/gorm"
)

// setupLtcRatesDB 建三率取数的三张表（商机 / 应收 / 回款），与生产同一模型。
func setupLtcRatesDB(t *testing.T) *gorm.DB {
	t.Helper()
	return testutil.NewTestDB(t, &sysmodel.Opportunity{}, &sysmodel.Bill{}, &sysmodel.Payment{})
}

func seedLtcOpp(t *testing.T, db *gorm.DB, id, status string, expectedClose *time.Time) {
	t.Helper()
	row := &sysmodel.Opportunity{
		ID: id, Code: "OPP-" + id, CustomerID: "cus_" + id,
		Stage: "prospecting", Status: status,
		Amount: 1000, Currency: "CNY", ExpectedCloseAt: expectedClose,
	}
	if err := db.Create(row).Error; err != nil {
		t.Fatalf("种商机行 %s 失败: %v", id, err)
	}
}

func seedLtcBill(t *testing.T, db *gorm.DB, id, quoteRowID string, amount float64, status string) {
	t.Helper()
	row := &sysmodel.Bill{
		ID: id, QuoteID: "q-" + id, QuoteRowID: quoteRowID, OpportunityID: "opp-1",
		Amount: amount, Currency: "CNY", Status: status,
	}
	if err := db.Create(row).Error; err != nil {
		t.Fatalf("种账单行 %s 失败: %v", id, err)
	}
}

func seedLtcPayment(t *testing.T, db *gorm.DB, id, billID string, amount float64, status string) {
	t.Helper()
	row := &sysmodel.Payment{
		ID: id, BillID: billID, ChannelRef: "ref-" + id,
		Amount: amount, Currency: "CNY", Platform: "taobao", OrderID: "ord-" + id,
		Status: status,
	}
	if err := db.Create(row).Error; err != nil {
		t.Fatalf("种回款行 %s 失败: %v", id, err)
	}
}

// TestLtcRatesService_GetLtcRates 三率口径（T-P8-05，LTC-29/AC4）：
//
// 商机 6 行：won×2、lost×1、open×2（一个逾期 2000 年、一个 2099 年未逾期）、
// cancelled×1 ⇒ 闭环率 (2+1)/6 = 0.5（作废行同样进分母）。
// 应收 2 行：1000 open + 500 voided ⇒ 分母 1000（作废剔除）。
// 回款 2 行：600 confirmed + 200 reversed ⇒ 分子 600（冲销不算）。
// 逾期：open 2 行中 1 行逾期 ⇒ 0.5。没定关单日的行另测（NULL 不算逾期）。
func TestLtcRatesService_GetLtcRates(t *testing.T) {
	db := setupLtcRatesDB(t)
	past := time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)
	future := time.Date(2099, 1, 1, 0, 0, 0, 0, time.UTC)
	seedLtcOpp(t, db, "opp-won-1", sysmodel.OpportunityStatusWon, nil)
	seedLtcOpp(t, db, "opp-won-2", sysmodel.OpportunityStatusWon, nil)
	seedLtcOpp(t, db, "opp-lost-1", sysmodel.OpportunityStatusLost, nil)
	seedLtcOpp(t, db, "opp-open-overdue", sysmodel.OpportunityStatusOpen, &past)
	seedLtcOpp(t, db, "opp-open-future", sysmodel.OpportunityStatusOpen, &future)
	seedLtcOpp(t, db, "opp-cancelled-1", sysmodel.OpportunityStatusCancelled, nil)
	seedLtcBill(t, db, "bill-1", "qr-1", 1000, sysmodel.BillStatusOpen)
	seedLtcBill(t, db, "bill-void", "qr-void", 500, sysmodel.BillStatusVoided)
	seedLtcPayment(t, db, "pay-1", "bill-1", 600, sysmodel.PaymentStatusConfirmed)
	seedLtcPayment(t, db, "pay-rev", "bill-1", 200, sysmodel.PaymentStatusReversed)

	svc := NewLtcRatesService().WithDB(db)
	got, err := svc.GetLtcRates(context.Background())
	if err != nil {
		t.Fatalf("GetLtcRates: %v", err)
	}
	if got.Closure.Rate != 0.5 || got.Closure.Numerator != 3 || got.Closure.Denominator != 6 {
		t.Errorf("闭环率 = %+v want {0.5 3 6}", got.Closure)
	}
	if got.Collection.Rate != 0.6 || got.Collection.Numerator != 600 || got.Collection.Denominator != 1000 {
		t.Errorf("回款率 = %+v want {0.6 600 1000}", got.Collection)
	}
	if got.Overdue.Rate != 0.5 || got.Overdue.Numerator != 1 || got.Overdue.Denominator != 2 {
		t.Errorf("逾期率 = %+v want {0.5 1 2}", got.Overdue)
	}
}

// TestLtcRatesService_EmptyDB 空库三率全 0（分母为 0 不炸，分子分母照实返）。
func TestLtcRatesService_EmptyDB(t *testing.T) {
	db := setupLtcRatesDB(t)
	got, err := NewLtcRatesService().WithDB(db).GetLtcRates(context.Background())
	if err != nil {
		t.Fatalf("GetLtcRates: %v", err)
	}
	for name, v := range map[string]struct {
		Rate, Num, Den float64
	}{
		"closure":    {got.Closure.Rate, got.Closure.Numerator, got.Closure.Denominator},
		"collection": {got.Collection.Rate, got.Collection.Numerator, got.Collection.Denominator},
		"overdue":    {got.Overdue.Rate, got.Overdue.Numerator, got.Overdue.Denominator},
	} {
		if v.Rate != 0 || v.Num != 0 || v.Den != 0 {
			t.Errorf("%s = {%v %v %v} want 全 0", name, v.Rate, v.Num, v.Den)
		}
	}
}

// TestLtcRatesService_NullExpectedCloseNotOverdue 没定关单日的 open 商机不算逾期。
func TestLtcRatesService_NullExpectedCloseNotOverdue(t *testing.T) {
	db := setupLtcRatesDB(t)
	seedLtcOpp(t, db, "opp-open-nodate", sysmodel.OpportunityStatusOpen, nil)
	got, err := NewLtcRatesService().WithDB(db).GetLtcRates(context.Background())
	if err != nil {
		t.Fatalf("GetLtcRates: %v", err)
	}
	if got.Overdue.Rate != 0 || got.Overdue.Numerator != 0 || got.Overdue.Denominator != 1 {
		t.Errorf("逾期率 = %+v want {0 0 1}", got.Overdue)
	}
}
