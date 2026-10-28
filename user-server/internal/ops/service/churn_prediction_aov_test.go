package service

import (
	"context"
	"encoding/json"
	"math"
	"testing"
	"time"

	usermodel "hivemtk-user/internal/model"
	"hivemtk-user/internal/ops/model"
	"hivemtk-user/internal/pkg/db"
	"hivemtk-user/internal/pkg/testutil"

	"gorm.io/gorm"
)

// D06 验收（docs/assessment/functionality-2026-10-27/开发顺序与验收.md）：
// 订单100元+900元，频次2、AOV500元；退款金额按统一净额规则；不同币种不可直接加；
// 重复事件只记一次业务金额；缺失历史金额不补0元真值、不视为低风险。
// 历史缺陷：aggregateCustomerBehavior 把 PurchaseCount90d（次数）赋给 AverageOrderValue。

func setupChurnAOVTestDB(t *testing.T) *gorm.DB {
	database := testutil.NewTestDB(t,
		&model.ChurnPrediction{},
		&model.ChurnWarning{},
		&model.ChurnModelConfig{},
		&model.ChurnStatistics{},
		&usermodel.CustomerEvent{},
	)
	db.SetTestDB(database)
	return database
}

func seedPurchaseEvent(t *testing.T, database *gorm.DB, customerID string, occurredAt time.Time, eventData map[string]any) {
	t.Helper()
	raw, err := json.Marshal(eventData)
	if err != nil {
		t.Fatalf("marshal event data: %v", err)
	}
	e := &usermodel.CustomerEvent{
		CustomerID: customerID,
		EventType:  usermodel.EventTypePurchase,
		EventData:  string(raw),
		OccurredAt: occurredAt,
	}
	if err := database.Create(e).Error; err != nil {
		t.Fatalf("seed purchase event: %v", err)
	}
}

// 验收主用例：100+900 两单 → 频次 2、AOV 500，而不是把次数 2 当金额。
func TestChurnAggregate_AOVFromRealAmounts(t *testing.T) {
	database := setupChurnAOVTestDB(t)
	svc := NewChurnPredictionServiceWithDB(database)
	now := time.Now()

	seedPurchaseEvent(t, database, "cust_aov", now.AddDate(0, 0, -1), map[string]any{"amount": 100.0})
	seedPurchaseEvent(t, database, "cust_aov", now.AddDate(0, 0, -2), map[string]any{"amount": 900.0})
	// 非购买事件不影响频次/AOV
	seedPageViewLikeEvent(t, database, "cust_aov", now.AddDate(0, 0, -1))

	snaps, err := svc.DebugAggregate(context.Background())
	if err != nil {
		t.Fatalf("DebugAggregate: %v", err)
	}
	if len(snaps) != 1 {
		t.Fatalf("expected 1 snapshot, got %d", len(snaps))
	}
	snap := snaps[0]
	if snap.PurchaseFreq != 2 {
		t.Errorf("PurchaseFreq = %d, want 2", snap.PurchaseFreq)
	}
	if math.Abs(snap.AverageOrderValue-500) > 0.001 {
		t.Errorf("AverageOrderValue = %v, want 500", snap.AverageOrderValue)
	}
	if !snap.AOVKnown {
		t.Errorf("AOVKnown = false, want true")
	}
}

