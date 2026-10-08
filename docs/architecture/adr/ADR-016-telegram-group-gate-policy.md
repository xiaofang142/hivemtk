# ADR-016：Telegram 社群门控的三项处置口径

| 字段 | 内容 |
|------|------|
| 编号 | ADR-016 |
| 标题 | Telegram 社群门控的三项处置口径（提示送达状态、到期处置、多实例选主与记账）|
| 状态 | ✅ Accepted（已落地，代码与本档同批）|
| 决策者 | @maintainer-team（"是否对真人移出发"这条属渠道运营口径，已由业务方确认）|
| 日期 | 2026-09-29 |
| 适用范围 | `user-server` Telegram 群门控（`telegram_group_gates` / `telegram_group_members`）、后台任务跨进程租约（`cron_job_leases`）|
| **关联排查** | `docs/architecture/CHANNEL_INTEGRATION_AUDIT_2026-09.md`（渠道审计主线）、`docs/TROUBLESHOOTING.md` §4.4 |

## 背景

门控群有两种模式：`mute_unlock`（入群即禁言，验证后解禁）与 `join_request`（加群申请需审批）。
线上一次"社群里没人回复"的排障把这条链路整个翻开后，留下三个**没有口径**的问题——
代码在跑，但它对真人的后果没人定过：

1. **入群提示被无界重播**。补偿循环原来把"`expires_at` 快到了"当作"提示大概没送达"的代理判据，
   于是每个验证窗口都给同一个人重播一次群消息；而重播顺带把 `expires_at` 顶回去，
   导致 TTL 到期清理对这些人**永不触发**。实测 6 名成员被重播 219 条群消息。
   同族还有另一条形状，跟"代理判据"无关：**单实例内**入群请求内路径与补偿循环抢同一个人
   （一次 TG 往返 8–10 秒的间隙里，那一行在台账上正好是"从未送达 + 窗口开着"），
   实测同一次入群收到三条提示、两次 `restrictChatMember` 相隔 8 秒——拦这一条的判据见决策一第 5 条。
2. **TTL 到期未验证的人怎么处置**。原实现直接 `banChatMember` 移出——上面那条"永不触发"
   把它掩盖住了，一旦把重复播报修掉，第一次真正开始对真人动手的就是它。
3. **同一套库上多个进程各跑各的清扫**。本地开发形态下常见"正式实例 + 热重载实例 + 手工起的旧二进制"
   同时连着同一个 PostgreSQL；清扫器没有跨进程互斥，重复播报量就是实例数的整数倍。

三项的共同点：**它们都不是崩溃型缺陷，而是"没人对真人后果负责"的策略缺位**。所以在这里逐条定成决策，
而不是散落在函数注释里。

## 决策

### 决策一：「提示是否送达」与「验证窗口是否到期」是两个独立事实，不许互为代理

台账 `telegram_group_members` 增加两列（`internal/model/telegram_gate.go:62-63`）：

| 列 | 含义 | 写入点 |
|----|------|--------|
| `welcome_sent_at` | 提示**最近一次确认送达**的时刻；`NULL` = 从未送达 | `MarkWelcomeSent`（`internal/repository/telegram_gate.go:119`），只在 TG API 回 `ok` 之后 |
| `welcome_resends` | 补发尝试次数，送达后清零 | 请求内路径 `BumpWelcomeResend`（`:126`）调用点 2 处：`internal/service/telegram_gate.go:267`（审批模式私聊未达）、`:337`（入群提示发送失败）；补偿路径不再事后 bump，改由原子认领 `ClaimStalledResend`（`internal/repository/telegram_gate.go:145`）在同一笔 UPDATE 里把本次尝试计进去——先认领再动手，抢不到的人连一次 TG 调用都不会发 |

配套的五条约束（缺一即回到"用代理判据猜送达状态"的原状）：

