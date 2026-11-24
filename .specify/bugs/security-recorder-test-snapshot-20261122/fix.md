# Bug Fix: 安全遥测并发测试错误地共享活载荷

- Slug: security-recorder-test-snapshot-20261122
- Fixed: 2026-11-22
- Source: 本轮 `make test-go` 实际失败的 `fatal error: concurrent map iteration and map write`
- Status: applied

## Changes

| File | Change |
|---|---|
| `user-server/internal/middleware/security_recorder_test.go` | 手工刷盘腿改为与生产路径一致:持锁换出 `securityFailureDeltas` 并换上新载荷、同步 `securityFailureLastFlsh`;用独立 `sync.WaitGroup` 追踪手工刷盘协程,记录腿结束后先 `flushWG.Wait()`,再显式收口刷盘 |

生产代码 `security_recorder.go` 未改动:生产 `recordAuthFailure` 本就在持锁时换出快照,
本证据不证明生产路径存在同一缺陷。

## Local Verification

- 专项:`go test -race ./internal/middleware -run '^TestSecurityRecorder_AsyncFlushRacesWithRecording$' -count=5` → ok(2.698s)。
- 全包:`go test -race ./internal/middleware -count=1` → ok(29.240s)。
- 专用测试库 127.0.0.1:8217,凭据由 `docker exec mtk-postgres-test printenv` 注入进程,不落盘。

## 边界

并发压力与断言保持原样,未放宽测试;测试仍不能关闭其不拥有的生产后台刷盘任务,
故收尾以"等待自建协程 + 显式 Flush"界定。未提交/推送/部署。