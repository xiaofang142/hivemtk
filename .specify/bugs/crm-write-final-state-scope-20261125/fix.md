# Bug Fix: 客户及关联实体按最终写入状态校验权限

- **Slug**: crm-write-final-state-scope-20261125
- **Date**: 2026-11-25
- **Task**: CRM-20261125-02
- **Assessment**: ./assessment.md

## Changes

| File | Change |
|---|---|
| `service/crm_customer.go` | `AllowsWrite`：受限账号集合为空一律拒绝；OnlyOwned/OwnerUserID 下 ownerID=0 不再跳过归属检查 |
| `service/crm_customer.go` | `Update` 先经 `finalCustomerState` 归一最终写入值，校验与落库共用同一份账号/行业/负责人/标签值；不再出现「校验旧值、写空值」 |
| `service/crm_customer.go` | 新增 `validateEntityAccount`：实体新增/改挂账号必须真实存在、属当前租户、且在操作者账号范围内（数字 ID 或识别码回查后校验）；未提交指针（nil/0）放行 |
| `service/crm_customer.go` | `validateEntityAccount` 接收目标客户：基础账号集合通过后，有 `AccountRules` 时必须满足同一账号的行业与角色/显式负责人配对；`owner` 角色按目标客户负责人判定；规则只收敛、不扩大 `AccountIDs` |
| `service/crm_customer.go` | 三个 CreateEntity 入口与 UpdateEntity 均调用 `validateEntityAccount`；实体对象在新账号校验通过前不落库 |
| `service/crm_customer.go` | `CRMCustomerService` 增注入 `ChannelAccountRepository`，保持五层职责（Service 组合 Repo） |

## Boundary

- 不修改 RBAC/授权表、不扩大管理员以外角色权限；`AllAccounts` 管理员语义不变。
- 「未提交」与「明确清空」区分：空输入沿用旧值；实体 nil 账号指针不作为越权。
- 不触碰其他会话在途文件（ReAct/LM-01/LM-02/CRM-04/SageX/Bridge）。
- 无 UI/外发/发布/提交动作。

## Verification

见 `test.md`；本文件不宣称全量回归或上线。
