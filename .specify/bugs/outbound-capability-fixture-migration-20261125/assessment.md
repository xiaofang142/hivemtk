# Bug Assessment: Bridge/Webhook capability 测试夹具漂移

- **Slug**: outbound-capability-fixture-migration-20261125
- **Created**: 2026-11-25
- **Verdict**: valid（测试夹具漂移，非生产回归）

## Report

E3（`outbound-explicit-account-validation-20261125`）把 bridge 出站显式账号改为必须数值 ID 并 fail-closed，但工作树里的 `outbound_capability_test.go` 等旧 capability 负例仍用 `local-account` 等外部字符串账号，账号解析先于 capability 门失败，导致 84 个用例报 `invalid explicit outbound account ID` 而非预期 capability reason。

## Scope

只迁移测试夹具：播种 canonical Channel/ChannelAccount 并把外部账号字符串替换为数值 ID；不放宽 capability 门、不改生产代码。
