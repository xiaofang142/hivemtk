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

- `cmd/nm-host/main.go`：单帧上限 1 MiB（超限**不断连**——出帧超限就地回错误帧并在 host stderr 留现场，入帧超限整帧丢弃并尽力按 `req_id` 回包；口径偏差与理由见 §7.2）；每个连接独立读泵 goroutine（现共享泵会让一条慢连接堵全表）；token 不再出现在 `addr` 字符串与 stderr（脱敏为 `token_sha256[:8]`）。
- `primitives.js` `refs` 按 `tabKey` 分桶（现全局单桶，多 tab 并发时 `@e3` 跨页解析到错元素）。
- `click_unacked` **不再落 DOM `el.click()` fallback**：CDP 可信输入未确认即记 `click_unacked` 失败交上层自愈。理由：fallback 在「CDP 其实已生效只是回包丢了」的形态下造成二次点击——对提交按钮就是双发。

### 3.6 控制台错误面（批10）

- `request.js`：`buildRequestError` 增设 `status`（HTTP 码）与沿用 `bizCode`；`t()` 本身在生产构建里对每个现存 key 抛 `UNEXPECTED_RETURN_TYPE`（F-N2 实测），而它的调用点全在给错误对象挂 `status/bizCode` **之前**，所以兜法取「`t()` 就地 try/catch，取不到译文退回 key」而非只把一处换成字面量——字面量修不掉遍布拦截器的其它抛出点；`extractServerMessage` 优先 `data.message`（已满足，未改）。
- `List.vue`：`handleRunError` 改按 `bizCode` 分流——`ErrUserBusy` 与 `ErrHostOffline` 服务端**分码**（前者 `BROWSER_TASK_BUSY_8002`、后者 `BROWSER_HOST_OFFLINE_8001`，仍都映射 HTTP 409；原拟的 3004/3005 码位已被既有域占用，见 §7.2），忙 → 提示「已有任务执行中」；离线 → 打开 Host 引导弹窗；其余 → 真实文案。`runBrowserTask` 改走 `_silent`：提示由分流方唯一出口给，不再两条同文案叠着弹。域内第三条 409（前置状态不满足，批10b/10c）另发 `BROWSER_STATE_CONFLICT_8003`，不让它折进 `DUPLICATE_ENTRY_3003`（那是唯一键冲突的意思），入参不合法另落 400——理由与实证见 §7.3。
- `onRun` 成功后自动跳转/暴露监控入口（F-N3）。

### 3.7 B 链路两项（批11）

- ~~幂等键 `contentHash = fnv32a(channel|trim(content))` 上/下行共用一个 32 位散列：撞车即丢消息。改为 `sha256(direction|channel|content)` 截 16 字节 hex，并在 `event_id`/`msg_id` 上带 direction 前缀。~~ **本条被实证推翻，不实施**（跑出来的证据见 §7.4，处置见 §6）：① 位宽不是坍缩原因——同文本撞键是「同一输入 ⇒ 同一键」的确定性身份，换 sha256 一条都不会少撞；② 跨会话同文本**同键**是前后端明写的既成契约（`types.js:105-106`「同一文本在不同会话的哈希相同，服务端按 msg_id 跨会话去重 patrol 回声」，且 `2026-08-07 更新：去掉 conversationID`），并由两条**已存在的具名用例**锁死（`TestContentHashMsgIDCrossLanguageContract` / `TestContentHashMsgID_StableContract` 断 `ContentHashMsgID(chan,"c2",x) == ContentHashMsgID(chan,"c1",x)` 与字面锚 `mh:00550fed`）；③ 单改一侧立刻断 JS↔Go 逐字节契约（`webhook_dedup.go:113-116` 明写这条契约），双改还要迁移已落库的 `mh:` 前缀唯一索引行。本批把同一域里**真会发错人**的那处修掉：SSE 与轮询对主动私信（`extra.dm_target=member`）的会话定位键不一致，以及 SentCache 命中的欠投递行永不了结。
- SSE 领取路径与 `FOR UPDATE SKIP LOCKED` 出队路径语义不对称（一处靠连接存活、一处靠事务锁）：统一为「先服务端权威认领（条件更新：`pending` 或 `inflight 且 claimed_at 超时` → `inflight + now()`），再推送，ack 落状态；断连不回收已认领行，由可见性超时重投」，并把这条写进代码注释与测试名。落地时按跑出来的后果加了半条：**「欠投递集合」必须按状态判而不是按 id 游标判**——`FetchOutboundSince` 的游标一过，「已推送但客户端发送失败」的行落在游标之下即永不再投（扩展端 `lastEventID` 一存即过），出站静默丢失，这是比原命题更硬的后果（`FetchOutboundUndelivered` 取代它，见 §7.4）。

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

**批11 移交的四项**（前三项落点全在并行会话的在途文件上——`internal/service/{inbox_ingress_ingest,inbox_ingress_persist,webhook_dedup,webhook_event_key}.go` 当前有未提交改动，本泳道去改就是踩别人的手；第四项属队列改造，半径在扩展侧全生命周期）：

1. **入口内容去重的 Redis 键不带会话维度**：`inbox_ingress_ingest.go:126-132` 用
   `hivemtk:dedup:sender-content:` + `ContentHashWithSender(channel, senderKeyForDedup(event), content)`
   做 `SetNX(…, InboxContentDedupTTL=5min)`，命中即 `IsDup` 拦在入库前。键里**没有 `conversation_id`**
   → 同一发送者（`self/agent` 时发送者键被折成账号 id，见 `inbox_ingress_ingest.go:59-67`）在两个会话里
   发同一句文本，5 分钟内第二条直接不落库、不触发 AI，客户侧表现就是「说了没回」。
   矛盾点：`webhook_dedup.go:128-130` 明写「严禁加入 conversationID——跨会话同内容必须可区分」，
   那句约束对「按 `msg_id` 落 DB 幂等」成立（复合索引 `(platform,msg_id,conversation_id)` 留了位置），
   对「按 Redis 在入口丢弃」**不成立**——同一个哈希被两种语义复用，第二种没有索引兜底。
2. **hub 层内容去重没有时间窗**：`inbox_ingress_persist.go:272-282` 命中 `GetByContentHash`
   （只额外校验同一 `conversation_id`）即幂等跳过，不看行多老 → 历史里发过的原话再说一遍会被静默吞。
3. **`officialEventID` 是正确方向**（`webhook_event_key.go:29`，并行会话批B 新增）：按平台官方事件 id 去重，
   内容哈希只该在「平台没给 id」时兜底。建议后续把 1/2 两处收敛到同一优先级：官方 id > (会话, 内容, 时间窗)。
4. **纯 SSE 模式不排 `_pendingAck` 内存重试队列**：`claimDuePendingAck` 只在 `pollDownlink` 里被调，
   而轮询定时器在生产形态下根本不启动——客户端 `getServerCapabilities` 探到 `sse_enabled` 即走 SSE 并
   `return`（`polling-loop.js:79-88`），只有服务端不支持 SSE 或**所有**渠道 SSE 启动失败才建轮询
   （`polling-loop.js:99`、`polling-loop.js:142-148`），而服务端侧 `FF_ENABLE_SSE_BRIDGE` 默认 true
   （`internal/pkg/featureflag/flag.go:69`）→ ack 失败的行没有客户端重试通道。
   本批的处置是把闭环放回服务端：30s 可见性超时重推 + 重推命中 SentCache 时补一次 `delivered` 确认（§7.4），
   于是「失败 → 重推 → 补确认 → 离开欠投递集合」自洽；**没做**的是把队列排水挂到 SSE 回调上（那是生命周期改造，
   且这条环的每个分支本批都打过变异：7 条打在补确认路径、5 条打在会话定位键，见 §7.4）。
   遗留风险：ack 失败的行最长多等一个 `InboxOutboundClaimTimeout`（30s）周期。

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

## 7.2 批9 / 批9a / 批9b（含批13）验收实证（Host 与扩展卫生、读数如实、重试认领门）

**代码面**：`cmd/nm-host/main_test.go` 11 条 Go 用例（`TestReadNativeFrameDropsOversizedFrameAndKeepsFraming`
/ `TestReadNativeFrameDesyncHeaderTerminates` / `TestWriteNativeFrameOutboundCap` /
`TestPumpLoopRepliesOversizedDropWithReqID` / `TestPumpLoopLogsOutboundRejectToStderr` /
`TestStdinEOFExitsProcess` / `TestDialAddrStripsTokenQuery` / `TestTokenFingerprintAndScrub` /
`TestReconnectKeepsFramesAndSingleReader` / `TestHostRegistryReadLimitHasHeadroomOverHostCap` /
`TestSourceWiringIsLive`）+ `hand.go` 2 条帧契约用例（`resolve_ref` / `click` 必须带 `tab_id`）+ 扩展 vitest 9 个文件 96 条全绿（其中 `batch9-host-hygiene` 16 条、`batch9a-open-tab` 12 条）。

**变异电池 44/44 全被抓红**：批9 主电池 29 条（G1–G10 覆盖入帧上限判定、req_id 归因、抽干正文、
读泵位置、token 三处脱敏、服务端 readlimit 配对、tab_id 下发；J1–J11 覆盖 `click_unacked` 守卫与
`refs` 分桶/归一/导航清桶/失效 ref 的结构化错误名），日志 `/tmp/b9_mut_battery_full.log`，
逐条按用例名判定、还原后 sha 一致；批9a 电池 15 条（`/tmp/b9a_mut_battery_full.log`，
「读数如实」四道新校验逐条打掉再复位）。

**批9b 归属门电池跑了四轮，两条 SURVIVED 都不是漏网而是「等价变异」**（`/tmp/b9b_mut_gate{,2,3,4}.log`）：
N3 第一版只摘认领条件一侧的到期下限 → 绿，因为条件更新那一侧还挡着 → 改成两侧同摘才红；
N11 第一版只摘显式 `deleted_at IS NULL` → 绿，因为 gorm 默认作用域本就在过滤软删 → 改成
`Unscoped()` 把作用域和显式条件三处一起解掉才红。**口径沉淀：条件成族的地方（多处同义过滤、
默认作用域 + 显式条件）变异必须整族同摘，否则电池是在给自己发绿。**这条与批13 代码注释里
「单独摘掉任一处都测不出红」互为一体，写死在测试名旁边。

**与 §3.5 的口径偏差（设计时定的「超限断连」被实测推翻）**：出帧超限就地回错误帧并在 host stderr
留现场、入帧超限整帧丢弃并尽力按 `req_id` 回包，**不断连**。理由是跑出来的不是猜的：真机 A5 腿
（`/tmp/b9c_device_legs.log`，session=439）实测超限帧 3600164B > 1048576B 的归因文案在 1.1s 落进
step 错误，A6 同刻 Host 仍 `servable=true`、A7 之后 stdio 分帧完好、A9 全程无 `nm_stdio_desync`；
若按断连处理，§7.1 末尾那条「断连钩子按 user 全量 sweep」会连带把该用户**其它在途会话**判 failed——
一次命令自己造成的超限去惩罚同用户全部任务，半径大于错误本身，且 30s 命令超时会先把它记成
「超时」而不是「帧太大」，归因反而出错。

**open_tab 假绿（批9a）的真机四联**：A1 页面 `page_loaded=true` 才回、A2 大页 markdown 真读到内容并
**如实标截断**（修复前此处是「# \n」空壳判绿）、A3 小页正控不标截断、A4 零可读内容判红 `empty_document`。

**设备级 15 条腿全绿**（`/tmp/b9c_device_legs.log` PASS=15 FAIL=0 WARN=1，session 436–442）：除上述
A 系列外，B4 Host 未换进程即重连成功（注册探针通过）、B5 重连窗口里被打断的会话收敛到终态
（僵尸并发闸已释放）、B6 重连后新任务完整跑通、B8 host stderr 记到真实重连事件、
**B9 token 只以指纹出现在日志**。`refs` 按 tabKey 分桶这一项没有对应的设备级腿（多 tab 并发在同机
串行闸下不可造），它的证据就是 J3–J8 六条变异 + vitest 用例名「两个 tab 各自的 @eN 互不覆盖、互不清空」。

**重试归属门（批9b）+ 批13 状态门**：归属门的立项事实是跨版本同库抢占——同库另一实例
（`./bin/user-server.r39`，无归属门）的重试扫描相位固定在每分钟 :14，我方 :27，到期行 100% 被它先
认领并以「browser host 未连接」烧掉一次 `MaxRetryTimes`（task=422 实测），所以除了加门
（`ConnectedUserIDs` 白名单 + 本机零连接就不认领）还要相位窗（`/tmp/b10_restart_server.sh` 注释）。
真机证据 `/tmp/b9d_retry_leg6.log` PASS=29 FAIL=0 SKIP=1：首轮 session=488 → 重试轮 session=489
（作废轮=0）、**一次认领一条会话**（task=431 会话数=2=retry_count+1，多出来的那条就是同一挂起行被
认领两遍）、重试轮写步 `skipped` 且服务端 `post_comment` 命令帧=0、页内 input 帧=0、零二次点击。
批13 把认领条件从「只有 `next_retry_at` 到期」收紧为「到期且**仍是 failed**」，四条逃逸路径写在
`repository/task.go:186-194`：`done`（手工重跑成功后又被后台重跑，含写步）、`paused`
（用户的暂停被后台解除）、`archived`/`running`（`RunTask` 会拒，但认领已把字段清空 → 挂起被无声吞掉）。
用例 `TestClaimDueRetriesOnlyFailedRowsAreClaimable`，两处条件（Pluck 与条件更新）同族摘除才可测红。

**批9 全套夹具腿复跑**：`/tmp/b9d_fixture_full.log` PASS=146 FAIL=0 WARN=0 SKIP=1（16 条腿，含
批6/7/8 的 D7 闸门、台账双发闸、静默吞、重试轮、派生写步共 6 族），其中本轮新出现的 session=505
（重试轮 `failed`）/ 506（挂起中中止 `stopped`）/ 507（放行 `completed`）；带外夹具 ledger 与
`command_log` 帧数在每条腿上双读一致，MCP 入口那条维持 SKIP（无 HTTP 面可调）。

## 7.3 批10 / 10b / 10c 验收实证（控制台错误面与执行入口契约）

**门禁全部跑在影子克隆 `/tmp/b10gate`**（`--shared` 克隆 HEAD + 88 个本链路文件覆盖 + 逐文件 `cmp`
零漂移，见 `/tmp/b10_overlay.sh`）：并行会话在此期间把 `internal/service/webhook.go` 改到不可编译，
而 `browser_automation/service` 经 `internal/service` 传递依赖它。克隆内
`go vet ./internal/browser_automation/... ./internal/migration/...` 零输出；
`go test ./internal/browser_automation/... -count=1 -p 1 -race` → service ok 112.776s /
controller ok 1.729s / platform ok 1.463s（`/tmp/b10c_race.log`）；`user-web` `npm run build` 出
535 个 precache 条目并生成 `dist/sw.js`（`/tmp/b10_web_build2.log`，同源拓扑要用它）。

**前端契约 3 用例 + 4 条变异全红**（`/tmp/b10_frontend_battery.log`）：契约断的是错误对象上的
`status`/`bizCode` 而非文案（`K1` 摘 `status` → 3/3 红、`K2` 摘 `bizCode` → 2/2 红），
`K3` 摘掉 `t()` 的就地 try/catch → 「i18n 抛错时契约字段不能一起丢」那条红（这条就是 F-N2 的原状），
`K4` 把服务端文案优先级从 `message||msg` 改成只看 `msg` → 2/2 红。

**Go 侧 10 条变异全红**（`/tmp/b10_go_battery.log`，只在克隆里打，每条 sha 还原校验）：
T1 摘掉整块先验门 / T2 门语句还在但条件被抽空 / T3 门无条件拦死 / T3b 条件写反（在线才拦）/
T4 状态类结论退回 500 / T5 两类前置错误码对调 / T6 `draft 不可执行` 丢了类型（退回裸 `fmt.Errorf`）/
T7 入参类与状态类混成同一种 / T8 装配时探针传 `nil`（`TestRunPreGateIsWiredToRealRegistry` 的字面量锁，
不是计数锁——批8 已经证明计数式锁挡不住「语句还在、语义被抽空」）/ T9 状态冲突退回按 HTTP 码折出来的通用码。
**T3 第一版是真漏网（不是等价变异）并被当场补掉**：原用例用「有没有 panic」当「走到了执行链路」的
代理，而门改成无条件拦死时同样 panic（`taskErrToResponse` 拿到的 `err` 是 nil，`err.Error()` 当场炸）
→ 改成断 `debug.Stack()` 里出现 `(*TaskService).RunTask`，位置才是证据。

