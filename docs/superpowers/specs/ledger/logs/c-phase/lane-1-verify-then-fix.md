# C 相 · lane 1 后续（先跑变异证实，再补腿）· `internal/browser_automation/service`

上游只读审查线扫了 §8.2-2 与 §8.3-12/13/16/20 五格，给了**两条缺口 + 三条"找不到可存活注码"**，
并自陈"所有候选均未实际注入运行"。本仓口径：二手结论入库前逐条自己重验，且结论必须跑出来。

**顺序与纪律和 `lane-4-verify-then-fix.md` 完全一致**（先 `cp` 备份 + md5 → 注码 → 真跑点名腿 →
逐字记读数 → `cp` 还原核 md5；**禁 git checkout/restore/stash**；不许为让注码红去改既有期望）。
另：本包另有泳道正在补 D7 相关的四条腿，开工前先看它是否已回报完成（未完成就等，别同包并发跑测试）。

## 1-A · §8.2-2 D7 超时支路：闸门算了结论，调用方可以不听（**主缺口**）

- 注码点：`internal/browser_automation/service/executor.go:1284-1286` `default:` 分支——
  把 `return nil, fmt.Errorf("post_comment 等待人工确认超时（%s），评论未提交", why)`
  换成 `logger.Warnf(...)` 后 **fall through**（`// confirmWaitTimedOut：` 那条注释字面保留）。
  执行流续到 `sendOnce()`（`:1313`）⇒ 真把评论提交上去。
- 我已在盘上核过的现状（`executor_confirm_test.go:181-184`）：这一格对调用点的唯一"锁"是
  `strings.Contains(src, "confirmWaitTimedOut：")` 与 `strings.Contains(src, "case confirmStoppedByUser:")`
  ——**注码保住了这两处字面量，所以子串锁照样绿**；而 `d7_gate_b20_test.go:244`、
  `executor_confirm_test.go:113-146` 的超时用例全部直调 `awaitConfirmGate` 只校验返回值，
  穿不到 `dispatchStep`；两条 WSE2E 只覆盖 granted（`executor_ws_e2e_test.go:421`）与 stop（`:461`）。
  ⇒ 形状＝"判据算得对、消费点没人管"，是典型的存活注码。这条我判断**大概率成立**，但**仍要先注码跑一遍取证**。
- 若成立要补的腿：把 `confirm_wait_sec` 调到极短（走 `stale_reconcile_test.go:76` 那种建任务口径），
  经真 `dispatchStep`/执行入口跑一遍，断三件事：
  ① `comment_send` 帧数＝0（一帧都不许下发）；
  ② 步/会话判 failed 且错误文本点名是"确认预算"还是"执行预算"到头（`why` 必须透出来，否则运维调错旋钮）；
  ③ 台账停在 `prepared`（不 sent）⇒ 可安全重下发。**③ 是反向腿**：注掉 ① 那条判据时它该红。

## 1-B · §8.3-20 `claimWriteSlot` 把 repo 报错吞成"放行"

- 注码点：`internal/browser_automation/service/write_ledger.go:389-392`
  `if err != nil { return err }` → `if err != nil { logger.Warnf(...); return nil }`。
- 上游给的凭什么不红：服务层六条腿（`write_claim_gate_a12_test.go` S1/S2/S3/S5/S6）
  S1 走 `holder != nil`（`:393`）、S4 走 `writeClaimRepo == nil`（`:386`）、其余走 happy path，
  **没有一条注入 repo 侧 error**；仓储层 `TestWriteClaimIncompleteKeyRefuses` 直连 `ClaimWriteSlot`
  绕过这层封装。⇒ 并发/跨进程下占坑报错被吞，本腿照常下发＝双发。
- 先证实：注码后跑 `write_claim_gate_a12_test.go` 全量（不 `-run` 收窄），看有没有腿红。
- 若成立要补的腿：用现成的注入面（该文件已有 `cancelOnFindRepo` 那类假 repo 形状，照抄它接 `ClaimWriteSlot` 报错），
  断 `ClaimWriteSlot` 报错 ⇒ 本腿拒发 + `comment_send==0` + 错误原文不被改写成"未接线"那种别的结论
  （文案要能区分"闸门没接线"与"占坑报错"，两条各自的腿都要有，别合成一条）。

## 1-C · 三条"找不到可存活注码"的负结论：也要跑一次才算数

`§8.3-12`（recordSubmitState 写失败即降级 + gap 上限，六条腿）、`§8.3-13`（guardResubmit fail-close）、
`§8.3-16`（三态分类 unknown ≠ none）。上游说这三格没找到可存活注码。
**负结论同样要跑出来**（本仓栽过"以为有牙、其实那两条腿都走不到判据"）：

- 每格挑上游点名的一条腿 + 它对应的那个谓词，真注一刀（例：把 `effectUnknown` 收窄成 `effectNone`、
  把 `ledgerGapHas` 短路成 false、把 `TransientLedgerWriteFailureRecoversWithoutGap` 对应的重试谓词翻一元），
  看点名腿是否红。
- 红 ⇒ 记 `refuted`（不成立），并把"哪一刀、红在哪条腿、逐字读数"一起写进台账条目——**只有结论没有读数的
  "已复核"在本仓不算复核过**。
- 不红 ⇒ 说明上游把"有牙"judgment 判错了：这一格升级为缺口，按 1-A/1-B 的口径补腿。

## 交付

每条给「注码 → 逐字读数 → 成立/不成立 → 若成立：新腿文件:行 + 注码下红过一次的真读数 + 还原后绿读数」。
最后跑 `go vet ./internal/browser_automation/service` + 该包全量（不带 `-run`），报 rc 与耗时。
不要提交 git；不要碰 `user-web/bridge/**`、`internal/service/**`、`scripts/**`（另有泳道在跑）。
