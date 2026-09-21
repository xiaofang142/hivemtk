package service

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"hivemtk-user/internal/aiagent/llm"
	"hivemtk-user/internal/dto"
	"hivemtk-user/internal/model"
)

func TestStartExecutor_NodeType(t *testing.T) {
	e := &StartExecutor{}
	if e.NodeType() != SOPNodeTypeStart {
		t.Errorf("NodeType=%s want=%s", e.NodeType(), SOPNodeTypeStart)
	}
}

func TestStartExecutor_IsAsync(t *testing.T) {
	e := &StartExecutor{}
	if e.IsAsync() {
		t.Error("StartExecutor should be sync")
	}
}

func TestStartExecutor_ExecuteReturnsCompleted(t *testing.T) {
	e := &StartExecutor{}
	startedAt := time.Now()
	ec := &ExecutionContext{
		Execution:     &model.SOPExecution{ID: 1, SOPID: 10, ExecutionData: model.JSONMap{"_trigger": "manual"}},
		Node:          &dto.SOPNode{ID: "start", Type: SOPNodeTypeStart},
		StartedAt:     startedAt,
		ExecutionData: model.JSONMap{"_trigger": "manual"},
	}
	result, err := e.Execute(context.Background(), ec)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if result.Status != NodeStatusCompleted {
		t.Errorf("Status=%s want=%s", result.Status, NodeStatusCompleted)
	}
	if _, ok := result.Output["_started_at"]; !ok {
		t.Error("Output should contain _started_at")
	}
	if result.Output["_trigger"] != "manual" {
		t.Errorf("Output _trigger=%v want=manual", result.Output["_trigger"])
	}
	if result.NextNodeID != "" {
		t.Errorf("StartExecutor NextNodeID=%s want empty (use default)", result.NextNodeID)
	}
}

func TestEndExecutor_NodeType(t *testing.T) {
	e := &EndExecutor{}
	if e.NodeType() != SOPNodeTypeEnd {
		t.Errorf("NodeType=%s want=%s", e.NodeType(), SOPNodeTypeEnd)
	}
}

func TestEndExecutor_IsAsync(t *testing.T) {
	e := &EndExecutor{}
	if e.IsAsync() {
		t.Error("EndExecutor should be sync")
	}
}

func TestEndExecutor_ExecuteTerminatesFlow(t *testing.T) {
	e := &EndExecutor{}
	ec := &ExecutionContext{
		Execution: &model.SOPExecution{ID: 1, SOPID: 10},
		Node:      &dto.SOPNode{ID: "end", Type: SOPNodeTypeEnd},
	}
	result, err := e.Execute(context.Background(), ec)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if result.Status != NodeStatusCompleted {
		t.Errorf("Status=%s want=%s", result.Status, NodeStatusCompleted)
	}
	if result.NextNodeID != "" {
		t.Errorf("EndExecutor NextNodeID=%s want empty (terminate)", result.NextNodeID)
	}
	if _, ok := result.Output["_ended_at"]; !ok {
		t.Error("Output should contain _ended_at")
	}
}

func TestMessageNodeBase_NodeType(t *testing.T) {
	b := NewMessageNodeExecutor(SOPNodeTypeGreeting, llm.ScenarioFriendlyChat, &SOPNodeExecutorDeps{})
	if b.NodeType() != SOPNodeTypeGreeting {
		t.Errorf("NodeType=%s want=%s", b.NodeType(), SOPNodeTypeGreeting)
	}
}

func TestMessageNodeBase_IsAsync(t *testing.T) {
	b := NewMessageNodeExecutor(SOPNodeTypeGreeting, llm.ScenarioFriendlyChat, &SOPNodeExecutorDeps{})
	if b.IsAsync() {
		t.Error("MessageNodeBase should be sync")
	}
}

