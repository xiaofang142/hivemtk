# 浏览器自动化：不可逆写操作台账与假绿收口 —— 设计（批6–批12）

日期：2026-09-19 ｜ 路径：Architectural（brainstorming 已批准，争议项按用户授权「自动执行」由本设计直接拍板）
前置：`2026-09-19-browser-automation-optimization-design.md`（批0–批5g 已落，本地 commit `ced76b9b`）

## 1. 问题（全部真机跑出来的，非推断）

| # | 事实 | 证据 |
|---|---|---|
| F11b | **零提交也判 verified=true**。`injPostCommentVerify` 的容器主分支 `norm(textOf(c)).includes(want)` 不排除输入节点：输入框在评论区容器内（小红书真实 DOM 形态）时，未提交草稿被当成已发布评论 | 常驻反向腿 `/tmp/hivemtk_leg_x.py`：`status=completed`、带外 ledger `post=0 / swallowed=1`、`extracted_data.verified=true`、`evidence={containers:1}`。导出的审计包里带着这条假 `verified:true` |
| F-N1 | **D7 确认超时的任务永久砖化**。`execCtx` 预算=`TimeoutSec`（`task.go:344`），确认等待也吃这个预算；ctx 一死，`feedback.OnSessionFinished(ctx,…)` 用已 Done 的 ctx 写 `browser_tasks`，DB 驱动取消（日志实锤「更新任务快照失败 … context deadline exceeded」），任务行停在 `running` → `status=="running"` 的四道门（edit/delete/run/幂等）全锁死，重试与失败邮件静默丢失，且**全仓无任何对账路径**（`FindRunning` 零调用者） | session 335 + 腿2（timeoutSec=15）+  bricks 任务 278/279/280/283 |
| F-N2 | **控制台错误面整体失效**。全局 i18n composer 在 `dropMessageCompiler:true` 下对 100% 现存 key 抛 `UNEXPECTED_RETURN_TYPE`（zh.json 叶子 1834 个 → 成功 0 / 抛错 1834，dev 8211 与 prod 8212 一致）。`request.js:172` 的 `t('http.requestFailed')` 落在 catch 之前 → 每条 API 错误都渲染成 toast「SyntaxError」（空 message）。叠加 `List.vue:108-121` 判 `err.code===409`（`buildRequestError` 从不设 `code`）与「未连接」文案，Host 未连接 / 用户忙 / 真失败三者不可辨 | `/tmp/hivemtk_err409_leg.mjs`：`xhr 409 DUPLICATE_ENTRY_3003` → `界面反馈 {"msgs":["SyntaxError"]}` → `new SyntaxError :: (empty)` stack in `i18n-*.js` |
| F-N3 | 执行成功后无监控入口（`onRun` 只弹「已开始执行」），D7 挂起最长 3600s 期间用户看不到倒计时，只能回列表点详情找会话 | 真机 UI 腿观察项 |
| 其余 | nm-host 无帧上限/单泵/token 进 stderr；`refs` 全局桶跨 tab 串；`click_unacked` 落 DOM fallback（可能二次点击）；B 链路 `contentHash` 作为幂等键跨通道撞车；契约测试靠 `strings.Contains` 断错误文案 | 批 9–批 11 逐条处置 |

## 2. 三处争议决策（用户授权自动拍板）

**决策 1 — 写台账落在 `browser_steps` 新列，不建新表。**
候选是「新表 `browser_write_ledger`」vs「扩 `browser_steps`」。选后者，三条理由：
① 新表必须改 `internal/pkg/db/migrate.go`，该文件属并行会话（本轮已多次撞车），任何方案只要碰它就是把自己绑在别人的发布节奏上；`browser_steps` 已是 `allModels()` 成员，`AutoMigrate` 自动加可空列，**零迁移文件**。
② `browser_steps` 天然带 `task_id + step_index + action + value`，即台账需要的自然键；不新增长事务。
③ 保留策略已核实：`retention.go` 只清 `command_log` 行与 `llm_plans` 快照文本，**不 prune `browser_steps`** → 台账天然长期可查。
代价：step 行一次执行一行（跨 session），判「这条文本历史上提交过没有」是跨行查询而非单行读。**实现口径**：不建 `(task_id, step_index, text_hash)` 复合索引，改用 `submit_state` / `text_hash` 两个单列索引——查询的驱动条件恒为 `task_id`（`BrowserStep.TaskID` 本就带索引），单任务步数量级为个位数，复合索引的收益不足以抵它需要的手写迁移（同决策 1 ① 的理由）。见 §3.1。

