# Bug Assessment: bridge 入参/body 校验晚于租户解析

- **Slug**: bridge-param-validation-order-20261125
- **Created**: 2026-11-25T03:15:00+08:00
- **Source**: E4 full bridge 回归
- **Verdict**: valid
- **Severity**: medium
- **Mode**: fixed（生产修复已落地，待主智能体独立复验；未 commit/push/deploy/外发/UI）

## Report

以下结构性入参错误被租户解析抢先拦截，返回 404 `tenant_unresolved`，而不是预期的 400/413：

- `TestGetBridgeOutbox_AccountIDRequired`
- `TestOutboxMetrics_ValidationError`
- `TestHandleOutboxSSE_RejectsBadParamsBeforeCommittingStream` 的缺 channel / 空 account_id
- `TestHTTPIngestHandler_BadRequest/body_超过_4MB`

## Evidence

`check-e4-bridge-full-after-migration.log`：

- outbox 缺 account_id：期望 400，实际 404；
- SSE 缺 channel/account_id：期望 400，实际 404；
- ingest 超 4MB：期望 413，实际 404 `tenant_unresolved`。

## Root Cause

- `GetBridgeOutbox` 在 account/channel 必填校验前调用 `bindBridgeTenant`；
- `HandleOutboxSSE` 在缺参分支先 bind，再返回 400；
- `HandleHTTPIngest` 在 body 大小校验前完成租户绑定。

identify_code-only 允许“请求省略 channel/account_id、由识别码补全”，因此修复不能简单把所有缺参都提前拒绝；应先区分“带合法识别码”与“无识别码且缺必填参数”。

## Proposed Remediation

1. 无 identify_code 且缺 channel/account_id：直接 400；
2. 有 identify_code：先解析身份，再用解析结果补全并校验；
3. body 大小等纯协议校验先于任何 DB/租户解析；
4. 保持无副作用：被拒请求不得刷在线位、不得写库。

## Conflict Boundary

`handler_http.go` 的 staged blob 保持 `a9ca29d6cd2bcfde5bcd36f57d4c0c1c420e18c586c74a5034cdd8e9dbae554b` 未变；修复只追加在 unstaged worktree，`sse.go` 同样保留其 staged blob。未覆盖外部 staged `tenant_binding.go` / channelgw registry 文件，也未触碰外部 constructor 重构。

## Closure Status

- assess→fix→test 已完成，详见 `fix.md` / `test.md`；
- 缺参/不支持渠道按契约返回 400，超限 body 返回 413，均先于租户绑定与业务副作用；
- bridge 全包最终 GREEN；E4 整体仍待 CS06、webhook assess-only 收口与主智能体独立复验。