func TestMessageNodeBase_IdempotentSkip(t *testing.T) {
	b := NewMessageNodeExecutor(SOPNodeTypeGreeting, llm.ScenarioFriendlyChat, &SOPNodeExecutorDeps{})
	exec := &model.SOPExecution{
		ID:            1,
		ExecutionData: model.JSONMap{"_side_effects": []any{"message_sent:1:greeting_node"}},
	}
	ec := &ExecutionContext{
		Execution: exec,
		Node:      &dto.SOPNode{ID: "greeting_node", Type: SOPNodeTypeGreeting, Prompt: "你好"},
	}
	result, err := b.Execute(context.Background(), ec)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if result.Status != NodeStatusSkipped {
		t.Errorf("Status=%s want=%s (idempotent skip)", result.Status, NodeStatusSkipped)
	}
}

func TestMessageNodeBase_PromptSource(t *testing.T) {
	b := NewMessageNodeExecutor(SOPNodeTypeGreeting, llm.ScenarioFriendlyChat, &SOPNodeExecutorDeps{})
	ec := &ExecutionContext{
		Execution:     &model.SOPExecution{ID: 1},
		Node:          &dto.SOPNode{ID: "n1", Type: SOPNodeTypeGreeting, Prompt: "您好，欢迎咨询"},
		ExecutionData: model.JSONMap{},
	}
	result, err := b.Execute(context.Background(), ec)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if result.Status != NodeStatusCompleted {
		t.Errorf("Status=%s want=%s", result.Status, NodeStatusCompleted)
	}
	content, _ := result.Output["_greeting_content"].(string)
	if content != "您好，欢迎咨询" {
		t.Errorf("content=%s want=您好，欢迎咨询", content)
	}
	source, _ := result.Output["_greeting_source"].(string)
	if source != "prompt" {
		t.Errorf("source=%s want=prompt", source)
	}
	if len(result.SideEffects) != 1 {
		t.Errorf("SideEffects len=%d want=1", len(result.SideEffects))
	} else if result.SideEffects[0] != "message_sent:1:n1" {
		t.Errorf("SideEffects[0]=%s want=message_sent:1:n1", result.SideEffects[0])
	}
}

func TestMessageNodeBase_PromptTemplateRendering(t *testing.T) {
	b := NewMessageNodeExecutor(SOPNodeTypeGreeting, llm.ScenarioFriendlyChat, &SOPNodeExecutorDeps{})
	ec := &ExecutionContext{
		Execution:     &model.SOPExecution{ID: 1},
		Node:          &dto.SOPNode{ID: "n1", Type: SOPNodeTypeGreeting, Prompt: "您好 {{customer_name}}，欢迎咨询"},
		ExecutionData: model.JSONMap{"customer_name": "张先生"},
	}
	result, err := b.Execute(context.Background(), ec)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if result.Status != NodeStatusCompleted {
		t.Errorf("Status=%s want=%s", result.Status, NodeStatusCompleted)
	}
	content, _ := result.Output["_greeting_content"].(string)
	if content != "您好 张先生，欢迎咨询" {
		t.Errorf("content=%s want=您好 张先生，欢迎咨询", content)
	}
}

func TestMessageNodeBase_ConfigContentSource(t *testing.T) {
	b := NewMessageNodeExecutor(SOPNodeTypeInquire, llm.ScenarioSOPReply, &SOPNodeExecutorDeps{})
	ec := &ExecutionContext{
		Execution:     &model.SOPExecution{ID: 1},
		Node:          &dto.SOPNode{ID: "n1", Type: SOPNodeTypeInquire, Config: map[string]any{"content": "请问您主要想了解什么？"}},
		ExecutionData: model.JSONMap{},
	}
	result, err := b.Execute(context.Background(), ec)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if result.Status != NodeStatusCompleted {
		t.Errorf("Status=%s want=%s", result.Status, NodeStatusCompleted)
	}
	content, _ := result.Output["_inquire_content"].(string)
	if content != "请问您主要想了解什么？" {
		t.Errorf("content=%s want=请问您主要想了解什么？", content)
	}
	source, _ := result.Output["_inquire_source"].(string)
	if source != "config" {
		t.Errorf("source=%s want=config", source)
	}
}

