# Bug Verification: webhook 出站能力查询收口 registry

- **Slug**: outbound-registry-seam-20261125
- **Tested**: 2026-11-25T02:22:00+08:00
- **Assessment**: ./assessment.md
- **Fix**: ./fix.md
- **Result**: verified

## Summary

`webhook_outbound.go` 已不再直接调用 `bridgeOutboxCapable`，两个账号选择 helper 迁入 `outbound_registry.go`。parity 闸、build、vet 均通过；service 扩展回归的 12 个 capability 失败在未修改基线同样复现，不改变本 seam 修复结论。

## Checks Performed

| Check | Command / Action | Result | Notes |
|-------|------------------|--------|-------|
| Reproduction (pre-fix) | channelgw parity 定向 | fail（预期 RED） | `webhook_outbound.go:602/635` 直查 |
| Parity (post-fix) | `go test ./internal/channelgw -run '^TestBridgeOutboxParityWithWebhookOutbound$' -count=1 -v` | pass | rc=0 |
| Build | `go build ./...` | pass | rc=0 |
| Vet | `go vet ./...` | pass | rc=0 |
| Related service | `TestOutboundRegistry*` / `TestBridgeOutbox*` / `TestDeliverBridgeOutbound*` | pass | 相关用例通过，1 个既有 skip |
| Extended service | `TestSendOutbound_Bridge_*` | fail（存量） | 12 个 capability 夹具失败，基线逐条复现 |

## Output Excerpts

```text
=== RUN   TestBridgeOutboxParityWithWebhookOutbound
--- PASS: TestBridgeOutboxParityWithWebhookOutbound
ok  hivemtk-user/internal/channelgw
rc=0
```

## Residual Risks

- 12 个存量 capability 测试失败需独立 lane 处理。
- 未做 UI、真实外发或生产验收。
- 实际仓回植需二次预检。

## Recommendation

本 seam bug 已验证，建议主智能体独立复验后关闭；E 的 bridge 测试迁移继续单独进行。
