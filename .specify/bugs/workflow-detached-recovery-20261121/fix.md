# Bug Fix: 受理后脱钩请求 ctx + 卡死执行恢复扫描（FA-WF-06）

- **Slug**: workflow-detached-recovery-20261121
- **Fixed**: 2026-11-21
- **Status**: applied

## Summary

Execute 把 HTTP 请求 ctx 原样交给 dispatcher.Run：受理成功后客户端断开，后台首次读库即 canceled，执行永久停在无节点的 running（隔离 PG 复现：context canceled、节点行与次数均为 0）。另 FindStuckExecutions 全仓无生产消费者，卡死行无人处置。现：①Run 用 `context.WithoutCancel(ctx)`（保留租户/追踪取值、摘掉取消信号，traceID 已显式提取）；②新增 `RecoverStuckExecutions` 恢复扫描——超阈 running 用 FA-WF-04 栅栏写标为明确 failed（可查询、可重试），**不盲目重跑**（无 per-node 副作用幂等前重跑=重复副作用）。

## Changes

| File | Change |
|---|---|
| `internal/service/workflow_orchestrator.go` | Execute 脱钩 ctx；新增 RecoverStuckExecutions（栅栏标记，返回标记数） |
| `internal/service/workflow_recover_detached_test.go` | 新增 2 用例：受理后取消 ctx 仍跑完；恢复扫描只标记超阈 running（终态/未超阈不触碰） |

## Local Verification

- 专项 2 用例 + `-race` 通过（green.log / race.log）。
- 工作流族回归 27 PASS / 0 FAIL（regression.log）。批次 8 项静态门 rc=0（gates.json）。
- 真实测试库；阻塞假执行器控制取消时机，无真实外发。