func TestMessageNodeBase_FallbackDefaultScript(t *testing.T) {
	b := NewMessageNodeExecutor(SOPNodeTypeClose, llm.ScenarioHighQuality, &SOPNodeExecutorDeps{})
	ec := &ExecutionContext{
		Execution:     &model.SOPExecution{ID: 1},
		Node:          &dto.SOPNode{ID: "n1", Type: SOPNodeTypeClose},
		ExecutionData: model.JSONMap{},
	}
	result, err := b.Execute(context.Background(), ec)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if result.Status != NodeStatusCompleted {
		t.Errorf("Status=%s want=%s", result.Status, NodeStatusCompleted)
	}
	content, _ := result.Output["_close_content"].(string)
	if !strings.Contains(content, "专属优惠") {
		t.Errorf("content=%s should contain 专属优惠 (close default script)", content)
	}
	source, _ := result.Output["_close_source"].(string)
	if source != "fallback" {
		t.Errorf("source=%s want=fallback", source)
	}
}

func TestMessageNodeBase_AllNodeTypesHaveDefaultScript(t *testing.T) {
	types := []string{
		SOPNodeTypeGreeting, SOPNodeTypeInquire, SOPNodeTypeIntroduce,
		SOPNodeTypeHandle, SOPNodeTypeClose, SOPNodeTypeInvite,
		SOPNodeTypeFollowUp, SOPNodeTypeActivate, SOPNodeTypeNurture,
		SOPNodeTypeMessage, SOPNodeTypeAction, SOPNodeTypeSendOffer,
	}
	for _, typ := range types {
		script := defaultScriptForNodeType(typ)
		if strings.TrimSpace(script) == "" {
			t.Errorf("node type %s has empty default script", typ)
		}
	}
}

func TestRenderPromptTemplate_VarSubstitution(t *testing.T) {
	tmpl := "你好 {{name}}，您的分数是 {{score}}"
	data := model.JSONMap{
		"name":  "张三",
		"score": float64(0.85),
	}
	out := renderPromptTemplate(tmpl, data)
	if !strings.Contains(out, "张三") {
		t.Errorf("output should contain 张三: %s", out)
	}
	if !strings.Contains(out, "0.85") {
		t.Errorf("output should contain 0.85: %s", out)
	}
}

func TestRenderPromptTemplate_UnmatchedVarPreserved(t *testing.T) {
	tmpl := "你好 {{name}}，未知变量 {{unknown}}"
	data := model.JSONMap{"name": "李四"}
	out := renderPromptTemplate(tmpl, data)
	if !strings.Contains(out, "李四") {
		t.Errorf("output should contain 李四: %s", out)
	}
	if !strings.Contains(out, "{{unknown}}") {
		t.Errorf("unmatched var should be preserved: %s", out)
	}
}

func TestRenderPromptTemplate_NilData(t *testing.T) {
	out := renderPromptTemplate("hello {{x}}", nil)
	if out != "hello {{x}}" {
		t.Errorf("nil data should preserve template: %s", out)
	}
}

func TestConditionExecutor_NodeType(t *testing.T) {
	e := &ConditionExecutor{nodeType: SOPNodeTypeCondition}
	if e.NodeType() != SOPNodeTypeCondition {
		t.Errorf("NodeType=%s want=%s", e.NodeType(), SOPNodeTypeCondition)
	}
	e2 := &ConditionExecutor{nodeType: SOPNodeTypeBranch}
	if e2.NodeType() != SOPNodeTypeBranch {
		t.Errorf("NodeType=%s want=%s (branch legacy)", e2.NodeType(), SOPNodeTypeBranch)
	}
}

func TestConditionExecutor_IsAsync(t *testing.T) {
	e := &ConditionExecutor{nodeType: SOPNodeTypeCondition}
	if e.IsAsync() {
		t.Error("ConditionExecutor should be sync")
	}
}

