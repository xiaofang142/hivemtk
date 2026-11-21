# Bug Assessment: 商户连接与回调表缺少模块归属

- **Slug**: merchant-module-ownership-20261121
- **Created**: 2026-11-21
- **Source**: 用户授权逐项完成未来任务清单；主智能体独立全量测试结果
- **Verdict**: valid
- **Severity**: medium

## Report

`TestEveryModelTableHasModuleOwner` 报告 `merchant_callback_events`、`merchant_callback_subscriptions`、`merchant_connections` 三张表未归属任何模块。

## Symptom

EC-02 的商户连接和可信回调模型已存在，但模块表登记未覆盖它们，导致模型完整性测试失败。

## Reproduction

1. 在产品 `user-server/` 运行 `go test -json ./... -p 3 -count=1 -timeout=15m`。
2. `internal/model` 包的 `TestEveryModelTableHasModuleOwner` 明确列出上述三表并失败。
3. 原始证据：`/var/folders/xw/pd_rr_z54w38gq41yfm8h7sm0000gn/T/hivemtk-future-full-go-el9j9aex/full-go.jsonl`。

## Suspected Code Paths

- `user-server/internal/model/module_tables.go`：`coreOwnedTables` 已登记 `integration_accounts`、`webhook_events`、`webhook_subscriptions`，没有 merchant 三表。
- `user-server/internal/model/merchant_connection.go`、`merchant_callback.go`：各自定义相应真实表名。
- `user-server/internal/model/module_tables_test.go`：完整性约束，不应削弱。

## Root Cause Hypothesis

置信度高。商户连接和回调持久基座已增加模型，但遗漏跨模块表生命周期归属登记。它们与现有 integration/webhook 基座同类，不能归入某一个可售渠道或营销模块，否则关闭该模块可能阻断必要的连接修复和事件观察。

## Proposed Remediation

在现有 core 集成/回调表相邻处登记三张表，简短解释基座归属。保留所有其他在途登记；不改变模型字段、迁移、鉴权、测试判据或业务行为。

**Files likely to change**:
- `user-server/internal/model/module_tables.go`

**Tests to add or update**:
- 不新增镜像实现的测试；重用现有 `TestEveryModelTableHasModuleOwner`、`TestTableOwnersPointToRealModules` 与完整 model 包测试。

## Risks & Considerations

仅修复登记遗漏。当前登记文件包含其他会话的 consent 修改，必须保留。全量测试仍在运行，本修复不等于全仓已通过。

## Open Questions

无；不涉及提供方网络、真实店铺、生产发布或外发。扩展配置 `hooks: {}`，没有前后置钩子。
