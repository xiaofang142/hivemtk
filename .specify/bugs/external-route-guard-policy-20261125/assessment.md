# Bug Assessment: 外部凭证路由守卫分类

- **Slug**: external-route-guard-policy-20261125
- **Created**: 2026-11-25
- **Source**: 工作区完整 Go 回归的路由策略门禁
- **Verdict**: valid
- **Severity**: high

## Symptom and Reproduction

路由链盘点发现 SageX upload/bootstrap 两条由 `identify_code` 建立外部身份的路由，被宽泛的 `/api/integrations/**` 超管规则错误覆盖；同时 `/api/mcp` 被误列入用户菜单能力表，但 live middleware chain 只使用专用 `MCPGuard`。

## Root Cause

超管策略按路径模式覆盖所有 integrations 路由，未表达两个 SageX 外部凭证端点的例外。MCP 的外部调用凭证路由被生成器误判为用户菜单能力，造成 capability-chain 测试要求它使用 `RoutePolicyGuard`/JWT，与 MCP 专用凭证链冲突。

## Security Constraints

- SageX 仍由原有 controller/service `identify_code` 校验解析租户和账号；不引入用户 JWT 或桥接凭证。
- MCP 仍保留专用 `MCPGuard`，不引入 JWT/RoutePolicy，不放宽桥接回退。
- 其他 integrations 管理路由继续受超管策略保护。

## Files Changed

- `user-server/internal/middleware/route_policy.go` and `route_policy_test.go`
- `user-server/internal/model/menu_capability_routes.go` (generated from the route audit source)
- `user-server/internal/router/route_policy_gate_test.go` and `route_surface_dump_test.go`

## Verification

先复现守卫链不一致，再验证 SageX/MCP 保持各自凭证语义、其他集成管理仍为超管、生成菜单能力表无漂移。
