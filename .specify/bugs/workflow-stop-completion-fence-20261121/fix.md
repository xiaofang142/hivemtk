# Bug Fix: 停止 CAS 与终态写栅栏（FA-WF-04）

- **Slug**: workflow-stop-completion-fence-20261121
- **Fixed**: 2026-11-21
- **Status**: applied

## Summary

StopExecution 此前读 running 后整行写 terminated（CAS 窗口内执行可能已终态）；dispatcher 的完成/失败写无旧状态条件，已确认停止的执行会被在途节点的完成写复活为 completed、停止原因被清空。现：仓储新增 UpdateStatusFenced（fromStatuses 非空=仅这些旧状态可迁移；空=仅不可从 terminated 迁出）；StopExecution 改 running→terminated CAS；completeExecution/failExecution 带非 terminated 栅栏；terminateExecution（停机）带 running CAS。被栅栏拦下的写显式留痕（Warn），停止原因保留。

## Changes

| File | Change |
|---|---|
| `internal/repository/workflow_orchestrator.go` | ErrExecutionStatusFenced + UpdateStatusFenced（条件原子更新，applied=false=被栅栏拦下） |
| `internal/service/workflow_orchestrator.go` | StopExecution 改 CAS（0 行=ErrExecutionNotRunning） |
| `internal/service/workflow_dispatcher.go` | complete/fail/terminate 三个终态写全部改栅栏/条件写，内存对象照常更新 |
| `internal/service/workflow_orchestrator_stop_fence_test.go` | 新增 3 用例：停止 CAS 双停拒绝+原因保留、确认停止不被完成/失败写改写、running 正常完成/失败不受栅栏影响 |

## Local Verification

- 专项 3 用例 + `-race` 通过；工作流回归（dispatcher/retry/validate/stop/subflow）16 PASS / 0 FAIL。
- 批次 8 项静态门 rc=0（gates.json）。