**决策 2 — D7 确认预算与机器执行预算解耦，UI 上限 900s。**
候选：把 `TimeoutSec` 整体调大 / 确认等待不计预算 / 新增 `confirm_wait_sec`。选新增列：`TimeoutSec` 的语义是「机器跑一步/跑一轮的墙钟」，把它撑大到 3600s 等于放弃僵尸任务检测（A5 连续命令超时、watchdog 全以它为基准）；「不计预算」则确认挂起 + 执行时间可能远超任务名义时长，用户看到的 120s 变成谎言。显式两列 = 两个可解释的计时器。上限 900s：超过 15 分钟的挂起在实践中等价于「人不会来」，不如让它失败并释放串行闸（F8 是每用户 1 个 running session，挂起=锁死整个 Host）。

**决策 3 — 错误面在 `request.js`/`List.vue` 内修，不动全局 i18n 配置。**
候选「修 vite 的 `dropMessageCompiler`/i18n 初始化」是全站半径变更（9 个 locale × 全部视图），且并行会话正在同一前端里改别的文件。本设计只做**自己链路内的、与 i18n 无关**的错误渲染：`extractServerMessage` 兜底走字符串字面量、`buildRequestError` 带上 HTTP status 与 `bizCode`、`List.vue` 按 `bizCode` 分流（忙/离线/其他）。全站 i18n 抛错作为独立工作项写入 §6，不在本批动。

## 3. 分节设计

### 3.1 写操作台账（批6）

新增列（`model/step.go`，可空、带默认，`AutoMigrate` 直加）：

```go
SubmitState string `gorm:"column:submit_state;size:16;index" json:"submit_state,omitempty"` // prepared / sent / verified / unattributed
TextHash    string `gorm:"column:text_hash;size:16;index"  json:"text_hash,omitempty"`      // fnv32a(trim(value)) hex，跨 session 自然键片段
```

状态机（唯一写点在 `executor.go` 的 `post_comment` 分支，`executeStepWithRetry` 只负责建行）：

| 转移 | 时机 | 语义 |
|---|---|---|
| `prepared` | `commentPrep` 成功返回后 | 文本已进输入框，点击从未发生 → **可安全重下发** |
| `sent` | `commentSend` 返回且 `!isInjectTimeout` | 不可逆点已跨越，结局未知（含 WS 超时）→ 只能回查，绝不重发 |
| `unattributed` | `finalize` 未确认 + `sendErr==nil`，或 `sendErr!=nil` 且非 inject_timeout | 结果未知态，需人判 |
| `verified` | `finalize` 且 §3.2 的收紧校验通过 | 唯一可对外宣称「已发布」的态 |

`isInjectTimeout` 路径**不写 `sent`**（点击从未发生，写 `prepared` + 失败），否则台账会把「没点过」记成「可能已发」。

跨 session 查询：`repository/step.go` 加
`FindSubmitted(ctx, taskID uint, stepIndex int, textHash string) (bool, *model.BrowserStep, error)`
条件 `submit_state IN ('sent','verified','unattributed')`，命中即**拦下重发**（返回明确错误「同文本已有提交尝试，拒绝双发」），`unattributed` 也拦——宁可让人来看，不可自动双发。落库失败（DB 错）时**放行执行**（fail-open 于可用性、fail-closed 于不可逆：错误方向是拒绝提交并提示，不是无台账就提交）。

### 3.2 假绿收口（批6，扩展侧）

`injPostCommentVerify` 两处收紧：
- 容器主分支改为「容器内文本，剔除输入子树」：新增 `textExcludingInputs(root)`（TreeWalker 逐文本节点，命中 `inInputNode(parentElement)` 的节点跳过；`inInputNode` 复用现有 `isContentEditable` + `closest('[contenteditable]:not([contenteditable="false"])')` + `INPUT|TEXTAREA|SELECT|OPTION` 三条判据），替换 `norm(textOf(c)).includes(want)`。
- `evidenceOf` 的条目命中同样跳过「条目自身或祖先可编辑」的节点，且 `verified` 判定与 `evidence` 必须来自同一套剔除了输入节点的文本（否则出现「verified=false 但 evidence 里有文本」的自相矛盾包）。

