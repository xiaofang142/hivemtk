# Bug Fix: 会话改派历史的并发一致性

- **Slug**: inbox-assignment-history-20261120（沿用当前评估）
- **Fixed**: 2026-11-20
- **Assessment**: ./assessment.md
- **Status**: implemented；专项验证通过；全量验证为 partial（全仓回归与 audit 有阻断），尚未发布

## Changes

AssignTx 在原事务的首次读取使用 GORM FOR UPDATE 行锁；更新、历史和返回的旧归属均从该锁内前态计算。只将 gorm.ErrRecordNotFound 映射成兼容的 conversation not found，其他读取错误保留。未改变人工连续改派及其他动作语义，未新增接口、表或配置。

测试在 GORM 查询回调中控制两个事务，通过 PostgreSQL pg_stat_activity 观察第二次读取已完成或进入真实锁等待，再放行第一事务；诊断 SQL 仅存在于测试。另覆盖历史插入失败回滚、查询取消、跨租户/缺租户拒绝和不存在兼容错误。

## Reproduction Before Fix

2026-11-20，专用 PostgreSQL 127.0.0.1:8217，测试库由 testutil 隔离创建；凭证仅从主仓 .env 注入子进程，未输出。

`go test ./internal/repository -run '^TestInboxAssignTx_' -count=1` 退出 1：

```text
FAIL: TestInboxAssignTx_ConcurrentHistoryChain
并发分配历史断裂: owner-0→owner-1, owner-0→owner-2
FAIL: TestInboxAssignTx_ReadCancellationPreserved
读取取消不应被改写为会话不存在，实际: conversation not found
```

历史回滚及租户拒绝用例在旧实现通过；不将它们伪装成新发现的越权问题。

## Checks During Fix

修复后定向测试首次退出 0（0.863s），后续复跑仍退出 0（`ok hivemtk-user/internal/repository 1.394s`）。完整门禁的部分阻断与边界记录在 [test.md](./test.md)。

## Scope and Deviations

核心生产及测试文件均在原评估范围内。为使全仓 `make lint` 可运行，对原本干净、但未通过格式门的 `user-server/internal/model/order_change_request.go` 做了仅 gofmt 的一行排版修正；没有改动其行为。未修改已有 EC-00 工作树及其他会话的知识采集文件。未覆盖自动认领资格、跨会话容量预占或缓存更新的异步提交顺序，不能据此宣称完整分配策略并发安全。
