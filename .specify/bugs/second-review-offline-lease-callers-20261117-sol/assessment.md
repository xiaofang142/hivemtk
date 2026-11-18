# 离线回扫旧租约调用方评估

Verdict: valid；Severity: high。

## 偏差与决策

`second-review-delayed-permission-20261117-sol`明确迁移无令牌离线写口，但其文件清单遗漏仓储旧测试的五处调用。本报告单独授权该测试迁移，不修改原assessment范围。旧测试直接按ID完成或改排pending记录，不能保留这种前提来绕过当前租约。

仅更新真实到期认领、令牌及租户前置；保留正常成功、失败改排、并发互斥、次数累计及判弃断言。新离线认领返回权威记录，回写要求当前sending、认领时间、有效租约与令牌；没有新增生产发送、积压释放或预算重置。

## Files likely to change

- `user-server/internal/repository/bridge_offline_replay_repo_test.go`
- 本目录fix/test与总计划。

## 验证

执行原仓储回扫测试及严格终态、双路径投递权限回归。仅专用PG测试库，不操作生产队列。
