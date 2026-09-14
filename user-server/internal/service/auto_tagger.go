package service

import (
	"context"
	"encoding/json"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/repository"
	"strings"
	"time"
)

// AutoTagger 自动标签服务
type AutoTagger struct {
	tagRepo    repository.CustomerTagRepository
	custRepo   repository.CustomerRepository
	assignRepo repository.CustomerTagAssignmentRepository
}

// NewAutoTagger 创建自动标签服务实例
func NewAutoTagger() *AutoTagger {
	return &AutoTagger{
		tagRepo:    repository.NewCustomerTagRepository(),
		custRepo:   repository.NewCustomerRepository(),
		assignRepo: repository.NewCustomerTagAssignmentRepository(),
	}
}

// TagRule 标签规则定义
type TagRule struct {
	Field    string `json:"field"`
	Operator string `json:"operator"`
	Value    any    `json:"value"`
}

// RemoveCondition 标签移除条件（Rule JSON 可选字段 remove_condition，向后兼容：缺省不移除）
type RemoveCondition struct {
	Type  string `json:"type"`
	Days  int    `json:"days"`
	Event string `json:"event"`
}

// EvaluateAndTag 评估客户并自动打标签（先执行移除评估，再执行添加）
func (s *AutoTagger) EvaluateAndTag(ctx context.Context, customerID string) error {
	customer, err := s.custRepo.GetByID(ctx, customerID)
	if err != nil {
		return err
	}
	if customer == nil {
		return ErrCustomerNotFound
	}

	autoTags, err := s.tagRepo.ListAutoTags(ctx)
	if err != nil {
		return err
	}

	if len(autoTags) == 0 {
		return nil
	}

	currentTags := GetCustomerTags(customer)
	tagSet := make(map[string]bool)
	for _, tag := range currentTags {
		tagSet[tag] = true
	}
	oldTagSet := make(map[string]bool, len(tagSet))
	for tag := range tagSet {
		oldTagSet[tag] = true
	}

	eventRepo := repository.NewCustomerEventRepository()
	events, err := eventRepo.GetByCustomerID(ctx, customerID, 1000)
	if err != nil {
		events = []*model.CustomerEvent{}
	}

	addedAtByTag, hasAddedAt := s.loadAssignmentTimes(ctx, customerID)

	now := time.Now()

	removedTags := make([]string, 0)
	for _, tag := range autoTags {
		if !tagSet[tag.Name] || tag.Rule == "" {
			continue
		}
		var parsed map[string]any
		if err := json.Unmarshal([]byte(tag.Rule), &parsed); err != nil {
			continue
		}
		rcRaw, ok := parsed["remove_condition"]
		if !ok {
			continue
		}
		rcMap, ok := rcRaw.(map[string]any)
		if !ok {
			continue
		}
		addedAt, hasAt := addedAtByTag[tag.Name]
		if s.shouldRemoveTag(rcMap, events, addedAt, hasAt && hasAddedAt, now) {
			if oldTagSet[tag.Name] {
				removedTags = append(removedTags, tag.Name)
			}
			delete(tagSet, tag.Name)
		}
	}

	customerData := s.buildCustomerDataSnapshot(ctx, customer, events)

	for _, tag := range autoTags {
		ruleStr := tag.Rule
		if ruleStr == "" {
			continue
		}

		matched := s.evaluateRuleWithEvents(ctx, customerData, ruleStr)

		if matched {
			tagSet[tag.Name] = true
		}
	}

	newTags := make([]string, 0, len(tagSet))
	for tag := range tagSet {
		newTags = append(newTags, tag)
	}

	if err := SetCustomerTags(customer, newTags); err != nil {
		return err
	}

	s.recordNewAssignments(ctx, customerID, oldTagSet, tagSet, autoTags)
	for _, removedTag := range removedTags {
		_ = s.assignRepo.DeleteByCustomerAndTag(ctx, customerID, removedTag)
	}

	return s.custRepo.Update(ctx, customer)
}

func (s *AutoTagger) loadAssignmentTimes(ctx context.Context, customerID string) (map[string]time.Time, bool) {
	assignments, err := s.assignRepo.ListByCustomerID(ctx, customerID)
	if err != nil {
		return nil, false
	}
	times := make(map[string]time.Time, len(assignments))
	for _, a := range assignments {
		times[a.Tag] = a.CreatedAt
	}
	return times, true
}

func (s *AutoTagger) shouldRemoveTag(cond map[string]any, events []*model.CustomerEvent, addedAt time.Time, hasAddedAt bool, now time.Time) bool {
	condType, _ := cond["type"].(string)

	switch condType {
	case "days_since":
		days := parseRuleDays(cond["days"])
		if !hasAddedAt || addedAt.IsZero() || days <= 0 {
			return false
		}
		return now.Sub(addedAt) > time.Duration(days)*24*time.Hour
	case "event_absent":
		eventName, _ := cond["event"].(string)
		if eventName == "" {
			return false
		}
		days := parseRuleDays(cond["days"])
		cutoff := now.AddDate(0, 0, -days)
		for _, event := range events {
			if string(event.EventType) == eventName && event.OccurredAt.After(cutoff) {
				return false
			}
		}
		return true
	default:
		return false
	}
}

