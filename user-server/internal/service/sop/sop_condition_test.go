package sop

import (
	"context"
	"testing"

	"hivemtk-user/internal/model"
)

func TestSOPParseCondition_Wrap(t *testing.T) {
	cases := []struct {
		input     string
		wantField string
		wantOp    string
		wantValue string
		wantErr   bool
	}{
		{"intent_score gte 0.7", "intent_score", "gte", "0.7", false},
		{"status eq active", "status", "eq", "active", false},
		{"tag in [a,b,c]", "tag", "in", "[a,b,c]", false},
		{"", "", "", "", true},
		{"justtext", "", "", "", true},
	}
	for _, tt := range cases {
		field, op, val, err := SOPParseCondition(tt.input)
		if (err != nil) != tt.wantErr {
			t.Errorf("[%s] err=%v wantErr=%v", tt.input, err, tt.wantErr)
			continue
		}
		if err != nil {
			continue
		}
		if field != tt.wantField || op != tt.wantOp || val != tt.wantValue {
			t.Errorf("[%s] got=(%s,%s,%s) want=(%s,%s,%s)",
				tt.input, field, op, val, tt.wantField, tt.wantOp, tt.wantValue)
		}
	}
}

func TestSOPEvaluateSingleCondition(t *testing.T) {
	cases := []struct {
		name      string
		cond      string
		data      map[string]any
		wantMatch bool
		wantErr   bool
	}{
		{"empty_cond", "", map[string]any{"x": 1}, true, false},
		{"missing_field", "y eq 1", map[string]any{"x": 1}, false, false},
		{"eq_match", "x eq 1", map[string]any{"x": float64(1)}, true, false},
		{"gte_match", "score gte 0.7", map[string]any{"score": float64(0.85)}, true, false},
		{"contains_match", "msg contains 你好", map[string]any{"msg": "世界你好"}, true, false},
		{"in_match", "status in [active,pending]", map[string]any{"status": "active"}, true, false},
		{"eq_nomatch", "x eq 1", map[string]any{"x": float64(2)}, false, false},
		{"gt_nomatch", "x gt 10", map[string]any{"x": float64(5)}, false, false},
		{"invalid_op", "x foo 1", map[string]any{"x": float64(1)}, false, true},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			matched, err := SOPEvaluateSingleCondition(tt.cond, tt.data)
			if (err != nil) != tt.wantErr {
				t.Errorf("[%s] err=%v wantErr=%v", tt.name, err, tt.wantErr)
				return
			}
			if err != nil {
				return
			}
			if matched != tt.wantMatch {
				t.Errorf("[%s] matched=%v want=%v", tt.name, matched, tt.wantMatch)
			}
		})
	}
}

func TestSOPEvaluateCompoundCondition_AndOr(t *testing.T) {
	cases := []struct {
		name      string
		cond      string
		data      map[string]any
		wantMatch bool
		wantErr   bool
	}{
		{"and_both_true", "x eq 1 AND y eq 2", map[string]any{
			"x": float64(1), "y": float64(2),
		}, true, false},
		{"and_first_false", "x eq 1 AND y eq 2", map[string]any{
			"x": float64(99), "y": float64(2),
		}, false, false},
		{"and_second_false", "x eq 1 AND y eq 2", map[string]any{
			"x": float64(1), "y": float64(99),
		}, false, false},
		{"and_missing_field", "x eq 1 AND z eq 2", map[string]any{
			"x": float64(1),
		}, false, false},

		{"or_first_true", "x eq 1 OR y eq 2", map[string]any{
			"x": float64(1), "y": float64(99),
		}, true, false},
		{"or_second_true", "x eq 1 OR y eq 2", map[string]any{
			"x": float64(99), "y": float64(2),
		}, true, false},
		{"or_both_false", "x eq 1 OR y eq 2", map[string]any{
			"x": float64(99), "y": float64(99),
		}, false, false},

		{"mixed_and_or", "x eq 1 AND y eq 2 OR z eq 3", map[string]any{
			"x": float64(1), "y": float64(2), "z": float64(3),
		}, false, true},

		{"empty", "", map[string]any{}, true, false},

		{"case_insensitive_and", "x eq 1 and y eq 2", map[string]any{
			"x": float64(1), "y": float64(2),
		}, true, false},
		{"case_insensitive_or", "x eq 1 or y eq 2", map[string]any{
			"x": float64(99), "y": float64(2),
		}, true, false},

		{"three_and", "x eq 1 AND y eq 2 AND z eq 3", map[string]any{
			"x": float64(1), "y": float64(2), "z": float64(3),
		}, true, false},
		{"three_and_fail", "x eq 1 AND y eq 2 AND z eq 3", map[string]any{
			"x": float64(1), "y": float64(2), "z": float64(99),
		}, false, false},

		{"three_or", "x eq 1 OR y eq 2 OR z eq 3", map[string]any{
			"x": float64(99), "y": float64(99), "z": float64(3),
		}, true, false},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			matched, err := SOPEvaluateCompoundCondition(tt.cond, tt.data)
			if (err != nil) != tt.wantErr {
				t.Errorf("[%s] err=%v wantErr=%v", tt.name, err, tt.wantErr)
				return
			}
			if err != nil {
				return
			}
			if matched != tt.wantMatch {
				t.Errorf("[%s] matched=%v want=%v", tt.name, matched, tt.wantMatch)
			}
		})
	}
}

