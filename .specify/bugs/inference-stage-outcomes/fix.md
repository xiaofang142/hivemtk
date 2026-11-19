# Bug Fix Report

- Slug: inference-stage-outcomes
- Fixed: 2026-11-20
- Assessment: valid（assessment.md）
- Status: applied，功能与完整门禁已验证；生产发布另行登记

## Summary

普通与流式入口共用串行五阶段调度，技术失败、超时、拒绝、降级及转人工意向分别记录。技术失败保留可查阶段和错误链，清除回复及计划，不进入发布；HTTP 返回非零错误码和安全诊断。失败不把最后成功检查点标记完成，失败尝试与成功恢复产出分开存储。

## Changes

- runtime/inference_cycle.go、inference_outcome.go、inference_types.go：统一调度、阶段轨迹和统计。
- runtime/stream.go：五阶段及终态事件、失败通道关闭。
- runtime/inference_checkpoint.go：版本升级，冻结成功恢复快照并附记失败尝试。
- runtime/dataflow_orchestrator.go：保留诊断，拒绝/失败不发布，统计尝试总数。
- runtime/reviewer_stage.go：复核实际候选回复，技术错误不能继续。
- app/inference_orchestrator_wiring.go、pkg/utils/response/errors.go：沿用原路由与响应封套，错误使用安全常量。

## Tests

真实原行为 RED：五阶段故障/超时十例、失败恢复、流式缺失复核、拒绝仍发布，共十三项独立叶用例均失败，日志 `inference-outcome-red.log`。修复后定向集五十九项叶用例全部通过，无跳过，包含七组 HTTP 路由验证及十组真实 PostgreSQL 故障/超时恢复。完整验收见 [test.md](./test.md)。

## Local Verification

八项门禁均退出 0：全量 Go 485.0s、运行时与 app 竞态 84.7s、vet 26.4s、build 16.3s、golangci-lint 14.5s（0 issues）、分层 0.3s、架构 29.2s、audit 65.5s。验证前后 6657 份冻结文件无漂移。

架构警告来自未改动的私有 webhook 拉取助手，非新增违规。audit 的跨仓检查、覆盖率和本机口令探针等未执行维度详列于 test.md，不计为通过；正式部署仍须完成线上口令探针。Graphify 刷新实际执行失败：本机缺少 graphify 模块，未刷新图谱。当前尚未执行本轮生产发布或清单销项。

## Deviations

评估未列逐文件清单；以上文件对应评估明确的运行时、检查点、流式、编排器及 HTTP 边界。未改变 RUN-02 工具执行契约、RUN-03 发布可靠受理或 CKPT-01 指纹作用域。仅首阶段失败没有成功游标，诊断返回调用方/流式并记录服务日志，不伪造成功检查点。

## Followups

本项验证、发布、销项后继续唯一清单下一项。