**设备级 19 条断言全绿**（task 468/469、session 518 等，`/tmp/b10_phaseAB_b10f.log` PASS=15 FAIL=0、
`/tmp/b10_phaseC_b10f.log` PASS=4 FAIL=0，真实浏览器里点真实按钮、读真实 HTTP 响应体与真实 DOM 提示）：
A0 未发布点执行 → 真实 409 且 `body.code=BROWSER_STATE_CONFLICT_8003`、提示带真实原因、不开 Host 弹窗；
A 执行成功 → 地址栏跳到 `#/browser-automation/sessions/518` 且该页标题「执行监控 #518」（F-N3）、
会话真机跑到 `completed`、成功路径零弹窗；B Host 被占时 UI 点执行 → 只弹**一条**
`el-message--warning`「已有浏览器任务执行中（同一 Host 串行）」、body `BROWSER_TASK_BUSY_8002`、
不误开 Host 弹窗、不再叠红色报错；C 真 `kill -9` nm-host 后点执行 → 真实 409 + `BROWSER_HOST_OFFLINE_8001` + 只开 Host 引导弹窗一个 + 不叠红色报错 + 之后夹具自愈（扩展把 Host 拉回，`servable=true`）。

**反向证据是编出「摘掉修复」的二进制跑出来的**（`/tmp/b10_rev_gate.sh`：克隆里打变异 → `go build` →
换件重启 8299 → 跑同一条腿 → 还原 → `cmp` 零漂移）：R1 摘掉先验门 → C 腿 2 项红，实测形态正是本批
立病根的那句「/run 回 200 + session_id=515」，且那条会话随后落 `failed`（**离线不该建会话**这条产品
结论的设备证据就是这行 DB 读数），日志 `/tmp/b10_rev_r1_phaseC.log`；R2 摘掉类型分流 → A0 腿 2 项红，
实测回到 `HTTP 500 INTERNAL_ERROR_6002` 而文案仍是真话（`/tmp/b10_rev_r2_phaseAB.log`）——
**「文案对、码域错」正是只看 toast 文字的验收会漏掉的那一类**，A0 的第一版绿就是靠
「`DUPLICATE_ENTRY_3003` ≠ `INTERNAL_ERROR_6002`」蹭过去的；补码 + 用例断码 + 设备腿断码之后
（T9 反向）才成为正向锁。

**本批踩出的两条环境/工具事实**：① 夹具 Chrome 里 loopback→loopback 的跨端口 fetch 一律
status 0 / `Failed to fetch`，服务端**零日志**（PNA 之外还叠着残留 Service Worker 仍控制页面），
而生产形态本就是同源（`VITE_API_BASE_URL=/` + 反代）→ 腿改跑
`/tmp/b10_serve.mjs`（8213 静态 `user-web/dist` + `/api` 反代到 8299）而不是削弱断言；
② `gin.CreateTestContext` 没有路由树，`Param("id")` 不手工喂就先进 `parseID` 回 400「invalid id」，
两条断言会以「像门禁坏了」的形态一起红——控制器级 handler 测试必须自带 `ctx.Params`。

**顺带澄清的两条旧账**：批10 的 `ErrUserBusy`/`ErrHostOffline` 分码（8001/8002，HTTP 同为 409）
坐实了 §3.6 里「原拟 3004/3005 已被既有域占用」的判断；`List.vue` 的 default 分支仍保留
「409 + 文案含『未连接』→ 开引导」的老网关兜底，本批不删（新增的 8003 与入参类 400 都不落在它的
触发文案上，实测 A0 腿走的是 `ElMessage.error(真实原因)`）。

## 7.4 批11 验收实证（B 链路出站领取语义 + 两侧会话定位键；含 §3.7-1 的当场证伪）

**先把推翻的那半条钉死（跑出来的，不是读注释读出来的）**：`/tmp/b10gate` 影子克隆里
`go test ./internal/service/ -run 'TestContentHashMsgIDCrossLanguageContract$|TestContentHashMsgID_StableContract$|TestContentHashWithSenderDistinguishesSender$' -v`
→ 三条 `--- PASS` + `ok hivemtk-user/internal/service 0.984s`；同一组向量在 node 里跑**真实前端实现**
（`user-web/bridge` 下 `node --input-type=module` 直接 `import('./src/core/types.js')`）：
`("douyin","c1","你好")` → `mh:00550fed`（与 Go 断言的字面锚同值），
`("douyin","TOTALLY_DIFFERENT_CONV","你好")` → **还是** `mh:00550fed`，
`("douyin","c1","  你好  ")` → `mh:00550fed`（trim 生效），`"你好吗"` → `mh:19384c9d`，
带中文标点 + emoji 的 `"在吗？我们家的面霜限时8折🥰"` → `mh:1c53a57f`（UTF-8 字节口径两侧一致）。
即 §3.7-1 想保留的「跨会话区分」恰恰是**既有契约的反面**（`types.js:97,105-106` 明写 2026-08-07 起
去掉 conversationID，patrol 回声要靠同键跨会话认出同一条），而撞键的原因是「同一输入 ⇒ 同一键」的
确定性身份、与位宽无关——换 sha256 一条都不会少撞，却会当场红掉上面三条具名用例，并让已落库的
`mh:` 前缀行与新算法不再匹配（`uni_message_hub_platform_msg_conv` 上的历史幂等整批失效）。
这条按 §3.7-1 原方案做完就是「改了个不该改的东西、并把回归网撕开」，故不实施；
同域内真实存在的过度抑制另有两处（Redis 入口键无会话维度、hub 层去重无时间窗），
落点全在并行会话在途文件上 → 移交（§6-1/§6-2），不在本批踩手。

**服务端两条投递路径收口到同一把认领门**：`ClaimOutboundForPush`（条件更新：
`pending` 或 `inflight 且 claimed_at < now()-timeout` → `inflight + now()`，`RETURNING id` 判命中）
与轮询侧既有的 `ClaimPendingOutbound`（先把超时 inflight 回收成 pending，再
`FOR UPDATE SKIP LOCKED` 认领 pending）机制不同而**可投递集合同口径**（同一个
`service.InboxOutboundClaimTimeout=30s`）→ 「同一条 pending 被 SSE 与轮询各取一次」的双投窗口关闭。
第二处修复比原命题更硬：补拉集合从 `FetchOutboundSince`（id 游标）换成 `FetchOutboundUndelivered`
（按状态），因为游标一过，「推给客户端但发送失败」的行再也进不了待办 → 出站静默丢失；
游标退化成纯协议记账（`id:` 帧 / `Last-Event-ID`），不再是读取闸门。
装配缺件时仍退回游标语义，但 `SetOutboxQuerier` 必须 Error/Warn 留痕（`H1–H5` 就是打这条装配链的）。

**跑绿的契约**：`go test ./internal/repository/ ./internal/bridge/ -count=1 -p 1` →
repository `ok 176.341s`、bridge `ok 17.004s`（整包，非子集）。本批新增/改写的 7 条 repository 用例
跑真库 8232，14 条 bridge 用例逐条 `--- PASS`（`/tmp/b11_bridge_v.log`）。

**Go 侧 28 条变异全被具名用例击杀**（`/tmp/b11_go_battery.log`，只在克隆里打，每条 sha256 逐轮还原校验 ok，
基线 repo/bridge 先各跑一遍确认全绿再开打）：R1–R11 打 SQL 语义（认领门不回收超时 inflight / 不接 pending /
不分入站出站方向 / 认领写出的状态轮询侧不认 / 不写 `claimed_at` / 欠交付比较方向写反 /
欠交付丢掉超时 inflight / 不按渠道隔离 / 丢方向门 / `limit=0` 不回退默认上限 / 补拉排序反向），
S1–S12 打 SSE 门（摘门、事件名判据写错、没人在线也认领、报错放行、未命中仍推、
会话级订阅者被饿死、账号维度丢掉、`hub_id` 断言取错键、缺 `hub_id` 放行、缺件时全吞、
注入时没设超时、非出站事件也被拦），H1–H5 打装配链（补拉不过门 / 补拉按订阅判定 / 不给总线注入认领器 /
欠交付查询器接上却不使用 / 补拉超时与认领不同源）。**电池自身的四类假证据当场处理掉，才是这轮的净收益**：
5 条 `ANCHOR-MISS`（锚缩进与实际差一个 tab）；S7/S8 `COMPILE-FAIL`（`key` declared-and-not-used、
`uint64`→`int` 实参）——按口径**编译失败不算击杀**，S7 改写成 `key == ""`、S8 改写成取错键的类型断言；
S4 是**假可观测**——`fakeClaimer` 原本返回 `(false, err)`，`!claimed` 分支已经把推送吞掉，于是
「报错分支」这个变异根本改变不了行为，改成返回 `(true, err)` 才真正打到「报错也必须放弃」；
S9 `SURVIVED` 是**真漏网**（`hubID==0` 没有任何用例覆盖 → 补 `TestOutboundPushRejectsEventWithoutHubID`，
断「一次都不认领、一帧都不下发」而不是断计数）。另外换掉一处**同义反复断言**：
`bus.claimer.(OutboundPushClaimer)` 的字段本就是该接口类型、断言恒真 → 改断具体实现类型 `*fullFakeRepo`。

**扩展/桥接侧 12 条变异全击杀**（`/tmp/b11_js_battery.log`，J1–J11+J3b，5 条打在会话定位键、
7 条打在补确认路径）：新增 `test/downlink-b11-reack.test.js` 12 用例，全量 bridge `npm test` →
52 文件 732 passed / 7 skipped；电池跑完 `src/core/downlink.js` 与打变异前的备份 **sha256 逐字节相同**
（`bef025ec…64f02351e`，两条 `ok` 一起打印）。

**DM 会话定位键是这批里唯一「会发错人」的缺陷**：轮询侧早有 `extra.dm_target==='member'` 的内联重映射，
SSE 侧完全没有，而 SSE 是生产默认通道 → 同一条主动私信经 SSE 下发时被填进**群会话**的输入框
（发错对象、不可撤回）；更糟的是两条路径的 SentCache 键从此分叉（`msg_id|群会话` vs `msg_id|成员id`），
一条经轮询发过的私信换 SSE 重推时缓存判不住 → 同一段文案发给两个不同对象。本批把它收敛成单一
`dmAwareConvId(row, fallback)`（两侧同源），并强制 **ack 仍用行原始 `conversation_id`**：
用页面键翻转会 ack 到服务端不存在的行，真行永远留在欠投递集合里被重推（J9 正是打这一刀）。
`dm_target` 今天只有测试在写，但生产 `Extra` 是自由 JSON、键本身是活的 → 按「不删除、把两侧对齐」处置，
而不是按 grep 命中率删码。

**两条旧契约按新语义重写、而不是把修复回退**：`downlink.test.js` 的「同 msg_id 不重复下发」现在要求
`ackOutbox` 被调 2 次（第 2 次带 `items:[{msg_id,conversation_id}]`），「转发成功但 ack 失败」要求 3 次
（首次失败 + `_pendingAck` 重试 + 欠投递行补确认）。旧断言把「静默 continue」当正确行为锁着，
而那正是本批病根：跳过 = 不了了结 = 服务端每 30s 重推 → 客户端每次命中缓存再跳过，
同一条文案在用户聊天窗里被反复打开又放弃，且没有一行日志说明「其实早就发出去了」。
两套重试机制（内存 `_pendingAck` 与持久 `SentCache`）**刻意不合并**：前者随页面/SW 重启即失效，
后者重启后仍在并继续拦下发 → 重启后的欠投递行只有补确认这一条了结通道，多一次幂等 POST 换「不留孤儿行」。

## 7.5 批12 验收实证（全量门禁 + 两链路真机腿复跑；含门禁自身被审出的三个洞）

批12 不改产品代码，改的是「结论怎么来的」。所以这一节的重点不是又绿了什么，而是**门禁自己
哪里在造假**——三处都是跑出来的，不是推出来的。

**Go 侧门禁（影子克隆 `/tmp/b10gate`：`--shared` 克隆 HEAD `652341b0` + 本泳道文件覆盖 + 逐文件 `cmp` 零漂移）**：
`gofmt -l` 对本泳道全部改动文件零命中；`go vet ./...` 无输出；
`internal/browser_automation/...` 无 race `platform ok 0.459s / service ok 111.511s`、带 race
`platform ok 1.442s / service ok 122.051s`；`internal/repository ok 178.043s`、`internal/bridge ok 15.428s`、
`internal/migration ok 0.508s` + `migrations ok 24.681s`、`internal/router ok 23.445s`。
`internal/service` 见下面的红→归因。
`internal/router` 补跑了一次带 `-race`（不默认计入门禁）：**27 个 DATA RACE 块、3 条红用例**
（`TestMonitorRoutes_RequireAuth` / `TestOrderWebhook_EndToEndContract` /
`TestOrderWebhook_LegacyPathAnouncesDeprecationAndPointsToLiveRoute`），竞争帧全部落在
trace/recovery 中间件捕获 `*gin.Context` 这一条链上（`context.go:174` 出现 254 次），
整份日志里 `browser_automation` **零命中** ⇒ 属在案的既有竞争、不在本题范围。

**洞一：门禁清单漏文件，批9 的 nm-host 用例历次都是 `[no test files]`**。
影子树的覆盖清单是手挑的，`user-server/cmd/nm-host/main_test.go`（批9 新增，含注册探针、
帧上限、读泵终止三族断言）**从未被复制进去** ⇒ 之前每一轮门禁里 nm-host 那行都打印
`?  hivemtk-user/cmd/nm-host [no test files]` 并被 `ok` 的总观感吃掉。补进清单（89 个文件）后
首跑：`ok hivemtk-user/cmd/nm-host 0.700s`。同类截断还吃掉过 `browser_automation/controller`
的具名结果（`tail -8` 只留了后 8 行）→ 单独补跑 `ok 1.763s`。**教训固化成脚本改法**：
门禁清单改为由 `git status --porcelain` 生成而不是手挑，且任何 `tail -N` 都不许出现在取结论的
命令上（见洞二）。

**洞二：门禁脚本用 `| tail -N` 把 FAIL 的用例名截没了**。
`internal/service` 第一次跑成 `FAIL 836.311s`，而脚本的 `tail -20` 只留下测试自己打的 INF/WRN 行，
`grep '^--- FAIL'` 零命中——一个红却拿不到名字，等于没有证据。处置分两步：
① 完整落盘独跑（`-timeout 1800s`，日志 `/tmp/b12_service_solo.log`）→ **`ok hivemtk-user/internal/service 796.985s`**；
② 按口径归因环境而非代码，三条证据一起：同一棵树独跑绿、影子树 `git status` 证明本泳道
**0 个 `internal/service` 文件**（25 M + 12 ?? 全在别处）、红的那一刻 `ps` 里同时有 3 条
`go test ./internal/service/`（并行会话两条 + 本会话一条）挤同一个 8232 库。
包脚本已改成 `run()` 把每阶段完整输出写到 `/tmp/b12_gate_*.log` 并只回显 `ok|FAIL|---|panic` 行。

**JS 侧与构建（跑在真工作树，不依赖 DB）**：A 链路扩展 `user-web/browser_automation` `npm test`
→ **9 文件 96 用例全绿**（16.68s，含 `primitives.test.js` 23 条、`cdp-input.test.js` 15.1s 的
轨迹起点记忆与慢 ack）；桥接扩展 `user-web/bridge` `npm test` → **52 文件 732 passed / 7 skipped**；
`user-web` 错误契约 `browser_automation_error_contract_b10.test.js` → 3 passed。
构建三件全成：`user-web` `npm run build` rc=0（535 个 precache / 7566.02 KiB，日志 1325 行里
`grep -i error|failed` 零命中）、A 链路 `dist/background.js 25.1kb + popup.js 8.2kb`、
桥接 `dist/` 5 个 `content-*.js` + `popup.js 59.0kb`。

**产物审计（不许用「应该打进去了」）**：桥接 dist 里 `dm_target` / `[SSE ack]` / 补确认标签
**五个 content 包全含**，`background.js`、`popup.js` 不含——与 `downlink.js` 只被 content 入口
引入的 import 图一致。A 链路 dist `background.js` 含 `empty_document`、`click_unacked`、
`comment_not_rendered`、`trusted`。两处口径纠正：① esbuild 的非 ASCII 转义是**大写**
`\uXXXX`，用小写 `\u4e0b…` 去 dist 里找会把「在」判成「不在」（上一轮的假阴性就是这么来的）；
② `refs` 在扩展源码里只出现在注释，不是线上字段，按字面量核 dist 是错的核对项。

**洞三（这轮最值钱的一条）：设备上跑着的 Host 不是被审的那份码**。
`/usr/local/bin/hivemtk_browser_nm_host` 是 22:40 装的，而 `cmd/nm-host/main.go` 在 00:31 又改过
⇒ 之前所有 A 链路设备结论都对着一份**旧 Host**。处置：影子树重新编一份、`sha256` 与安装态
对照（`4785abfa…` vs `f7fe5fcc…`，确实不同）→ 安装态先备份 `/tmp/nm-host.installed.2240.bak`
→ 覆盖安装 → `kill -9` 旧 pid 73131 → 扩展重连循环自愈拉起新 pid 78177（注册即
`online:true`，探针过后 `servable:true`）→ **所有 A 链路结论在配好的产物上重跑**。
另记一条归属：`nm_frame_too_large_outbound` 不在扩展里而在 `cmd/nm-host/main.go:101`，
去 dist 找它是找不到的。