常驻反向腿：夹具页新增 `?inbox=1` 形态（把 `#card` 整体搬进 `.comments-container`，即小红书的真实 DOM 形态；只搬位置，提交与记账逻辑不动），由 `task_write_fixture(..., swallow=True, inbox=True)` 组合出 **Leg X**（用例表最后一行），断言 `status=="failed" && ledger_post==0 && verified==false`。**这条腿对旧代码必红**：旧实现真机复现（session352）给出 `completed + verified=true + evidence` 仅 `{containers:1}`，与立项证据 session343 逐字段一致；新实现同腿全绿（session354）。

双发闸的设备级锁（同批新增常驻腿 `run_resubmit_gate_leg`）：同一任务连跑两次——首跑标准夹具腿全绿，二次运行必须 `failed` + 拒绝文案 + 带外 ledger 恒 0 + **页内零 trusted 触达**（夹具的 `kind=touched` 同步上行记账，配「首跑 input 帧≥1」的正控，防恒真判据）。真机实测：session359 首跑 input=16，session360 二次运行 input=0、ledger=0。

### 3.3 `is_write` 属性化（批7）

`isWriteAction` 只认 `post_comment`，覆盖面不足。改为「步骤声明 ∪ 原语推导」（`isWriteStep`，判据落库为 `browser_steps.is_write` 列——不入库的推导等于事后不可考）：

- `post_comment`（不变）
- `type` 且 `submit_on_enter == true` **且 target 命中平台定位表 `comment_input` 的任一候选段**
- `click` 且 target 命中 `send_button` 候选段；`click_near` 且 `button_text == send_button_text`
- 编排/LLM 的 `is_write: true` 声明位

两处刻意收窄与加强（相对初稿）：

1. **`type`+回车不再一律判写**。初稿的「回车即提交」会把任何表单回车（搜索框、筛选框、登录框）都算成不可逆写，于是这些步被剥掉重试能力、并在重试轮被跳过；而「哪个输入框是评论框」只有 L3 适配器定位表知道。收窄到「目标命中注册 `comment_input` 候选段」后，判据挂在平台知识上而不是挂在一个人人都用的参数形状上。诚实记录：现有真机腿里没有 `type+submit_on_enter` 步（搜索腿是「键入后另起一步点搜索按钮」），所以这条收窄是设计判断，不是被某条现成腿测出来的回归——它由 `TestIsWriteStepAttribution` 的两条对照用例（命中评论框=写 / 命中搜索框≠写）钉住。
2. **推导赢声明，且顺序在前**。LLM 把发送步标成 `is_write=false` 就能摘掉服务端的重试闸门，所以声明只能追加、不能撤销推导。

写步强制 `retries=0`（原逻辑保留，判据换成 `isWriteStep`），非 `post_comment` 写步补记台账：成功→`sent`（这些原语没有回查通路，永不记 `verified`——记了就又是假绿）；`*_not_found`/`*_inject_timeout_` 证明动作从未发生→**不记**（记成尝试会把一次干净的定位失败永久拦死）；其余错误→`unattributed`。

**任务级重试豁免**：`scheduleRetry` → `RunTaskWithRetry` 路径带 `task.RetryCount > 0`，此时若该文本已有提交尝试（`sent/verified/unattributed`），整步**跳过**（`status='skipped'`、整步零帧）而非撞闸失败，让任务把剩余只读步跑完；人工重跑（`RetryCount==0`）不豁免，仍必须 `failed` + 拒绝文案——被拦下是要呈现的事实。

跳过之后判绿还是判红，取决于闸门查回的那行台账态（`writeAttemptPrior` 随哨兵一起上抛，不靠文案回传）：

- 前一轮 `verified` → 计成功数之外的第三种：不计成败、不中断，会话照常收口。评论已被收紧回查证明发出，本轮只是补完剩余只读步，判绿诚实。
- 前一轮 `sent`/`unattributed` → 该步计入 `failed_steps` 并把会话判 `failed`，文案「重试轮防双发未重发，前一轮提交未验证…本轮无法证明内容已发布」。不 break——后面的只读步照跑，现场证据越全越好判。
- Brain 模式同上：未验证的跳过直接终止本轮（「发这条内容」的目标在本轮已不可能达成，继续烧 LLM 调用没有出路）。

