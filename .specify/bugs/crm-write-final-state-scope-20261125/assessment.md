# Bug Assessment: 客户及关联实体按最终写入状态校验权限

- **Slug**: crm-write-final-state-scope-20261125
- **Created**: 2026-11-25
- **Source**: 外层 `docs/未完成任务清单.md` §3.29 `CRM-20261125-02`
- **Verdict**: valid
- **Severity**: high

## Report

`service/crm_customer.go` 的写权限存在可组合绕过：

1. `AllowsWrite` 在 `RestrictedAccounts=true && AccountIDs` 为空时，对任意 `accountID` 返回 true；`OnlyOwned`/`OwnerUserID` 在 `ownerID==0` 时同样跳过校验。空授权员工可创建客户、owner 员工可通过 0 值写入绕过归属限定。
2. `Create`/`Update` 用 `firstNonEmptyCRMValue` 把旧值兜底只用于权限校验，写库时却直接使用原始输入，校验对象与最终落库对象不一致（空输入可把账号/行业/负责人清空）。
3. `CreateContact/CreateSupplier/CreateServiceProvider` 仅校验客户可读，随后原样存入任意 `ChannelAccountID`；`UpdateEntity` 同样未校验新增/改挂的账号指针是否真实存在、属于当前租户且在操作者范围内。
4. 即使账号在基础 `AccountIDs` 集合内，实体写校验仍忽略 `AccountRules` 的账号×行业×角色/负责人配对；跨行业、`role=owner` 不匹配、显式负责人不匹配均被错误放行。`role=owner` 与显式负责人必须按**目标客户负责人**判定，不能误用渠道账号负责人。

## Symptom / Reproduction

真实 PG + Service 直调：`scope=RestrictedAccounts(true), AccountIDs=[]` 调 `Create`，预期 `ErrCRMCustomerForbidden`；给定授权账号 A，验证 `OwnerUserID=0` 不能绕过 OnlyOwned；`Update` 传空行业/账号确认最终落库值不会绕过校验；以合法客户挂未授权/他租户/不存在的 `ChannelAccountID` 创建实体及改挂均被拒绝且无副作用。

## Scope

- 修改 `service/crm_customer.go`、`service/customer_permission.go`（如需共享工具）。
- 新增 `service/crm_customer_write_scope_test.go`，真实 PG 覆盖正反例。
- 不修改 Controller/RBAC/授权表，不扩大管理员以外角色权限；不触碰其他会话在途文件（ReAct/LM-01/LM-02/CRM-04）。

## Verification Plan

RED（当前行为失败）→ 修复 → GREEN；再跑 `go build ./...`、`go vet ./...`、限定测试、层级/架构/GORM 门禁。迁移/发布按仓库规则另行授权，不自动上线。