**A 链路设备腿复跑（当前二进制 `/tmp/user-server.b12` + 当前源码编的 Host，`/tmp/b12_device_legs_rerun.log`）：
`PASS=16 FAIL=0 WARN=0`**。腿A 10/10：A1 `open_tab` 真等到 `page_loaded=true`（session=529）、
A2 1.7MB 大页读到 65536 字符并如实标 `truncated`、A3 小页反向对照、A4 空页判红 `empty_document`、
A5 超限帧在 Host 边缘丢弃并带归因且 1.0s 快速失败（不是 30s 干等）、A6–A9 丢帧后 Host 仍可服务、
分帧完好、无 `nm_stdio_desync`。腿B 6/6：在途任务时 `kill -9` 服务端，Host **进程不换**即重连
（pid 78177 重启前后一致）、被打断的 session=534 在 108s 收敛到 `failed`（窗口 195s，僵尸并发闸
已释放）、重连后新任务跑通、全程无流错位、stderr 记到「断开 1 次 / 连接 1 次」、token 只以指纹出现。
其中 A1 一条同时是产物新鲜度的证据：老 Host 不回 `page_loaded` 时 Go 侧如实记 `null`
（`open_tab_truth_b9a_test`），断到 `true` 才说明运行中的是批9a 之后的码。

**批10 UI 腿复跑**：`PHASE=AB` **PASS=15 FAIL=0**（A 执行成功自动跳「执行监控 #527」并看得见步骤；
B 后台占用时点执行 → 真实 HTTP 409 + `body.code=BROWSER_TASK_BUSY_8002`、只弹一条 warning、
不误开 Host 引导、不叠红条）；`PHASE=C` **PASS=4 FAIL=0**（`kill` 注册表里那个 pid →
`servable:false,count:0` → 点执行 409 `BROWSER_HOST_OFFLINE_8001` + 「本机 Chrome 未连接」引导弹窗
且仅此一页、无红条，之后 Host 被扩展重新拉起 68583→73131 自愈）。
这里顺带纠正 §7.3 的一处取证口径：当时按「监控入口」这个**文案字面量**去 dist 里核对，
而实现其实是路由跳转（页面标题 `执行监控 #<sid>`），dist 里根本没有该字面量 ——
产物核对要认 HTTP 状态 / `body.code` / 导航结果，不认我以为的文案。

**B 链路活服务端腿复跑（真 8299 + 真 psql 落行，`/tmp/b12_leg_new_rerun.log`）**：
新语义 **9/9** —— 首连补拉到帧、推送即 `inflight` 且写 `claimed_at`、轮询取不到已被认领的行、
第二条 SSE 也拿不到未 ack 的新鲜行、ack v2 翻 `delivered`、游标已盖过时仍按状态补拉、
等 34s 可见性超时后回到欠投递集合被重投、重投后仍只归一条路径；
旧二进制（`/tmp/user-server.b10f`）**6/6 复现两处原缺陷** ——
推完不认领（行仍 `pending`，轮询立刻再给一遍 = 双投面）、未 ack 行被第二条连接再推、
游标盖过后再也没有路径取它（§3.7-2 的静默丢失现场）。
两边都跑，新加的断言才不是「只会给自己发绿」的断言。

**提交后自洽复验（`064c6a21`）**：门禁跑的是影子树，提交对不对只有「新克隆里只含已提交内容」才说得清——
`git clone --shared` 后 `git checkout 064c6a21`（漏掉任何一个未跟踪的测试文件都会当场编不过）：
`go build ./...` rc=0，`browser_automation` 三包 `controller ok 0.635s / platform ok 0.457s / service ok 123.917s`、
`internal/bridge ok 16.317s`、`cmd/nm-host ok 0.720s`、`internal/repository ok 134.956s` 全绿
（日志 `/tmp/b12_verify_go1.log`、`/tmp/b12_verify_repo.log`）。12 个新增测试文件确在提交内。

**未跑到的部分（不写成已验证）**：批11 扩展侧的补确认路径**没有在真 Chrome 里跑过**——夹具里只装了
A 链路扩展，桥接扩展未加载，而真跑它要把消息发到真实平台会话页（本泳道纪律禁止）。
它的证据面因此是：12 条变异击杀的单测（`/tmp/b11_js_battery.log`）+ 服务端活腿把「认领 / 互斥 /
超时重投 / ack 了结」这一整圈闭环真跑出来（`/tmp/b12_leg_new_rerun.log` 9/9）；
「扩展真的会去调那第二次幂等 ack POST」这一跳，属真机待用户侧回归项。

## 7.6 批14 验收实证（一次 click 只点一次：三层证据 + 真机 trusted 通道首次立证）

批14 修的是同一条因果链上的三个缺陷，链头不在 `primitives.js` 而在**测试环境本身**：

1. **链头（藏得最深的一条）**：`actionabilityCheck` 是模块顶层函数，`injClick('probe')` 通过
   `chrome.scripting.executeScript` 注入时只有 `func.toString()` 过线，自由变量在页面侧是
   `ReferenceError`，而 Chrome 的回包形态是 `result:null`（不报引用错误）。真机上 probe 分支
   **从未成功过一次**。
2. **放大器**：`dispatch` 的 `case 'click'` 把 probe 和 `cdpInput.clickAt` 写在**同一个 try 里**，
   于是 `inject_no_result` 被 catch 成「CDP 不可用 → 事件从未下发 → DOM 兜底安全」，每一次点击
   都静默降级（`channel:'dom_fallback'`），而兜底路径不重查可见性与遮挡。
   同一段代码里 `probe` 报 `element_not_interactable: covered` 也会被同一 catch 吞掉——
   **闸门在它最该生效的那一刻被自己绕过**（浮层还压着，按钮已经被点掉了）。
3. **落点**：兜底路径 `el.click()` 之外又 `dispatchEvent(new MouseEvent('click'))`，一次步骤 =
   页面侧两个 click。真机取证（session 536/537，`/tmp/b14_ev537.txt`）：6 个事件全
   `isTrusted=false`、其中 `click` 两个（t=3460 / t=3461）、按钮计数 `CLICKED-2`。
   对「发送」按钮这等于双发公开内容且不可撤回。

修复后的动作形状：一次兜底 = 一轮指针事件（pointerdown/mousedown/pointerup/mouseup）+
**一个** `el.click()` 收尾（它既是唯一那个 click，又带浏览器激活行为）；probe 移到 try 外面，
只有「probe 已通过、坐标已拿到、CDP 命令本身失败」才允许兜底；`click_unacked` 单独上抛
（`isUnackedClick`）绝不兜底。`injType` 顺带收掉一处：`submitOnEnter=false` 时不再无条件派发
Enter——富文本框上「键入」和「提交」是两件事，一个 type 步骤不该带不可逆语义。
`clickNear` 的三处同规格内联（`injClick` / `injClickNear` / `injPostCommentSend`）是**自包含约束逼出来的
重复**，注释里写明两处必须同步改；这也是 G4 变异锚点必须带尾部上下文的原因（只截中间四行命中 2 次）。

**三层证据面（各挡一类，缺一不可）**

- **静态层** `test/inject-lint.js`（acorn AST，不是字符串正则——第一版用 `fn.toString()+\bname\b`
  粗匹配，注释里写一句「与 injClick 同一份检查」就被判成引用，误报的门最终等于没人看的门）：
  把源文件里**每一个** `executeInTab` 的实参函数都查一遍自由变量，覆盖单测不一定跑到的
  `wait_for_selector`/`markdown`/`comment_verify`。枚举面本身也被钉住（手工清单要加两条守卫：
  成员表达式注入点必须被枚举到、清单成员必须还在导出）。
  **这条门自己被抓到一次**：`acorn` 头一版只当作 vitest 的传递依赖、没写进本包
  `devDependencies`，而它其实落在上层 `user-web/node_modules`（兄弟工程独立装过）——
  工作树里靠 Node 向上查找侥幸能跑，`--shared` 克隆里当场 `Failed to resolve import "acorn"`、
  整条静态门**消失但不报错**。"换台机器 `cd user-web/browser_automation && npm ci` 就跑不动"的门
  不算门，所以补声明：`package.json` 加 `"acorn": "^8.18.0"` + lock 同步（净 +15 行、零 churn）。
  这两次克隆跑正好构成这条声明的**反向测试**：未声明时红（`Failed to resolve import "acorn"`，
  `/tmp/b16_js_clone.log`）、声明后同一棵树 12 文件 rc=0（`/tmp/b16_js_clone3.log`）。
- **沙箱层** `test/inject-sandbox.js`：`new Function('(' + fn.toString() + ')')()` 重建注入函数，
  编译出的函数作用域是全局而非模块作用域——真去闭包，任何自由引用在测试里当场
  `ReferenceError`。旧测试直接 `func(...args)` 调用，闭包全在，这条断链在单测里永远不可能露出来。
  扩展侧全量首轮 **12 文件 129 用例全绿**（含新增 `batch14-click-single-fire` /
  `batch14-comment-send-gate` / `batch14-inject-selfcontained` 三组）；
  本轮收尾又补 2 条 idle-detach 用例 ⇒ 终态 **131 用例 rc=0**（见下面「全绿却 rc=1」那条）。
- **真机层** `/tmp/b15_device_legs.py` → **15/15 PASS**（本地夹具页 18611/18612/18613，
  18611/18613 记录 pointerover/pointerdown/mousedown/pointerup/mouseup/click 及 `isTrusted`
  到 `#log`、click 计数到 `#hits`、标题写成 `CLICKED-n`；18612 是带全屏 `#mask` 的发送夹具，
  计数器 `send=`/`mask=`）。L-A1 **`channel:"cdp"`**、L-A2 页面侧恰好 1 个 click + 1 个 pointerdown、
  L-A3 事件全 `isTrusted=true`。**这是本项目第一次拿到「trusted 输入通道在真机上是活的」的证据**
  ——它同时是链头修好的证据：老代码 probe 必然 `result:null`，`channel` 只会是 `dom_fallback`。
  顺带纠正一处历史口径：批2/批5g 那些「点击腿」都跑在这次修复之前，它们是**兜底通道**的结论，
  当时没有 `channel` 这一列因而看不出来（详见记忆回灌）。
- **降级通道审计面**：`navigated`/`type`/`click_near` 的回包必须交回上层——`hand.go` 里
  `typeText`/`clickNear` 旧签名 `) error` 把整个回包丢在 hand 层，降级在审计面上完全不可见；
  现在 executor 落 `channel` 如实透传（`cdp` / `dom_fallback` / `null`=老扩展无此字段）。
  兜底本身不是失败，「一片绿里全是 dom_fallback」才是它要发现的东西。

**`comment_send` 提交闸门（比一次普通 click 更该严）**：`injPostCommentSend` 找到按钮后先
`scrollIntoView` 再跑同一份 `check`，判死返回 `send_button_not_interactable: {covered|zero_box|disabled}`，
**坐标不下发**；`aria-disabled` 与 `disabled` 同权（组件库常用 aria 而非原生 disabled）。
Go 侧 `isSendGateReject`（`executor.go:284`）把它与注入超时归为同一类「零副作用」：
台账留在 `prepared`、不进 finalize 白轮、不自愈重发（换文本重选对一个「存在但不可点」的按钮没有依据）。
真机 L-C1..C6：错误文案为 `post_comment 未提交（发送按钮不可点，点击未发生）:
send_button_not_interactable: covered`、`submit_state='prepared'`（psql 直读，steps API 不暴露该列）、
该步 `direction='command'` 帧 1 条 / `'event'` 帧 1 条 / `duration_ms=0`（远小于 finalize 白轮预算
`rounds=[6000,5000,5000]`，证明确实没进白轮）、页面侧 `send=0 mask=0`（浮层一次都没被点到），
第二轮跑到同一闸门依旧 `prepared`。L-B1/B2/B3 另外挡住一条本次真机才露出来的面：
带遮挡层的页面快照必须非空且含 `textbox "写评论" @e1` / `button "发送" @e2`。

**取证口径纠偏（我自己先踩）**：三段式子命令帧（`comment_prep`/`comment_send`/`comment_verify`）
只到扩展，`browser_command_log` 记的是**步级**帧（`action=post_comment`），
所以「发送帧到线恰好一次」**不能靠帧计数证明**；它由状态机保证——`prepared` 只能由
「`comment_prep` 成功之后、`comment_send` 下发之前」那一次台账写产生，而写步强制 `retries=0`
（`executor.go:692-694`）。另两条纪律：① 同页计数器要留在同一 tab，而 session 是 fail-fast 的，
所以取证步必须显式 `continue_on_error`（这是取证前提，不是被测行为）；② `continue_on_error` 只影响
后续步，计数器归零要靠新 tab，不靠重置。

**L-D（负结果，写进台账而不是删掉）**：想构造「调试器被占」逼出 DOM 兜底、在真机上复验兜底单发。
三条尝试全部证伪：外部 CDP 客户端 `Target.attachToTarget` 与 `chrome.debugger` **不互斥**；
临装的第二条 MV3 扩展也确实 attach 成功（从它自己的 SW 调 `chrome.debugger.getTargets()`，
输出 `attached:true`，`/tmp/b15_grab_probe.mjs`），但被测腿依旧 `channel=cdp`。
⇒ **`chrome.debugger` 在本机不可排他占用，DOM 兜底路径在真机不可构造**。
兜底路径的单发性因此只在沙箱层被证（`inject-sandbox.js` + batch14 用例），这是本批诚实的边界；
它不再是线上的活跃风险，因为链头修好后真机走的就是 trusted 通道。
附带一条环境事实：夹具扩展必须**临装**（`onInstalled` 是唯一可靠唤醒点，MV3 SW 30s 空闲即回收，
本扩展系没有 `alarms` 权限），提前装好等到跑腿时它已经死了——上一轮 L-D1 绿成 cdp 就是这么来的。

**变异电池（新写：`/tmp/b15_mut_gate.py`，替掉取证口径弱的旧版；收尾扩到 G1–G6）**
**全部被具名用例杀掉**（终态 `/tmp/b16_mut_gate2.log`，电池 rc=0）：G1 去掉闸门早返分支 →
`TestWSE2E_SendGateRejectStaysPrepared`、`TestSendGateOrderedBeforeSentLedger`；G2 谓词放宽成含
`not_` → `TestIsSendGateReject`（把定位失效/WS 超时误判成未发生）；G3 去掉 `disabled` 判 →
disabled + aria-disabled 两条用例；G4 去掉遮挡判 → covered 用例；G5 把 `.keys()` 退回
`[...attached]` → 两条 idle detach 用例同时红；G6 撤掉整段守卫（`?.` 与 `try/catch` **必须一起去掉**，
单独去掉任一个都留下等价类、另一条路仍能吞掉，这是有意为之的变异而不是漏掉）。每条都带正向对照
（可点按钮仍恰好一次坐标点击，闸门不得过修正成永不提交）。
**电池自己被审出一个洞**（这一轮第二值钱的一条）：旧版按「`go test` 非 0」判红，而它还有第二种成因——
包根本编不过。本泳道并行会话一个未跟踪的 `internal/service/ltc_config.go`（mtime 10:21:16）让
所有传递编译 `internal/service` 的包 `[build failed]`，四条 Go 腿于是集体"红"却**报不出被杀的用例名**。
处置：① 严格判据——`[build failed]`/`cannot find package`/`undefined:` 一律判「无法判定」，
击杀必须 `re.findall(r"^--- FAIL: (\S+)", out, re.M)` 有名字；② Go 腿改跑 `--shared` 克隆
（`/tmp/b15gate`，HEAD + 只含本泳道 diff 的树）而不是别人的工作树；③ 每处源码改动 `cp` 备份 +
逐次 md5 比对还原（严禁对未提交文件 `git checkout`）；④ **每腿带对照组自证**——驱动器统计并打印
本腿实际跑了几个用例、跳过几个（`ran=2/2 skip=0`、`ran=6/6 skip=0`、`ran=12/12 skip=0`），
`skip>0` 或 `ran<期望` 一律判「无法判定」而不是「已杀」。**这条规矩是给所有变异驱动器立的**：
红但没有名字，就不算证据；绿但没证明跑过，同样不算。

**门禁**：`go vet` 干净；`browser_automation` 三包 `controller ok 0.849s / platform ok 0.451s /
service ok 131.868s`；架构门 clone-at-HEAD 绿、clone+本泳道 diff 绿，工作树里唯一那一条报错来自
并行会话未跟踪的 `dingtalk_media.go`（mtime 10:20:31），与本泳道 0 个 `internal/service` 文件改动一致。

**产物自洽复验（真机腿对的是哪份码）**：`src/core/primitives.js` 的 mtime（10:37）晚于
`dist/background.js`（09:36）——单看 mtime 会以为跑的是旧产物。做法是把 dist 整体快照到
`/tmp/dist_before_b16` 后重编逐文件比 md5：`background.js`、`popup.js`、`manifest.json`、
`icons/128.png` **全部逐字节相同**，只有 `build-info.json` 差一个 `builtAt` 时间戳（version 均 `1.5.0`）。
原因：10:37 那次改动是注释，esbuild 会把注释剥掉。**结论：真机腿跑的正是已提交这份码**
（`grep -c send_button_not_interactable dist/background.js` = 1，`dom_fallback` 也在）。
顺带一条口径：mtime 只能证明"改过"，不能证明"改到产物里"——产物新鲜度要以重新构建后的字节比对为准。