这条分界是本批实现期发现的初稿漏洞：初稿只写「不计成功也不计失败」，等于让 unverified 的跳过白拿一个绿的会话——批6 立项要消灭的正是这种「没人证明却报成功」的形状。（原 `unattributed` 静默吞腿真机复现见 §3.2 Leg X。）

**键里去掉 `step_index`（F-N4，实现期发现的真洞）**：Brain 模式 `stepIdx` 每轮递增，同一条评论换轮重投会落在不同下标上，带下标的键等于没有键。双发闸与豁免共用同一趟 `FindSubmitAttempt` 查询，键收为 `(task_id, text_hash)`；代价是同任务内针对不同下标、同一文本的两次合法提交也会被拦——这正是我们要的方向（宁可拦下）。无正文的发送步（`click`/`click_near`）以 `action+target+anchor+button_text` 兜底成键。

### 3.4 砖化与收敛（批8）

1. `model/task.go` 加 `ConfirmWaitSec int` (`column:confirm_wait_sec;default:600`)；`waitForConfirm` 用 `context.WithTimeout(ctx, ConfirmWaitSec)` 的**独立**计时器 + 显式区分三种出口（放行/停止/确认超时），不再靠 `execCtx.Done()` 兼职确认超时。
2. `OnSessionFinished` 终态写库改走 `writeCtx = WithoutCancel + sessionFinalWriteBudget`（与 session 行同一模式，`executor.go:355-361` 的 R25 结论外扩到 task 行）；`feedback.go:29-60` 内 `UpdateRunResult` / `scheduleRetry` / `notifyFailure` 三步各自可归因记日志，任一失败不影响其余。
3. 对账器（`service/stale_reconcile.go` 新文件）：启动时 + 每分钟扫描 `status='running'` 且其 `GetLatestByTaskID` 已终态、且 `updated_at` 超过 `TimeoutSec + taskWatchdogGrace` 的任务 → 用最新 session 的真实终态回填 task 快照；扫不到终态 session 的置 `failed` + `error_msg='对账收敛：执行协程已退出（进程重启或超时）'`。需要 `repository/task.go` 加 `FindStaleRunningAll(ctx, grace time.Duration)`（现 `FindRunning` 强制 userID 参数，保持不动、另开方法）。
4. `Editor.vue` 确认等待上限 900s（前端夹紧 + 后端 `PublishTask` 校验 `1..900`）。

### 3.5 Host / 扩展卫生（批9）

- `cmd/nm-host/main.go`：单帧上限 1 MiB（超限断连并回明确错误，防页面快照把 stdio 缓冲打爆）；每个连接独立读泵 goroutine（现共享泵会让一条慢连接堵全表）；token 不再出现在 `addr` 字符串与 stderr（脱敏为 `token_sha256[:8]`）。
- `primitives.js` `refs` 按 `tabKey` 分桶（现全局单桶，多 tab 并发时 `@e3` 跨页解析到错元素）。
- `click_unacked` **不再落 DOM `el.click()` fallback**：CDP 可信输入未确认即记 `click_unacked` 失败交上层自愈。理由：fallback 在「CDP 其实已生效只是回包丢了」的形态下造成二次点击——对提交按钮就是双发。

### 3.6 控制台错误面（批10）

- `request.js`：`buildRequestError` 增设 `status`（HTTP 码）与沿用 `bizCode`，消息兜底用**字面量**（`'请求失败'`）而非 `t(...)`，不再在全站 i18n 抛错的路径上裸奔；`extractServerMessage` 优先 `data.message`。
- `List.vue`：`handleRunError` 改按 `bizCode` 分流——`ErrUserBusy` 与 `ErrHostOffline` 服务端**分码**（前者 `BROWSER_TASK_BUSY_3004`、后者 `HOST_OFFLINE_3005`，仍都映射 HTTP 409），忙 → 提示「已有任务执行中」并给监控入口；离线 → 打开 Host 引导弹窗；其余 → 真实文案。
- `onRun` 成功后自动跳转/暴露监控入口（F-N3）。

### 3.7 B 链路两项（批11）

- 幂等键 `contentHash = fnv32a(channel|trim(content))` 上/下行共用一个 32 位散列：撞车即丢消息。改为 `sha256(direction|channel|content)` 截 16 字节 hex，并在 `event_id`/`msg_id` 上带 direction 前缀。
- SSE 领取路径与 `FOR UPDATE SKIP LOCKED` 出队路径语义不对称（一处靠连接存活、一处靠事务锁）：统一为「先 `SKIP LOCKED` 认领，再推送，ack 落状态；断连不回收已认领行，由可见性超时重投」，并把这条写进代码注释与测试名。