func TestConditionExecutor_PriorityRouting(t *testing.T) {
	e := &ConditionExecutor{nodeType: SOPNodeTypeCondition}
	ec := &ExecutionContext{
		Execution: &model.SOPExecution{ID: 1},
		Node: &dto.SOPNode{
			ID:   "cond",
			Type: SOPNodeTypeCondition,
			Conditions: []dto.SOPConditionBranch{
				{Label: "高意向", Condition: "intent_score gte 0.7", Next: "close_node", Priority: 100},
				{Label: "中意向", Condition: "intent_score gte 0.4", Next: "nurture_node", Priority: 50},
				{Label: "低意向", Condition: "intent_score lt 0.4", Next: "activate_node", Priority: 10},
			},
		},
		ExecutionData: model.JSONMap{"intent_score": float64(0.85)},
	}
	result, err := e.Execute(context.Background(), ec)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if result.Status != NodeStatusCompleted {
		t.Errorf("Status=%s want=%s", result.Status, NodeStatusCompleted)
	}
	if result.NextNodeID != "close_node" {
		t.Errorf("NextNodeID=%s want=close_node (high intent)", result.NextNodeID)
	}
	if result.Output["_condition_branch"] != "高意向" {
		t.Errorf("branch label=%v want=高意向", result.Output["_condition_branch"])
	}
}

func TestConditionExecutor_MidIntentRouting(t *testing.T) {
	e := &ConditionExecutor{nodeType: SOPNodeTypeCondition}
	ec := &ExecutionContext{
		Execution: &model.SOPExecution{ID: 1},
		Node: &dto.SOPNode{
			ID:   "cond",
			Type: SOPNodeTypeCondition,
			Conditions: []dto.SOPConditionBranch{
				{Label: "高意向", Condition: "intent_score gte 0.7", Next: "close_node", Priority: 100},
				{Label: "中意向", Condition: "intent_score gte 0.4", Next: "nurture_node", Priority: 50},
				{Label: "低意向", Condition: "intent_score lt 0.4", Next: "activate_node", Priority: 10},
			},
		},
		ExecutionData: model.JSONMap{"intent_score": float64(0.55)},
	}
	result, _ := e.Execute(context.Background(), ec)
	if result.NextNodeID != "nurture_node" {
		t.Errorf("NextNodeID=%s want=nurture_node (mid intent)", result.NextNodeID)
	}
}

func TestConditionExecutor_LowIntentRouting(t *testing.T) {
	e := &ConditionExecutor{nodeType: SOPNodeTypeCondition}
	ec := &ExecutionContext{
		Execution: &model.SOPExecution{ID: 1},
		Node: &dto.SOPNode{
			ID:   "cond",
			Type: SOPNodeTypeCondition,
			Conditions: []dto.SOPConditionBranch{
				{Label: "高意向", Condition: "intent_score gte 0.7", Next: "close_node", Priority: 100},
				{Label: "中意向", Condition: "intent_score gte 0.4", Next: "nurture_node", Priority: 50},
				{Label: "低意向", Condition: "intent_score lt 0.4", Next: "activate_node", Priority: 10},
			},
		},
		ExecutionData: model.JSONMap{"intent_score": float64(0.2)},
	}
	result, _ := e.Execute(context.Background(), ec)
	if result.NextNodeID != "activate_node" {
		t.Errorf("NextNodeID=%s want=activate_node (low intent)", result.NextNodeID)
	}
}

func TestConditionExecutor_FallbackToNextZero(t *testing.T) {
	e := &ConditionExecutor{nodeType: SOPNodeTypeCondition}
	ec := &ExecutionContext{
		Execution: &model.SOPExecution{ID: 1},
		Node: &dto.SOPNode{
			ID:   "cond",
			Type: SOPNodeTypeCondition,
			Next: []string{"default_node"},
			Conditions: []dto.SOPConditionBranch{
				{Label: "高意向", Condition: "intent_score gte 0.7", Next: "close_node", Priority: 100},
			},
		},
		ExecutionData: model.JSONMap{"intent_score": float64(0.1)},
	}
	result, _ := e.Execute(context.Background(), ec)
	if result.NextNodeID != "default_node" {
		t.Errorf("NextNodeID=%s want=default_node (fallback)", result.NextNodeID)
	}
}

