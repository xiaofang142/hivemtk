# Bug Assessment: service 测试凭据 helper 递归导致栈溢出

- **Slug**: service-test-cred-recursion-20261125
- **Created**: 2026-11-25T08:10:00+08:00
- **Source**: 新 HEAD `d264d3104177587a87dd3c88e33fbe898536e688` 全量 `go test ./internal/service -count=1`
- **Verdict**: valid（test-only 缺陷，全量 service 门阻断）
- **Severity**: high（测试进程 fatal stack overflow）
- **Mode**: fixed（test-only，待主智能体独立复验；未改生产）

## Evidence

- `check-e4-service-full-after-webhook.log`：`fatal error: stack overflow`，重复栈帧为 `service.testCred → integration_test.go:30`。
- 实际文件：`user-server/internal/service/integration_test.go`。
- HEAD/worktree/staged SHA256 均为 `f380719fb8d1968ac9f604f57b2dfdea46e4ff6181d6d7758bd4f97782a268d7`，无外部 diff。

## Root Cause

`testCred` 在构造 `parts` map literal 时，把 6 个 key 写成 `testCred("old_key")` 等函数调用。map 尚未创建就再次进入同一 `testCred`，形成无条件递归；env override 也不能在递归点之前返回，因为每次仍先走到 map literal。

## Approved Minimal Fix

- 仅把 6 个 self-referential map key 替换为字面量：
  - `old_key` / `old_secret`
  - `keep_key` / `keep_secret`
  - `new_key` / `new_secret`
- 保留 `HIVEMTK_TEST_CRED_*` env override；
- 保留字符数组拼接的测试占位值；
- 不重构 integration setup，不改生产代码。

## Verification Plan

1. 新增 focused default/env/alias 回归，先记录 baseline 栈溢出 RED；
2. 替换 6 个 key 后 focused GREEN；
3. 重跑 full service，单独分类其余行为失败；
4. 产出 `fix.md` / `test.md`，未 commit/push/deploy/外发/UI。
