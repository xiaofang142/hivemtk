package repository

import (
	"context"
	"encoding/json"
	"errors"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/db"
	"testing"

	"hivemtk-user/internal/pkg/testutil"

	"gorm.io/gorm"
)

func testTagSetRule(t *model.CustomerTag, rule map[string]any) error {
	jsonData, err := json.Marshal(rule)
	if err != nil {
		return err
	}
	t.Rule = string(jsonData)
	return nil
}

func testTagGetRule(t *model.CustomerTag) map[string]any {
	if t.Rule == "" {
		return map[string]any{}
	}
	var rule map[string]any
	json.Unmarshal([]byte(t.Rule), &rule)
	return rule
}

func testTagSetRuleString(t *model.CustomerTag, ruleStr string) error {
	var tmp map[string]any
	if err := json.Unmarshal([]byte(ruleStr), &tmp); err != nil {
		return err
	}
	t.Rule = ruleStr
	return nil
}

func setupCustomerTagTestDB(t *testing.T) *gorm.DB {
	database := testutil.NewTestDB(t,
		&model.CustomerTag{},
	)
	db.SetTestDB(database)
	return database
}

func setupCustomerTagRepository(t *testing.T) CustomerTagRepository {
	setupCustomerTagTestDB(t)
	return NewCustomerTagRepository()
}

// TestCustomerTagRepository_Create tests creating customer tags
func TestCustomerTagRepository_Create(t *testing.T) {
	repo := setupCustomerTagRepository(t)
	ctx := context.Background()

	tests := []struct {
		name    string
		tag     *model.CustomerTag
		wantErr bool
	}{
		{
			name: "create auto tag",
			tag: &model.CustomerTag{
				Name:     "High Value Customer",
				Category: model.TagCategoryTransactional,
				Source:   model.TagSourceAuto,
			},
			wantErr: false,
		},
		{
			name: "create manual tag",
			tag: &model.CustomerTag{
				Name:     "VIP",
				Category: model.TagCategoryDemographic,
				Source:   model.TagSourceManual,
			},
			wantErr: false,
		},
		{
			name: "create behavioral tag",
			tag: &model.CustomerTag{
				Name:     "Frequent Buyer",
				Category: model.TagCategoryBehavioral,
				Source:   model.TagSourceAuto,
			},
			wantErr: false,
		},
		{
			name: "create psychographic tag",
			tag: &model.CustomerTag{
				Name:     "Eco-conscious",
				Category: model.TagCategoryPsychographic,
				Source:   model.TagSourceManual,
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := repo.Create(ctx, tt.tag)

			if (err != nil) != tt.wantErr {
				t.Errorf("Create() error = %v, wantErr %v", err, tt.wantErr)
			}

			if !tt.wantErr && tt.tag.ID == "" {
				t.Error("Expected tag ID to be set after creation")
			}
		})
	}
}

// TestCustomerTagRepository_GetByID tests retrieving tag by ID
func TestCustomerTagRepository_GetByID(t *testing.T) {
	repo := setupCustomerTagRepository(t)
	ctx := context.Background()

	tag := &model.CustomerTag{
		ID:       "test-tag-id",
		Name:     "Test Tag",
		Category: model.TagCategoryDemographic,
		Source:   model.TagSourceManual,
	}
	repo.Create(ctx, tag)

	tests := []struct {
		name    string
		id      string
		wantNil bool
	}{
		{
			name:    "get existing tag",
			id:      "test-tag-id",
			wantNil: false,
		},
		{
			name:    "get non-existing tag",
			id:      "non-existing-id",
			wantNil: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := repo.GetByID(ctx, tt.id)

			if err != nil {
				t.Errorf("GetByID() error = %v", err)
			}

			if tt.wantNil && result != nil {
				t.Error("Expected nil for non-existing tag")
			}

			if !tt.wantNil {
				if result.ID != tt.id {
					t.Errorf("Expected ID %s, got %s", tt.id, result.ID)
				}
				if result.Name != "Test Tag" {
					t.Errorf("Expected name 'Test Tag', got '%s'", result.Name)
				}
			}
		})
	}
}