- 补偿取数判据是 `join_status = restricted AND authorized = false AND welcome_sent_at IS NULL
  AND (expires_at IS NULL OR expires_at > now) AND (welcome_resends > 0 OR updated_at IS NULL OR updated_at <= idleBefore)`
  （`ListStalledRestricted`，`internal/repository/telegram_gate.go:231`，判据在 `:234-235`）。
  **`welcome_sent_at IS NULL` 是必要条件**；"窗口还开着"只是避免与到期路径抢同一个人。
- 上面最后那组括号是**归属判据**：入群请求内路径还握着这一行时，补偿循环不许碰它
  （安静窗口 `tgGateInFlightQuiet = 2 * gateSweeperInterval`，`internal/service/telegram_gate.go:716`；调用点 `:744`）。
  `HandleNewMembers`（`:273`）从写台账（`:312`）到本次尝试收尾（`:337` 计失败 / `:340` 登记送达）之间夹两次 TG 往返
  （实测 8–10 秒，上界受渠道层 30 秒超时约束到 60 秒），那段时间这一行**同样**满足"从未送达 + 窗口开着"；
  每分钟一跳的清扫器落进去就是给同一个人第二次禁言 + 第二次播报（线上实测同一次入群收到三条提示）。
  归属信号取 `updated_at`：写台账与每一笔收尾写入都会把它顶到现在，所以"早于 idleBefore"＝这一行已经没人管了。
  `welcome_resends > 0` 单独放行——那次 +1 是请求内路径在这一行上的**最后一笔**写入，看见它就等于看见"这次尝试已失败并交棒"，
  当轮就该补，否则安静窗口比 `verify_ttl_min` 还长的群会整批丢掉补偿能力。
  **这一条只延迟、不取消补偿**：不是"年轻就永久跳过"（跨过窗口照样补）、`updated_at IS NULL` 的行（手工/迁移写入）
  视为早已安静照补、补发上限一列没动。
- 补发上限 3 次（`tgGateWelcomeResendMax`，`internal/service/telegram_gate.go:701`）。
  发不出去一般不是网络抖动而是 Bot 在这个群里没了发言权限，对这种成员继续重发只会刷屏；
  到上限后**只停手**——不改计时，让 `expires_at` 自然到期。
- `retimeGateWindow`（`internal/service/telegram_gate.go:809`）只允许在"这一轮补偿真把提示发出去"之后调用，
  倒计时从被告知的时刻起算才成立。除这一处外，代码里不存在任何"重新计时"的路径——
  无限续期正是 TTL 清理永不触发的原因。
- `HandleNewMembers` 里发送失败必须**留下未送达的痕迹**（计数 +1、`welcome_sent_at` 保持 `NULL`）并跳过本轮，
  而不是照原样把行登记成已送达。

### 决策二：到期处置 = 送达过才罚 + 再宽限一个 TTL + 移出但保留可重入

`SweepExpired`（`internal/service/telegram_gate.go:828`）按顺序三条判据，前两条只用于 `mute_unlock`：

| 序 | 判据 | 处置 |
|----|------|------|
| 0 | 处置前按**主键**重读台账那一行（`GetMemberByID`）；读不出、或 `authorized` 已成立 | 本轮跳过（判据只能来自重读出的那一行，不拿二手数据对真人动手；重读的键不能选 `verify_token` 这种会被重新入群改写的业务列）|
| 1 | `welcome_sent_at IS NULL` | **不处置**（Debugf 留痕）。没被告知的人超时不是他的过错 |
| 2 | 送达过，但 `now - expires_at < verify_ttl_min` | 再宽限一个 TTL。"群里点链接 → 跳私聊 → 发 `/start`"这条链路本来就慢，一次窗口没点不等于不想入群 |
| 3 | 宽限也过了 | `mute_unlock`：`banChatMember(untilDate = now-1min)` 后立即 `unbanChatMember`，台账写 `kicked`；`join_request`：`declineChatJoinRequest` |

