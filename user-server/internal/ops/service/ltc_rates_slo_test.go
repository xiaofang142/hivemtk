package service

import (
	"context"
	"testing"
	"time"
)

// TestLtcRatesSLODocMatchesCode SLO 口径文档与代码一致性（T-P8-06，AC②）。
//
// docs/operations/SLA_SLO.md §7.1–7.3 的 SQL 在此逐字执行（同一 fixture），
// 结果必须等于 LtcRatesService 的输出。文档改了口径不改这里 → 红灯；
// 代码改了聚合不改文档 → 红灯。两边必须同进退。

const (
	sloDocSQLClosedNum = `SELECT COUNT(*) FROM opportunities WHERE status IN ('won','lost')`
	sloDocSQLTotal     = `SELECT COUNT(*) FROM opportunities`
	sloDocSQLBillDen   = `SELECT COALESCE(SUM(amount),0) FROM bills WHERE status <> 'voided'`
	sloDocSQLPayNum    = `SELECT COALESCE(SUM(amount),0) FROM payments WHERE status IN ('confirmed')`
	sloDocSQLOpenDen   = `SELECT COUNT(*) FROM opportunities WHERE status = 'open'`
	sloDocSQLOverdue   = `SELECT COUNT(*) FROM opportunities WHERE status = 'open' AND expected_close_at IS NOT NULL AND expected_close_at < ?`
)

func TestLtcRatesSLODocMatchesCode(t *testing.T) {
	db := setupLtcRatesDB(t)
	ctx := context.Background()
	now := time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC)
	past := time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)
	future := time.Date(2099, 1, 1, 0, 0, 0, 0, time.UTC)

	seedLtcOpp(t, db, "slo-w1", "won", nil)
	seedLtcOpp(t, db, "slo-w2", "won", nil)
	seedLtcOpp(t, db, "slo-l1", "lost", nil)
	seedLtcOpp(t, db, "slo-o1", "open", &past)
	seedLtcOpp(t, db, "slo-o2", "open", &future)
	seedLtcOpp(t, db, "slo-c1", "cancelled", nil)
	seedLtcBill(t, db, "slo-b1", "qr-1", 1000, "open")
	seedLtcBill(t, db, "slo-b2", "qr-2", 500, "voided")
	seedLtcPayment(t, db, "slo-p1", "slo-b1", 600, "confirmed")
	seedLtcPayment(t, db, "slo-p2", "slo-b1", 200, "reversed")

	var closedNum, total, openDen, overdueNum int64
	var billDen, payNum float64
	if err := db.Raw(sloDocSQLClosedNum).Scan(&closedNum).Error; err != nil {
		t.Fatalf("文档 SQL(闭环分子): %v", err)
	}
	if err := db.Raw(sloDocSQLTotal).Scan(&total).Error; err != nil {
		t.Fatalf("文档 SQL(商机分母): %v", err)
	}
	if err := db.Raw(sloDocSQLBillDen).Scan(&billDen).Error; err != nil {
		t.Fatalf("文档 SQL(应收分母): %v", err)
	}
	if err := db.Raw(sloDocSQLPayNum).Scan(&payNum).Error; err != nil {
		t.Fatalf("文档 SQL(回款分子): %v", err)
	}
	if err := db.Raw(sloDocSQLOpenDen).Scan(&openDen).Error; err != nil {
		t.Fatalf("文档 SQL(逾期分母): %v", err)
	}
	if err := db.Raw(sloDocSQLOverdue, now).Scan(&overdueNum).Error; err != nil {
		t.Fatalf("文档 SQL(逾期分子): %v", err)
	}

	svc := NewLtcRatesService().WithDB(db)
	svc.now = func() time.Time { return now }
	got, err := svc.GetLtcRates(ctx)
	if err != nil {
		t.Fatalf("GetLtcRates: %v", err)
	}

	if got.Closure.Numerator != float64(closedNum) || got.Closure.Denominator != float64(total) {
		t.Errorf("闭环率 文档=(%d/%d) 代码=(%v/%v)", closedNum, total, got.Closure.Numerator, got.Closure.Denominator)
	}
	if got.Collection.Numerator != payNum || got.Collection.Denominator != billDen {
		t.Errorf("回款率 文档=(%v/%v) 代码=(%v/%v)", payNum, billDen, got.Collection.Numerator, got.Collection.Denominator)
	}
	if got.Overdue.Numerator != float64(overdueNum) || got.Overdue.Denominator != float64(openDen) {
		t.Errorf("逾期率 文档=(%d/%d) 代码=(%v/%v)", overdueNum, openDen, got.Overdue.Numerator, got.Overdue.Denominator)
	}
}
