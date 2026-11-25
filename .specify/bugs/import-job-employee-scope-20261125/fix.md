# Bug Fix: 旧导入作业及报告访问纳入员工权限

- **Slug**: import-job-employee-scope-20261125
- **Task**: CRM-20261125-05

## Changes

| File | Change |
|---|---|
| `service/customer_import_job.go` | 注入权限服务；`Execute` 增加逐行 scope 校验钩子（越权行记失败不落库）；新增 `PrecheckScoped/ExecuteScoped/GetScoped/ListScoped/ErrorReportScoped/ErrorReportCSVScoped/Scope` |
| `service/customer_import_job.go` | 作业归属：管理员本租户全见，其他员工仅限自己创建；空授权执行拒绝；逐行校验 source_account_id/industry/owner |
| `controller/customer_import_job.go` | Precheck/Execute/List/Get/Errors/DownloadErrors 全部改走 scoped 方法；403 显式映射；创建者从可信登录上下文写入 |

## Boundary

保留已修四处状态写失败处理；不改 RBAC/授权表；不触碰并行会话在途文件；无发布/外发/提交。

## Verification

见 test.md。