`join_request` 不受 1、2 两层约束，因为 `decline` 只把申请退回"可以再次申请"，人既没被移出也没被拉黑。

两个容易写错、且写错就会伤到真人的细节：

- **`banChatMember(untilDate=0)` 不是"只踢一次"**。Telegram 会把它归化成约 365 天的封禁；
  过去时间戳（此处取 `now-1min`）才是"踢出但不拉黑"，随后补一次 `unbanChatMember` 解掉残留封禁位，
  才得到"可再次入群"的语义。
- **ban 失败 ⇒ 本轮不写 `kicked`**（直接 `continue`，下轮重试）。把没发生的处置记成事实，
  代价是台账与群内真实状态永久错位。
- `kicked` 的回写走窄更新 `MarkKicked`（`internal/repository/telegram_gate.go:168`，
  `WHERE id = ? AND authorized = false`）：只改状态列，不碰送达/计时列，也不给已放行的人补一刀。

**否决的备选**：

- *"永久静音、只在入群时提示一次"*：省掉了对真人动手，但静音成员在群内不可观测、
  会积累一批"看起来像机器人"的哑成员，而门控存在的意义就是筛掉它们。
- *"到期即移出，不要求送达过"*：会系统性踢掉从未收到提示的人（Bot 失去群内发言权限时是整批误伤）。
  业务方报过的"有人加入但是又被踢掉了"正是这一型的现场形态，故列为不可接受。

### 决策三：门控广播不进 `message_hub`；会对真人动手的后台任务按 `cron_job_leases` 选主

**记账口径**：禁言与入群提示属**群运营事件**，不是"客服代表客户发出的一条回复"。
`message_hub` 的出站行数被直接当业务指标用（`internal/repository/growth_repo.go:211`：
`SELECT '发送消息', COUNT(*) FROM message_hub WHERE direction='outbound'`）。
门控广播若落库，上面那 219 条重复播报会立刻变成客户可见的"发送消息"数，
且客服收件箱会被入群提示淹没。⇒ 门控广播只在 `telegram_group_members` 留送达状态，不落 `message_hub`。

**选主口径**：新增 `cron_job_leases`（`job_name` 主键、`owner`、`heartbeat_at`），
仓库 `internal/repository/cron_job_lease.go`：

| 方法 | 语义 |
|------|------|
| `Hold(job, worker)` | 原子抢/续：先 `INSERT ... ON CONFLICT DO NOTHING`，再带陈旧条件 `UPDATE`，**以 `RowsAffected == 1` 为决胜点**（插入成败不能当判据——两个进程同时首次抢占必有一方插入失败，但胜者是那一行 UPDATE 的持有者）|
| `Release(job, worker)` | 只清自己名下那一行，别的进程已接管时不会误清 |
| `Holder(job)` | 读回持有者与心跳，排查用 |

心跳陈旧窗口 `cronLeaseStaleAfter = 3 * time.Minute`（`:18`）配 1 分钟的清扫节拍：
一次网络抖动/慢查询最多让出一拍，而进程真死后最多 3 分钟就有人接上。
非持有者本轮**零 TG 调用**（`sweepOnce`，`internal/service/telegram_gate.go:946`）。
`StopGateSweeper`（`:1059`）**先取消 ticker 并等协程真退出，再释放租约**——顺序反了会留下
"本进程还在动手、下一任已抢到租约"的双跑窗；等不到退出就不释放，让租约自然过期（宁可让下一任多等一轮）。

装配与关停成对：`internal/router/router.go:736` 启动、`cmd/api/main.go:493` 的
`defer service.StopGateSweeper(context.Background())` 登记位置在持久层 Close **之后**，
LIFO 下它先执行，释放语句仍进得了库（`cmd/api/startup_order_test.go:213 TestGateSweeperPairedOnShutdown` 钉这个形状）。