**提交后自洽复验（`2a75cee9` → 影子克隆 `/tmp/b16gate`）**：Go 侧在只含已提交内容的树里
`go build ./...` rc=0、`go vet ./internal/browser_automation/...` rc=0、
`controller ok 2.898s / platform ok 0.916s / service ok 219.871s`（`/tmp/b16_go_clone.log`）。
**JS 侧在克隆里跑出两次红，两次都是本泳道自己的缺陷**（这正是克隆复验的价值——工作树全绿骗过了它们）：

1. **静态闸门自己不可运行**：`Failed to resolve import "acorn" from "test/inject-lint.js"`。
   见上面静态层那条：`acorn` 未声明在本包，工作树靠上层 `user-web/node_modules` 侥幸解析得到，
   克隆树里整条门静默消失。补 `devDependencies` + lock 后同树 **12 文件 129 用例 rc=0**。
2. **一条用例的预算写错了环境**：`节点上限 400` 在克隆树里 `Test timed out in 5000ms`
   （单跑 7.8s、全量并跑 14.9s，而工作树单跑只有 2.0s，所以从未暴露）。根因是 `nameOf` 读
   `innerText` 而 **jsdom 每次访问都重算整篇样式**，成本随节点数线性放大——这是测试环境的成本，
   不是产品侧的（真 Chrome 循环内不改 DOM，布局只算一次）。按本仓 `cdp-input.test.js` 的既有
   做法给该用例显式 30s 预算，并把三个实测数字写进注释。
   反向测试跟着做了一遍：把 `MAX_NODES` 改小 → 该用例以
   `AssertionError: expected 300 to be 400` 红（不是超时红），证明加超时没把它变成永不失败的空壳。

**顺着「全绿却 rc=1」挖出的两条真缺陷（本轮最值钱的收尾）**：补完 acorn 后工作树全量跑出
`Test Files 12 passed / Tests 129 passed` 而 **`worktree_rc=1`** —— vitest 报
`Unhandled Errors / This might cause false positive tests`，栈顶是
`TypeError: Cannot read properties of undefined (reading 'detach')  src/core/cdp/input.js:100`。
读码定因，两条独立缺陷都在 `withDebugger` 的 idle-detach 收尾回调里：

- **缺陷 A（功能从未生效）**：`attached` 是 `Map`，而回调写的是 `for (const tid of [...attached])`——
  Map 展开给出的是 `[key, value]` **对**，于是 `chrome.debugger.detach({ tabId: [21, true] })`
  必定失败（又被 `.catch` 吞掉），`attached.delete([21,true])` 删的是一个不存在的键。
  净效果：**「3s 无命令就收起调试横幅」这个功能从上线起一次都没做成**，Map 里的 tab 只增不减。
  单测此前只断言"发过命令"，从不检查 detach 的实参形状，所以照不出来。
- **缺陷 B（清理路径不容错）**：该回调可能在 `chrome.debugger` 已经不在时才跑（测试拆除全局；
  真机上等价形态是 SW 上下文消失），而 `chrome.debugger.detach` 是**属性访问**，`.catch(()=>{})`
  挡不住这个同步 TypeError；异常跑在定时器里无人接 ⇒ 整轮 rc=1，且循环半途而废把后面的 tab 全憋住。

处置：`.keys()` + `chrome.debugger?.detach(...)` + 逐条 `try/catch`，并且**先让用例红再修**
（`cdp-input.test.js` 新增 2 条：① 全局拆除后触发定时器必须不抛且把状态清干净（用"下一条命令必须
重新 attach"作为可观测判据），② 一个 tab 的 detach 抛错不得憋住其余 tab，断言
`detach.mock.calls` 的 tabId 集合）。反向测试：把 `input.js` 还原成修复前形态 → 两条同时红
（第 ② 条红成 `expected [ [ 21, true ], [ 22, true ] ] to deeply equal [ 21, 22 ]`，
正好把缺陷 A 的形状摊在失败信息里），还原后 md5 与修复版逐字节一致。
修完全量 **12 文件 131 用例 rc=0、Unhandled Errors 段消失**（`/tmp/b16_js_worktree2.log`）。
因为改的是 attach 生命周期（真机上现在真的会在空闲 3s 后 detach），**产物重编 + 冷装 + 真机腿整轮复跑**
（见下条），不是在旧结论上打补丁。

**收尾后的整轮复跑（改生命周期必须重跑的那一条）**：`npm run build` → `dist/background.js`
26703 → **26771 B**，且修复确实进了产物（bundle 里 `keys()]){v.delete(o)`，`attached` 被压成 `v`；
只看 mtime 得不到这个结论）；再重编一次逐文件 md5 与快照 `dist` **全等**（`background.js`
`a1848758…`、`popup.js`、`manifest.json` 三条一致）。扩展冷装 + nm-host 重启（pid 2519，
`host/status` = `online:true / servable:true / version:1.5.0 / last_cmd_ok_at 12:14:53`），
`/tmp/b15_device_legs.py` 整轮复跑 → **15/15 PASS**（`/tmp/b16_legs_rerun.log`，
session 561/562/564、step 2178–2199）：L-A1 依旧 `channel:"cdp"`、L-A2 页面侧恰好 1 个 click、
L-C2 台账依旧 `prepared`、L-C4 依旧 `send=0 mask=0`。**口径说清楚**：这轮复跑证明的是
「attach 生命周期改了没打断任何一条真机腿」；「空闲 3s 真的 detach」本身在真机侧**没有观测点**
（夹具页看不见横幅），它的证据仍是那条单测（以"下一条命令必须重新 attach"为可观测判据）
加 G5/G6 两条击杀。

**未落地（不写成已验证）**：§8.2-1 / §8.3-1 的两小步里只落了「闸门不被绕过」这半边——
(a) `actionabilityCheck` 加 `stable`（注入函数内部 rAF 双帧比盒）与 (b) **仅 `is_write` 步**在
`clickAt` 之后、返回 `ok` 之前补一次身份复核（selector 仍可解析 + 中心点未变 + 该点 hit-target
命中同一元素，不满足改写为 `element_moved` 交自愈）**仍未做**。真机 L-A 组现在能证明
「探测到的那一次点击确实发生了、且只发生一次」，但**证不了**「点的就是探测的那个元素」——
贝塞尔飞行时间（可达数百毫秒）内页面挪动仍会点到从未被探测过的元素而返回 `{ok:true, channel:'cdp'}`。
这是 §8.3 A1 的剩余半径，归下一批。

## 7.7 批15 验收实证（提交后复验做实 + B 链路「重复」判定的一处静默丢消息入口）

**先结清批14 的账**：收尾 commit `32d1bce4`（7 个路径）之后按同一规矩回 `--shared` 影子克隆复验，
这一轮把「独立装」做透了——删掉那根指向工作树的 `node_modules` 软链，在克隆里真跑
`npm ci`（111 包 / 3s / `acorn@8.18.0` 落地），再跑全量 ⇒ **12 文件 131 用例 rc=0、无
Unhandled Errors**（`/tmp/b17_npm_ci.log`、`/tmp/b17_js_ci.log`）。比上一轮强一档：上一轮仍是
「借工作树的依赖树跑克隆的代码」，这一轮才是「换台机器 `npm ci` 能不能跑」。Go 侧同树
`go build ./...` rc=0、`go vet ./internal/browser_automation/...` rc=0、
`browser_automation` 三包 `controller 1.750s / platform 0.704s / service 138.045s` 全 ok
（`/tmp/b17_go_build.log`、`/tmp/b17_go_clone_test.log`）。**一条环境成因先记下**：第一次跑
`internal/bridge` 十余条红，根因是我这次漏导出 `POSTGRES_TEST_PASSWORD`
（`FATAL: password authentication failed for user "admin"`），补 env 后同树两包全绿 ⇒ 红先排环境前提，
别急着改代码。

**同一份调研的另一条腿照出真缺陷（读码 → 跑出来 → 修 → 反向验证，一轮走完）**：
`channelgw.IsDuplicateReason` 用 `strings.Contains` 嗅探 6 个关键词（`msg_id already exists` /
`intercepted` / `echo` / `duplicate` / `skip` / `already exists`），命中即让 HTTP 与 WS 两条传输层
回 `Duplicate=true`；而扩展侧的确认条件是 `accepted || duplicate`
（`user-web/bridge/src/core/uplink.js:65`）——**一次误判的代价是那条 event_id 永久不再上报**。
子串嗅探的入口不在别处，就在错误文案里：`internal/service/inbox_ingress.go:856` 把 per-event
失败整段包成 `"batch handle error: %v"`（此时 `Accepted=false`），而 `persistMessage` 的失败原文是
`持久化消息失败: <DB 原文>`，**PG 唯一键冲突的原文天然含 `duplicate key value violates unique
constraint`**。⇒ 一次「没存进去」被翻成「已经存过了」，客户端从此闭嘴。

这不是推演：在影子克隆里把真文案喂进函数跑出来 `true`（临时用例
`zz_b17_sniff_proof_test.go`，证完即删；永久版进
`internal/channelgw/dup_outcome_b17_test.go`，其中一条断言就是这段 PG 原文）。

**修法（本泳道可改的三个文件，零跨协议改动）**：判定权从「任意子串」收成「**结论短语前缀**」——
`duplicateOutcomePrefixes` 逐字对齐真实产出方（`msg_id already exists` /
`msg_id exists with different direction` / `content_hash already exists` /
`intercepted by middleware` / `self-echo` / `self echo` / `duplicate`），任意位置命中一律不算。
收紧方向刻意选**漏判侧**：漏判 = 客户端重报，服务端下一轮用真结论短语作答，**收敛**；
误判 = 客户端停发，**不可恢复**。顺带修掉一条旧漏判：`:474` 的「msg_id 命中但方向冲突」分支
不含任何关键词，旧实现判非重复 ⇒ 客户端每轮巡逻都重报同一条、服务端每次再走一遍钩子2。

两条行为变化之外的都保持原样：`IsDuplicateReason` 签名与两条调用点未动，`webhook.go` 那两处
直接置 `Duplicate: true` 的路径不经此函数。测试面：`internal/channelgw` 与 `internal/bridge`
各有一张**假设文案关键词表**（`protocol_test.go`、`handler_http_ack_test.go`）随契约一起改，
理由写在测试注释里（`skip due to cooldown` 冷却跳过根本不是重复；`record already exists` 不是任何
产出方会写的文案，留着就等于把"任意文案都可能命中"写进契约）。

**反向测试（`/tmp/b17_mut_gate.py`，跑在克隆树）**：M1 把实现退回旧的子串嗅探 →
`TestIsDuplicateReason`、`TestIsDuplicateReason_只认重复结论短语不认任意子串`、
`TestIsIngestDuplicate_ReasonKeywords` 三条同时红；M2 抽掉 `content_hash already exists` 前缀
（收紧过头的等价变异）→ 后两条红。两条都带 `ran=3/3 skip=0` 对照，每处注码 `cp` 备份 +
逐次 md5 比对还原（`1a86e01b…` 三次一致），还原后对照腿 rc=0。**门禁**：`gofmt -l` 两包为空、
`go vet ./internal/channelgw/... ./internal/bridge/...` rc=0、两包 `-count=1` 全 ok。

**仍未了结（不当作已修完）**：① 正解是**结构化 outcome 枚举**（`IngestResult.Outcome`），
把"是否重复"从文案里拿出来——它要改 `service.InboxIngressResult` 与全部产出点，
落在 `internal/service/inbox_ingress*.go`（并行会话在途文件，本泳道不改）；
② 前缀表是**穷举式契约**，新增产出方写新文案时会静默漏判（方向安全，但要有门）：
产出面**跑过普查**（克隆树 `grep -rnE '\.Reason[[:space:]]*=[[:space:]]' --include='*.go'`，
非测试文件 52 处），其中会流进 `InboxIngressResult.Reason` 的只有 `inbox_ingress.go` 的
14 条字面量 + `channelgw/ws.go:315` 的透传赋值；其余（`feature_flag` / `agent_co_pilot` /
`layer` / `risk_gate` / `sms·email_unsubscribe` / `sop_compensation` / `human_task` /
`user_blacklist` / `chat_public`）是别的类型，**不经这条判定**。14 条里恰好 3 条命中前缀
（`msg_id already exists` / `msg_id exists with different direction` /
`content_hash already exists`）+ `intercepted by middleware`，其余 10 条（human-locked、
system/agent persisted-only、outbound-last、5min 窗外、trigger AI、batch merged/batched、
webhook 门控、`batch handle error`）全不命中——正是想要的形状。**注**：`intercepted by
middleware` 命中即"停发但库里根本没这条"，那是 §8.3-18 的 C2 问题，本轮**刻意不改现状**
（改判定只会把"丢得更快"说成修好），前缀表与它一起留给 outcome 枚举那一批；
新增文案时必须同步 `duplicateOutcomePrefixes`。另外 `bridge.isIngestDuplicate` 实测只是
`channelgw.IsDuplicateReason` 的一行转发（`handler_http.go:725-727`），**单一事实源**，
两处不会漂移；③ 调研1 的 C1（`event_id` 用内容哈希 ⇒
同会话同文本第二条被永久吞）与 C2（中间件拦截 ⇒ 消息**根本不入库**，`:494-507` 在
`persistMessage` 之前 return，"证据消失"）两条经读码复核为真，但落点全在
`inbox_ingress*.go`（BLOCKED），维持 §8.3-9 的移交口径，不在本轮动。

**落地与提交后复验**：本批改 5 个路径本地 commit `9200a608`（+171/-19，未推送）。提交前先确认
克隆树里待测的四份 Go 文件与本仓工作树**逐只 md5 相同**（"同 ×4"），随后 `--force --detach` 到
`9200a608`（该提交叠加了并行会话的 `f837effa` 等，因此复验同时是对"我的改动 + 他们的已提交代码"
这一组合的检验），再跑：`go build ./...` rc=0、`go vet ./internal/channelgw/ ./internal/bridge/`
rc=0、两包 `-count=1` `ok 20.835s / ok 17.205s`（`/tmp/b19_clone.log`）。提交前一轮同口径的
`gofmt -l` 为空 + 两包 ok 记在 `/tmp/b18_gate.log`。

## 7.8 批16：闸门的地基不能静默失效（A7 写失败降级 / A8 fail-close / A11 副作用第三态）

立项依据是 §8.3-12/13/16 三条，共同形状是「防双发那一整套结论，建立在一件从来没人检查它有没有
真的写进去的东西上」。三处现状全部朝「继续执行」倒：`recordSubmitState` 写失败只 Warn
（四个调用点拿不到失败事实）、`guardResubmit` 查询失败按放行处理（`:61-62` 注释还把它写成刻意的
取舍）、`isWriteStep` 在 `:123` 用 `locs, _ :=` 把取表错误丢掉。

**先跑红（`/tmp/b20_red.log`，5 红 1 对照组绿）**：注入 `UpdateSubmitState` 恒失败 ⇒
`comment_send=1 want 0`（照样把不可逆帧发出去了）；两条写步都失败 ⇒ `comment_send=2`；
只让 `sent` 那次写失败 ⇒ **重试轮 `comment_send=2 want 1`，双发在同一台机器上真发生了**；
注入 `FindSubmitAttempt` 报错 ⇒ `comment_prep=1 want 0`；平台未注册 + `retry_count=2` 的 click ⇒
**到线 3 次**（retries 没钳住，因为写步判定整个失效了）。对照组（平台已注册、未命中发送位）
今日绿——批7 的收窄口径不许被本批改宽，这条就是它的看门腿。

**A11 的推理要写清楚，否则第三态会被当成"什么都判成写"**：`effectUnknown` 只在
`platform.Get` **报错**时出现，且只升级本来可能被推导成提交的动作形态（`type`+回车 / `click` /
带按钮文案的 `click_near`）；单纯"没命中 locator"仍是 `effectNone`，`scroll`/`open_tab` 这类
与表内容无关的形态也不受牵连。落库 `is_write=true`——降级判的也要事后看得出来是被降级判的。

**A7 的 `crossed` 判据（本批唯一一处需要证明的推理）**：`UpdateSubmitState` 失败不会动旧值，
所以「跨越不可逆点之后、库里那行仍不在拦阻集合内」等价于「`sent` 那一次写失败」。
于是 `prepared` 传 `crossed=false`（未跨越，拦下即可，且这是**最有价值的一格**：
prepared 不在拦阻集合内，它写失败之后若还去 send，下一轮就完全查不到凭据）；
`sent` / 终态 / 通用写步传 `true`。多记一条 gap 只会多拦不会漏拦（终态写失败时 `sent`
若已成功，库里本来就在拦阻集合内，DB 闸门自己就会拦），所以这个方向不必再收窄。

