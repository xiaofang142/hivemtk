<!-- 恢复件：C 相复验子代理 `4cf67074da3` 的回报正文（取该会话最长的一条 assistant 文本）。活动窗口 09-22T18:39→09-22T18:54Z（本地 +8）；装它的任务书＝未在逐字记录里点名。正文一字未改，由主会话 09-23 05:0x 从子代理逐字记录抽出落盘。 -->

Done. All work is complete, the production file is byte-for-byte restored, and the full package is green.

## What was done

Added **one new test file** (never edited the shared `retention_a6_test.go`, to avoid collisions with parallel lanes), reusing its package-level harness (`a6DB / a6Row / a6Seed / a6SeedFrozen / a6Digests / a6RunRows / a6CountLog / a6Prune / a6Cutoff / a6FrozenStamp` and constants).

`/Users/xiaofang/Documents/www/go/hivemtk/hivemtk/user-server/internal/browser_automation/repository/retention_a6_chain_prune_test.go` — four legs, each headed by a Chinese comment naming the mutation it kills:

- `TestA6ChainLinksToNewestDigestAcrossThreeRounds` — three prune rounds on one session; kills **#1** (`ordinal DESC`→ASC) and **#3** (`prevChain = prev.ChainHash`→`prev.BatchDigest`). Pins round‑3 `prev_chain_hash` to round‑2's stored `chain_hash` (≠ its `batch_digest`, ≠ round‑1's `chain_hash`).
- `TestA6ChainHashComposesPrevWithBatch` — two rounds (non-empty prev); kills **#2** (`ChainHash = auditChainHash(...)`→`= w.BatchDigest`). Asserts `chain_hash != batch_digest` and `chain_hash == sha256(prev‖batch)`.
- `TestA6PruneFailsCloseWhenDeleteFailsMidRun` — an `ENABLE ALWAYS` row trigger blocking only session B's DELETE; kills **#4** (`tx.Create(&d)`→`r.db...Create(&d)`). Asserts NO digest survives the rolled-back delete-transaction, plus a reverse-lock.
- `TestA6RunRowClosesArithmeticUnderTwoBatches` — 2×5000+1 interleaved rows; pins `rows_before == rows_pruned + rows_left` (documents why **#5** is a non-hole).

## Mutation-injection results (each: backup → Edit → run → restore → md5)

command_log.go md5, pre-injection and after every restore: **`fc9fe4469ffb73a15f0686aa2838ecd9`** (verified equal 5×).

| # | line mutated | md5 while injected | RED? | red reason (intended) |
|---|---|---|---|---|
| 1 | `ordinal DESC`→ASC (:121) | `239fe7378572e91fac9859c6983a5a70` | yes | round 3 links to oldest → ordinal regresses → `duplicate key ... uk_browser_audit_digests_session_ordinal` → batch rolls back |
| 2 | `ChainHash=auditChainHash`→`=w.BatchDigest` (:205) | `4c629bcec3c00b041b2d96b0ceaadffa` | yes | `chain_hash == batch_digest` + `!= sha256(prev‖batch)` (64-hex values prove not a column-width artifact) |
| 3 | `prev.ChainHash`→`prev.BatchDigest` (:194) | `135de5f95c64cf7a7f9213f28a156bbd` | yes | round 2 & 3 `prev_chain_hash != 上一条 chain_hash` (read batch_digest instead) |
| 4 | `tx.Create(&d)`→`r.db...Create(&d)` (:208) | `560eb97c0bfd72ee145b93b843939c63` | yes | delete-transaction rollback leaves **2 digests** (want 0); reverse-lock sees 4 (want 2) |

Each red was a genuine assertion/logic consequence, not a compile or DB/env error. (Existing `TestA6ChainLinksAcrossRuns` also goes red under #2/#3 — secondary coverage.)

## Finding #5 — not a real hole (honest conclusion)

`RowsBefore: rowsBefore` → `total` is **unobservable**. Injected it (md5 `2b81af6dcb97856fb5c8fd1fbb56e148`) and ran the **full package: 0 failures, `ok`**. Reasoning: the run row is only written after the batch loop runs to exhaustion, so every row counted at entry (`created_at<cutoff AND id<=highwater`) is deleted by that same run ⇒ `total == rowsBefore` on every write path. Any mid-run divergence (a `RowsAffected != RowCount` mismatch, an aborted digest insert, or a partial delete) makes `PruneBefore` `return total, err` **before** the run row is created — so `total` and `rowsBefore` can never differ in a committed run row, and the `id<=highwater` cap doesn't help (every expired row has id ≤ that max by construction). My two-batch fixture confirms `rows_before == rows_pruned == 10001`, staying green under the mutation. No tautological assertion was invented.

## Final baseline run (production file restored)

`go test -count=1 -timeout 40m ./internal/browser_automation/repository/` → `ok hivemtk-user/internal/browser_automation/repository 23.057s`.
