# Bug Fix: 共享上下文员工账号授权

- **Slug**: conversation-context-employee-scope-20261125
- **Date**: 2026-11-25
- **Assessment**: valid / high；原 assessment 保留不改。

## Changes

1. Controller 仅从可信认证上下文取员工和租户；缺员工返回 401，账号授权拒绝返回 403，统一响应不携带私有内容。
2. `conversation_context_access.go` 新增员工专用 `SnapshotForUser`，先校验当前租户、非系统作用域、有效员工、真实账号及真实渠道，再读取上下文。内部双模块继续使用 `Snapshot`，不伪造员工主体。
3. 保留既有全量角色和旧新授权并集；新授权以数值账号 ID 精确匹配。旧授权保留渠道约束、有效范围和撤销状态；账号名称必须在授权渠道内唯一，未指定渠道时必须在整个租户内唯一；识别码不作为 ACL。
4. 每次读取重查授权。授权存储错误闭合拒绝；测试验证拒绝前没有访问上下文表，撤权前成功、撤权后立即拒绝。

## Red / Green

- 初始 RED：`crm04-red-gwmo3ycd/tests.jsonl`，7 PASS、17 FAIL（含父测试）；未授权账号、伪造身份等请求返回 200 并泄露六类私有上下文。
- 补充 RED：`crm04-review-red-tcu5bvzr/tests.jsonl`，同平台重名授权错误放行；`crm04-wildcard-red-yrq4vpcs/tests.jsonl`，未指定渠道的跨平台重名授权错误放行。
- 修复后验证见 `test.md`。证据目录统一位于 `/private/var/folders/xw/pd_rr_z54w38gq41yfm8h7sm0000gn/T/opencode/`。

## Scope / Integration

无迁移、新路由、前端或外发。未修改 CRM-24 行业/owner 配对和 LM-12 原子更新。保留其他会话在途代码；为配合其已更新的 `buildLeadReactTask` 签名，仅在本轮已经修改的 `lead_mining.go` 调用处传入当前 context 和冻结 scope，没有回退对方接口或测试。最终验收不依赖历史临时编译失败。
