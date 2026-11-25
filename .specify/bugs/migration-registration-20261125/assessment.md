# Bug Assessment: v3.114.0 迁移未注册

- **Slug**: migration-registration-20261125
- **Created**: 2026-11-25
- **Source**: 工作区整体提交前 Go 全量测试
- **Verdict**: valid
- **Severity**: medium

## Symptom and Reproduction

`TestEveryImplementedMigrationIsRegistered` 报告 `v3.114.0: MergedIntoIDWidenVarchar64Migration` 未注册。
复现：在 `user-server/` 执行 `go test ./internal/migration/migrations -run '^TestEveryImplementedMigrationIsRegistered$' -count=1`，退出 1。
完整输出保存在工作区 `.planning/2026-11-25-workspace-commit/user-migration-registration-before.log`。

## Root Cause

`initial_schema.go` 保留了迁移实现暂缺时不注册的临时注释，但 `v3_114_0_merged_into_id_widen_migration.go` 已恢复；
注册清单未同步恢复，生产启动因而不会执行现有的合并目标标识宽度迁移。

## Proposed Remediation

在 v3.113.0 与 v3.115.0 之间注册既有的 `NewMergedIntoIDWidenVarchar64Migration(db)`，删除失效临时注释。
不修改迁移的 DDL 或版本，不添加未注册白名单。

## Files likely to change

- `user-server/internal/migration/migrations/initial_schema.go`

## Tests to add or update

使用既有 `TestEveryImplementedMigrationIsRegistered` 及注册表元数据测试验证，无须重复添加同义测试。
完成后复跑迁移包及 Go 全量门禁。

## Risks and Considerations

注册会使后续正常部署执行原本遗漏的 v3.114.0 迁移；本轮仅验证并推送，不部署。
既有实现使用模型声明的宽度执行幂等加宽，Down 不缩窄列。