func TestSOPEvaluateConditionBranches_PriorityRouting(t *testing.T) {
	branches := []SOPConditionBranch{
		{Label: "高意向", Condition: "intent_score gte 0.7", Next: "close_node", Priority: 100},
		{Label: "中意向", Condition: "intent_score gte 0.4", Next: "nurture_node", Priority: 50},
		{Label: "低意向", Condition: "intent_score lt 0.4", Next: "activate_node", Priority: 10},
	}

	cases := []struct {
		name        string
		data        map[string]any
		wantMatched bool
		wantNext    string
	}{
		{"high_intent", map[string]any{"intent_score": float64(0.85)}, true, "close_node"},
		{"mid_intent", map[string]any{"intent_score": float64(0.55)}, true, "nurture_node"},
		{"low_intent", map[string]any{"intent_score": float64(0.2)}, true, "activate_node"},
		{"missing_field", map[string]any{}, true, "activate_node"},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			if tt.name == "missing_field" {
				br, err := SOPEvaluateConditionBranches(branches, tt.data)
				if err != nil {
					t.Fatalf("[%s] err=%v", tt.name, err)
				}
				if br.Matched {
					t.Errorf("[%s] expected Matched=false for missing field", tt.name)
				}
				return
			}

			br, err := SOPEvaluateConditionBranches(branches, tt.data)
			if err != nil {
				t.Fatalf("[%s] err=%v", tt.name, err)
			}
			if !br.Matched {
				t.Errorf("[%s] expected Matched=true", tt.name)
				return
			}
			if br.NextNode != tt.wantNext {
				t.Errorf("[%s] NextNode=%s want=%s", tt.name, br.NextNode, tt.wantNext)
			}
		})
	}
}

func TestSOPEvaluateConditionBranches_CatchAll(t *testing.T) {
	branches := []SOPConditionBranch{
		{Label: "高意向", Condition: "intent_score gte 0.7", Next: "close_node", Priority: 100},
		{Label: "兜底", Condition: "", Next: "activate_node", Priority: 0},
	}

	br, err := SOPEvaluateConditionBranches(branches, map[string]any{"intent_score": float64(0.85)})
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	if !br.Matched || br.NextNode != "close_node" {
		t.Errorf("expected close_node, got %+v", br)
	}

	br, err = SOPEvaluateConditionBranches(branches, map[string]any{"intent_score": float64(0.2)})
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	if !br.Matched || br.NextNode != "activate_node" {
		t.Errorf("expected activate_node, got %+v", br)
	}

	br, err = SOPEvaluateConditionBranches(branches, map[string]any{})
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	if !br.Matched || br.NextNode != "activate_node" {
		t.Errorf("expected activate_node for missing field, got %+v", br)
	}
}

func TestSOPEvaluateConditionBranches_Empty(t *testing.T) {
	br, err := SOPEvaluateConditionBranches(nil, map[string]any{})
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	if br.Matched {
		t.Errorf("expected Matched=false for empty branches")
	}
}

func TestSOPEvaluateConditionBranches_PriorityOrder(t *testing.T) {
	branches := []SOPConditionBranch{
		{Label: "低意向", Condition: "intent_score gte 0", Next: "low_node", Priority: 1},
		{Label: "高意向", Condition: "intent_score gte 0.7", Next: "high_node", Priority: 100},
	}

	br, err := SOPEvaluateConditionBranches(branches, map[string]any{"intent_score": float64(0.85)})
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	if !br.Matched || br.NextNode != "high_node" {
		t.Errorf("expected high_node (priority 100), got %+v", br)
	}
}

