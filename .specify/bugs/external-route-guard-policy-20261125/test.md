# Bug Test: 外部凭证路由守卫分类

- **Slug**: external-route-guard-policy-20261125
- **Result**: RED and focused/full GREEN

## RED Evidence

工作区 `.planning/2026-11-25-workspace-commit/route-security-red.log` 捕获修复前的 route-chain failures：SageX upload/bootstrap 与身份守卫/超管策略不匹配；MCP route 被生成 capability table 命中但 middleware chain 不含 RoutePolicyGuard。

## Focused GREEN

- `route-security-primary-final.log`: router/middleware focused regression exit 0。
- `route-security-results.md`: live route export 区分 `external-credential`；SageX 两条路由和 MCP 均正确归档；菜单能力生成与漂移检查成功，42 项自检通过；非零类型统计和静态检查结果均记录。
- 回归断言覆盖 SageX 两条路由使用 identify_code 外部身份、MCP 必须保留 MCPGuard、三个代表性的普通 integrations 管理路径仍受 admin-only 保护。

## Complete Gate

`user-go-summary.json` 记录最终完整 Go build、vet、test-go、audit、lint 及 CRM Python unittest 全部退出码 0。没有执行浏览器/UI 测试或部署。