### 3.8 测试与门禁（批12）

- 契约测试从 `strings.Contains(err.Error())` 迁到 WS fake host：断 `submit_state` 落库值、断命令帧次数（双发的直接证据是 `comment_send` 帧数 > 1）。
- 每条新校验都配反向测试（mutation）：改坏一处必须变红——尤其 §3.2，若腿 X 在旧代码上不红，则本设计证伪、回到 §1 重开。
- 全量：`go vet ./...` + `go test ./internal/browser_automation/...`（含 `-race`）+ `internal/service` 单独跑（≈567s，避开并发挤占假红）+ `user-web` 构建 + 扩展构建。
- 真机腿（本地夹具，禁真实平台写）：腿1 全流程放行（UI，含审计包 `own:true`）、腿2 不放行必失败零提交、腿S 停止后串行闸释放、**腿X 输入框在容器内 + swallow 必判 failed 零提交**、腿B 桥接双向 ack。

## 4. 明确不做（YAGNI）

不引入分布式锁/唯一约束表（单库单 Host，条件更新足够）；不做超时预算配置化（A6 纪律保持）；不重建 DAG（依赖仍是 `depends_on_task_id` 单链 + DFS 检环）；不做 `browser_steps` 历史归档；不在本批修全站 i18n 配置。

## 5. 批次与顺序

批6 §3.1+§3.2（含腿X 常驻）→ 批7 §3.3 → 批8 §3.4 → 批9 §3.5 → 批10 §3.6 → 批11 §3.7 → 批12 §3.8 全量门禁 + 真机腿复跑 + 只 add 本批显式路径本地 commit（不推送）。

## 6. 记录为独立工作项（本批不修）

全站 vue-i18n 生产构建 `UNEXPECTED_RETURN_TYPE`（1834/1834 key 抛错）：根因在 `vite.config.js` 的 `dropMessageCompiler:true` 与 `@intlify/unplugin-vue-i18n` 消息预编译的组合作用于 v9 legacy:false composer，影响 9 个 locale 的所有视图，半径远超本链路，需单独排期与全站回归。当前缓解 = §3.6 使 browser-automation 的错误渲染不依赖 `t()`。

## 7. 批6–批7 验收实证（全部跑出来的，非推断）

**服务端门禁**：`go test ./internal/browser_automation/service/ -count=1 -p 1` 定向 38.609s / 整包 106.627s 全绿；
`gofmt -l` 与 `go vet ./internal/browser_automation/...` 归零；扩展 `npm test` 68/68 绿（7 文件）。

**变异电池（12 条，逐条必须让至少一个测试变红，且按名字报出是哪条红的）**：
M1 派生需声明背书 / M2 回车全判写（不收窄） / M3 派生写步不钳重试 / M4 未执行也记台账 /
M5 人工重跑也跳过 / M6 重试轮不豁免 / M7 skipped 一律计失败 / M8 未验证跳过也判绿 /
M9 已验证跳过也判红 / M10 跳过不带台账态 / M11 unattributed 不算尝试 / M12 is_write 不落库。
实测 12/12 被抓到（M8 只有 `TestWSE2E_RetryRoundSkipOfUnverifiedWriteStillFails` 会红、
M6/M7/M9/M10 只有配对的正向那条会红——两条同族测试反向互锁，正是「拆分真实存在」的证据）；
还原方式=cp 备份写回并逐次比 md5（禁 git checkout/restore），末次 md5 一致 ✅ + 还原后整包复绿。
电池脚本在 `/tmp/b7_mutation_battery.sh`（一次性证据脚本，含基线预检 `exit 7`、锚点命中回读、
不编译的变异报 INVALID 而非「抓到」、判红器只认 `^--- FAIL:`）——环境类脚本不入仓，与批5/批6 同规。

**设备级三腿（`--only 夹具`，PASS=108 FAIL=0 WARN=0 SKIP=1，9 条夹具腿全绿）**：
- A 派生写属性化：session 379→380。首轮 `is_write=true`、`submit_state=sent`（这类原语无回查通路，
  记 verified 就是又一处假绿）、页内触达 17 帧（零触达判据的正控）；二次运行 `failed`+「拒绝执行」，
  页内 input 帧=0。
