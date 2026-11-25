# Test: LM-17 指标契约

- RED：`LeadPipelineMetrics undefined` 编译失败。
- GREEN：`go test ./internal/service -run 'TestLeadPipelineMetrics_' -count=1` 2/2 PASS，覆盖分阶段分母不混用、零分母不可判断。
- `go build ./...` rc=0（以当时工作树为准）。
- 余项：完整入站→投递链路回归矩阵、SC-001 百条人工标注、precision/recall/时延/成本等真实指标未测；不宣称整项完成。