func TestCustomerTagRepository_ListByMerchant(t *testing.T) {
	repo := setupCustomerTagRepository(t)
	ctx := context.Background()

	tags := []*model.CustomerTag{
		{Name: "Tag A", Category: model.TagCategoryDemographic, Source: model.TagSourceManual},
		{Name: "Tag B", Category: model.TagCategoryBehavioral, Source: model.TagSourceAuto},
		{Name: "Tag C", Category: model.TagCategoryTransactional, Source: model.TagSourceManual},
		{Name: "Tag D", Category: model.TagCategoryPsychographic, Source: model.TagSourceAuto},
	}

	for _, tag := range tags {
		repo.Create(ctx, tag)
	}

	result, err := repo.ListByMerchant(context.Background())
	if err != nil {
		t.Errorf("ListByMerchant() error = %v", err)
	}

	if len(result) != 4 {
		t.Errorf("Expected 4 tags, got %d", len(result))
	}
}

// TestCustomerTagRepository_ListAutoTags tests listing only auto tags
func TestCustomerTagRepository_ListAutoTags(t *testing.T) {
	repo := setupCustomerTagRepository(t)
	ctx := context.Background()

	tags := []*model.CustomerTag{
		{Name: "Auto Tag 1", Category: model.TagCategoryDemographic, Source: model.TagSourceAuto},
		{Name: "Manual Tag 1", Category: model.TagCategoryDemographic, Source: model.TagSourceManual},
		{Name: "Auto Tag 2", Category: model.TagCategoryBehavioral, Source: model.TagSourceAuto},
		{Name: "Manual Tag 2", Category: model.TagCategoryBehavioral, Source: model.TagSourceManual},
		{Name: "Auto Tag 3", Category: model.TagCategoryTransactional, Source: model.TagSourceAuto},
	}

	for _, tag := range tags {
		repo.Create(ctx, tag)
	}

	result, err := repo.ListAutoTags(context.Background())
	if err != nil {
		t.Errorf("ListAutoTags() error = %v", err)
	}

	if len(result) != 3 {
		t.Errorf("Expected 3 auto tags, got %d", len(result))
	}

	for _, tag := range result {
		if tag.Source != model.TagSourceAuto {
			t.Errorf("Expected auto tag, got source '%s'", tag.Source)
		}
	}
}

// TestCustomerTagRepository_ListAutoTags_WithNoAutoTags tests when no auto tags exist
func TestCustomerTagRepository_ListAutoTags_WithNoAutoTags(t *testing.T) {
	repo := setupCustomerTagRepository(t)
	ctx := context.Background()

	tags := []*model.CustomerTag{
		{Name: "Manual Tag 1", Category: model.TagCategoryDemographic, Source: model.TagSourceManual},
		{Name: "Manual Tag 2", Category: model.TagCategoryBehavioral, Source: model.TagSourceManual},
	}

	for _, tag := range tags {
		repo.Create(ctx, tag)
	}

	result, err := repo.ListAutoTags(context.Background())
	if err != nil {
		t.Errorf("ListAutoTags() error = %v", err)
	}

	if len(result) != 0 {
		t.Errorf("Expected 0 auto tags, got %d", len(result))
	}
}

// TestCustomerTagRepository_Delete tests deleting a tag
func TestCustomerTagRepository_Delete(t *testing.T) {
	repo := setupCustomerTagRepository(t)
	ctx := context.Background()

	tag := &model.CustomerTag{
		ID:       "delete-test-id",
		Name:     "Tag To Delete",
		Category: model.TagCategoryDemographic,
		Source:   model.TagSourceManual,
	}
	repo.Create(ctx, tag)

	err := repo.Delete(ctx, tag.ID)
	if err != nil {
		t.Errorf("Delete() error = %v", err)
	}

	deleted, err := repo.GetByID(ctx, tag.ID)
	if err != nil {
		t.Errorf("GetByID() error = %v", err)
	}

	if deleted != nil {
		t.Error("Expected nil after deletion")
	}
}

// TestCustomerTagRepository_Delete_NotFound 删除不存在的 id 必须报 not found。
//
// #981：原实现直接返回 Delete().Error，GORM 命中 0 行不报错，
// 接口回 200「删除成功」而规则原样还在（假成功）。与同文件 Update 的
// RowsAffected 守卫同一口径，D-35 同型缺陷。
func TestCustomerTagRepository_Delete_NotFound(t *testing.T) {
	repo := setupCustomerTagRepository(t)
	ctx := context.Background()

	err := repo.Delete(ctx, "no-such-tag-id")
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("删除不存在的标签应报 ErrRecordNotFound，实际: %v", err)
	}
}

