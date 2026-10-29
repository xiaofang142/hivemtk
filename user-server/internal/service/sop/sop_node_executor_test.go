package sop

import (
	"context"
	"testing"

	"hivemtk-user/internal/dto"
	"hivemtk-user/internal/model"
)

func TestNodeExecutorRegistry_RegisterAndGet(t *testing.T) {
	r := NewNodeExecutorRegistry()
	exec := &StartExecutor{}
	r.Register(context.Background(), exec)

	got, err := r.Get(context.Background(), SOPNodeTypeStart)
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if got.NodeType() != SOPNodeTypeStart {
		t.Errorf("NodeType=%s want=%s", got.NodeType(), SOPNodeTypeStart)
	}
}

func TestNodeExecutorRegistry_GetNotFound(t *testing.T) {
	r := NewNodeExecutorRegistry()
	_, err := r.Get(context.Background(), "non_existent_type")
	if err == nil {
		t.Error("expected error for non-existent type")
	}
}

func TestNodeExecutorRegistry_MustGetFallbackToNoop(t *testing.T) {
	r := NewNodeExecutorRegistry()
	got := r.MustGet(context.Background(), "non_existent_type")
	if got == nil {
		t.Fatal("MustGet returned nil")
	}
	if got.NodeType() != "non_existent_type" {
		t.Errorf("NoopExecutor NodeType=%s want=non_existent_type", got.NodeType())
	}
}

func TestNodeExecutorRegistry_MustGetRegistered(t *testing.T) {
	r := NewNodeExecutorRegistry()
	r.Register(context.Background(), &StartExecutor{})
	r.Register(context.Background(), &EndExecutor{})

	got := r.MustGet(context.Background(), SOPNodeTypeStart)
	if _, ok := got.(*StartExecutor); !ok {
		t.Errorf("expected *StartExecutor, got %T", got)
	}
	got2 := r.MustGet(context.Background(), SOPNodeTypeEnd)
	if _, ok := got2.(*EndExecutor); !ok {
		t.Errorf("expected *EndExecutor, got %T", got2)
	}
}

func TestNodeExecutorRegistry_DuplicateRegisterPanics(t *testing.T) {
	r := NewNodeExecutorRegistry()
	r.Register(context.Background(), &StartExecutor{})

	defer func() {
		if rec := recover(); rec == nil {
			t.Error("expected panic on duplicate registration")
		}
	}()
	r.Register(context.Background(), &StartExecutor{})
}

func TestNodeExecutorRegistry_AllRegistered(t *testing.T) {
	r := NewNodeExecutorRegistry()
	r.Register(context.Background(), &StartExecutor{})
	r.Register(context.Background(), &EndExecutor{})

	all := r.AllRegistered(context.Background())
	if len(all) != 2 {
		t.Errorf("expected 2 registered, got %d", len(all))
	}
	hasStart, hasEnd := false, false
	for _, typ := range all {
		if typ == SOPNodeTypeStart {
			hasStart = true
		}
		if typ == SOPNodeTypeEnd {
			hasEnd = true
		}
	}
	if !hasStart || !hasEnd {
		t.Errorf("expected start and end in registered list, got %v", all)
	}
}

func TestNoopExecutor_NodeType(t *testing.T) {
	n := &NoopExecutor{nodeType: "custom_type"}
	if n.NodeType() != "custom_type" {
		t.Errorf("NodeType=%s want=custom_type", n.NodeType())
	}
}

func TestNoopExecutor_IsAsync(t *testing.T) {
	n := &NoopExecutor{nodeType: "test"}
	if n.IsAsync() {
		t.Error("NoopExecutor should be sync (IsAsync=false)")
	}
}

func TestNoopExecutor_ExecuteReturnsCompleted(t *testing.T) {
	n := &NoopExecutor{nodeType: "test"}
	ec := &ExecutionContext{
		Execution: &model.SOPExecution{ID: 1, SOPID: 1},
		Node:      &dto.SOPNode{ID: "n1", Type: "test"},
	}
	result, err := n.Execute(context.Background(), ec)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if result.Status != NodeStatusCompleted {
		t.Errorf("Status=%s want=%s", result.Status, NodeStatusCompleted)
	}
	if result.Output == nil {
		t.Error("Output should not be nil")
	}
}

func TestHasSideEffect_EmptyExecution(t *testing.T) {
	if hasSideEffect(nil, "msg:1:n1") {
		t.Error("nil execution should return false")
	}
	exec := &model.SOPExecution{}
	if hasSideEffect(exec, "msg:1:n1") {
		t.Error("empty ExecutionData should return false")
	}
}

func TestHasSideEffect_WithEffects(t *testing.T) {
	exec := &model.SOPExecution{
		ExecutionData: model.JSONMap{
			"_side_effects": []any{"message_sent:1:n1", "ws_push:1:n1"},
		},
	}
	if !hasSideEffect(exec, "message_sent:1:n1") {
		t.Error("expected true for existing side effect")
	}
	if hasSideEffect(exec, "message_sent:1:n2") {
		t.Error("expected false for non-existing side effect")
	}
}

func TestExtractSideEffects_NilData(t *testing.T) {
	if effects := extractSideEffects(nil); effects != nil {
		t.Errorf("expected nil, got %v", effects)
	}
	if effects := extractSideEffects(model.JSONMap{}); effects != nil {
		t.Errorf("expected nil for empty map, got %v", effects)
	}
}

func TestExtractSideEffects_ValidData(t *testing.T) {
	data := model.JSONMap{
		"_side_effects": []any{"effect1", "effect2"},
	}
	effects := extractSideEffects(data)
	if len(effects) != 2 {
		t.Fatalf("expected 2 effects, got %d", len(effects))
	}
	if effects[0] != "effect1" || effects[1] != "effect2" {
		t.Errorf("effects=%v want=[effect1 effect2]", effects)
	}
}