func TestSOPEvaluateNodeCondition_ConditionNode(t *testing.T) {
	node := &SOPNode{
		ID:   "c1",
		Type: SOPNodeTypeCondition,
		Name: "意向判断",
		Conditions: []SOPConditionBranch{
			{Label: "高", Condition: "score gte 0.7", Next: "high_node", Priority: 100},
			{Label: "低", Condition: "", Next: "low_node", Priority: 0},
		},
	}

	result, err := SOPEvaluateNodeCondition(node, model.JSONMap{"score": float64(0.85)})
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	if result["_condition_matched"] != true {
		t.Error("expected matched=true")
	}
	if result["_next_node"] != "high_node" {
		t.Errorf("expected high_node, got %v", result["_next_node"])
	}

	result, err = SOPEvaluateNodeCondition(node, model.JSONMap{"score": float64(0.3)})
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	if result["_condition_matched"] != true {
		t.Error("expected matched=true (catch-all)")
	}
	if result["_next_node"] != "low_node" {
		t.Errorf("expected low_node, got %v", result["_next_node"])
	}
}

func TestSOPEvaluateNodeCondition_LegacyConditionField(t *testing.T) {
	node := &SOPNode{
		ID:        "b1",
		Type:      SOPNodeTypeBranch,
		Name:      "状态判断",
		Condition: "status eq active",
		Next:      []string{"active_branch", "inactive_branch"},
	}

	result, err := SOPEvaluateNodeCondition(node, model.JSONMap{"status": "active"})
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	if result["_condition_matched"] != true {
		t.Error("expected matched=true")
	}
	if result["_next_node"] != "active_branch" {
		t.Errorf("expected active_branch, got %v", result["_next_node"])
	}

	result, err = SOPEvaluateNodeCondition(node, model.JSONMap{"status": "inactive"})
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	if result["_condition_matched"] != false {
		t.Error("expected matched=false")
	}
	if result["_next_node"] != "inactive_branch" {
		t.Errorf("expected inactive_branch, got %v", result["_next_node"])
	}
}

func TestSOPEvaluateNodeCondition_EmptyCondition(t *testing.T) {
	node := &SOPNode{
		ID:   "n1",
		Type: SOPNodeTypeMessage,
		Name: "消息节点",
		Next: []string{"next_node"},
	}
	result, err := SOPEvaluateNodeCondition(node, model.JSONMap{})
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	if result["_condition_matched"] != true {
		t.Error("expected matched=true for empty condition")
	}
	if result["_next_node"] != "next_node" {
		t.Errorf("expected next_node, got %v", result["_next_node"])
	}
}

func TestSOPEvaluateNodeCondition_NilNode(t *testing.T) {
	result, err := SOPEvaluateNodeCondition(nil, model.JSONMap{})
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	if result["_condition_matched"] != true {
		t.Error("expected matched=true for nil node")
	}
}

func TestNextNode_ConditionNode_PriorityRouting(t *testing.T) {
	graph := &SOPGraph{
		Nodes: []SOPNode{
			{ID: "start", Type: SOPNodeTypeStart, Name: "开始", Next: []string{"cond"}},
			{
				ID:   "cond",
				Type: SOPNodeTypeCondition,
				Name: "意向判断",
				Conditions: []SOPConditionBranch{
					{Label: "高", Condition: "score gte 0.7", Next: "high", Priority: 100},
					{Label: "低", Condition: "", Next: "low", Priority: 0},
				},
				Next: []string{"low"},
			},
			{ID: "high", Type: SOPNodeTypeClose, Name: "促单", Next: []string{"end"}},
			{ID: "low", Type: SOPNodeTypeNurture, Name: "培育", Next: []string{"end"}},
			{ID: "end", Type: SOPNodeTypeEnd, Name: "结束"},
		},
	}

	condNode := findNodeByID(graph, "cond")
	if condNode == nil {
		t.Fatal("cond node not found")
	}

	next := nextNode(graph, condNode, model.JSONMap{"score": float64(0.85)})
	if next == nil || next.ID != "high" {
		t.Errorf("expected high, got %v", next)
	}

	next = nextNode(graph, condNode, model.JSONMap{"score": float64(0.3)})
	if next == nil || next.ID != "low" {
		t.Errorf("expected low, got %v", next)
	}
}

func TestNextNode_LLMNode(t *testing.T) {
	graph := &SOPGraph{
		Nodes: []SOPNode{
			{ID: "start", Type: SOPNodeTypeStart, Name: "开始", Next: []string{"llm"}},
			{
				ID:     "llm",
				Type:   SOPNodeTypeLLM,
				Name:   "LLM 决策",
				Prompt: "请判断客户意向并返回 next_node_id",
				Next:   []string{"default_next"},
			},
			{ID: "high_intent", Type: SOPNodeTypeClose, Name: "促单"},
			{ID: "default_next", Type: SOPNodeTypeMessage, Name: "默认消息"},
		},
	}

	llmNode := findNodeByID(graph, "llm")
	if llmNode == nil {
		t.Fatal("llm node not found")
	}

	next := nextNode(graph, llmNode, model.JSONMap{"_llm_decision": "high_intent"})
	if next == nil || next.ID != "high_intent" {
		t.Errorf("expected high_intent, got %v", next)
	}

	next = nextNode(graph, llmNode, model.JSONMap{})
	if next == nil || next.ID != "default_next" {
		t.Errorf("expected default_next, got %v", next)
	}
}

