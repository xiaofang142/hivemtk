# Bug Verification: FA-29 / FA-30 RFM

- Tested: 2026-11-22
- Result: verified（本机专用库与模拟时钟范围）

## Checks

| Check | Command / Action | Result | Notes |
|---|---|---|---|
| 原始复现 | 专用PG 127.0.0.1:8217，`go test -run '^TestRFM(Composite_|RawMetrics_|ComputeAll_)'` | fail | 最低/中间/单维全100、固定时钟2509天、激活规则未消费 |
| 修复后回归 | 同上命令 | pass | 27条PASS/0FAIL，含真实规则仓储双租户、501条分页、规则只读一次、读故障零写入 |
| 竞争检测 | `go test -race` 同范围 | pass | 3主用例PASS/0FAIL，0 DATA RACE |
| 相关既有回归 | RFM独立打分/分层/流失/cron | pass | 既有用例持续PASS |
| 全后端 | `make test-go-fast` | fail→pass | 唯一失败为`user-server/docs/capabilities.md`漂移；UPDATE_GOLDEN重新生成后capregistry全通过 |

## Residual Risks

未做真实租户页面操作、真实生产重算和部署验证；既有历史恒100数据需运营按既有重算入口触发。

## Recommendation

代码与本地集成判据成立；保留整体交付/链A核验后方可销项。
