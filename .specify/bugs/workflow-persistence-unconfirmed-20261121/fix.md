# Bug Fix: 持久化未确认不得报成功（FA-WF-02）

- **Slug**: workflow-persistence-unconfirmed-20261121
- **Fixed**: 2026-11-21
- **Status**: applied

## Summary

节点证据（结果/状态/耗时）与上下文/进度（context/current_node_id）两处写错误此前被 `_ =` 丢弃——隔离 PG 注入节点结果 UPDATE 失败后实际执行 completed、节点行仍 running。现两处写失败 ⇒ 执行以明确 failed 收口（错误串标 persistence unconfirmed 与具体写点），绝不冒充成功；外部副作用已发生的情况由 FA-WF-03 幂等标记与 FA-WF-06 恢复扫描承载，不盲目重放。

## Changes

| File | Change |
|---|---|
| `internal/service/workflow_dispatcher.go` | executeNode 成功路径：nodeExecRepo.Update 与 execRepo.UpdateFields 失败均 failExecution 并返回 |
| `internal/service/workflow_persistence_unconfirmed_test.go` | 新增 2 用例：GORM 回调注入上下文写失败 ⇒ failed+可诊断原因；无注入 ⇒ completed 对照 |

## Local Verification

- 专项 2 用例通过；工作流族回归 27 PASS / 0 FAIL；批次门禁 rc=0。