func TestNextNode_EndNode(t *testing.T) {
	graph := &SOPGraph{
		Nodes: []SOPNode{
			{ID: "start", Type: SOPNodeTypeStart, Next: []string{"end"}},
			{ID: "end", Type: SOPNodeTypeEnd},
		},
	}
	endNode := findNodeByID(graph, "end")
	next := nextNode(graph, endNode, model.JSONMap{})
	if next != nil {
		t.Errorf("expected nil for end node, got %v", next)
	}
}

func TestNextNode_LegacyBranchNode(t *testing.T) {
	graph := &SOPGraph{
		Nodes: []SOPNode{
			{ID: "start", Type: SOPNodeTypeStart, Next: []string{"branch"}},
			{ID: "branch", Type: SOPNodeTypeBranch, Next: []string{"default"}},
			{ID: "yes", Type: SOPNodeTypeMessage},
			{ID: "no", Type: SOPNodeTypeMessage},
			{ID: "default", Type: SOPNodeTypeMessage},
		},
		Edges: []SOPEdge{
			{From: "branch", To: "yes", When: "true"},
			{From: "branch", To: "no", When: "false"},
		},
	}

	branchNode := findNodeByID(graph, "branch")

	next := nextNode(graph, branchNode, model.JSONMap{"_branch_result": "true"})
	if next == nil || next.ID != "yes" {
		t.Errorf("expected yes, got %v", next)
	}

	next = nextNode(graph, branchNode, model.JSONMap{"_branch_result": "false"})
	if next == nil || next.ID != "no" {
		t.Errorf("expected no, got %v", next)
	}

	next = nextNode(graph, branchNode, model.JSONMap{})
	if next == nil || next.ID != "default" {
		t.Errorf("expected default, got %v", next)
	}
}

func TestValidateGraph_NewNodeTypes(t *testing.T) {
	svc := &SOPService{}

	graph := &SOPGraph{
		Nodes: []SOPNode{
			{ID: "start", Type: SOPNodeTypeStart, Next: []string{"greeting"}},
			{ID: "greeting", Type: SOPNodeTypeGreeting, Next: []string{"inquire"}},
			{ID: "inquire", Type: SOPNodeTypeInquire, Next: []string{"introduce"}},
			{ID: "introduce", Type: SOPNodeTypeIntroduce, Next: []string{"cond"}},
			{
				ID:   "cond",
				Type: SOPNodeTypeCondition,
				Conditions: []SOPConditionBranch{
					{Label: "高", Condition: "score gte 0.7", Next: "close"},
					{Label: "低", Condition: "", Next: "nurture"},
				},
			},
			{ID: "close", Type: SOPNodeTypeClose, Next: []string{"end"}},
			{ID: "nurture", Type: SOPNodeTypeNurture, Next: []string{"follow_up"}},
			{ID: "follow_up", Type: SOPNodeTypeFollowUp, Next: []string{"end"}},
			{ID: "end", Type: SOPNodeTypeEnd},
		},
	}
	if err := svc.validateGraph(context.Background(), graph); err != nil {
		t.Errorf("expected valid graph, got err: %v", err)
	}

	badGraph := &SOPGraph{
		Nodes: []SOPNode{
			{ID: "start", Type: SOPNodeTypeStart, Next: []string{"unknown"}},
			{ID: "unknown", Type: "unsupported_type", Next: []string{"end"}},
			{ID: "end", Type: SOPNodeTypeEnd},
		},
	}
	if err := svc.validateGraph(context.Background(), badGraph); err == nil {
		t.Error("expected error for unsupported node type")
	}

	badCondGraph := &SOPGraph{
		Nodes: []SOPNode{
			{ID: "start", Type: SOPNodeTypeStart, Next: []string{"cond"}},
			{
				ID:   "cond",
				Type: SOPNodeTypeCondition,
				Conditions: []SOPConditionBranch{
					{Label: "高", Condition: "score gte 0.7", Next: "missing_node"},
				},
			},
			{ID: "end", Type: SOPNodeTypeEnd},
		},
	}
	if err := svc.validateGraph(context.Background(), badCondGraph); err == nil {
		t.Error("expected error for missing condition branch target")
	}
}