// 同一 order_id 的重复上报只记一次业务金额（频次也按去重后的订单数）。
func TestChurnAggregate_DuplicateOrderCountedOnce(t *testing.T) {
	database := setupChurnAOVTestDB(t)
	svc := NewChurnPredictionServiceWithDB(database)
	now := time.Now()

	seedPurchaseEvent(t, database, "cust_dup", now.AddDate(0, 0, -1), map[string]any{"amount": 300.0, "order_id": "ORD-1"})
	seedPurchaseEvent(t, database, "cust_dup", now.AddDate(0, 0, -1), map[string]any{"amount": 300.0, "order_id": "ORD-1"})
	seedPurchaseEvent(t, database, "cust_dup", now.AddDate(0, 0, -2), map[string]any{"amount": 100.0, "order_id": "ORD-2"})

	snaps, err := svc.DebugAggregate(context.Background())
	if err != nil {
		t.Fatalf("DebugAggregate: %v", err)
	}
	if len(snaps) != 1 {
		t.Fatalf("expected 1 snapshot, got %d", len(snaps))
	}
	snap := snaps[0]
	if snap.PurchaseFreq != 2 {
		t.Errorf("PurchaseFreq = %d, want 2 (dedup by order_id)", snap.PurchaseFreq)
	}
	if math.Abs(snap.AverageOrderValue-200) > 0.001 {
		t.Errorf("AverageOrderValue = %v, want 200 (300+100)/2", snap.AverageOrderValue)
	}
}

// 缺金额事件不补 0 元真值：只有无金额事件 → AOV unknown。
func TestChurnAggregate_MissingAmountIsUnknown(t *testing.T) {
	database := setupChurnAOVTestDB(t)
	svc := NewChurnPredictionServiceWithDB(database)
	now := time.Now()

	seedPurchaseEvent(t, database, "cust_noamount", now.AddDate(0, 0, -1), map[string]any{"items": []string{"x"}})

	// 坏 JSON 事件直接手工插入（该客户只有这一条购买事件）
	if err := database.Create(&usermodel.CustomerEvent{
		CustomerID: "cust_badjson",
		EventType:  usermodel.EventTypePurchase,
		EventData:  "{not-json",
		OccurredAt: now.AddDate(0, 0, -1),
	}).Error; err != nil {
		t.Fatalf("seed bad json event: %v", err)
	}

	snaps, err := svc.DebugAggregate(context.Background())
	if err != nil {
		t.Fatalf("DebugAggregate: %v", err)
	}
	byCustomer := map[string]UserBehaviorSnapshot{}
	for _, s := range snaps {
		byCustomer[s.CustomerID] = s
	}
	if s, ok := byCustomer["cust_noamount"]; ok {
		if s.AOVKnown {
			t.Errorf("cust_noamount AOVKnown = true, want false (no valid amount)")
		}
		if s.AverageOrderValue != 0 {
			t.Errorf("cust_noamount AOV = %v, want 0 (unknown, not fabricated)", s.AverageOrderValue)
		}
	}
	// 只有坏 JSON 事件的客户：金额不可信
	if s, ok := byCustomer["cust_badjson"]; ok && s.AOVKnown {
		t.Errorf("cust_badjson AOVKnown = true, want false (corrupt payload excluded)")
	}
}

// 不同币种不可直接加：混币种 → AOV unknown，且不丢币种信息。
func TestChurnAggregate_MixedCurrencyIsUnknown(t *testing.T) {
	database := setupChurnAOVTestDB(t)
	svc := NewChurnPredictionServiceWithDB(database)
	now := time.Now()

	seedPurchaseEvent(t, database, "cust_cny", now.AddDate(0, 0, -1), map[string]any{"amount": 100.0, "currency": "CNY"})
	seedPurchaseEvent(t, database, "cust_cny", now.AddDate(0, 0, -2), map[string]any{"amount": 20.0, "currency": "USD"})

	snaps, err := svc.DebugAggregate(context.Background())
	if err != nil {
		t.Fatalf("DebugAggregate: %v", err)
	}
	if len(snaps) != 1 {
		t.Fatalf("expected 1 snapshot, got %d", len(snaps))
	}
	if snaps[0].AOVKnown {
		t.Errorf("AOVKnown = true, want false (mixed currency must not be summed)")
	}
}

