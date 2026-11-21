# Bug Assessment: 并发分配必须保留连续的会话归属历史

- **Slug**: inbox-assignment-history-20261120（自主生成；未复用其他会话目录）
- **Created**: 2026-11-20
- **Source**: 用户要求吸收 LangBot / Chatwoot 的系统能力，当前源码对照发现
- **Verdict**: valid（源码成立）；复现状态 partial，尚未运行新增数据库用例
- **Severity**: high

## Report

Chatwoot `app/services/auto_assignment/assignment_service.rb` 在事务内重新限定收件箱、未分配条件并锁定会话；HiveMTK 的 `AssignTx` 虽将会话更新和历史插入放在同一事务，却无锁读取旧归属。

## Symptom

两个事务都可读到 owner-0，随后依次改为 owner-1、owner-2，但历史却记录 owner-0→owner-1 和 owner-0→owner-2；后一次的旧归属和实际提交顺序不一致。该输出还用于 service 的负载缓存增减，因此不能把历史仅视为展示数据。

## Reproduction

在专用 PostgreSQL 测试实例创建一条已归属 owner-0 的会话。第一事务读取后，由测试回调暂缓进入更新；启动第二事务，在实际 PostgreSQL 活跃查询中确认它已经尝试读取同一会话，再放行第一事务。两次成功更新后按历史 ID 排序，第二条 FromUserID 必须等于第一条 ToUserID，最终会话归属必须等于最后一条 ToUserID。测试只操作隔离测试库，不触达消息平台。

## Suspected Code Paths

- `user-server/internal/repository/message_hub_inbox_assignment.go:AssignTx`：无锁 First 后根据快照写历史，任意读取错误都改写为不存在。
- `user-server/internal/service/inbox.go:Assign`：消费 OldAssignedTo / NewAssignedTo 更新缓存。
- `user-server/internal/pkg/tenancyidx/tenant_callback.go:RegisterTenantCallbacks`：已有 Query / Update / Create 租户注入；不能因 SQL 未手写 tenant_id 就断言越租户。

## Root Cause Hypothesis

高置信：PostgreSQL 默认 READ COMMITTED 的普通 SELECT 不锁行，事务只能保证本事务内更新与历史一起提交，不能保证读到的前态仍是本次修改的直接前态。

## Proposed Remediation

在同一事务的首次读取加 GORM `clause.Locking{Strength: "UPDATE"}`，使所有分配动作基于锁内最新行生成更新和历史。只将真正的 gorm.ErrRecordNotFound 转为既有 conversation not found，取消、锁超时与数据库故障保留原错误供上层判读。保持现有人工 assign/reassign/release/close/reopen 行为和接口兼容；不引入新状态、迁移或依赖。

**Files likely to change**:

- `user-server/internal/repository/message_hub_inbox_assignment.go`
- `user-server/internal/repository/message_hub_inbox_assignment_concurrency_test.go`

**Tests to add or update**:

- 双事务重分配的历史链与最终归属一致；通过数据库观察读取交错，不靠固定 sleep 猜执行顺序。
- 历史插入失败时会话更新回滚。
- 取消查询的错误不伪装成不存在；真正不存在仍兼容既有返回。
- 多租户回调启用时，跨租户 ID 不可修改且没有新增历史；缺租户失败关闭。
- 既有 AssignTx_AllActions / InboxAssignmentRepository_ListAndGroup 回归。

## Risks & Considerations

- 同一会话的写操作等待行锁，遵循既有上下文截止时间和数据库超时；不同会话不共享进程锁。
- 不把行锁等同于自动分配的唯一认领；当前通用 assign 支持连续改派，自动策略需另行约束其合法旧状态。
- 不宣称解决坐席容量跨会话预占、权限检查到使用的竞态或人工/AI 回复交接；它们有不同的不变量。
- 无新增配置、公开接口或生产副作用。

## Open Questions

无阻止本次修复的待确认需求；测试结果在 fix.md / test.md 留证，不回写本评估冒充事先复现。
