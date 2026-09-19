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
