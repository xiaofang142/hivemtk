# Bug Fix: channel dispatch query failures are no longer swallowed

- **Slug**: channel-dispatch-query-errors-20261125
- **Fixed**: 2026-11-25T01:29:30+08:00
- **Assessment**: ./assessment.md
- **Status**: applied

## Summary

`ChannelDispatchService.PickAccount` 现在区分“记录不存在”和“查询运行错误”：渠道、粘性会话、粘性/首选账号查询的运行错误使用 `%w` 上抛，不再降级到 preferred/round-robin；会话 `gorm.ErrRecordNotFound` 仍保持原有降级语义。

## Changes

| File | Change | Notes |
|------|--------|-------|
| `user-server/internal/service/channel_dispatch.go` | modified | channel/conversation/sticky-account/preferred-account 查询错误 fail-visible；仅 record-not-found 可继续 |
| `user-server/internal/service/channel_dispatch_query_errors_test.go` | added | 5 个错误传播用例 + 8 个子测的选择矩阵 |

## Diff Highlights

```go
ch, err := s.channelRepo.GetByTenantAndCode(ctx, in.TenantID, in.Channel)
if err != nil {
    return nil, fmt.Errorf("resolve dispatch channel: %w", err)
}
```

```go
conv, err := s.convRepo.FindLatestByPlatformAndPeer(...)
if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
    return nil, fmt.Errorf("query sticky conversation: %w", err)
}
```

## Tests Added or Updated

- `TestChannelDispatchService_ChannelQueryErrorIsNotSwallowed` — 渠道查询错误必须上抛。
- `TestChannelDispatchService_ConversationQueryErrorIsNotSwallowed` — 会话查询错误不得降级选号。
- `TestChannelDispatchService_StickyAccountQueryErrorIsNotSwallowed` — 粘性账号查询错误不得 round-robin。
- `TestChannelDispatchService_PreferredAccountQueryErrorIsNotSwallowed` — 首选账号查询错误不得 round-robin。
- `TestChannelDispatchService_RoundRobinListErrorIsNotSwallowed` — List 错误必须上抛且零账号。
- `TestChannelDispatchService_SelectionMatrix` — sticky/preferred/round-robin、record-not-found、禁用/跨渠道/跨租户、无账号。

## Local Verification

### 冻结快照（`934825c2d` + 本修复）

- RED：`go test ./internal/service -run '^TestChannelDispatchService_' -v` → rc=1；4 个错误传播用例因错误被吞失败，SelectionMatrix 8/8 通过。日志 `a934-dispatch-errors-red-r3.log`。
- GREEN：同命令 → rc=0；5 个错误传播用例 + SelectionMatrix 8/8 全通过。日志 `a934-dispatch-errors-green-r2.log`。
- `go build ./...` → rc=0。
- `go vet ./...` → rc=0。
- `python3 scripts/check-layer-boundary.py` → rc=0，0 新增跨层 import。
- `bash scripts/check-architecture.sh` → rc=0，1 个既有 interface warning。
- repository 两个定向测试 → rc=0，2/2 PASS。
- app `TestE2E_ReachPipeline_ChannelDispatch` → rc=0。

### 实际工作树（HEAD `934825c2d`，含外部 59 文件）

- 两个 scoped 文件与冻结快照 `cmp=IDENTICAL`；`channel_dispatch.go` diff 仅 assessment 范围。
- `go build ./...` → rc=0。
- `go test ./internal/repository -run '...'` → rc=0，2/2 PASS。
- `go test ./internal/app -run '^TestE2E_ReachPipeline_ChannelDispatch$'` → rc=0。
- `check-layer-boundary.py` → rc=0。
- `check-architecture.sh` → rc=0。
- `go test ./internal/service -run '^TestChannelDispatchService_'` → rc=1，**前置编译失败**：外部 `lead_miner_unified_test.go` 引用已消失的 `MineUnifiedLead`、`MineUnifiedLeadDetailed`、`ProcessBridgeLead`。
- `go vet ./...` → rc=1，同外部测试文件 4 个缺失符号。

实际工作树失败日志：`actual-a-dispatch-errors-final.log`、`actual-a-vet-final.log`。未修改外部文件。

## Deviations from Assessment

无。生产修复严格限制在 assessment 所列两个文件。

## Follow-ups

- 主智能体独立复验 A；外部 lead-mining 测试文件恢复编译后，在实际工作树重跑 service 定向与 vet。
- 桥接旧 token/tenant 契约测试漂移转 E，不在本 bug 范围。