**这把互斥不覆盖单实例内的抢跑**：入群请求内路径（`HandleNewMembers`）压根不参与租约——它不 `Hold` 也不看心跳，
租约只在清扫器那一侧生效。所以"同一进程里 webhook 请求与补偿循环抢同一个人"这一族，租约一条都拦不住，
只能靠决策一的归属判据；别拿 `cron_job_leases` 当那一条的答案。

**否决的备选**：

- *Redis 锁*：本地开发模式与 CI 不保证 Redis 可用，而既有 polling 互斥已经是 DB 行锁形状（同一判据来源好排查）。
- *配置里指定"哪台跑"*：部署形态一变就得改配置，且持锁进程死后没人接管。
- *给所有 cron 都加租约*：本轮只对"会对真人/外部世界动手"的清扫器加。只写自己聚合行的任务
  （小时汇总等）重复执行不外发，加了只是多一张锁的运维面。将来出现新的外发型定时任务时复用这张表，
  `job_name` 用作业名，键名进本档。

## 落点（代码坐标）

| 面 | 位置 |
|----|------|
| 送达状态列 | `internal/model/telegram_gate.go:62-63` |
| 租约表模型 / 注册 | `internal/model/cron_job_lease.go`、`internal/pkg/db/migrate.go:112`（AutoMigrate，无手工迁移脚本）|
| 租约仓库 | `internal/repository/cron_job_lease.go:18`（陈旧窗口）`:50`（`Hold`）`:77`（`Release`）`:87`（`Holder`）|
| 成员仓库 | `internal/repository/telegram_gate.go:119`（`MarkWelcomeSent`）`:126`（`BumpWelcomeResend`）`:145`（`ClaimStalledResend`）`:157`（`RetimeVerification`）`:168`（`MarkKicked`）`:192`（`ListExpired`，判据 `:194`）`:231`（`ListStalledRestricted`，判据含归属窗口在 `:234-235`）`:267`（`GetMemberByID`，主键重读）|
| 门控服务：送达登记 | `internal/service/telegram_gate.go:265`（审批模式私聊送达）`:273`（`HandleNewMembers`：台账 `:312` → 禁言 `:321` → 发送 `:331` → 失败计数 `:337` / 送达 `:340`）|
| 门控服务：补偿/到期 | `:701`（补发上限）`:716`（安静窗口 `tgGateInFlightQuiet`）`:737`（`RecoverStalled`：取数 `:744`、认领 `:777`、送达+计时 `:800-801`）`:809`（`retimeGateWindow`）`:828`（`SweepExpired`）`:903`（TTL 兜底 10 分钟）|
| 门控服务：清扫器与租约 | `:916-921`（节拍 1 分钟 / 单轮 30 秒 / 批量 100 / 作业名 `telegram_gate_sweeper`）`:946`（`sweepOnce`）`:970`（`StartGateSweeper`）`:987`（`startGateSweeperAt`，重复注册幂等）`:1000`（`tickLoop`，panic 自愈重启）`:1042`（`restartAfterPanic`，关停中不再重启）`:1059`（`StopGateSweeper`）|

## 验证

每条判据都有点名用例，改坏对应分支就会红：

