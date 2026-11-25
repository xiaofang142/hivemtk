# Bug Assessment: bridge channel hint 负例与 identify_code 权威语义漂移

- **Slug**: bridge-channel-hint-test-drift-20261125
- **Created**: 2026-11-25T08:30:00+08:00
- **Source**: 新 HEAD `d264d3104` full bridge 回归
- **Verdict**: valid（test-only 断言漂移，不是生产回归）
- **Severity**: medium（旧安全负例与当前 identify_code-only 契约冲突）
- **Mode**: fixed（test-only，待主智能体独立复验；未改 tenant/handler 生产）

## Evidence

- `check-e4-final-bridge-full.log`：`TestBindBridgeTenant_ChannelMismatchRejected` 期望 `ErrBridgeTenantAmbiguous`，实际 `err=nil`。
- `tenant_binding.go` / `tenant_binding_test.go` 的 staged 与 worktree SHA256 完全一致，均属 HEAD `d264d3104`，无外部 diff。

## Root Cause

`resolveBridgeIdentity` 当前明确把 identify_code 作为权威身份：请求 channel 只作为客户端 hint，渠道不一致时不拒绝，而是保留识别码解析出的 canonical channel，避免错误路由。账号不一致仍在同一入口 fail-closed。

旧测试仍要求 channel hint 不一致返回 ambiguous，与 identify_code-only 权威语义冲突。

## Approved Minimal Fix

- 仅修改 `TestBindBridgeTenant_ChannelMismatchRejected`：
  - 合法识别码 + 错误 channel hint + 正确 account 应成功；
  - 统一身份必须使用识别码解析出的 `douyin`，不能沿用错误 `weibo` hint；
  - account mismatch 负例保持不变。
- 不改生产代码，不恢复 tenant_code/bartoken 自证。

## Verification Plan

focused RED（当前已记录）→ 测试迁移 GREEN → full bridge 重跑；产出 `fix.md` / `test.md`。
