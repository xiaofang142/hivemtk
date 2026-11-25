# Bug Assessment: 跟进记录列表与写入复用客户数据范围

- **Slug**: followup-record-scope-20261125
- **Created**: 2026-11-25
- **Source**: `docs/未完成任务清单.md` §3.29 `CRM-20261125-25`
- **Verdict**: valid

## Report

`service/followup_record.go` 的 List/Create/Get/Update 直接转仓储，列表仅租户 + 可选 customer/reminder/owner 筛选。另有 owner 守卫只在详情/更新局部存在，不覆盖列表/创建，也不校验关联客户数据范围。员工可通过指定 owner=0 或自己 owner 但关联未授权客户绕过。

## Scope

Service 注入 `CustomerPermissionService`，新增 ListScoped/GetScoped/CreateScoped/UpdateScoped/Scope；列表按权限过滤客户/owner，写入按最终关联客户校验。Controller 从可信登录上下文传 scope；403 显式映射。真实 PG 测试覆盖不传 owner、指定他员工、自己 owner 但关联未授权客户、改挂/清空、空授权、跨租户。
