# Bug Assessment: 转化事件分页元数据不一致

- Slug: api-ab-events-pagination-20261120
- Created: 2026-11-20
- Source: 本轮 user-web #10 curl
- Verdict: valid
- Severity: medium

## Reproduction

SaaS GET `/api/ab-experiments/1/conversion-events?page=-1&page_size=-2` HTTP200/code0回显-1/-2；证据 `docs/verification/api-audit-2026-11-20/user-web/010-on-invalid-page.json`。正常请求200，跨租户404。

## Root Cause / Proposed Remediation

同控制器 `GetConversionEvents` 保留了未经归一的 Atoi 结果。使用已有 utils.ParsePagination 默认10、上限200、超限钳制，查询与响应共用参数。已有列表修复不覆盖此方法。

## Files likely to change

- `user-server/internal/ops/controller/ab_experiment.go`
- `user-server/internal/ops/controller/ab_experiment_test.go`
- 本轮清单、此报告和HTTP证据。

## Tests

添加有效实验下负数/非数字/超限/正常分页具体元数据断言，修复前红、修复后绿；全控制器测试、build/vet和双模式curl回归。hooks为空。保持合法调用契约。