// TestCustomerTagRepository_Delete_TwiceSecondIsNotFound 已删再删同 id 同样是 not found，
// 不许第二次也回成功（幂等删除不是本接口的契约——前端要靠 404 知道行已不在）。
func TestCustomerTagRepository_Delete_TwiceSecondIsNotFound(t *testing.T) {
	repo := setupCustomerTagRepository(t)
	ctx := context.Background()

	tag := &model.CustomerTag{
		ID:       "delete-twice-id",
		Name:     "Tag Delete Twice",
		Category: model.TagCategoryDemographic,
		Source:   model.TagSourceManual,
	}
	if err := repo.Create(ctx, tag); err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if err := repo.Delete(ctx, tag.ID); err != nil {
		t.Fatalf("第一次删除应成功，实际: %v", err)
	}
	if err := repo.Delete(ctx, tag.ID); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("第二次删除应报 ErrRecordNotFound，实际: %v", err)
	}
}

// TestCustomerTagRepository_TagRule tests tag rule serialization
func TestCustomerTagRepository_TagRule(t *testing.T) {
	repo := setupCustomerTagRepository(t)
	ctx := context.Background()

	tag := &model.CustomerTag{
		Name:     "High Spender",
		Category: model.TagCategoryTransactional,
		Source:   model.TagSourceAuto,
	}

	rule := map[string]any{
		"field":    "total_spent",
		"operator": "greater_than",
		"value":    1000,
	}
	err := testTagSetRule(tag, rule)
	if err != nil {
		t.Errorf("SetRule() error = %v", err)
	}

	err = repo.Create(ctx, tag)
	if err != nil {
		t.Errorf("Create() error = %v", err)
	}

	result, err := repo.GetByID(ctx, tag.ID)
	if err != nil {
		t.Errorf("GetByID() error = %v", err)
	}

	retrievedRule := testTagGetRule(result)

	if retrievedRule["field"] != "total_spent" {
		t.Errorf("Expected field 'total_spent', got '%v'", retrievedRule["field"])
	}

	if retrievedRule["operator"] != "greater_than" {
		t.Errorf("Expected operator 'greater_than', got '%v'", retrievedRule["operator"])
	}

	if retrievedRule["value"] != float64(1000) {
		t.Errorf("Expected value 1000, got '%v'", retrievedRule["value"])
	}
}

// TestCustomerTagRepository_TagRuleString tests tag rule string serialization
func TestCustomerTagRepository_TagRuleString(t *testing.T) {
	repo := setupCustomerTagRepository(t)
	ctx := context.Background()

	tag := &model.CustomerTag{
		Name:     "Frequent Visitor",
		Category: model.TagCategoryBehavioral,
		Source:   model.TagSourceAuto,
	}

	ruleStr := `{"field":"page_views","operator":"greater_than","value":100}`
	err := testTagSetRuleString(tag, ruleStr)
	if err != nil {
		t.Errorf("SetRuleString() error = %v", err)
	}

	err = repo.Create(ctx, tag)
	if err != nil {
		t.Errorf("Create() error = %v", err)
	}

	result, err := repo.GetByID(ctx, tag.ID)
	if err != nil {
		t.Errorf("GetByID() error = %v", err)
	}

	retrievedRule := testTagGetRule(result)

	if retrievedRule["field"] != "page_views" {
		t.Errorf("Expected field 'page_views', got '%v'", retrievedRule["field"])
	}

	if retrievedRule["operator"] != "greater_than" {
		t.Errorf("Expected operator 'greater_than', got '%v'", retrievedRule["operator"])
	}

	if retrievedRule["value"] != float64(100) {
		t.Errorf("Expected value 100, got '%v'", retrievedRule["value"])
	}
}

