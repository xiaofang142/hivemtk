# Bug Assessment: 渠道账号版本门子查询继承外层条件

- **Slug**: channel-account-edition-subquery-20261125
- **Created**: 2026-11-25
- **Source**: 工作区提交前 Go 全量测试的定向复现
- **Verdict**: valid
- **Severity**: high

## Symptom and Reproduction

渠道分发首选账号、CRM 实体账号挂接和 SageX 合法识别码解析均出现“有效账号查不到”。使用账号主键与渠道主键不同的真实 PostgreSQL 夹具复现：版本门生成的 `channels` 子查询错误继承了外层 `channel_accounts.id` 条件。

## Root Cause

`internal/repository/channel_account.go` 在 `Session(NewDB: true)` 后又调用 `WithContext(ctx)`。GORM 的第二次普通 Session 会复制原语句 clauses，导致 `channels` 子查询继承外层账号查询条件；同时，传入的原始调用 context 会覆盖查询本身的 system-scope context。多租户下无绑定租户的识别码解析因此触发 fail-closed，生成空子查询。普通测试数据中账号与渠道主键恰好相同、且 tenant mode 关闭，掩盖了这两个问题。

## Files likely to change

- `user-server/internal/repository/channel_account.go`
- `user-server/internal/repository/edition_channel_account_repository_matrix_test.go`

## Verification

先运行新增回归用例确认原实现失败，再以最小仓储修复验证账号 ID、识别码解析和跨租户隔离均正确。
