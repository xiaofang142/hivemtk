package service

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/db"
	"hivemtk-user/internal/pkg/testutil"
	"hivemtk-user/internal/repository"

	"gorm.io/gorm"
)

func setupAutoTaggerTestDB(t *testing.T) *gorm.DB {
	database := testutil.NewTestDB(t,
		&model.CustomerTag{},
		&model.Customer{},
		&model.CustomerEvent{},
		&model.CustomerTagAssignment{},
	)
	db.SetTestDB(database)
	return database
}

func setupAutoTagger(t *testing.T) *AutoTagger {
	setupAutoTaggerTestDB(t)
	return NewAutoTagger()
}

// TestNewAutoTagger 测试创建 AutoTagger 实例
func TestNewAutoTagger(t *testing.T) {
	tagger := NewAutoTagger()
	if tagger == nil {
		t.Fatal("Expected AutoTagger instance, got nil")
	}
}

// TestAutoTagger_CreateAutoTag 测试创建自动标签
func TestAutoTagger_CreateAutoTag(t *testing.T) {
	tagger := setupAutoTagger(t)

	rule := map[string]any{
		"type":      "simple",
		"field":     "rfm_score",
		"operator":  "gte",
		"threshold": 50,
	}

	err := tagger.CreateAutoTag(context.Background(), "High Value", string(model.TagCategoryBehavioral), rule)
	if err != nil {
		t.Fatalf("CreateAutoTag failed: %v", err)
	}

	tags, err := tagger.GetAutoTags(context.Background())
	if err != nil {
		t.Fatalf("GetAutoTags failed: %v", err)
	}
	if len(tags) != 1 {
		t.Errorf("Expected 1 auto tag, got %d", len(tags))
	}
	if tags[0].Name != "High Value" {
		t.Errorf("Expected tag name 'High Value', got %s", tags[0].Name)
	}
}

// TestAutoTagger_GetAutoTags 测试获取自动标签列表
func TestAutoTagger_GetAutoTags(t *testing.T) {
	tagger := setupAutoTagger(t)

	rule := map[string]any{"type": "simple", "field": "rfm_score", "operator": "gte", "value": 50}
	tagger.CreateAutoTag(context.Background(), "Tag1", string(model.TagCategoryBehavioral), rule)
	tagger.CreateAutoTag(context.Background(), "Tag2", string(model.TagCategoryDemographic), rule)

	tags, err := tagger.GetAutoTags(context.Background())
	if err != nil {
		t.Fatalf("GetAutoTags failed: %v", err)
	}
	if len(tags) != 2 {
		t.Errorf("Expected 2 auto tags, got %d", len(tags))
	}
}

// TestAutoTagger_evaluateSimpleRule 测试简单规则评估
func TestAutoTagger_evaluateSimpleRule(t *testing.T) {
	tagger := setupAutoTagger(t)

	customerData := map[string]any{
		"rfm_score": 75,
	}

	rule := map[string]any{
		"field":    "rfm_score",
		"operator": "gte",
		"value":    50,
	}

	result := tagger.evaluateSimpleRule(context.Background(), customerData, rule)
	if !result {
		t.Error("Expected rule to match (75 >= 50)")
	}

	ruleNotMatch := map[string]any{
		"field":    "rfm_score",
		"operator": "gte",
		"value":    100,
	}
	result2 := tagger.evaluateSimpleRule(context.Background(), customerData, ruleNotMatch)
	if result2 {
		t.Error("Expected rule not to match (75 < 100)")
	}
}

