# Bug Fix: SRM 卡方上尾概率

- Slug: api-ab-srm-probability-20261120
- Fixed: 2026-11-20
- Assessment: ./assessment.md
- Status: applied

## Changes

`chiSquareSurvival` 使用go.mod已有Gonum `distuv.ChiSquared.Survival`，替代双尾正态近似。不增加依赖。回归断言5个已知分布值与单调性。

## Verification

- 修复前纯函数测试RED：5个已知概率全部失败，且单调性失败；日志 `.planning/ab-srm-red.log`。
- 修复后 `go test ./internal/ops/service ./internal/ops/controller -count=1`：PASS，71.544s/36.346s；日志 `.planning/ab-srm-green.log`。
- API build与两个修改包的go vet均exit0。
- 双模式HTTP回归已完成：chi2=0、df=1均返回srm_p_value=1、srm_passed=true。证据013-off-diagnostics-fixed.json和013-on-diagnostics-fixed.json；跨租户读取404。

## Deviations

无。没有修改既有统计契约或测试断言以掩盖错误。