**为什么还需要进程内兜底（`ledgerGaps`）而不是只置会话降级标志**：双发闸的查询键是
`(task_id, text_hash)` 而不是 session，而自动重试是**换新 session 跑同一任务**
（`scheduleRetry` → retry scanner → `RunTaskWithRetry`），所以会话级降级标志恰好挡不住它。
兜底集合有界（`ledgerGapCap=512`，**且只按不同 `(task,文本)` 键增长**——重复写同一键先被
`rememberLedgerGap` 的去重挡掉，所以"512"量的是独立缺口次数而不是写尝试次数，批16b 用
`TestLedgerGapSetGrowsOnlyPerDistinctKey` 把这句话钉住），**不落库**。

> **批16b 对本段的口径纠正（重要，原文说过头了）**：这里原来写的是「自动重试是同进程换 session、
> 兜底因此覆盖唯一会自动重跑的那条路」。前半句不成立——`next_retry_at` 是**持久化**的，
> 认领它的是扫描器，任何持该用户 Host 连接的实例都能领，同进程只是最常见情形而非唯一情形；
> 后半句因此也不成立：兜底不落库 ⇒ 进程重启即空，而挂起的重试行还在库里。
> 已落地的收口见 §7.9（B2）：反馈层挂重试前查一次、认领后再查一次，缺口任务不再自动重跑，
> 原因写进任务行。仍然挡不住的是**跨进程**（重启后认领存量行、或人工换机重跑），
> 所以那一步的文案必须自己把「请人工核对该评论是否已发布」说尽（test 3 断言 `error_msg` 含「人工」，
> 断的就是这个不能只存在于日志里）。审查给出的「还有第二条 `time.AfterFunc` 进程内重试路径」
> 经全仓 grep 证伪（`AfterFunc` 在本泳道 0 命中），未采纳。

**跑出来的两个中间缺陷（都记着，它们是这批的实测收获）**：
① 我把重试循环写成 `if err == nil ... { break }`，`break` 出的是 for 而不是"成功返回"，
于是**每一次正常的台账写都被当成失败**——`/tmp/b20_green.log` 里 `写台账落库失败 state=prepared: <nil>`
就是它的现场（err 是 nil）。它让 5 条腿红了 2 条、剩下 3 条**照样绿**：因为写其实成功了，
只断言帧数/落库行的腿看不见差别。教训回灌：判"写失败"的分支必须有"失败时步状态"这一侧的断言，
本批的 test 1/3 正是靠状态断言把它揪出来的。
② 顺序模式里失败步是否终止本轮，取决于 `if !step.ContinueOnError { …; break loop }`
（`executor.go:388-401`，本批落码后的行号），所以"同会话后续写步被降级标志挡住"在默认编排下
**根本不可达**：一旦首条写步判败且未标 `continue_on_error`，循环当场跳出，那条步头的
`ledgerBrokenReason` 检查再也没有第二次被读到的机会。据此把可达面精确成三格——① 失败写步带
`continue_on_error: true`（本会话其后每条写步、含后续 `loop_count` 轮，都在派发前被拦）；
② Brain 模式一轮计划里的多条步（首条判败后计划仍继续派发）；③ 除此之外都只能靠**单条写步自身**
的失败把状态报出去，那走的是"步红"这条路而不是"降级"这条路。因此把 test 2 的夹具改成第一条写步带
`continue_on_error`，这才是这个标志真正服务的现场；不改夹具而直接宣布"降级已生效"就是拿测不到的
分支充当证据。**注意它与上面那个兜底集合的分工**：`writeLedgerBroken` 以 **session** 为键，
自动重试换的是 session ⇒ 标志天然不跨 session 生效，跨 session 拦阻的是 `ledgerGaps`
（以 `(task, 文本)` 为键）。两条腿各挡一侧，把其中一条当成另一条的替身就是 §7.9（B2）修的洞。

**绿（`/tmp/b20_green2.log`）**：7 条腿全 PASS（含纯函数三态表 12 行、对照组、`rc=0`，
`ok hivemtk-user/internal/browser_automation/service 119.941s`）。

**但这条"绿"用的是 `-run` 过滤，它不是门禁**：全量包一跑（`/tmp/b20_pkg.log`，
`FAIL … service 374.697s`）立刻点出两条红，一条是我的、一条不是。

- **我的一条**：`TestSendGateOrderedBeforeSentLedger` 是批14 的**静态顺序锁**，锚点写的是
  `model.StepSubmitSent, textHash)`——本批给 `recordSubmitState` 加了 `crossed` 实参，尾巴
  字面量随之消失（`sent=-1`）。锚点收窄成只认状态 token（`:93`），顺序语义一字未减。
  收窄后的锁必须重新被反向验证，否则就是假锁：电池里加 M8，把闸门早返整块挪到落 `sent`
  之后，区域文本**运行时从 executor.go 里取**（硬抄一个标点就是假「无法判定」）。
- **不是我的一条，但确实是个洞**：D7 两条 E2E 腿在全量跑里红。归因不靠推断——把克隆
  退回 HEAD（`9200a608`，批16 四文件移开）同口径全量再跑一次（`/tmp/b20_head_base.log`，
  104 PASS / 2 named FAIL，`FAIL … 404.326s`）：**同一台机器、同一负载下 HEAD 也红**
  （`D7GateHoldsSendUntilConfirmed` 17.49s、`D7AbortBeforeConfirmNeverSends` 10.50s），
  批16 就此清白。但"预存在"不是免修：红的根因是断言窗口写死 3s/5s，而**同一个包的
  `timeouts.go` 里单条命令的合法预算是 `defaultCmdTimeout=30s`**——测试在要求一个代码里
  不存在的前提（事件必须 5 秒内到线），它断的其实只是顺序与次数。同仓门禁并行时 load
  均值 73–88，一条 WS 往返秒级起步，于是必红。
  修法是把窗口从预算表推导而不是换个更大的魔数：`e2eCmdWindow = defaultCmdTimeout +
  handConditionGrace`（40s，一条命令的合法上限）、`e2eExecBudget`（一轮 D7 会话的执行 ctx 上限）。
  放宽只改"多久还没等到判红"，不改"等到后断什么"：真闸门失效照样红——**这句当时是推断，
  批16b 才把它跑出来**（电池 M9 把 `if task.RequireConfirm {` 摘掉，两条 D7 腿点名红，见 §7.9）。
  其余 E2E 腿的 `60*time.Second` 执行 ctx 同属这一类但**暂不动**——它们的腿命令数少、
  本批全量跑里没红过，改它属于扩大改动面；一旦哪天它假红，直接换成 `e2eExecBudget`。

> **批16b 对本段的两处算术复核**（都是自己重算一遍才露出来的，二手结论不可直接入库）：
> ① 基线那个 `9200a608` **不是** `3b8ef900^`（父提交是 `4d93ac0b`）。当时的动作是把工作树退回
> `9200a608` 再全量跑，二者之间差的提交全在旁道——本轮用
> `git log 9200a608..HEAD -- user-server/internal/browser_automation` 复算，命中只有我自己那条
> `3b8ef900` ⇒ 对**本泳道**而言 `9200a608` 与 `3b8ef900^` 同码，"预存在"的结论成立；
> 但原文的写法让人以为退的就是父提交，换一批旁道提交就会得出相反的结论。
> ② `e2eExecBudget` 当时写作 `3×e2eCmdWindow + handCommentSendTimeout + 15s`，理由句是
> "一轮 D7 会话最多三条命令在途"——这条算术**不成立**：夹具 `threeStageSteps` 本身就是四条命令
> （open_tab / snapshot / markdown / post_comment-prep），放行之后还有 send 与 verify，
> 合计六个命令槽、合法上界 ≈235s，而当时给的是 180s。负载足够高时这两条腿**仍然会假红**，
> 只是比 3s/5s 时代难得多——我上一步没跑出来，是因为它没红，而"没红"不等于"预算够"。
> 批16b 按命令槽数重推为 `6×e2eCmdWindow + handCommentSendTimeout + 15s`（=300s），
> 并把"六个槽、各是什么"写进常量注释：算术要能被下一个人复算，而不是留一个没人敢动的魔数。
> 顺带否证审查线报来的用量口径（"27 处用 `e2eCmdWindow`、只有 2 处用 `e2eExecBudget`"）：
> 实测该文件里 `e2eCmdWindow` 出现 6 次（含定义，等待点 3 处 + `waitConfirmPending` 1 处）、
> `e2eExecBudget` 出现 4 次、`60*time.Second` 出现 4 次。数字对不上就不要拿它当依据。

取证口径的两处自我更正（都写下来，因为它们正是本仓反复踩的那两个）：全量跑的红必须按
`--- FAIL:` 的名字读，不带 `-test.v` 时 PASS 计数恒为 0（基线那 104 是加了 `-test.v` 才有的）；
`go test … | grep …; echo rc=$?` 报的是 **grep** 的状态，基线日志里那行 `test rc=0` 就是这么来的
假绿——那里的权威证据只能是 `FAIL … 404.326s` 本身。

**反向电池跑了两轮，第一轮是电池自己的问题（`/tmp/b20_mut.log`，`电池终态: 有存活/无法判定`、`battery rc=1`）**：
- M8（把闸门早返整块挪到落 `sent` 之后）**注码无效**：区域文本从文件里现取时我把 `tail` 结尾的
  缩进 `rstrip` 掉再拼回 `sent` 行，锚点当场 0 命中；上一版更糟——`old` 截到 `sentLedgerErr` 那行
  之前却在 `new` 里复制了一遍该行，于是声明两次，`executor.go:1010: no new variables on left side of :=`
  被编译期抬走。**编译期红不是"锁抓住了缺陷"**，判据改成 `old` 必须是 `src[i:j_end+1]` 连续片段，
  并在跑电池之前先把变异后的文本打出来目视核对（命中 1 次、声明 1 次、顺序确实翻了）。
- M7（`sent` 调用点改传 `crossed=false`）**8/8 全绿=变异存活**。判下来是等价类而不是漏：
  `post_comment` 从 `sent` 到终态写之间**没有任何提前 return**，两次写用同一个 `(taskID|textHash)` 键，
  于是「`sent` 失败 + 终态也失败」两处记同一键（`rememberLedgerGap` 去重）、「`sent` 失败 + 终态成功」
  库里那行本来就在拦阻集合内、DB 闸门自己拦。所以该处实参今天是**冗余但语义正确**的一格——
  它声明的是"这次写跨越了不可逆点"这个事实，不因后一次写存在而失效，保留。
  **但这条判据是有寿命的**：一旦将来在这两次写之间插入提前 return，M7 立刻从等价变成真漏，
  届时必须重跑本电池，不许沿用本轮结论。

**绿（电池第二轮 `/tmp/b20_mut2.log`，`电池终态: OK`、`battery rc=0`）**：8 处变异 —— M1–M6、M8 共
**7 处各被点名杀掉**（每条 `rc=1 ran=8/8 skip=0`，击杀用例名逐条落日志；M1 被两条腿同时抓住），
M7 按上面写明理由判为等价类；每腿前后 `ran=8/8 skip=0`、每次还原 md5 与注码前一致
（`executor.go=b5b60cfa`、`write_ledger.go=05d68813`），对照腿首尾各一次均 `rc=0`。
电池集合里除了本批 7 条新腿，还**拉进批14 的静态顺序锁**——M8 就是专门为它设的反向验证：
锚点收窄之后必须仍然杀得掉顺序交换，否则收窄等于把锁拆了。

**绿（全量无过滤，`/tmp/b21_verify.log`）**：`go build ./...` `build rc=0`、
`go vet ./internal/browser_automation/...` `vet rc=0`、
`go test -count=1 -timeout 1500s -test.v ./internal/browser_automation/...` `test rc=0` ⇒
`ok controller 0.930s`、`ok platform 1.354s`、`ok service 214.617s`，**128 PASS / 0 SKIP**，
`--- FAIL` 零命中。上一轮假红的两条 D7 腿这次按名字绿
（`TestWSE2E_D7GateHoldsSendUntilConfirmed 5.01s`、`TestWSE2E_D7AbortBeforeConfirmNeverSends 4.29s`），
且**放宽窗口没有拖慢绿路径**——`waitFor` 一到就返回，40s/180s 只是"多久还没等到才判红"的上限，
真闸门失效时依旧红，只是晚知道。

> 三处措辞按批16b 复核收紧：① 上面这段的"全量"口径是**本泳道三个包**
> （`./internal/browser_automation/...`）不带 `-run` 过滤，不是仓库级全量（那是 §7.5 那一档，
> 单 `internal/service` 就要 880s）；两者不能互相代替，本批改动全部落在本泳道内，
> 仓库级全量留给 §7.5 的口径另跑。② 日志里记的 `executor.go=b5b60cfa`、`write_ledger.go=05d68813`
> 是 **md5 前 8 位**（脚本只打印短前缀），要比对全文得回 `/tmp/b20_mut2.log` 现场重算。
> ③ "只是晚知道"的秒数当时写 35s、正文另一处写 30s，同源事实是 `e2eCmdWindow=40s`；
> 批16b 把 `e2eExecBudget` 从 180s 重推为 300s（见上文算术复核），所以这句里的 180s 已成历史值。

**落地与提交后复验**：批16 七文件本地 commit `3b8ef900`（`write_ledger.go`/`executor.go`/
`write_ledger_b16_test.go`（新）/`write_ledger_b7_test.go`/`batch14_send_gate_b14_test.go`/
`executor_ws_e2e_test.go` + 本稿；`git status --porcelain -- user-server/internal/browser_automation`
提交后为空 ⇒ 暂存面只有本泳道路径）。提交**不是终点**：另起 `git clone --shared` 于
`/tmp/b22clone`（干净检出 `3b8ef900`，只补 gitignore 掉的 `user-server/.env`）复验自洽 ⇒
`build rc=0`、`vet rc=0`、`test rc=0`，`ok controller 1.862s / ok platform 0.651s /
ok service 170.416s`，**128 PASS / 0 SKIP**（`/tmp/b22_verify.log`）——克隆里没有工作树里那些
未提交的旁道文件，这一跑证明本批提交自身完备。随后双远端各自 `git fetch <remote> master`
分开复算 `0 1`（远端零独有、fast-forward 成立，且待推集合只有我这一个提交），
`git push upstream master` 与 `git push gitee-upstream master` 均 `4d93ac0b..3b8ef900`（无 `+`
即非 force），推后两侧 `0 0` 且三个 rev 同为 `3b8ef900`。
电池与验证两轮日志（`/tmp/b20_mut2.log`、`/tmp/b21_verify.log`）里每条 `rc=` 都取自紧邻命令自身，
不经管道——上一段记的那条"grep 冒充 rc"的教训这轮已按新口径执行。

## 7.9 批16b：二次对抗审核的四条发现（0 行台账 / 缺口后的自动重试 / 拦截文案落库 ctx / 闸门 TOCTOU）

审核方式按用户指令再来一遍（审查线 + 我自己逐条回原文复核），落点全在批16 那七文件之内。
**四条发现里三条改码收口（B1/B2/B3），一条以文档收口（B4 → §8.3-20 的 A12）**，另有一处
是文档口径本身（B5，已就地改进 §7.8 并标注"批16b 复核"）。审查线交回的条目里有三条不成立，
否决理由与复算过程全部记在 §7.8 那两段 blockquote 里（`AfterFunc` 第二重试路径 0 命中、
`e2eCmdWindow` 用量 27/2 实测 6/4、基线 `9200a608` 与父提交的关系），这里不再重复——
**留否决记录的意义就在于下一轮不必重提**。

**B1（改码）：台账写"命中 0 行"仍然返回 nil。** `UpdateSubmitState` 只看 `res.Error`，
而 `gorm` 对 `Model(...).Where("id = ?").Updates(...)` 的 0 行不报错。于是"这次提交已记入台账"
可以是**零行**——两种现实路径都落在这里：步行为手工清理后的不存在 id，以及**软删行**
（`BrowserStep` 带 `DeletedAt` ⇒ 那条 UPDATE 被自动加 `deleted_at IS NULL` 而命不中，
而同一条谓词也让闸门查询 `FindSubmitAttempt` 永远查不到它）。两处口径必须一致：
要么都当作"没有这条凭据"，那就不能返回成功。修：`RowsAffected == 0` 一律上抛，
文案点名"该行不存在或已被软删，闸门查不到这次提交"。

**B2（改码）：带着"越点未落账"缺口的任务照样被挂上自动重试。** 这一条是 §7.8 里
我那句"兜底覆盖唯一会自动重跑的路"说过头之后**必然**暴露的洞：`scheduleRetry` 落的
`next_retry_at` 是**持久化**的，而 `ledgerGaps` 活在进程内存 ⇒ 重启（或换实例认领）之后
缺口消失、重试照跑，而库里那条凭据从未写进去——**这一跑就是双发**。收口分三处，
且必须两处都拦（同一道闸门的两个消费方）：
① `NewExecutor` 里单点接线 `feedback.SetLedgerGapProvider(e.HasCrossedLedgerGap)`
（接线点选在构造函数，是为了让"装配即生效"这件事本身可被测试面覆盖——
`ledger_b16b_test.go` 的夹具就是重新 `NewExecutor` 出来跑的，不走旁路注入）；
② `OnSessionFinished` 里 `gapBlocked` 抑制 `scheduleRetry`，并把原因**写进任务行**
（`last_result` 追加一段固定文案），因为运维看不到日志；
③ `runRetry` 起跑前再查一次（缺口可能在挂起期间产生）。
抑制的**代价**由对照腿钉住：`prepared` 写失败（未跨越）时重试必须照旧挂上
（`TestPreparedLedgerFailureStillSchedulesRetry`），否则这条修法就把 A7 变成了"台账一抖就永久停摆"。