// TestCustomerTagRepository_AllCategories tests all tag categories
func TestCustomerTagRepository_AllCategories(t *testing.T) {
	repo := setupCustomerTagRepository(t)
	ctx := context.Background()

	categories := []model.TagCategory{
		model.TagCategoryDemographic,
		model.TagCategoryBehavioral,
		model.TagCategoryTransactional,
		model.TagCategoryPsychographic,
	}

	for _, category := range categories {
		tag := &model.CustomerTag{
			Name:     string(category) + " Tag",
			Category: category,
			Source:   model.TagSourceManual,
		}
		err := repo.Create(ctx, tag)
		if err != nil {
			t.Errorf("Create() error for category %s = %v", category, err)
		}
	}

	result, err := repo.ListByMerchant(context.Background())
	if err != nil {
		t.Errorf("ListByMerchant() error = %v", err)
	}

	if len(result) != len(categories) {
		t.Errorf("Expected %d tags, got %d", len(categories), len(result))
	}
}

// TestCustomerTagRepository_BothSources tests both tag sources
func TestCustomerTagRepository_BothSources(t *testing.T) {
	repo := setupCustomerTagRepository(t)
	ctx := context.Background()

	sources := []model.TagSource{
		model.TagSourceAuto,
		model.TagSourceManual,
	}

	for _, source := range sources {
		tag := &model.CustomerTag{
			Name:     string(source) + " Tag",
			Category: model.TagCategoryDemographic,
			Source:   source,
		}
		err := repo.Create(ctx, tag)
		if err != nil {
			t.Errorf("Create() error for source %s = %v", source, err)
		}
	}

	allTags, err := repo.ListByMerchant(context.Background())
	if err != nil {
		t.Errorf("ListByMerchant() error = %v", err)
	}

	if len(allTags) != 2 {
		t.Errorf("Expected 2 tags, got %d", len(allTags))
	}

	autoTags, err := repo.ListAutoTags(context.Background())
	if err != nil {
		t.Errorf("ListAutoTags() error = %v", err)
	}

	if len(autoTags) != 1 {
		t.Errorf("Expected 1 auto tag, got %d", len(autoTags))
	}

	if autoTags[0].Source != model.TagSourceAuto {
		t.Errorf("Expected auto source, got '%s'", autoTags[0].Source)
	}
}

// TestCustomerTagRepository_EmptyRule tests tag with empty rule
func TestCustomerTagRepository_EmptyRule(t *testing.T) {
	repo := setupCustomerTagRepository(t)
	ctx := context.Background()

	tag := &model.CustomerTag{
		Name:     "Simple Tag",
		Category: model.TagCategoryDemographic,
		Source:   model.TagSourceManual,
		Rule:     "",
	}

	err := repo.Create(ctx, tag)
	if err != nil {
		t.Errorf("Create() error = %v", err)
	}

	result, err := repo.GetByID(ctx, tag.ID)
	if err != nil {
		t.Errorf("GetByID() error = %v", err)
	}

	rule := testTagGetRule(result)
	if len(rule) != 0 {
		t.Errorf("Expected empty rule, got %v", rule)
	}
}

// TestCustomerTagRepository_ComplexRule tests tag with complex rule
func TestCustomerTagRepository_ComplexRule(t *testing.T) {
	repo := setupCustomerTagRepository(t)
	ctx := context.Background()

	tag := &model.CustomerTag{
		Name:     "VIP High Spender",
		Category: model.TagCategoryTransactional,
		Source:   model.TagSourceAuto,
	}

	rule := map[string]any{
		"conditions": []any{
			map[string]any{
				"field":    "total_spent",
				"operator": "greater_than",
				"value":    5000,
			},
			map[string]any{
				"field":    "purchase_count",
				"operator": "greater_than",
				"value":    10,
			},
		},
		"logic": "AND",
	}

	err := testTagSetRule(tag, rule)
	if err != nil {
		t.Errorf("SetRule() error = %v", err)
	}

	err = repo.Create(ctx, tag)
	if err != nil {
		t.Errorf("Create() error = %v", err)
	}

	result, err := repo.GetByID(ctx, tag.ID)
	if err != nil {
		t.Errorf("GetByID() error = %v", err)
	}

	retrievedRule := testTagGetRule(result)

	conditions, ok := retrievedRule["conditions"].([]any)
	if !ok {
		t.Fatal("Expected conditions to be []interface{}")
	}

	if len(conditions) != 2 {
		t.Errorf("Expected 2 conditions, got %d", len(conditions))
	}

	if retrievedRule["logic"] != "AND" {
		t.Errorf("Expected logic 'AND', got '%v'", retrievedRule["logic"])
	}
}
