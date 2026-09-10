package service

import (
	"context"
	"testing"
	"time"

	"hivemtk-user/internal/dto"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/testutil"
	"hivemtk-user/internal/repository"
)

func newD03Graph(nodeID, nodeType string) *dto.SOPGraph {
	return &dto.SOPGraph{
		Nodes: []dto.SOPNode{{ID: nodeID, Type: nodeType}},
	}
}

// LLM Compensate：清 ExecutionData._llm_* 键且落库；重复补偿幂等
func TestD03_LLMCompensateClearsKeys(t *testing.T) {
	db := testutil.NewTestDB(t, &model.SOPExecution{})
	exec := &model.SOPExecution{
		SOPID:      1,
		CustomerID: "c1",
		Status:     "failed",
		ExecutionData: model.JSONMap{
			"_llm_decision": "next_a",
			"_llm_reason":   "because",
		},
		TraceID: "tr-d03",
	}
	if err := db.Create(exec).Error; err != nil {
		t.Fatal(err)
	}

	e := &LLMNodeExecutor{execRepo: repository.NewSopExecutionRepository(db)}
	execCtx := &ExecutionContext{
		Execution: exec,
		Node:      &dto.SOPNode{ID: "node1", Type: SOPNodeTypeLLM},
		Graph:     newD03Graph("node1", SOPNodeTypeLLM),
		TraceID:   exec.TraceID,
	}
	if err := e.Compensate(context.Background(), execCtx); err != nil {
		t.Fatalf("Compensate: %v", err)
	}

	if err := e.Compensate(context.Background(), execCtx); err != nil {
		t.Fatalf("Compensate idempotent: %v", err)
	}

	var got model.SOPExecution
	if err := db.First(&got, exec.ID).Error; err != nil {
		t.Fatal(err)
	}
	if _, exists := got.ExecutionData["_llm_decision"]; exists {
		t.Errorf("_llm_decision 应被清除, got %v", got.ExecutionData)
	}
}

// Wait Compensate：删 pending timer；已 fired 的不动；幂等
func TestD03_WaitCompensateDeletesPendingTimers(t *testing.T) {
	db := testutil.NewTestDB(t, &model.SOPExecution{}, &model.SOPTimer{})
	exec := &model.SOPExecution{SOPID: 1, CustomerID: "c1", Status: "failed"}
	db.Create(exec)

	pending := &model.SOPTimer{
		ExecutionID: exec.ID,
		NodeID:      "wait1",
		WaitEvent:   "timer",
		WaitUntil:   time.Now().Add(time.Hour),
		Status:      "pending",
	}
	fired := &model.SOPTimer{
		ExecutionID: exec.ID,
		NodeID:      "wait2",
		WaitEvent:   "timer",
		WaitUntil:   time.Now().Add(-time.Hour),
		Status:      "fired",
	}
	db.Create(pending)
	db.Create(fired)

	e := &WaitExecutor{timerRepo: repository.NewSOPTimerRepository(db)}
	execCtx := &ExecutionContext{
		Execution: exec,
		Node:      &dto.SOPNode{ID: "wait1", Type: SOPNodeTypeWait},
		Graph:     newD03Graph("wait1", SOPNodeTypeWait),
	}
	if err := e.Compensate(context.Background(), execCtx); err != nil {
		t.Fatalf("Compensate: %v", err)
	}

	if err := e.Compensate(context.Background(), execCtx); err != nil {
		t.Fatalf("Compensate idempotent: %v", err)
	}

	var cnt int64
	db.Model(&model.SOPTimer{}).Where("id = ? AND status = 'pending'", pending.ID).Count(&cnt)
	if cnt != 0 {
		t.Error("pending timer 应被删除")
	}
	var firedCnt int64
	db.Model(&model.SOPTimer{}).Where("id = ?", fired.ID).Count(&cnt)
	_ = firedCnt
	var stillThere int64
	db.Model(&model.SOPTimer{}).Where("id = ?", fired.ID).Count(&stillThere)
	if stillThere != 1 {
		t.Error("fired timer 不应被删除")
	}
}

// 直构降级：deps nil 不 panic，Compensate 直接成功
func TestD03_DegradedNilDeps(t *testing.T) {
	llm := &LLMNodeExecutor{}
	wait := &WaitExecutor{}
	execCtx := &ExecutionContext{
		Execution: &model.SOPExecution{ID: 999},
		Node:      &dto.SOPNode{ID: "n1"},
	}
	if err := llm.Compensate(context.Background(), execCtx); err != nil {
		t.Errorf("LLM nil db 应 nil-成功, got %v", err)
	}
	if err := wait.Compensate(context.Background(), execCtx); err != nil {
		t.Errorf("Wait nil repo 应 nil-成功, got %v", err)
	}
}
