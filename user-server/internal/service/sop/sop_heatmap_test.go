package sop

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"hivemtk-user/internal/model"
)

type fakeSopAgentGetter struct {
	agent *model.SOPAgent
}

func (f *fakeSopAgentGetter) GetByID(ctx context.Context, id uint) (*model.SOPAgent, error) {
	if f.agent != nil && f.agent.ID == id {
		return f.agent, nil
	}
	return nil, nil
}

type fakeSopExecLister struct {
	execs []model.SOPExecution
}

func (f *fakeSopExecLister) ListBySOPID(ctx context.Context, sopID uint, limit int) ([]model.SOPExecution, error) {
	return f.execs, nil
}

func makeExecutedNodes(records []CompensationRecord) model.JSONArray {
	b, _ := json.Marshal(records)
	var out model.JSONArray
	_ = json.Unmarshal(b, &out)
	return out
}

func TestSopHeatmap_BasicAggregation(t *testing.T) {
	agent := &model.SOPAgent{
		ID:   1,
		Name: "销售SOP",
		SOPGraph: model.JSONMap{
			"nodes": []any{
				map[string]any{"id": "start", "type": "start"},
				map[string]any{"id": "n_qualify", "type": "llm"},
				map[string]any{"id": "n_pitch", "type": "llm"},
				map[string]any{"id": "n_close", "type": "llm"},
				map[string]any{"id": "end", "type": "end"},
			},
		},
	}
	execs := []model.SOPExecution{
		{
			ID: 1, SOPID: 1, Variant: "A", Status: "completed",
			ExecutedNodes: makeExecutedNodes([]CompensationRecord{
				{NodeID: "start", NodeType: "start", Status: "completed", StartedAt: time.Now().Add(-10 * time.Second), FinishedAt: time.Now().Add(-9 * time.Second)},
				{NodeID: "n_qualify", NodeType: "llm", Status: "completed", StartedAt: time.Now().Add(-9 * time.Second), FinishedAt: time.Now().Add(-7 * time.Second)},
				{NodeID: "n_pitch", NodeType: "llm", Status: "completed", StartedAt: time.Now().Add(-7 * time.Second), FinishedAt: time.Now().Add(-4 * time.Second)},
				{NodeID: "n_close", NodeType: "llm", Status: "completed", StartedAt: time.Now().Add(-4 * time.Second), FinishedAt: time.Now().Add(-2 * time.Second)},
				{NodeID: "end", NodeType: "end", Status: "completed", StartedAt: time.Now().Add(-2 * time.Second), FinishedAt: time.Now()},
			}),
		},
		{
			ID: 2, SOPID: 1, Variant: "A", Status: "completed",
			ExecutedNodes: makeExecutedNodes([]CompensationRecord{
				{NodeID: "start", NodeType: "start", Status: "completed", StartedAt: time.Now().Add(-10 * time.Second), FinishedAt: time.Now().Add(-9 * time.Second)},
				{NodeID: "n_qualify", NodeType: "llm", Status: "completed", StartedAt: time.Now().Add(-9 * time.Second), FinishedAt: time.Now().Add(-7 * time.Second)},
				{NodeID: "n_pitch", NodeType: "llm", Status: "failed", StartedAt: time.Now().Add(-7 * time.Second), FinishedAt: time.Now().Add(-5 * time.Second), Error: "llm timeout"},
			}),
		},
		{
			ID: 3, SOPID: 1, Variant: "B", Status: "running",
			ExecutedNodes: makeExecutedNodes([]CompensationRecord{
				{NodeID: "start", NodeType: "start", Status: "completed"},
				{NodeID: "n_qualify", NodeType: "llm", Status: "completed"},
			}),
		},
	}

	svc := NewSopHeatmapService(&fakeSopAgentGetter{agent: agent}, &fakeSopExecLister{execs: execs})
	rpt, err := svc.GenerateHeatmapForSOP(context.Background(), 1, "A", 100)
	if err != nil {
		t.Fatalf("GenerateHeatmapForSOP: %v", err)
	}
	if rpt.SOPID != 1 || rpt.SOPName != "销售SOP" {
		t.Errorf("report meta: %+v", rpt)
	}
	if rpt.TotalExec != 2 {
		t.Errorf("total exec = %d, want 2 (variant A only)", rpt.TotalExec)
	}
	if len(rpt.Nodes) != 5 {
		t.Errorf("nodes = %d, want 5", len(rpt.Nodes))
	}

	found := false
	for _, n := range rpt.Nodes {
		if n.NodeID == "start" {
			found = true
			if n.Entered != 2 || n.Completed != 2 || n.DropRate != 0 {
				t.Errorf("start: entered=%d completed=%d drop=%.2f", n.Entered, n.Completed, n.DropRate)
			}
			if n.AvgDurationMs <= 0 {
				t.Error("start 应有 avg_duration > 0")
			}
		}
		if n.NodeID == "n_pitch" {
			if n.Entered != 2 || n.Completed != 1 {
				t.Errorf("n_pitch: entered=%d completed=%d (one failed)", n.Entered, n.Completed)
			}
			if n.DropRate < 0.49 || n.DropRate > 0.51 {
				t.Errorf("n_pitch drop_rate=%.2f want ~0.5", n.DropRate)
			}
		}
		if n.NodeID == "n_close" {
			if n.Entered != 1 || n.Completed != 1 {
				t.Errorf("n_close: entered=%d completed=%d", n.Entered, n.Completed)
			}
		}
	}
	if !found {
		t.Error("start node missing in report")
	}
}

