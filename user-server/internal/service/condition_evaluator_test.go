package service

import (
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/db"
	"math"
	"strings"
	"testing"

	contentmodel "hivemtk-user/internal/content/model"
	contentrepo "hivemtk-user/internal/content/repository"
	contentsvc "hivemtk-user/internal/content/service"
	"hivemtk-user/internal/pkg/testutil"
	cdprepo "hivemtk-user/internal/repository"
)

func setupConditionEvaluatorService(t *testing.T) *contentsvc.MarketingFlowService {
	database := testutil.NewTestDB(t,
		&contentmodel.MarketingFlow{},
		&contentmodel.FlowExecution{},
		&model.UserTag{},
	)
	db.SetTestDB(database)
	// AV-04c：service 生产层不再持 *gorm.DB 形参，测试侧直构 Deps。
	return contentsvc.NewMarketingFlowService(contentsvc.MarketingFlowDeps{
		FlowRepo:         contentrepo.NewMarketingFlowRepository(database),
		ExecutionRepo:    contentrepo.NewFlowExecutionRepository(database),
		UserTagRepo:      cdprepo.NewUserTagRepositoryWithDB(database),
		OrderRepo:        cdprepo.NewOrderRepositoryWithDB(database),
		ClueRepo:         cdprepo.NewClueRepositoryWithDB(database),
		SessionRepo:      cdprepo.NewCustomerSessionRepositoryWithDB(database),
		AgentRepo:        cdprepo.NewAgentStatusRepositoryWithDB(database),
		OperationLogRepo: cdprepo.NewOperationLogRepositoryWithDB(database),
	})
}

func TestEvaluateCondition_EqOperator(t *testing.T) {
	service := setupConditionEvaluatorService(t)

	tests := []struct {
		name      string
		condition string
		context   map[string]any
		wantMatch bool
		wantErr   bool
	}{
		{
			name:      "string equality match",
			condition: "status eq active",
			context:   map[string]any{"status": "active"},
			wantMatch: true,
			wantErr:   false,
		},
		{
			name:      "string equality no match",
			condition: "status eq active",
			context:   map[string]any{"status": "inactive"},
			wantMatch: false,
			wantErr:   false,
		},
		{
			name:      "number equality match",
			condition: "count eq 5",
			context:   map[string]any{"count": float64(5)},
			wantMatch: true,
			wantErr:   false,
		},
		{
			name:      "number equality no match",
			condition: "count eq 5",
			context:   map[string]any{"count": float64(10)},
			wantMatch: false,
			wantErr:   false,
		},
		{
			name:      "field not found",
			condition: "missing eq value",
			context:   map[string]any{"other": "value"},
			wantMatch: false,
			wantErr:   false,
		},
		{
			name:      "case insensitive string match",
			condition: "name eq John",
			context:   map[string]any{"name": "john"},
			wantMatch: true,
			wantErr:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			node := contentmodel.FlowNode{
				ID:     "test_condition",
				Type:   "condition",
				Config: map[string]any{"condition": tt.condition},
			}

			result, err := service.EvaluateCondition(node, tt.context)

			if (err != nil) != tt.wantErr {
				t.Fatalf("evaluateCondition() error = %v, wantErr %v", err, tt.wantErr)
			}

			if result == nil {
				t.Fatal("Expected result map to be returned")
			}

			matched, ok := result["_condition_matched"].(bool)
			if !ok {
				t.Fatal("Expected _condition_matched in result")
			}

			if matched != tt.wantMatch {
				t.Errorf("Expected match=%v, got %v", tt.wantMatch, matched)
			}
		})
	}
}

