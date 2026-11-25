# Bug Fix: 外部凭证路由守卫分类

- **Slug**: external-route-guard-policy-20261125
- **Assessment**: [assessment.md](assessment.md)
- **Status**: applied; focused and full Go gates green

## Changes

- `/api/integrations/**` 超管规则只精确排除 SageX upload/bootstrap 两条外部凭证端点；其余集成管理端点仍匹配原策略。
- 从生成菜单能力表移除错误的 `/api/mcp` 用户能力项；生成文件通过 `scripts/menu-route-role-audit.py` 生成/核对。
- 增加守卫链分类断言，把 SageX identify-code 与 MCPGuard 识别为 external-credential，不将其误认为匿名无校验、用户 JWT 或菜单授权。

## Security Preservation

SageX/MCP 的既有凭证验证路径未改变；没有新增白名单身份绕过、JWT/桥接回退或租户策略例外。测试同时断言普通 integrations 管理端点仍为超管受限。

## Local Verification

- RED：工作区 `route-security-red.log` 记录原路由门禁失败：两条 SageX 路由缺少识别身份/被超管规则覆盖，`POST /api/mcp` 错误命中能力表但没有 RoutePolicyGuard。
- GREEN：`route-security-primary-final.log` 的 focused route regression exit 0；具体路由守卫导出、菜单生成器自检及静态检查见工作区 `route-security-results.md`。
- GREEN：完整 user Go 门禁全部 exit 0，摘要 `user-go-summary.json`，包括 build、vet、test-go、audit、lint、Python CRM unittest。
- Graphify rebuild 受本机缺少 Python `graphify`/`tree_sitter` 依赖限制，未改变图谱产物。
