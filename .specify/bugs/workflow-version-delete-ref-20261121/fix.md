# Bug Fix: 版本删除的执行引用保护（FA-36）

- **Slug**: workflow-version-delete-ref-20261121
- **Fixed**: 2026-11-21
- **Status**: applied

## Summary

DeleteVersion 无状态/引用检查直接硬删：workflow_executions 只存 workflow_id+version（无外键，dispatcher 每次启动/重试按这两个值重载定义），删除被引用版本会让在途恢复无法加载定义、历史失去可重放定义。现删除前按 (workflow_id, version) 统计执行引用，有引用一律拒绝（`ErrVersionReferenced`，带引用条数可诊断），零删除；不存在版本保持既有 not found。

## Changes

| File | Change |
|---|---|
| `internal/repository/workflow_orchestrator.go` | 新增 CountByWorkflowVersion |
| `internal/service/workflow_orchestrator.go` | ErrVersionReferenced + DeleteVersion 引用检查（先查版本→查引用→删） |
| `internal/service/workflow_version_delete_ref_test.go` | 新增 3 用例：四种状态引用均拒绝且零删除、无引用可删、不存在报 not found |

## Local Verification

- 专项 3 用例通过；工作流族回归 27 PASS / 0 FAIL；批次 8 项静态门 rc=0。
