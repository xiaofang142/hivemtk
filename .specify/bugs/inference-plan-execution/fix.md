# Bug Fix Report

- Slug: inference-plan-execution
- Fixed: 2026-11-20
- Assessment: valid（assessment.md）
- Status: implemented and locally verified；正式 API 发布及台账独立验收

## Summary

默认 Process/HTTP 改为仅规划，显式返回 mode、plan、planned_tool_call_count、execution_status 和 executed；实际调用次数归零，不发布内部规划提示。显式 execution 需装配授权审批门、工具执行器、回复生成器、最终复核及发布端口。全部调用预检通过后才执行；按实际执行尝试计数，使用真实工具结果生成正文，裁剪后的正文通过最终复核才发布。

## Changes

- runtime/dataflow_orchestrator.go：默认仅规划、真实计数、执行端口快照，移除默认空工具及发布器。
- 新 runtime/inference_execution.go：执行契约、门禁/生成端口、顺序执行与最终复核、安全错误链。
- runtime/inference_types.go：审批中独立业务终态。
- app/inference_orchestrator_wiring.go、pkg/utils/response/errors.go：显式 mode、未配置 503、待审批 202、结果诊断。
- runtime/inference_execution_test.go、app/inference_execution_test.go 及既有编排器/HTTP 测试：真实 RED、规划/执行及故障边界回归。

## Tests

原始真实默认五阶段 RED 已执行：ToolCallCount=1 而 spy.Execute=0，发布内部“客户在查订单。请：1. 调 order.lookup …”指令，缺少规划模式与未执行元数据；日志 inference-execution-red.log。修复后同一场景通过。工具业务失败原始原因遗漏也先 RED 后修复。最终定向 107 个独立叶用例通过（本项 48、RUN01 回归 59），无失败或跳过；覆盖真实默认链、实际次数、审批预检、缺端口、失败/取消/超时、部分执行、交接及 HTTP 契约。

## Local Verification

完整 runtime/app/tooluse 三包回归通过。八项全量命令退出码均为 0：Go 全量 726.1s、竞态 250.1s、vet 172.7s、build 31.0s、lint 17.0s（0 issues）、layer 0.5s（0 跨层）、architecture 28.7s、audit 75.7s。门禁期间冻结 6663 份文件，逐份 SHA-256 核对零漂移。最终仅补正式报告，产品源码不变。

architecture 的既有小写 Service 缺接口提示位于未修改的 webhook_wecomcs_puller.go；audit 的对端仓缺 checkout、无覆盖率 profile、本机 8204 无实例维度明确未核验，不把 SKIP 当作通过。正式部署另实跑内外健康及公开口令探针。Graphify 标准刷新实际失败（缺模块），不宣称图谱已更新。详见 test.md。

## Deviations

审批端口未直接桥接处于观察态的全局工具执行器，缺配置安全失败。授权拒绝或审批中不会执行任一工具。同步发布/交接端口返回的错误直接报失败，但本项不提供 RUN03 的持久化可靠受理、幂等或跨进程恢复；工具执行结果也不写入推理成功检查点冒充可恢复执行。

## Followups

仅按官方 API 链 A 发布并独立核验产物、双健康、公开口令探针和回滚点，登记上线台账后精准销项。继续唯一清单 RUN03；审批持久队列/可靠送达及跨进程幂等不在本项冒称完成。
