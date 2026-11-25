# Test: LM-13 统计范围

- RED：`GetQualityStatsScoped undefined` 编译失败。
- GREEN：`go test ./internal/service -run 'TestLeadScopeStats_' -count=1` 3/3 PASS（真实 PG），覆盖受限账号统计、空授权拒绝、管理员全量。
- `go build ./...` rc=0。余项：详情/三路证据/摘要下钻的 scope 统一、多轴统计与空账号历史修复未覆盖；不宣称整项完成。