func TestEvaluateCondition_NeOperator(t *testing.T) {
	service := setupConditionEvaluatorService(t)

	tests := []struct {
		name      string
		condition string
		context   map[string]any
		wantMatch bool
	}{
		{
			name:      "string not equal match",
			condition: "status ne inactive",
			context:   map[string]any{"status": "active"},
			wantMatch: true,
		},
		{
			name:      "string not equal no match",
			condition: "status ne inactive",
			context:   map[string]any{"status": "inactive"},
			wantMatch: false,
		},
		{
			name:      "number not equal match",
			condition: "count ne 10",
			context:   map[string]any{"count": float64(5)},
			wantMatch: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			node := contentmodel.FlowNode{
				ID:     "test_condition",
				Type:   "condition",
				Config: map[string]any{"condition": tt.condition},
			}

			result, _ := service.EvaluateCondition(node, tt.context)
			matched := result["_condition_matched"].(bool)

			if matched != tt.wantMatch {
				t.Errorf("Expected match=%v, got %v", tt.wantMatch, matched)
			}
		})
	}
}

func TestEvaluateCondition_GtLtOperators(t *testing.T) {
	service := setupConditionEvaluatorService(t)

	tests := []struct {
		name      string
		condition string
		context   map[string]any
		wantMatch bool
	}{
		{
			name:      "greater than match",
			condition: "amount gt 100",
			context:   map[string]any{"amount": float64(150)},
			wantMatch: true,
		},
		{
			name:      "greater than no match",
			condition: "amount gt 100",
			context:   map[string]any{"amount": float64(50)},
			wantMatch: false,
		},
		{
			name:      "less than match",
			condition: "age lt 18",
			context:   map[string]any{"age": float64(15)},
			wantMatch: true,
		},
		{
			name:      "less than no match",
			condition: "age lt 18",
			context:   map[string]any{"age": float64(25)},
			wantMatch: false,
		},
		{
			name:      "greater or equal match",
			condition: "score gte 90",
			context:   map[string]any{"score": float64(90)},
			wantMatch: true,
		},
		{
			name:      "less or equal match",
			condition: "score lte 100",
			context:   map[string]any{"score": float64(100)},
			wantMatch: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			node := contentmodel.FlowNode{
				ID:     "test_condition",
				Type:   "condition",
				Config: map[string]any{"condition": tt.condition},
			}

			result, _ := service.EvaluateCondition(node, tt.context)
			matched := result["_condition_matched"].(bool)

			if matched != tt.wantMatch {
				t.Errorf("Expected match=%v, got %v", tt.wantMatch, matched)
			}
		})
	}
}

func TestEvaluateCondition_ContainsOperator(t *testing.T) {
	service := setupConditionEvaluatorService(t)

	tests := []struct {
		name      string
		condition string
		context   map[string]any
		wantMatch bool
	}{
		{
			name:      "contains match",
			condition: "email contains @gmail",
			context:   map[string]any{"email": "user@gmail.com"},
			wantMatch: true,
		},
		{
			name:      "contains no match",
			condition: "email contains @gmail",
			context:   map[string]any{"email": "user@yahoo.com"},
			wantMatch: false,
		},
		{
			name:      "contains case insensitive",
			condition: "name contains john",
			context:   map[string]any{"name": "Johnny"},
			wantMatch: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			node := contentmodel.FlowNode{
				ID:     "test_condition",
				Type:   "condition",
				Config: map[string]any{"condition": tt.condition},
			}

			result, _ := service.EvaluateCondition(node, tt.context)
			matched := result["_condition_matched"].(bool)

			if matched != tt.wantMatch {
				t.Errorf("Expected match=%v, got %v", tt.wantMatch, matched)
			}
		})
	}
}

