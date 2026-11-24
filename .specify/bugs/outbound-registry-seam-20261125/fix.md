# Bug Fix: webhook 出站能力查询收口 registry

- **Slug**: outbound-registry-seam-20261125
- **Fixed**: 2026-11-25T02:22:00+08:00
- **Assessment**: ./assessment.md
- **Status**: applied

## Summary

将 `autoPickOutboundAccount` 与 `normalizeOutboundAccount` 从 `webhook_outbound.go` 整体迁移到 `outbound_registry.go`，桥接能力查询不再散落在 webhook 文件；选号、校验、降级与日志逻辑保持不变。

## Changes

| File | Change | Notes |
|------|--------|-------|
| `user-server/internal/service/webhook_outbound.go` | modified | 移除两个账号选择 helper 与不再使用的 `strconv` import |
| `user-server/internal/service/outbound_registry.go` | modified | 接收两个 helper，并补充 dbstate/tenant/logger/strconv/strings imports |

## Tests Added or Updated

- 既有 `TestBridgeOutboxParityWithWebhookOutbound` 作为 RED/GREEN 主闸。
- `TestOutboundRegistry*`、`TestBridgeOutbox*`、`TestDeliverBridgeOutbound*` 作为相关回归。

## Local Verification

- RED：`go test ./internal/channelgw -run '^TestBridgeOutboxParityWithWebhookOutbound$'` → rc=1，指出 `webhook_outbound.go:602/635` 直查能力位。
- GREEN：同命令 → rc=0。
- `go build ./...` → rc=0。
- `go vet ./...` → rc=0。
- service 扩展回归中 12 个 `capability_mapping_incomplete` 失败在未修改 `934825c2d` 基线逐条复现，属存量夹具问题，不是本次搬迁引入。

日志：`check-e-channelgw-baseline.log`、`check-e-outbound-seam-green-r2.log`、`check-e-outbound-build.log`、`check-e-outbound-vet.log`、`check-e-outbound-service-regression.log`、`check-e-outbound-service-baseline-compare.log`。

## Deviations from Assessment

无。

## Follow-ups

- 实际仓回植前需二次预检；`registry_parity_test.go` 正被外部修改，不在本 bug scope。
- 12 个存量 capability 测试失败留在既有 lane，不在本 bug 修复。
