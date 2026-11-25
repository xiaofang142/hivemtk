# Bug Assessment: CRM 导入导出（CSV/xlsx、映射、后台作业、受权下载）

- **Slug**: crm-import-export-20261125
- **Created**: 2026-11-25
- **Source**: 外层 `docs/未完成任务清单.md` §3.29 `CRM-20261125-09/10/11/12` 原文判据
- **Verdict**: valid（四项均为判据成立的代码缺口，静态证据见下；本轮施工后仍保留开放项）
- **Severity**: high

## 原文判据与当前缺口

### CRM-09（复用导入向导 + 真 Excel + 字段映射）

- 判据：复用现有 `customer-import-jobs` 预检/作业向导，不另造第二套预检/报告；补行业/负责人/来源/公司字段映射，预检展示映射、非法行、重复、越权原因；CSV 引号/换行/BOM、中文 Excel、坏格式和体积/行数上限；不能把 BOM/改扩展名当 Excel。
- 静态证据：`service/customer_import_job.go` 的 `readCSV/readCSVFile` 只支持 CSV；`TemplateCSV` 导出的 CSV 加 BOM 只是给 Excel 打开用，并非 xlsx 解析；`customerImportColumns` 无 `industry/owner_user_id/company_name` 映射；`mapRow` 只按表头名取值；`Precheck` 的 `ImportPrecheckReport` 无越权原因统计；`views/crm/ImportExport.vue` 仍是八列粘贴 CSV + JSON/CSV 直导；`views/customerImportJob/List.vue` 是既有向导（复用它，不重写）。
- 2026-11-25 工作树：`import-job-employee-scope-20261125` 已补作业归属/账号边界（CRM-05），本项是在其上扩展映射与文件解析，不能改 `customer_import_job.go`（边界禁止）。

### CRM-10（导入执行可恢复后台作业）

- 判据：执行请求返回 jobID；按批持久化游标与成功/跳过/失败结果；重启/重复派发不重复创建或更新；每批前复核权限、撤权停止；UI 查进度与下载完整失败报告；恢复后计数可对账与部分成功边界；状态写失败维持显式错误。
- 静态证据：`Execute` 在请求中一次跑完全部行，仅终态 `UpdateStatus`；`model.CustomerImportJob` 无游标/批次字段；无后台派发/恢复入口；`ExecuteScoped` 仅在开始检查一次行权限（通过共享变量 `rowScopeCheck`，且未逐批重新 Resolve）。

### CRM-11（导出筛选/字段对齐 + 真 Excel）

- 判据：可导出实体/字段/列表筛选映射契约；补来源账号/负责人等界面条件与字段白名单；支持真 xlsx；权限/脱敏在服务端；各类型清单/数量与同条件可见列表一致；>500 条、空结果、受限字段、跨行业/账号、恶意注入、中间页错误。
- 静态证据：`service/customer_import_export.go` 的 `Export` 只有客户类型固定八列 CSV/JSON；`ExportByQuery` 只接 keyword/industry/owner；`views/crm/ImportExport.vue` 未承接来源账号/实体类型/字段选择；联系人/公司/供应商/服务商未有导出映射；xlsx 支持缺失。

### CRM-12（大批量导出后台作业 + 受限下载）

- 判据：复用作业/存储按批流式生成，保存筛选/字段/脱敏策略/操作者；重试不产生不受控重复产物；执行及下载重新验证当前权限，范围收窄拒绝旧文件或重新生成；过期清理、失败状态、审计；重启/超量/撤权/他员工/他租户下载负例；不能公开静态 URL。
- 静态证据：CRM 导出在主请求内分页汇总内存由 `controller/crm.go:Export` 直接返回；无导出作业模型/Repository 入口；无可回溯的策略快照与下载鉴权。

## Scope / 边界

- 允许修改：`service/customer_import_export.go`、导入/导出专用 repository、`controller/customer_import_export*.go`（若存在）、`views/crm/ImportExport.vue`（仅必要接线）、新建 `_test.go`、本规格目录。
- 禁止修改：`customer_permission.go`、`crm_customer.go`、`company.go`、`customer_import_job.go`（CRM-05 主 agent 已施工）、`followup_record.go`、`auto_reply.go`、`lead_*.go`、`channel_*.go`、`outbound_*.go`、`webhook_*.go`、`customer_dedupe*.go`、`router/*`、任何已存在 `.specify/bugs` 目录。不改数据库 schema（需要时用现有 jsonb/text 字段或进程内作业状态，并在本文件说明并标余项）。不 commit/push/发布/外发/UI 真机点验。
- 架构约束：五层；Service 不碰 gorm/db，Repository 首参 ctx；新 Controller 不 import repository/gorm、不 c.JSON；裸 go func 禁用；新增文件命名不带禁用后缀。
- 验证策略：标准库 `testing` + 真实 PG `testutil.NewTestDB`（`POSTGRES_TEST_*` 取自 `hivemtk-user/.env`，端口 8217）；纯函数（CSV/xlsx/映射/白名单/策略快照）可单测；RED 先行，逐项限定测试，不跑全量 service（Bridge/Webhook/Outbound 有并行在途基线失败且很慢）。
- xlsx 依赖：`go.mod` 当前无 xlsx 库。若新增第三方依赖被边界/离线环境阻塞，则以标准库 `archive/zip` + `encoding/xml` 实现最小 OOXML（spreadsheetml）读取与写入，测试用真实 zip+xml 夹具（不是 BOM CSV/改扩展名），并在 test.md 记录依赖决策。

## 逐项施工范围

1. CRM-09：在 `service/customer_import_export.go` 增加字段映射/文件解析纯函数与复用现有 `CustomerImportJobService` 的入口（仅调用，不改其源文件）；预检报告补齐越权原因、映射显式展示；`ImportExport.vue` 改为复用既有作业向导的接线（只做必要接线，不重写向导）。
2. CRM-10：在 `service/customer_import_export.go` 增加导入作业的"排队执行/按批续跑"状态机，游标存于现有作业的可写字段（不新增 schema），通过专用 repository 的方法实现耐久；执行前/每批复核权限；重复派发幂等；失败报告复用现有错误报告。
3. CRM-11：在 `service/customer_import_export.go` 明确导出类型/字段白名单契约与统一筛选转换，服务端脱敏，支持真实 xlsx 输出；CSV/JSON 格式保留；`ImportExport.vue` 接来源账号/负责人/实体/字段。
4. CRM-12：在 `service/customer_import_export.go` 增加受权导出作业（筛选/字段/脱敏/操作者快照）、受权下载接口与清理/审计；下载时以当前范围重新判定，收窄拒绝旧文件。

## 已知余项（即使 GREEN 也保留开放的判据）

- 不能改 `router/*`，新 HTTP 入口仅在允许的 controller 文件内注册既有 `RegisterRoutes`（若该 controller 文件不存在则不新增路由装配），所以"UI 进度/下载"只能做必要前端接线 + 服务层可验证契约，不承诺线上真实路由可用；未做 UI 真机点验（边界禁止）。
- 不改 schema：游标/导出作业若用现有字段承载，字段长度/类型约束可能导致部分元数据需压缩或在内存中重建；无法持久化跨进程重启的部分明确写余项。
- CRM-09 的"执行与预检口径完全一致"依赖 CRM-10 后台作业；若后台恢复仅进程内，则跨进程重启后的续跑记为余项。
- 多实体导出的"数量与列表一致"若既有列表 repository 不提供同类过滤，则以服务端范围过滤对齐，并在 test.md 写明未覆盖的实体类型。