**B3（改码）：拦截/降级文案的落库走的是已经 Done 的执行 ctx。** 超时腿与中止腿上
`ctx.Err() != nil` 是**必然**而非偶发，而八处步终态写全部用的这个 ctx ⇒
"为什么这一步被拦"只留在日志里，面板上那行步还停在 `running`。这与批16 立项的那句
"闸门的地基不能静默失效"是同一类缺陷，只是这次失效的是**可见性**。
修：`finishStep` 统一走 `context.WithoutCancel(ctx) + stepFinalWriteBudget(3s)`
（形状照 `ledgerWriteBudget`，理由写进 `timeouts.go`：步行没有对账器，写完写不上是终局差别），
写失败要 `Errorf` 上报而不是 `_ =`。

**B4（文档收口，不动码）：双发闸是 check-then-act。** `guardResubmit` 先 `First` 读、
之后才写 `prepared`，中间隔着 prep 与最长 600s 的 D7 等待；同层三条并发防护
（`t.Status=="running"`、`CountRunningByTask`、`CountRunningByUser`）也全是同一形状，
而 `task.go:318` 那句注释自陈"靠 DB 唯一性兜底竞态"——`browser_tasks`/`browser_sessions` 上
**并不存在**那样一条约束。再补一道 check-then-act 只是复制同一种形状，正解是台账的
部分唯一索引，那属 DDL 决策（牵动软删语义与 #6 的裁剪口径）⇒ 登记为 §8.3-20（A12），
并写清它从"可缓"变"必做"的触发条件（放开每用户并发或引入多副本 worker）。

**跑出来的证据链（全部按名字读）：**

- **RED `/tmp/b23_red.log`**：9 腿里 4 红 5 绿，四条红正是三条改码项各自的断言
  （`ledger_b16b_test.go:67/71` 步行停在 running 且 `error_msg=""`；`:103/107` 缺口任务
  `next_retry_at` 非 NULL 且任务行没写原因；`:157` 拒绝原因是"retry runner 未装配"而不是缺口；
  `:183/190` 不存在 id 与软删行的台账写返回 nil）。五条绿的对照腿同样重要——它们证明
  这批红不是夹具自身塌了（`prepared` 未跨越仍挂重试、缺口集合按键去重、瞬时抖动恢复后不留缺口）。
- **GREEN `/tmp/b23_full.log`**（`-test.v`）：`rc=0`，**137 PASS / 0 FAIL / 0 SKIP**，
  `ok controller 0.779s / ok platform 1.243s / ok service 307.675s`；九条新腿按名字逐个绿
  （最长 `TestPreparedLedgerFailureStillSchedulesRetry 12.81s`）。
- **GREEN `/tmp/b23_full2.log`**：把 §7.8 里 `e2eExecBudget` 从 180s 重推成 300s 之后，
  本泳道三包不带 `-run` 过滤再跑一次 `rc=0`（`ok service 272.188s`）。这一跑的权威证据是
  `ok` + 零 `FAIL` 行——它不带 `-test.v`，所以 PASS 计数恒为 0，那个 0 不是"没跑"。

**为什么这一批的门禁现场是克隆而不是工作树**：动手时旁道 `internal/platform/sync.go`
正处于半写状态（`"errors" imported and not used` × 6 + `undefined: loadOrInitMerchantKey`），
而本泳道的 service 包 import 它 ⇒ 工作树里 `FAIL hivemtk-user/internal/browser_automation/service
[build failed]`（`/tmp/b23_green.log`）。**编译不过不是可以用 `-run` 绕过的小事**：注码电池遇到
它只能整趟判「无法判定」。于是 RED/GREEN 全打在 `git clone --shared` 出来的
`/tmp/b23clone`（干净检出 + 只补 gitignore 掉的 `.env` + 覆盖本泳道 7 个文件）。
批16b 收尾时旁道已把该文件修好（`16163bf2`），本批最终复验改用 `16163bf2` 的新克隆
`/tmp/b24clone`，`go build ./...` 与 `go vet ./internal/browser_automation/...` 均 `rc=0`。

**批16b 反向电池（`/tmp/b23_mut.log`，11 处变异 M9–M19，`电池终态: OK`、`battery rc=0`）**：
对照腿与收尾腿各一次 `rc=0 ran=11/11 skip=0`；基线 md5 `executor=ead9e9e7`、
`feedback=e7cba53e`、`step=5577e92c`、`write_ledger=a7c85746`，每腿还原后逐次比对一致。
11 处**全部被点名杀掉**，其中三条值得单记：
- **M9 是给 §7.8 那句话补的证据**：把 `if task.RequireConfirm {` 摘掉 ⇒ 两条 D7 腿同时红，
  红因 `ConfirmPending 未变为 true`（闸门不存在 ⇒ 挂起点根本注册不上）。
  原文里"放宽窗口不影响'真闸门失效照样红'"当时是推断，这轮才是跑出来的。
- **M14 是反向对照腿的变异**：把抑制条件取反 ⇒ `TestPreparedLedgerFailureStillSchedulesRetry`
  红（"未跨越的失败必须照旧挂起重试"）。少这一刀，B2 的修法可以退化成"台账一抖就永不重试"还全绿。
- **M17 顺带咬住一条跨批口径**：`writeStepKey` 的兜底键退化成空串时，红的不是它的自家用例，
  还有 `TestGenericWriteStepLedgerFailureJudgedRed`——空串键会让 `guardResubmit` 就地放行、
  兜底也记不住，等于批16 整套闸门对"无正文写步"整体失效。

**电池自身的两个缺陷（本轮新增，比"哪条被杀"更值得记）**：
① **M15 的锚点我写成了注码前（RED 态）的文本** `WithTimeout(ctx, …) // RED`——修复后那行
永不存在，电池只会在锚点计数上判「无法判定」，一条腿根本没跑。加了 `--check` 预检
（逐条打 `old` 命中数 + 用例名是否存在）；预检自己也要防过严：**只以 `old` 唯一为硬门**，
"替身本来出现几次"打印不判坏（`return nil` 这类替身在包里本来就有 7 处，拿它判坏会让
下一轮把预检当噪声源绕过）。
② **只看"名字红"判不了是不是负载红**：这三轮门禁与并行会话的审核轮同时在抢同一个测试库
（load 30+，同机还跑着他们的 `go test -p 1 ./internal/...`），一条腿完全可能因
`test timed out after` 而红、名字恰好对上，于是"闸门有牙齿"这个结论其实是负载给的。
现在每条红都把测试自己写的原因行打出口，输出含超时/panic 字样时不判"已杀"。

**电池从 /tmp 搬进仓**：`scripts/mut_ledger_b16.py`（批16 那八刀）与 `scripts/mut_ledger_b16b.py`
（本批十一刀）。留在 `/tmp` 的电池等于没有电池——它证明过的东西随目录一起消失，而下一轮
改到同一处代码时无人能重跑。副本相比 /tmp 版多四件事：项目根由 `__file__` 反推（不再硬编码
克隆路径与仓名，改名克隆里也能跑）、`MUT_ROOT` 覆盖、启动时按遗留 `*.bak` 还原
（上一趟被 kill 会让"基线 md5"取到**被注码的**文本，此后每次"还原一致"都在一致地还原缺陷）、
上面那条红因打印。§7.8 记录的批16 电池结论仍属 /tmp 版产物，副本只补口径不改判据。

**本批没跑真机夹具腿，这是明确的取舍而不是遗漏**：批16b 四改动面全部在服务端落库与重试编排里
（`UpdateSubmitState` 返回值、`OnSessionFinished`/`runRetry`、`finishStep`、文档），
**扩展侧与 CDP 层零改动**，而夹具腿证的是那两层（§7.6/§7.7 的 L-A/L-B/L-C）。
另一半原因是现场：本批收尾时夹具那一侧已经不在位（`9333` 无监听、nm-host 进程已退出），
拉起它要重装扩展 + 换服务端二进制（我这一泳道的 8299 跑的还是批15 那版），而同机并行会话
正在跑它们自己的变异电池与全量轮（`go test -p 1 ./internal/...` 在跑、load 30+），
重启 8299 就是打断别人的腿。真实执行证据因此仍由 WS-E2E 那一层给
（真 Host WS + 真 PG，批16 的双发现象就是它跑出来的）。
**触发条件写在前面**：批17 一动扩展（`stable` 判定与点后身份复核必然动 `primitives.js`），
夹具腿就是硬门禁的一部分，且必须按 §7.6 的口径重跑 L-A/L-C 两条。

**批16 电池在批16b 代码上重跑**（证明旧闸门没被新改动松掉）：`--check` 先确认八条锚点
在改动后的 `executor.go`/`write_ledger.go` 里**仍然唯一命中**（M4 的替身 `return nil` 本来
出现 7 次，不构成注码歧义），随后在 `/tmp/b24clone`（检出 `16163bf2` + 本泳道七文件，
`go build ./...` `rc=0`）整趟重跑 ⇒ `/tmp/b24_b16on16b.log`：`电池终态: OK`、`battery rc=0`，
对照腿与收尾腿各一次 `rc=0 ran=8/8 skip=0`，M1–M6、M8 七处**仍被点名杀掉**、红因与本批
首次记录逐字一致（M3 仍是"重试轮 `comment_send=2 want 1`"那句双发），M7 仍是同一条等价类。
基线 md5 与 §7.9 上面那趟电池相同（`executor=ead9e9e7`、`write_ledger=a7c85746`）——
两趟打的是同一份代码，这一点是由 md5 前缀对上而不是由"我看过文件"保证的。

**批16 电池在批16c 代码上第三次重跑**（同一份 tightened 脚本，`/tmp/b25_mut_a2.log`）：
`电池终态: OK`、`battery rc=0`，八刀里 M1–M6、M8 七处仍是「红 已杀」且**红集合恰好等于 `must`**
（新上加的上界口径，见 §7.10），M7 仍是同一条等价类，对照腿/收尾腿 `rc=0 ran=8/8 skip=0`。
基线 md5 与 §7.9 那两趟对不上（`executor=48abc4e5` vs `ead9e9e7`）是**预期的**：这一趟的树里
多了批16c 的 `retryBackoffDelay` 抽取，而 `write_ledger=a7c85746` 未动——对不上 md5 的那一格
恰好是本批唯一改过的那一格，这比"两趟 md5 相同"更能说明打的是哪份代码。

## 7.10 批16c：二次审核线的结论逐条重跑 + 电池自身八条口径收紧

**这一批的输入是外部审核线（同一份代码、行号截至 `73f92fb7`）报回的三份清单**：
13 处「一行注码、全套测试仍绿」的候选、电池脚本自身 8 条不诚实、以及一条对 §7.8 里
M7 等价类的保留意见。本批**没有一处按报告原文入库**——§7.9 立的规矩（二手结论先自己复算）
在这里第二次生效，而且这次连我自己上一轮写下的数字一起复算。判定口径只有一条：
**把那把刀真注进代码、看有没有腿红**，不看报告怎么说、也不看我怎么说。

### 十三刀逐条判定

| 报告的刀 | 复验方式 | 判定 |
|---|---|---|
| A1/A2/A3/A4/B2b/`RowsAffected` 那一族（6 刀） | 批16b 电池在收紧后的脚本下整趟重跑（`/tmp/b25_mut_b3.log`） | **当时是真洞、现已闭**：M9–M19 十一刀全部「红 已杀（红集合恰好等于 must）」，其中就包括这六条对应的落点 |
| B1「`if writeStep {`→`if effect == effectWrite {` ⇒ unknown 步绕过双发闸」 | 拆成六刀分别下：M20（四道一起）、M27（双发闸+降级那一格）、M28（D7 那一格）、M29（只摘降级拦截）、M30（只钳 retries）、M31（只漏失败路径落账） | **实质成立、表述不成立**：M30/M31 当场被批16 的老腿 `TestWSE2E_UnknownLocatorTableTreatedAsWrite` 点名杀掉（红因原话 `click 到线 3 次 want 1`），所以「**全套**测试仍绿」这句是错的；但同一符号下另外三格（双发闸、降级拒绝、D7）在 unknown 步上确实一条断言都没走过 ⇒ 补三条腿 |
| B3「摘掉 `defer clearLedgerBroken` 全绿」 | M21 | 成立。批16 那条腿**刻意不读降级表**（表在 `ExecuteSession` 末尾恒被清空，读它就是永远绿的断言），代价是"清除"这个动作本身没有腿——它表现的真形态是**下一个会话还能不能下发写步** |
| A6「`isNeverExecuted` 的 `_inject_timeout_` 分支没腿」 | M22 | 成立：批7 那几条只打了 `_not_found`。这条分支的语义是「点击从未发生 ⇒ 可安全重下发」，摘掉它就是把一次注入拥堵超时永久钉成不可重跑 |
| A7「指数退避改成线性全绿」 | M23 | 成立 ⇒ 算式抽成 `retryBackoffDelay`（`timeouts.go`）后补腿。**本批唯一的生产码改动**，且是纯函数抽取、无行为变化（腿里同时钉住调用点仍走它，防止"抽了没人用"） |
| A8「`FindSubmitAttempt` 的 `Order("id asc")`→`desc` 全绿」 | M24（打在 `repository/step.go`） | 成立：顺序是**确定性**承诺，没腿就等于「同一份库、同一次判定可以给出两种结论」。这一刀同时补掉报告缺陷 #5 的另一半——`step.go` 此前**既没有变异也没有测试文件**（`? …/repository [no test files]`） |
| B4「`writeStepKey` 丢掉 `Anchor`/`ButtonText` 全绿」 | M25 | 成立：现有腿只断"键非空"，丢字段仍然非空。补的腿断四个面各自换值都要变键、且空白差异仍归一 |
| gap-cap 的字面量 512 与淘汰方向 | 方向早有腿（`TestLedgerGapSetGrowsOnlyPerDistinctKey` 断最新键必在集合内）；只把 512 改成别的数字注不出红 | **一半成立、按等价类登记**：安全论证是「集合有界」，不是「上界恰好 512」。改上限要同时改那条腿里的 `ledgerGapCap` 引用（腿读的是常量不是字面量），这是**刻意**的——把 N 写进断言只会让人以为 N 有语义 |
| M7 等价类的保留意见 | 见下一节 | 采纳，且补法改成了可自动失效的锁 |

### 审核线对 M7 的保留意见：等价类的前提要有锁，而不是靠人记得

批16 把「sent 落账点不再标 `crossed`」判成等价类，靠的是一条**控制流事实**：
`sent` 写与终态写之间没有任何早返、且两次写用同一个 `textHash`，所以 sent 失败要么被终态写补成
一条库里的提交尝试（DB 闸门照拦），要么两次一起失败（同键去重后仍记一条兜底缺口）。
报告两点保留：(a) 没有任何东西在"有人往中间插一条早返"的那天变红，spec 只写了"有寿命、要重跑"；
(b) 现有选靶（`state != prepared`）永远把两次写一起打断，所以等价性的第二支是读码得出的。

(a) 采纳：新增静态锁腿 `TestSentToFinalLedgerWritePathHasNoEarlyReturn`——两个落账点之间一旦长出
`return`/`break`/`continue`，或尾巴上的实参不再是同一个 `textHash`，当场判红并注明"等价类失效、
M7 必须重判"。它的牙由 M32 证，且**实测红集合只有这一条腿**：注码是行为不变的 `if false { return nil, nil }`，
所以除这条锁以外没有第二条腿变红——用会改行为的注码去证明静态锁等于没证明（行为一变全屋皆红，
锁红不红无从分辨）。(b) 记为**已知不补**：造"只有 sent 失败"的形状要往 `recordSubmitState` 里加
注码面（三个 state 选靶已覆盖同一条落库路径），而它证明的是"补写可达"，与 (a) 的锁是同一条事实的
两种写法——留锁不留靶，理由在此写清而不是默默不做。

### 电池自身：报告点出的 8 条，7 条成立并已修，1 条否决

