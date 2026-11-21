package service

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"hivemtk-user/internal/content/model"
)

// 同层节点各自修改嵌套输入，不能相互观察，也不能修改调用方数据。
func TestMarketingFlowGraph_IsolatedSnapshots(t *testing.T) {
	graph, err := parseMarketingFlowGraph(`{"nodes":[{"id":"start","type":"trigger","next_nodes":["a","b"]},{"id":"a","type":"action"},{"id":"b","type":"action"}]}`)
	if err != nil {
		t.Fatal(err)
	}
	input := map[string]any{"nested": map[string]any{"owner": "original"}, "slice": []string{"original"}, "integer": uint64(9007199254740993)}
	var barrier sync.WaitGroup
	barrier.Add(2)
	result, err := graph.run(context.Background(), input, func(_ context.Context, node model.FlowNode, data map[string]any) (map[string]any, error) {
		if node.Type == "trigger" {
			return data, nil
		}
		nested := data["nested"].(map[string]any)
		items := data["slice"].([]string)
		nested["owner"], items[0] = node.ID, node.ID
		barrier.Done()
		barrier.Wait()
		if nested["owner"] != node.ID || items[0] != node.ID {
			return nil, fmt.Errorf("读到了其他节点的写入")
		}
		return map[string]any{node.ID: nested["owner"]}, nil
	}, nil)
	if err != nil {
		t.Fatalf("独立快照执行失败: %v", err)
	}
	if input["nested"].(map[string]any)["owner"] != "original" || input["slice"].([]string)[0] != "original" {
		t.Errorf("调用方输入被修改: %+v", input)
	}
	if result["a"] != "a" || result["b"] != "b" || result["integer"] != uint64(9007199254740993) {
		t.Errorf("并发结果或大整数有损: %+v", result)
	}
}

// 完成顺序不影响冲突裁定；同值合并后汇合节点只执行一次。
func TestMarketingFlowGraph_DeterministicMerge(t *testing.T) {
	for _, conflict := range []bool{false, true} {
		for _, first := range []string{"a", "b"} {
			t.Run(fmt.Sprintf("conflict=%v/first=%s", conflict, first), func(t *testing.T) {
				graph, err := parseMarketingFlowGraph(`{"nodes":[{"id":"start","type":"trigger","next_nodes":["a","b"]},{"id":"a","type":"action","next_nodes":["join"]},{"id":"b","type":"action","next_nodes":["join"]},{"id":"join","type":"action"}]}`)
				if err != nil {
					t.Fatal(err)
				}
				release := make(chan struct{})
				joinCalls := 0
				result, err := graph.run(context.Background(), nil, func(_ context.Context, node model.FlowNode, data map[string]any) (map[string]any, error) {
					if node.ID == "start" {
						return data, nil
					}
					if node.ID == "join" {
						joinCalls++
						return nil, nil
					}
					if node.ID == first {
						defer close(release)
					} else {
						<-release
					}
					value := "same"
					if conflict {
						value = node.ID
					}
					return map[string]any{"shared": value}, nil
				}, nil)
				if conflict {
					if err == nil || !strings.Contains(err.Error(), "同层节点结果冲突：shared") || joinCalls != 0 {
						t.Errorf("冲突必须确定性失败且不执行后继: result=%v err=%v joins=%d", result, err, joinCalls)
					}
				} else if err != nil || result["shared"] != "same" || joinCalls != 1 {
					t.Errorf("同值应合并且汇合一次: result=%v err=%v joins=%d", result, err, joinCalls)
				}
			})
		}
	}
}

