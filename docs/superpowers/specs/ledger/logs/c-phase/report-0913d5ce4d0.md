<!-- 恢复件：C 相复验子代理 `0913d5ce4d0` 的回报正文（取该会话最长的一条 assistant 文本）。活动窗口 09-22T18:40→09-22T19:00Z（本地 +8）；装它的任务书＝未在逐字记录里点名。正文一字未改，由主会话 09-23 05:0x 从子代理逐字记录抽出落盘。 -->

All three legs are in, mutation-proven, and every injected file restored. Summary:

## Path corrections (the review's coordinates were stale)
| Finding | Review said | Actually on disk |
|---|---|---|
| F1 | `internal/browser_automation/service/outbound_push_budget_b20d_test.go:228` | `user-server/internal/repository/message_hub_outbound_push_cap_b20d_test.go` (the two "teeth" legs are in `…/repository/message_hub_outbound_push_b11_test.go`) |
| F2 | `internal/browser_automation/service/sse_outbound_claim_b11_test.go:81,:280` | `user-server/internal/bridge/sse_outbound_claim_b11_test.go:81,:280` |
| F3 | `user-web/browser_automation/src/lib/downlink.js:48-53`, `constants.js:76` | `user-web/bridge/src/core/downlink.js:48-53`, `sendOutboundTimeoutMs` in `user-web/bridge/src/core/constants.js` §9 |

## F1 — `TestPushCapSweepSparesUnexpiredInflightRow` (repository)
Row at `push_attempts = MaxOutboundPushAttempts` + `status='inflight'` + `claimed_at = now()`, swept via `FetchOutboundUndelivered(claimTimeout=1h)`. Asserts (a) still `inflight`, `push_error` empty, and (b) its later `AckOutboundDeliveredBatchReturning` still matches 1 row.
- Mutation `cutoff` → `time.Now()` at `message_hub_inbox_outbound.go:71`. **All 7 prior legs PASS** (`FreshInflightIsExclusive`, `StatusNotCursor`, and the five b20d legs) → the review's "no teeth" claim reproduced exactly. New leg RED: `message_hub_outbound_push_cap_b20d_test.go:307: 正被桥端持有的到界行 status="failed" want inflight：…收口少了「认领已超时」那一半条件`.
- md5 `message_hub_inbox_outbound.go` before `9a972bd6889704015960a20d95313516` = after restore.

## F2 — `TestInboxOutboundClaimTimeoutCoversBridgeSendBudget` (service)
Lower bound: `InboxOutboundClaimTimeout >= 20s`, with the numeric source (`sendOutboundTimeoutMs = 20000`) and the reason (extension acks only after sending; a shorter window reclaims a row mid-flight → double delivery). Comment states honestly that B4's dynamic budget (cap 120s) is out of scope of this floor.
- Mutation `30s` → `5s`. `AckReclaimRace` **PASSES** (only catches increases), both bridge self-referential legs `ok` (they compare the constant against itself). New leg RED: `inbox_ingress_boundary_test.go:405: InboxOutboundClaimTimeout=5s < 桥端单次发送预算 20s：…这一行就被回收重投 ⇒ 双发`.
- md5 `inbox_ingress_outbound.go` before `659b2f3b8b55f99861282549407cfb58` = after.

## F3 — two legs (vitest), and an honest split verdict
`user-web/bridge/test/downlink-b20d-sentcache-ttl.test.js`. Note a parallel lane added an `H` leg mid-session; I renamed mine to `I`/`J` and switched to distinct channel keys (`b20di`/`b20dj`) so the module-level `caches` singleton isn't shared.

The review's own mutation ("write-back drops TTL-lapsed entries even while the server re-pushes") is **not a real defect here**, with evidence: `has()` and `evict()` read the same `sentCacheTtlMs`, so a live frame is never forgotten, and the server's re-push ceiling is `20 × 30s ≈ 10min ≪ 24h` (pinned by existing leg E). Every "forget a live entry" mutation I could express was already caught by a pre-existing leg.

But the battery found a **real, separate hole**: dropping `this.evict(now)` from `load()` (`downlink.js:51`) left **all 9 prior legs green**. So I added:
- **`I 落盘=恰好 TTL 存活集`** — pre-seeded mixed-format storage + 3 `add()` rounds; asserts write-back = exact live set, all-entries-have-ts, **ts-ascending**, and the 23h entry un-renewed. Unique kill: "落盘按时间降序写回" → only I red (this order is the precondition for `evict`'s head-prefix fast path).
- **`J 装载即回收`** — load-only round with zero sends (legacy entry forces `dirty`). Unique kill: M6 above → only J red.

Full battery (each cell = which legs went red; restored after each):
```
M1 drop load() sort(:47)        -> C, I, J
M2 evict ttl halved             -> A, I, J
M3 drop evict TTL filter        -> F, H, I, J
M4 write-back ts-descending     -> I only
M5 write-back only latest batch -> C, F, I, J
M6 drop load() evict(:51)       -> J only   <-- was SURVIVING all 9 prior legs
```
M1/M2/M3/M5 registered as redundant insurance, not new coverage (said so in the leg comments). `downlink.js` md5 before `712a0b00292f4671489a98ccb4c59fbc` = after; `constants.js` never touched (`f17a0e34…`).

## Gates run
`go vet ./internal/repository/ ./internal/service/` clean; `gofmt -l` on all 4 touched Go files: no output. `go test -p 1 -run …` on the outbound/b11/b20d set: `ok repository 10.2s`; claim/ack set: `ok service 12.3s`. `npx vitest run test/downlink-b20d-sentcache-ttl.test.js`: **10 passed**. Full `./internal/service/` package deliberately not run (merge gate covers it). Scratch battery script and `/tmp` backups removed; nothing committed.
