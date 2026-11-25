# Bug Assessment: 公司与客户公司关系入口补员工数据范围

- **Slug**: crm-company-relation-scope-20261125
- **Created**: 2026-11-25
- **Source**: 外层 `docs/未完成任务清单.md` §3.29 `CRM-20261125-03`
- **Verdict**: valid
- **Severity**: high

## Report

`CompanyService`（`service/company.go`）的 `List/GetByID/Update/Delete/UpsertRelation/ListRelationsBy*` 只校验参数与租户，未接入 `CustomerPermissionService` 的员工数据范围。Controller (`controller/company.go`) 有 `actor()` 但只用于审计，不用于数据过滤。因此：

- 同租户不同员工可列出/读取全部公司及其关系，包括未授权客户关联的公司；总数泄露隐藏关系。
- `UpsertRelation` 只验证参数非空，可把未授权客户与公司建立关系，或把关系写到未授权公司。

## Scope

- `service/company.go`：注入权限服务，新增带 scope 的方法（列表/详情/关系读写），按 `CustomerDataScope` 限制。
- `repository/company_repo.go`：补充 scoped 查询（公司列表按可见客户关联 + owner 规则；关系按可见客户/公司过滤）。
- `controller/company.go`：所有入口从可信登录上下文解析 scope 并传给 Service。
- 新增真实 PG 测试覆盖：同租户不同员工、跨租户、未授权客户 ID、授权正常读写、总数不泄露。

## Boundary

不使用「同租户」代替员工授权；保留 Company/多关系模型和 Controller 审计；不修改 RBAC/授权表；不触碰其他会话在途文件；无 UI/外发/发布/提交。
