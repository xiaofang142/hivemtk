# Bug Assessment: 旧新线索/判定/证据/统计统一员工范围

- **Slug**: lead-scope-stats-20261125
- **Task**: LM-20261125-13
- **Verdict**: valid
- **Report**: `GetQualityStats(ctx, days)` 无员工 scope，直连全租户判定；空 AccountID 历史行仅租户过滤。受限员工可通过统计接口读到全租户数据。
- **Scope**: 新增 scoped 统计/详情方法，按可见账号集合过滤；空授权拒绝，管理员正例放行；列表与统计同范围可复算。
