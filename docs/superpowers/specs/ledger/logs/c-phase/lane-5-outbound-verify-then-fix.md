# C 相续刀 · lane 5：出站上界/TTL/认领预算/结论位四把二手刀成格并跑

## 为什么还有这一刀要跑

`docs/superpowers/specs/ledger/logs/b-phase/lane-5-outbound-ack-findings.md` 四条候选自陈「未实际注入运行」。
02:47–02:58 那批按四条各写/各补了腿：

- `internal/repository/message_hub_outbound_push_cap_b20d_test.go`（02:47，`TestPushCapSweepTouchesOnlyOwedOutbound` 一族）
- `internal/service/inbox_ingress_boundary_test.go:401`（02:48，`TestInboxOutboundClaimTimeoutCoversBridgeSendBudget`，
  注释自陈「杀掉的变异：把 `InboxOutboundClaimTimeout` 从 30s 砍到 5s」）
- `user-web/bridge/test/adapter-b24-send-verify.test.js`（02:48，markSent 那一格）
- `user-web/bridge/test/downlink-b20d-sentcache-ttl.test.js`（02:58，add() 那条分支）

但**四把刀在电池里的覆盖不齐**：`scripts/mut_push_budget_b20d.py`（09-22 00:24，早于上述腿）的 R7/R8
注的是「**不调** exhaustOutbound」，而 lane 5 的刀是「**调用时把 cutoff 换成 time.Now()**」＝放宽收口面，
两把刀红的是不同的格。动手前先把这句复核一遍（读 `cuts()` 与两把刀的锚点），我判错了要以盘面为准并指出。

## 四把刀 ↔ 该红的腿

| 刀 | 注码 | 该红的腿 |
|---|---|---|
| O1 | `internal/repository/message_hub_inbox_outbound.go` 收口调用实参 `cutoff` → `time.Now()`（两处调用点各自单独一刀，别并成一格） | `TestPushCapSweepTouchesOnlyOwedOutbound` |
| O2 | `internal/service/inbox_ingress_outbound.go` `30 * time.Second` → `5 * time.Second`（`InboxOutboundClaimTimeout`） | `TestInboxOutboundClaimTimeoutCoversBridgeSendBudget` |
| O3 | `user-web/bridge/src/core/downlink.js` `add()` 里删掉 `this.mem.delete(id)` 只留 `set` | `downlink-b20d-sentcache-ttl.test.js` 的 B/C 格 |
| O4 | `user-web/bridge/src/core/channel-adapter.js` 把 `this.rateLimiter.markSent(...)` 包进 `if (sendVerified)` | `adapter-b24-send-verify.test.js` 的「反向半边：平台静默吞…照常 ack」那格 |

**动手前点腿**：Go 侧 `go test -list '^Test<腿名>$' ./internal/repository/ ./internal/service/`；
JS 侧 `npx vitest run test/<文件> --reporter=basic 2>&1 | grep -E '^ ✓|^ ✗|Tests '` 点名到具体 it。
点不到 ⇒ 停下回报，不许给没跑的腿建格。

## 环境闸门

- PG 在（`pg_isready -h localhost -p 8232`）。`internal/service` 的腿连不上库会 **Skip 且整包绿**，
  所以先跑不注码的那条腿、要求看到 `--- PASS`（不是 `--- SKIP`）。口令只从 `user-server/.env`
  用 awk 精确取 `POSTGRES_PASSWORD` 喂 `POSTGRES_TEST_PASSWORD`（`.env` 有重复键，**永不打印**）。
- **本泳道撞车预告**：`internal/service/{bill,collection_job,human_task}.go` 与 `internal/system/install/`
  有别的泳道在飞（mtime 03:46–04:15，仍在动）。若 `go build/vet ./internal/service/` 报的红**不在本泳道文件里**，
  那是别人的编译红：等（复测间隔几分钟）或停下回报，**绝不去替别人改**，也不许拿这个红当「我的刀红了」。
  这条判定要写进报告（红因文件名 + 行 + 一句归属依据）。
- JS 侧：桥端测试只在 `user-web/bridge/` 里跑 `npx vitest run`，不许改夹具期望。

## 交付形态

- Go 两刀（O1/O2）：新建常驻电池 `scripts/mut_outbound_claim_r22lane.py`，**照抄
  `scripts/mut_push_budget_b20d.py` 的克隆 + overlay + 控制组 + 判格 + md5 骨架**。
  注码只在私有 `git clone --shared --no-checkout` 里发生，**绝不碰共享工作树**；
  本泳道未跟踪的新腿文件（`message_hub_outbound_push_cap_b20d_test.go` 若未跟踪）必须进 overlay 名单
  ——**这一条踩过**：新腿没进克隆，电池把「克隆里没这条腿」读成「刀存活」。
- JS 两刀（O3/O4）：照 `scripts/mut_dedupkey_shape_r22.py` 的形状新建 `scripts/mut_sentcache_r22lane.py`
  （它走的是**就地 cp 备份 + md5 还原**那一套，因为 vitest 读的是工作树）：
  每刀 `cp` 备份 → 单处精确注码 → `npx vitest run <点名文件>` → 记 rc 与 `Tests  x failed | y passed` 逐字 →
  `cp` 还原并核 md5 与备份一致。禁止 `git stash/checkout/restore`（这两个文件另有泳道动过，未提交）。
- 每格断言 `PASS+FAIL == 控制组数`；BUILD FAILED / panic / 红而没点名 ⇒ `BROKEN` 不计入杀掉；
  控制组 **放刀前现测**，不写死常量。

## 存活怎么处置

先排除取证假象（没跑 / overlay 漏 / 编译 BROKEN / `-run` 名单漂），再判真存活。
真存活 ⇒ 缺口成立，按承诺补窄腿（O1 断「界内且未超时的 inflight 行必须仍是 inflight」；
O2 已有腿则看它断言方向是否只夹上界——放大才红、压小无感就是半条腿，要把下界也钉上；
O3 断「命中已有键必须把位置挪到最新，淘汰按最后命中序」；O4 断「回查未见时 `cs.lastHash/lastSentAt` 仍被写」）。
新腿必须红过一次（注码下红、还原后绿），读数逐字进日志；然后**重跑整电池**，不是那一格。
补不动 ⇒ `refuted` 写清哪一面挡住（符号 + 文件:行），不许写「待评估」。

## 日志

`docs/superpowers/specs/ledger/logs/R22-lanes/` 下，Go 一份、JS 一份；
**文件名不许带结论**（红就 red、绿就 green）；末行必须含 `rc=`。整包全量归 E 相，本泳道不跑。

## 回报（≤400 字 + 日志路径）

四把刀各：腿名 → 注码 → 实际读数逐字 → 杀掉/存活 → 若存活：新腿文件:行 + 它红过的读数 + 还原后绿读数。
外加一段「撞车观测」：有没有看到别的泳道的编译红、怎么归因的。末行写「本轮没跑的」。
