# Bug Fix: v3.114.0 迁移未注册

- **Slug**: migration-registration-20261125
- **Fixed**: 2026-11-25
- **Assessment**: [assessment.md](assessment.md)
- **Status**: applied

## Changes

在 `user-server/internal/migration/migrations/initial_schema.go` 中，按版本顺序恢复
`register(NewMergedIntoIDWidenVarchar64Migration(db))`，删除实现暂缺时遗留的临时说明。
既有迁移实现、DDL、版本和注册完整性测试均未修改。

## Local Verification

- 修复前：`go test ./internal/migration/migrations -run '^TestEveryImplementedMigrationIsRegistered$' -count=1`，退出 1，准确指出 v3.114.0。
- 修复后：`go test ./internal/migration/migrations -run '^(TestEveryImplementedMigrationIsRegistered|TestRegisteredMigrationsMetadata)$' -count=1`，退出 0，`ok .../internal/migration/migrations 0.510s`。
- `gofmt -d user-server/internal/migration/migrations/initial_schema.go` 无输出。

## Deviations from Assessment

无。完整回归结果见 [test.md](test.md)。
