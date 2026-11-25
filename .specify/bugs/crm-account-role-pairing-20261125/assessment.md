# Bug Assessment: 保留每个账号与行业及角色授权的配对关系

- **Slug**: crm-account-role-pairing-20261125
- **Created**: 2026-11-25
- **Source**: `docs/未完成任务清单.md` §3.29 `CRM-20261125-24`
- **Verdict**: valid

## Report

`Resolve` 将各账号的可见行业并入全局 `Industries`，任一 owner 角色令全局 `OnlyOwned=true`；`ToFilter/CanAccess/AllowsWrite` 按独立账号集合与行业集合组合过滤。A 账号只许行业甲、B 账号只许行业乙时，A/乙、B/甲可能被组合放行；混合 owner/viewer 又可能错误收窄合法账号。识别码被并入账号集合，码重置后可继续充当 ACL。

## Scope

- `CustomerDataScope` 增加逐账号配对规则（账号引用 + 行业集合 + 角色条件）。
- `Resolve` 按每条授权保留配对，识别码仅作为账号解析引用，不直接作为 ACL 键。
- `CanAccess/AllowsWrite/ToFilter` 在配对存在时按配对判定；旧字段作为兼容回落。
- 真实 PG/纯函数测试：A/甲、B/乙允许，交叉拒绝；owner/viewer 混合不互扩；码重置、撤权、管理员正例。
