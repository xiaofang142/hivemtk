# Bug Assessment: 旧导入作业及报告访问纳入同一员工权限

- **Slug**: import-job-employee-scope-20261125
- **Created**: 2026-11-25
- **Source**: `docs/未完成任务清单.md` §3.29 `CRM-20261125-05`
- **Verdict**: valid

## Report

`CustomerImportJobService.Precheck/Execute` 逐行调用 `CustomerRepository` 直接创建/更新客户，未叠加 CRM scope；`List/Get/ErrorReport/ErrorReportCSV` 只按租户/作业 ID 读取，actor 仅用于审计。旧模板允许 `source_account_id`/`company_id`，构成受限 CRM 入口之外的旁路。

## Scope

- Service 增加 scope 参数方法：PrecheckScoped/ExecuteScoped/GetScoped/ListScoped/ErrorReportScoped/ErrorReportCSVScoped。
- 执行前校验作业归属（CreatedBy 与当前用户/管理员），执行时逐行校验账号/行业/负责人范围，撤权后拒绝。
- Controller 从可信登录上下文解析 scope 并传入。
- 真实 PG 测试覆盖空授权、两员工、撤权、跨租户、报告下载。