func parseRuleDays(v any) int {
	switch n := v.(type) {
	case float64:
		return int(n)
	case int:
		return n
	case int64:
		return int(n)
	default:
		return 0
	}
}

func (s *AutoTagger) recordNewAssignments(ctx context.Context, customerID string, oldTagSet, tagSet map[string]bool, autoTags []*model.CustomerTag) {
	categoryByTag := make(map[string]string, len(autoTags))
	for _, tag := range autoTags {
		categoryByTag[tag.Name] = string(tag.Category)
	}

	for tagName := range tagSet {
		if oldTagSet[tagName] {
			continue
		}
		existing, err := s.assignRepo.GetByCustomerAndTag(ctx, customerID, tagName)
		if err == nil && existing != nil {
			continue
		}
		_ = s.assignRepo.Create(ctx, &model.CustomerTagAssignment{
			CustomerID: customerID,
			Tag:        tagName,
			Category:   categoryByTag[tagName],
			Source:     string(model.TagSourceAuto),
		})
	}
}

// ProcessEvent 处理事件以触发标签更新
func (s *AutoTagger) ProcessEvent(ctx context.Context, event *model.CustomerEvent) error {
	if event == nil || event.CustomerID == "" {
		return nil
	}

	return s.EvaluateAndTag(ctx, event.CustomerID)
}

func (s *AutoTagger) evaluateRuleWithEvents(ctx context.Context, customerData map[string]any, ruleStr string) bool {

	var rule map[string]any
	if err := json.Unmarshal([]byte(ruleStr), &rule); err != nil {
		return false
	}

	ruleType, _ := rule["type"].(string)

	switch ruleType {
	case "event_count":
		return s.evaluateEventCountRule(ctx, customerData, rule)
	case "purchase_amount":
		return s.evaluatePurchaseAmountRule(ctx, customerData, rule)
	case "days_since":
		return s.evaluateDaysSinceRule(ctx, customerData, rule)
	case "custom":
		return s.evaluateCustomRule(ctx, customerData, rule)
	case "simple":
		return s.evaluateSimpleRule(ctx, customerData, rule)
	default:
		return s.evaluateSimpleRule(ctx, customerData, rule)
	}
}

func (s *AutoTagger) evaluateSimpleRule(ctx context.Context, customerData map[string]any, rule map[string]any) bool {
	field, _ := rule["field"].(string)
	operator, _ := rule["operator"].(string)
	value := rule["value"]

	if field == "" || operator == "" {
		return false
	}

	fieldValue, exists := customerData[field]
	if !exists {
		return false
	}

	return compareValues(fieldValue, operator, value)
}

func (s *AutoTagger) evaluateEventCountRule(ctx context.Context, customerData map[string]any, rule map[string]any) bool {
	eventType, _ := rule["event_type"].(string)
	operator, _ := rule["operator"].(string)

	var threshold int
	switch v := rule["threshold"].(type) {
	case float64:
		threshold = int(v)
	case int:
		threshold = v
	default:
		threshold = 0
	}

	events, _ := customerData["events"].([]*model.CustomerEvent)
	if events == nil {
		return false
	}

	count := 0
	for _, event := range events {
		if eventType == "" || string(event.EventType) == eventType {
			count++
		}
	}

	return compareValues(count, operator, float64(threshold))
}

func (s *AutoTagger) evaluatePurchaseAmountRule(ctx context.Context, customerData map[string]any, rule map[string]any) bool {
	operator, _ := rule["operator"].(string)
	thresholdVal, ok := rule["threshold"].(float64)
	if !ok {
		return false
	}
	threshold := thresholdVal

	totalAmount, _ := customerData["total_purchase_amount"].(float64)

	return compareValues(totalAmount, operator, threshold)
}

func (s *AutoTagger) evaluateDaysSinceRule(ctx context.Context, customerData map[string]any, rule map[string]any) bool {
	field, _ := rule["field"].(string)
	operator, _ := rule["operator"].(string)

	var threshold int
	switch v := rule["threshold"].(type) {
	case float64:
		threshold = int(v)
	case int:
		threshold = v
	default:
		threshold = 0
	}

	days, ok := customerData[field].(int)
	if !ok {
		if daysFloat, ok := customerData[field].(float64); ok {
			days = int(daysFloat)
		}
	}

	return compareValues(days, operator, float64(threshold))
}

