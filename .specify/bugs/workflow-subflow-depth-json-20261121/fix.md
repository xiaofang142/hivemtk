# Bug Fix: 子流程深度上限 JSON 往返生效（FA-WF-05）

- **Slug**: workflow-subflow-depth-json-20261121
- **Fixed**: 2026-11-21
- **Status**: applied

## Summary

TriggerPayload["_subflow_depth"] 仅以 int 断言读取——JSONMap 经数据库 JSONB 往返后数值是 float64，断言失败即按 0 继续，深度上限可被"重启"绕过（隔离 PG 复现：深度 5 落库重载后返回 completed 并创建 1 个子执行）。现 workflowJSONInt 稳定数值解码（int/int64/float64/json.Number），非法/负值夹回 0。

## Changes

| File | Change |
|---|---|
| `internal/service/workflow_node_executors.go` | 深度读取改 workflowJSONInt + 负值夹 0 |
| `internal/service/workflow_node_executors_subflow_depth_test.go` | 新增 2 用例：JSON 往返 float64 深度 5 被拒（零子执行）、int/负值/垃圾值三形态 |

## Local Verification

- 专项 2 用例通过（真实 JSON 序列化往返复现 float64 形状）；工作流回归 16 PASS。