func TestExtractSideEffects_InvalidType(t *testing.T) {
	data := model.JSONMap{
		"_side_effects": "not_an_array",
	}
	effects := extractSideEffects(data)
	if effects != nil {
		t.Errorf("expected nil for invalid type, got %v", effects)
	}
}

func TestAppendSideEffect_NewEffect(t *testing.T) {
	data := model.JSONMap{}
	data = appendSideEffect(data, "effect1")
	effects := extractSideEffects(data)
	if len(effects) != 1 || effects[0] != "effect1" {
		t.Errorf("after append effect1, effects=%v", effects)
	}
}

func TestAppendSideEffect_DuplicateDeduplicated(t *testing.T) {
	data := model.JSONMap{}
	data = appendSideEffect(data, "effect1")
	data = appendSideEffect(data, "effect1")
	data = appendSideEffect(data, "effect2")
	effects := extractSideEffects(data)
	if len(effects) != 2 {
		t.Errorf("expected 2 effects after dedup, got %d: %v", len(effects), effects)
	}
}

func TestAppendSideEffect_NilData(t *testing.T) {
	data := appendSideEffect(nil, "effect1")
	if data == nil {
		t.Fatal("expected non-nil data")
	}
	effects := extractSideEffects(data)
	if len(effects) != 1 || effects[0] != "effect1" {
		t.Errorf("expected [effect1], got %v", effects)
	}
}

func TestExecutionContext_FieldsAccessible(t *testing.T) {
	ec := &ExecutionContext{
		Execution:     &model.SOPExecution{ID: 42, SOPID: 1},
		Node:          &dto.SOPNode{ID: "n1", Type: SOPNodeTypeGreeting},
		Graph:         &dto.SOPGraph{Nodes: []dto.SOPNode{{ID: "n1"}}},
		CustomerID:    "cust_001",
		SessionID:     "sess_001",
		Variant:       "A",
		Input:         model.JSONMap{"key": "value"},
		ExecutionData: model.JSONMap{"intent_score": float64(0.85)},
		TraceID:       "trace-001",
		Attempt:       1,
	}
	if ec.Execution.ID != 42 {
		t.Errorf("Execution.ID=%d want=42", ec.Execution.ID)
	}
	if ec.Node.ID != "n1" {
		t.Errorf("Node.ID=%s want=n1", ec.Node.ID)
	}
	if ec.CustomerID != "cust_001" {
		t.Errorf("CustomerID=%s want=cust_001", ec.CustomerID)
	}
	if ec.Attempt != 1 {
		t.Errorf("Attempt=%d want=1", ec.Attempt)
	}
}

func TestNodeExecResult_FieldsAccessible(t *testing.T) {
	r := &NodeExecResult{
		Status:       NodeStatusCompleted,
		Output:       model.JSONMap{"k": "v"},
		NextNodeID:   "next_node",
		WaitEvent:    WaitEventTimer,
		ErrorMessage: "",
		Retryable:    true,
		SideEffects:  []string{"effect1"},
		TokensUsed:   100,
	}
	if r.Status != NodeStatusCompleted {
		t.Errorf("Status=%s", r.Status)
	}
	if r.NextNodeID != "next_node" {
		t.Errorf("NextNodeID=%s", r.NextNodeID)
	}
	if len(r.SideEffects) != 1 {
		t.Errorf("SideEffects len=%d", len(r.SideEffects))
	}
	if r.TokensUsed != 100 {
		t.Errorf("TokensUsed=%d", r.TokensUsed)
	}
}

func TestNodeStatusConstants(t *testing.T) {
	cases := []struct{ name, value string }{
		{"NodeStatusCompleted", NodeStatusCompleted},
		{"NodeStatusWaiting", NodeStatusWaiting},
		{"NodeStatusFailed", NodeStatusFailed},
		{"NodeStatusSkipped", NodeStatusSkipped},
	}
	expected := map[string]string{
		"NodeStatusCompleted": "completed",
		"NodeStatusWaiting":   "waiting",
		"NodeStatusFailed":    "failed",
		"NodeStatusSkipped":   "skipped",
	}
	for _, c := range cases {
		if c.value != expected[c.name] {
			t.Errorf("%s=%s want=%s", c.name, c.value, expected[c.name])
		}
	}
}

func TestNodeEventConstants(t *testing.T) {
	if NodeEventStarted != "started" {
		t.Errorf("NodeEventStarted=%s want=started", NodeEventStarted)
	}
	if NodeEventExecuted != "executed" {
		t.Errorf("NodeEventExecuted=%s want=executed", NodeEventExecuted)
	}
	if NodeEventCompleted != "completed" {
		t.Errorf("NodeEventCompleted=%s want=completed", NodeEventCompleted)
	}
	if NodeEventFailed != "failed" {
		t.Errorf("NodeEventFailed=%s want=failed", NodeEventFailed)
	}
	if NodeEventWaiting != "waiting" {
		t.Errorf("NodeEventWaiting=%s want=waiting", NodeEventWaiting)
	}
	if NodeEventRetried != "retried" {
		t.Errorf("NodeEventRetried=%s want=retried", NodeEventRetried)
	}
}

func TestWaitEventConstants(t *testing.T) {
	if WaitEventTimer != "timer" {
		t.Errorf("WaitEventTimer=%s want=timer", WaitEventTimer)
	}
	if WaitEventCustomerReply != "customer_reply" {
		t.Errorf("WaitEventCustomerReply=%s want=customer_reply", WaitEventCustomerReply)
	}
	if WaitEventExternal != "external" {
		t.Errorf("WaitEventExternal=%s want=external", WaitEventExternal)
	}
}
