# Bug Assessment: SRM 诊断卡方上尾概率错误

- Slug: api-ab-srm-probability-20261120
- Created: 2026-11-20
- Source: user-web #13 双模式curl
- Verdict: valid
- Severity: medium

## Reproduction

各100曝光、50/50期望，`GET /api/ab-experiments/1/diagnostics` 双模式HTTP200，chi2=0、df=1却p=0.0989601540194058，应为1。证据013-off-diagnostics.json、013-on-diagnostics.json。

## Root Cause

`chiSquareSurvival` 把Wilson-Hilferty变换后的值送入双尾正态函数。上尾概率不对称，这导致chi2=0不为1，统计值越接近期望也可能反而接近告警阈值。现有测试仅检查passed而未断言p值。

## Proposed Remediation

复用go.mod已有gonum的distuv.ChiSquared.Survival，保持无效df/负x现有防御行为。新增已知值：x=0→1、df1/x1→0.3173105、df1/x3.8414588→0.05、df2/x2→exp(-1)、df2/x13.8155106→0.001，以及单调性与均衡诊断p=1。不引入依赖。

## Files likely to change

- user-server/internal/ops/service/ab_stats.go
- user-server/internal/ops/service/ab_stats_test.go
- 本报告/本轮清单与证据。

## Validation

纯函数回归先红后绿；受影响服务与控制器包检查、build/vet；双模式HTTP回归。hooks为空。
