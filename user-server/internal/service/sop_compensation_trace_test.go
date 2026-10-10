package service

import (
	"encoding/json"
	"testing"

	"hivemtk-user/internal/dto"
	"hivemtk-user/internal/model"
)

// A10：executed_nodes 轨迹追加（SAGA 补偿数据源）
func TestAppendExecutedNode(t *testing.T) {
	exec := &model.SOPExecution{}
	node := &dto.SOPNode{ID: "n1", Type: "greeting"}

	appendExecutedNode(exec, node, 0, "")
	appendExecutedNode(exec, &dto.SOPNode{ID: "n2", Type: "inquire"}, 1, "")

	if len(exec.ExecutedNodes) != 2 {
		t.Fatalf("应追加2条轨迹，实际 %d", len(exec.ExecutedNodes))
	}
	raw, err := json.Marshal(exec.ExecutedNodes)
	if err != nil {
		t.Fatal(err)
	}
	var entries []compensationTraceEntry
	if err := json.Unmarshal(raw, &entries); err != nil {
		t.Fatalf("轨迹应可反序列化为补偿记录形态: %v", err)
	}
	if entries[0].NodeID != "n1" || entries[0].NodeType != "greeting" || entries[0].Status != "completed" {
		t.Fatalf("首条轨迹内容不符: %+v", entries[0])
	}
}

func TestAppendExecutedNode_NilSafety(t *testing.T) {
	appendExecutedNode(nil, nil, 0, "")
	exec := &model.SOPExecution{}
	appendExecutedNode(exec, nil, 0, "")
	if len(exec.ExecutedNodes) != 0 {
		t.Fatalf("nil node 不应追加")
	}
}

func TestAppendExecutedNode_TruncatesOversizedTrace(t *testing.T) {
	exec := &model.SOPExecution{}
	node := &dto.SOPNode{ID: "n", Type: "llm"}
	for i := 0; i < DefaultMaxExecutedNodeTrace+10; i++ {
		appendExecutedNode(exec, node, 0, "")
	}
	if len(exec.ExecutedNodes) != DefaultMaxExecutedNodeTrace {
		t.Fatalf("轨迹应封顶 %d 防止无限增长，实际 %d", DefaultMaxExecutedNodeTrace, len(exec.ExecutedNodes))
	}
}

// 补偿计划反向序：最后执行的先补偿
func TestCompensationPlan_ReversedOrder(t *testing.T) {
	m := NewCompensationManager(DefaultCompensationConfig())
	in := []CompensationRecord{
		{NodeID: "n1"}, {NodeID: "n2"}, {NodeID: "n3"},
	}
	got := m.Plan(in)
	if got[0].NodeID != "n3" || got[2].NodeID != "n1" {
		t.Fatalf("计划应为反向序: %v", got)
	}
}
