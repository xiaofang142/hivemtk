# Bug Assessment: 安全遥测并发测试错误地共享活载荷

- Slug: security-recorder-test-snapshot-20261122
- Created: 2026-11-22
- Source: 本轮 make test-go 实际失败日志
- Verdict: valid
- Severity: high（测试进程 fatal，阻断交付）

## Symptom and reproduction

`make test-go` 在 middleware 包发生 `fatal error: concurrent map iteration and map write`，栈为 securitytelemetry.Merge → flushSecurityAccessFailures → TestSecurityRecorder_AsyncFlushRacesWithRecording。原始日志 `/tmp/hivemtk-ec01-full-test-go.log`。

## Root cause

生产 recordAuthFailure 已在持锁时换出 securityFailureDeltas 并创建新载荷；测试的手动刷盘腿只读取活指针，未换出。它在锁外遍历仍被记录腿写入的 map。测试还未等待手工刷盘协程退出，可能污染后续测试。该证据不证明生产路径存在同一缺陷。

## Proposed remediation

仅修正测试手动刷盘腿，使其同生产路径一样持锁换出快照、更新刷盘时间；追踪并等待手工刷盘退出后再最终收口。保留并发压力与断言，不改生产遥测或放宽测试。以 -race 多次运行该用例和 middleware 全包验证。

## Files likely to change

- user-server/internal/middleware/security_recorder_test.go

## Risks

测试不能关闭其不拥有的后台任务；手工腿使用独立 WaitGroup，记录腿退出后等待，最后显式刷盘。
