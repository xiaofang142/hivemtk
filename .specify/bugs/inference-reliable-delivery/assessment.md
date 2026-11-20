# Bug Assessment: 外发与人工交接缺少可靠受理及确认

- Slug: inference-reliable-delivery
- Created: 2026-11-20
- Source: FA-AGENT-RUN-03
- Verdict: valid（源码确认，下一阶段补实际行为 RED）
- Severity: high

## Report / Root Cause

RUN02 已区分规划与实际执行，但执行末端仍直接调用 Publish/Trigger，返回 nil 只说明同步端口没有报错，不能证明可靠受理或第三方确认。同一消息重试会重新外发，进程退出会丢失意向和回执。handoff_to_human 只表达决策，没有持久交接证据。

## 方案与取舍

独立的租户隔离交付账本记录不可变意向、幂等键、状态、发送尝试、外部受理与确认回执。键基于可信租户上下文及明确 message_id、账号、Agent、会话、动作；不拿正文或随机 TraceID 充当稳定消息身份。同键异意图报冲突。普通回复与人工交接共用账本，工具执行不纳入本项幂等承诺。

Gateway 契约放在无业务依赖的 delivery 包；runtime 只依赖该端口，app 装配 service，service 编排 repository，Model 仅字段。仓储显式拒绝缺租户，短事务领取租约、持久 sending 后再调用外部。完成采用租约 token 比较交换，网络不占数据库事务。丢回执、发送后写库失败、进程退出统一进入查证路径；恢复先按同一键 Lookup，只有从未受理且外部明确 absent 时才允许用原键 Send。Sender 必须提供原子幂等及回执查询能力，旧 Publish/Trigger 返回 nil 不能升级为 confirmed。

状态分为 intended、blocked、sending、unknown、accepted、confirmed、rejected；accepted 与 confirmed 分别累计持久时间证据，API 返回当前状态及实际计数，202 仅表示账本已持久受理而待后续确认。缺 sender/policy 时持久 blocked，可查询/重试；拒绝为终态，未知状态不自动重发。查询及人工重试接口均在现有鉴权组内，重试重新检查当前策略。服务重建实例仍可按键恢复，租约内竞争方不得发送。

不复用 MessageHub 桥接 outbox（其绑定渠道和消息 ACK）、SOPOutbox（流程事件语义）、StageCheckpoint（推理成功游标）或 HumanTask（人工待办索引，非交接确认）；不使用内存版生产存储。生产默认不装外部 sender 或放行策略，故没有启用第三方外发；使用本地可复现的受控回执和真实 PostgreSQL 故障/恢复验收。

## Planned Files

- 新 `internal/aiagent/agent/delivery/contracts.go`、`internal/model/inference_delivery.go`。
- 新 `internal/repository/inference_delivery.go` 及真实 PostgreSQL 测试。
- 新 `internal/service/inference_delivery.go` 及受控回执/故障/恢复测试。
- 新 `internal/controller/inference_delivery.go` 及 HTTP 测试。
- runtime 的 dataflow_orchestrator.go、inference_execution.go、types.go 及相关执行测试。
- app/inference_delivery_wiring.go、inference_orchestrator_wiring.go 与相关 HTTP 测试。
- router/router.go、cmd/api/main.go、pkg/db/migrate.go、response/errors.go 的定点装配与消息常量。
- 本目录 fix.md、test.md。

## Acceptance / Boundaries

先用旧接口证明重复外发和缺少可查受理证据的 RED，再验证未装配、拒绝、回执丢失、写库失败、重启恢复、并发单一领取、同键不同正文冲突、跨租户不可见。sender 受理数/确认数与账本事实一致；旧回执或迟到租约不能覆盖新状态。未知外部状态不能根据超时直接假成功或盲发。本项不承诺工具跨请求幂等、不修改 CKPT01 的推理恢复身份，也不把模拟信号表述为真实第三方生产送达。