- B 台账不过拦：session 381→382。元素不存在 → 步 `failed` 但 `is_write=true`（判定看编排不看结果），
  `submit_state` 留空；二次运行仍失败在定位而非闸门（改好选择器可原任务重跑）。
- C 重试豁免不换假绿：首轮 session 383（swallow 页，点击落地 1 次、ledger 0、`verified=false`，
  归因文案「不重试防双发」）；自动重试轮 session 384 `failed`，写步 `skipped`，
  error=「重试轮防双发未重发，前一轮提交未验证（step=1587 session=383 state=unattributed）——
  本轮无法证明内容已发布」，`post_comment` 命令帧=0、页内 input 帧=0、二次点击=0、只读步照常重放
  （success_steps=3）。verified 前态走良性跳过那一支由 WS 测 `TestWSE2E_RetryRoundSkipsAttemptedWriteStep`
  锁（两条同族测试一绿一红，形状相反，正是「拆分真实存在」的证据）。

**本批新踩出的环境事实**：批7 机车架重启时用了 `--load-extension`，`host/status` 恒 `count=0`
且 `ps` 查无 nm-host ——稳定版 Chrome 152 忽略该 flag（§实测事实① 的重演），扩展根本没装载，
而 popup target 的 `location.href` 是 `chrome-error://chromewebdata/`、`chrome.runtime` 为 undefined
（「扩展看得见」的表象就是这么来的）。正确路径只有：全新 profile + `NativeMessagingHosts` 复制进
profile + `HIVE_MTK_WS_URL` 在 Chrome env 里 + CDP `Extensions.loadUnpacked` → SW 起、
`count=1 online=true servable=true version=1.5.0`。排障时长约 20 分钟，教训：**flag 装载成功与否要看
SW target 存不存在，不要看 tab 能不能开**。

## 7.1 批8 验收实证（D7 预算解耦 + 任务砖化对账，同样全部跑出来的）

**门禁跑在 `--shared` 克隆里**（`/tmp/b8gate` = HEAD `8199a436` + 本批 19 个文件覆盖，覆盖后
逐文件 `cmp` 与工作树零漂移）：并行会话在 19:38–19:39 把 `internal/service/sales_workbench.go`
改到不可编译，而 `browser_automation/service` 经 `internal/service` 传递依赖它 → 本批在工作树里
连基线都起不来（电池的基线预检直接 `exit 7` 拒跑，没有把它记成「测试抓到变异」）。
克隆内结果：`go vet ./internal/browser_automation/... ./internal/migration/...` 零输出；
`go test ./internal/browser_automation/... ./internal/migration/migrations/ -count=1 -p 1` →
service 114.602s / controller 0.359s / platform 0.166s / migrations 7.927s 全 `ok`；
`user-web` `npm run build` exit 0（Editor.vue 的 D7 常显 + 逐步「写操作」勾选）。

**变异电池 21/21 被抓红**（`/tmp/b8_mut_gate.log`，末次全盘 md5 一致 ✅、还原后判据用例复绿）：
M1 预算相加退回旧口径 / M2 确认自带计时器失效 / M3 超时归因不分条 / M4 执行 ctx 未用合成预算 /
M5 看门狗未用合成预算 / M6 Brain 期限漏改 / M7 派生写闸门摘掉 / M8 派生写闸门无视开关 /
M9 派生写闸门排在下发后 / M10 反馈写库未脱 ctx / M11 对账不看自身预算 / M12 completed 回填错向 /
M13 僵尸会话不收口 / M14 回填无条件覆盖 / M15 会话收口跨终态 / M16 会话收口无下限 /
M17 粗筛漏时间下限 / M18 Publish 不校验区间 / M19 区间上界失守 / M20 更新不映射确认预算 /
M21 Update 绑定缺区间。

