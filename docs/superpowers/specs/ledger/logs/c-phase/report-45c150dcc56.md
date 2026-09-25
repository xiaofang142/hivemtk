<!-- 恢复件：C 相复验子代理 `45c150dcc56` 的回报正文（取该会话最长的一条 assistant 文本）。活动窗口 09-22T18:42→09-22T18:53Z（本地 +8）；装它的任务书＝未在逐字记录里点名。正文一字未改，由主会话 09-23 05:0x 从子代理逐字记录抽出落盘。 -->

Done. Both mutation legs are in, each verified green-at-baseline → RED-under-mutation → restored, and both findings are confirmed real holes (no other leg in the existing suites catches them).

## J1 — `add()` 先删后设 (`downlink.js:63`)
- **Leg**: cell `H 会话内重命中续期：evict 按最后命中时间裁，落在重命中条目之后的界外尾条必须回收（杀：add 去掉先删）` in `/Users/xiaofang/Documents/www/go/hivemtk/hivemtk/user-web/bridge/test/downlink-b20d-sentcache-ttl.test.js`. Seeds 3 entries (re-hit target oldest, two out-of-window tails behind it), all far under `sentCacheMax` so only the TTL prefix loop can trim; re-hits the head entry in a clock-shifted poll and asserts it survives renewed while both tails are evicted.
- **Baseline**: green (file was 8/8 then, 9/9 now incl. a parallel lane's new cell I).
- **Mutation** (`this.mem.delete(id)` line dropped, `cp` backup + exact-string edit): RED —
  `AssertionError: expected [ 'm-rehit|c1', 'k-tail1|c1', …(1) ] to not include 'k-tail1|c1'`
  i.e. `evict`'s break stops at the in-place-renewed re-hit entry and the expired tail is never reclaimed — exactly the "eviction by insertion order" regression §8.3 row 4 promises to fix. All 7 pre-existing cells stayed green (confirmed blind spot).
- **Restore**: `downlink.js` md5 = `712a0b00292f4671489a98ccb4c59fbc`, identical before injection and after restore.

## J2 — `markSent` gated on verdict bit (`channel-adapter.js:1268`)
- **Leg**: `红线①下半句：回查未见也必须记 markSent 账 —— 第二发同文本仍被内容去重层短路（杀：markSent 被 if (sendVerified) 门控）` in `/Users/xiaofang/Documents/www/go/hivemtk/hivemtk/user-web/bridge/test/adapter-b24-send-verify.test.js`. Reuses the file's harness (swallowed `rawSendText` → `sendVerified=false`), spies `rateLimiter.markSent` (spy keeps the real impl so the dedup bookkeeping stays observable), then shifts the clock past minInterval(1500)/cooldown(3000) but inside the 60s dedup window so the only layer that can block the second same-text send is content dedup; asserts `markSent` called with `('douyin_web','acct-b24','conv-b24','在的')`, second send `rateLimited` with reason containing `dedup same text`, and `rawSendText` never ran a second time. Explicit 15s timeout guards the jitter sleeps.
- **Baseline**: green (file 8/8).
- **Mutation** (`markSent` wrapped in `if (sendVerified) { … }`): RED —
  `AssertionError: expected "markSent" to be called 1 times, but got 0 times`
  Only my new leg failed; the other 7 stayed green (the verdict bit silently becoming a second send gate goes unobserved otherwise — the missed dedup slot lets the next identical text through to a real second send, as the leg's second half is built to catch).
- **Restore**: `channel-adapter.js` md5 = `6af6de63da9ac20a83e6d8ea8ebcbab1`, identical before/after.

## Final tails (restored tree)
- `npx vitest run test/downlink-b20d-sentcache-ttl.test.js` → `Test Files 1 passed (1) / Tests 9 passed (9)`.
- `npx vitest run test/adapter-b24-send-verify.test.js` → `Tests 8 passed (8)` (my leg 1850–2402ms across runs).
- `npx eslint src/core/downlink.js src/core/channel-adapter.js` + both test files → `0 errors, 12 warnings`, all pre-existing warnings on untouched src lines; my test edits add zero.

One shared-worktree caveat worth knowing: a transient 3-failure reading of the downlink file mid-session (and one flake in a two-file combined run) was NOT my legs — `diff`/`md5` re-reads proved a parallel lane had their own mutation momentarily applied to `downlink.js` on disk during those runs; the file is byte-identical to my pre-injection backup and everything is green on the settled tree. The two src files show `M` vs HEAD only from other lanes' pre-existing work; my net change to them is zero.
