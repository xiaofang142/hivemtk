<!-- 恢复件：C 相复验子代理 `258fa118b25` 的回报正文（取该会话最长的一条 assistant 文本）。活动窗口 09-22T18:39→09-22T18:50Z（本地 +8）；装它的任务书＝未在逐字记录里点名。正文一字未改，由主会话 09-23 05:0x 从子代理逐字记录抽出落盘。 -->

All four legs are in place, proven to have teeth, and the source files are restored byte-identical. Here is the report.

## Deliverable
Edited file: `/Users/xiaofang/Documents/www/go/hivemtk/hivemtk/user-server/internal/browser_automation/service/d7_gate_b20_test.go`
- Finding #2: two assertions added inside the existing `TestConfirmGateFramesBindPayloadAndOmitBody` (after the seq-monotonicity check).
- Findings #1, #4, #3: three new legs appended, reusing existing fixtures (`newConfirmExecutor`, `captureGateLogRepo`, `gatePayload`, `waitForGate`, `gateSessRepo`, `exportStepRepo`, `verdict`, `awaitConfirmGate`):
  - `TestAwaitConfirmGatePreviewTruncatesSubmittedText`
  - `TestAwaitConfirmGateFrameWritesRealDeadline`
  - `TestGateElsewhereFollowsNewestWaitFrame`

Each carries a Chinese comment naming the mutation it exists to kill. `gofmt -l` clean on all touched files.

## md5 (shared-worktree safety)
- Pre-injection: `executor.go = 5bb50835aa08db2918266303c367e7de`, `session.go = 91eec660ed669b2b55d6231fbdaf6b64`
- After every `cp`-backup restore the value matched exactly; final state confirmed identical to pre-injection for both.

## Per-finding readings (baseline green → mutated RED → restored)
Baseline: all four legs PASS (`ok … 1.194s`).

1. `d7PreviewRunes 200→0` — RED (rc≠0). Reason: `预览去掉标注后为空（=常量被翻成 0 的形状：只剩一个省略号）…got "…"` at test:438. The existing hand-built `&confirmGate{preview:…}` legs stayed green, confirming the constant was previously unbound. New leg drives the real `awaitConfirmGate` path.

2. Judge-frame step binding — both halves RED (rc=1):
   - `stepID→0` (executor.go:507): `judge 帧 step_id=0 必须与 wait 帧(77)同值且非零` at :221.
   - `step_index→0` (executor.go:509): `judge 帧 step_index=0 必须与 wait 帧(2)同值且非零` at :225.

3. `l.Seq > latest.Seq → <` (session.go:142) — RED (rc=1) on both heads (:523 got `no_gate` want `gate_on_another_instance`; :532 got `gate_on_another_instance` want `no_gate`). Decisive evidence: the pre-existing single-seed `TestConfirmStatusDistinguishesGateElsewhere` PASSED under the mutation, proving the flip was previously undetectable.

4. `expires_at = gate.expiresAt → time.Now().Add(24h)` (executor.go:498) — RED (rc=1) on both sub-assertions: `帧 expires_at=…2026-09-24… 不在预算到期窗口 [45s-window)` (:482) and `帧 expires_at 与闸门内存 ExpiresAt 相差 23h59m15s` (:491). The "写下的期望 AND 内存真值" contract now has teeth.

All four are genuine producer-side holes; none was caught by anything else.

## Full package (no `-run`)
`RC=0` — `ok  hivemtk-user/internal/browser_automation/service  237.230s`. No regression.

DB env used: `POSTGRES_TEST_PORT=8232`, `POSTGRES_TEST_PASSWORD` sourced via awk from `.env` (never printed); password not included in any log or this report.