func TestSopHeatmap_VariantFilter(t *testing.T) {
	agent := &model.SOPAgent{
		ID:   2,
		Name: "测试SOP",
		SOPGraph: model.JSONMap{
			"nodes": []any{
				map[string]any{"id": "s", "type": "start"},
				map[string]any{"id": "a", "type": "llm"},
			},
		},
	}
	execs := []model.SOPExecution{
		{ID: 1, SOPID: 2, Variant: "A", Status: "completed", ExecutedNodes: makeExecutedNodes([]CompensationRecord{{NodeID: "s", Status: "completed"}, {NodeID: "a", Status: "completed"}})},
		{ID: 2, SOPID: 2, Variant: "B", Status: "completed", ExecutedNodes: makeExecutedNodes([]CompensationRecord{{NodeID: "s", Status: "completed"}})},
	}
	svc := NewSopHeatmapService(&fakeSopAgentGetter{agent: agent}, &fakeSopExecLister{execs: execs})

	rptA, _ := svc.GenerateHeatmapForSOP(context.Background(), 2, "A", 100)
	if rptA.TotalExec != 1 {
		t.Errorf("variant A: total_exec=%d want 1", rptA.TotalExec)
	}
	if rptA.Variant != "A" {
		t.Errorf("variant field: %s", rptA.Variant)
	}

	rptB, _ := svc.GenerateHeatmapForSOP(context.Background(), 2, "B", 100)
	if rptB.TotalExec != 1 {
		t.Errorf("variant B: total_exec=%d want 1", rptB.TotalExec)
	}
}

func TestSopHeatmap_EmptyGraphReturnsError(t *testing.T) {
	agent := &model.SOPAgent{ID: 3, Name: "空图SOP", SOPGraph: model.JSONMap{}}
	svc := NewSopHeatmapService(&fakeSopAgentGetter{agent: agent}, &fakeSopExecLister{execs: nil})
	_, err := svc.GenerateHeatmapForSOP(context.Background(), 3, "", 100)
	if err == nil {
		t.Error("空图应报错")
	}
}

func TestSopHeatmap_DynamicNodeFromExecution(t *testing.T) {

	agent := &model.SOPAgent{
		ID:       4,
		Name:     "动态节点SOP",
		SOPGraph: model.JSONMap{"nodes": []any{map[string]any{"id": "main", "type": "llm"}}},
	}
	execs := []model.SOPExecution{
		{
			ID: 1, SOPID: 4, Variant: "A", Status: "completed",
			ExecutedNodes: makeExecutedNodes([]CompensationRecord{
				{NodeID: "main", NodeType: "llm", Status: "completed"},
				{NodeID: "extra", NodeType: "tool", Status: "completed"},
			}),
		},
	}
	svc := NewSopHeatmapService(&fakeSopAgentGetter{agent: agent}, &fakeSopExecLister{execs: execs})
	rpt, _ := svc.GenerateHeatmapForSOP(context.Background(), 4, "", 100)
	foundExtra := false
	for _, n := range rpt.Nodes {
		if n.NodeID == "extra" {
			foundExtra = true
			if n.NodeType != "tool" || n.Entered != 1 {
				t.Errorf("extra node: %+v", n)
			}
		}
	}
	if !foundExtra {
		t.Error("动态出现的节点应被纳入报告")
	}
}
