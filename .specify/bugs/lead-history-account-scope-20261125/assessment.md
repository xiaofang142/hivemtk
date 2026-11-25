# Bug Assessment: 线索历史与节流缺少账号作用域

- **Slug**: lead-history-account-scope-20261125
- **Created**: 2026-11-25
- **Source**: pasted text，外层 `docs/未完成任务清单.md` LM-20261125-01
- **Verdict**: valid
- **Severity**: high

## Report

同租户账号可复用 conversation_id/group_id/sender_id，历史不能混入其他账号；节流、摘要应遵循真实渠道账号与会话边界。私聊双方、群已采集成员完整读取，空账号/解析失败/跨租户拒绝或显式降级。

## Symptom / Reproduction

静态确认：ListRecentByConversation 仅过滤 conversation_id；ListRecentByGroup 仅过滤 platform/group_id；ListRecentInboundBySender 仅过滤 platform/sender_id/inbound。leadMiningDebounceKey 未含账号，群还按 sender 分窗。

复现输入：同租户两账号、另一租户及另一平台插入相同会话/群/发送者，再查询 A 的历史及顺序；连续处理 A/B 消息观察 Judge 调用次数。运行复现尚未执行，状态 **not-run**；fix 阶段先新增失败测试再改实现。

## Suspected Code Paths / Root Cause Hypothesis

高置信度：查询 API 没有账号参数，因此调用方无法表达账号边界；单独解析规则/共享摘要账号未冻结同一已校验租户和平台的结果。现有 ChannelResolver 的识别码分支可返回其他租户/平台，不能直接作为取证授权。原始账号别名与规范账号键需区分，识别码必须先解析到真实账号并校验归属。

## Proposed Remediation

历史仓储显式接收平台、账号引用集合、单群聊类型，强制租户上下文和稳定 sent_at/id 倒序；sender 缺会话兜底仅限无会话私聊、双向读取。服务复用 ChannelResolver 和 ChannelAccount 归属校验，只有能唯一映射到同一真实账号的引用才合并。解析失败降级为精确原始账号范围，禁读共享上下文；空账号拒绝。已确认跨租户/平台与歧义拒绝。缓存/节流按 tenant/platform/规范账号/群或会话（无会话时 peer）统一，保留客户关联行为。

**Files likely to change**:

- `user-server/internal/repository/message_hub_inbox.go`
- `user-server/internal/repository/message_hub_history_scope_test.go`
- `user-server/internal/repository/message_hub_core_test.go`
- `user-server/internal/service/lead_history_scope.go`
- `user-server/internal/service/lead_history_scope_test.go`
- `user-server/internal/service/lead_mining.go`（保留既有 ReAct 差异）
- `user-server/internal/service/lead_judge_context.go`
- `user-server/internal/service/lead_mining_test.go`
- `user-server/internal/service/lead_mining_integration_test.go`
- `user-server/internal/service/lead_mining_tenant_isolation_test.go`
- `user-server/internal/service/lead_mining_tenant_scope_test.go`（仅必要夹具迁移）

**Tests to add or update**:

- 真实 PG：相同会话/群/发送者跨账号/平台/租户隔离；私聊双方、群多成员；等时间排序/limit；空作用域拒绝。
- 服务真实查询：已验证别名合并、账号节流隔离、群成员共窗、不同会话独立；解析失败/空账号/跨租户/错平台/歧义无扩读；共享摘要按规范账号。
- 迁移所有原查询调用和受影响夹具，不改客户跨账号归一规则。

## Risks & Considerations

解析失败只读同租户同平台精确原始账号，不合并别名、不加载共享摘要，日志明确降级。缺 conversation 的 sender 不能跨有明确会话/群的行扫描。历史有界最近 20 条不代表来源全量采集。无 schema/员工权限/发布变更；禁止 stage/commit/push，其他任务由主智能体处理。

## Open Questions

无阻塞项。扩展权限、真实身份入库和耐久尾触发不属本任务。
