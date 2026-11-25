# Test: CRM 导入导出（CRM-09/10/11/12）

- Slug: crm-import-export-20261125
- Date: 2026-11-25
- 环境：`POSTGRES_TEST_HOST=127.0.0.1 POSTGRES_TEST_PORT=8217 POSTGRES_TEST_USER=admin POSTGRES_TEST_PASSWORD=<hivemtk-user/.env 的 POSTGRES_PASSWORD>`；所有 go test 单包串行 `-p 1 -parallel 1`。

## RED（实现前）

命令（在 `user-server/` 下）：

```
go test ./internal/service/ -run '^Test(ParseCustomerFile|NormalizeImportFieldMapping|PrecheckRows|PrecheckWithScope)' -count=1 -p 1 -parallel 1
```

结果：build failed，新增测试引用尚不存在的 `ParseCustomerFile`、`NormalizeImportFieldMapping`、`customerImportMaxBytes`、`customerImportMaxRows`、`PrecheckRows`、`PrecheckWithScope`（以及测试内 `itoa` 与其他测试文件重名，已改名为 `crmItoa`）。确认 RED 后才写实现。

## GREEN（实现后，真实运行结果）

1. 纯函数用例（CRM-09/11/12，含 xlsx 真 ZIP/inlineStr 校验与脱敏断言）：

```
POSTGRES_TEST_PASSWORD=... POSTGRES_TEST_HOST=127.0.0.1 POSTGRES_TEST_PORT=8217 POSTGRES_TEST_USER=admin \
go test ./internal/service/ -run '^Test(ParseCustomerFile|NormalizeImportFieldMapping|PrecheckRows|NormalizeExportFields|ExportRowsXLSX|ExportJobStore|ExportJobDownload)' -count=1 -p 1 -parallel 1
```

真实输出（2026-11-25 17:53）：`ok  hivemtk-user/internal/service  0.559s`（RC=0）。
含用例：BOM/引号/换行 CSV、中文真 xlsx（inlineStr）、坏格式与改扩展名、体积/行数上限、映射白名单、预检越权/重复原因、导出字段白名单、真 xlsx 输出与服务端脱敏、导出作业下载范围收窄/他员工/跨租户/过期拒绝、过期清理。

2. 真实 PG 用例（CRM-09/10）：

```
POSTGRES_TEST_PASSWORD=... POSTGRES_TEST_HOST=127.0.0.1 POSTGRES_TEST_PORT=8217 POSTGRES_TEST_USER=admin \
go test ./internal/service/ -run '^Test(PrecheckWithScope|DispatchImport|RunImportBatch)_' -count=1 -p 1 -parallel 1 -v
```

真实输出（2026-11-25 17:54）：

```
=== RUN   TestPrecheckWithScope_ReusesJobReport
--- PASS: TestPrecheckWithScope_ReusesJobReport (1.38s)
=== RUN   TestDispatchImport_Idempotent
--- PASS: TestDispatchImport_Idempotent (0.11s)
=== RUN   TestRunImportBatch_ResumeCounts
--- PASS: TestRunImportBatch_ResumeCounts (0.09s)
=== RUN   TestRunImportBatch_RevokedStops
--- PASS: TestRunImportBatch_RevokedStops (0.07s)
PASS
ok  hivemtk-user/internal/service  6.057s
```

覆盖：复用作业 jobID 与持久化报告、越权行原因、重复派发幂等（作业表计数恒 1）、按批恢复后计数对账（4 行只新增后 2 行）、撤权后不写库并标失败。

3. 格式与构建：

```
gofmt -w internal/service/customer_import_export.go internal/service/customer_import_export_test.go
go build ./internal/service/     # RC=0（17:50 复测）
go build ./...                   # 协调者复测 RC=0（并行会话收口后）
npx eslint src/views/crm/ImportExport.vue --fix
npx eslint src/views/crm/ImportExport.vue --max-warnings 80   # RC=0
```

4. 过程记录（不虚报）：曾有一次 CRM-09 全量限定测试在共享测试库上 >2 分钟未结束（多 go test 争用 8217），按协调提醒停止后重跑；期间并行会话在途文件多次短暂破坏 `service` 包编译（`crm_merge_snapshot.go`、`crm_dedupe.go`、`lead_mining.go`、`lead_channel_wiring_contract_test.go`），这些不属于本任务边界，协调者已代为修复 import 缺失，最终上述限定测试均 GREEN。

## 未覆盖范围

- 未跑全量 `go test ./internal/service/`（Bridge/Webhook/Outbound 基线失败且慢；并行会话在途）。
- 未做 UI 真机点验；前端仅 eslint 通过。
- CRM-11 多实体真实数据导出与数量对账、CRM-12 跨进程重启恢复、HTTP 路由接线未覆盖，详见 fix.md 余项。
- 既有 `TestImportScope_*`（CRM-05 主 agent 施工）在开工前复测 3 用例 GREEN（7.450s），本批未修改其源文件。