// TestAutoTagger_evaluateEventCountRule 测试事件数量规则评估
func TestAutoTagger_evaluateEventCountRule(t *testing.T) {
	tagger := setupAutoTagger(t)

	events := []*model.CustomerEvent{
		{EventType: model.EventTypePageView},
		{EventType: model.EventTypePageView},
		{EventType: model.EventTypePurchase},
	}

	customerData := map[string]any{
		"events": events,
	}

	rule := map[string]any{
		"type":       "event_count",
		"event_type": "page_view",
		"operator":   "gte",
		"threshold":  2,
	}

	result := tagger.evaluateEventCountRule(context.Background(), customerData, rule)
	if !result {
		t.Error("Expected rule to match (2 page_views >= 2)")
	}

	ruleNotMatch := map[string]any{
		"type":       "event_count",
		"event_type": "purchase",
		"operator":   "gte",
		"threshold":  2,
	}
	result2 := tagger.evaluateEventCountRule(context.Background(), customerData, ruleNotMatch)
	if result2 {
		t.Error("Expected rule not to match (1 purchase < 2)")
	}
}

// TestAutoTagger_evaluatePurchaseAmountRule 测试购买金额规则评估
func TestAutoTagger_evaluatePurchaseAmountRule(t *testing.T) {
	tagger := setupAutoTagger(t)

	customerData := map[string]any{
		"total_purchase_amount": 500.00,
	}

	rule := map[string]any{
		"type":      "purchase_amount",
		"operator":  "gte",
		"threshold": 300.00,
	}

	result := tagger.evaluatePurchaseAmountRule(context.Background(), customerData, rule)
	if !result {
		t.Error("Expected rule to match (500 >= 300)")
	}

	ruleNotMatch := map[string]any{
		"type":      "purchase_amount",
		"operator":  "gte",
		"threshold": 1000.00,
	}
	result2 := tagger.evaluatePurchaseAmountRule(context.Background(), customerData, ruleNotMatch)
	if result2 {
		t.Error("Expected rule not to match (500 < 1000)")
	}
}

// TestAutoTagger_evaluateDaysSinceRule 测试距离天数规则评估
func TestAutoTagger_evaluateDaysSinceRule(t *testing.T) {
	tagger := setupAutoTagger(t)

	customerData := map[string]any{
		"days_since_last_purchase": 45,
	}

	rule := map[string]any{
		"type":      "days_since",
		"field":     "days_since_last_purchase",
		"operator":  "gte",
		"threshold": 30,
	}

	result := tagger.evaluateDaysSinceRule(context.Background(), customerData, rule)
	if !result {
		t.Error("Expected rule to match (45 >= 30)")
	}
}

// TestAutoTagger_evaluateCustomRule_AND 测试自定义规则（AND 逻辑）
func TestAutoTagger_evaluateCustomRule_AND(t *testing.T) {
	tagger := setupAutoTagger(t)

	customerData := map[string]any{
		"rfm_score":             80,
		"total_purchase_amount": 1000.00,
	}

	rule := map[string]any{
		"type":  "custom",
		"logic": "AND",
		"conditions": []any{
			map[string]any{
				"field": "rfm_score", "operator": "gte", "value": 50.0,
			},
			map[string]any{
				"field": "total_purchase_amount", "operator": "gte", "value": 500.0,
			},
		},
	}

	result := tagger.evaluateCustomRule(context.Background(), customerData, rule)
	if !result {
		t.Error("Expected AND rule to match (all conditions met)")
	}
}

// TestAutoTagger_evaluateCustomRule_OR 测试自定义规则（OR 逻辑）
func TestAutoTagger_evaluateCustomRule_OR(t *testing.T) {
	tagger := setupAutoTagger(t)

	customerData := map[string]any{
		"rfm_score": 30,
	}

	rule := map[string]any{
		"type":  "custom",
		"logic": "OR",
		"conditions": []any{
			map[string]any{
				"field": "rfm_score", "operator": "gte", "value": 50.0,
			},
			map[string]any{
				"field": "rfm_score", "operator": "lte", "value": 40.0,
			},
		},
	}

	result := tagger.evaluateCustomRule(context.Background(), customerData, rule)
	if !result {
		t.Error("Expected OR rule to match (one condition met)")
	}
}

