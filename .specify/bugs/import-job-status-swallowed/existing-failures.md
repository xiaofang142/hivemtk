# 既存失败用例根因登记（只读，不修改文件）

- **日期**: 2026-11-25
- **范围**: 全量 `go test ./internal/service/ -count=1`（已导出 `POSTGRES_TEST_*`）的 10 个 FAIL
- **证据等级**: S 静态 + 测试复现；无源码修改

## 结论

10 个失败均为**并发 session 引入的测试夹具/实现契约漂移**，与本批 import-job 修复无因果关系。
已在纯净 HEAD（`15fa630b1`）隔离 worktree 上重跑 3 个代表用例，同样 FAIL，证明为基线既存。

## 根因链

1. `ea06bcf77 fix: 收口数据权限与线索降级判定并整改测试夹具凭据`（2026-11-25 06:58:47）
   修改 `user-server/internal/service/channel_dispatch.go`：显式出站账号 ID 必须能
   `strconv.ParseUint(accountID, 10, 32)` 且 DB 中存在 status=1 的真实账号行；
   否则返回 `invalid explicit outbound account ID %q`。
2. 测试夹具仍传字符串哨兵 ID（如 `bridge_observation_test.go:59` 的 `"observation-account"`、
   `:30` 的 `"result-observation-account"`、`:152` 的 `"observation-ingest-account"`），
   以及 CS01/CS02/CS04、delayed replay 相关用例的同类 ID → 校验拒绝 → 预期出站行/投递未发生。
3. 失败表现：
   - `TestBridgeObservation_*`：`channel error [bad_request]: invalid explicit outbound account ID "observation-account"`
   - `TestSmartCS01_*` / `TestCS02_*` / `TestCS04_*` / `TestDelayedReplayPermission_*`：获准路径出站行 0，期望 1

## 完整失败清单

| 用例 | 类别 |
|---|---|
| TestBridgeObservation_EnqueueStartsBeforePersistence | bridge 出站账号校验 |
| TestBridgeObservation_RegenerationHasOwnAttempt | bridge 出站账号校验 |
| TestWebhookBridgeSender_RejectsBeforeDirectOutboxWrite | bridge 出站账号校验 |
| TestSmartCS01_SampleA_KnowledgeHit_AIReplyApprovedOutbound | CS01 出站行 |
| TestSmartCS01_SampleB_LowConfidence_TransferToHuman_NoAIOutbound | CS01 出站行 |
| TestSmartCS01_ModelEmptyReply_FallbackNotMasqueradingAsKnowledgeHit | CS01 出站行 |
| TestCS02_ModelEmptyReply_DegradedRowCaliber | CS02 出站行 |
| TestCS02_Douyin_EngineFault_L3FallbackTransferToHuman | CS02 出站行 |
| TestCS02_Douyin_EngineFault_L1BackupHandleRecovers | CS02 出站行 |
| TestCS04_TakeoverDuringAIGeneration_PreSendLockCheck | CS04 出站行 |
| TestDelayedReplayPermission_BothPathsRespectHumanAndNewReply | replay 权限出站行 |

## 处置边界（不越界）

按 `未完成任务清单.md §3.28`「不与并发 session 争抢同一批文件」与
`hivemtk-user/CLAUDE.md` 规则 0：这些测试文件属并发 session 在途面，本批**只读登记、不修改、不提交**。
建议由负责该并发 session 的一方在收口时把夹具 account_id 改为真实数字账号（或为校验加测试注入 seam），
并全量重跑 service 包确认 10 个 FAIL 归零。
