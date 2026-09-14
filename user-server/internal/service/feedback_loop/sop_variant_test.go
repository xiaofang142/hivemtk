package feedbackloop

import (
	"encoding/json"
	"testing"

	"hivemtk-user/internal/model"
)

func sampleGraph() model.JSONMap {
	graphJSON := `{
		"nodes": [
			{"id": "start", "type": "start", "name": "开始", "next": ["n1"]},
			{"id": "n1", "type": "action", "name": "识别来源", "next": ["n2"]},
			{"id": "n2", "type": "message", "name": "需求分析", "next": ["n2b"]},
			{"id": "n2b", "type": "message", "name": "补充询问", "next": ["n3"]},
			{"id": "n3", "type": "llm", "name": "产品推荐", "prompt": "推荐产品", "next": ["n4"]},
			{"id": "n4", "type": "wait", "name": "等待", "config": {"wait_seconds": 3600}, "next": ["n5"]},
			{"id": "n5", "type": "close", "name": "促成下单"}
		]
	}`
	var g model.JSONMap
	_ = json.Unmarshal([]byte(graphJSON), &g)
	return g
}

func nodesOf(t *testing.T, g model.JSONMap) []map[string]any {
	t.Helper()
	raw, ok := g["nodes"].([]any)
	if !ok {
		t.Fatalf("nodes missing")
	}
	out := make([]map[string]any, 0, len(raw))
	for _, item := range raw {
		m, ok := item.(map[string]any)
		if ok {
			out = append(out, m)
		}
	}
	return out
}

func TestMutateSOPGraph_BranchPrune(t *testing.T) {
	g := SOPGraphMutatorForExperiment("branch_prune")(sampleGraph())
	if g == nil {
		t.Fatal("expected mutated graph, got nil")
	}
	nodes := nodesOf(t, g)
	if len(nodes) != 6 {
		t.Fatalf("expected 6 nodes after prune, got %d", len(nodes))
	}
	for _, n := range nodes {
		if id, _ := n["id"].(string); id == "n1" {
			t.Fatalf("pruned node n1 still present")
		}
	}

	for _, n := range nodes {
		if id, _ := n["id"].(string); id == "start" {
			next, _ := n["next"].([]any)
			if len(next) == 0 || next[0] != "n2" {
				t.Fatalf("start.next not rewired to n2: %v", next)
			}
		}
	}
}

func TestMutateSOPGraph_AddObjection(t *testing.T) {
	g := SOPGraphMutatorForExperiment("add_objection")(sampleGraph())
	if g == nil {
		t.Fatal("expected mutated graph, got nil")
	}
	nodes := nodesOf(t, g)
	if len(nodes) != 8 {
		t.Fatalf("expected 8 nodes, got %d", len(nodes))
	}
	found := false
	for _, n := range nodes {
		if id, _ := n["id"].(string); id == "obj_auto_1" {
			found = true
		}
	}
	if !found {
		t.Fatal("objection node not inserted")
	}
}

func TestMutateSOPGraph_TimingAdjust(t *testing.T) {
	g := SOPGraphMutatorForExperiment("timing_adjust")(sampleGraph())
	if g == nil {
		t.Fatal("expected mutated graph, got nil")
	}
	for _, n := range nodesOf(t, g) {
		if id, _ := n["id"].(string); id == "n4" {
			cfg, _ := n["config"].(map[string]any)
			sec, _ := cfg["wait_seconds"].(float64)
			if sec != 1800 {
				t.Fatalf("expected wait_seconds=1800, got %v", sec)
			}
		}
	}
}

func TestMutateSOPGraph_AddEmpathy(t *testing.T) {
	g := SOPGraphMutatorForExperiment("add_empathy")(sampleGraph())
	if g == nil {
		t.Fatal("expected mutated graph, got nil")
	}
	for _, n := range nodesOf(t, g) {
		if id, _ := n["id"].(string); id == "n2" {
			p, _ := n["prompt"].(string)
			if p == "" || !contains(p, "共情") {
				t.Fatalf("empathy not appended to n2 prompt: %q", p)
			}
		}
	}
}

func TestMutateSOPGraph_NodeMerge(t *testing.T) {
	g := SOPGraphMutatorForExperiment("node_merge")(sampleGraph())
	if g == nil {
		t.Fatal("expected mutated graph, got nil")
	}
	nodes := nodesOf(t, g)
	if len(nodes) != 6 {
		t.Fatalf("expected 6 nodes after merge, got %d", len(nodes))
	}
	for _, n := range nodes {
		if id, _ := n["id"].(string); id == "n2b" {
			t.Fatal("merged node n2b should be removed")
		}
	}

	for _, n := range nodes {
		if id, _ := n["id"].(string); id == "n2" {
			next, _ := n["next"].([]any)
			if len(next) == 0 || next[0] != "n3" {
				t.Fatalf("n2.next not inherited to n3: %v", next)
			}
		}
	}
}

func TestMutateSOPGraph_UnknownTag(t *testing.T) {
	if g := SOPGraphMutatorForExperiment("bogus")(sampleGraph()); g != nil {
		t.Fatal("unknown tag should return nil")
	}
}

func TestMutateSOPGraph_NilGraph(t *testing.T) {
	if g := SOPGraphMutatorForExperiment("branch_prune")(nil); g != nil {
		t.Fatal("nil graph should return nil")
	}
}

func TestMutateNodePrompt_ExactAndFallback(t *testing.T) {
	g := SOPGraphMutatorForNodePrompt("n3", "新prompt")(sampleGraph())
	if g == nil {
		t.Fatal("expected mutated graph")
	}
	for _, n := range nodesOf(t, g) {
		if id, _ := n["id"].(string); id == "n3" {
			if p, _ := n["prompt"].(string); p != "新prompt" {
				t.Fatalf("n3 prompt not replaced: %q", p)
			}
		}
	}

	g2 := SOPGraphMutatorForNodePrompt("no_such", "兜底prompt")(sampleGraph())
	if g2 == nil {
		t.Fatal("expected fallback mutation")
	}
	for _, n := range nodesOf(t, g2) {
		if id, _ := n["id"].(string); id == "n2" {
			if p, _ := n["prompt"].(string); p != "兜底prompt" {
				t.Fatalf("fallback prompt not applied: %q", p)
			}
		}
	}
}

func TestExtractNodePrompt(t *testing.T) {
	if p, ok := ExtractNodePrompt(sampleGraph(), "n3"); !ok || p != "推荐产品" {
		t.Fatalf("exact match failed: ok=%v prompt=%q", ok, p)
	}
	if p, ok := ExtractNodePrompt(sampleGraph(), "no_such"); !ok || p == "" {
		t.Fatalf("fallback to first llm/message node failed: ok=%v prompt=%q", ok, p)
	}
	if _, ok := ExtractNodePrompt(nil, "n3"); ok {
		t.Fatal("nil graph should return ok=false")
	}
	if _, ok := ExtractNodePrompt(model.JSONMap{"foo": 1}, "n3"); ok {
		t.Fatal("graph without nodes should return ok=false")
	}
	empty := model.JSONMap{"nodes": []any{}}
	if _, ok := ExtractNodePrompt(empty, "n3"); ok {
		t.Fatal("empty nodes should return ok=false")
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(sub) == 0 || indexOf(s, sub) >= 0)
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
