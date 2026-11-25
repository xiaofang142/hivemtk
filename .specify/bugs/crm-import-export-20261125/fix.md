# Fix: CRM 导入导出（CRM-09/10/11/12）

- Slug: crm-import-export-20261125
- Date: 2026-11-25

## 改动文件

| 文件 | 内容 |
|---|---|
| `user-server/internal/service/customer_import_export.go` | 全部实现（唯一后端改动文件） |
| `user-server/internal/service/customer_import_export_test.go` | 新增 RED/GREEN 用例 |
| `user-web/src/views/crm/ImportExport.vue` | 必要接线：导入跳转到既有向导；导出补来源账号/负责人/实体/字段筛选与 xlsx 下载按钮 |
| `.specify/bugs/crm-import-export-20261125/{assessment,fix,test}.md` | 规格记录 |

未改：`customer_permission.go`、`crm_customer.go`、`company.go`、`customer_import_job.go`、`router/*`、任何其他在途文件；未改数据库 schema，未 commit/push/发布/UI 真机点验。

## CRM-09 实现要点

1. `ParseCustomerFile` 用 ZIP magic + OOXML 部件判定真 xlsx（标准库 `archive/zip` + `encoding/xml`，无新增依赖），支持 sharedStrings 与 inlineStr；改扩展名的 CSV 仍按 CSV 报告，坏 xlsx 显式失败；10MB 体积上限与 10000 行上限。
2. `NormalizeImportFieldMapping` 字段白名单（行业/负责人/来源账号/公司/渠道/多身份字段），未知目标字段丢弃。
3. CSV 用 `encoding/csv`（`LazyQuotes=false`），支持 BOM、引号包裹逗号、字段内换行。
4. `PrecheckRows` 给出映射、非法行、重复行、越权行四类原因与计数；`PrecheckWithScope` 调既有 `CustomerImportJobService.PrecheckScoped` 复用同一作业/报告，jobID 与持久化报告不另造。
5. `rowAllowed` 改为复用 `crmImportDenyReason`，空授权、行业/负责人越权均拒绝（修掉原来 `OnlyOwned` 下 owner=0 放行）。

## CRM-10 实现要点

1. `DispatchImport`：校验归属/权限后标记 `queued` 并返回 jobID；`running/queued/completed` 重复派发幂等（不重建、不重跑）。
2. `RunImportBatch`：从作业持久化计数恢复游标（`Success+Skipped+Failed`），按批写入并 `UpdateStatus` 持久化成功/跳过/失败/更新与错误报告；`completed` 重复调用直接读回，不重复创建/更新；状态写失败显式返回错误。
3. 每批前 `scopeResolver` 重新解析权限；空授权或范围变化（账号/行业/负责人）立即标 `failed` 并停止，后续行不写库。
4. `ImportProgressFromJob` 供 UI 查进度；错误报告复用既有作业的错误报告与下载入口。

## CRM-11 实现要点

1. `ExportContract` 明确 customer/contact/company/supplier/service_provider 的白名单字段；`NormalizeExportFields` 过滤实体/字段恶意注入，空字段回默认全量。
2. `ToCustomerFilter` 把来源账号、行业、负责人、关键词叠加到权限过滤，与列表同口径。
3. `ExportRowsXLSX` 生成真正 OOXML（ZIP + worksheet + inlineStr），服务端按权限脱敏 phone/email；CSV/JSON 保留。
4. `ExportByContract` 用白名单字段 + 服务端脱敏导出。

## CRM-12 实现要点

1. `ExportJobSnapshot` 保存筛选/字段/脱敏策略/操作者/权限范围快照；`ExportJobStore` 承载作业状态与文件字节（不改 schema）。
2. `CreateExportJob` 按批分页流式生成，保存快照与操作者、写操作审计；格式 xlsx/csv。
3. `DownloadExportJob` 重新校验租户、创建者、当前权限范围；范围收窄（生成时授权账号/行业不在当前集合）、他员工、跨租户、过期一律拒绝；不提供公开静态 URL。
4. `CleanupExpired` 清理过期文件内容并标 `expired`，保留审计快照。

## 余项 / 未整项关闭原因

- 路由装配禁止修改：`DispatchImport/ImportProgressFromJob/CreateExportJob/DownloadExportJob` 已在 Service 层可用，但没有新增 HTTP 路由（不允许改 `router/*`，`controller/crm.go` 也不在可改范围）；前端 Excel 按钮调用的 `format=xlsx` 目前既有 Controller 仍会回 JSON，需后续授权改 `controller/crm.go` 才能真正下载 xlsx。
- 导出作业存储在进程内 `ExportJobStore`（不改数据库 schema），跨进程重启后作业不可恢复；审计/快照仅 DB 里的 `operation_logs`。
- 多实体导出（联系人/公司/供应商/服务商）已定义白名单契约，但实体行查询仍只有客户仓储的真实分页实现；其他实体清单数量一致性未接入真实数据（静态契约，不等于运行验收）。
- 导入执行按批恢复依赖调用方传入解析后的行集；从 `job.FilePath` 自动重读并跨进程派发的 worker 接线（租约/定时扫描）不在本次边界（不得改 router/作业源文件），需后续补。
- 未做 UI 真机点验（边界禁止），前端仅静态 lint 通过。