**电池第一轮自己就是四处假证据**，逐条堵掉才算数（`/tmp/b8_mut_run.log` 留着原始失败形态）：
M6/M19 锚点 0 命中（缩进与 gofmt 对齐空格数对不上）——电池如实报「变异锚点失败（无证据）」而不是绿，
但若没人读日志、只看退出码就是假绿（外层 `; echo exit=$?` 会把 1 吞成 0，本批三次运行都按名字读日志）；
M17 是**真假绿**：`FindStaleRunningAll` 去掉时间下限后没有任何用例会红 → 新增仓库级用例
`TestReconcileFindStaleRunningAllRespectsFloor`（老快照进候选 / 刚起步的不进 / 非 running 的不进）；
M20 也是**真假绿**：静态锁只数 `ConfirmWaitSec` 出现次数，`if false {` 包住赋值后列名还在、语义已死 →
锁改成必须存在 `if req.ConfirmWaitSec != nil {` 这条指针守卫本身。教训写死：**计数式静态锁挡不住
「语句还在、条件被抽空」，要么锁字面守卫，要么配真 DB 用例**。

**v3.43.0 版本化迁移补齐**（批6/批7 的 `browser_steps.submit_state/text_hash/is_write` 与批8 的
`browser_tasks.confirm_wait_sec` 此前只靠 AutoMigrate 直加）：4 个用例绿（元信息 / nil-db /
Up 幂等 + 列类型与默认值 + gorm 往返 + Down 幂等 / registry 注册），另配 4 条反向变异全被抓红
（A1 默认写成 0、A2 丢 `text_hash` 索引、A3 不注册进迁移链、A4 Down 漏列）。
Up 用例沿用「先 AutoMigrate 建表再手动摘列」还原迁移前 schema 的手法，否则列是模型标签自己建出来的，
Up 做了什么无从证明。

**设备级 14 条夹具腿全绿**（`--only 夹具`，PASS=142 FAIL=0 WARN=0 SKIP=1，session 390–407）：
新增两臂互为正反控——确认预算先到期（session 388：`failed` + 「人工确认等待 6s」，带外 ledger=0）、
执行预算不得掐断长确认等待（session 397：15s 执行预算 / 900s 确认预算，40s 后仍 `active` 且
`confirm_pending=true`、全程零提交，主动 stop 后落 `stopped` 且并发闸随即可用）；
派生写步的 D7 面两条（未放行 → `type` 命令帧=0；放行 → 键入照常落页且 nonce 命中）。

**新腿的反向证据是拿旧二进制跑出来的**：同一份编排打到批7 的 `bin/user-server.b7`（无批8 改动）上，
session 409 在 15s 被判 `failed`、文案「等待人工确认超时（任务预算 15s）」，本腿 3 项判据全红
（`/tmp/b8_reverse_b7.log`，PASS=16 FAIL=3）。也就是说这条腿在改前必红、改后才绿，不是恒真判据。

**对账器的设备级实证 = 真把服务端打死**（`/tmp/b8_reconcile_device.py`，9/9 PASS）：
在途长任务（task 363 / session 412）跑进 `wait` 步时 `kill -9` 服务端进程 → 重启后现场形状
「task=running（砖住）+ session 已被 Host 断连钩子置 failed」→ 对账器把任务回填成
`failed` + `last_result=对账回填自 session=412` → 紧接着的新任务照常下发并跑完（session 413
`completed`），并发闸确实释放。服务端日志同刻可查：`[BrowserReconcile] task=363 快照收敛 → failed
（对账回填自 session=412）` + `断连清理完成 user=26 failed_sessions=1`。
顺带跑出来的附带收益：b8 二进制一起服，对账器就把**以往各批重启留下的 12 个砖化 running 任务**
（task=208/221/245/252/266/278/279/280…，末位 session 277–338）一次性收敛掉了——这些正是
批8 立项时「用户再也发不动任务」那句话的实际库存。

**本批踩出的两条环境/工具事实**：① 夹具腿轮询到期若不中止会话，那条 900s 的确认挂起会一路占住
「同一 Host 串行 / 用户并发」两道闸，实测把后面 9 条腿全判成「已有浏览器任务执行中」——脚本判错
一处，整轮证据就全废（已加到期兜底 stop，`expect_hanging` 腿例外由自己收口）；
② Host 断连钩子（`browser_automation_routes.go:59` `FailRunningByUser`）是**按 user 全量 sweep**，
重启窗口里一次「注册探针无回包 → shutdown 帧」的旧连接清理，就会把该用户所有在途会话置 failed，
而执行协程可能仍在另一条连接上活着。本次实测无害（协程确实随进程一起死了），但这条按 user 的口径
与「一台机器多个 host 连接」并存时是误判面，与 §3.4 的会话收口职责重叠，记进 §6 独立评估，不在本批动。
