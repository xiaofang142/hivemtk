# Bug Assessment: 商业闸顺序晚于角色权限与数据权限

- **Slug**: commercial-gate-order-20261125
- **Created**: 2026-11-25T01:54:00+08:00
- **Source**: pasted text — 工作区根 `docs/architecture/FULL_FEATURE_INVENTORY.md` §3 I-01
- **Verdict**: valid
- **Severity**: medium

## Report (verbatim or summarized)

> `user-server/internal/router/router.go` mounts JWT → Tenant → RoutePolicyGuard → DataScopeGuard → GuardSubscriptionPath → GuardModulePath, but `middleware/module_gate.go` and `subscription_guard.go` comments explicitly require commercial gates after auth/tenant but before role/data permission. This causes 403 to precede 4301/4305 and queries data permissions before commercial entitlement. Existing `route_policy_gate_test.go:525-556` documents current RoutePolicy-first behavior and must be updated. Inventory I-01 is a confirmed defect, not merely doc drift.

## Symptom

多租户 SaaS 下，未开通收费模块或订阅已到期的请求，若同时没有角色/菜单权限，会先被 `RoutePolicyGuard` 以 HTTP 403 拒绝，拿不到应返回的 4301/4305 购买或续费引导；按行寻址的请求还会在商业资格判断前执行 `DataScopeGuard` 的归属查询。预期顺序是 JWT → Tenant → Subscription → Module → RoutePolicy → DataScope。

## Reproduction

1. 在 `router.Setup` 装配前调用 `tenant.SetMode(tenant.ModeOn)`，确保订阅闸实际挂上。
2. 使用真实测试库与真实 `Setup` 引擎；装配后注入确定性 fake：订阅锁定/未锁定、模块开通/未开通，以及记录调用次数的菜单授予 provider。
3. 以无 GEO 菜单授予的 staff JWT 请求真实路由 `GET /api/geo/config`（模块归属 `geo`，且在能力面表内）。
4. 当前代码先执行 `RoutePolicyGuard`，返回 HTTP 403 + 能力面拒绝文案；预期在订阅锁定时返回业务码 4305，在模块未开通时返回业务码 4301，且两者都发生在角色权限判定之前。
5. 对真实 live 路由 `GET /api/customer-sessions/:id/transcript` 抓取 `HandlerNames`：当前顺序为 Tenant → RoutePolicy → DataScope → Subscription → Module；预期 Subscription/Module 均排在 RoutePolicy/DataScope 之前。
6. 匿名 ON 模式请求必须仍先被 JWT 以 401 拒绝，且两个商业 checker 都未被调用。

## Suspected Code Paths

- `user-server/internal/router/router.go:460-510` — 唯一生产装配点；`RoutePolicyGuard`/`DataScopeGuard` 在 `GuardSubscriptionPath`/`GuardModulePath` 之前注册，直接决定 Gin 快照到每条 auth 路由上的执行顺序。
- `user-server/internal/middleware/module_gate.go:24-26` — 明确要求模块闸位于“鉴权之后、角色权限之前”。
- `user-server/internal/middleware/subscription_guard.go:28` — 明确要求订阅闸与模块闸同位置，即“鉴权之后、角色权限之前”。
- `user-server/internal/router/route_policy_gate_test.go:525-556` — 现有 staff/admin 行为测试只验证 RoutePolicy 与路由内 AdminAuth 的先后，没有商业闸优先级的正反证据，需补充并修正说明。
- `user-server/internal/router/data_scope_gate_test.go:51-86` — 已有 live 链上 DataScopeGuard 的装配证据，可复用同一 `HandlerNames` 判据钉住商业闸与 DataScope 的相对顺序。
- `user-server/internal/pkg/tenant/tenant.go:60-90` — 模式是进程级 `atomic.Value`；ON 用例必须保存并恢复 `CurrentMode()`，且不得并行修改。

## Root Cause Hypothesis

高置信度：缺陷是 `router.Setup` 的中间件注册顺序错误，而不是任一 guard 的判定语义错误。Gin 在注册路由时快照 handler 链，因此后注册的商业闸不会提前执行；代码注释规定的顺序与装配顺序直接冲突。现有测试只覆盖“有守卫”以及 RoutePolicy 相对路由内 AdminAuth 的顺序，未覆盖商业闸相对 RoutePolicy/DataScope 的顺序，使该缺陷长期保持绿灯。

## Proposed Remediation

**Preferred**：在 `router.Setup` 中把订阅闸（仅 ON 模式）与模块闸的整段装配移动到 `TenantMiddleware` 之后、`RoutePolicyGuard` 之前；JWT、Tenant、公开路由、两条桥接 token 路由、guard 内部判定与 ON/OFF 语义均不变。

**Alternatives**：

- 在 RoutePolicy/DataScope 内部手动先调用商业 checker：拒绝。会把商业资格复制到两个 guard，形成多套顺序权威，且无法覆盖未来新增权限 guard。
- 只改注释接受现状：拒绝。I-01 已确认是行为缺陷，会导致错误优先级和商业判断前的权限查询。

**Files likely to change**:

- `user-server/internal/router/router.go`
- `user-server/internal/router/route_policy_gate_test.go`
- `user-server/internal/router/commercial_gate_order_test.go`（新增）
- `.specify/bugs/commercial-gate-order-20261125/assessment.md`（本报告）

**Tests to add or update**:

- ON 模式真实 `Setup` live 链：JWT/Tenant 在商业闸之前，Subscription/Module 在 RoutePolicy/DataScope 之前。
- 真实请求 + fake checker/provider：锁定优先返回 4305；未锁定但模块未开通返回 4301；商业闸均放行时才进入能力面 403；fake 调用序列证明商业拒绝时角色权限 provider 未被调用。
- ON 模式匿名控制组仍为 JWT 401，商业 checker 零调用。
- 更新 `TestRouteGuardChain_StaffRejectedOnAdminOnlyRoutes` 的说明，明确其验证的是“商业闸放行后”的角色权限优先级，并固定 OFF 模式避免受全局模式影响。

## Risks & Considerations

- `tenant.CurrentMode()` 与 checker/provider 都是进程级状态；用例必须串行、用 `t.Cleanup` 恢复原模式并撤掉 fake，不能污染并行用例。
- 订阅闸仍只在 ON 模式装配；OFF 模式行为必须保持原样。
- 只调整注册顺序，不改变 4301/4305、403、401 的 guard 语义或响应通道。
- 工作区已有并行改动，其中 `router.go` 已有 staged hunk；只能追加本任务的最小 unstaged 顺序调整，不能 reset/stash/覆盖。
- 真实 `Setup` 测试依赖专用 PostgreSQL 8217；28P01/端口钉扎属于环境问题，需与代码失败分开报告。

## Open Questions

- 无。
