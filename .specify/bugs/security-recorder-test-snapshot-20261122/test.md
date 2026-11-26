# Bug Verification: 安全遥测并发测试活载荷

- Slug: security-recorder-test-snapshot-20261122
- Tested: 2026-11-22
- Result: pass(专项 5 轮 race + middleware 全包 race)

## Checks

| Check | Command | Result | Notes |
|---|---|---|---|
| 专项 race ×5 | `go test -race ./internal/middleware -run '^TestSecurityRecorder_AsyncFlushRacesWithRecording$' -count=5 -v` | pass | green.log 5 PASS / 0 FAIL,无 concurrent map 崩溃 |
| middleware 全包 race | `go test -race ./internal/middleware -count=1` | pass | race-middleware.log,ok 29.6s |

## 说明

- 修复前:该用例在 `make test-go` 中触发 `fatal error: concurrent map iteration and map write`(栈 `securitytelemetry.Merge ← flushSecurityAccessFailures`)。
- 修复后连续 5 轮 race 与全包 race 均通过,压力形态(8 记录协程 × 50 轮刷盘触发)未放宽。
- 专用测试库 127.0.0.1:8217,凭据由容器环境读入进程,不落盘。

## 边界

本证据只覆盖测试腿;生产 `recordAuthFailure` 早已持锁换出快照,未被本故障指涉。
未提交/推送/部署。

## Evidence Files

`assessment.md` / `fix.md` / `green.log` / `race-middleware.log` / `source-sha256.json`
