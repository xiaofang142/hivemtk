# Bug Assessment: 规划与实际执行契约混淆

- Slug: inference-plan-execution
- Created: 2026-11-20
- Source: 唯一清单 FA-AGENT-RUN-02
- Verdict: valid（源码直接确认，下一阶段补真实 RED）
- Severity: high

## Report

计划工具数冒充实际调用数，内部规划指令充当最终回复。关闭判据要求明确仅规划与执行契约；仅规划返回计划及计划数量并显式未执行；执行须在授权、审批、工具结果与回复生成后才发布，用假工具核对实际调用计数、正文及失败/审批中状态，不绕过工具闸门。

## Root Cause

`runtime/dataflow_orchestrator.go` 在工具分支之前把空 Reply 回落为 Plan.ReplyHint 并发布；工具分支只读取计划长度，从未调用 ToolRouterExecutor.Execute，构造器默认 noopToolRouter 又遮蔽缺少端口。生产 app 装配明确没有工具及生成端口，但 HTTP 仍暴露 final_reply/tool_call_count。RUN01 已解决阶段失败，不会自动解决本项的结果含义。

## 方案论证

采用显式 planning/execution 两种模式。原 Process 和 HTTP 未指定模式时默认 planning，返回 plan、planned_tool_call_count、mode、execution_status=not_executed；final_reply 为空、实际 tool_call_count=0、publishable=false，不发布或触发人工副作用。执行模式显式通过 ProcessWithMode 或 HTTP mode 请求；生产缺执行端口时关闭执行并返回安全诊断，不自动装配可能处于观察态的工具权限链。

执行模式使用显式 ExecutionGate（按请求及每个计划调用检查授权/审批）、现有 ToolRouterExecutor、ReplyGenerator 和 Reviewer。工具执行前检查所有必需端口；每个实际 Execute 调用递增一次计数，业务失败/错误/取消停止后续调用。审批中返回 pending_approval，不执行、不发布、不假报成功。仅全部工具结果成功后，将真实结果送生成器，再复核最终正文；复核不通过/生成失败不发布。无工具且已有经过复核的 SkipLLM 正文可直接使用，否则仍须生成及复核。执行恢复/跨进程恰好一次不在本项承诺。

不采用直接调用 ExecuteByName 的补丁，因为会在未建立身份、审批、回复生成端口时扩大线上副作用；不采用给现有计数改名字而继续发布提示，因为没有消除假最终回复。规划结果中的 ReplyHint 仍是内部提示，保留在明确 plan 字段中，不进入 final_reply。生产默认仅规划为本轮实现选择，符合清单验收要求。

## Files likely to change

- `user-server/internal/aiagent/agent/runtime/dataflow_orchestrator.go`
- `user-server/internal/aiagent/agent/runtime/inference_execution.go`（新增契约及执行调度）
- `user-server/internal/aiagent/agent/runtime/inference_execution_test.go`（新增故障、审批及顺序回归）
- `user-server/internal/aiagent/agent/runtime/dataflow_orchestrator_test.go`（既有副作用用例显式选执行模式）
- `user-server/internal/aiagent/agent/runtime/inference_types.go`（审批中结果枚举）
- `user-server/internal/app/inference_orchestrator_wiring.go`
- `user-server/internal/app/inference_execution_test.go`（新增 HTTP 契约验证）
- `user-server/internal/app/inference_outcome_test.go`（成功 fixture 对齐默认规划契约，保留原失败/拒绝断言）
- `user-server/internal/pkg/utils/response/errors.go`

## Tests / Risks / Boundaries

先在原接口上复现默认真实五阶段查单路径虚增计数和发布内部指令；再 RED/GREEN 验证缺端口、授权拒绝、审批中、工具失败/超时、多个工具实际计数、真实结果传生成器、生成及最终复核拒绝/失败、规划无副作用和 HTTP 模式。执行端口只使用本地受控信号，不触达第三方。保持原五阶段失败与检查点回归不变；计划与执行结果分层，工具执行不写入推理阶段检查点，不宣称跨请求幂等执行。RUN03 的发布/人工可靠受理和 CKPT01 的配置指纹仍独立。图谱文件在当前工作树不存在，只按实际源码定位。