// TestAutoTagger_buildCustomerDataSnapshot 测试构建客户数据快照
func TestAutoTagger_buildCustomerDataSnapshot(t *testing.T) {
	tagger := setupAutoTagger(t)

	customer := &model.Customer{
		ID:        "test-customer",
		RFMScore:  75,
		ChurnRisk: "low",
		Tags:      "[]",
	}

	events := []*model.CustomerEvent{
		{
			EventType:  model.EventTypePurchase,
			OccurredAt: tagger.getTimeDaysAgo(context.Background(), 5),
		},
	}
	eventData := map[string]any{"amount": 100.00}
	if err := SetCustomerEventData(events[0], eventData); err != nil {
		t.Fatalf("SetCustomerEventData failed: %v", err)
	}

	snapshot := tagger.buildCustomerDataSnapshot(context.Background(), customer, events)

	if snapshot["rfm_score"] != 75 {
		t.Errorf("Expected rfm_score 75, got %v", snapshot["rfm_score"])
	}

	totalAmount, _ := snapshot["total_purchase_amount"].(float64)
	if totalAmount != 100.00 {
		t.Errorf("Expected total_purchase_amount 100.00, got %v", totalAmount)
	}

	if snapshot["purchase_count"] != 1 {
		t.Errorf("Expected purchase_count 1, got %v", snapshot["purchase_count"])
	}
}

func (a *AutoTagger) getTimeDaysAgo(ctx context.Context, days int) time.Time {
	return time.Now().AddDate(0, 0, -days)
}

// TestAutoTagger_compareValues 测试值比较函数
func TestAutoTagger_compareValues(t *testing.T) {
	tests := []struct {
		name       string
		fieldValue any
		operator   string
		value      any
		want       bool
	}{
		{"等于", 50.0, "eq", 50.0, true},
		{"不等于", 50.0, "ne", 40.0, true},
		{"大于", 60.0, "gt", 50.0, true},
		{"小于", 40.0, "lt", 50.0, true},
		{"大于等于", 50.0, "gte", 50.0, true},
		{"小于等于", 50.0, "lte", 50.0, true},
		{"整数比较", 100, "gte", 50.0, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := compareValues(tt.fieldValue, tt.operator, tt.value)
			if got != tt.want {
				t.Errorf("compareValues(%v, %s, %v) = %v, want %v",
					tt.fieldValue, tt.operator, tt.value, got, tt.want)
			}
		})
	}
}

// TestAutoTagger_compareValues_MismatchedTypes 字段为 string 而规则 value 非 string（商户 JSON 规则可编辑产生）
// 不得 panic（R76 修复：原裸断言 value.(string) 触发 panic），类型不匹配按不命中处理
func TestAutoTagger_compareValues_MismatchedTypes(t *testing.T) {
	mixed := []struct {
		name       string
		fieldValue any
		operator   string
		value      any
	}{
		{"string字段对数字值", "gold", "eq", 50.0},
		{"string字段对布尔值", "gold", "eq", true},
		{"string字段对nil值", "gold", "contains", nil},
		{"int64字段对string值", int64(5), "eq", "5"},
		{"string字段对切片值", "a", "gt", []any{1}},
	}
	for _, tt := range mixed {
		t.Run(tt.name, func(t *testing.T) {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("compareValues(%v, %s, %v) panic: %v", tt.fieldValue, tt.operator, tt.value, r)
				}
			}()
			if got := compareValues(tt.fieldValue, tt.operator, tt.value); got != false {
				t.Errorf("compareValues(%v, %s, %v) = %v, want false(类型不匹配不命中)", tt.fieldValue, tt.operator, tt.value, got)
			}
		})
	}
}

// TestAutoTagger_compareStringValues 测试字符串比较
func TestAutoTagger_compareStringValues(t *testing.T) {
	tests := []struct {
		name       string
		fieldValue string
		operator   string
		value      string
		want       bool
	}{
		{"等于", "VIP", "eq", "VIP", true},
		{"包含", "VIP Customer", "contains", "vip", true},
		{"大于", "zebra", "gt", "apple", true},
		{"小于", "apple", "lt", "zebra", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := compareStringValues(tt.fieldValue, tt.operator, tt.value)
			if got != tt.want {
				t.Errorf("compareStringValues(%s, %s, %s) = %v, want %v",
					tt.fieldValue, tt.operator, tt.value, got, tt.want)
			}
		})
	}
}

