# Test: CS06 检查点夹具迁移

- **Slug**: cs06-pipeline-fixture-drift-20261125
- **Status**: verified (main independently verified 2026-11-25)
- **Verdict**: GREEN

## RED Evidence

`check-e4-cs06-red-current.log`：

- Sample A：FAQ 生成成功但 canonical 账号缺失，10s 等不到 outbox；
- Sample B：转人工公告同样被 canonical 账号缺失阻断；
- Replay/SendFailure：旧断言等待 `-unknown` failed outbox，与 E3 零 outbox 语义冲突；
- 三例均伴随 `feedback_records` 缺表；总耗时 31.516s，rc=1。

## GREEN Evidence

- `check-e4-cs06-green-r1.log`：三例全部 PASS，rc=0，1.841s；
- `check-e4-channelgw-full-green.log`：`go test ./internal/channelgw -count=1` rc=0，7.494s；
- 日志中三例均使用数值 canonical 账号 `1` / `2`，未再出现 `feedback_records` 缺表；
- invalid `1-unknown` 路径完成真实 `ai_result` 后保持 0 outbox；
- AI drain 在 DB cleanup 前释放处理标记，未再出现 `sql: database is closed`。

## Covered Contracts

- canonical Channel/ChannelAccount + custom edition 下国内桥接渠道可出站；
- FeedbackRecord setup 完整；
- 重放幂等且不重触 AI；
- 无效显式账号可见失败、零 outbox；
- 跨租户入站隔离与认领不可见；
- 不放大 timeout、不绕过 capability 门。

## Remaining Boundary

主智能体独立复验：`go test ./internal/channelgw -count=1 -v` PASS rc=0，9.027s（`main-monitor41-channelgw-full.log`）；bridge vitest 108/108 文件、1328 passed / 7 skipped（`main-monitor41-bridge-vitest.log`）。CS06 scoped lane main-verified；E 整体仍未关闭。
