# Bug Assessment: CS06 检查点夹具与旧负例断言漂移

- **Slug**: cs06-pipeline-fixture-drift-20261125
- **Created**: 2026-11-25T03:15:00+08:00
- **Source**: E4 channelgw 全包与未修改 HEAD 基线对照
- **Verdict**: valid（测试 setup/断言问题，不是 E3 引入的生产回归）
- **Severity**: medium
- **Mode**: fixed（仅测试夹具/断言迁移；生产未改，待主智能体独立复验）

## Baseline Evidence

未修改 `head-snapshot-934825c2d` 上三个 CS06 用例全部 10s 超时：

- `TestCS06_PipelineSampleA_KnowledgeHit_FullChainWithTraceReconciliation`
- `TestCS06_PipelineSampleB_LowConfidence_HandoffNoticeNotModelReply`
- `TestCS06_PipelineReplayAndSendFailure_Negatives`

日志：`check-e4-cs06-clean-head-baseline.log`。actual 全包同样仅这 3 个失败：`check-e4-channelgw-full-after-migration.log`。

## Root Cause Breakdown

1. **capability fixture 缺失**：使用 `cs06-acc-*` 外部字符串，未播种 canonical Channel/ChannelAccount，`capability_mapping_incomplete` 阻断出站。
2. **FeedbackRecord 缺表**：`testutil.NewTestDB` 模型清单未包含 `FeedbackRecord`，持续出现 `relation feedback_records does not exist` 警告；是 setup 缺口，不是超时根因。
3. **旧负例断言与 E3 冲突**：`-unknown` 显式账号按 E3 应 fail-closed 零 outbox，旧测试仍等待一条 failed outbox 行，必然超时。
4. **database closed 为次生**：超时后测试 cleanup 关闭 DB，异步 AI goroutine 继续写，产生 `sql: database is closed`；不能靠加 timeout 掩盖。

## Controlled Experiment

在临时 canonical fixture 实验中，样本 A/B 均通过；样本 C 的旧“无效账号落 failed 行”断言仍不成立，因为 E3 正确语义是零 outbox。实验文件已恢复，不计入 E4 通过。

## Recommended Separate Repair

- 在 CS06 setup 增加 `FeedbackRecord`、Channel、ChannelAccount canonical fixture；
- 将失败负例改为“无效显式账号可见 error + 零 outbox”，或直接验证 `ResolveExplicitOutboundAccount`；
- 测试结束前确定性等待异步 AI 收尾，再关闭 DB；
- 不放大 timeout、不放宽 capability 门。

## Boundary

CS06 不并入 E3/E4 test-migration 结论；需主智能体批准独立修复 lane。

## Closure Status

- 主智能体已批准独立 test-only lane；
- 生产代码零改动，仅 `cs06_pipeline_checkpoint_test.go` unstaged 修复；staged blob `8254d9138b6f1b1b81793bc94bb15795e6da6bef594837ac9cba96906b9b251d` 保持未变；
- RED→GREEN 与 full channelgw 证据见 `test.md`，修复内容见 `fix.md`；
- 未放大 timeout、未放宽 capability 门、未 commit/push/deploy/外发/UI。
