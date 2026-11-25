# Fix: capability 测试夹具迁移到 canonical 数值账号

- 新增 `outbound_test_canonical_helper_test.go:seedCanonicalOutboundAccount`：按 tenant/channel 播种 canonical 渠道账号，返回数值 ID。
- 迁移 `outbound_capability_test.go` 三组（CapabilityDenials、UnregisteredCells、HubTenantMismatch）、`webhook_outbound_card_fallback_test.go`、`webhook_batchc_bridge_sent_test.go`、`webhook_ai_result_test.go`、`outbound_golden_test.go` 的账号夹具。
- 未改任何生产文件；capability 门策略未放宽。
