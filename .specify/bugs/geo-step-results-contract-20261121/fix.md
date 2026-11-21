# Bug Fix: GEO 决策链上游结果类型契约统一（FA-31）

- **Slug**: geo-step-results-contract-20261121
- **Fixed**: 2026-11-21
- **Status**: applied

## Summary

引擎 `WorkflowService.Run` 把 `_step_results` 压平成 `map[string]string` 再传执行器，而真实注册的决策执行器 `upstreamJSON` 只识别 `map[string]*dto.StepResult`，source_attribution/content_gap_fill 在真实 Run 路径上必然报「缺少上游」。现把传递契约冻结为 `map[string]*dto.StepResult`（步骤名/类型/状态/结果），读侧两处（action 执行器、extractStepContent）同步对齐并按步骤名排序取首个非空结果（消除 map 随机序）。

## Changes

| File | Change |
|---|---|
| `internal/geo/service/workflow.go` | 传执行器前保留完整 StepResult（map 结构浅拷贝）；action 执行器与 extractStepContent 改读新契约 + sort |
| `internal/geo/service/workflow_step_results_contract_test.go` | 新增：真实 Run 四步闭环（探测→归因→补缺→线索捕获，思维链/任务/线索端口全断言）+ 缺上游显式报错 + 旧类型形状不得通过上游校验的契约守卫 |

## Local Verification

- 专项 2 用例 + `-race` 通过（green.log / race.log）。
- `go test ./internal/geo/... -count=1`：controller / repository / service 全 ok（regression.log，专用快测实例，凭据由容器环境读入）。
- 既有 decision_executors_test 的自拼 StepResult 用例与新契约一致，无需改动。