// 嵌套条件、未选分支汇合与终止条件不得继承旧的路由控制键。
func TestMarketingFlowGraph_NestedBranchAndJoin(t *testing.T) {
	graph, err := parseMarketingFlowGraph(`{"nodes":[{"id":"start","type":"trigger","next_nodes":["first"]},{"id":"first","type":"condition","config":{"condition":"answer eq yes"},"next_nodes":["nested","unselected"]},{"id":"nested","type":"condition","next_nodes":["chosen"]},{"id":"chosen","type":"action","next_nodes":["join"]},{"id":"unselected","type":"action","next_nodes":["join"]},{"id":"join","type":"action","next_nodes":["terminal"]},{"id":"terminal","type":"condition"}]}`)
	if err != nil {
		t.Fatal(err)
	}
	var seen []string
	svc := &MarketingFlowService{}
	result, err := graph.run(context.Background(), map[string]any{"answer": "yes", "_next_node": "unselected"}, func(_ context.Context, node model.FlowNode, data map[string]any) (map[string]any, error) {
		seen = append(seen, node.ID)
		if node.Type == "condition" {
			return svc.evaluateCondition(node, data)
		}
		return data, nil
	}, nil)
	if err != nil {
		t.Fatalf("嵌套条件执行失败: %v", err)
	}
	want := []string{"start", "first", "nested", "chosen", "join", "terminal"}
	if !reflect.DeepEqual(seen, want) {
		t.Errorf("分支/汇合执行错误: want=%v got=%v", want, seen)
	}
	if _, ok := result["_next_node"]; ok {
		t.Errorf("控制键不应进入业务数据: %+v", result)
	}
}

// 非法图必须在发布和实际触发时拒绝，不能以全图执行作为环路的退路。
func TestMarketingFlowGraph_RejectsInvalidDefinitions(t *testing.T) {
	tests := map[string]string{
		"cycle":              `{"nodes":[{"id":"start","type":"trigger","next_nodes":["a"]},{"id":"a","type":"action","next_nodes":["start"]}]}`,
		"disconnected_cycle": `{"nodes":[{"id":"start","type":"trigger"},{"id":"a","type":"action","next_nodes":["a"]}]}`,
		"dangling":           `{"nodes":[{"id":"start","type":"trigger","next_nodes":["missing"]}]}`,
		"duplicate":          `{"nodes":[{"id":"start","type":"trigger"},{"id":"start","type":"action"}]}`,
		"blank_id":           `{"nodes":[{"id":"","type":"trigger"}]}`,
		"duplicate_edge":     `{"nodes":[{"id":"start","type":"trigger","next_nodes":["a","a"]},{"id":"a","type":"action"}]}`,
	}
	for name, raw := range tests {
		t.Run(name, func(t *testing.T) {
			svc := setupMarketingFlowService(t)
			if err := svc.validateFlowDefinition(raw); err == nil {
				t.Error("非法图必须被校验拒绝")
			}
			flow := &model.MarketingFlow{ID: 7788, Name: name, Status: model.FlowStatusActive, FlowData: raw}
			if execution, err := svc.TriggerFlow(context.Background(), flow, "invalid", "user", nil); err == nil || execution != nil {
				t.Fatalf("非法图不得创建执行或启动动作: execution=%+v err=%v", execution, err)
			}
			rows, total, err := svc.executionRepo.GetByFlowID(context.Background(), flow.ID, 1, 10)
			if err != nil || total != 0 || len(rows) != 0 {
				t.Fatalf("非法图应零执行记录: total=%d rows=%d err=%v", total, len(rows), err)
			}
		})
	}
}

