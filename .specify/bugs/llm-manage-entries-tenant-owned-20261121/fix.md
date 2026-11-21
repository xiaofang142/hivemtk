# Bug Fix: LLM 管理写口/测试口的租户归属判定（FA-LLM-01 子范围）

- **Slug**: llm-manage-entries-tenant-owned-20261121
- **Fixed**: 2026-11-21
- **Status**: applied

## Summary

列表早已按租户过滤（tenantProviderNames），但 UpdateModel/DeleteModel/TestModel 三个写/测口按名字直取共享注册表——多租户下其他租户商户管理员可用名字改到/删到/拿密钥测到别的租户（或平台）的 provider。现三口统一过 ensureProviderManageable：与列表同一白名单口径（多租户 fail-closed），单租户模式沿用既有全局管理语义（needFilter=false 短路）。controller 三入口映射 403。

## Changes

| File | Change |
|---|---|
| `internal/service/llm_routing.go` | ErrLLMProviderForbidden + ensureProviderManageable（复用 tenantProviderNames）；Update/Delete/Test 三口接入 |
| `internal/controller/llm_routing.go` | 三入口 errors.Is → 403（TestModel 走 bizerr.CodeForbidden） |
| `internal/service/llm_routing_test.go` | 新增 2 用例：多租户 A 改/删/测 B 名字拒绝且零注册表变化、本租户名字照常改；单租户语义不变 |
| `internal/service/llm_provider_crud_test.go` | AttachAuditDB 后复原 nil（修前序用例遗留已关句柄导致同进程后续用例 "database is closed" 的测试卫生问题） |

## Local Verification

- 专项 2 用例 + `-race` 通过；LLM 路由回归 14 PASS / 0 FAIL（regression.log）。
- 全仓 build 通过。快测实例凭据由容器环境读入。