| 决策 | 用例 |
|------|------|
| 一（送达/计时分轴、上限、失败留痕、归属窗口）| `internal/service/telegram_gate_recover_stalled_test.go`：`TestRecoverStalledNeverRebroadcastsDeliveredWelcome`、`TestRecoverStalledResendsOnlyWhenNeverDelivered`、`TestRecoverStalledCountsFailedAttemptsAndStopsAtCap`、`TestHandleNewMembersFailedSendStaysUndelivered`、`TestHandleNewMembersRecordsDeliveryOutcome`、`TestHandleNewMembersReJoinResetsWelcomeState`（第三腿：重新入群且本次发送失败 ⇒ 上一段的送达与计数都不许继承）；`internal/service/telegram_gate_in_flight_test.go`：`TestRecoverStalledSkipsRowStillInsideInRequestWindow`（请求内路径还握着 ⇒ 一次 TG 调用都不许发）、`TestRecoverStalledResendsAfterInRequestWindowPasses`（同一行跨过窗口后当轮补发并登记送达——证明这一条只是延迟不是取消）、`TestRecoverStalledResendsImmediatelyAfterRequestPathHandedOff`（`welcome_resends>0` 交棒 ⇒ 不等窗口就补）、`TestRecoverStalledStillPicksRowsWithoutUpdatedAt`（NULL 时刻照补）；`internal/repository/telegram_gate_repo_test.go`：`:25 TestTelegramMember_ListStalledRestricted`（含"未送达但窗口已走完"这一枚非命中；`idleBefore` 那一列在本矩阵里被刻意中和，归属判据单独由下一份文件守）、`:99 TestTelegramMember_UpsertResetsWelcomeState`、`:141 TestTelegramMember_WelcomeStateGuards`、`:242 TestTelegramMember_ClaimStalledResend`；`internal/repository/telegram_gate_stalled_idle_test.go`：`:25 TestTelegramMember_ListStalledRestrictedIdleWindow`（七枚种子逐分支：握着 / 已过期但年轻 / 安静 / 交棒 / 无时刻 / 已送达）|

归属判据落地后，补偿类用例的种子必须显式表态"这一行已经没人管了"：`gateMakeRowIdle`
（`internal/service/telegram_gate_in_flight_test.go:33`，把 `updated_at` 用 `UpdateColumn` 推到安静窗口之外，
绕开 `autoUpdateTime`）就是这一句表态，`telegram_gate_recover_stalled_test.go` 的两枚补偿用例都调它。
新写的补偿用例若不调它，拿到的读数是"本轮没补发"，而那正是被跳过的那一行的表现——不是判据生效。
| 二（三层到期判据、可重入、失败不写状态）| `internal/service/telegram_gate_sweep_test.go`：`TestSweepExpiredLeavesNeverNotifiedMember`、`TestSweepExpiredGivesOneTTLGrace`、`TestSweepExpiredRemovesAfterGraceAndKeepsRejoinOpen`（同时断 `banChatMember` 与 `unbanChatMember` 各 1 次）、`TestSweepExpiredDeclinesJoinRequest`、`TestSweepExpiredKeepsRetryWhenBanFails`、`TestSweepExpiredSkipsAuthorizedMember`；`internal/service/telegram_gate_test.go:256 TestSweepExpiredFilter`（五条种子两枚命中；非命中里
"窗口未到期"与"`authorized` 已成立但 `join_status` 仍 `restricted`"各隔离一条谓词——后者专治
`ListExpired` 的授权位条件被改坏：授权位与入群状态是两列，判据也得各管各的）|
| 三（租约选主、零动作、成对关停）| `internal/repository/cron_job_lease_test.go`：`TestCronJobLease_HoldAndRelease`、`TestCronJobLease_RejectsEmptyArgs`；`internal/service/telegram_gate_lease_test.go`：`TestSweepOnceRunsOnlyForLeaseHolder`、`TestGateSweeperTickLoopTakesAndReleasesLease`、`TestStartGateSweeperIdempotentAndStopSafe`、`TestGateSweeperSelfHealsAfterTickPanic`（tick 体里制造 panic ⇒ 断"制造那台真退出过"（`done` 关闭）再看复活，否则"另起一台"也能长得一样）、`TestSweeperPanicRestartSkippedDuringShutdown`（关停中崩溃不再补起一台）；`cmd/api/startup_order_test.go:213 TestGateSweeperPairedOnShutdown` |

节拍做成参数（`startGateSweeperAt`）只有一个原因：一分钟一次的线上节拍进不了用例，
而"注册了协程 ⇒ 它自己抢租约、真跑一轮清扫、`Stop` 时把租约交回"这一整条只能在毫秒节拍上观测到。

### 逐刀杀伤（变异电池）