func (s *AutoTagger) evaluateCustomRule(ctx context.Context, customerData map[string]any, rule map[string]any) bool {
	conditions, ok := rule["conditions"].([]any)
	if !ok {
		return false
	}

	logic, _ := rule["logic"].(string)
	if logic == "" {
		logic = "AND"
	}

	results := make([]bool, 0, len(conditions))
	for _, cond := range conditions {
		condMap, ok := cond.(map[string]any)
		if !ok {
			continue
		}
		results = append(results, s.evaluateSimpleRule(ctx, customerData, condMap))
	}

	if len(results) == 0 {
		return false
	}

	if logic == "OR" {
		for _, r := range results {
			if r {
				return true
			}
		}
		return false
	}

	for _, r := range results {
		if !r {
			return false
		}
	}
	return true
}

func (s *AutoTagger) buildCustomerDataSnapshot(ctx context.Context, customer *model.Customer, events []*model.CustomerEvent) map[string]any {
	now := time.Now()

	var totalPurchaseAmount float64
	var purchaseCount int
	var lastPurchaseAt time.Time

	for _, event := range events {
		if event.EventType == model.EventTypePurchase {
			purchaseCount++
			eventData := GetCustomerEventData(event)
			if amount, ok := eventData["amount"].(float64); ok {
				totalPurchaseAmount += amount
			}
			if event.OccurredAt.After(lastPurchaseAt) {
				lastPurchaseAt = event.OccurredAt
			}
		}
	}

	daysSinceLastPurchase := 0
	if !lastPurchaseAt.IsZero() {
		daysSinceLastPurchase = int(now.Sub(lastPurchaseAt).Hours() / 24)
	}

	daysSinceActive := 0
	if len(events) > 0 {
		lastActiveAt := events[0].OccurredAt
		for _, event := range events {
			if event.OccurredAt.After(lastActiveAt) {
				lastActiveAt = event.OccurredAt
			}
		}
		daysSinceActive = int(now.Sub(lastActiveAt).Hours() / 24)
	}

	interactions30d := 0
	thirtyDaysAgo := now.AddDate(0, 0, -30)
	for _, event := range events {
		if event.OccurredAt.After(thirtyDaysAgo) {
			interactions30d++
		}
	}

	return map[string]any{
		"customer_id":              customer.ID,
		"rfm_score":                customer.RFMScore,
		"churn_risk":               customer.ChurnRisk,
		"total_purchase_amount":    totalPurchaseAmount,
		"purchase_count":           purchaseCount,
		"days_since_last_purchase": daysSinceLastPurchase,
		"days_since_active":        daysSinceActive,
		"interactions_30d":         interactions30d,
		"events":                   events,
		"tags":                     GetCustomerTags(customer),
	}
}

func compareValues(fieldValue any, operator string, value any) bool {
	var fv float64
	var fvOk bool

	switch v := fieldValue.(type) {
	case float64:
		fv, fvOk = v, true
	case int:
		fv, fvOk = float64(v), true
	case int64:
		fv, fvOk = float64(v), true
	case string:
		// 规则 value 来自商户可编辑 JSON，字段为 string 时值可能是数字/布尔类型——裸断言 value.(string) 会 panic，类型不匹配按不命中处理
		sv, svOk := value.(string)
		if !svOk {
			return false
		}
		return compareStringValues(v, operator, sv)
	default:
		return false
	}

	if !fvOk {
		return false
	}

	var val float64
	switch v := value.(type) {
	case float64:
		val = v
	case int:
		val = float64(v)
	case int64:
		val = float64(v)
	default:
		return false
	}

	switch operator {
	case "eq", "=":
		return fv == val
	case "ne", "!=":
		return fv != val
	case "gt", ">":
		return fv > val
	case "lt", "<":
		return fv < val
	case "gte", ">=":
		return fv >= val
	case "lte", "<=":
		return fv <= val
	case "contains":
		return false
	default:
		return false
	}
}

func compareStringValues(fieldValue, operator, value string) bool {
	switch operator {
	case "eq", "=":
		return strings.EqualFold(fieldValue, value)
	case "ne", "!=":
		return !strings.EqualFold(fieldValue, value)
	case "contains":
		return strings.Contains(strings.ToLower(fieldValue), strings.ToLower(value))
	case "gt", ">":
		return fieldValue > value
	case "lt", "<":
		return fieldValue < value
	case "gte", ">=":
		return fieldValue >= value
	case "lte", "<=":
		return fieldValue <= value
	default:
		return false
	}
}

// CreateAutoTag 创建自动标签规则
func (s *AutoTagger) CreateAutoTag(ctx context.Context, name, category string, rule map[string]any) error {
	tag := &model.CustomerTag{
		Name:     name,
		Category: model.TagCategory(category),
		Source:   model.TagSourceAuto,
	}

	if err := customerTagSetRule(tag, rule); err != nil {
		return err
	}

	return s.tagRepo.Create(ctx, tag)
}

func (s *AutoTagger) GetAutoTags(ctx context.Context) ([]*model.CustomerTag, error) {
	return s.tagRepo.ListAutoTags(ctx)
}