// TestAutoTagger_evaluateRuleWithEvents 测试规则评估
func TestAutoTagger_evaluateRuleWithEvents(t *testing.T) {
	tagger := setupAutoTagger(t)

	rule := map[string]any{
		"type":     "simple",
		"field":    "rfm_score",
		"operator": "gte",
		"value":    50,
	}
	ruleJSON, _ := json.Marshal(rule)

	customerData := map[string]any{
		"rfm_score": 75,
	}

	result := tagger.evaluateRuleWithEvents(context.Background(), customerData, string(ruleJSON))
	if !result {
		t.Error("Expected rule to evaluate as true")
	}
}

// TestAutoTagger_ProcessEvent 测试事件处理
func TestAutoTagger_ProcessEvent(t *testing.T) {
	tagger := setupAutoTagger(t)

	customerService := NewCustomerService()
	customer, _ := customerService.CreateOrUpdate(context.Background(), &CustomerDTO{
		Phone: "13800138030",
	})

	rule := map[string]any{
		"type":     "simple",
		"field":    "rfm_score",
		"operator": "gte",
		"value":    0,
	}
	tagger.CreateAutoTag(context.Background(), "Active", string(model.TagCategoryBehavioral), rule)

	event := &model.CustomerEvent{
		CustomerID:  customer.ID,
		EventType:   model.EventTypePageView,
		EventSource: model.EventSourceWebsite,
	}

	err := tagger.ProcessEvent(context.Background(), event)
	if err != nil {
		t.Fatalf("ProcessEvent failed: %v", err)
	}
}

// TestAutoTagger_EvaluateAndTag 测试评估和打标签
func TestAutoTagger_EvaluateAndTag(t *testing.T) {
	tagger := setupAutoTagger(t)

	customerService := NewCustomerService()
	customer, _ := customerService.CreateOrUpdate(context.Background(), &CustomerDTO{
		Phone: "13800138031",
	})

	rule := map[string]any{
		"type":     "simple",
		"field":    "rfm_score",
		"operator": "gte",
		"value":    0,
	}
	tagger.CreateAutoTag(context.Background(), "TestTag", string(model.TagCategoryBehavioral), rule)

	err := tagger.EvaluateAndTag(context.Background(), customer.ID)
	if err != nil {
		t.Fatalf("EvaluateAndTag failed: %v", err)
	}

	updated, _ := tagger.custRepo.GetByID(context.Background(), customer.ID)
	tags := model.GetCustomerTags(updated)
	found := false
	for _, tag := range tags {
		if tag == "TestTag" {
			found = true
			break
		}
	}
	if !found {
		t.Error("Expected TestTag to be added")
	}
}

func setupCustomerWithTags(t *testing.T, tagger *AutoTagger, phone string, tags []string) *model.Customer {
	customerService := NewCustomerService()
	customer, err := customerService.CreateOrUpdate(context.Background(), &CustomerDTO{Phone: phone})
	if err != nil {
		t.Fatalf("CreateOrUpdate failed: %v", err)
	}
	if tags != nil {
		if err := model.SetCustomerTags(customer, tags); err != nil {
			t.Fatalf("SetCustomerTags failed: %v", err)
		}
		if err := tagger.custRepo.Update(context.Background(), customer); err != nil {
			t.Fatalf("custRepo.Update failed: %v", err)
		}
	}
	return customer
}

func createRuleWithRemoveCondition(t *testing.T, tagger *AutoTagger, name string, removeCond map[string]any) {
	rule := map[string]any{
		"type":             "simple",
		"field":            "rfm_score",
		"operator":         "gte",
		"value":            9999,
		"remove_condition": removeCond,
	}
	if err := tagger.CreateAutoTag(context.Background(), name, string(model.TagCategoryBehavioral), rule); err != nil {
		t.Fatalf("CreateAutoTag failed: %v", err)
	}
}

