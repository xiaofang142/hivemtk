# Test

- `go test ./internal/service -run 'TestLeadArchiveConsistency_|TestLeadMaintainerMapping_' -count=1`：全部 PASS（4+2 用例）。
- 覆盖：候选/降级不得越闸、失败不伪填归档时间、多账号单维护人、一账号多维护人、未绑定不猜。
- 余项：与真实归档链/通知外发/前端配置的接线未做，不宣称整项完成。
