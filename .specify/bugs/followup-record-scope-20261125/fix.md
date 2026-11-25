# Bug Fix: 跟进记录复用客户数据范围

- **Slug**: followup-record-scope-20261125
- **Task**: CRM-20261125-25

## Changes

- `service/followup_record.go`：注入权限服务；新增 `Scope/ListScoped/GetScoped/CreateScoped/UpdateScoped`；列表按可见客户 + owner 过滤并正确计数；创建/更新按最终关联客户与负责人双重校验，改挂越权客户拒绝。
- `controller/followup_record.go`：List/Get/Create/Update 全部走 scoped 方法；403 显式映射。
- `service/followup_record_scope_test.go`：真实 PG 4 用例。

## Boundary

保留既有 owner 守卫语义；不改 RBAC/授权表；无发布/外发/提交。
