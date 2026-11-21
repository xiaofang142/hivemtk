package service

// workflow_node_executors_subflow_depth_test.go FA-WF-05：子流程深度上限在
// JSON 落库往返后继续生效。
//
// 原缺陷：TriggerPayload["_subflow_depth"] 仅以 int 断言读取——JSONMap 经数据库
// JSONB 往返后数值是 float64，断言失败即按 0 继续，深度上限可被"重启"绕过
// （隔离 PG 复现：深度 5 落库重载后返回 completed 并创建 1 个子执行）。
// 本文件以 JSON 序列化往返真实复现该形状，钉住稳定解码。

import (
	"context"
	"encoding/json"
	"testing"

	"hivemtk-user/internal/model"
)

func fa05ExecWithPayload(payload model.JSONMap) *WorkflowExecContext {
	return &WorkflowExecContext{
		NodeID:     "sub1",
		NodeType:   WorkflowNodeTypeSubflow,
		NodeConfig: map[string]any{"sub_workflow_id": "wf-child"},
		Execution:  &model.WorkflowExecution{TriggerPayload: payload},
	}
}

// JSON 往返后的 float64 深度必须被识别：达到上限即拒绝（orchestrator 缺席也走不到创建）。
func TestSubflowDepthSurvivesJSONRoundtrip(t *testing.T) {
	SetMaxSubflowDepthProvider(func() int { return 5 })
	t.Cleanup(func() { SetMaxSubflowDepthProvider(nil) })

	raw, _ := json.Marshal(map[string]any{"_subflow_depth": 5})
	var payload model.JSONMap
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatalf("构造 JSON 往返 payload 失败: %v", err)
	}
	if _, ok := payload["_subflow_depth"].(float64); !ok {
		t.Fatalf("前置条件：往返后深度应为 float64，实际 %T", payload["_subflow_depth"])
	}

	e := &SubflowNodeExecutor{} // orchestrator=nil：深度守卫必须先于它生效
	res, err := e.Execute(context.Background(), fa05ExecWithPayload(payload))
	if err != nil {
		t.Fatalf("Execute 不应返回 error：%v", err)
	}
	if res.Status != NodeStatusFailed {
		t.Fatalf("JSON 往返后深度 5 必须被拒（零子执行），实际 status=%s output=%v", res.Status, res.Output)
	}
	if out, _ := res.Output["error"].(string); out == "" || !contains(out, "max depth") {
		t.Errorf("拒绝原因应写明深度上限，实际 %v", res.Output)
	}
}

// int 形态（内存链路）同样生效；负值夹回 0 不放大也不拒绝正常链路。
func TestSubflowDepthIntAndNegative(t *testing.T) {
	SetMaxSubflowDepthProvider(func() int { return 5 })
	t.Cleanup(func() { SetMaxSubflowDepthProvider(nil) })
	e := &SubflowNodeExecutor{}

	res, _ := e.Execute(context.Background(), fa05ExecWithPayload(model.JSONMap{"_subflow_depth": 5}))
	if res.Status != NodeStatusFailed {
		t.Fatalf("int 形态深度 5 必须被拒，实际 %s", res.Status)
	}

	// 负值：夹回 0，继续走正常链路（orchestrator 缺席 = 跳过分支，非深度拒绝）。
	res, _ = e.Execute(context.Background(), fa05ExecWithPayload(model.JSONMap{"_subflow_depth": -3}))
	if res.Status == NodeStatusFailed {
		t.Fatalf("负深度夹回 0 后不得触发深度拒绝，实际 output=%v", res.Output)
	}

	// 非数值垃圾：夹回 0，不 panic 不拒绝。
	res, _ = e.Execute(context.Background(), fa05ExecWithPayload(model.JSONMap{"_subflow_depth": "garbage"}))
	if res.Status == NodeStatusFailed {
		t.Fatalf("非数值深度夹回 0 后不得触发深度拒绝，实际 output=%v", res.Output)
	}
}
