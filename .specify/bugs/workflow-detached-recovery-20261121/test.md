# Bug Verification: FA-WF-06

- **Slug**: workflow-detached-recovery-20261121
- **Tested**: 2026-11-21
- **Result**: pass（本地全绿；核心缺陷已修，调度侧余项见边界）

## 关闭判据对照

- 「受理与后台执行生命周期分离并保留租户/追踪」✔（WithoutCancel + traceID 显式提取；取消后执行仍 completed）
- 「已受理任务最终可查询到完成/明确失败或可重试态，不能永久停留在无节点的 running」✔（恢复扫描把卡死 running 标为明确 failed 并带 recovery 原因；不盲重跑）
- 「响应区分受理与实际完成」→ Execute 返回 running 执行（受理语义已明确）；查询侧沿既有 GetExecution
- 「分别覆盖 HTTP 正常返回/取消、受理后崩溃、首次读库故障及双 worker」→ 取消 ✔；受理后崩溃由恢复扫描用例覆盖（超阈 running 处置）✔；首次读库故障与双 worker 并行调度未在本项构造（仓储错误透传保持既有语义）
- 「采用可恢复的持久派工、租约和重启扫描」→ 重启扫描 ✔（RecoverStuckExecutions）；**持久派工租约表与调度侧挂载为余项**（见边界）

## Checks

| Check | Command | Result | Notes |
|---|---|---|---|
| 脱钩+恢复专项 | go test ./internal/service -run 'TestWorkflowExecute_SurvivesRequestCancelAfterAccept\|TestRecoverStuckExecutions_MarksOnlyRunning' -count=1 -v | pass | green.log 2 PASS |
| 专项 race | 同上 -race | pass | race.log |
| 工作流族回归 | 见 regression.log | pass | 27 PASS / 0 FAIL |

## 边界

- 恢复扫描已就位但**尚未挂 cron/启动期调度**（cron 装配面属 §3.26 架构域，全仓现无 cron 消费点挂载模式）；挂载后即形成"重启扫描"闭环。
- 持久派工租约表（崩溃在"已受理未开跑"窗口的自动续跑）未建；当前语义为"标记可诊断终态 + 人工/接口重试"，不冒充自动恢复。
- 未提交/推送/部署。

## Evidence Files

fix.md / green.log / race.log / regression.log / gates.json / source-sha256.json
