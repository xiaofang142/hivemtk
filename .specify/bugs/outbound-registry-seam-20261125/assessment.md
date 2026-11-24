# Bug Assessment: webhook 出站仍直查桥接能力位

- **Slug**: outbound-registry-seam-20261125
- **Created**: 2026-11-25T02:20:00+08:00
- **Source**: pasted text — E BRIDGE-PROTOCOL / `TestBridgeOutboxParityWithWebhookOutbound`
- **Verdict**: valid
- **Severity**: medium

## Report (verbatim or summarized)

`TestBridgeOutboxParityWithWebhookOutbound` 要求 `webhook_outbound.go` 不再直接调用 `bridgeOutboxCapable`，桥接能力查询统一收口在 `outbound_registry.go`。clean `934825c2d` 实测仍在 `webhook_outbound.go:602` 与 `:635` 直查。

## Symptom

出站账号自动选择/显式账号归一化仍绕过 outbound registry seam，形成两个能力判断住址。当前行为未必错误，但后续注册表演进时容易出现 webhook 文件与 registry 漂移。

## Reproduction

```bash
go test ./internal/channelgw -run '^TestBridgeOutboxParityWithWebhookOutbound$' -count=1 -v
```

输出：

```text
webhook_outbound.go:602 不应再直接做桥接能力查询
webhook_outbound.go:635 不应再直接做桥接能力查询
```

## Suspected Code Paths

- `user-server/internal/service/webhook_outbound.go:601` — `autoPickOutboundAccount` 直查 `bridgeOutboxCapable`。
- `user-server/internal/service/webhook_outbound.go:633` — `normalizeOutboundAccount` 直查 `bridgeOutboxCapable`。
- `user-server/internal/service/outbound_registry.go:89` — 既有 registry 能力 seam。
- `user-server/internal/channelgw/registry_parity_test.go:289` — 静态闸住址。

## Root Cause Hypothesis

高置信度：B1 注册表化时只迁移了 sender/authorization 查询，账号选择 helper 仍留在 webhook 文件并直查能力位。

## Proposed Remediation

**Preferred**：将 `autoPickOutboundAccount` 与 `normalizeOutboundAccount` 整体迁移到 `outbound_registry.go`，与 `authorizeOutboundSubmission`、`outboundSenderFor` 同住址；`webhook_outbound.go` 只调用 helper，不再出现 `bridgeOutboxCapable(`。保持选号逻辑、错误降级与日志逐字不变。

**Alternatives**：

- 新增 wrapper 隐藏字面量：拒绝，只是把双住址藏起来，未真正收口。
- 删除能力判断：拒绝，会改变非桥接渠道行为。

**Files likely to change**:

- `user-server/internal/service/webhook_outbound.go`
- `user-server/internal/service/outbound_registry.go`

**Tests to add or update**:

- 既有 `TestBridgeOutboxParityWithWebhookOutbound` 作为 RED/GREEN 闸。
- `TestBridgeOutboxParityWithWebhookOutbound` 全 channel 集合断言保持通过。
- service 出站相关回归保持通过。

## Risks & Considerations

- 纯代码搬迁，不改变路由、数据库或外发语义。
- 需保持 `dbstate`、`tenant`、`logger` 等 import 归属清晰。
- 实际仓 `registry_parity_test.go` 正被外部修改；本 assessment 基于 clean 快照，回植前必须二次预检。

## Open Questions

无。
