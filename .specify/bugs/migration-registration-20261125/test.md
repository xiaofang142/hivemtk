# Bug Verification: v3.114.0 迁移注册

- **Slug**: migration-registration-20261125
- **Tested**: 2026-11-25
- **Assessment**: [assessment.md](assessment.md)
- **Fix**: [fix.md](fix.md)
- **Result**: focused-and-full-gates-pass

## Reproduction and Focused Verification

修复前，`TestEveryImplementedMigrationIsRegistered` 退出 1，指出
`v3.114.0: MergedIntoIDWidenVarchar64Migration` 已实现却未注册。
恢复既有注册调用后，下列检查退出 0：

```bash
go test ./internal/migration/migrations -run '^(TestEveryImplementedMigrationIsRegistered|TestRegisteredMigrationsMetadata)$' -count=1
```

输出为 `ok hivemtk-user/internal/migration/migrations 0.510s`。
`gofmt -d user-server/internal/migration/migrations/initial_schema.go` 无输出。
修复前后完整输出分别保存在工作区
`.planning/2026-11-25-workspace-commit/user-migration-registration-{before,after}.log`。

## Full Gates

最终完整 Go 门禁全部退出 0：`go build -p 1 ./...`、`make vet`、`make test-go`、
`make audit`、`make lint` 和 CRM Python unittest。摘要及日志位于工作区
`.planning/2026-11-25-workspace-commit/user-go-summary.json` 与 `user-go-{build,vet,tests,audit,lint,python}.log`；
完整测试结果也汇总于[工作区门禁报告](../workspace-user-go-gates-20261125/test.md)。

## Scope and Limitations

本轮只验证并提交代码，不部署。既有迁移的 DDL 和版本均未修改；恢复注册后，
后续运行迁移器会执行尚未应用的 v3.114.0。
Graphify 重建命令已执行，但本机 Python 缺少 `graphify`，导入失败，图谱未更新。
