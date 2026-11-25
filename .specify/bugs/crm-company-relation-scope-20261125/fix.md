# Bug Fix: 公司与客户公司关系入口补员工数据范围

- **Slug**: crm-company-relation-scope-20261125
- **Task**: CRM-20261125-03
- **Assessment**: ./assessment.md

## Changes

| File | Change |
|---|---|
| `service/company.go` | 注入 `CustomerPermissionService`；新增 `Scope/ListScoped/GetByIDScoped/UpdateScoped/DeleteScoped/UpsertRelationScoped/ListRelationsByCustomerScoped/ListRelationsByCompanyScoped` |
| `service/company.go` | 公司可见性 = 与当前员工可见客户存在关联的公司（管理员不受关联反向收敛）+ owner 角色约束；分页在过滤后计算总数 |
| `service/company.go` | 写关系两端校验：客户可见 + 公司可见，拒绝 `ErrCRMCustomerForbidden` |
| `controller/company.go` | List/Get/Update/Delete/客户关系列表/写关系/公司关系列表全部改走 scoped 方法；scope 从可信登录 uid 解析；403 显式映射 |
| `service/company_scope_test.go` | 真实 PG：受限列表/详情/两端拒绝/合法写入/关系列表 6 用例 |
| `controller/company_scope_test.go` | httptest：空授权读/写关系均 403 且响应无私有内容 |

## Boundary

不使用同租户代替员工授权；保留 Company/多关系模型与 Controller 审计；不改 RBAC/授权表；不触碰并行会话在途文件；无 UI/发布/外发/提交。