func TestConditionExecutor_LegacyConditionField(t *testing.T) {
	e := &ConditionExecutor{nodeType: SOPNodeTypeBranch}
	ec := &ExecutionContext{
		Execution: &model.SOPExecution{ID: 1},
		Node: &dto.SOPNode{
			ID:        "b1",
			Type:      SOPNodeTypeBranch,
			Condition: "status eq active",
			Next:      []string{"active_branch", "inactive_branch"},
		},
		ExecutionData: model.JSONMap{"status": "active"},
	}
	result, _ := e.Execute(context.Background(), ec)
	if result.NextNodeID != "active_branch" {
		t.Errorf("NextNodeID=%s want=active_branch (legacy match)", result.NextNodeID)
	}
}

func TestLLMNodeExecutor_NodeType(t *testing.T) {
	e := NewLLMNodeExecutor(SOPNodeTypeLLM, &SOPNodeExecutorDeps{})
	if e.NodeType() != SOPNodeTypeLLM {
		t.Errorf("NodeType=%s want=%s", e.NodeType(), SOPNodeTypeLLM)
	}
	e2 := NewLLMNodeExecutor(SOPNodeTypeAIDecide, &SOPNodeExecutorDeps{})
	if e2.NodeType() != SOPNodeTypeAIDecide {
		t.Errorf("NodeType=%s want=%s (ai_decide legacy)", e2.NodeType(), SOPNodeTypeAIDecide)
	}
}

func TestLLMNodeExecutor_IsAsync(t *testing.T) {
	e := NewLLMNodeExecutor(SOPNodeTypeLLM, &SOPNodeExecutorDeps{})
	if e.IsAsync() {
		t.Error("LLMNodeExecutor should be sync")
	}
}

func TestLLMNodeExecutor_DispatcherNilFallsBackToNext(t *testing.T) {
	e := NewLLMNodeExecutor(SOPNodeTypeLLM, &SOPNodeExecutorDeps{LLMSem: make(chan struct{}, 4)})
	ec := &ExecutionContext{
		Execution: &model.SOPExecution{ID: 1},
		Node: &dto.SOPNode{
			ID:   "llm1",
			Type: SOPNodeTypeLLM,
			Next: []string{"default_next"},
		},
		ExecutionData: model.JSONMap{},
	}
	result, err := e.Execute(context.Background(), ec)
	if err == nil {
		t.Error("expected error when dispatcher is nil")
	}
	if result == nil {
		t.Fatal("result should not be nil even on error")
	}
	if result.Status != NodeStatusFailed {
		t.Errorf("Status=%s want=%s", result.Status, NodeStatusFailed)
	}
	if result.Retryable {
		t.Error("dispatcher=nil should not be retryable (configuration error)")
	}
}

func TestLLMNodeExecutor_NoCandidates(t *testing.T) {
	e := NewLLMNodeExecutor(SOPNodeTypeLLM, &SOPNodeExecutorDeps{LLMSem: make(chan struct{}, 4)})
	ec := &ExecutionContext{
		Execution: &model.SOPExecution{ID: 1},
		Node: &dto.SOPNode{
			ID:   "llm1",
			Type: SOPNodeTypeLLM,
			Next: []string{},
		},
		ExecutionData: model.JSONMap{},
	}
	result, _ := e.Execute(context.Background(), ec)
	if result.NextNodeID != "" {
		t.Errorf("NextNodeID=%s want empty (no candidates)", result.NextNodeID)
	}
}

func TestLLMNodeExecutor_SemaphoreLimitsConcurrency(t *testing.T) {
	sem := make(chan struct{}, 4)
	e := NewLLMNodeExecutor(SOPNodeTypeLLM, &SOPNodeExecutorDeps{LLMSem: sem})
	ec := &ExecutionContext{
		Execution: &model.SOPExecution{ID: 1},
		Node: &dto.SOPNode{
			ID:   "llm1",
			Type: SOPNodeTypeLLM,
			Next: []string{"default_next"},
		},
		ExecutionData: model.JSONMap{},
	}
	if len(sem) != 0 {
		t.Fatalf("pre-execute semaphore len=%d want=0", len(sem))
	}
	result, _ := e.Execute(context.Background(), ec)
	if result == nil {
		t.Fatal("result should not be nil")
	}
	if len(sem) != 0 {
		t.Errorf("semaphore not released after Execute, len=%d", len(sem))
	}
}

