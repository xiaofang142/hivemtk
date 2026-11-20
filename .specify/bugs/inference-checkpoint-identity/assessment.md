# Bug Assessment: 检查点身份隔离与当前安全策略重检

- Slug: inference-checkpoint-identity
- Created: 2026-11-20
- Source: 唯一清单 FA-AGENT-CKPT-01
- Verdict: valid（真实运行时与 PostgreSQL RED）
- Severity: high

## Report / Root Cause

旧 v2 检查点只按渠道、会话、客户、类型和正文寻址；未绑定可信租户、账号、稳定消息 ID 或智能体完整配置。恢复旧快照后跳过游标之前阶段，包含当前 gatekeeper。仓储使用原始 Table 查询时没有显式租户条件，写入未设置 tenant_id，依赖可开关回调或数据库默认值。

独立 Go overlay 保持 RUN03 冻结源码不变，真实调用当前恢复链复现七组身份/配置变化仍复用旧游标及一组绕过当前安全门，共八个叶用例 FAIL。真实本地 PostgreSQL 又复现同 thread 读取两个租户、无可信租户仍可读、显式租户 773 写入默认租户 1 三个业务断言失败。这些是受控复现，不再仅凭静态代码推断生产泄漏。

## 方案与替代论证

采用 v3 稳定身份：可信 tenant 上下文、显式消息/账号/渠道/会话/客户、完整 AgentContext 和消息语义序列化 SHA-256，排除运输 trace、消息时间和配置加载时间。不能序列化或身份缺失时不读写检查点，明确降级并完整运行当前阶段；不默认租户。旧版本不复用，旧 CheckpointThreadID 仅保留源兼容并弃用，不用于实际存储。

恢复仍复用同身份已完成的感知、对齐和规划产出，但每次必须执行当前 gatekeeper 和 reviewer。安全重检不覆盖较早成功阶段的快照、不倒退成功游标；失败继续使用 RUN01 的 LastAttempt 与冻结成功快照分离机制。仓储自身严格要求非零可信租户，使用 OnlyTenant、显式查询条件和写入 tenant_id，不把隔离责任交给可关闭的插件。

不采用单加 AccountID：无法隔离新消息、智能体或配置变更。不采用只比较 Version：版本未递增的 prompt、模型或策略变更仍可能绕过。不整体禁用恢复：同身份重试仍应复用成功的昂贵阶段。当前安全闸与复核必须重跑，运行时可变策略不能仅靠配置摘要替代。

## Files likely to change

- `user-server/internal/aiagent/agent/runtime/inference_checkpoint.go`
- `user-server/internal/aiagent/agent/runtime/inference_outcome.go`（正式审查新增 RED：自定义安全阶段名仍须按职责位置重检）
- 新 `user-server/internal/aiagent/agent/runtime/inference_checkpoint_identity.go`
- 新 `user-server/internal/aiagent/agent/runtime/inference_checkpoint_identity_test.go`
- `user-server/internal/aiagent/agent/runtime/inference_checkpoint_test.go`
- `user-server/internal/aiagent/agent/runtime/inference_outcome_test.go`
- `user-server/internal/aiagent/agent/runtime/inference_outcome_boundary_test.go`
- `user-server/internal/repository/agent_checkpoint_repo.go`
- 新 `user-server/internal/repository/agent_checkpoint_identity_test.go`
- `user-server/internal/app/agent_checkpoint_wiring_test.go`
- `user-server/internal/app/inference_outcome_checkpoint_test.go`
- `user-server/internal/service/agent_checkpoint_test.go`
- `user-server/internal/service/agent_checkpoint_cursor_test.go`

## Tests / Risks / Boundaries

验证两账号相同外部身份与正文、不同租户、换智能体/版本/任一配置、重复文本新消息、同消息重试；当前安全门交接/失败/取消/超时及流式复核拒绝；真实 PG 显式租户读写和缺身份拒绝。旧测试迁移显式身份并增加 gate 重检期望，保留冻结快照、失败游标和完成标记原断言。生产装配真实落五阶段，开关关闭零写入。

本项不迁移旧 v2 快照，不保证工具或外部副作用跨请求恰好一次；RUN02/03 的规划执行和可靠交付边界保持。完整配置哈希可能保守地触发全重跑，优先防止误复用。无新表、路由或前端变更。