func seedAssignment(t *testing.T, customerID, tagName string, addedAt time.Time) {
	repo := repository.NewCustomerTagAssignmentRepository()
	err := repo.Create(context.Background(), &model.CustomerTagAssignment{
		CustomerID: customerID,
		Tag:        tagName,
		Category:   "behavioral",
		Source:     string(model.TagSourceAuto),
		CreatedAt:  addedAt,
	})
	if err != nil {
		t.Fatalf("seedAssignment failed: %v", err)
	}
}

func getCustomerTags(t *testing.T, tagger *AutoTagger, customerID string) map[string]bool {
	customer, err := tagger.custRepo.GetByID(context.Background(), customerID)
	if err != nil || customer == nil {
		t.Fatalf("custRepo.GetByID failed: %v", err)
	}
	set := make(map[string]bool)
	for _, tag := range model.GetCustomerTags(customer) {
		set[tag] = true
	}
	return set
}

// TestAutoTagger_RemoveCondition_DaysSinceExpired 标签添加超 N 天应被移除（规则本身已不再匹配）
func TestAutoTagger_RemoveCondition_DaysSinceExpired(t *testing.T) {
	tagger := setupAutoTagger(t)

	customer := setupCustomerWithTags(t, tagger, "13900000001", []string{"HotLead"})
	createRuleWithRemoveCondition(t, tagger, "HotLead", map[string]any{"type": "days_since", "days": 90})
	seedAssignment(t, customer.ID, "HotLead", time.Now().AddDate(0, 0, -100))

	if err := tagger.EvaluateAndTag(context.Background(), customer.ID); err != nil {
		t.Fatalf("EvaluateAndTag failed: %v", err)
	}

	if getCustomerTags(t, tagger, customer.ID)["HotLead"] {
		t.Error("Expected HotLead to be removed after 90 days")
	}
}

// TestAutoTagger_RemoveCondition_DaysSinceNotExpired 未到期应保留
func TestAutoTagger_RemoveCondition_DaysSinceNotExpired(t *testing.T) {
	tagger := setupAutoTagger(t)

	customer := setupCustomerWithTags(t, tagger, "13900000002", []string{"HotLead"})
	createRuleWithRemoveCondition(t, tagger, "HotLead", map[string]any{"type": "days_since", "days": 90})
	seedAssignment(t, customer.ID, "HotLead", time.Now().AddDate(0, 0, -10))

	if err := tagger.EvaluateAndTag(context.Background(), customer.ID); err != nil {
		t.Fatalf("EvaluateAndTag failed: %v", err)
	}

	if !getCustomerTags(t, tagger, customer.ID)["HotLead"] {
		t.Error("Expected HotLead to be kept before expiry")
	}
}

// TestAutoTagger_RemoveCondition_EventAbsent 近 N 天无指定事件应被移除
func TestAutoTagger_RemoveCondition_EventAbsent(t *testing.T) {
	tagger := setupAutoTagger(t)
	ctx := context.Background()

	customer := setupCustomerWithTags(t, tagger, "13900000003", []string{"RecentBuyer"})
	createRuleWithRemoveCondition(t, tagger, "RecentBuyer", map[string]any{"type": "event_absent", "event": string(model.EventTypePurchase), "days": 60})

	oldPurchase := &model.CustomerEvent{
		CustomerID:  customer.ID,
		EventType:   model.EventTypePurchase,
		EventSource: model.EventSourceWebsite,
		OccurredAt:  time.Now().AddDate(0, 0, -90),
	}
	eventRepo := repository.NewCustomerEventRepository()
	if err := eventRepo.Record(ctx, oldPurchase); err != nil {
		t.Fatalf("eventRepo.Create failed: %v", err)
	}
	seedAssignment(t, customer.ID, "RecentBuyer", time.Now().AddDate(0, 0, -10))

	if err := tagger.EvaluateAndTag(ctx, customer.ID); err != nil {
		t.Fatalf("EvaluateAndTag failed: %v", err)
	}
	if getCustomerTags(t, tagger, customer.ID)["RecentBuyer"] {
		t.Error("Expected RecentBuyer to be removed (no purchase in last 60 days)")
	}

	recentPurchase := &model.CustomerEvent{
		CustomerID:  customer.ID,
		EventType:   model.EventTypePurchase,
		EventSource: model.EventSourceWebsite,
		OccurredAt:  time.Now().AddDate(0, 0, -5),
	}
	if err := eventRepo.Record(ctx, recentPurchase); err != nil {
		t.Fatalf("eventRepo.Create failed: %v", err)
	}
	if err := model.SetCustomerTags(customer, []string{"RecentBuyer"}); err != nil {
		t.Fatalf("SetCustomerTags failed: %v", err)
	}
	if err := tagger.custRepo.Update(ctx, customer); err != nil {
		t.Fatalf("custRepo.Update failed: %v", err)
	}

	if err := tagger.EvaluateAndTag(ctx, customer.ID); err != nil {
		t.Fatalf("EvaluateAndTag failed: %v", err)
	}
	if !getCustomerTags(t, tagger, customer.ID)["RecentBuyer"] {
		t.Error("Expected RecentBuyer to be kept (recent purchase exists)")
	}
}

