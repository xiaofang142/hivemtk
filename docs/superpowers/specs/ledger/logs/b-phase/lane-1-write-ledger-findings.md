<!-- 恢复件：本文件是 B 相 lane 1 子代理的**原样回报正文**，由主会话于 2026-09-23 04:2x 从子代理逐字记录 `agent-ageneral-purpose-03ece5c7c487bda7.jsonl` 的最后一条 assistant 文本里抽出落盘（派发时间 02:2x，报告当时只留在会话里、没进磁盘）。它是**二手候选**：每条都带「未验证：候选均未实际注码运行」的自陈，逐条的重验结果在 `logs/c-phase/` 与主 spec §八/§7.29。-->

**§8.2 编号 2 · D7 fail-closed / 零帧下发**

- 承诺：未放行（含超时）即一帧不下发；不匹配即拒。
- 注码：`user-server/internal/browser_automation/service/executor.go:1284-1285` `default:` 分支把 `return nil, fmt.Errorf("post_comment 等待人工确认超时…")` 换成 `logger.Warnf(...)` 后 fall through；1284 行的 `confirmWaitTimedOut：` 注释字面保留。执行流即续到 `sendOnce()`（`:1313`）落 `comment_send`。
- 预言：`require_confirm` 任务在确认预算耗尽后仍把评论提交上去——不可逆动作绕过 D7。
- 凭什么不红：`TestConfirmGatePrecedesIrreversibleSend`（`executor_confirm_test.go:183`）只对 `confirmWaitTimedOut：` 做 `strings.Contains` 子串锁，注码保注释即绿；两条 WSE2E 只覆盖 granted（`executor_ws_e2e_test.go:421`）与 stop（`:461`），没有任何一条端到端把 `confirm_wait_sec` 调到极短去触发默认支并计数 `comment_send`；`d7_gate_b20_test.go:242`、`executor_confirm_test.go:113-146` 的超时用例全部直接调 `awaitConfirmGate`，只校验返回值，穿不到 `dispatchStep`。派生写步那一格（`:1014-1027`）走独立分支，不受影响也不牵连。

**§8.3-12 · recordSubmitState 写失败即降级 + gap 上限**：找不到可存活的语义注码。`TestClassifyStepEffectThreeStates` 之外的 `LedgerWriteFailureAbortsBeforeSend` / `DegradesRestOfSession` / `SentLedgerGapStillBlocksRetryRound` / `BlockedWriteStepReasonSurvivesCanceledCtx` / `LedgerGapSetGrowsOnlyPerDistinctKey` / `TransientLedgerWriteFailureRecoversWithoutGap` 六条腿分别钉住"prepared 写失败零帧 / 整会话后续写步拒派 / sent 缺口跨 session / 已取消 ctx 下仍留因 / cap 计数只算 distinct / 抖动恢复不留缺口"，任一谓词翻转都命中其中一条。

**§8.3-13 · guardResubmit fail-close**：找不到。`TestWSE2E_GuardQueryFailureFailsClosed` 直接注入 `cancelOnFindRepo` 断言 status=failed 且 `comment_send=0`；`ledger_b16b` 的 `CrossedLedgerGapSuppressesAutoRetry` 与 `RunRetryRefusesTaskWithLedgerGap` 把 `ledgerGapHas` 短路与重试侧各自钉住；错误文案与"步判 failed、不记尝试"两侧都有断言，`return nil` 或改判 success 都立刻红。

**§8.3-16 · 三态分类，unknown ≠ none**：找不到。`TestClassifyStepEffectThreeStates` 12 项表逐格钉 `needsWriteGate`；`UnknownLocatorTableTreatedAsWrite` 钉取表报错下 click 只到线 1 次；`RegisteredPlatformNonMatchingStepStaysReadOnly` 钉"没命中 locator 仍判 none"这条反向；`UnknownEffectStepHoldsDoubleSendGate` / `RefusedAfterLedgerDegrade` / `HoldsConfirmGate` 三条按消费点分别下刀（批16c M27/M28），把 `effectUnknown` 收窄到 `effectNone` 或把 err 支默认成 `none` 都立刻命中。

**§8.3-20 · writeClaimRepo fail-close + 键不全拒 + 唯一约束**

- 承诺：三种结论里只有"占坑到手"放行；repo 返回 error（含"插 0 行却查不到持有者"那类竞态）也拒。
- 注码：`user-server/internal/browser_automation/service/write_ledger.go:389-392` 的 `if err != nil { return err }` 改成 `if err != nil { logger.Warnf(...); return nil }`。
- 预言：跨进程/并发下 `ClaimWriteSlot` 内部报错被吞，本腿照常下发，双发。
- 凭什么不红：服务层六条腿（`write_claim_gate_a12_test.go` S1/S2/S3/S5/S6）都没注 repo 错误——S1 预置 holder 走 `:393` 的 `holder != nil`；S4 用 `SetWriteClaimRepository(nil)` 走 `:386` 的 nil 支；其余走 happy path。仓储层 `TestWriteClaimIncompleteKeyRefuses` 直连 `ClaimWriteSlot` 绕过服务封装。E2E 无一条断言可观察被吞掉的错。

未验证：所有候选均未实际注入运行。
