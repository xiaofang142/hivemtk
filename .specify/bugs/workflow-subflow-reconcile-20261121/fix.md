# Bug Fix: 子流程占位与真实创建结果对账（FA-WF-03）

- **Slug**: workflow-subflow-reconcile-20261121
- **Fixed**: 2026-11-21
- **Status**: applied

## Summary

SubflowNodeExecutor 先写 "已执行" 标记再创建子流程：创建失败节点记 failed 但标记已落库，重放见标记即返回 completed/already_executed，而子执行数为 0（失败伪装完成）。现：①父子链接落库（子执行 TriggerPayload 带 `_parent_exec_id`/`_parent_node_id`）；②重放对账——按链接统计真实子执行，有则幂等返回、零则清孤儿标记继续创建；③创建失败即清占位标记。

## Changes

| File | Change |
|---|---|
| `internal/repository/workflow_orchestrator.go` | CountSubflowChildren（按父执行/父节点/子工作流计数） |
| `internal/service/workflow_node_executor.go` | RemoveWorkflowSideEffect |
| `internal/service/workflow_node_executors.go` | 子流程创建失败清占位；重放对账；父子链接写入 |
| `internal/service/workflow_subflow_reconcile_test.go` | 新增 3 用例：失败清占位零子执行、孤儿标记对账恰一次创建+父链接可查、合法重放幂等 |

## Local Verification

- 专项 3 用例通过；工作流族回归 27 PASS / 0 FAIL；批次门禁 rc=0。