func TestEvaluateCondition_InOperator(t *testing.T) {
	service := setupConditionEvaluatorService(t)

	tests := []struct {
		name      string
		condition string
		context   map[string]any
		wantMatch bool
	}{
		{
			name:      "in list match",
			condition: "status in [active,pending,review]",
			context:   map[string]any{"status": "active"},
			wantMatch: true,
		},
		{
			name:      "in list no match",
			condition: "status in [active,pending,review]",
			context:   map[string]any{"status": "rejected"},
			wantMatch: false,
		},
		{
			name:      "in list with spaces",
			condition: "tier in [gold, silver, platinum]",
			context:   map[string]any{"tier": "gold"},
			wantMatch: true,
		},
		{
			name:      "in list number match",
			condition: "count in [1,5,10]",
			context:   map[string]any{"count": float64(5)},
			wantMatch: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			node := contentmodel.FlowNode{
				ID:     "test_condition",
				Type:   "condition",
				Config: map[string]any{"condition": tt.condition},
			}

			result, _ := service.EvaluateCondition(node, tt.context)
			matched := result["_condition_matched"].(bool)

			if matched != tt.wantMatch {
				t.Errorf("Expected match=%v, got %v", tt.wantMatch, matched)
			}
		})
	}
}

func TestEvaluateCondition_InvalidConditions(t *testing.T) {
	service := setupConditionEvaluatorService(t)

	tests := []struct {
		name      string
		condition string
		context   map[string]any
		wantErr   bool
	}{
		{
			name:      "empty condition",
			condition: "",
			context:   map[string]any{},
			wantErr:   false,
		},
		{
			name:      "invalid operator",
			condition: "status invalid value",
			context:   map[string]any{"status": "active"},
			wantErr:   true,
		},
		{
			name:      "missing field value",
			condition: "status eq",
			context:   map[string]any{},
			wantErr:   true,
		},
		{
			name:      "malformed in list",
			condition: "status in [unclosed",
			context:   map[string]any{"status": "active"},
			wantErr:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			node := contentmodel.FlowNode{
				ID:     "test_condition",
				Type:   "condition",
				Config: map[string]any{"condition": tt.condition},
			}

			_, err := service.EvaluateCondition(node, tt.context)

			if (err != nil) != tt.wantErr {
				t.Errorf("evaluateCondition() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestEvaluateCondition_ConditionBranches(t *testing.T) {
	service := setupConditionEvaluatorService(t)

	node := contentmodel.FlowNode{
		ID:        "test_condition",
		Type:      "condition",
		Name:      "Check Status",
		Config:    map[string]any{"condition": "status eq active"},
		NextNodes: []string{"active_branch", "inactive_branch"},
	}

	context := map[string]any{"status": "active"}
	result, _ := service.EvaluateCondition(node, context)

	if result["_condition_matched"] != true {
		t.Error("Expected condition to match for active status")
	}
	if result["_next_node"] != "active_branch" {
		t.Errorf("Expected next_node 'active_branch', got '%s'", result["_next_node"])
	}

	context = map[string]any{"status": "inactive"}
	result, _ = service.EvaluateCondition(node, context)

	if result["_condition_matched"] != false {
		t.Error("Expected condition to not match for inactive status")
	}
	if result["_next_node"] != "inactive_branch" {
		t.Errorf("Expected next_node 'inactive_branch', got '%s'", result["_next_node"])
	}
}

// TestEvaluateCondition_FullMatrix 100+ 用例全量覆盖（按运营商分桶）
// 覆盖维度：eq/ne/gt/lt/gte/lte/contains/in × 类型（string/float64/int/其他）× 边界值 × 特殊字符
func TestEvaluateCondition_FullMatrix(t *testing.T) {
	service := setupConditionEvaluatorService(t)

	type tc struct {
		name      string
		condition string
		context   map[string]any
		wantMatch bool
		wantErr   bool
	}
	cases := []tc{
		{"eq_str_exact", "name eq John", map[string]any{"name": "John"}, true, false},
		{"eq_str_case_upper", "name eq JOHN", map[string]any{"name": "john"}, true, false},
		{"eq_str_diff", "name eq John", map[string]any{"name": "Jane"}, false, false},
		{"eq_str_empty_value", "name eq", map[string]any{"name": "John"}, false, true},
		{"eq_str_unicode", "city eq 北京", map[string]any{"city": "北京"}, true, false},
		{"eq_str_unicode_diff", "city eq 北京", map[string]any{"city": "上海"}, false, false},
		{"eq_str_with_space", "status eq active user", map[string]any{"status": "active user"}, true, false},
		{"eq_str_with_special", "email eq a@b.c", map[string]any{"email": "a@b.c"}, true, false},
		{"eq_str_long", "title eq " + strings.Repeat("x", 200), map[string]any{"title": strings.Repeat("x", 200)}, true, false},
		{"eq_str_emoji", "mood eq 😊", map[string]any{"mood": "😊"}, true, false},
		{"eq_str_empty_field", "name eq John", map[string]any{}, false, false},
		{"eq_str_zero_value", "name eq 0", map[string]any{"name": ""}, false, false},
		{"eq_str_tab", "tag eq a\tb", map[string]any{"tag": "a\tb"}, true, false},
		{"eq_str_newline", "note eq line1\nline2", map[string]any{"note": "line1\nline2"}, true, false},
		{"eq_str_mix_chinese_english", "msg eq 你好world", map[string]any{"msg": "你好world"}, true, false},

		{"eq_num_int", "count eq 5", map[string]any{"count": float64(5)}, true, false},
		{"eq_num_int_diff", "count eq 5", map[string]any{"count": float64(6)}, false, false},
		{"eq_num_float", "price eq 99.99", map[string]any{"price": float64(99.99)}, true, false},
		{"eq_num_zero", "score eq 0", map[string]any{"score": float64(0)}, true, false},
		{"eq_num_neg", "balance eq -100", map[string]any{"balance": float64(-100)}, true, false},
		{"eq_num_large", "max eq 9999999999", map[string]any{"max": float64(9999999999)}, true, false},
		{"eq_num_small_decimal", "ratio eq 0.0001", map[string]any{"ratio": 0.0001}, true, false},
		{"eq_num_invalid_str", "count eq abc", map[string]any{"count": float64(5)}, false, true},
		{"eq_num_str_value_5", "count eq 5", map[string]any{"count": "5"}, true, false},
		{"eq_num_int_type", "count eq 5", map[string]any{"count": 5}, true, false},
		{"eq_num_neg_zero", "val eq -0", map[string]any{"val": float64(0)}, true, false},
		{"eq_num_inf", "val eq inf", map[string]any{"val": math.Inf(1)}, true, false},

		{"ne_str_diff", "status ne inactive", map[string]any{"status": "active"}, true, false},
		{"ne_str_same", "status ne inactive", map[string]any{"status": "inactive"}, false, false},
		{"ne_str_case", "name ne JOHN", map[string]any{"name": "john"}, false, false},
		{"ne_num_diff", "count ne 10", map[string]any{"count": float64(5)}, true, false},
		{"ne_num_same", "count ne 10", map[string]any{"count": float64(10)}, false, false},
		{"ne_num_zero", "count ne 0", map[string]any{"count": float64(1)}, true, false},
		{"ne_missing_field", "missing ne x", map[string]any{}, false, false},
		{"ne_unicode", "city ne 上海", map[string]any{"city": "北京"}, true, false},
		{"ne_empty", "name ne", map[string]any{"name": "x"}, false, true},
		{"ne_invalid_num", "count ne abc", map[string]any{"count": float64(5)}, false, true},

		{"gt_pos", "amount gt 100", map[string]any{"amount": float64(150)}, true, false},
		{"gt_neg", "amount gt 100", map[string]any{"amount": float64(50)}, false, false},
		{"gt_equal", "amount gt 100", map[string]any{"amount": float64(100)}, false, false},
		{"gt_zero_gt_zero", "x gt 0", map[string]any{"x": float64(0.001)}, true, false},
		{"gt_neg_gt_neg", "x gt -10", map[string]any{"x": float64(-5)}, true, false},
		{"gt_float_int_type", "x gt 5", map[string]any{"x": 6}, true, false},
		{"gt_invalid_str", "amount gt abc", map[string]any{"amount": float64(100)}, false, true},
		{"gt_string_type", "amount gt 100", map[string]any{"amount": "100"}, false, true},
		{"gt_huge_num", "x gt 1e10", map[string]any{"x": 1e11}, true, false},
		{"gt_inf", "x gt 1e308", map[string]any{"x": math.Inf(1)}, true, false},

		{"lt_pos", "age lt 18", map[string]any{"age": float64(15)}, true, false},
		{"lt_neg", "age lt 18", map[string]any{"age": float64(25)}, false, false},
		{"lt_equal", "age lt 18", map[string]any{"age": float64(18)}, false, false},
		{"lt_neg_lt_neg", "x lt -5", map[string]any{"x": float64(-10)}, true, false},
		{"lt_zero_lt_pos", "x lt 0", map[string]any{"x": float64(-1)}, true, false},
		{"lt_small_decimal", "x lt 0.001", map[string]any{"x": 0.0001}, true, false},
		{"lt_invalid", "age lt abc", map[string]any{"age": float64(15)}, false, true},
		{"lt_string_type", "age lt 18", map[string]any{"age": "10"}, false, true},
		{"lt_int_type", "age lt 18", map[string]any{"age": 15}, true, false},
		{"lt_min_int", "x lt 0", map[string]any{"x": math.MinInt64}, true, false},

		{"gte_equal", "score gte 90", map[string]any{"score": float64(90)}, true, false},
		{"gte_greater", "score gte 90", map[string]any{"score": float64(95)}, true, false},
		{"gte_less", "score gte 90", map[string]any{"score": float64(85)}, false, false},
		{"gte_zero", "x gte 0", map[string]any{"x": float64(0)}, true, false},
		{"gte_neg", "x gte -5", map[string]any{"x": float64(-5)}, true, false},
		{"gte_invalid", "score gte abc", map[string]any{"score": float64(90)}, false, true},
		{"gte_string", "score gte 90", map[string]any{"score": "90"}, false, true},
		{"gte_int", "score gte 90", map[string]any{"score": 100}, true, false},

		{"lte_equal", "score lte 100", map[string]any{"score": float64(100)}, true, false},
		{"lte_less", "score lte 100", map[string]any{"score": float64(80)}, true, false},
		{"lte_greater", "score lte 100", map[string]any{"score": float64(120)}, false, false},
		{"lte_zero", "x lte 0", map[string]any{"x": float64(0)}, true, false},
		{"lte_invalid", "score lte abc", map[string]any{"score": float64(100)}, false, true},
		{"lte_string", "score lte 100", map[string]any{"score": "100"}, false, true},
		{"lte_int", "score lte 100", map[string]any{"score": 50}, true, false},
		{"lte_neg_inf", "x lte 0", map[string]any{"x": math.Inf(-1)}, true, false},

		{"contains_match", "email contains @gmail", map[string]any{"email": "user@gmail.com"}, true, false},
		{"contains_nomatch", "email contains @gmail", map[string]any{"email": "user@yahoo.com"}, false, false},
		{"contains_case", "name contains john", map[string]any{"name": "Johnny"}, true, false},
		{"contains_empty_substr", "name contains", map[string]any{"name": "John"}, false, true},
		{"contains_chinese", "msg contains 你好", map[string]any{"msg": "世界你好世界"}, true, false},
		{"contains_number_str", "id contains 123", map[string]any{"id": "abc123def"}, true, false},
		{"contains_int_value", "id contains 123", map[string]any{"id": 12345}, true, false},
		{"contains_nil", "id contains 1", map[string]any{"id": nil}, false, false},
		{"contains_at_start", "url contains https", map[string]any{"url": "https://example.com"}, true, false},
		{"contains_at_end", "url contains .com", map[string]any{"url": "https://example.com"}, true, false},

		{"in_str_match", "status in [active,pending,review]", map[string]any{"status": "active"}, true, false},
		{"in_str_nomatch", "status in [active,pending,review]", map[string]any{"status": "rejected"}, false, false},
		{"in_str_with_space", "tier in [gold, silver, platinum]", map[string]any{"tier": "gold"}, true, false},
		{"in_num_match", "count in [1,5,10]", map[string]any{"count": float64(5)}, true, false},
		{"in_num_nomatch", "count in [1,5,10]", map[string]any{"count": float64(7)}, false, false},
		{"in_single", "status in [only]", map[string]any{"status": "only"}, true, false},
		{"in_missing_field", "status in [a,b]", map[string]any{}, false, false},
		{"in_empty_list", "status in []", map[string]any{"status": "x"}, false, false},
		{"in_no_bracket", "status in active,pending", map[string]any{"status": "active"}, false, true},
		{"in_only_open_bracket", "status in [active", map[string]any{"status": "active"}, false, true},
		{"in_only_close_bracket", "status in active]", map[string]any{"status": "active"}, false, true},
		{"in_unicode", "city in [北京,上海,广州]", map[string]any{"city": "北京"}, true, false},
		{"in_special_chars", "tag in [@,#,$]", map[string]any{"tag": "@"}, true, false},
		{"in_num_str", "count in [1,2,3]", map[string]any{"count": "2"}, true, false},
		{"in_with_empty_item", "tag in [a,,b]", map[string]any{"tag": ""}, true, false},

		{"parse_no_operator", "justfield", map[string]any{"justfield": "x"}, false, true},
		{"parse_only_spaces", "   ", map[string]any{}, false, true},
		{"parse_field_with_spaces", "field name eq value", map[string]any{"field name": "value"}, true, false},
		{"parse_trailing_space", "name eq  John ", map[string]any{"name": "John"}, true, false},
		{"parse_unknown_op", "name ~= John", map[string]any{"name": "John"}, false, true},
		{"parse_contains_long", "name contains john", map[string]any{"name": "john"}, true, false},
	}

	passed, failed := 0, 0
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			node := contentmodel.FlowNode{
				ID:     "tc",
				Type:   "condition",
				Config: map[string]any{"condition": tt.condition},
			}
			result, err := service.EvaluateCondition(node, tt.context)
			if (err != nil) != tt.wantErr {
				t.Errorf("[%s] error=%v wantErr=%v", tt.name, err, tt.wantErr)
				failed++
				return
			}
			if err != nil {
				passed++
				return
			}
			matched, _ := result["_condition_matched"].(bool)
			if matched != tt.wantMatch {
				t.Errorf("[%s] matched=%v want=%v", tt.name, matched, tt.wantMatch)
				failed++
				return
			}
			passed++
		})
	}
	if failed > 0 {
		t.Logf("condition_evaluator: %d/%d passed, %d failed", passed, passed+failed, failed)
	} else {
		t.Logf("condition_evaluator: %d/%d passed", passed, passed+failed)
	}
}

// TestEvaluateCondition_NextNodeRouting 验证 next_nodes 路由选择（0/1/2+ 各种情况）
func TestEvaluateCondition_NextNodeRouting(t *testing.T) {
	service := setupConditionEvaluatorService(t)

	type tc struct {
		name      string
		nextNodes []string
		condition string
		context   map[string]any
		wantNext  any
		wantMatch bool
	}
	cases := []tc{
		{"zero_next_match", []string{}, "x eq 1", map[string]any{"x": float64(1)}, nil, true},
		{"zero_next_nomatch", []string{}, "x eq 1", map[string]any{"x": float64(2)}, nil, false},
		{"one_next_match", []string{"only"}, "x eq 1", map[string]any{"x": float64(1)}, "only", true},
		{"one_next_nomatch", []string{"only"}, "x eq 1", map[string]any{"x": float64(2)}, "only", false},
		{"two_next_match", []string{"yes", "no"}, "x eq 1", map[string]any{"x": float64(1)}, "yes", true},
		{"two_next_nomatch", []string{"yes", "no"}, "x eq 1", map[string]any{"x": float64(2)}, "no", false},
		{"three_next_match", []string{"a", "b", "c"}, "x eq 1", map[string]any{"x": float64(1)}, "a", true},
		{"three_next_nomatch", []string{"a", "b", "c"}, "x eq 1", map[string]any{"x": float64(0)}, "b", false},
		{"missing_field_two_next", []string{"yes", "fallback"}, "x eq 1", map[string]any{}, "fallback", false},
		{"missing_field_one_next", []string{"only"}, "x eq 1", map[string]any{}, "only", false},
		{"missing_field_zero_next", []string{}, "x eq 1", map[string]any{}, nil, false},
	}

	passed, failed := 0, 0
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			node := contentmodel.FlowNode{
				ID:        "routing",
				Type:      "condition",
				Config:    map[string]any{"condition": tt.condition},
				NextNodes: tt.nextNodes,
			}
			result, err := service.EvaluateCondition(node, tt.context)
			if err != nil {
				t.Errorf("[%s] unexpected error: %v", tt.name, err)
				failed++
				return
			}
			matched, _ := result["_condition_matched"].(bool)
			if matched != tt.wantMatch {
				t.Errorf("[%s] matched=%v want=%v", tt.name, matched, tt.wantMatch)
				failed++
				return
			}
			got, hasNext := result["_next_node"]
			if tt.wantNext == nil {
				if hasNext {
					t.Errorf("[%s] expected no _next_node, got %v", tt.name, got)
					failed++
					return
				}
			} else {
				if got != tt.wantNext {
					t.Errorf("[%s] _next_node=%v want=%v", tt.name, got, tt.wantNext)
					failed++
					return
				}
			}
			passed++
		})
	}
	if failed > 0 {
		t.Logf("routing: %d/%d passed, %d failed", passed, passed+failed, failed)
	}
}

