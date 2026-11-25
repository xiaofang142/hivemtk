# Fix: 判定统计按员工账号范围（LM-13）

- `repository/lead_mining_judgement.go`：新增 `GetQualityStatsScoped(ctx, accounts, days)` 与 `ErrLeadScopeDenied`；accounts=nil=管理员全量，空切片拒绝，非空按 account_id IN 过滤，复用既有统计聚合。
- `service/lead_mining.go`：新增 `GetLeadMiningQualityStatsScoped` 包装。