func TestLLMNodeExecutor_SemaphoreBlocksWhenFull(t *testing.T) {
	sem := make(chan struct{}, 1)
	sem <- struct{}{}
	e := NewLLMNodeExecutor(SOPNodeTypeLLM, &SOPNodeExecutorDeps{LLMSem: sem})
	ec := &ExecutionContext{
		Execution: &model.SOPExecution{ID: 1},
		Node: &dto.SOPNode{
			ID:   "llm1",
			Type: SOPNodeTypeLLM,
			Next: []string{"default_next"},
		},
		ExecutionData: model.JSONMap{},
	}
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	result, err := e.Execute(ctx, ec)
	if err == nil {
		t.Error("expected error when ctx cancelled during semaphore wait")
	}
	if result == nil {
		t.Fatal("result should not be nil")
	}
	if result.Status != NodeStatusFailed {
		t.Errorf("Status=%s want=%s", result.Status, NodeStatusFailed)
	}
	if !result.Retryable {
		t.Error("ctx cancellation should be retryable")
	}
	<-sem
}

func TestParseLLMDecision_ValidJSON(t *testing.T) {
	content := `{"next_node_id":"close_node","reason":"客户意向高"}`
	d, err := parseLLMDecision(content)
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}
	if d.NextNodeID != "close_node" {
		t.Errorf("NextNodeID=%s want=close_node", d.NextNodeID)
	}
	if d.Reason != "客户意向高" {
		t.Errorf("Reason=%s want=客户意向高", d.Reason)
	}
}

func TestParseLLMDecision_JSONInText(t *testing.T) {
	content := `根据分析，我建议 {"next_node_id":"nurture_node","reason":"继续培育"} 希望对你有帮助`
	d, err := parseLLMDecision(content)
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}
	if d.NextNodeID != "nurture_node" {
		t.Errorf("NextNodeID=%s want=nurture_node", d.NextNodeID)
	}
}

func TestParseLLMDecision_InvalidJSON(t *testing.T) {
	content := `这不是 JSON`
	_, err := parseLLMDecision(content)
	if err == nil {
		t.Error("expected error for invalid JSON")
	}
}

func TestParseLLMDecision_EmptyContent(t *testing.T) {
	_, err := parseLLMDecision("")
	if err == nil {
		t.Error("expected error for empty content")
	}
}

func TestParseLLMDecision_EmptyNextNodeID(t *testing.T) {
	content := `{"next_node_id":"","reason":"无决策"}`
	d, err := parseLLMDecision(content)
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}
	if d.NextNodeID != "" {
		t.Errorf("NextNodeID=%s want empty", d.NextNodeID)
	}
}

func TestBuildLLMDecisionPrompt_ContainsCandidates(t *testing.T) {
	ec := &ExecutionContext{
		Execution: &model.SOPExecution{ID: 1},
		Node: &dto.SOPNode{
			ID:     "llm1",
			Type:   SOPNodeTypeLLM,
			Name:   "决策节点",
			Prompt: "请根据意向判断",
			Next:   []string{"close", "nurture", "activate"},
		},
		ExecutionData: model.JSONMap{"intent_score": float64(0.85)},
	}
	prompt := buildLLMDecisionPrompt(ec, ec.Node.Next)
	if !strings.Contains(prompt, "close") || !strings.Contains(prompt, "nurture") || !strings.Contains(prompt, "activate") {
		t.Errorf("prompt should contain all candidates: %s", prompt)
	}
	if !strings.Contains(prompt, "0.85") {
		t.Errorf("prompt should contain intent_score: %s", prompt)
	}
	if !strings.Contains(prompt, "next_node_id") {
		t.Errorf("prompt should contain JSON format hint: %s", prompt)
	}
}

func TestWaitExecutor_NodeType(t *testing.T) {
	e := &WaitExecutor{}
	if e.NodeType() != SOPNodeTypeWait {
		t.Errorf("NodeType=%s want=%s", e.NodeType(), SOPNodeTypeWait)
	}
}

func TestWaitExecutor_IsAsync(t *testing.T) {
	e := &WaitExecutor{}
	if !e.IsAsync() {
		t.Error("WaitExecutor should be async")
	}
}

