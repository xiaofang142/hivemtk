# Bug Assessment: 工具来源时间精度导致误撤销

- Created: 2026-11-20
- Verdict: valid
- Severity: medium

## Reproduction

完整Go回归中 TestManagedMCP_RevocationAcrossReplicas 初始暖缓存被拒；在未改动远端基线4ebf64ff及仅注入测试库凭据时仍复现。日志 api-ab-baseline-comparison.log。审批与控制器失败在移除部署env后通过，属于测试环境污染。

## Root Cause / Remediation

repository.ToolRuntimeRepository 对创建后内存副本的纳秒CreatedAt与PostgreSQL读回的微秒时间作精确Equal比较。PG持久化精度为微秒，不足一微秒差异不表示来源变化。仅对两类来源的CreatedAt比较统一截断到微秒；其余ID、租户、启用、配置与凭据校验不变。

## Files likely to change

- user-server/internal/repository/tool_runtime.go
- user-server/internal/repository/tool_runtime_test.go

## Regression

固定纳秒时间创建自定义/MCP来源，验证创建后内存副本通过；配置变化、创建时间变化一微秒、跨租户仍拒绝。已有MCP多副本撤销和app管理撤销回归必须通过。
