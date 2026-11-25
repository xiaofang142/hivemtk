# Bug Fix: 账号 × 行业 × 角色授权配对

- **Slug**: crm-account-role-pairing-20261125
- **Task**: CRM-20261125-24

## Changes

- `service/customer_permission.go`：新增 `CustomerAccountRule` 与 `CustomerDataScope.AccountRules`；`Resolve` 按每条 Phase2 授权保留配对（账号引用 + 行业 + 角色），识别码仅用于账号解析不再直接当 ACL 键。
- `CanAccess` / `ListScoped`：配对存在时按规则逐行精判，A/甲、B/乙允许而交叉拒绝；列表按精判结果分页计数，不泄露被拒行。
- `service/crm_customer.go`: `AllowsWrite` 同样消费配对，写入不能跨账号组合行业/角色。
- 旧 scope 零值语义保留，管理员不受配对收敛。

## Boundary

不擅改既有 union 为交集（旧字段语义保留），不扩大管理员以外角色权限；不改 RBAC/授权表；无发布/外发/提交。