// 单币种时 AOV 已知且币种透出。
func TestChurnAggregate_SingleCurrencyKnown(t *testing.T) {
	database := setupChurnAOVTestDB(t)
	svc := NewChurnPredictionServiceWithDB(database)
	now := time.Now()

	seedPurchaseEvent(t, database, "cust_usd", now.AddDate(0, 0, -1), map[string]any{"amount": 60.0, "currency": "USD"})
	seedPurchaseEvent(t, database, "cust_usd", now.AddDate(0, 0, -2), map[string]any{"amount": 140.0, "currency": "USD"})

	snaps, err := svc.DebugAggregate(context.Background())
	if err != nil {
		t.Fatalf("DebugAggregate: %v", err)
	}
	if len(snaps) != 1 {
		t.Fatalf("expected 1 snapshot, got %d", len(snaps))
	}
	if !snaps[0].AOVKnown || snaps[0].AOVCurrency != "USD" {
		t.Errorf("AOVKnown=%v currency=%q, want true/USD", snaps[0].AOVKnown, snaps[0].AOVCurrency)
	}
	if math.Abs(snaps[0].AverageOrderValue-100) > 0.001 {
		t.Errorf("AverageOrderValue = %v, want 100", snaps[0].AverageOrderValue)
	}
}

// 金额未知时：排除客单价分量并重归一权重，不冒充低风险也不冒充“订单金额偏低”因子。
func TestChurnCalculate_AOVUnknownRenormalizesWeights(t *testing.T) {
	database := setupChurnAOVTestDB(t)
	svc := NewChurnPredictionServiceWithDB(database)

	userData := map[string]any{
		"user_id":                   "user_unknown_aov",
		"days_since_active":         90,
		"days_since_purchase":       120,
		"average_order_value":       0.0,
		"average_order_value_known": false,
		"interactions_30d":          0,
	}
	if err := svc.CalculateChurnPrediction(context.Background(), "user_unknown_aov", userData); err != nil {
		t.Fatalf("CalculateChurnPrediction: %v", err)
	}

	pred, err := svc.GetPredictionByUserID(context.Background(), "user_unknown_aov")
	if err != nil {
		t.Fatalf("GetPredictionByUserID: %v", err)
	}
	// 三个已知分量都是 100 → 重归一后总分仍为 100；若错误地把 aov=0 当真值，
	// 分量同为 100，分数相同——因此这里必须检查因子与权重效果。
	var factors []string
	if err := json.Unmarshal([]byte(pred.RiskFactors), &factors); err != nil {
		t.Fatalf("unmarshal risk factors: %v", err)
	}
	for _, f := range factors {
		if f == "订单金额偏低" {
			t.Errorf("unknown AOV must not produce 订单金额偏低 factor, got %v", factors)
		}
	}
	// unknown 时分量被排除：分数字段存 0 表示无数据，而不是冒充高风险 100。
	if pred.AverageOrderValue != 0 {
		t.Errorf("AverageOrderValue = %v, want 0 for unknown AOV", pred.AverageOrderValue)
	}
}

// 金额未知时权重重归一的数值验证：只有互动分量为已知非满分。
func TestChurnCalculate_AOVUnknownWeightRenormValue(t *testing.T) {
	database := setupChurnAOVTestDB(t)
	svc := NewChurnPredictionServiceWithDB(database)

	// inactive=0, freq=0, engagement=100，AOV unknown → score = 100 * 0.2/0.8 = 25
	userData := map[string]any{
		"user_id":                   "user_renorm",
		"days_since_active":         0,
		"days_since_purchase":       0,
		"average_order_value_known": false,
		"interactions_30d":          0,
	}
	if err := svc.CalculateChurnPrediction(context.Background(), "user_renorm", userData); err != nil {
		t.Fatalf("CalculateChurnPrediction: %v", err)
	}
	pred, err := svc.GetPredictionByUserID(context.Background(), "user_renorm")
	if err != nil {
		t.Fatalf("GetPredictionByUserID: %v", err)
	}
	want := 100.0 * 0.2 / 0.8
	if math.Abs(pred.ChurnScore-want) > 0.001 {
		t.Errorf("ChurnScore = %v, want %v (renormalized without AOV)", pred.ChurnScore, want)
	}
}