// TestAutoTagger_NoRemoveCondition_LegacyBehaviorUnchanged 无 remove_condition 的旧规则行为不变（只增不减）
func TestAutoTagger_NoRemoveCondition_LegacyBehaviorUnchanged(t *testing.T) {
	tagger := setupAutoTagger(t)

	customer := setupCustomerWithTags(t, tagger, "13900000004", []string{"LegacyTag"})
	rule := map[string]any{"type": "simple", "field": "rfm_score", "operator": "gte", "value": 0}
	if err := tagger.CreateAutoTag(context.Background(), "MatchedTag", string(model.TagCategoryBehavioral), rule); err != nil {
		t.Fatalf("CreateAutoTag failed: %v", err)
	}

	if err := tagger.EvaluateAndTag(context.Background(), customer.ID); err != nil {
		t.Fatalf("EvaluateAndTag failed: %v", err)
	}

	tags := getCustomerTags(t, tagger, customer.ID)
	if !tags["LegacyTag"] {
		t.Error("Expected LegacyTag to be kept (tags only grow without remove_condition)")
	}
	if !tags["MatchedTag"] {
		t.Error("Expected MatchedTag to be added by matching rule")
	}
}

// TestAutoTagger_RemoveCondition_BackwardCompatibleJSON remove_condition 解析容错（字段缺失/类型异常不 panic）
func TestAutoTagger_RemoveCondition_BackwardCompatibleJSON(t *testing.T) {
	tagger := setupAutoTagger(t)
	ctx := context.Background()

	customer := setupCustomerWithTags(t, tagger, "13900000005", []string{"OldTag"})

	rule := map[string]any{
		"type": "simple", "field": "rfm_score", "operator": "gte", "value": 9999,
		"remove_condition": "not-an-object",
	}
	if err := tagger.CreateAutoTag(ctx, "OldTag", string(model.TagCategoryBehavioral), rule); err != nil {
		t.Fatalf("CreateAutoTag failed: %v", err)
	}

	if err := tagger.EvaluateAndTag(ctx, customer.ID); err != nil {
		t.Fatalf("EvaluateAndTag failed: %v", err)
	}

	if !getCustomerTags(t, tagger, customer.ID)["OldTag"] {
		t.Error("Expected OldTag to be kept when remove_condition is malformed")
	}

	tagger2 := setupAutoTagger(t)
	customer2 := setupCustomerWithTags(t, tagger2, "13900000006", []string{"LegacyNoRecord"})
	createRuleWithRemoveCondition(t, tagger2, "LegacyNoRecord", map[string]any{"type": "days_since", "days": 30})

	if err := tagger2.EvaluateAndTag(ctx, customer2.ID); err != nil {
		t.Fatalf("EvaluateAndTag failed: %v", err)
	}
	if !getCustomerTags(t, tagger2, customer2.ID)["LegacyNoRecord"] {
		t.Error("Expected tag without assignment record to be kept (conservative)")
	}
}