上面那张表只回答"用例存在"，不回答"用例有牙"——本档三项口径全是**删掉判据不会有任何东西红**的那一类
（它不崩、不报错，只是对真人多动手一次）。常驻驱动 `scripts/mut_tg_gate_policy.py`：19 刀各改坏一条判据，
每刀在影子 `--shared` 克隆里重跑门控用例，活树一刀不碰。

| 读数 | 值 |
|------|----|
| 控制组 | `rc=0 pass=24 fail=0 skip=0 ran=24/24`（装架前当场测，不写死常量——共享树上别人加用例会让计数漂）|
| 杀伤 | `KILLED=17`、`KILLED-PANIC=1`（M04）、`DECLARED-SURVIVOR=1`（M18）、`SURVIVED=0`、`BROKEN=0` |
| 复原 | `RESTORE-LEFT=0`，14 份泳道文件逐文件 md5 对账 |
| 每刀自检 | 断言 `PASS+FAIL+SKIP == 控制组数`，红因写进该刀的日志 |
| 取证 | `docs/superpowers/specs/ledger/logs/TGGATE/20260929-124222/`（22 份＝控制组 + 19 刀 + tally + 驱动）|

**读数所属轮次**：上表是 12:42 那一轮（19 刀 / 24 用例 / 14 份泳道文件）的数。驱动现在已装到 **26 刀 / 30 用例 / 16 份泳道文件**
（认领谓词 M20-M22 与安静窗口 M23-M26 是那一轮之后增补的），这 7 刀**没有跑过完整一轮**，所以只有"锚点在交付字节上
恰好命中一次 + 影子树逐字同步"这一条是本轮实测（`--check`），杀伤读数仍是上一轮的 ⇒ 新装 7 刀 **未验证**。
M23-M26 的期望红因已经在这批代码的手工反向测试里量到（删掉归属判据 ⇒ 三枚用例各自红，见 `docs/TROUBLESHOOTING.md` §4.4 末段），
所以这里不是"凭空登记"，是"电池没重跑"。

两枚读数需要单独说明：

- **M04（去掉"未送达不罚"那道守卫）的杀是 panic 带走的**，`ran=18/24`：摘掉守卫后清扫器走到
  `fresh.WelcomeSentAt.Format(...)`（`:892`）对 `nil` 取值即崩。**不给这个状态补防御性判空**——
  崩得响，比静默把"从未被告知的人"移出去有用；这一刀同时证明那道守卫是承重的。
- **M18（去掉 `SweepExpired` 里对 `fresh.Authorized` 的跳过）单线程测不到**：`ListExpired` 已带
  `authorized = false` 谓词，被摘掉的那道守卫只有在"取数与处置之间，另一次 `/start` 放行落了库"
  这种跨进程竞态里才可能开火。它是纵深防御而非主判据，故登记 `DECLARED-SURVIVOR`（理由写进驱动里），
  不为好看补一枚假用例；主判据那一面由 M19（`ListExpired` 的授权谓词变恒真）杀到 `TestSweepExpiredFilter`。

## 后果

- **存量成员**：`welcome_sent_at` 新列对历史行取 `NULL`，因此 `mute_unlock` 群的历史行按判据 1 一律"不处置"，
  直到补偿循环真把提示发出去一次才开始计时；已经在群里被无限重播过的人从此不会再被重播。
  `join_request` 群的历史行不受这层保护（到期即 `decline`），因为 decline 只把申请退回"可以再次申请"，人没被移出也没被拉黑。
  若某个人已被上一版代码"送达 + 早已过期"，升级后第一轮清扫会先进宽限期、宽限过了才被移出；
  想保留这类成员就手工把 `expires_at` 推到未来或直接把状态改成 `approved`（见 `docs/TROUBLESHOOTING.md` §4.4）。
- **指标面不受污染**：门控广播不落 `message_hub`，"发送消息"指标继续只统计真实外发回复。
- **重复播报的第二层成因（多实例）被独立封住**：即使有人手工再起一个实例，
  非持有者那一台每轮零动作，日志只留一行 Debugf。