// 未带 known 标志的旧调用方保持旧行为：aov=0 → 分量 100（不视为低风险）。
func TestChurnCalculate_BackwardCompatibleWithoutKnownFlag(t *testing.T) {
	database := setupChurnAOVTestDB(t)
	svc := NewChurnPredictionServiceWithDB(database)

	userData := map[string]any{
		"user_id":             "user_legacy",
		"days_since_active":   0,
		"days_since_purchase": 0,
		"average_order_value": 0.0,
		"interactions_30d":    15,
	}
	if err := svc.CalculateChurnPrediction(context.Background(), "user_legacy", userData); err != nil {
		t.Fatalf("CalculateChurnPrediction: %v", err)
	}
	pred, err := svc.GetPredictionByUserID(context.Background(), "user_legacy")
	if err != nil {
		t.Fatalf("GetPredictionByUserID: %v", err)
	}
	// 旧语义：inactive 0*0.3 + freq 0*0.3 + aov 100*0.2 + eng 20*0.2 = 24
	want := 0.0*0.3 + 0.0*0.3 + 100.0*0.2 + 20.0*0.2
	if math.Abs(pred.ChurnScore-want) > 0.001 {
		t.Errorf("ChurnScore = %v, want %v (legacy path unchanged)", pred.ChurnScore, want)
	}
}

// 端到端：聚合 → 计算，已实付金额正确进入评分链路（不再把次数当金额）。
func TestChurnRunForAllCustomers_RealAOVEndToEnd(t *testing.T) {
	database := setupChurnAOVTestDB(t)
	svc := NewChurnPredictionServiceWithDB(database)
	now := time.Now()

	seedPurchaseEvent(t, database, "cust_e2e", now.AddDate(0, 0, -1), map[string]any{"amount": 100.0})
	seedPurchaseEvent(t, database, "cust_e2e", now.AddDate(0, 0, -2), map[string]any{"amount": 900.0})
	seedPageViewLikeEvent(t, database, "cust_e2e", now.AddDate(0, 0, -1))

	n, err := svc.RunChurnCalculationForAllCustomers(context.Background())
	if err != nil {
		t.Fatalf("RunChurnCalculationForAllCustomers: %v", err)
	}
	if n != 1 {
		t.Fatalf("expected 1 user calculated, got %d", n)
	}
	pred, err := svc.GetPredictionByUserID(context.Background(), "cust_e2e")
	if err != nil {
		t.Fatalf("GetPredictionByUserID: %v", err)
	}
	if pred == nil {
		t.Fatal("expected prediction row for cust_e2e")
	}
	// 若仍是旧缺陷（次数 2 当金额），orderValue 分量 = 80；修正后 aov=500 → 分量 10。
	if math.Abs(pred.AverageOrderValue-10) > 0.001 {
		t.Errorf("AverageOrderValue(component) = %.2f, want 10 (aov=500 → score 10; count-as-amount bug would give 80)", pred.AverageOrderValue)
	}
	// 订单金额偏低因子不得出现（分量 10 < 70）。
	var factors []string
	if err := json.Unmarshal([]byte(pred.RiskFactors), &factors); err != nil {
		t.Fatalf("unmarshal risk factors: %v", err)
	}
	for _, f := range factors {
		if f == "订单金额偏低" {
			t.Errorf("aov=500 must not produce 订单金额偏低 factor, got %v", factors)
		}
	}
}

func seedPageViewLikeEvent(t *testing.T, database *gorm.DB, customerID string, occurredAt time.Time) {
	t.Helper()
	if err := database.Create(&usermodel.CustomerEvent{
		CustomerID: customerID,
		EventType:  usermodel.EventTypePageView,
		EventData:  `{"url":"/home"}`,
		OccurredAt: occurredAt,
	}).Error; err != nil {
		t.Fatalf("seed page view event: %v", err)
	}
}