// TestEvaluateCondition_ResultMapCopy 验证 result 不会污染原 data
func TestEvaluateCondition_ResultMapCopy(t *testing.T) {
	service := setupConditionEvaluatorService(t)
	data := map[string]any{"x": float64(1), "y": "hello"}
	node := contentmodel.FlowNode{
		ID:     "copy_test",
		Type:   "condition",
		Config: map[string]any{"condition": "x eq 1"},
	}
	result, _ := service.EvaluateCondition(node, data)

	if _, hasInjected := result["_condition_matched"]; !hasInjected {
		t.Error("result should have _condition_matched")
	}
	if _, polluted := data["_condition_matched"]; polluted {
		t.Error("original data should NOT be polluted with _condition_matched")
	}
}

// TestParseCondition_Unit 单测 parseCondition 函数
func TestParseCondition_Unit(t *testing.T) {
	cases := []struct {
		name      string
		input     string
		wantField string
		wantOp    string
		wantValue string
		wantErr   bool
	}{
		{"eq", "name eq John", "name", "eq", "John", false},
		{"ne", "name ne John", "name", "ne", "John", false},
		{"gt", "age gt 18", "age", "gt", "18", false},
		{"lt", "age lt 18", "age", "lt", "18", false},
		{"gte", "score gte 90", "score", "gte", "90", false},
		{"lte", "score lte 100", "score", "lte", "100", false},
		{"contains", "msg contains hello", "msg", "contains", "hello", false},
		{"in", "tag in [a,b,c]", "tag", "in", "[a,b,c]", false},
		{"with_extra_spaces", " name   eq   John ", "name", "eq", "John", false},
		{"empty", "", "", "", "", true},
		{"no_operator", "justtext", "", "", "", true},
		{"unknown_op", "x foo y", "", "", "", true},
		{"trailing_op_no_value", "x eq", "x", "eq", "", true},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			field, op, val, err := contentsvc.ParseCondition(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("err=%v wantErr=%v", err, tt.wantErr)
				return
			}
			if err != nil {
				return
			}
			if field != tt.wantField {
				t.Errorf("field=%q want=%q", field, tt.wantField)
			}
			if op != tt.wantOp {
				t.Errorf("op=%q want=%q", op, tt.wantOp)
			}
			if val != tt.wantValue {
				t.Errorf("val=%q want=%q", val, tt.wantValue)
			}
		})
	}
}