// 非法更新不能替换已激活定义或推进版本，创建与激活同样拒绝非法图。
func TestMarketingFlowGraph_MutationsValidateBeforeWrite(t *testing.T) {
	svc := setupMarketingFlowService(t)
	ctx := context.Background()
	valid := &model.FlowDefinition{Nodes: []model.FlowNode{{ID: "start", Type: "trigger"}}}
	invalid := &model.FlowDefinition{Nodes: []model.FlowNode{{ID: "start", Type: "trigger", NextNodes: []string{"missing"}}}}
	if flow, err := svc.CreateFlow(ctx, 1, &CreateFlowRequest{Name: "bad", FlowData: invalid}); err == nil || flow != nil {
		t.Fatalf("非法图创建应失败: flow=%+v err=%v", flow, err)
	}
	flow, err := svc.CreateFlow(ctx, 1, &CreateFlowRequest{Name: "valid", FlowData: valid})
	if err != nil {
		t.Fatal(err)
	}
	original, version := flow.FlowData, flow.Version
	if updated, err := svc.UpdateFlow(ctx, flow.ID, 1, false, &UpdateFlowRequest{FlowData: invalid}); err == nil || updated != nil {
		t.Errorf("非法图更新应失败: updated=%+v err=%v", updated, err)
	}
	stored, err := svc.GetFlowByID(ctx, flow.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.FlowData != original || stored.Version != version {
		t.Fatalf("非法更新不得写入或改版本: stored=%+v", stored)
	}
	raw, err := json.Marshal(invalid)
	if err != nil {
		t.Fatal(err)
	}
	stored.FlowData = string(raw)
	if err := svc.flowRepo.Update(ctx, stored); err != nil {
		t.Fatal(err)
	}
	if err := svc.ActivateFlow(ctx, flow.ID, 1, false); err == nil {
		t.Error("历史非法图激活必须失败")
	}
	stored, err = svc.GetFlowByID(ctx, flow.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Status != model.FlowStatusDraft {
		t.Errorf("非法激活不得改变状态: stored=%+v", stored)
	}
}

// 节点 panic 或取消应终止后继，循环引用输入必须在执行前拒绝。
func TestMarketingFlowGraph_FailuresDoNotRunDescendants(t *testing.T) {
	graph, err := parseMarketingFlowGraph(`{"nodes":[{"id":"start","type":"trigger","next_nodes":["child"]},{"id":"child","type":"action"}]}`)
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	if _, err := graph.run(context.Background(), nil, func(context.Context, model.FlowNode, map[string]any) (map[string]any, error) {
		calls++
		panic("local panic")
	}, nil); err == nil || calls != 1 {
		t.Errorf("panic 后不得执行后继: calls=%d err=%v", calls, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	calls = 0
	execute := func(context.Context, model.FlowNode, map[string]any) (map[string]any, error) {
		calls++
		return nil, nil
	}
	if _, err := graph.run(ctx, nil, execute, nil); err == nil || calls != 0 {
		t.Errorf("取消后不得执行: calls=%d err=%v", calls, err)
	}
	cyclic := map[string]any{}
	cyclic["self"] = cyclic
	if _, err := graph.run(context.Background(), cyclic, execute, nil); err == nil || calls != 0 {
		t.Errorf("循环数据不得执行: calls=%d err=%v", calls, err)
	}
}

// 实际条件节点的真假结果只能执行对应短信替身，未选分支和孤立动作均零副作用。
func TestMarketingFlowGraph_BranchHasOnlySelectedSideEffects(t *testing.T) {
	for _, value := range []string{"yes", "no"} {
		t.Run(value, func(t *testing.T) {
			svc := setupMarketingFlowService(t)
			var mu sync.Mutex
			var sent []string
			old := smsSenderFunc
			SetSmsSender(func(_ context.Context, _, content string) error {
				mu.Lock()
				defer mu.Unlock()
				sent = append(sent, content)
				return nil
			})
			t.Cleanup(func() { SetSmsSender(old) })
			def := model.FlowDefinition{Nodes: []model.FlowNode{
				{ID: "start", Type: "trigger", NextNodes: []string{"condition"}},
				{ID: "condition", Type: "condition", Config: map[string]any{"condition": "answer eq yes"}, NextNodes: []string{"yes", "no"}},
				{ID: "yes", Type: "action", Config: map[string]any{"action_type": "send_sms", "phone": "13900001800", "content": "yes"}},
				{ID: "no", Type: "action", Config: map[string]any{"action_type": "send_sms", "phone": "13900001800", "content": "no"}},
				{ID: "orphan", Type: "action", Config: map[string]any{"action_type": "send_sms", "phone": "13900001800", "content": "orphan"}},
			}}
			raw, err := json.Marshal(def)
			if err != nil {
				t.Fatal(err)
			}
			execution := &model.FlowExecution{FlowID: 33, UserID: "local-spy", Status: "running", StartedAt: time.Now()}
			if err := svc.executionRepo.Create(context.Background(), execution); err != nil {
				t.Fatalf("创建执行记录失败: %v", err)
			}
			svc.executeFlow(context.Background(), execution, &model.MarketingFlow{FlowData: string(raw)}, map[string]any{"answer": value})
			if execution.Status != "completed" {
				t.Errorf("合法分支应完成: %+v", execution)
			}
			if !reflect.DeepEqual(sent, []string{value}) {
				t.Errorf("只允许选中的分支发送一次: want=%v got=%v", []string{value}, sent)
			}
		})
	}
}