| # | 报告说的 | 判定与处置 |
|---|---|---|
| 1 | 对照腿打印「skip=0」却没查它 | **成立**，最直白的一种假绿（库不可达 ⇒ 8 条全跳 ⇒ `ran==set(TESTS)`、无红、`rc=0` ⇒ 宣布"门在位"而一条没跑）。三处脚本都改成 `skip0` 参与判定，收尾腿同 |
| 2 | `must ⊆ failed` 单向、没有"其余腿必须绿"的上界 | **成立**，本批修完立刻兑现了价值：任何把整包打红的注码（负载红、夹具前提崩、panic 前一地红）以前都会被记成"已杀"。现在要求红集合**恰好**等于 `must`，多一条就判「无法判定」 |
| 3 | 电池那 7 条腿的执行 ctx 仍是 60s，而同批已把 D7 腿换成从生产预算推导的窗口 | **成立**（我自己上一批留的债：同一条根因两套处置，且留着的那套是反向证据的承重墙）。三趟电池的 `TESTS` 集合里现在**没有一条腿**还用 60s：本批改掉 11 处（`write_ledger_b16_test.go` 6 + `ledger_b16b_test.go` 5），`ledger_b16c_test.go` 从第一行起就用推导值。`go vet` 随即报两个文件 `"time" imported and not used`，删掉导入——**vet 报了这个错，恰好证明替换是真的**。<br>**没做完的部分要记在账上**：本包另有 20 处 `60*time.Second`（`write_ledger_b7_test.go` 8、`executor_ws_e2e_test.go` 4、`ledger_ws_test.go` 4 等）不在这三趟电池的靶上，同一根因仍在；它们的红目前只会红在全量门禁里（那里 `-timeout` 兜得住），不会污染反向证据 |
| 4 | `rc=1` 却读不到 `--- FAIL:` 名字时仍印「红 已杀」 | **成立**：`ok = rc==1 && failed` 与打印式 `rc==1` 不是同一个式子，"红 已杀 ｜（无红）"是脚本能合法输出的行。改为红而无名 ⇒ 无法判定 |
| 5 | 备份面窄于结论面 | **一半成立**。「备份只需覆盖被注码的文件」这句本身没错，但结论面确实大过它：`step.go` 从没进过任何一趟电池（见 A8 那行），所以"末次 md5 一致"读起来像全树一致。处置是**扩注码面而不是扩措辞**：本批起 `repository/step.go`（M24）与 `timeouts.go`（M23）第一次进电池目标，三趟合起来的注码面是 `executor.go`/`write_ledger.go`/`feedback.go`/`repository/step.go`/`timeouts.go` 五个文件，每趟日志的基线 md5 行列的正是**该趟真注码的那些**（b16 两格、b16b 四格、b16c 四格）——按趟读，不要跨趟想象 |
| 6 | `-run` 子集不是门禁，而电池日志正是唯一的落盘证据 | **成立**，改文档不改行为：三处脚本 docstring 各写一句「**本电池不是门禁**；门禁是干净克隆里的 `go build ./... && go vet ./... && go test ./...`」 |
| 7 | `EQUIV` 逃生口绕开 `must` 检查 | **成立**：判成等价的注码永远不再有条腿会红，而旧代码在 EQUIV 分支直接 `continue`。现在要求该趟"绿且 ran 齐全且 skip==0"才允许登记等价类，否则仍判无法判定 |
| 8 | 没有「注码真的改到字节」断言 | **成立**：`old==new` 的注码会以"全绿 ⇒ 存活/等价类"的形态出现，而这是电池最省一次跑、也最容易骗自己的一条。现在注码后先比 md5，没变字节直接判无法判定 |

新口径（#2 的上界）第一次跑就抓出三处**多红**，全部逐条单独复现过再判：M26 首轮多红
`TestInjectTimeoutStepStaysReDispatchable`、批16b 的 M14 多红 `TestCrossedLedgerGapSuppressesAutoRetry`、
M17 多红 `TestGenericWriteStepLedgerFailureJudgedRed`。判"是真实连带面"而不是"注码打偏"的依据不是推理：
用 `/tmp/b25_probe_m14_m17.py` 把那两个注码分别只打在**那一条腿**上单独跑，取回原因行原文
（`挂起重试 next_retry_at=2026-09-20 20:17:23… want NULL` / `通用写步的越点未落账必须进缺口兜底集合`），
两条都是同一判据的另一面 ⇒ `must` 加名并在脚本里写清原因。M17 那条 §7.9 早就记过（"键退化成空串时红的不是它的自家用例"），
旧口径容得下它、新口径容不下——这正是加上界的意义。

### 批16c 的实证

- **九条新腿**（`service/ledger_b16c_test.go`，八条覆盖 + 一条静态锁）在干净代码上全绿：
  批16c 电池 `--check` 先确认 13 处锚点唯一命中、11 个用例名（九条新腿 + 两条老腿）都在包里，
  对照腿 `rc=0 ran=11/11 skip=0`（`/tmp/b25_mut_c3.log`）。腿 ① 首轮是**我错它不红**：我按"重试轮跳过"预言 `status="skipped"`，
  真形态是非重试轮被闸门硬判 `failed`（"写步拒绝执行…重发即双发"），闸门是对的、预言是错的，
  断言改过来并写明两种判法都算拦住。
- **13 刀全部「红 已杀（红集合恰好等于 must）」**：M20–M32（`/tmp/b25_mut_c3.log`，
  基线 md5 `executor=48abc4e5 step=5577e92c timeouts=525839d6 write_ledger=a7c85746`，收尾对照腿 `rc=0 ran=11/11 skip=0`）。
  逐刀拆（M27–M31）而不是只打 M20 一包，是为了**每条新腿各自有牙**：一起摘会红不等于每道闸都有腿。
- **三趟电池打的是同一棵树**（批16 八刀 8/8、批16b 十一刀 11/11、批16c 十三刀 11/11），
  串行跑在同一个干净克隆 `/tmp/b25clone`（检出 `7a37f6b8` + 本泳道文件 + `.env`），
  注码目标目录只在该克隆，共享工作树未被注过一刀。
- **驱动脚本自己也被抓出一条假绿**：链式驱动先 `echo "battery rc=$?"` 再 `echo ">>> 结束 rc=$?"`，
  第二个 `$?` 取到的是**上一条 echo 的 rc**，于是 b16b 那趟实际 `rc=1` 在链日志里被记成 `rc=0`。
  链日志的 rc 一律作废，判定只按电池自己写进日志的 `battery … rc=` 与 `电池终态` 读——
  这是"exit code 0 不算证据"的又一种形态：**取 rc 的那一行代码本身要审**。
- **`gofmt -w` 之后的最终复跑**（格式改动只落在 `feedback.go` 与 `ledger_b16c_test.go` 的空白对齐，
  前者 md5 因此是 `ae2bcaca`）：门禁换到另一个克隆 `/tmp/b26clone`（检出并行会话当时的 `00c7c263` +
  本泳道文件 + `.env`）跑 `gofmt -l 泳道=空`、`go build ./... rc=0`、`go vet rc=0`、
  `go test -test.v ./internal/browser_automation/...`（**不带 `-run`**）`rc=0`，
  计数 `PASS=146 FAIL=0 SKIP=0`（controller 0.437s / platform 0.561s / service 170.558s，`/tmp/b26_test2.log`）；
  三趟电池在格式化后的同一棵 `/tmp/b25clone` 上整趟重跑（`/tmp/b26_chain.log`）：
  b16 八刀 = 7 杀 + M7 那条等价类（`/tmp/b26_mut_a.log`，`ran=8/8 skip=0`）、
  b16b 十一刀全杀（`_b.log`）、b16c 十三刀全杀（`_c.log`），三趟 `电池终态: OK`、
  对照腿与收尾腿的 `ran/skip` 全对。**两棵树的基线 commit 不同（`7a37f6b8` vs `00c7c263`）而泳道九个文件逐字节相同**
  ——差的那几个 commit 全在并行会话的文件里（本批跑过一次注码现场核对：三趟结束后 `*.bak` 零残留、
  九文件 md5 与工作树一致，且等于日志里引用的 `executor=48abc4e5 step=5577e92c timeouts=525839d6 write_ledger=a7c85746 feedback=ae2bcaca`），
  所以"电池打的就是要提交的这份代码"是由 md5 对上保证的，不是由"我看过文件"保证的。

**本批仍是零生产行为改动**（除 A7 那一处纯函数抽取），扩展与 CDP 层一字未动，
所以 §7.9 那段"没跑夹具真机腿"的取舍与触发条件原样有效：批17 一动 `primitives.js`，
L-A/L-C 两条就是硬门禁的一部分。

## 8. 批14 同行调研台账（六维度取证 + 对本仓的实证纠正）

取证方法：六路并行 agent，每路给「本仓现状线索 + 待查同行清单」，要求每条机制带真实字段名与来源 URL、
自标 `[doc]/[src]/[blog]/[unverified]`、并列 `未取到`。报告落盘 `/tmp/peer-research/0{1..6}-*.md`（六份）。

### 8.1 先记账：agent 报来的「本仓现状」有七条不成立，逐条读码纠正

**规则：同行证据可信，agent 对本仓的 file:line 断言一律自己读一遍再用。** 这一轮里 6 份报告有 2 份
给我们仓库编了不存在的锚点，其中一份的「P0 结论」整个建立在假锚点上。

| # | agent 断言 | 实测 | 证据 |
|---|---|---|---|
| 1 | 定位 P0："`backendNodeId` 被跨命令持久化复用，落点 `src/core/cdp/cdp-page-actions.js:56-64`" | **该文件不存在**（`src/core/cdp/` 下只有 `input.js`），且 `backendNodeId` 在整个扩展源码里 **0 命中**。真机制是 `@eN → cssPath` 存 SW 内存 | `find src -name '*.js'`；`grep -rn backendNodeId src/` 无命中；`accessibility.js:144-165` |
| 2 | "refs 只有 TTL 兜底，DOM 变更后旧 ref 静默指错" | 每次 `assembleSnapshot` **整桶清空**（`accessibility.js:146`），导航/`open_tab` 也清（`:171-179`）；解析不到时抛**结构化** `element_not_found`，正是为了让 Go 侧自愈 `isSelectorMiss` 接住（`primitives.js:630-640` 注释即此因） | `accessibility.js:144-179`、`primitives.js:635-640` |
| 3 | "动作前只判 `length>0`，没有 visible/enabled/hit-test" | `actionabilityCheck`（`primitives.js:12-26`）已覆盖 `display/visibility/opacity`、零尺寸盒、`disabled`/`aria-disabled`、**`elementFromPoint` 遮挡**四项；缺的只有 `stable`，且代码里已写明为什么缺与替代手段 | `primitives.js:12-26`（含 23-24 行注释） |
| 4 | "桥接扩展 SentCache 是内存态，扩展重启即失效" | **持久化到 `chrome.storage.local`**（key `bridge_sent_${channel}`），`load()`/`flush()` 成对；只有条数上限 `sentCacheMax:2000`、无 TTL | `user-web/bridge/src/core/downlink.js:11-53`、`constants.js:248` |
| 5 | "90 天保留期清理会截断 `browser_command_log` 内容字段" | command_log 是**整行删除**（`PruneBefore` 分批 5000）；被"清空文本、保留行"的是 `llm_plans` 的快照大字段 | `repository/command_log.go:45-60`、`service/retention.go:44-56` |
| 6 | 调研1-C4："嗅探表里的 `skip`/`already exists` 会命中**非重复**语义的 reason，例如 `inbox_ingress.go:700` '跳过本次（前端将重新上报…）' 与 `:516` 'persisted only'" | **两处举证都不成立**：`:516` 的文案是 `sender_type=system; persisted only (系统消息不触发 AI)`，不含任何英文关键词；中文 reason 更不命中；而 `handler_http.go:617` 那条 `internal_only: persisted only` 在 `:619` 直接 `continue`，**根本走不到** `isIngestDuplicate`。报告把"关键词表很脆"这个正确的直觉，配上了三个不存在的实例 | `inbox_ingress.go:450-570`、`handler_http.go:605-660` 逐行读 |
| 7 | （承 6，替代结论）真正的命中面在**错误文案**上，不在业务文案上 | `inbox_ingress.go:856` 把 per-event 失败包成 `batch handle error: <原因原文>` 且 `Accepted=false`，PG 唯一键冲突原文自带 `duplicate key value violates unique constraint` ⇒ 子串命中 ⇒ 前端 `accepted\|\|duplicate` 停发一条从未落库的消息。**跑出来的**：把真文案喂进函数返回 `true` | `/tmp/b16gate` 临时用例（跑完删除）→ 永久断言 `channelgw/dup_outcome_b17_test.go`；详见 §7.7 |

自我纠正第 5 条也说明：§7.5 里"命令日志截断"的措辞不准，正确表述是"整行删除 + llm_plans 清文本"。

### 8.2 纠正之后，仍然成立的三个真缺口（本泳道可改，已读码定位）

1. **写步点击的"探测 → 真点"之间没有再校验**（P1）。`injClick('probe')` 在页面里算完中心点后返回
   `{x,y}`，真事件由 `cdpInput.clickAt(tabId, probe.x, probe.y, {jitterRadius})` 注入（`primitives.js:646-647`），
   中间隔着**拟人贝塞尔轨迹的飞行时间**（可达数百毫秒）。这期间轮播/懒加载/toast 挪动页面，就会
   **点到一个从未被探测过的元素**，而返回值仍是 `{ok:true, channel:'cdp'}`。
   同行同题的答案：Playwright `_retryPointerAction` 在派发前 `scrollIntoViewIfNeeded`→
   `checkElementStates(['visible','enabled','stable'])`→`_clickablePoint`→`_checkFrameIsHitTarget`
   一连串都在**同一动作事务内**完成，且 `stable` 定义为**连续两帧 boundingBox 一致**（rAF ~16ms 轮询）；
   Skyvern 把 `elementFromPoint(center)` 的 `occluded` 判定做成动作失败后的一等探针，
   并把 remediation 文本写成"act on a freshly reported selector rather than retrying this one"。
   → 落地两小步：(a) `actionabilityCheck` 加 `stable`（在注入函数内部 rAF 双帧比盒，仍是单次 evaluate）；
   (b) **仅对 `is_write` 步**，在 `clickAt` 之后、返回 `ok` 之前补一次身份复核（selector 仍可解析 +
   中心点未变 + 该点 hit-target 命中同一元素），不满足则改写为 `element_moved` 交自愈，**不允许静默 ok**。
   > 状态（批14 后）：本条的**前提**被推翻了三分之一——读码 + 真机发现 probe 因为注入自包含断链
   > 在真机上从未成功过（§7.6 链头），所以「探测 → 真点之间」这段时间此前**根本不存在**。
   > 批14 落了「probe 不被兜底绕过」+「兜底不双发」（§7.6），(a) `stable` 与 (b) 点后身份复核
   > 两条仍开放，且现在才真正可测（真机 `channel=cdp` 已是可断言的列）。
2. **审批没绑载荷**（P1，D7 闸门）。我们的放行是一条布尔/行状态，同行一致把审批绑到"被批的具体载荷 + 版本"上：
   GitHub `dismiss_stale_reviews` + "records the state of the diff at the point when a pull request is approved"、
   Salesforce "Approvers see the values at submission time, not current changes"、
   Stripe `confirmation_token.expires_at` 且服务端只 redeem、browser-use `compute_action_hash()` /
   `_normalize_action_for_hash()`（非 None 参数 + `sort_keys` + `sha256[:12]`）。
   → 最小形态：审批行存 `approved_payload_hash`，执行前对**即将发出的**参数重算，不等即 **fail-closed**
   （拒绝并留痕，不是"再问一次"）；审批消费用
   `UPDATE approvals SET used_at=now() WHERE id=$1 AND used_at IS NULL AND expires_at>now() AND payload_hash=$2`
   的 0 行即不执行（LangChain `HumanInTheLoopMiddleware` 用 `tool_call["id"]` 回填、
   LangGraph `interrupt` 对 resume 值**完全不校验**是两个方向的正反教材）。
3. **`ok bool` 一列混装"跑了"和"成了"**（P1）。同行没有一家用单布尔：GitHub Checks `status` × `conclusion`
   两列、K8s `phase` × `conditions[]`、Stripe PI 七态（`processing` ≠ `succeeded`）、
   WhatsApp/Twilio `sent/accepted/delivered/read/failed`、OTel recording-errors 明令
   **"无错时 status MUST 留 Unset"**（OK 要调用方显式写）。我们 `browser_command_log` 只有 `ok bool`
   （`model/command_log.go:22`），而 §7.x 一路在防的"读页面自证成功"假绿，本质就是把 Unset 当成功。
   → 最小形态：命令日志加 `attempted/accepted/confirmed` 三态，`confirmed` 只能由**第二条通道**置位
   （换 selector / 新会话回读并匹配作者 + 内容哈希），扩展上报只允许写 `accepted`；
   回读前先比 `PageFingerprint(url, element_count, text_hash)` 式指纹，**指纹未变即判 `unverified`**。

### 8.3 差距矩阵与取舍（21 行，每条左列都经本泳道读码复核，未复核的一律不进；#11 为「被子 agent 证伪故不进矩阵」的反例记录；#20、#21 为批16b/批16c 自审新增、左列不是同行口径而是本仓缺陷形状）

`采纳`=本批改；`拒绝`=给出理由并留档；`BLOCKED`=落点在并行会话在途文件（`git status` 实测仍脏）。