- **重复播报的第三层成因（单实例内抢跑）由归属判据封住**：请求内路径与补偿循环在同一进程里也会抢同一个人，
  代价是补偿最坏多等 2 个清扫节拍（`tgGateInFlightQuiet`）。这条路只在"请求内路径一笔收尾都没留下就断了"
  （进程被 `SIGKILL`、panic 在发送之前）时才真的要等满窗口；留下失败计数的那类当轮就补。
- **`updated_at` 从此兼任归属信号**：任何新加的写入路径（补列、手工修数据、后台刷新）都会把这一行的
  `updated_at` 顶到现在，从而把它的补偿推后最多一个安静窗口。加写路径的人要么接受这段延迟，
  要么同时把 `welcome_resends` 留下非零值（`> 0` 单独放行，等于显式交棒）。
- **运维可见性**：`SELECT * FROM cron_job_leases` 直接答"这一路现在在谁名下、心跳什么时候"。
- **未覆盖面**（不得当作已清）：门控广播不进 `message_hub` ⇒ 群内运营事件目前**没有**跨渠道统一台账，
  要按渠道看入群/验证转化只能读 `telegram_group_members`；本档不把它当成已解决的通用记账问题。
- **`SweepExpired` 里"重读后 `authorized` 已成立 ⇒ 跳过"那道守卫没有用例覆盖**（电池 M18 登记为
  `DECLARED-SURVIVOR`）：它只在跨进程竞态里开火，单线程夹具造不出那个时序。也就是说
  "本进程读到过期行 → 另一进程恰好在此刻放行同一人"这条窗**未测**；防线在（`internal/service/telegram_gate.go:847`），
  判据来自 `ListExpired` 的授权谓词（M19 已杀到用例），但竞态本身没被复现过。
- **租约的接管延迟上界是 3 分钟**（`cronLeaseStaleAfter`）：持有者被 `kill -9` 时不释放租约，
  下一任要等心跳陈旧才接上。这段窗口里没人清扫，属设计内（宁可少动一轮，不可双人动手）。

## 修订历史

| 版本 | 日期 | 修订人 | 内容 |
|------|------|--------|------|
| v1.0 | 2026-09-29 | @maintainer-team | 首次把社群门控的三项悬置口径定成决策：送达状态与验证计时分轴、到期处置三层判据（送达过才罚 + 宽限一个 TTL + 可重入移出）、门控广播不落 `message_hub` 与清扫器按 `cron_job_leases` 选主；记录否决的备选与未覆盖面 |
| v1.1 | 2026-09-29 | @maintainer-team | 补逐刀杀伤读数（19 刀：`KILLED=17`／`KILLED-PANIC=1`／`DECLARED-SURVIVOR=1`，控制组 24/24，`RESTORE-LEFT=0`）；清扫器处置前的重读从 `verify_token` 改为**主键** `GetMemberByID`（`verify_token` 是会被重新入群改写的业务列，不能当"还是不是同一行"的依据）；`ListExpired` 的授权谓词与重新入群重置送达状态各补一枚独立判据用例；订正本档代码坐标（源码在补守卫后有位移）|
| v1.2 | 2026-09-29 | @maintainer-team | 决策一新增第 5 条约束（补偿的**归属判据**）：`ListStalledRestricted` 增加 `(welcome_resends > 0 OR updated_at IS NULL OR updated_at <= idleBefore)`，请求内路径还握着的那一行不再被补偿循环抢走——线上"同一次入群收到三条提示"的最后一条活路径；配套 `tgGateInFlightQuiet`、4 枚 service 用例与 1 枚 repository 用例、变异电池 M23-M26；`welcome_resends` 的写入点从"4 处 bump"更正为"2 处 bump + 补偿走原子认领"（认领是上一批代码改的，本档当时没跟上）；按最后一次源码编辑全量重算本档 file:line |