func TestWaitExecutor_WaitSeconds(t *testing.T) {
	e := &WaitExecutor{}
	ec := &ExecutionContext{
		Execution: &model.SOPExecution{ID: 1},
		Node: &dto.SOPNode{
			ID:     "wait1",
			Type:   SOPNodeTypeWait,
			Config: map[string]any{"wait_seconds": float64(5)},
		},
		ExecutionData: model.JSONMap{},
	}
	result, err := e.Execute(context.Background(), ec)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if result.Status != NodeStatusWaiting {
		t.Errorf("Status=%s want=%s", result.Status, NodeStatusWaiting)
	}
	if result.WaitEvent != WaitEventTimer {
		t.Errorf("WaitEvent=%s want=%s", result.WaitEvent, WaitEventTimer)
	}
	if result.WaitUntil == nil {
		t.Error("WaitUntil should not be nil")
	}
	expected := time.Now().Add(5 * time.Second)
	delta := result.WaitUntil.Sub(expected)
	if delta > 500*time.Millisecond || delta < -500*time.Millisecond {
		t.Errorf("WaitUntil=%v want ~%v (delta=%v)", result.WaitUntil, expected, delta)
	}
}

func TestWaitExecutor_WaitUntilAbsoluteTime(t *testing.T) {
	target := time.Now().Add(1 * time.Hour).Format(time.RFC3339)
	e := &WaitExecutor{}
	ec := &ExecutionContext{
		Execution: &model.SOPExecution{ID: 1},
		Node: &dto.SOPNode{
			ID:     "wait1",
			Type:   SOPNodeTypeWait,
			Config: map[string]any{"wait_until": target},
		},
		ExecutionData: model.JSONMap{},
	}
	result, _ := e.Execute(context.Background(), ec)
	if result.Status != NodeStatusWaiting {
		t.Errorf("Status=%s want=%s", result.Status, NodeStatusWaiting)
	}
	if result.WaitUntil == nil {
		t.Fatal("WaitUntil should not be nil")
	}
	expected, _ := time.Parse(time.RFC3339, target)
	delta := result.WaitUntil.Sub(expected)
	if delta > 1*time.Second || delta < -1*time.Second {
		t.Errorf("WaitUntil=%v want ~%v (delta=%v)", result.WaitUntil, expected, delta)
	}
}

func TestWaitExecutor_CustomerReplyDefault(t *testing.T) {
	e := &WaitExecutor{}
	ec := &ExecutionContext{
		Execution: &model.SOPExecution{ID: 1},
		Node: &dto.SOPNode{
			ID:     "wait1",
			Type:   SOPNodeTypeWait,
			Config: map[string]any{},
		},
		ExecutionData: model.JSONMap{},
	}
	result, _ := e.Execute(context.Background(), ec)
	if result.Status != NodeStatusWaiting {
		t.Errorf("Status=%s want=%s", result.Status, NodeStatusWaiting)
	}
	if result.WaitEvent != WaitEventCustomerReply {
		t.Errorf("WaitEvent=%s want=%s", result.WaitEvent, WaitEventCustomerReply)
	}
	if result.WaitUntil == nil {
		t.Fatal("WaitUntil should not be nil")
	}
	expected := time.Now().Add(24 * time.Hour)
	delta := result.WaitUntil.Sub(expected)
	if delta > 1*time.Second || delta < -1*time.Second {
		t.Errorf("WaitUntil=%v want ~%v (delta=%v)", result.WaitUntil, expected, delta)
	}
}

func TestWaitExecutor_CustomWaitEvent(t *testing.T) {
	e := &WaitExecutor{}
	ec := &ExecutionContext{
		Execution: &model.SOPExecution{ID: 1},
		Node: &dto.SOPNode{
			ID:     "wait1",
			Type:   SOPNodeTypeWait,
			Config: map[string]any{"wait_event": WaitEventExternal, "wait_seconds": float64(10)},
		},
		ExecutionData: model.JSONMap{},
	}
	result, _ := e.Execute(context.Background(), ec)
	if result.WaitEvent != WaitEventExternal {
		t.Errorf("WaitEvent=%s want=%s", result.WaitEvent, WaitEventExternal)
	}
}