| # | 同行口径（证据） | 本仓实测 | 取舍 |
|---|---|---|---|
| 1 | 派发前必须在**同一动作事务内**重算可点性，`stable` = 连续两帧 boundingBox 一致（Playwright `_retryPointerAction`: `scrollIntoViewIfNeeded`→`checkElementStates(['visible','enabled','stable'])`→`_clickablePoint`→`_checkFrameIsHitTarget`；Skyvern `classify_element_state` 的 `occluded` 用 `elementFromPoint(center)` 且 `top!==el && !el.contains(top)`） | `actionabilityCheck`（`primitives.js:12-26`）已有 visible / 零盒 / disabled / **遮挡**四项，**只缺 `stable`**（23-24 行注释自陈理由）。更关键：`injClick('probe')` 返回 `{x,y}` 后，真点击走 `cdpInput.clickAt(tabId, probe.x, probe.y)`（`:646-647`），**中间隔着拟人贝塞尔飞行时间**，这期间页面挪动即点到从未探测过的元素，返回值仍是 `{ok:true, channel:'cdp'}` | **采纳（P1）** A1（批14 落「闸门不被兜底绕过」+「兜底单发」并真机立证 `channel=cdp`；`stable` 与点后身份复核两条开放，见 §7.6 未落地段） |
| 2 | "跑了"与"成了"永不共列：GitHub Checks `status`×`conclusion`、K8s `phase`×`conditions[]`、Stripe PI `processing`≠`succeeded`、OTel recording-errors "无错时 status MUST 留 Unset" | `browser_command_log` 只有 `Ok bool`（`model/command_log.go:22`） | **采纳（P1）** A2 |
| 3 | 重试必须有上界并落终态：Sidekiq `DEFAULT_MAX_RETRY_ATTEMPTS=25` → `dead` ZSET（`dead_timeout_in_seconds` 6 月、`dead_max_jobs=10000`）、SQS `redrivePolicy.maxReceiveCount` → DLQ、River `discarded` | 出站集合语义本身正确（`FetchOutboundUndelivered` 只取 `pending` 或 `inflight && claimed_at<now()-30s`，`delivered/failed` 自然离开；`handler_http.go:236` 证实 SSE 轮询定时器确走此函数，未注入时按 `:211` 显式 Warn 回退游标）。但**没有任何尝试计数列**，于是"扩展每次都发不出去"（目标会话已不存在等）的行会以 30s 周期**永久重推**，无人升级为终态 | **采纳（P1）** A3 |
| 4 | 去重必须有界：SQS `MessageDeduplicationId` 5 分钟窗、Stripe 幂等键 24h、Azure "Retain each record at least as long as the broker can still redeliver" | 桥接扩展 `SentCache` 持久化到 `chrome.storage.local`（`downlink.js:24,48`，**非内存态**），但**只有条数上限 `sentCacheMax:2000`、无时间界**（`constants.js:248`），且 `add()` 命中已有 key 不刷新插入位（Set 语义）→ 淘汰纯按插入序 | **采纳（P2）** A4：加 **24h** TTL，不是 5min（服务端重推窗取决于浏览器离线时长，界必须 ≥ 上游仍能重投的时长，否则反而放大重复风险） |
| 5 | 审批绑载荷 + 一次性 redeem：GitHub `dismiss_stale_reviews` / "records the state of the diff at the point when a pull request is approved"、Salesforce "Approvers see the values at submission time"、Stripe `confirmation_token.expires_at`、LangChain `HumanInTheLoopMiddleware` 用 `tool_call["id"]` 回填、browser-use `_normalize_action_for_hash`（非 None 参数 + `sort_keys` + `sha256[:12]`）；反例：LangGraph `interrupt` 对 resume 值**完全不校验** | D7 放行不携载荷指纹（待读码定锚点后实现） | **采纳（P2）** A5 |
| 6 | 裁剪不等于证据消失：CloudTrail digest `logFiles[].{hashValue,hashAlgorithm}` + `previousDigestHashValue` + **`logFiles:[]` 空摘要可断言"该时段无事件"**；RFC6962 `inclusionProof{treeSize,rootHash,hashes}`；pg_partman `retention_keep_table=true`（detach 不 drop） | `PruneBefore` 整行删除（`command_log.go:45-60`），90 天界由 `BROWSER_AUDIT_RETENTION_DAYS` 定（`retention.go:20-27`）；删后**无任何自证手段** | **采纳（P2）** A6：删除前把 `(seq, row_hash, prev_row_hash)` 沉进永不裁剪的小表 |
| 7 | 租约靠心跳续期而非固定 TTL：pg-boss `heartbeatSeconds`/`heartbeatRefreshSeconds=hb/2`、SQS `ChangeMessageVisibility`、Temporal `HeartbeatTimeout`；且"完成写必须与租约同事务，被抢即回滚" | 需扩展每 10s 回写 `claimed_at`，是**双端协议改动**（HTTP 端点 + 扩展定时器 + 权限面），而现 30s 认领 + 命中缓存补确认已收敛，收益只是"少重推几轮"的延迟 | **拒绝**（半径/收益不划算，留档） |
| 8 | 发送前先落本地台账、事后去 DOM 核对自己的气泡（"已发出但 ack 丢失"的同行正解） | `SentCache` 已是持久化台账且 `reAckSentDuplicates` 两条路径都挂（轮询 `:315`、SSE `:804`），SW 重启场景已兜住；缺的只是"发成功后回 DOM 复核" | **暂缓**：需每个平台各写一条"我的气泡"判据，半径在 5 个适配器；列为下一批候选 |
| 9 | 入口去重键必须带会话维度、窗口必须 ≥ 上游重投窗、且与副作用同事务（Stripe/Azure/企微/飞书一致否证现状） | `inbox_ingress_ingest.go:126-132` 键内无 `conversation_id`、`InboxContentDedupTTL=5min`；hub 层内容命中无时间界 | **BLOCKED**：两文件 `git status` 实测仍为并行会话在途（`M` / `??`），本泳道不改，维持 §6 移交 |
| 10 | `officialEventID` 优先级高于内容哈希 | `webhook_event_key.go` 未跟踪（在途），且它含 `event_type` → 同一次推送多事件会算多条；`self/agent` 巡逻回环消息根本无 event_id，仍需内容兜底 | **BLOCKED + 认知修正**：§6-3 说的"正确方向"不等于"能覆盖我们主要流量" |
| 11 | agent 报告称 `BridgeOutboxMessage.Extra` 是 `json:"-"`（扩展拿不到 `dm_target`）、称存在 `reAckDeliveredOnCacheHit`/`InboxConversationID`/`ErrOutboundAckScopeMismatch` | 实测：`Extra map[string]any json:"extra,omitempty"`（`channelgw/protocol.go:157`）且 HTTP 侧 `handler_http.go:770` 真的带出 `extra`；后三个符号**全仓 0 命中** | **不进矩阵**：子 agent 对本仓的断言被证伪，本轮第 2 次。教训回灌记忆 |
| 12 | 「闸门所依据的记录本身可以静默失败」是同一类缺陷：K8s `sideEffects` 把 `Unknown` 与 `Some` 同等对待、admission `failurePolicy` 默认 `Fail`；审批/幂等建立在"可能没写进去的行"上等于没有 | `recordSubmitState`（`write_ledger.go:39-48`）写失败**只 Warn、不返回 error**，四个调用点（`executor.go:939/969/982`、`write_ledger.go:207`）拿不到失败事实 ⇒ 「send 已跨越但台账没落」时下一轮 `FindSubmitAttempt` 查空，双发闸门整体消失，而 §3.1 给这张表的定性是"重发闸门的唯一事实来源" | **采纳（P1）** A7：`recordSubmitState` 返回 error，写失败即给该 session 置 `writeLedgerBroken`，其后所有写步**在派发前**拒绝（零帧下发）、错误文案含"台账未落，本会话写能力已降级"。不对称决定取舍：写步失败**可见、可人工重跑**，双发**不可见、不可撤销**。**批16 已落（§7.8）**：另加两条定稿时才想清楚的收口——`sent` 写失败按「越点未落账」记进程内兜底（会话级标志挡不住同进程换 session 的自动重试），且台账没落成的写步**不得再报 success** |
| 13 | 闸门查询失败要 fail-close（同上，`failurePolicy: Fail` 就是这条默认值） | `guardResubmit`（`write_ledger.go:71-74`）`err != nil` 时 `Warnf` + `return nil` **放行**；`:61-62` 注释明写这是刻意的（"DB 抖动不该把整条自动化链路锁死"）⇒ 属**决策复审**而非隐藏 bug：同行口径下"抖动期闸门消失"正是它付不起的那一侧 | **采纳（P1）** A8：写步判定的这条分支改 fail-close，与 A2 的 `send_gate` 早返同形（步判 failed、不记尝试）；只改这一条，`appendCommandLog` 的 Ignore 语义不动（那是增强件不是闸门）。**批16 已落（§7.8）**：查询失败上抛「双发闸查询失败……拒绝下发」，走既有"步判 failed、台账不动"分支 |
| 14 | 审批必须进审计链：GitHub deployment review / CloudTrail 的"谁、何时、对哪份载荷" | `SessionService.Confirm`（`service/session.go:99-108`）只调 `SignalConfirm`，**一帧都不落**；而 `direction=judge` 这一类**早就存在**（`executor.go:527-529` 写 judge 帧、`controller/session.go:79` 已支持 `?direction=judge` 过滤、I5 导出取全量） ⇒ 缺口只是"没人给它写"，不是"没地方写" | **采纳（P2）** A9：放行成功补一帧 `judge`，payload **只放 `text_hash`**（`HashWriteText`）不放正文——I5 导出会把正文带进离线件 |
| 15 | 挂起态要可跨进程查证：Temporal 的 signal 落 history、任何 worker 都能投递；LangGraph 的 interrupt 进 checkpoint store | D7 挂起是**进程内**裸 map（`executor.go:74-75` `confirmRegistry`）+ `ConfirmPending bool gorm:"-"`（`model/session.go:37` 不落库）⇒ 多副本时 confirm 落到别的副本只会得"没有待确认的提交点"，与"根本没开闸门"同一句文案；进程重启后挂起协程消失，靠 `stale_reconcile.go` 对账收敛（不砖化，但人看不到原因） | **采纳（P2）** A10：挂起时补一帧 `d7_wait{expires_at, step_index, text_hash}`，`SignalConfirm=false` 且库里存在**未到期** `d7_wait` ⇒ 回 `gate_on_another_instance`。**零 DDL**（`browser_sessions` 连 `updated_at` 都没有，加列会牵动 `completed_at` 口径）；`expires_at` 只当"写下的期望"，必须与内存真值 AND 起来用 |
| 16 | 「取不到副作用分类」不等于「无副作用」（承 12 的 K8s `Unknown`） | `isWriteStep`（`write_ledger.go:119-142`）在 `:123` 写作 `locs, _ := platformStepLocators(task)` **把错误丢掉**：平台适配器未注册/取表失败时三条推导全体不命中，只剩 `post_comment` 与显式 `is_write` 声明 ⇒ **retries=0、双发闸、D7 三道同时静默消失**。`:153-154` 注释只论证了"不会误判成写"，没论证漏判的代价 | **采纳（P1）** A11：取表错误升为第三态 `unknown`，处置等同 `irreversible`；**收窄条件**：只有"取表报错"才是 `unknown`，单纯没命中 locator 仍判 `none`（保住批7 刻意收窄的那条：交互搜索腿不得被判写而白丢重试能力）。**批16 已落（§7.8）**：`classifyStepEffect` 三态 + `stepCouldBeSubmit` 只升级"本来可能被推导出写"的形态，`is_write` 落库为真 |
| 17 | 重投要保留同一身份（Pub/Sub "A redelivered message retains the same message ID"），而**内容当身份**的前提是"同一条内容不会第二次真实出现"——这个前提在聊天里不成立 | `computeMsgID` 就是 `contentHash(channel\|conversationId\|content)`（`user-web/bridge/src/core/uplink.js` `enqueue` 里缺省填 `event_id`），服务端钩子2 按 `msg_id + conversation_id` 判等（`inbox_ingress.go:457-484`）⇒ **同一会话里第二条"好的"必然被吞**，且这是"说了没回"在 B 链路里比 Redis 更早、更永久的一层 | **登记待对齐**：改点在干净的 `uplink.js`，但 `computeMsgID` 与服务端 `ContentHashMsgID` **严格同源**（函数注释自证），单边改会把幂等判定裂成两套；`types.js` 的跨语言契约注释也得同步。验收口径先立：同会话两条同文本 ⇒ 2 行；同一条重投（DOM timestamp 不变）⇒ 仍 1 行 |
| 18 | "接受但不再执行"是默认档，"根本不落库"几乎没人这么做（sidekiq-unique-jobs 把**锁时机**与**冲突怎么办**拆成两个维度：`until_executing`/`while_executing`… × `on_conflict: :log/:raise/:reject/:replace/:reschedule`） | `decision.Blocked` 时两条分支（单条 `inbox_ingress.go:494-507`、批量 `:944-957`）都在 `persistMessage` **之前** `return` ⇒ 消息**不入库**，只回一个 `Accepted=true` 的好看回执 ⇒ 排障时"客户说了没回"在库里查不到任何痕迹（证据消失） | **BLOCKED**：两文件均为并行会话在途（承 §8.3-9 同一移交面）。移交口径：`IsDup` 分支改"入库 + 抑制 AI"、`IsSelfEcho` 维持不落库（回声落库会污染会话） |
| 19 | 幂等层命中应**回放首次结果**而不是重新判定，且"是否重复"要由结论决定而非文案（Stripe 存首次 status+body 原样回放） | `IsDuplicateReason` 用子串嗅探 reason，而 reason 里混得进**落库失败原文**（§8.1-7）⇒ 误判方向是"永久停发" | **采纳（P0）批15 已落**：判定收成结论短语前缀 + 永久断言 + 两条反向腿（§7.7）。**正解仍是 outcome 枚举**，要改 `InboxIngressResult` 及全部产出点 ⇒ 落点在在途文件，随 18 一起移交 |
| 20 | 自审发现（非同行调研轴，批16b 二次审核 B4）：防不可逆动作的"先查后做"若两端都在应用层，两个并发执行流就都能查到"没做过"再各自做一遍——正解是把判定下推到存储层的**唯一约束**（Postgres 部分唯一索引天然具备，本仓该目录却只有 `model/cron.go:15` 一处 `uniqueIndex`） | 双发闸是**读后再写**：`guardResubmit` 先 `FindSubmitAttempt`（`repository/step.go`，一条普通 `First`，无 `FOR UPDATE`、无 advisory lock），通过后才由 `recordSubmitState` 写 `prepared`/`sent`；而同一时刻并发的另一条腿看到的是同一条"查不到"。同层的三条并发防护也全是同一形状：`t.Status == "running"`、`CountRunningByTask`、`CountRunningByUser`（`task.go:312-334`，v1 每用户同时 1 个 running session ⇒ 现实窗口很窄，但注释自陈"**靠 DB 唯一性兜底竞态**"，而 `browser_tasks`/`browser_sessions` 上并不存在那样一条约束） | **不采纳"再补一道 check-then-act"**：那只是把同一形状的闸门复制一遍。诚实收口是给台账加**部分唯一索引** `(task_id, text_hash) WHERE submit_state IN (sent, unattributed, verified) AND deleted_at IS NULL`，把"这次提交是否已被记过"交给插入语句本身判定（撞约束即拒发）——代价是它同时把软删语义变成契约问题（软删行参不参与唯一性、`PruneBefore` 类裁剪会不会腾出键位 ⇒ 与 #6 的"裁剪不等于证据消失"连体），属 DDL 决策而非本泳道随手改。**登记 A12（待拍板，不阻塞本批）**：本轮以文档记账，理由是"当前并发面被 per-user running 闸压到极窄"+"改法牵动软删与裁剪两处口径"。若将来放开并发（每用户 >1 session）或引入多副本 worker，A12 立即从"可缓"变为"必做"，届时必须重跑本批电池再谈 |
| 21 | 自审发现（非同行调研轴，批16c 二次审核 B1）：**同一个判据符号被多处消费，测试却只覆盖其中一格**——评审里最常见的假绿形状是"摘掉整块会红"，它被当成了"每一格都有腿"的证据；正解是按消费点逐个下刀，让每条腿各自证明它有牙 | `classifyStepEffect` 的三态由一个符号 `writeStep := effect.needsWriteGate()` 同时喂四处（钳 retries、降级+闸门键+双发闸、D7 确认、成功/失败两路落账）。批16 只在"钳 retries"那一格有腿（`TestWSE2E_UnknownLocatorTableTreatedAsWrite` 断 click 只到线 1 次），把符号收窄回 `effect == effectWrite` 时另外三格（双发闸、降级拒绝、D7）在 unknown 步上一条断言都不走过 ⇒ 一次"不知道有没有副作用"的动作被原样重发 | **已落（批16c）**：三条腿逐格钉住 unknown 步的双发闸/降级拒绝/D7 闸门，电池不再打"一包"而是**逐消费点下刀**（M27 双发闸+降级、M28 D7、M29 只摘降级拦截、M30 只钳 retries、M31 只漏失败路径落账，M20 保留为"四道一起摘"的对照），从而证明每条新腿各自有牙。**通用口径回灌记忆**：一次"摘掉整块全红"不能登记为覆盖，必须按消费点拆刀 |