func TestRegisterAllNodeExecutors_AllTypesRegistered(t *testing.T) {
	r := NewNodeExecutorRegistry()
	deps := &SOPNodeExecutorDeps{
		LLMSem: make(chan struct{}, 4),
	}
	RegisterAllNodeExecutors(r, deps)

	expectedTypes := []string{
		SOPNodeTypeStart, SOPNodeTypeEnd,
		SOPNodeTypeGreeting, SOPNodeTypeInquire, SOPNodeTypeIntroduce,
		SOPNodeTypeHandle, SOPNodeTypeClose, SOPNodeTypeInvite,
		SOPNodeTypeFollowUp, SOPNodeTypeActivate, SOPNodeTypeNurture,
		SOPNodeTypeCondition, SOPNodeTypeLLM, SOPNodeTypeWait,
		SOPNodeTypeMessage, SOPNodeTypeAction, SOPNodeTypeSendOffer,
		SOPNodeTypeAIDecide, SOPNodeTypeBranch,
		// T-P5-03：主动外发节点。计数是**上界**断言（下面那句），
		// 少这一项就是「注册了但没进契约表」。
		SOPNodeTypeReachSend,
	}
	registered := map[string]bool{}
	for _, t := range r.AllRegistered(context.Background()) {
		registered[t] = true
	}
	for _, typ := range expectedTypes {
		if !registered[typ] {
			t.Errorf("node type %s not registered", typ)
		}
	}
	if len(r.AllRegistered(context.Background())) != len(expectedTypes) {
		t.Errorf("registered count=%d want=%d", len(r.AllRegistered(context.Background())), len(expectedTypes))
	}
}

func TestRegisterAllNodeExecutors_PanicsOnDuplicate(t *testing.T) {
	r := NewNodeExecutorRegistry()
	deps := &SOPNodeExecutorDeps{}
	RegisterAllNodeExecutors(r, deps)
	defer func() {
		if rec := recover(); rec == nil {
			t.Error("expected panic on duplicate RegisterAllNodeExecutors")
		}
	}()
	RegisterAllNodeExecutors(r, deps)
}

func TestFirstOrEmpty(t *testing.T) {
	if firstOrEmpty(nil) != "" {
		t.Error("nil slice should return empty")
	}
	if firstOrEmpty([]string{}) != "" {
		t.Error("empty slice should return empty")
	}
	if firstOrEmpty([]string{"a", "b"}) != "a" {
		t.Error("first element should be 'a'")
	}
}

func TestContainsString(t *testing.T) {
	if !containsString([]string{"a", "b", "c"}, "b") {
		t.Error("should contain b")
	}
	if containsString([]string{"a", "b", "c"}, "d") {
		t.Error("should not contain d")
	}
	if containsString([]string{"a", "b", "c"}, "") {
		t.Error("should not contain empty string (not in slice)")
	}
	if !containsString([]string{""}, "") {
		t.Error("should contain empty string (in slice)")
	}
}

func TestLLMDecision_JSONRoundTrip(t *testing.T) {
	original := llmDecision{NextNodeID: "close", Reason: "高意向"}
	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}
	var restored llmDecision
	if err := json.Unmarshal(data, &restored); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	if original.NextNodeID != restored.NextNodeID || original.Reason != restored.Reason {
		t.Errorf("round-trip mismatch: original=%+v restored=%+v", original, restored)
	}
}

func TestLLMDecision_FieldNamesMatchJSONContract(t *testing.T) {
	original := llmDecision{NextNodeID: "x", Reason: "y"}
	data, _ := json.Marshal(original)
	var raw map[string]any
	_ = json.Unmarshal(data, &raw)
	if _, ok := raw["next_node_id"]; !ok {
		t.Error("JSON should contain next_node_id field")
	}
	if _, ok := raw["reason"]; !ok {
		t.Error("JSON should contain reason field")
	}
}

var _ llm.DispatchScenario = llm.ScenarioHighQuality
