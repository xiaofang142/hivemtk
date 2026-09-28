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

1. **`type`+回车不再一律判写**。初稿的「回车即提交」会把任何表单回车（搜索框、筛选框、登录框）都算成不可逆写，于是这些步被剥掉重试能力、并在重试轮被跳过；而「哪个输入框是评论框」只有 L3 适配器定位表知道。收窄到「目标命中注册 `comment_input` 候选段」后，判据挂在平台知识上而不是挂在一个人人都用的参数形状上。诚实记录：批7 当时**现有真机腿里没有 `type+submit_on_enter` 步**（搜索腿是「键入后另起一步点搜索按钮」），所以这条收窄是设计判断，不是被某条现成腿测出来的回归——它由 `TestIsWriteStepAttribution` 的两条对照用例（命中评论框=写 / 命中搜索框≠写）钉住。（批18 补上了这条缺失的腿：真机 B18-1/B18-3 各自把「命中注册评论框⇒落 `sent`」与「搜索框⇒`is_write=false` 且 `submit_state` 留空」在真实扩展上跑成了读数，见 §7.12 四。）
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
> （批17 状态回写：本段描述的"仍未做"已结，(a)(b) 两条落地、真机与电池实证见 **§7.11**；
> 本段保留为批14 当时的取证边界，不作为现状描述读。）

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

## 7.11 批17：可点性补 `stable` + 写步点击后身份复核（§8.2-1 两条落地，含二次检查抓出的一处闸门误伤）

**本批动了 `primitives.js`**——§7.10 末尾立的那条触发条件当场生效，所以真机腿 L-A/L-C 不是加分项，
而是门禁的一部分。生产改动面比前几批大：扩展侧三处（三份内联 `settleBox`、`injClickIdentityCheck`、
`asIdentityVerdict`）+ Go 侧三个文件（`hand.go` 打帧标志、`executor.go` 把 `is_write` 喂给复核并落
`identity_checked`、`write_ledger.go` 的 `isNeverExecuted` 补一族）。

### 一、(a) `stable`：判据一份、内联三份，且拒在零副作用上

`settleBox`（`primitives.js:44-59` / `:286-301` / `:514-529`，三份内联）的判据是 Playwright 口径的形状：
**连续两帧 `getBoundingClientRect()` 完全一致**才算落位，不等即 `{error:'unstable'}`。三处设计细节都有理由：

- **节拍用 rAF、并挂 `setTimeout(fin, 50)` 兜底**：真机取证 tab 是 `active=false` 的隐藏页，隐藏页不出
  rAF 帧，只等 rAF 的版本会在隐藏页里**永挂**到 15s 注入竞速窗超时。真机腿 ALIVE-3 直接把这条读成证据
  （`document.visibilityState == 'hidden'`），所以**定时器兜底才是本批真机上实际在跑的那条分支**——
  它不是"rAF 之外的备胎"。
- **先比盒、后查 deadline**：被节流的隐藏页里两帧之间就是 50ms，静止元素第一轮即通过；把 deadline
  查在前面会让"隐藏页里的静止元素"永远多等一轮。
- **500ms 上限远小于 15s 注入窗**：超窗会被切成 `*_inject_timeout_`，那是"点击从未发生"的归因，
  被 `stable` 借用就是假归因（文案不同、台账后果也不同）。
- **拒了还要重查一次可点性**（`:71-73`）：等待期间浮层可能刚渲染完，拿旧那帧的遮挡结论去点新位置的
  坐标等于把 hit-target 作废。

拒绝的形状是**派发前**拒绝：`{error:'unstable'}` → `element_not_interactable: unstable`（第三份是
`send_button_not_interactable: unstable`），**一帧坐标都不下发**。真机腿钉住两面：页面侧
`#moved` 零事件（L-A2/L-A6，证明确实没点）+ 命令帧恰好 1 条（L-A3，证明没被降级成兜底下发）+
同页静止按钮照常一次 `isTrusted=true` 的 click（L-A4/A5，证明没误伤）。

**为什么内联三份而不是抽共享函数**：注入函数跨序列化边界只过 `func.toString()`（§8.1-3 / 记忆里的
批14 链头教训），共享函数会在真机上静默断链。代价是三份会漏改，所以补了两样牙：
JS 侧断言"`unstable` 判据在三处 probe 里各出现一次"，加上电池**逐格下刀**（M1/M2/M3 分别在
`injClick`/`injClickNear`/`injPostCommentSend` 那一格装死），避免"摘一份会红"被当成"三份都有腿"。

### 二、(b) 点后身份复核：只给写步、只跑在 CDP 之外、失败绝不回头再点

`injClickIdentityCheck(target, probeX, probeY, tolerancePx)`（`:234`）只复核页面侧此刻观测得到的三件事：
selector 仍可解析、中心点未挪出 `IDENTITY_RECHECK_TOLERANCE_PX = 5`、该点 hit-target 未被别的节点接管；
任一不过即 `element_moved: …`。接线三段各有理由：

- **`cmd.verify_identity` 由 Go 侧的 `stepRow.IsWrite` 喂**（`hand.go` 的 click/click_near 帧 +
  `executor.go:956/977`）：读步不付这次额外注入的时延预算（电池 M9 / 真机 L-C1：只读步
  `identity_checked` 缺省且照样点了一次）。判据本身也要可信，所以 L-C3 直接读库比 `is_write`。
- **复核跑在 CDP 的 `try` 之外**。这是本批最要紧的一条形状：复核若放在同一个 `try` 里，
  "复核不过"会被 catch 误读成"CDP 不可用"→ 走 DOM 兜底**再点一次**——正是批14 花一整批消灭的
  双发形状。`asIdentityVerdict`（`:747`）因此单独归因：复核自己给出 `element_moved` 就原样上抛
  （那是结论），其余异常切成 `identity_recheck_failed`，**都不回落到兜底**。电池 M4/M5/M6 三格
  分别钉"删掉复核""复核失败改走兜底""复核跑不动时静默 ok"。
- **`navigated` 时跳过中心点判据**（`:850`）：页面跳走不是元素挪位，用中心点判它会把一次成功的
  提交报成 `element_moved`。这条腿的注码（M8）一开始**存活**，原因是测试自己的 DOM 没变——
  去掉 `!navigated` 后没有任何可观测差异，是**我错它不红**的反面（测试不够真）。修测试
  （在 `cdp.clickAt` 里换掉 `document.body.innerHTML`）后 M8 才被杀。
- **`click_near` 是第二个消费点**，用 probe 自己回传的 `probe.selector` 再解析（`injClickNear` 新增
  `pathOf()`：优先 `#id`，否则 `tag:nth-of-type` 路径）。单独下刀（M10/G5/G6），因为"click 有复核"
  推不出"click_near 也有"。

真机 L-B 组钉的是最坏形状的否定面：元素在 `pointerdown` 上自己挪走 ⇒ 报 `element_moved`（不是
`identity_recheck_failed`）、台账 `unattributed`、命令帧恰好 1 条、页面侧 `pointerdown=1` 而
`click=0` ⇒ **复核失败没有换来第二次点击**。

### 三、二次检查抓出的真缺陷：派发前的拒绝被记成了"提交尝试"

第一轮 27 条腿全绿之后，深查把 `*_not_interactable` 的台账后果读了一遍，发现一处**闸门误伤**：

| 事实 | 证据 |
|---|---|
| 一次被浮层遮住的写步，步判 `failed` 但 `submit_state=unattributed` | 真机库读 step **2287**（`element_not_interactable: covered`） |
| 同一种"派发前拒绝"在另一条路径上却是 `prepared` | step **2308**（`post_comment` 发送闸门）⇒ 同一事实两套口径，说明 `unattributed` 不是设计而是漏 |
| `unattributed` ∈ `StepSubmitAttemptedStates()`，而 `guardResubmit` 明写"unattributed 也拦" | `write_ledger.go` 双发闸 ⇒ 唯一正确的处置（等页面停下再跑一次）被**永久**拦死，直到人工改文本或换新任务 |

修法是一行判据（`isNeverExecuted` 补 `strings.Contains(msg, "_not_interactable")`）+ 两处锁：

- **前提要可查，不能靠猜**：这些文案只由三份页面内 probe 产出，且产出点全在 `cdpInput.clickAt`
  之前——所以补一条 JS 静态腿锁死"SW 侧 `dispatch` 那一段不许出现 `*_not_interactable`"
  （同时断源码别处确有命中，防"因为不存在所以永远绿"）。它的牙由电池 M11 证：在 SW 侧合成一句
  `*_not_interactable` 即判红。
- **Go 侧三例反证**（`TestPreDispatchRefusalRecordsNoAttempt`：`unstable` / `covered` /
  `send_button_not_interactable`）先跑出 RED（三格都打
  `submit_state="unattributed" 落在「已尝试」集合里`），加那一行后 GREEN；电池 G7 再证明
  "把 `element_moved` 也一并判成从未发生"会红（`unattributed` 那一族不许顺手扩大）。

**刻意不修的三处**（写清楚，不算漏）：① **写步点击后发生跳转 ⇒ 复核按设计跳过**
（`:850` 的 `!navigated`），此时 `identity_checked` 如实留空——"点中了什么"在跳转之后没有页面侧
观测点，那条事实归 §7.7 的提交后复验负责，中心点判据不冒充它。M8 与那条 navigated 腿
（断 `identity_checked` 为 `undefined`）一起钉住这个形状，防止有人把"跳过"读成"漏了"。
② `element_moved` 继续留 `unattributed`——它发生在派发**之后**，动作已经发生、结果未知，
记成"从未发生"就是把双发留给下一轮（G4 专门钉这一条：把它并进 `isNeverExecuted` 那族会当场红）。
③ `click_near` 没有导航探测（批14 之前就有的形状，本批不扩半径），所以"发送后跳转"的站点上
它可能报 `element_moved` → `unattributed`；方向是保守的（宁可判红也不假绿），要改得先给
`click_near` 补 `navigated` 判定，属另一批。

另有一类**观测不到**的：同位置被换成同 selector 的另一节点，除非页内埋身份令牌，无从分辨——
`injClickIdentityCheck` 的注释里原样记着这条边界（§8.2-1 的状态块也记了，防止下一批以为它已覆盖）。

### 四、证据（全部跑出来的）

| 层 | 跑法 | 读数 |
|---|---|---|
| 扩展单测 | `user-web/browser_automation` 全量 vitest（不带过滤） | 13 文件 / **149 条全绿**（含批17 的 `batch17-actionability.test.js` 18 条）；`npm run build` 后 `dist/background.js` 与构建前那份逐字节相同（md5 `6371b327…`）⇒ dist 可复现 |
| Go 门禁 | 干净 `git clone --shared` @ HEAD `8f07f792` + 泳道八文件覆盖（逐文件 md5 与工作树对得上）；`gofmt -l` 泳道**空**、`go build ./...` rc=0、`go vet` rc=0、`go test -test.v ./internal/browser_automation/...`（**不带 `-run`**） | rc=0，`PASS=152 FAIL=0 SKIP=0`（controller 1.178s / platform 1.573s / service 175.360s） |
| 反向证据 | `scripts/mut_actionability_b17.py`：JS M1–M11（三份内联各自一格 + 复核的六种坏形状各一格）+ Go G1–G7（帧标志 / 传参 / 落库列 / 两类误判成"从未发生"） | 控制组 JS `passed=18 skipped=0`、Go `ran=15 skip=0`；**18 格逐格被杀、无存活**，每格还原后 md5 与基线一致；**最终树（门禁与真机腿跑完之后）整趟重跑一次**：18 格仍逐格被杀、
控制组读数不变（`/tmp/b17_mut_final.log`，`battery rc=0`）。同族提示只有一条：G1 与 G2 杀掉同一用例——它们是同一事实的两条线（帧里有没有 `verify_identity` / Go 有没有把 `is_write` 传进去），登记不拆 |
| 真机腿 | 真实夹具 Chrome + 真实扩展 + 该克隆构建的服务端（`/tmp/user-server.b17`，起服后 `host/status` 报 `online/servable=true`、pid 16732、version 1.5.0）；只打 `127.0.0.1:1864x` 本地夹具页，**不碰真实平台** | **29/29 PASS**（session 592–596，`/tmp/b17_device_legs6.log`） |

**门禁为什么必须在克隆里跑**（本批现场撞到的）：共享工作树此刻 `go build ./...` 直接红
（`internal/service/bridge_offline_replay.go:83 undefined: atomic`，并行会话在途文件），
在真树上跑门只能得到"整树红"这一条无归属信息。克隆里 `HEAD + 泳道八文件` 才是本批的树，
md5 逐文件核过才算"电池/门禁打的就是要提交的那份"。

### 五、取证侧自己被抓的两处（不记下来，这条腿下次还会红得没道理）

- **`sql()` 把"读数为空串"和"psql 失败"糊成同一个 `ERR `**：L-A7 断的恰恰就是空串
  （台账留空 = 不算提交尝试），于是那条腿**永远 FAIL**。改成按 `returncode` 分流，并用
  `CASE` 把 `''` 与 `NULL` 分辨出来（两者对闸门含义相同，但读数不能把形状糊掉）。
  这是 §7.10「取 rc 的那一行代码本身要审」的同一族——**取读数的 helper 也不能把两种形状并成一种**。
- **夹具会自己盖住自己要测的元素**：`#moved` 的位移幅度（x≤38px）在三个按钮同行紧排时
  恰好盖住相邻 `#still` 的中心点 ⇒ L-A4/ALIVE-2 随机红，红因 `element_not_interactable: covered`。
  **红的是取证面，闸门在正确地工作**（中间那趟 26/29 反过来白送了一条遮挡腿的真机证据：
  step 2358 与 2356 一样留空 `submit_state`）。三按钮分行后 29/29 稳定复现。

## 7.12 批18：`type + submit_on_enter` 的提交键失败被吞掉（一次报绿 + 把自己永久锁死）

### 一、缺陷形状（读码读出来的，三条后果各自有代码落点）

扩展侧 `case 'type'` 的旧写法把 `cdpInput.pressEnter()` 挂在**键入那个 try 里面**，并且写成
`await cdpInput.pressEnter(tabId).catch(() => {})`，try 之后无条件 `return {ok:true, channel:'cdp'}`：

| 环节 | 落点 | 后果 |
|---|---|---|
| Enter 没发出去（CDP 被 detach / 该帧销毁 / ack 前预算用尽） | `.catch(() => {})` 把失败原地抹掉 | 这一步照样回 `ok` |
| Go 只看回包 | `status=success` | 编排面与审计面全绿，页面上一条评论都没有 |
| 该步是**写步**（`type`+`submit_on_enter` 命中注册评论框） | `write_ledger.go:279` 判写 → 成功即 `submit_state=sent` | `sent ∈ StepSubmitAttemptedStates()`，`guardResubmit` 拦同任务同文本 |

三条连起来是最坏的一种组合：**一次根本没发出去的评论既报绿，又被自己的台账锁死**——唯一正确的
处置（看清页面再跑一次）恰好被拦下，直到人工改文本或换新任务。真实平台上的用户形态就是
"系统说发成功了，但那条评论不存在，而且再也发不出去"。

为什么前 17 批的网没接住它：批14 审的是 `click` 兜底双发，批15 给 `post_comment` 补了提交后回查
（它有 `comment_verify` 这条只读通路，能把"绿"建立在页面证据上）。而 `type+enter` **没有回查通路**，
它的"绿"完全依赖 `pressEnter` 自己肯不肯抛错——那个信号正是被 `.catch(() => {})` 抹掉的唯一一个。

### 二、修法与两条边界（都在扩展侧，Go 侧一行没改）

把提交键从输入 try 里挪出来，失败上抛 `submit_key_not_dispatched: <原因>`，且**不许**被兜底吞掉。
Go 侧不加新分支：这个文案不在 `isNeverExecuted` 那族里 ⇒ 走 `unattributed`（记尝试、拦重发、交人工核），
这正是本案该有的语义——提交键可能已部分送达页面，判"从未发生"就是把双发留给下一轮。本批做的是
**用测试把这套已有语义钉住**（含一条跨语言锁：Go 直接读 `user-web/.../primitives.js` 那一份源码，
文件读不到就 `t.Fatalf`，不许 skip 成绿）。

两条边界是这个 fix 的半径，都各有一格变异专门防"修过头"：

- **兜底通道不补按**：只有 `res.channel === 'cdp'` 才按 Enter。`injType` 的 fallback 分支在页面侧
  已经发过 Enter 了，兜底之后再补一次 CDP Enter = 一次步骤两次提交（J3/J5）。
- **失败后不回头重键**：提交键的 try 在输入 try 之外。若把整个 case 包进同一个 catch，`pressEnter`
  抛错会去触发 `injType` 兜底重键一遍正文，把"没发出去"修成"填了两遍"（单测③断
  `cdp.typeText`×1 / `executeScript`×1 / 页面 `input` 事件×1）。

### 三、刻意不做的三处（写清楚，不算漏）

① **不给 `type+enter` 补提交后回查**：它没有平台注册的评论容器定位器可查（`comment_verify` 依赖
`locs.CommentContainer`，搜索框一类输入根本没有"我的提交会出现在哪里"的观测点）。硬造一个回查只会
生产一种新的"看起来绿"，正是批6 立项要消灭的形状；要证明发布，编排层该走 `post_comment`。
② **不在 Go 侧给 `submit_key_not_dispatched` 单开一个台账态**：`unattributed` 的定义就是"尝试过但
归因不到"，新增态只会让拦阻集合多一处要人记得的漏（G1 专门钉：把它并进 never-executed 会当场红）。
③ **`comment_send` 仍没有点后身份复核**：批15 的 verify 轮询是它的假绿防线，与本案不是一条线；
本条作为**刻意边界**登记（§8.2-1 状态块同步记），防止下一批以为已覆盖。

### 四、证据（全部跑出来的）

| 层 | 跑法 | 读数 |
|---|---|---|
| 扩展单测 | `user-web/browser_automation` 不带过滤全量 vitest | 14 文件 / **155 条全绿**，进程 `rc=0`（`/tmp/b18_vitest.log`；批15 那次"129 条全绿 + rc=1"的教训：rc 要单独取，不能只看汇总行），含新增 `batch18-submit-enter.test.js` 6 条（其中 3 条先跑出 RED 再实现；第 4 条是"不许修成双发"的过修锁，前后都绿是它的正确形状） |
| 构建可复现 | `npm run build` 前后 `dist/background.js` md5 | 源未改时重构建 md5 不变；本批源改了，md5 `6371b327…`→`83dd3d6d…`（构建确实吃了这份改动） |
| Go 契约腿 | `submit_enter_b18_test.go`：跨语言字面量锁 + `TestWSE2E_SubmitKeyFailureRecordsAttempt` | 断到线 `type` 帧恰 1 条、`is_write` 落库、`status=failed`、`submit_state=unattributed`、`FindSubmitAttempt` 查得到 |
| 变异电池 | `scripts/mut_submit_enter_b18.py`：J1 恢复吞掉 / J2 换成 never-executed 文案 / J3 去掉 cdp 通道判据 / J4 挪回输入 try / J5 无条件按 / J6 从不按 / G1 `isNeverExecuted` 认这个文案 / G2 通用写台账默认不记 / G3 只改 JS 字面量（跨语言那格） | **9/9 逐格被杀、无存活**，每格还原后与工作树 md5 一致；控制组 JS `rc=0 passed=6 skipped=0 红名=[]`、Go `rc=0 ran=2 skip=0 FAIL=[]`。**门禁与真机腿跑完之后整趟重跑一次**（`/tmp/b18_mut_final.log`，此时 HEAD 已被并行会话推进两格），判定与首跑一致 |
| Go 门禁 | `git clone --shared` 克隆 `/tmp/b18gate` @ HEAD `3693e1da` + 泳道文件覆盖（逐文件 md5 对得上工作树）；`gofmt -l` 空、`go build ./...` rc=0、`go vet` rc=0、`go test -test.v -count=1 ./internal/browser_automation/...`（**不带 `-run`**） | rc=0，`PASS=154` 顶层（+21 子测试）、`FAIL=0 SKIP=0`（controller 3.420s / platform 3.551s / service 197.519s）。克隆在提交前刷新过一次 HEAD（并行会话连推两格），刷新后重跑才是"要提交的那棵树"的读数 |
| 装机身份 | CDP 读 SW 内 `fetch(runtime.getURL('background.js'))` 的 sha256 与 `performance.timeOrigin` | sha256 与本地 `dist/background.js` 相等（`bd023917…`）、`timeOrigin 07:28:07 > dist mtime 07:20:26`（`chrome.runtime.reload()` 之后）、`b18_throws=true`、`legacy_swallow=false` ⇒ 真机腿打的确实是这份代码 |
| 真机腿 | 真实夹具 Chrome + 真实扩展 + 泳道服务端（`:8299`，`host/status` 报 `online/servable=true`、pid 37694、v1.5.0）；只打 `127.0.0.1:18645` 本地夹具页，**不碰真实平台** | **14/14 PASS**（会话 597–600，任务 540–542，`/tmp/b18_legs.log`）：一次步骤恰好 1 次 `isTrusted=true` 的 Enter、提交时页面读到的正文=本步要发的内容（键入与回车没被 refactor 打乱）、台账 `success\|sent\|true\|<hash>`、命令帧 1 条；同任务重跑被双发闸拦下且**一帧都没下发**、拦下的那行台账不动（`is_write` 仍 true、`submit_state` 仍空）；搜索腿照常提交但不进写台账；不要求回车时零 Enter、评论区零条目 |

失败形状本身（`pressEnter` 抛错）在真机上不可造——要精确掐断一次 CDP 命令——由电池 J1/J2/G1/G3 承担，
这一条分工写在真机脚本抬头里，免得下次以为真机漏了一条腿。

### 五、取证侧自己被抓的四处（不记下来，这批的腿会红得没道理）

- **双发闸是任务级自然键**（`task_id + text_hash`，`model/step.go:27`）：第一版给第二条腿**新建**了一个
  任务，那样就算绿也什么也没证明（闸门根本查不到上一轮），实际表现是断言 `skipped` 却拿到 `success`
  的一条莫名红。改成重跑同一个 task id。
- **拦下后的步态是 `failed` 不是 `skipped`**：`guardResubmit` 的三态里，`task.RetryCount==0`（人工重跑）
  走"人要看到被拦下这个事实"的分支（`executor.go:762`），只有自动重试轮才 `skipped`。照抄批17 的文案
  断 `skipped` 会把一个正确工作的闸门读成坏掉的闸门。
- **台账四列直拼会把"查不到这一行"和"列值为空"糊成同一个空串**：`submit_state`/`text_hash` 是可空列，
  Postgres 里 `'x'||NULL` 整行变 NULL，psql 对 NULL 行输出空行。改成逐列 `coalesce` + 显式 `NOROW`
  （§7.11 同族：取读数的 helper 不能把两种形状并成一种）。
- 附带一条工具侧的：**ALIVE 探针不能靠 esbuild 之后的函数名**——`injClickIdentityCheck` 在 dist 里计数为 0
  （局部函数名被改写），换成字符串字面量 `verify_identity` 才读得到；`legacy_swallow` 那条正则则专门
  盯着"旧形状有没有回来"。

## 7.13 批19：`comment_send` 的落点抖动半径漏传（三份同形 probe 里第三份少一个字段）

### 一、形状

批17 给三份内联 probe（`injClick` / `injClickNear` / `injPostCommentSend`）定了同一份规格：回传
`{x, y, selector, jitter_radius}`，Go 侧据此把点击落点在元素内随机偏移。第三份是 批19 之前新加的，
`return` 里**没有 `jitter_radius`** —— 字段缺失不是错误，CDP 照样点到 (x, y)，于是 `comment_send`
的落点静默退化成"永远点正中心 ±3px"。这不是崩溃类缺陷，是**拟人度归零而全链路无一处报红**：
Go 读到 `jitter_radius` 为 0/undefined 就用小容差，审计面只看 `ok`。

同族风险来自内联约束本身：注入函数必须自包含（§7.6 的断链教训），所以三份抽不成一个共享函数，
"看起来一样"就真的一样了。判据只能挂在**每份各自回传了哪些字段**上。

### 二、修法与取证

`src/core/primitives.js` 的 `injPostCommentSend` 补 `jitter_radius: Math.min(r.width, r.height) / 2`
（与另两份逐字同式）。锁它的是批17 电池里专门加的一格 **M12**：把这一行删掉（用带前导换行的锚点，
只命中第三份），`test/batch17-actionability.test.js` 必须红 —— 静态字段形状锁 + 行为锁（同一份用例
里断 comment_send 的落点确实离开过中心）配成对。

本轮（批19h 收口时）在私有副本里把整趟 JS 电池重跑过一次：控制组 `rc=0 passed=20 skipped=0`，
M1–M12 **12 格逐格被杀**（`/tmp/b19h_b17js_rerun.log`）。M12 点名的两条正是本批的判据：
「comment_send 的落点抖动半径必须跟着按钮尺寸走」与「三份 probe 各回传一次、dispatch 里三处消费
（第三份漏传就是静默退化）」。

## 7.14 批19b：依赖归属门（`Create` 漏校验 + 执行期把别人的状态写成 409 文案）

### 一、形状（一个不变式的两条入口，只有一条查了）

`depends_on_task_id` 由请求方任意指定 ⇒ 必须验它属于当前用户。`SetDependency` 查了（`task.go:227`），
`Create` 没查，直接写库。两条后果：① **读侧外泄** —— 执行期 `checkDependency` 用只按 task_id 查的
`GetLatestByTaskID`/`HasSuccess` 读别人的会话，并把它的状态原样写进 409 文案（「前置任务最近一次执行
状态为 %s」），成为一台可枚举 id 的跨用户状态探针；② **写侧脏数据** —— 这条依赖永远不可能被合法满足，
且绕过了检环。

### 二、修法与取证

`Create` 与 `SetDependency` 走同一个归属判据；「不存在」与「不是你的」文案**完全一致**（否则又是一台
存在性探针）；历史越权行（修复前建的）在执行期也不许把别人的状态透出来。
锁：`service/dependency_b19_test.go`（四件事各一条，含"自己的前置照常放行"的反向锁，防判据写成一律拒绝）。
电池 `scripts/mut_dependency_b19.py`：控制组 `ran=6 skip=0`，**6 格逐格被杀**（`/tmp/b19dep_mut3.log`；
前两版判"无法判定"，红在注码而不在被测代码 —— 口径收紧的理由见 §7.10 对电池自身的审查）。

## 7.15 批19c：触发器域把三类结论折成一句 400

cron 的四个写入口一律 `response.Error(ctx, 400, err.Error())`：别人的/不存在的触发器 id 该是 404 且主语
是「触发器」，前置条件不满足该是 400/409 + 原因原文，真·未知错误该是 500（不许洗成客户端错误）。
而 400 里塞的是 gorm 的 `record not found` / DB 故障的 SQLSTATE 原文 —— 前端弹条直接显示内部串。
同域的任务侧在批10b 已经分流过了，这里是漂移而不是新设计。

修法：按错误类型分流，内部原文只进日志。锁 `controller/cron_error_b19c_test.go` **走真 handler + 真库**
（不用静态锁：门的形状改了，字面量锁会照样绿）；电池 `scripts/mut_cron_err_b19c.py` 控制组 `ran=11 skip=0`，
**8 格逐格被杀**（`/tmp/b19c_mut.log`）。

## 7.16 批19d：会话域把「读不出来」说成「不存在」

session 六个读/写入口把所有 service 错误一律写成 `404 会话不存在`：真没有这条会话（用户该换 id）与
会话在但库里读不动（连接/超时/数据故障，服务端该进监控）被折成一句。真机观感是"列表里明明有，
点详情说不存在"；同时一次服务端故障被记成用户误操作 —— 与批10b 方向相反的洗白。

取证上这批最值得记的是**取径先量后写**：首版想用"可空列写 NULL + 模型侧非指针 string"造读失败，
实测 GORM 把 NULL 落成零值、读不报错（两条腿都是绿的，见 `controller/session_error_b19d_test.go`
抬头），负数 id 又在 `parseID` 就先回 400 —— 于是改用真会发生的读失败：**库不可达**（不碰共享测试库、
不改 schema）。锁断的是**码与主语**，不是措辞；电池 `scripts/mut_session_err_b19d.py` 控制组 `ran=16 skip=0`，
**10 格逐格被杀**（`/tmp/b19d_battery2.log`）。

## 7.17 批19e：编排步数无上界（落点不是跑不完，是读放大）

`CreateBrowserTaskReq` 给每个数字参数都钳了区间，唯独 `Steps` 数组本身没有 `max` ⇒ 一条任务可以带
百万步。兜底不在这里：会话有 `TimeoutSec≤3600` 硬闸，跑不完是执行层的事；真正的落点是 **`steps` 整列
随任务详情返回**，GET 一次任务就把百 MB JSON 拉进内存再吐给前端，编排面板当场卡死 —— 而这台实例是
共享的，一个人的任务能拖慢所有人。设计前提本身是"单任务步数量级为个位数"（写台账决策 1 就是按它省掉
复合索引的），无上界等于让那条决策站不住。

刻意不做：给 `Value`/`Target` 加字数上限 —— 单条命令帧在 nm-host 侧已有 1 MiB 硬顶
（`cmd/nm-host/main.go: nmMaxOutboundFrameBytes`），超帧的步根本执行不了；在服务端判死反而会把
"一条本来就合法的长评论"拦下来，是更坏的交易。锁 `controller/step_cap_b19e_test.go`（上界两侧各一条：
越界拒、界内放行）；电池 `scripts/mut_step_cap_b19e.py` 控制组 `ran=3 skip=0`，**6 格逐格被杀**
（`/tmp/b19e_battery2.log`）。

## 7.18 批19f：Host 通道的「仅限本机」被自己写的取 IP 方式绕过 + 撤销承诺比实现强

两处各自独立成病：

1. `clientIPOf` 原先无条件采信 `X-Real-IP` / `X-Forwarded-For` —— 调用方说什么就是什么。而
   `/api/browser/host-ws` 注册在 engine 上、不过 JWT，本就只有"token + 回环"两道关，于是宣称的
   双层防护实际只剩一层：任何摸到这条路径的人加一行 `X-Real-IP: 127.0.0.1` 就过了门。
   修法用 `ctx.RemoteIP()`（gin 的 `ClientIP` 会顺着转发头改写，`RemoteIP` 只看连接真实对端）。
   **反方向的腿同样要有**：本机 Host 经 nginx/frp 进来时对端本来就是 127.0.0.1、公网地址在转发头里 ——
   按头判就把真人拦在门外（第 3 条腿专门锁这个）。
2. `ResetToken` 对运维说"旧 token 24h 内仍可用"，但 `ValidateHostToken` 对 `_prev` 没有任何时限：
   旧 token 实际活到**下一次重置**为止。撤销手段比它承诺的更弱，而没人会去读实现才发现。
   本批不新增时限（全仓 bridge/webhook 的 `_prev` 都是同一形状，单给这条加就是第二个口径），
   改的是**文案不再承诺没做的事**，并留一条锁盯着这句话。

锁 `controller/host_loopback_b19f_test.go`；电池 `scripts/mut_host_gate_b19f.py` 控制组 `ran=5 skip=0`，
**7 格逐格被杀**（`/tmp/b19f_battery2.log`）。

## 7.19 批19g：治理裁剪的分批承诺与实现不符（`PruneSnapshotText` 无界 UPDATE）

`retention.go` 对运维的承诺是"分批避免长事务锁"（G19），`PruneSnapshotText` 的注释也写着"分批 5000"，
但实现是一条不带批的 UPDATE：首次上线时表里攒着多少超期行，就在一个事务里重写多少行（快照每行可到
64 KiB，十万行就是 GB 级 WAL + 全程持锁）。注释与实现不符的坏处不在注释，在于**读它的人以为这事已处理过**。
plan 侧（清文本）与 command_log 侧（删行）是两处实现，只修一处另一处照样裸奔 —— 两格分开注码。

观测口径：往 gorm 的 update/delete 回调链上挂计数器，一次回调 = 实际发出的一条语句。
**不数语句就看不见分批**：只看结果的话，一条 UPDATE 和两条 UPDATE 都是"全清干净"，永远绿。
锁 `repository/retention_b19g_test.go`；电池 `scripts/mut_prune_batch_b19g.py` 控制组 `ran=2 skip=0`，
**8 格逐格被杀**（`/tmp/b19g_battery4.log`；前三版的"无法判定"是注码/期望自身的形状问题）。

## 7.20 批19h：扩展静默续期后仍发已拉黑的旧令牌 + Brain 动作名不过闸就落库、进 `innerHTML`

第三轮深查一次出到两处，同属"承诺与实现分家"，但落点一个在扩展、一个在服务端。

### 一、扩展侧：一次性轮换下"续期成功"≠"这次请求用的令牌还活着"

服务端 `POST /api/auth/refresh-token` 是**一次性轮换**（`service/auth.go:197` 刷新即把旧令牌拉黑）。
`ensureFreshToken` 换来 T2 并写进存储，但它没有返回值，`apiCall` 随后仍带本地 `auth.token`（刚被拉黑的
T1）发业务请求 ⇒ 必然 401 ⇒ 兜底分支再用同一把死令牌去 refresh（恒失败）⇒ `saveAuth({username})`
把**存储里刚换来的 T2 一起抹掉** ⇒ 用户被告知"登录已过期，请在弹窗中重新登录"。这是每 24h 到点必发一次
的强制重登 —— 正好破掉这段代码自己的抬头承诺（"效果 = 登录一次长期有效"）。并发两个在途调用时（一个
换来 T2、另一个还握着 T1）同样触发。

修法（`src/core/api-client.js`，四刀各有一格）：① `ensureFreshToken` 返回**这次该带哪把令牌**而非布尔；
② 401 兜底先看盘 —— 盘上令牌与实发不同 ⇒ 另有在途调用刚完成轮换，拿它重试一次，这一腿排在清盘之前；
③ `refreshTokenOnce` 换来的新令牌必须落盘；④ 续期窗口只在剩余 <1h 时触发（"别把续期改成每次调用都刷"
是反向锁）。契约不是"一个字都不许带着旧令牌发出"（并发下无从预知对手已轮换，撞一次 401 再换是正常成本），
而是"撞一次就要靠盘上的新令牌补救，既不许原地打转，更不许把赢家换来的令牌当失效证据抹掉"。

### 二、服务端：Brain 的 `action` 是 LLM 原文，REST 那条 `oneof` 在这条路上一行都不跑

`executeBrain` 每轮把模型输出 `json.Unmarshal` 进 `[]dto.StepItem`（`service/executor.go`），**不过 gin 的
binding 校验**。旧循环里唯一的闸是"写台账未初始化时禁 screenshot"，其余一律 `dispatchStep`；表外动作名落到
`default: "未知动作"` ⇒ 照样 `CreateIfAbsent` 写进 `browser_steps`，popup 用 `innerHTML` 把它当 HTML 拼出来。
而模型是被 `<page_snapshot>` 里的第三方页面内容喂出来的（`brain.go` 自己把快照定性为"不可信第三方内容"并
为此加分隔符护栏）——一个改版页面可以让模型吐出 `{"action":"<img src=x onerror=...>"}`，这句话原文落库、
在用户自己那把 JWT 的上下文里执行，而扩展还有 debugger 与 Native Host 通道。

修法分三层，且**判据不许是第二份要人记得维护的清单**：

- `dto/step_action.go`：`KnownStepActions` 用 `reflect` 从 `StepItem.Action` 的 `binding` 标签现读 `oneof` ——
  REST 校验什么，Brain 就只认什么（同一事实源）。标签解析不出来时返回**空集**（fail-closed，闸门静默常开
  是最坏形状，单独一格 J4 盯着）。
- `service/brain_reliability.go: brainPlanStepRejection`：落库前判，`screenshot` 保留原文案（G17 的腿），
  表外动作名回"只允许 action 表里列出的动作"，回显一律 `truncateRunes(…, 40, …)`（防 8000 字符幻觉整段
  灌回下一轮 prompt）。
- `src/core/render.js`：popup 两段模板拆成纯函数，**每一个插值都过 `escapeHtml`**，不按"这个字段看着像枚举"
  豁免；判据挂在真实 DOM 上（jsdom 里 mount 产物、查不到节点才算转义住）。

跨锁三条（都跑得出红，各自一格）：白名单集合 ⇄ `dispatchStep` 的 `case` 集合（脚本扫源码，扫到 0 个 `case`
就 `t.Fatalf`，不许空集合静默判等）⇄ prompt 动作表；`escapeHtml` 的 `"` 与 `&` 分支各有一条专属用例
（属性位劈标签 / 实体被二次解码），`'` 分支则**登记为当下不可观测的防御性保留**。

### 三、证据（本轮全部跑出来的）

| 层 | 跑法 | 读数 |
|---|---|---|
| 立项形状复现 | `service/brain_action_gate_loop_b19h_test.go` 注码前首跑 | 红因正是缺陷本身：`落库 1 条步骤（[hover:failed]），want 0` |
| Go 行为锁 | 3 条真 WS 用例（表外动作整轮零落库 / 混编轮只落合法那一条且 `StepIndex==0` / screenshot 仍旧拒） | 全绿，`service` 包 219.768s 内 |
| Go 电池 | `scripts/mut_brain_action_b19h.py`（私有 `--shared` 克隆 + 泳道文件覆盖）J1–J10 | 控制组 `rc=0 ran=10 skip=0`，**10 格逐格被杀**（`/tmp/b19h_battery1.log`）；J1–J3 三格同时点到 5 条锁（同一处 oneof 漂移的三条可见路径），J4 fail-open、J5 判据写反、J6 回显无界、J7 oneof 多出执行器不认的动作、J9/J10 闸门回退 |
| JS 电池 | `scripts/mut_extension_auth_b19h.py`（私有副本）A1–A5 + R1–R13 | 控制组 `rc=0 ran=12 skip=0 红名=[]`，**16 格被杀 + 2 格按论证存活**（A5：一次性轮换下 `auth.token` 必是实发令牌的前身、前身必已拉黑 ⇒ 两把都救不了，这条区别观察不到；R13：模板属性一律双引号 ⇒ 单引号分支当下劈不开标签）。同族登记 6 对（如 R6/R7/R8 三格杀掉同一句断言）照实打在报告里，不当成三个覆盖面 |
| 门禁（覆盖批18–19h 全部未提交改动） | `gofmt -l` / `go vet` / `go build`（含 `cmd/nm-host`）/ `go test -count=1 -v ./internal/browser_automation/...`（**不带 `-run`**） | 四项 rc 全 0；`PASS=187 FAIL=0 SKIP=0`，五个包（controller 13.6s / dto 1.3s / platform 3.1s / repository 14.6s / service 219.8s），日志 `/tmp/b19h_lane_gate.log`，跑时宿主 load≈30 |
| 扩展全量 + 构建 | `npx vitest run`（不带过滤）+ `npm run build` | `Test Files 16 passed / Tests 171 passed`、`rc=0`（本轮起计，批18 时是 14 文件 155 条）；构建 rc=0，`dist/background.js` md5 与本批开工前记录的 `38685a63…` 一致 ⇒ 注入侧源码没动，`dist/popup.js` `7df99d9c…` 含本批 render/api-client 改动 |
| 真机腿 | —— | **本批未跑真机腿**，理由是这两处的失败形状在真机上不可造：令牌那半要精确掐断一次 401/轮换（真机时间窗不可控），Brain 那半要模型恰好吐出表外动作名。分工写在抬头：形状由电池承担，链路可达性由 3 条真 WS 用例承担。真机回归仍走既有任务集（§7.12 那批的 14 条腿），本批没改它们覆盖的通路 |

### 四、本轮登记的两条"不成立"（否决也要留理由）

- **"`.npmrc` 里明文 npm token 被提交进仓库"**：不成立。`git ls-files | grep -i npmrc` 空 ⇒ 本仓从未把任何
  npmrc 纳入版本控制；`git log --all -- '*npmrc*'` 与 `git log --all -S'_authToken'` 均空 ⇒ 历史里也没有；
  全仓（排除 `node_modules`/`.git`）扫 `npm_[A-Za-z0-9]{20,}` 无命中。本机那份 `user-web/.npmrc` 未追踪、
  且当前内容只有 `legacy-peer-deps` 与审计注释。当初那句话的来源是**本机文件的读数**，不是仓库外泄 ——
  记在这里是为了防止下一轮把它又当成一次"凭据入库"重新立项。
- **"CI 的 Go 门禁缺 setup"**：本轮不成立（结论过期，不是当初错）。当前 `.github/workflows/user-server-ci.yml`
  第 90 行有 `Set up Go`（`actions/setup-go`），最近一笔 touching 该文件的是并行会话的 `99b54c6f`。
  本泳道不再改 CI 文件（属对方 lane），只把这条从"待修"划成"已被上游关闭"。
  ESLint 那道常红门仍按 §（da68ac26 的归因）挂着，不是浏览器自动化泳道的债。

## 7.21 批20：D7 审批闭环三件套（A5 放行绑载荷 / A9 收口落 judge 帧 / A10 挂起可跨进程查证）+ 前端收尾

立项即 §8.3 的 5、14、15 三行。合批的理由不是"顺手"，而是**三件事共用同一个事实源**：一次挂起的
身份就是「第几步 + 哪份载荷 + 到什么时候」。把它做成三个调用方各自记得加的步骤，漏一步的形状与
漏一个闸门同构 —— 所以三件套收在 `awaitConfirmGate` 一个入口里，同生同死。

一句话立项理由（写代码前先说清缺的是什么）：旧实现的一条布尔放行既不说**批的是哪份载荷**、
也不说**"批过"这件事发生过**、更不说**闸门其实在别的进程上**，三件事全靠人脑补，就等于没有。

### 一、A5：放行绑载荷，且一次性消费

- `confirmGate` 在开闸时把 `payloadHash` 定死（来自 `writeStepKey`），存续期间不许改 ⇒
  "批的是哪份内容"有唯一答案，比对才有意义。反面教材同行实测：LangGraph 的 interrupt 对 resume 值
  完全不校验，等于任何一次放行都可为任何一份载荷背书。
- `SignalConfirm(sessionID, payloadHash) ConfirmVerdict` 三态而非 bool：`VerdictNoGate`（没闸门）/
  `VerdictGranted`（相符，已放行）/ `VerdictMismatch`（有闸门但批错了内容 —— 已拒、闸门仍在、
  一帧命令都不下发）。命名前缀与闸门自己的出路 `confirmOutcome` 分开：两边都叫 "granted" 时，
  "放行命中了"和"等待方被放行"在读码时会互相顶替。
- **先摘后关**：同一通道只可能被 close 一次，第二次放行落 `VerdictNoGate` ⇒ 重复放行不会 double close。
- 空载荷的闸门就地 `panic`（开闸前，不是开闸后判个错）：绑一份空哈希的闸门等于匿名放行口，
  谁都能以"我也批了这份"放行；前提被绕过属于编排 bug，该炸在原地。
- `mismatchAttempts` 由闸门自己计数：放行不撤闸门，事后无从重建"拒过几次错载荷"。

### 二、A9：挂起与收口都落盘，两帧只带哈希不带正文

- `d7_wait`（event）在**登记之后**落：帧一到，读侧就认为有人能放行；若此刻注册表还空着，
  一次真放行会被答成"没有待确认的提交点"。顺序不是洁癖，是有两条腿各自承重的（电池格 W2/W2b）。
- `d7_confirm`（judge）在**每条出路**落，`decision` 分 `granted` / `stopped` / `timeout` 并带
  `rejected_attempts`：连"没人批、超时了"都要留一行，否则审计面上"用户拒了"与"用户没来"又折成一句。
- 两帧 payload 只放 `payload_hash` 与 `expires_at/step_index`，**绝不放正文** —— I5 审计导出会把
  payload 原样带进离线件，评论正文不该住在那儿。正文只在读侧（归属校验之后）以 200 字预览出现。
- `gateWaitFrame` / `gateConfirmFrame` 两个动作名常量单点定义：写侧在 `executor.go`、读侧
  （`session.go:gateElsewhere`）在另一个文件，各写一遍字面量就会有一个拼错的常量悄悄把跨进程查证变成死代码
  （帧一直在落，没人查得到）。

### 三、A10：挂起态可查证，四态各有各的下一步

- 读侧 `GET /sessions/:id/confirm-gate` 返回 `{pending, gate{session_id,step_index,payload_hash,preview,expires_at}}`，
  受归属校验约束（预览含正文，越权读到就是外泄）。这道口不是便利贴：没有它，前端唯一的输入就是一个布尔，
  绑载荷就无从谈起。
- 服务层四态 `ConfirmStatus` = `granted` / `no_gate` / `payload_mismatch` / `gate_on_another_instance`。
  第三种靠"本进程内存没有闸门"AND"库里最近一帧 `d7_wait` 仍未到期"共同判出。
  **`expires_at` 只是写下的期望**，必须和"内存里没有"一起用：挂起协程是进程内的，重启或换副本后 map 必空而
  那一帧还躺在审计里；只比到期时间的话，一条三年前的旧帧能让这个 session 永远被回答"闸门在别的实例上"。
- 控制器映射：缺载荷 400；`granted`/`no_gate` 200（后者是查出来的事实，不是错误）；
  `payload_mismatch`/`gate_on_another_instance` 409 且 mismatch 时把当前挂起闸门一起带回。
  机器可读判别位放在 `data.status` 而不是错误码：`response.Error` 走 int 码时会把 409 一律折成
  `DUPLICATE_ENTRY_3003`（批10 实测），靠文案 substring 分辨是哪一种"没放行"等于把结论建在措辞上；
  公共错误码表（`internal/pkg/utils`）在本批车道外，不为两个态去扩码域。
- 为什么"另一个实例"与"载荷不符"必须各自一态：这两条的下一步完全不同（前者去查实例数/等对账，
  后者去重新看内容再批），折成一句会让用户去改编排，而该改的是实例数。

### 四、前端收尾（本批最后落的一段，另一条 lane 在飞时补上）

服务端从批20 起不带 `payload_hash` 一律 400，而旧前端是 `http.post(url, {})` + 只判 `confirmed === false`
⇒ **不改前端的后果不是"少个提示"，是放行永远失败**，且在页面上长得很像"闸门坏了"。四条契约：

| 契约 | 落点 | 为什么不能反过来 |
|---|---|---|
| 哈希只能来自 `GET …/confirm-gate` | `getBrowserConfirmGate(id)` | 前端自己算不出被批的那份内容（正文在库与服务端侧） |
| 没拿到哈希就不发请求 | `confirmBrowserSession` 本地 `reject` | 服务端会 400，但"把没指明内容的支票寄出去"这个动作本身不该发生；且少一次"400 被读成闸门没了"的误读 |
| 结论只认 `status`，不认文案 | `interpretConfirmResult` | 200 也可能是否定结论（`no_gate`）；409 的结论住在 `err.response.data.data`，两种形状必须过同一个判读，否则"不符"与"网络断了"在前端长得一样 |
| 拿不到已知 status ⇒ `unknown` | 同上，`released` 恒 false | 一次网络抖动既不是"放行成功"也不是"没有闸门"，报任一都是在替用户编结论 |

`Monitor.vue` 侧：闸门卡片渲染 `第 N 步 / 将要提交的内容 / 挂起到期`，放行按钮 `:disabled="!gate?.payload_hash"`
（没有载荷就没有可点的放行入口），四个态各一条独立文案，`payload_mismatch` 时用服务端带回的 gate 立刻换掉预览
（用户下一步是照新内容重批，不是刷新碰运气），`_silent` 让 toast 只有一条。

**顺带修的一处真缺陷**：第 9 行 `<el-tag v-if="session.confirm_pending">` 在首帧 `session=null` 时会抛
（同一块里下一行的按钮写的就是 `session?.`，说明是漏改而非设计），已统一为 `session?.confirm_pending`。
这一格不是"顺手美化"：把守卫改回去做单腿探针 ⇒ 4 条渲染腿**全部**红，红因原文
`TypeError: Cannot read properties of null (reading 'confirm_pending')`，外加那条静态锁 ——
也就是说这个页面从批3 那一笔（`git log -S` 定位到 `ced76b9b`，正是加 D7 放行入口的同一笔）起
**首帧渲染必抛**，只是 element-plus 的挂载错误过去没人看。
探针走"cp 备份 → 就地改一行 → 跑两个文件 → 还原比 md5"（`1b7d44df…` 回到注码前），没有动用整文件快照。

### 五、证据（全部跑出来的）

| 层 | 跑法 | 读数 |
|---|---|---|
| 服务端契约锁 | `service/d7_gate_b20_test.go` 7 条（绑载荷三态 / 读侧详情 / 两帧形状与不含正文 / 三态 decision / 四态含"闸门在别处" / 读侧归属校验 / 空载荷不得开闸） | 全绿 |
| HTTP 面 | `controller/d7_confirm_http_b20_test.go`（缺载荷 400 + 四态映射，mismatch 带 gate） | 全绿；四条映射必须都能在 HTTP 面断言到，否则态名只活在 service 单测里 |
| 链路可达 | `executor_ws_e2e_test.go`（真 WS + 影子库：闸门载荷 == `HashWriteText(正文)`、放行后 `comment_verify` 才发） | 全绿 |
| 前端行为锁 | `tests/unit/browser_d7_gate_b20.test.js` 15 条 + `browser_d7_confirm.test.js` 5 条（改 2 条：旧腿"body 是对象就行"正好放过了批20 修的空对象支票） | 20/20 绿 |
| 前端渲染腿 | `tests/unit/browser_d7_gate_render_b20.test.js` 4 条（真 mount：预览进 DOM / 无载荷按钮禁用且点了不发 / 未挂起一次闸门请求都不发 / 点击带的是闸门那份哈希） | 4/4 绿 |
| 前端电池 | `/tmp/b20f_mut.mjs`（8 刀，控制组 + 逐刀 `ran=24 skip=0` + 红集合上下界 + 每刀自带快照还原与 md5 比对），日志 `/tmp/b20f-mut-r1.log` | **8 格全杀**：F1 本地拒绝撤掉、F2 空白支票、F3 `_silent` 丢、F4 把 `no_gate` 读成放行、F5 未知态折成 `no_gate`、F6 按钮不再禁用、F7 不判 `confirm_pending` 就取闸门、F8 卡片永不渲染；每格红因逐条落日志（如 F2 `expected {} to deeply equal { payload_hash: 'cafe1234' }`） |
| 电池自身被抓的两处 | 第 1 轮 | ① `base` 字典键写成路径、比较时用相对名 ⇒ `md5 != undefined` 恒不等，**还原根本没被验过**却照样输出"8 格全杀"；② 汇总把 `RESTORE-BAD` 也计进"未杀格" ⇒ 同一趟报出 `cut=8 未杀格=8` 这种自相矛盾的数。修完复跑才有表里那行"8 格全杀 + 还原异常=0" |
| 门禁 | 前端：`npx vitest run`（不带过滤）+ `npm run build`；Go：`gofmt -l` / `go vet` / `go build ./...` / `go test -count=1 -v ./internal/browser_automation/...`（不带 `-run`），日志 `/tmp/b20f_lane_gate.log` | 前端 `build_rc=0`（44.71s）、本泳道 4 个浏览器文件 27 条全绿；全量 `255 条 / 2 红`，两条红都在**另一 lane 的** `approvalTask_render.test.js`（详见下条归因）。Go：`lane_rc=0`，5 个包全 `ok`（controller 4.207s / dto 1.108s / platform 2.237s / repository 1.820s / service 189.816s），`--- PASS=204`、嵌套 `--- PASS=21`、`FAIL=0`、`SKIP=0` | 本批 Go 侧收口。这一趟是 **gofmt 之后重跑**的那版（`host_registry.go` 与 `host_offline_b21_test.go` 的注释 bullet 被 `gofmt -l` 点过，`-w` 后重跑），日志 `/tmp/b20f_lane_gate2.log`；gofmt 前那趟（`/tmp/b20f_lane_gate.log`，service 237.843s）**同口径同计数**（PASS 204 / FAIL 0 / SKIP 0），两趟一致才是"改的只是注释排版"的证据，而不是"跑过一次就算了" |
| 归因（红不认领） | 另一 lane 的组件用例在同一台机器上两轮红在**不同**用例（18:02 是 `operationLog_format/render`，18:05 是 `approvalTask_render`），单条耗时 5.1–5.3s 而绿时 0.6–1.5s，宿主 load 41–45；本批未改过这些文件与它们依赖的源码 | 判"并行 lane 的负载红"，登记不认领、不去改别人的用例；同一棵树单独复跑转绿即为归因证据 |

## 7.22 批21：传输层"命令从未上线"的归因收口（第三种形态，前一批只处理了两种）

### 一、形状：D7 让 Host 掉线变成常态事件，而台账把它当成"提交过"

`isNeverExecuted` 到批20 为止认两类零副作用失败（扩展侧回 `*_inject_timeout_` / `*_not_found` /
`*_not_interactable`，即"命令到了 Host、注入前就拒"）。传输层还有**第三种**：这一帧**根本没写进 socket**。
批20 之前它和前两类长得一样（都是 `ErrHostOffline`），于是：

- `post_comment` 的 send 腿拿到离线错误 → 台账**已**写成 `sent` → finalize 轮询拿不到回包 → 落 `unattributed`；
  `unattributed` 在双发闸的拦阻集合里 ⇒ 任务永久卡死，而那次点击**从未发生**；
- 顺带还造了一条假的 `extracted_data` 证据条目（把"没提交"记成"提交过、结果未知"）。

D7 挂起时长是分钟级（等人审批），等待期间 Host 掉线、机器休眠、扩展重载都是常态，**不是**边角。
R26-2 的矩阵说"超时 ≠ 没发生"，这一条是它的对偶：**只有能证明零副作用的失败才允许让台账保持干净**。

### 二、判据只能来自时序，不能来自文案

`HostRegistry.Request` 里三种失败看起来一模一样，可证性完全不同：

| 失败 | 时序位置 | 能否证明这帧没上线 | 归因 |
|---|---|---|---|
| `GetConn` 未命中 | 写之前 | **能**（注册表此刻没有这个用户的连接，帧无处可写） | `ErrCommandNeverOnWire` |
| `writeJSON` 报错 | 写的当场 | 不能 —— gorilla 明写"connection state is not knowable"，半帧可能已交付 | 保持 `unattributed` |
| 写成功后 `<-conn.Done()` | 写之后 | 不能 —— 帧已进 socket，Host 可能已收到 | 保持 `unattributed` |
| 命令超时 | 写之后 | 不能 | 保持 `unattributed` |

曾经考虑过两条更省事的路，都被自己的用例否掉：**按文案判**（`strings.Contains(err, "未连接")`）会把
第 2、3 类一起折进来（T2/T3b 两条腿就是为这条准备的，注码后必红）；**就地重发一次**没意义
（判据成立即 Host 已没了，微秒级的第二次调用只会拿到同一条错误），恢复归**任务级重试**：
台账留在 `prepared` ⇒ 不在拦阻集合 ⇒ 下一轮重跑会重新 prep、重新过一次闸门 —— 那是更严而不是更松的路径。

### 三、实现与一条刻意的中文选择

- `ErrCommandNeverOnWire = errors.New("命令未上线")`，抛出点用 **双 `%w`**：
  `fmt.Errorf("%w: %w", ErrCommandNeverOnWire, ErrHostOffline)` —— 未上线标记给执行侧归因用，
  `ErrHostOffline` 原样保留给 controller 的 409 引导（Go 1.25 支持多重 wrap，已实测）。
- 写后进 select 的 `case <-conn.Done()` 那条**刻意不带**未上线标记，代码里写死了为什么。
- 哨兵文案选中文不是风格问题：`platform.ClassifyError` 的重试 token 是
  `timeout/超时/未就绪/element_not_found`，"命令未上线"一个都不命中 ⇒ 读步的可重试性仍是 `ErrUnknown`，
  **行为与批20 完全一致**（改了文案就会顺手放开一条没设计过的重试面）。
- `write_ledger.isNeverExecuted` 加"第四类"；`executor.go` send 腿在写 `sent` 之前早返，
  注释同时改掉"上面两个早返分支"的计数（现在是三个：未上线 / 注入超时 / 闸门拒点）。
- 契约变更一处（不是放宽）：`host_registry_test.go` 原来用 `err != ErrHostOffline` 严格相等，
  多挂一层 wrap 之后改判 `errors.Is`，并在旁边写明"对外的离线判据不变"。

### 四、证据（全部跑出来的）

| 层 | 跑法 | 读数 |
|---|---|---|
| 行为用例 | `service/host_offline_b21_test.go` 6 条：无连接=未上线 / 写后才断≠未上线 / 命令超时≠未上线 / 写失败≠未上线 / 通用写步未送达不留 `attempted` / `post_comment` 未送达台账仍 `prepared` 且 `comment_verify` 零调用 | 6/6 绿；控制组 `ran=6 skip=0` |
| 后端电池 | `/tmp/b21_mut_battery.sh` 6 刀（M1 离线分支丢标记 / M2 写后才断也带标记 / M3 写失败也带标记 / M4 台账不认标记 / M5 send 腿早返短路 / M6 早返前先落 `sent`） | **6 格全杀**，M6 的红因原文就是 `got "sent"`；`go build ./...` 干净，浏览器全量 lane 绿（service 186.240s，与批20 基线同量级）。**gofmt 收口后重跑**（`/tmp/b20f_lane_gate2.log`）：`lane_rc=0`、5 包全 `ok`、`PASS=204 / 嵌套 PASS=21 / FAIL=0 / SKIP=0`，`gofmt -l internal/browser_automation/` 空输出，收口三标志符号逐一在位（`host_registry.go:31` 哨兵 + `:350` 双 `%w`、`write_ledger.go` 第四类、`executor.go:1221` 早返与 `:1231`"三个早返分支"注释）；本批落盘件 md5：`host_registry.go a5d732da…`、`host_offline_b21_test.go 59c28caf…` |
| 取证侧自抓 | 第 1 轮 | ① 红因用 `-A3` 抓不到（Go 把原因行印在 `--- FAIL` **之前**）⇒ 改 `-B6`；② M5 的注码把 `{` 吃掉 ⇒ `syntax error: unexpected keyword return`，正确判为 BUILD-BROKEN 不计杀，锚点补 `\{` 后重打；③ 控制组那行 `rc=$?` 被末条命令替换吃掉 ⇒ 假读数 |
| 未覆盖站点记账 | 设计上的零覆盖，写下来免得下一轮把"6/6 全杀"读成"传输层已证明" | ① `writeJSON` 半帧已交付这一格无法用单测构造（判据来自 gorilla 的内部状态约定，只能靠"我们不把它当可证"这条纪律）；② `conn.Done()` 与命令超时两格只有"不得带标记"的反向锁，没有更细的可观测差异 |
| 一条血的教训 | 上一段收尾时曾从批20 的 `executor.go` 快照还原，把批21 的改动整块抹掉 | `md5(备份)==md5(活文件)` 是**被摧毁**的证据，不是还原成功的证据。口径改为：**每一刀在注码前一刻自己备份、只还原自己那份**，还原后 grep 上一批的标志符号（本批收口已核：`ErrCommandNeverOnWire`、`isNeverExecuted` 第四类、三个早返分支注释均在位） |

## 7.23 批20e：编辑接口把重试策略整列吞掉（批3 登记残项收口）

### 一、形状：一个说"已保存"的 200

编辑页 `Editor.vue:101-105` 早就摆着「失败自动重试」开关 + 两个 `el-input-number`（`:min="30" :max="86400"`、`:min="1" :max="10"`），
`save()`（`:232-241`）把整个 `form` 原样 PUT 出去。问题在收端：`dto.UpdateBrowserTaskReq` **根本没有这三个字段**。
而 gin 的 `ShouldBindJSON` 对未知 JSON 键默认宽容（不是 `DisallowUnknownFields`），于是这条链的每一步都"成功"：
请求合法 → 绑定不报错 → `Update` 逐个 `if req.X != nil` 应用（`controller/task.go:177-224`，压根没有这三条分支可走）
→ 落库 → 回 200 → 前端 toast「已保存」。**库里一个字节都没变。**

这不是"少个便利"：重试三件套是有活消费者的配置——`service/feedback.go:66` 用 `RetryCount < MaxRetryTimes` 决定
要不要排下一次重跑，`:162` 用 `task.RetryDelaySec` 算到期时间。所以真实后果是：
**重试策略只能在建任务那一刻定死**，想给一条已在跑的任务收紧/放宽额度，唯一的路是删掉重建（连带丢历史与依赖）。

### 二、补齐的口径：区间两侧同字面量 + 指针语义

- 三个字段按 Create 的**同一份**区间补进 Update：`retry_delay_sec omitempty,min=30,max=86400`、
  `max_retry_times omitempty,min=0,max=10`（`retry_on_fail` 是 bool，无区间）。
  刻意不写成"另一套更合理的上下界"——只在一侧钳等于没钳，届时"哪个值合法"取决于走哪个入口，比现在更难查。
- **指针语义**（`*bool` / `*int`，nil=不改），与 `RequireConfirm`/`ConfirmWaitSec` 同族。理由：编辑只改名字时，
  值语义会把 `retry_on_fail` 折回 `false`、把间隔与次数抹成 `0` —— 那是把用户配好的策略静默清空，
  比"改不动"更坏。这条锁由 `TestB20EUpdateWithoutRetryFieldsKeepsThem` 看着，变异 E7 专打它。
- 依赖两字段（`depends_on_task_id` / `depends_on_mode`）**刻意不进通用 PUT**：它们的归属与成环校验
  在 `service/task.go` 的 `SetDependency`（批19b 加的）里，通用 Update 要么绕过那道门、要么把两道门复制一遍。
  编辑器至今也没调用过依赖端点之外的路径 ⇒ 保持 `PUT /tasks/:id/dependency` 单入口，本批不动，登记为"看过并决定不做"。

### 三、证据（全部跑出来的）

| 层 | 跑法 | 读数 |
|---|---|---|
| RED（先看着它按设计的形状红） | `go test ./internal/browser_automation/controller/ -run TestB20E -v`，日志 `/tmp/b20e_red.log` | 6 红 1 绿。红因逐条与预判同形：`落库 retry=(false,60,1) want (true,600,5) —— 接口回 200 而库里没变`；三条越界腿全是 `HTTP 200 code=0 want 400`（越界值被吞而不是被拒）；两条内侧腿 `落库 (500,7) want (30,7)`。唯一那条绿的"不改"腿此时**还没有牙**（字段不存在当然不会被改），牙是 E7 补的 |
| GREEN | 同 `-run TestB20E`，日志 `/tmp/b20e_green.log` | `RC=0`，3 条顶层 + 5 条子用例全 PASS |
| 包级回归 | `go test ./internal/browser_automation/controller/ ./internal/browser_automation/dto/ -count=1`（不带 `-run`） | 两包 `ok`（5.755s / 1.023s）——新增字段没碰坏既有编辑面断言 |
| 跨入口静态锁 | `dto/retry_range_parity_b20e_test.go`（reflect 读 tag，比 Create 与 Update 两侧的 `min=/max=` 数值对；字段缺失单列一条红） | 绿（此刻不变量成立）。**行为腿看不到这件事**——PUT 的 8 条腿读不到 Create 的 tag，所以"只改一侧"这种漂移只有这条锁能抓，靶子见电池 E8 |
| 变异电池 | 第 2 轮 `/tmp/b20e_mut2.mjs` 8 刀，每刀**同时跑两个包**（controller 行为面 + dto 契水面），红集合按包各自上下界精确比对 + 计数漂移检查 + 控制组 `ran=8/1 skip=0` + 每刀注码前自备份与 md5 还原校验 + 注码前 `go vet` 未注码树；日志 `/tmp/b20e-mut-r2.log` | **8 格全杀，异常=0，还原异常=0**，终检两文件 md5 回基线（`task.go dbf4c3b8…` / `dto/task.go 8fc9069a…`）。E1–E3 删回写分支（红因原样是 `落库 retry=(false,600,5) want (true,600,5)` 这类"200 而库里没变"）、E4–E6 放开 PUT 侧区间（每格 controller 精确红 2 条**且** dto 精确红 1 条＝两把锁同时咬住）、E7 指针退回值语义（`未提供的重试字段被重置成 (false,420,4)，want 原样 (true,420,4)`）、**E8 只改 Create 一侧：controller 全绿、dto 静态锁精确红 1 条**——这一格就是那条锁存在的全部理由 |
| 电池自身被抓的三处 | 第 1 轮 → 第 2 轮 | ① 期望集漏算**父用例**：Go 在子用例红时给父用例也印一条 `--- FAIL`，E2/E3 因此被判"多红"——是判据建模错，不是判据没牙；第 2 轮把父名显式写进期望集（不自动推导，推导会把"父用例自己该红"这种形状藏起来）；② 第 1 轮 E4–E7 四格撞 `BUILD-BROKEN`，红因指向 `internal/geo/service/monitor_crawler.go:69: undefined: config`——**不是我注的码**：并行 lane 正在改那个文件（mtime 18:27），而 `go list -deps -test` 证明 controller 的测试二进制经依赖链吃它。于是加"每刀注码前先 `go vet` 未注码的那棵树"前置，不过就等 30s 重试并单独成类（LANE-BROKEN，绝不计杀）——**第 2 轮这道前置真的开火了**（E3 前 `# hivemtk-user/internal/geo/service` 又红一次，等 30s 恢复后照常杀），说明它不是装饰；③ 第 1 轮的汇总把 `RESTORE-BAD` 计进"未杀格"这类自相矛盾读数，本轮按包分开计数并单列还原异常 |
| 全量门禁 | `go test -count=1 -v ./internal/browser_automation/...`（不带 `-run`），日志 `/tmp/b20e_lane_gate.log` | `lane_rc=0`，5 包全 `ok`（controller 37.934s / dto 2.316s / platform 3.022s / repository 11.965s / service 260.076s），`--- PASS=207`、嵌套 `26`、`FAIL=0`、`SKIP=0`。与批20/21 那趟基线（`PASS=204 / 21`）比**恰好多 3 条顶层 + 5 条子用例=本批新增 8 条腿**，多一条少一条都不对；service 那 260s（基线 190–238s）是宿主 load 21–28 时的同量摆动，不是本批引入的等待 |
| 前端无需改（并核对过理由） | 读 `Editor.vue` 的 save/load 两条路径 | `loadTask()`（`:223-229`）用 `{...form.value, ...t}` 合并，编辑既有任务时重试三列由服务端详情回填；新建时表单自带初值 `(false,300,3)`（`:183`），与库里默认同口径。所以本批的"用户可见变化"是**同一份 UI 从谎话变成真话**，不需要动前端一行 |





## 7.24 批20b：审计流里「下发了」与「平台确认了」拆成两件事（A2 落地：`ok` 从二态常量改成三态观测）

立项即 §8.3 第 2 行。§8.3 写这一行时留的是「采纳（P1）A2」六个字，本批把它落成一句可核对的话：
**`browser_command_log` 从此没有一行携带它没观测到的结论。**

### 一、形状：一列上叠了三件事，其中一件恒真

`Ok bool` 自 `e2084d3f`（2026-09-10「统一收件箱+浏览器自动化全栈落地」）就是这个形状，
而 `appendCommandLog` 的调用方在写命令帧时传的是**字面量 `true`**（旧 `executor.go`：
`..., "command", step.Action, cmdPayload, 0, true`）。三件事因此全挤进一列：

- 下发帧：这一行说的是「服务端把这一帧交出去了」，那一刻 Host 连回执都没有，`ok` 却已经是真；
- 回包帧：这一行才是「Host 回执无错」；
- 平台侧结论（`comment_verify` 回查见没见）：**压根不在流里**，只落在 `browser_steps.submit_state`
  与 `extracted_data` 两处**可变行**上。

批21 之后第一件事可以**证伪**：`GetConn` 未命中时那一帧根本没写进 socket（`ErrCommandNeverOnWire`），
可审计流里那条 command 行照样是绿的——不是"缺信息"，是**有一条假信息**。

存量规模不靠推断，dev 库（`127.0.0.1:8232/user_db`，2026-09-21 只读 `SELECT`）实测分布：

| direction | ok | 行数 |
|---|---|---|
| command | `true` | **1888**（= 该 direction 的全部，无一例外） |
| event | `true` / `false` | 2715 / 361 |
| judge | `false` | 1 |

同库 `information_schema.columns`：`ok` 的 `is_nullable=NO`、`column_default=false`
⇒ 旧库连「这一帧没有结论」都**表达不出来**（写 NULL 直接被拒），所以本批不是改语义就完事，
列定义与存量行都要一起过。

### 二、为什么是「让 `ok` 可以空 + 补一帧真有结论的 judge」，而不是把列改名成 `status`×`conclusion`

同行口径给了方向（GitHub Checks 两轴、Stripe `processing ≠ succeeded`），但没给本仓的落法。OTel 那条
才是可执行的：**无观测时该字段 MUST 留 Unset，而不是填一个看着像结论的值**。据此：

- **轴住在 `direction` 里，不在同一帧上再叠一列。** 每一帧只承载一次观测：`command`=attempted、
  `event`=accepted、`judge`=confirmed。给每帧配第二个 `conclusion` 列，等于要求 1888 条历史下发帧
  各自"补"出一个它们从未有过的结论——要么填假值（正是本批修的），要么整列 NULL（那不如现在这样一列三态）。
- **judge 帧不是新发明的第三态**：`d7_confirm`（§7.21）与 `judge_done` 早就在这个 direction 上，
  本批只是给写步补齐那一帧，形状与既有两族一致。
- 新帧 `write_confirm`（`executor.go:455`）与 §7.21 的 A5/A9 绑同一条线：payload 带
  `payload_hash`（=`HashWriteText(正文)`）+ `step_index` + `state`，**不带正文**；`ok` 直接取
  `verified`。于是"用户批的载荷 / 实际提交的载荷 / 回查认到的载荷"三者可比，且重放不会把结论重放
  （每步恰好一帧，腿二按 `len(frames)!=1` 判红）。

### 三、三态要真能过库：指针 + 无默认值 + 一条把存量折平的迁移

- **模型**（`model/command_log.go:30`）：`Ok *bool` `gorm:"column:ok"`，标签**刻意不带 `default:false`**。
  带默认值的标签会让 gorm 把 nil 当成「这列交给默认」从而写回 `false`，三态在门口就退化成两态，
  而且退化得毫无声响（新代码读回来还是 `false`，与真实的"结论为否"同形）。这条由腿五
  （`TestB20BOkTriStateSurvivesRoundTrip`，真库往返 `nil/true/false` 三行）看着。
- **迁移 v3.44.0**（`migrations/v3_44_0_browser_command_log_ok_tristate_migration.go`）：
  `Up`（`:49`）先 `HasTable` 守卫（`:53`，表还没建时 no-op，与 v3.42/43 同口径），再
  `DROP DEFAULT`（`:57`）→ `DROP NOT NULL`（`:58`）→ **分批**把存量 `direction='command'` 的行
  置 NULL（`:69`，批大小 `okNullBackfillBatch=5000` `:47`，理由与 `pruneBatchRows` 同：
  首次上线时待回填行数就是上表那 1888 行乃至生产全量，一条语句吃完就是长事务）。
- **`Down` 不改回 `NOT NULL`/`DEFAULT`**（`:86` 走 `declineColumnDrop`）：要把 NULL 收回两态，
  只能把「无结论」折成一个**编出来的结论**或把**已观测的** `false` 一起抹掉，两个方向都是造信息。
  登记为"看过并决定不做"，与 §7.19 的裁剪不可逆同一条纪律。
- 回填的**范围**也要有牙：只 null 掉 command 帧。迁移用例里 event/judge 的 `true`/`false` 必须原样还在
  （按 `SELECT seq, ok` 回读比对）——把全表抹成 NULL 一样能让五条腿全绿，那叫销毁现场。

### 四、正文零泄漏：`evidence` 只从流侧摘，不从库侧摘

`finalizeComment` 的回查证据 `evidence.text` 就是评论正文的复读，它随成功 payload 回到步级 event 帧
（`executor.go:1342`）——而 I5 审计导出会把 `browser_command_log.payload` 整列原样带进离线件。
于是新增 `auditEventPayload`（`:188`）：**只在落流那一刻删 `evidence` 一个键**，非 JSON 或没有该键就原样返回
（脱敏不是格式改写，解析失败不许变成"这帧不落了"）。追溯面板与 I4 续跑要用的那份正文证据仍由
`mergeExtract`（`:1323`）写进 `browser_steps.extracted_data`——可变行里有正文是有意的，那里不进导出。
这条不是预防性的：变异 M3（删掉脱敏调用）让零泄漏腿**单独红**，说明泄漏面是真的接通的。

### 五、`comment_send` 补一帧一命令，顺带修掉一处 seq 同号缺陷

写链路里最不可逆的那一跳（真正把 `comment_prep` 的令牌交出去）此前在流里**没有自己的行**——
只有步级的 `post_comment` 帧。补齐成 command/event 一对（`sendOnce`，`:1270-1280`）：command 帧
`ok=nil` 且只带 `payload_hash`，event 帧带 `error` 文本与回执结论。

- **自愈重发落第二对**（`:1304`）：`send_button_not_found` 的归因是"点击从未发生"（A1），重发不违
  「单次提交禁重试」红线；但审计面的职责是**如实记录发生过几次动作**，把自愈藏进同一对帧，
  等于让一次重发在流里隐形。运行时红线管动作，审计面管动作的次数，两者不互相打折。
- **顺带修的真缺陷**：`dispatchStep` 内部落的帧（`d7_wait`/`d7_confirm`，以及本批新加的
  `comment_send`/`write_confirm`）沿用了调用方的 `*seq` 而不自增，而步级 event 帧又按
  「一问一答同序号」复用同一个号 ⇒ 同一条流里出现**两条同号帧**，按 seq 还原执行序的读者会看到
  一次动作凭空分成两叉。现在步级 event 帧自占一号（`:1021-1024`），写侧改动的同时把
  「确认帧的 seq 必须晚于提交命令帧」钉进腿二（反了就是编出来的一帧）。

### 六、前端同一列得画对三态（改服务端的那一批必须同批改）

`Monitor.vue` 旧模板是 `row.ok ? '✓' : '✗'`。服务端把常量 `true` 换成 `nil` 的**同一批**，
这条二态三元就把 1888 条下发全画成 ✗——**比原来的假绿更坏**（假绿至少不改变读数分布，这个直接把
「没结论」判成「失败」）。所以：`outcomeMark`（`:140`）三态且未知形状一律 `—`
（宁可说"这帧没结论"，也不替用户编一个红或绿）、列名改「帧结论」（`:80`）、表头写明
`✓=该帧结论成立 / ✗=不成立 / —=此帧不携带结论`（`:63`），读包的人不该靠猜。
`tests/unit/browser_cmdlog_b20b.test.js`：3 条真 mount 行为锁（null 画 `—` 而不是 `✗` / 表头三句解释进 DOM /
judge 帧结论原样进表不在前端二次加工）+ 2 条静态锁（模板不得留二态三元表达式、判定函数必须把
`=== false` 与兜底分开）。静态锁的模板区间按 `<script` 切，不按 `</template>`——后者停在**第一个嵌套
slot** 的闭合处，被锁的列整块落在区间外，是一条会自我通过的锁。

### 七、证据（全部跑出来的）

| 层 | 跑法 | 读数 |
|---|---|---|
| RED（先看着它按设计的形状红） | 测试三份先落盘、生产码一行未动时 `go test ./internal/browser_automation/service/ -run TestB20B` | `vet: command_log_outcome_b20b_test.go:76:15: invalid operation: l.Ok != nil (mismatched types bool and untyped nil)` —— 红在类型上而不是断言上，正是"这五条腿此刻还没有可断的三态"的形状。**本轮补记一条反向复证的口径**：今天把模型单独退回 `Ok bool` 再跑 `go vet` 得到的是**另一条**红（`executor.go:172:46: cannot use ok (variable of type *bool) as bool value`），因为 vet 只报首个出错点、而彼时 executor 还没转换。两次的红都真，但**不是同一条**，引用本行时别互换 |
| 契约锁 | `service/command_log_outcome_b20b_test.go`（真 WS + 影子库跑完整三段式后回读整列审计流）：①command 帧不得带结论、event 帧必须带 ②写步恰好一帧 `write_confirm`（judge / `ok=true` / seq 晚于 `comment_send` / 绑 `HashWriteText` / `state=verified` / 全流零正文） ③回查未见时同帧改口 `state=unattributed` 且不与已确认同形 ④纯读步不得冒确认帧（且零帧也算红，不是"没噪音"） ⑤三态过库往返 | `--- PASS` 5 条：3.56 / 2.79 / 3.90 / 1.68 / 0.12s（`/tmp/b20b_lane_gate.log`） |
| 迁移 | `migrations/v3_44_0_..._test.go`（元信息 / nil-db / Up+回填 / registry 注册） | `go test -count=1 ./internal/migration/... ./internal/pkg/db/...`（不带 `-run`）三包全 `ok`：migration 0.551s、**migrations 54.414s**、pkg/db 126.938s。Up 那条腿含四件反向证明：迁移前把列摘成旧形状（`NOT NULL DEFAULT false`）后**直写 NULL 必须失败**（证明列是真门槛，不是注释）、`Up` 跑两遍幂等、新代码写 nil 往返仍是 nil、`nulled=2` 精确等号（不是 `≥1`）＋ event/judge 的既有观测逐行原样 |
| 前端 | `npx vitest run`（**不带过滤**，整套） | `Test Files 18 passed / Tests 265 passed`，`rc=0`（18.41s）。本批新增的 5 条在内，另 260 条是回归面 |
| 变异（服务端，2026-09-21 本轮实跑复证）〔**复现路径已常驻化**：那两份 `/tmp` 脚本随重启即失效，同六刀现在住在 `scripts/mut_command_log_ok_b20b.py`，2026-09-22 全量实跑读数见 §7.28 末两行。另：本行末尾那两个 md5 是"就地注共享树"那次的**当时终态**，`executor.go` 此后被批21 再改过（现值 `5bb50835aa08db2918266303c367e7de`，mtime 2026-09-22 00:47），迁移文件未再动（现值仍 `18178bda…`）〕 | `/tmp/b20b_remute.py` + `/tmp/b20b_remute2.py`：每刀 `shutil.copy2` 就地备份 → 注码（锚点命中数必须 `==1`，注码后 md5 必须变）→ 跑该组 → 还原 → 比 md5；判据一律读红因 | **6 刀全杀，还原异常 0**。M1 命令帧退回 `verdict(true)` ⇒ `TestB20BCommandFramesCarryNoVerdict` 红；M2 `writeConfirmFrame` 值改名（帧照落、名字不对）⇒ 腿二/腿三各红一次，红因 `write_confirm 帧数=0 want 1`；M3 摘掉 `auditEventPayload` 脱敏 ⇒ `TestB20BWriteStepEmitsConfirmJudgeFrame` 红（正文零泄漏那一断言）；M4 把回填范围写成不命中的 `direction` ⇒ `command 帧无结论行数=1 want 2`；M5 不摘 `NOT NULL` ⇒ `Up() failed: ERROR: null value in column "ok" ... violates not-null constraint (SQLSTATE 23502)`（**这就是"存量库不改列，新代码第一次写 nil 就炸"的原形**）；M6 不摘 `DEFAULT` ⇒ `Up 后 ok 应为可空且无默认值, got nullable=true hasDefault=true`。终态 md5 回基线：`executor.go bdeff0422b15cb0b7581d4a87be91af5`、迁移文件 `18178bda923ed95a9e793fe92c9caa44` |
| 变异（前端，同轮复证） | `outcomeMark` 退回 `(ok ? '✓' : '✗')`，`npx vitest run tests/unit/browser_cmdlog_b20b.test.js` | `Tests 2 failed \| 3 passed`——行为锁（`null 画「—」而不是「✗」`）与静态锁（`判定函数得对「无结论」与「结论为否」分开`）各咬一口，即"改服务端不改前端"这条接缝两边都有人守；`Monitor.vue` 还原 md5 `313776c3fcab681d5fa66bd44f3b97ab` |
| 变异自身被抓的一处 | 第 1 轮复跑 | 原计划的 M4 是**整段摘除回填循环**，实跑 `[build failed]`（`Up` 末尾的 `return nil` 在那段循环里，删循环即删返回）——那是 BUILD-BROKEN 不是杀，**绝不计入已杀**；改成"只把范围写错"这一刀后，红因精确落在回填计数上。另：`find user-server -name "*.bak"` 复跑前后各查一次为空，确认上一轮的快照没有留在这棵树里污染基线 |
| 全量门禁（本泳道 Go） | `go test -count=1 -v ./internal/browser_automation/...`（**不带 `-run`**），日志 `/tmp/b20b_lane_gate.log` | `lane_rc=0`，5 包全 `ok`（controller 14.873s / dto 3.065s / platform 1.714s / repository 10.958s / service 215.197s），`^--- PASS`=213、`^    --- PASS`=26、FAIL=0、SKIP=0。与批20e 记录的 207/26 比顶层 +6：**5 条是本批 `TestB20B*`，余 1 条不在本批新增面**（本批在 service 只新增这一份测试文件；已按 `git diff --unified=0 | grep '^+func Test'` 核过被改的既有测试没加新函数），按口径漂移登记、不认领 |
| 门禁 | `go vet ./...` rc=0；`gofmt -l internal/browser_automation/ internal/migration/` 空输出；`make fmt-check`；`bash scripts/check-architecture.sh`；`check-date-bucket-tz.sh`；`check-unwired-assets.sh`；`golangci-lint run ./...`；`npm run lint:check` | 见下两行归因。日期口径 `21 处（基线 21 处）` ✅；未接线台账 `58/68 已接线，其余按登记保持未接线` ✅；golangci-lint `0 issues`；ESLint 全仓 `0 errors / 18745 warnings` rc=0（与 CI 注释里那份 18788 同量级） |
| 门禁红不认领（两处，均为并行 lane 的**未跟踪**新文件） | `make fmt-check` rc=2、`check-architecture.sh` rc=1 | ① fmt 唯一未过文件是 `internal/controller/wechat_batchf4_m01_inbound_test.go`，`git status` 为 `??`；② 架构门唯一红是 `internal/service/dingtalk_media.go:191,204`「service 直接调 db」，同为 `??`。归因不需要猜：`check-architecture.sh:114-134` 的 `[L4] 直接调 db` 规则只扫 `$TARGET/internal/service/`，而本批改动全在 `internal/browser_automation/**` 与 `internal/migration/**`，两棵树不相交。本批自己的面单测过 gofmt：`gofmt -l` 空输出 |
| golangci-lint 的版本口径（记一笔，别让下批当成 0 issues 的凭据） | `golangci-lint --version` = **v2.1.6**，而 CI 与 `Makefile:320` pin **v2.10.0** | 本机那份跑不了 CI 的 pin（`make lint` 有 `lint-version-check` 前置），装新版会覆盖并行 lane 正在用的同一只二进制，属改变共享环境，故不擅自换。读数「0 issues」只对本机 v2.1.6 成立；CI 注释自陈 v2.13.2 相对 v2.10.0 会另报 6 条，换版本从不等于修存量 |

### 八、登记（看过、本批不做）

- `docs/architecture/BROWSER_AUTOMATION.md:220`（六表 schema 速查）仍把 `ok` 记成无括注的一列，
  且整份 master 文档停在 v1.2 / 2026-09-11：§4.4 写的是「migration v3.37/38/39」、§0 写的是「v3.37.0–v3.39.0 迁移三件」，
  而 v3.40.0–v3.44.0 与批14–20b 共 7 个批次已落。**这不是本批的遗漏，是本泳道自批14 起的系统性 drift**：
  按该文档自己的规矩（§5.4「任何对 §4 事实层的改动需追加勘误行并同步修订记录」），只补 `ok` 一格
  会让它看起来像"只有这一处过时"，比整段标注过期更误导。回灌 master 文档另立一卡（含版本号与
  §4.4/§0/§5.4 三处对齐），不塞进功能批。
  > **2026-09-22 本轮收口（卡片已执行，登记项作废）**：`BROWSER_AUTOMATION.md` 升 **v1.7**，
  > 按其 §5.4 自己的规矩做三处对齐＋勘误：§0（迁移三件→本泳道七件、六表→九表、D6/D7 现状）、
  > §4.3（G4 心跳缺口已闭：`host_registry.go:98,130-135`）、§4.4（`:213` 标题、`:220` 的 `ok` 加三态括注、
  > 逐表补 v3.40/42/43/44 的列、新增三张表、G1/G3 过期标注改写、轨制现状注）、§4.6（版本锚点 1.4.2→1.5.0，
  > 实测三处字节一致）、§5.1（测试面按实测 49/244、17/181、54/700 重写，G8 的"零覆盖"表述作废但**深度未评**）、
  > §5.4 追加勘误 **B11/B12/B13**、修订记录加 v1.7 行。顺带就地修两处源码注释里的事实错误：
  > `model/step.go:16`（11 条动作名单改为指向 `dto/task.go` 的 oneof 唯一事实源）、
  > `pkg/db/migrate.go:382`（"browser_steps 两列零 DDL 文件"——恰恰有，即 v3.43.0）。
  > 反向读数：`npx markdownlint-cli2` 全仓 170 files `0 issues`；`gofmt -e -l` 两文件空输出。
  > **未做**：批14–22 的功能级叙述不进 master 文档（论证与证据住本 spec §7.15–§7.28 与 `ledger/R22.jsonl`），
  > 避免同一件事两个事实源——这正是该卡选择"另立一卡"而不是"顺手补一格"的同一理由。
- `appendCommandLog` 的 `Ignore` 语义（写审计失败不阻断执行）本批不动：它是增强件不是闸门（§8.3 第 13 行同款理由）。
  但要记一句：**审计流自己也可以悄悄少行**，M2/M4 那两格杀的就是"少行的形状"，而"整帧不落"这一格今天仍无人守。
  它属 A6（裁剪自证）那一条线，不属本批。
- `comment_verify` 的回包 `evidence` 在流侧被摘掉后，`browser_command_log` 里再无处可查「平台侧到底认到了哪段文本」。
  这是**有意的**：结论住在 judge 帧，证据住在可变行。若日后要"离线件自带证据"，那要先给 I5 导出加脱敏，
  而不是回到流里塞正文。


## 7.25 批20c：三条 trusted 写通道的最后一处补上「stable + 点后身份复核」（§8.2-1 尾项收口）

### 一、形状：同一族缺陷有第三个消费点，而它是三条里唯一不可逆的那一条

批17 落 (a)(b) 时覆盖的是 `click` / `click_near`。`comment_send` 走的是同一条路
（页内算按钮中心点 → `cdpInput.clickAt(tabId, x, y)`，现 `primitives.js:997-1003`），
两份判定都没有。§8.2-1 在批18 之后把它登记为开放项，登记时给的安慰是"它有第二条通道兜住假绿"
（Go 侧 `finalizeComment` 的只读回查）。本批把那句话反过来读：**回查兜住的是结论，兜不住的是
"这一次公开提交已经发生"**。点错元素在 `click` 上可撤销，在评论上不可撤销——
所以三条里最该有闸门的恰恰是最晚有闸门的那一条。收益按登记里的原话兑现：更早、更便宜地判红。

### 二、复核要有对象：没有 selector 的按钮必须自己回传一条可再解析的路径

`click` 的 target 是调用方给的 selector，`click_near` 靠 probe 回传 `pathOf(hit)`。
发送按钮不一样：它是"输入框往上 4 层找文本"现场算出来的，调用方手里**没有任何定位**。
于是 `injPostCommentSend` 必须自己回传 `selector: pathOf(btn)`（`primitives.js:540-562`）——
不回传的话，点后那次复核就是拿空串去 `querySelector`，结果是"无从复核却照样 `sent:true`"，
**加一道复核反而多买一处静默 ok**。这一格单独有腿（`batch20c-send-identity.test.js` 第 5 条），
理由与 §8.3-21 同一条：新加的闸门要能证明它自己会被走到。

### 三、`stable` 是第三份内联副本，与批19 的"半径漏传"同族

三份 probe 各自内联一份可点性判定（注入函数在页面里自包含，不能共享闭包），
这个形状已经因为"第三份少一个字段"出过一次批19。本批把 `settleBox`（rAF 与 `setTimeout(fin,50)`
双兜底、500ms 预算、比盒四边，`:514-531`）补进第三份，并且**落位之后重判一次** `check(btn)`
（`:534-536`）：等待期间才挂上来的浮层、刚被 disable 的按钮，都必须拦下这次提交而不是拿到旧坐标。

### 四、开关在 Go、结论在扩展，两侧各拆一刀

`service/hand.go:203` 给 `comment_send` 帧带上 `verify_identity`；扩展回包带 `identity_checked`。
Go 侧三条腿：①帧必须带开关（请求侧不发这个布尔，执行侧那道闸门在真机上一次都不会跑）
②结果必须记 `identity_checked` ③复核给出 `element_moved` 时仍按"越点已尝试"落账，
不得因为"这次没提交成功"就把步退回可自动重发态（那是 §7.8 的 A7/A8 口径，不能在新闸门上松动）。
`comment_send` 与另两条的**差异**也钉在腿上：自愈重发（`send_button_not_found` 归因）落第二对帧，
审计面记录动作次数，运行时红线管动作（§7.24 五）。

### 五、证据（全部跑出来的）

| 层 | 跑法 | 读数 |
|---|---|---|
| 新行为腿（扩展） | `npx vitest run test/batch20c-send-identity.test.js` | `Test Files 1 passed / Tests 10 passed`，984ms（`/tmp/b20c_ext2.log`） |
| 与批17 同族合跑 | `npx vitest run test/batch20c-send-identity.test.js test/batch17-actionability.test.js` | `2 passed / Tests 30 passed`，5.84s（`/tmp/b20c_ext.log`）；两份 probe 对照腿（`click_near` / `comment_send` "同样有牙"）各在批17 文件里点名 |
| 回归面 | `npx vitest run`（browser_automation 整套，不带过滤） | `Test Files 17 passed / Tests 181 passed`，33.41s（`/tmp/b20c_ext_full.log`） |
| 变异 | `python3 scripts/mut_actionability_b17.py`（本批把第三消费点并进批17 那只电池；本批新增的格是 `M3` 第 3 份 stable 装死、`M13` `comment_send` 分支复核单独断线、`M14` 第三份 probe 不回传 `selector`、`M15` `pathOf` 退化成 tagName、`M16` 未请求复核也冒充 `identity_checked`、`M17` 整块复核摘掉，另 `M12`（批19 那格）随本批扩到三份，Go 侧 `G8`/`G9`/`G10`/`G11`） | **2026-09-22 复跑读数**（`/tmp/b17_20c_remut2.log`，`rc=0`）：`===== 电池判定：JS 17 格 + Go 11 格 逐格被杀，无存活 =====`；JS 控制组 `rc=0 total=30 failed=0 skipped=0 红名=[]`，Go 控制组 `rc=0 settled=13 passed=13 skip=0 FAIL=[]`，两侧收尾各一句 `[JS]/[Go] 已全量还原（md5 一致）`。本批那几格的红因逐条点名到腿：`M3`→「comment_send 的发送按钮 probe 同样有牙」+「unstable 判据在三处 probe 里各出现一次」；`M13`→7 条红（含「提交后目标消失…绝不再点第二次」）；`M14`/`M15`→「复核的再解析对象 = probe 回传的那条路径，且它确实指回被点的按钮」；`M17`→9 条红；`G8`/`G9`→`TestCommentSendFrameRequestsIdentityRecheck`；`G10`/`G11`→`TestCommentSendResultRecordsIdentityChecked`。**两格同族如实登记**（脚本自己报的，不是事后找补）：`G1 与 G2 杀掉的用例集合相同（1 条）`、`G8 与 G9 杀掉的用例集合相同（1 条）` ⇒ 请求侧的"帧不再请求"与判据侧的"is_write 恒 false"在 Go 侧只有一条腿能分辨，另一处要收紧得先加一条能分辨它们的腿 |
| Go 三条腿 | `comment_send_identity_b20c_test.go`（`TestCommentSendFrameRequestsIdentityRecheck` / `…ResultRecordsIdentityChecked` / `…ElementMovedRecordsCrossedAttempt`） | 批20c 当时未单独留日志 ⇒ 记为**取证口径漂移**（同一批的 JS 侧留了三份日志、Go 侧零份）。**该缺口已由 §7.25 上一行那次复跑 discharge**：电池 Go 控制组就是这三条腿的一次全绿实跑（`settled=13 passed=13 skip=0`，三条腿的名字逐条出现在 `G8`–`G11` 的红因里 ⇒ 它们确实在跑、且确实被这四刀打到）。随本轮全泳道门禁还各复跑一次，读数见 §7.28 末行 |

## 7.26 批20d：出站重推必须有上界并落终态（A3），去重台账必须有时间界（A4）

### 一、A3 的形状：owed 集合的语义是对的，缺一列，于是"永远发不出去"= 永远重推

`FetchOutboundUndelivered` 的判定本身没错（`pending` 或 `inflight && claimed_at < now()-30s`；
`delivered/failed` 自然离开）。缺的是尝试计数：一条"桥端每次都发不出去"的行（目标会话已删、
页面改版但仍在线）会以 `claimTimeout` 为周期在 `pending ⇄ inflight` 之间**永远轮流为真**，
没有哪一轮把它升级成终态，也没人知道它已经推过 200 次。同行都有这一界
（Sidekiq 25 → `dead`、SQS `maxReceiveCount` → DLQ、River `discarded`），本仓取
`MaxOutboundPushAttempts = 20`（`repository/message_hub_inbox_outbound.go:22`）。

- 计数必须落在**库里的列**而不是内存计数器：SW 回收、进程重启、多实例认领同一行，
  任何内存态都会在下一轮从 0 开始，等价于没设界。
- 到界即离开 owed 集合，且**三条取行路径同口径**（轮询认领 / SSE 单行认领 / 待推列表补拉）——
  只给一条加界等于没给，换条路径照样永推。
- 到界必须落**已有**终态 `failed` 并把原因写进行上（`push_error`），不是悄悄消失：行还在、能查、能解释。
  原因里不重复带上界数字（同一行的 `push_attempts` 就是那个数，两份事实会漂移）。
- 超时回收（`inflight → pending`）不许把计数清零，否则预算每 30s 重置一次，上界形同虚设。

### 二、v3.45.0 两列：默认值等于代码缺省，且存量刻意不回填

`ADD COLUMN IF NOT EXISTS push_attempts INTEGER NOT NULL DEFAULT 0` +
`push_error VARCHAR(200) NOT NULL DEFAULT ''`（默认值口径沿用 v3.43.0：列缺省 ≠ 代码缺省时，
存量行读出来与"新行还没推过"无法区分，上界判断会把老行当成已到界）。
**存量不回填**是刻意的：把历史 `pending/inflight` 行的 `push_attempts` 填成非 0，
等于在升级那一刻直接判掉它们的一部分预算；填成上界更是当场把真实待发的消息标成 `failed`。
老行从 0 起算至多多活一轮预算，而错填会直接吞消息——两个方向的代价不对称。
`Down` 不 DROP 这两列（`declineColumnDrop`）：降级时列里装着在用的事实。

### 三、A4：`SentCache` 加 24h TTL，不是同行那个 5min

桥端 `SentCache` 早就持久化到 `chrome.storage.local`（不是内存态），但只有条数上限
（`sentCacheMax:2000`）没有时间界，且命中已有 key 不刷新插入位 ⇒ 淘汰纯按插入序。
本批给条目带写入时刻，装载与追加时按 `sentCacheTtlMs`（`constants.js:253`，`24*3600*1000`）回收，
超上限按"最旧的时间戳"淘汰（`downlink.js:13,57,71`）。**为什么取 24h 而不是 SQS 的 5min**：
服务端的重推窗取决于浏览器离线多久，去重窗必须 ≥ 上游仍能重投的时长，
否则"缓存先过期、重推后到达"反而放大重复——照抄同行的数字会把它自己的前提带进来。

### 四、证据（全部跑出来的）

| 层 | 跑法 | 读数 |
|---|---|---|
| 契约腿（仓储，真 PG 影子库） | `message_hub_outbound_push_cap_b20d_test.go`：`TestClaimPendingOutboundCapsPushAttemptsAndTerminalizes` / `TestInflightRecycleKeepsPushAttemptBudget` / `TestClaimOutboundForPushHonorsCap` / `TestFetchOutboundUndeliveredExcludesAndEscalatesExhausted` / `TestPushCapSweepTouchesOnlyOwedOutbound` | 5 条 + 迁移 5 条 = 电池控制组 `total=10 skip=0`，每趟电池各自重测（`/tmp/b20dmut-go-r2.log`） |
| 迁移腿 | `v3_45_0_..._test.go`（元信息 / nil-db / Up+列形状 / 无表守卫 / 注册链 / Down 不销毁） | M1~M7 逐格点名 `TestOutboundPushBudgetMigration_*` |
| A4 行为腿（桥端，RED→GREEN） | `npx vitest run test/downlink-b20d-sentcache-ttl.test.js`：A 界内(23h)不重发 / B 界外(25h)重发并续期 / C 上限按时间淘汰 / D 旧格式可读并改写 / E TTL 下界锁 ≥24h | RED 实测在实现前：`5 tests | 5 failed`（`/tmp/b20d-a4-red.log`）；实现后全绿 |
| 变异电池 | `python3 scripts/mut_push_budget_b20d.py` | `JS 9 格 + Go 20 格 逐格被杀，无存活`，`BATTERY_RC=0`（`/tmp/b20dmut-full-r1.log`；Go 侧单独复跑 `/tmp/b20dmut-go-r2.log` 同判 `Go 20 格 逐格被杀`）。电池自带 7 组同族提示（`R1≈R7`、`M1≈M2≈M3≈M4≈M7`…），是同一段 SQL 被多处消费的正常形状，不是判据重叠的告警 |
| 回归面（桥端全套） | `npx vitest run`（bridge 整套，不带过滤） | 第一次全套：**7 条红**（`/tmp/b20d-a4-green1.log`：`3 failed files / 7 failed | 730 passed | 7 skipped`）。红因分两类，都不许含糊过去：① A4 把 `chrome.storage.local` 的条目形状从纯字符串改成 `[key, ts]`，撞掉 6 条**既有断言**（`downlink.test.js` 3 条 + `security.test.js` 3 条，全是 `expected [ [ 'm-d1|c1', 1790… ] ] to include 'm-d1|c1'` 这种"旧形状还在"的断言）⇒ 改的是夹具对形状的认知，不是放松判据；② 1 条 `b7-humanized-typing` 的 `Test timed out in 5000ms`，单跑 11 条 `2.47s` 全绿（`/tmp/b20d-b7.log`）⇒ **负载红，不是回归**。形状夹具改完后复跑 6 文件 62 条全绿（`/tmp/b20d-a4-green2.log`）；全套的最终复跑读数见 §7.28 末行（本轮合并门禁里 bridge 再跑一次不带过滤） |

## 7.27 批20f：双发闸从「读后再写」下推到存储层的唯一约束（A12 拍板并落地）

### 一、批16b 留下的那句话，本批兑现

§8.3-20 当时的登记是"不采纳再补一道 check-then-act，诚实收口是给台账加部分唯一索引，
但它是 DDL 决策"。**A12 的拍板结果：做**。理由不是并发变宽了，而是读码确认了另一件事——
`guardResubmit` 依赖的 `browser_steps.submit_state` 是一张**可变行**：任何一次写失败、
任何一次裁剪、任何一条并发的腿把它改回去，闸门依据就消失了。把"这次提交是否已被记过"
交给一条唯一约束，判定的现场就从 Go 挪进了库，而库是唯一那个"两条腿都绕不过去"的地方。

### 二、形状：`(task_id, text_hash)` 一行一坑，占 / 判 / 放三件事各自有牙

- **占坑**：`INSERT ... ON CONFLICT (task_id, text_hash) DO NOTHING`（`repository/write_claim.go`）。
  插进 1 行 = 占到；插 0 行 = 回读持有者。**持有者是自己**（同一步重入）幂等放行，
  持有者是别人 = 拒发，且拒在**零帧下发之前**（腿 `S_REFUSE`）。
  键不完整（空正文 / 零号步）直接拒，不往库里插一条"什么也没说"的声明（`R_KEYS`）。
- **约束住在库里**，不住在一段 Go 代码里：模型标签上 `uniqueIndex:uk_browser_write_claims_task_text`
  两列（`model/write_claim.go`）。电池 C15 把两列**一起**摘成普通索引，闸门当场只剩 Go——
  那一格判据点名 `TestWriteClaimSchemaContract`，它读的是 `pg_indexes`，不是读代码。
- **释放分三个条件两个时机**：台账已跨过提交点（`sent`/`unattributed`/`verified`）就**留坑**（`S_KEEP`）、
  本会话台账写失败过就**留坑**（`S_LEDGER`，把唯一凭据删掉等于只剩读闸一列）、
  台账态读不出来也**留坑**（`S_READFAIL`，判不出折向放行是最坏的一侧）；
  从未跨过提交点才 `defer` 释放（`S_RELEASE`）。`defer` 变同步调用是另一格（C14：窗口长度归零）。
- **自愈重发落第二对帧**：与 §7.24 五同口径，运行时红线管动作次数，审计面如实记次数。

### 三、`writeClaimRepo == nil` 必须 fail-close，而且这一格差点被注码骗过去

闸门没接线时**拒绝下发**而不是"当没这回事"（`S_UNWIRED`）。这一格的电池注码在首轮判 `BROKEN`：
最初把条件写成 `== nil && false`，于是走到 nil 接口上调 `ClaimWriteSlot` 直接 panic 带走整个二进制——
"删掉一条腿"不是"让它放行"。改成整块替换成"未接线视为无需裁决"（照抄 `cmdLogRepo` 的可选注入形状）后，
C8 精确红在 `TestWriteClaimGateFailCloseWhenUnwired` 一条上（r2 复跑记录）。

### 四、证据（全部跑出来的）

| 层 | 跑法 | 读数 |
|---|---|---|
| 契约腿 | `repository/write_claim_a12_test.go`（8 条：占坑 / 判出持有者 / 只释放自己的坑 / 约束住在库里 / 并发单胜者 / schema 契约 / 键不完整拒占 / 幂等）+ `service/write_claim_gate_a12_test.go`（6 条，真 WS + 影子库跑完整三段式） | 两包合起来 `total=14 skip=0` 全绿（每趟电池各自重测控制组） |
| 变异电池 | `python3 scripts/mut_write_claim_a12.py` | r1：`15 格里 14 格被杀`、`C8 BROKEN=判不了`，`BATTERY_RC=1`（`/tmp/a12mut-r1.log`）；按上节换注码形状后 r2：`Go 15 格逐格被杀，无存活`（`/tmp/a12mut-r2.log`）。C15 那格一次带走 12 条腿（约束没了，读侧与写侧一起塌），是同族而非误判 |
| 表登记的牙 | 摘掉 `migrate.go` 里 `&browsermodel.BrowserWriteClaim{}` 的注册 → `python3 scripts/check_model_migration.py` 必须红并点名 | 三刀（write_claim / audit_digest / audit_prune_run）各 `rc=1` 且点名对应模型，还原后 `rc=0`，逐文件 md5 回基线。这一格电池管不到（克隆里的表由 testutil 按模型标签直建），所以门的反向验证是它**唯一**的牙 |
| 全泳道门禁 | `go test -count=1 ./internal/browser_automation/...`（不带 `-run`） | `lane_rc=0`，5 包全 `ok`（controller 4.207s / dto 1.108s / platform 2.237s / repository 1.820s / service 189.816s），`^--- PASS`=204、子测试 PASS=21、FAIL=0（`/tmp/b20f_lane_gate2.log`） |

## 7.28 批22：裁剪不等于证据消失（A6 落地：digest-before-delete + 永不裁剪的自证小表）

### 一、事实：90 天一过，"那三天没发生事"与"那三天的记录被删了"在库里完全同形

`PruneBefore` 是整行删除（`BROWSER_AUDIT_RETENTION_DAYS` 定界）。§7.24 把"这一帧有没有结论"修对了之后,
剩下的更硬：**这行流水本身可以被合法地消失**，而消失之后没有任何东西能证明它存在过。
这不是理论风险——`PruneBefore` 的界由环境变量决定，改一次配置就能在保留期内删掉一段历史，
而库里连"改过界"这件事都不留痕。同行口径（§8.3-6）：CloudTrail 给每个交付窗口留 digest
（`logFiles[].hashValue` + `previousDigestHashValue`），并且**`logFiles: []` 是一条可断言的事实**；
RFC 6962 用 inclusion proof；pg_partman 用 `retention_keep_table=true`（detach 而非 drop）。
本批落的是同一条路的 Go/PG 形状：**删除前把这一批的内容折成指纹，写进一张永不裁剪的表**。

### 二、算术要闭合：删掉的行 = 摘要里的行 = 留痕的差

两张新表（`model/audit_digest.go`，登记在 `internal/pkg/db/migrate.go:367`，无版本化迁移——
纯新表，`AutoMigrate` 建，与批20f 同口径）：

- `browser_audit_digests`：一行 = 一次裁剪批里属于某个 session 的那一段。
  `(session_id, ordinal)` 唯一（`uk_browser_audit_digests_session_ordinal`）——**重放同一次裁剪会撞约束**，
  而不是悄悄多出一条并行的历史；本表若可重复，它就失去作为凭据的资格（与 §7.27 同一条理由）。
  刻意不带 `gorm.DeletedAt`：软删的摘要行等于给"抹掉证据"留一条正规通道（腿 `R_STORE` 直接读
  `HasColumn(deleted_at)` 判它不存在）。
- `browser_audit_prune_runs`：一次扫描一行（`rows_before` / `pruned` / `digests` / `cutoff`），
  **0 行的扫描也写一行**——那正是 CloudTrail `logFiles: []` 的用法：空摘要不是一句"没跑"，
  而是一句可断言的"该界内无到期行"。

闭合算术是这张表的资格：`rows_before = pruned + 前一轮已删`，且 `digests` = 摘要行数 = 每批每 session 一条
（腿 `R_CLOSE`；电池 D16 抹掉 `RowsBefore`、D17 把摘要计数改成按批计数，各红在这一条上）。

### 三、指纹要能证内容：三个字段各自一刀

`batch_digest` = 库内 `sha256` 逐行哈希后按 `id` 序 `string_agg` 再二次 `sha256`
（payload 单行可到 64 KiB，一批 5000 行拉到 Go 里哈希就是把治理任务变成内存炸弹）。
逐行哈希吃九个字段，其中三处是电池逐格验出来的**敏感性**：

- `payload` 全文（D9）。这条腿的形状也被电池纠正过一次：最初写成"两个 session 各一行、正文不同"，
  而 `session_id` 本来就在哈希里，跨主键对照**证不了任何单列的敏感性**——注掉 payload 那格当场存活。
  现在的形状是同 session 两轮、`seq/action/direction` 全同、只有正文不同，
  并先用前提锁钉住"两轮的 (行数, first_seq, last_seq) 必须相同"（前提不成立时这腿会假绿）。
- `created_at` 必须先 `AT TIME ZONE 'UTC'` 再转文本（D10）。直接 `::text` 按**会话时区**渲染，
  同一行数据在主库与任何一颗 UTC 副本上会折出不同指纹，摘要就从"内容的指纹"变成
  "内容 + 那条连接的环境"。本仓为同一件事摔过一次（日期分桶裂脑，§见门禁 `check-date-bucket-tz.sh`），
  所以这条腿是**在非 UTC 会话时区下**跑的（`a6DigestUnderSessionTZ`）。
- `ok` 的三态不许折成两态（D11）。`CASE WHEN ok IS NULL THEN 'n' WHEN ok THEN 't' ELSE 'f' END`：
  §7.24 刚把"无结论"从假 `✓` 里解放出来，摘要这边若把 `NULL` 与 `false` 折成同一个字符，
  等于在凭据里把它重新塞回去。腿 `R_OK3` 用两轮（`ok=nil` 与 `ok=&false`、其余字段全同）钉这一格。

### 四、digest 先落库、写不上就不许删：这一格的真牙在"读得到、写不进"

每一批的摘要 INSERT 与该批 DELETE 在**同一个事务**里；写不进（缺表、约束冲突、库不可达）就整批回滚。
两条 fail-close 腿管的**不是同一件事**，而且先后顺序很要紧：

- 「缺表」那条锁的是前置条件（表整体不在时同样不许删），但它**证不了写侧有牙**——表不在时
  `lastAuditDigest` 的 SELECT 先报错，路径根本走不到 INSERT。这一点是电池逼出来的：
  D1（摘要根本不落库）与 D2 的初版（吞掉 `tx.Create` 的错）都没能让它变红。
- 「读得到、写不进」这条才是写侧的牙：给 `browser_audit_digests` 挂一条 `CHECK (FALSE) NOT VALID`，
  摘要插不进去，`PruneBefore` 必须报错、一行都不许消失；末尾还有一条反向锁——
  约束摘掉后同一批行必须裁得动，否则上面的红可能只是夹具把整条路一起堵死。
  **夹具本身也被实测审过一遍**（2026-09-22）：最初用 `BEFORE INSERT` 触发器挡写入，
  触发器在 `pg_trigger` 里查得到、摘要照样插进去——`testutil` 建池时执行了
  `SET session_replication_role = 'replica'`（`internal/pkg/testutil/testdb.go:180`），
  PG 默认（ORIGIN）触发器在这个角色下**静默不触发**。补一句 `ALTER TABLE ... ENABLE ALWAYS TRIGGER`
  确实有牙（两条都量过），但 CHECK 不受这个角色影响、少一处环境耦合，故用后者。
  这类"看起来挂上了其实没生效"的夹具，比没有夹具更糟：它会让那条腿永远绿着替实现担保。

链侧另有三格各自一刀（D3 摘链哈希、D4 摘 `prev_seq`、D5 把 `ordinal` 永远写 1）：
`chain = sha256(prev_chain_hash || batch_digest)` 让这张表自己也是条链，
只删中间某一批 digest 会当场断链，否则"删日志"可以升级成"连摘要一起删、再补一段假的"。

### 五、导出面：`null` 与 `[]` 是两句话，不能都说成"没东西"

`SessionExport` 多返回一组摘要，`GET /sessions/:id/export` 的载荷多一个键 `audit_digests`。
两条控制器腿（真 `gin` 上下文 + 影子库）：

- ①**按 `ordinal ASC` 出**，且逐字段与库里一致，第二条的 `prev_chain_hash` 必须等于第一条的 `chain_hash`
  ——离线件拿到手就能接链，不需要读侧自己猜序（D14 把 `Order` 换成 `id ASC` 即红）。
- ②`audit_digests: []`（已接线、该会话确实没裁过）与 `null`（读侧没接线）**必须不同形**。
  这一格是 I5 导出唯一的解释面：漏一行 setter 与干净的库若都导出"没有摘要"，
  读离线件的人就再也分不出"没裁过"与"没人去查"。键名换掉（D13）也红在这里。

### 六、Go 到不了的两格，交给两道门并各自反向验过

`repository/audit_digest.go` 的装配、以及 `migrate.go` 里两张新表的登记，都在 Go 用例的可达面之外
（用例就地 new 仓储/服务，没有任何一条走 `router.Setup`）。所以：

- `scripts/check-unwired-assets.sh` 新增**第 22 项**（定义式 `func NewBrowserAuditDigestRepositoryWithDB`
  → 消费式 `SetAuditDigestRepository\(`，scope `internal/router`，期望 wired）。
  正向：`项22 已接线 接线数=1`；反向：摘掉 `router/browser_automation_routes.go` 那一行 setter
  → 该门 `rc=1` 且点名"项22"，还原后 md5 回基线。
- `scripts/check_model_migration.py` 管表登记。本批顺带修了**这道门自己的一个假红源**：
  `collect_models()` 不跳 `_test.go` 而 `collect_registered()` 跳，于是测试夹具里一个带
  `gorm:"primaryKey"` 的临时 struct 会**永久报红**（它永远不该进 `allModels()`）。
  永红的门等于没有门——真漏登记时读数同样是红，反向验证当场失去判据。
  触发形态是并行 lane 未提交的 `internal/pkg/db/migrate_tolerance_batchm7_test.go` 四个夹具；
  改的是门的文件域，不动它任何一条判据。

### 七、证据（全部跑出来的）

| 层 | 跑法 | 读数 |
|---|---|---|
| 仓储契约腿（12 条） | `go test -p 1 -count=1 -v ./internal/browser_automation/repository/ -run TestA6`（真 PG 影子库，8232） | 12 条全 `--- PASS`，包 `ok 1.327s`（2026-09-22 本轮）。逐腿：`DigestCoversDeletedRows` / `DigestClosesOverRunRow` / `ChainLinksAcrossRuns` / `DigestRecordsSeqGap` / `BatchDigestSensitiveToPayload` / `PruneFailsCloseWhenDigestWriteBlocked` / `PruneFailsCloseWithoutDigestTable` / `EmptyWindowStillWritesRunRow` / `DigestReproducibleFromRowContent` / `DigestSplitsWithDeleteBatches` / `DigestSeparatesUnackedFromFailedFrame` / `DigestChainIdempotentAtStorage` |
| 读侧 / 导出腿 | `TestSessionExportAggregates`（service，真 WS + 影子库）+ `TestA6ExportPayloadCarriesAuditDigests` / `TestA6ExportDistinguishesUnwiredFromNothingPruned`（controller） | 含在下方电池的控制组里：`total=16 skip=0` |
| 变异电池 | `python3 scripts/mut_retention_a6.py`（16 格；单次命令预算 10 分钟装不下 16 次全量跑，故按 `--cells` **分四趟**跑：`D1,D2,D3,D4,D5,D15` / `D9,D10,D11,D8,D16,D17` / `D7,D14,D12,D13` / 复跑 `D2`） | **16 格逐格被杀，无存活**。每趟各自重建 `--shared` 克隆、各自重测控制组：四趟均 `rc=0 total=16 passed=16 skip=0 FAIL=[]`。日志 `/tmp/a6mut_p1.log`（p1 里 D2 判"红了但没点出 `R_COVER/R_CLOSE`"，见下节）、`/tmp/a6mut_p2.log`、`/tmp/a6mut_p3.log`、`/tmp/a6mut_p4.log`（收窄后 D2 `杀掉`，`PASS4_RC=0`）。同族提示两组：`D3≈D4`（链哈希与 `prev_seq` 同由链腿承接）、`D16≈D17`（闭合算术一行看两个字段），逐格仍各自下刀。**"16 格"与"格号到 D17"不矛盾**：`cells()` 在磁盘上定义的是 D1–D5 + D7–D17，**D6 是编号空位、不是漏跑的格**（2026-09-22 核：`grep -oE '"D[0-9]+"' scripts/mut_retention_a6.py` 再 `sort -u` 恰 16 个），而上面三趟的并集与这 16 个**逐名相等** ⇒ 没有任何一格因 `--cells` 名单写错而静默少跑 |
| 首轮（16 格未收窄前） | 同一只电池，`expect` 未修、D2 注码为"吞掉事务内 INSERT 的错"、payload 腿为跨 session 对照 | `12 格被杀 / 4 格有洞`（`/tmp/a6_battery_r1.log`，`BATTERY_RC=1`）。四处**没有一处是实现坏了**：两处 expect 写宽（D1、D8：单批种子看不见 `LIMIT`）、一处判据形状错（D9）、一处注码无后果（D2，PG 事务 aborted 替它回滚）。逐格改的是判据与注码形状，`/tmp/a6mut_p1..p4.log` 为改后终态 |
| 两道门的反向验证 | `bash scripts/check-unwired-assets.sh`（摘 setter / 还原）；`python3 scripts/check_model_migration.py`（摘表登记 / 还原） | 见 §六；正向读数见下一行 |
| **本轮合并门禁（2026-09-22，一次性全泳道复跑， discharge §7.25 / §7.26 挂下的两处"随本轮门禁再报"）** | 一个脚本一条命令：`bash /tmp/b22_gate.sh`，26 步串行、每步全量输出落 `/tmp/b22g/<step>.log`、摘要（步名 + rc + 秒数）落 `/tmp/b22_gate_summary.txt`。**跑在共享工作树上**（`git status` 实测 393 个脏文件、跨多条泳道），所以每条红都是**读文件路径归因**、不是"先当不是我的"。开工前先测环境前提：`df` 59Gi 可用、`pg_isready -h 127.0.0.1 -p 8232` 接受连接、`load 3.7` | **本泳道零红。**<br>① Go 编译/静态：`go build ./...` `rc=0 10s`、`go vet ./...` `rc=0 4s`、`golangci-lint`（本机 v2.1.6，CI 钉 v2.10.0 ⇒ 只作参考不作门）`0 issues 8s`。<br>② Go 测试：`go test -p 1 -count=1 -v -timeout 1800s ./internal/browser_automation/...` **`rc=0 222s`，5 个包全 `ok`（controller 3.938s / dto 0.444s / platform 0.443s / repository 2.694s / service 212.427s），244 条顶层 `--- PASS` + 28 条子测试 `--- PASS`，`FAIL=0`、`SKIP=0`** ⇒ §7.25 那三条 `comment_send_identity_b20c_test.go` 腿、本批 12 条 `TestA6*` 腿、批20f 的 `write_claim_gate_a12_test.go` 腿全在这一次里复跑过。批20d/批20f 落在宿主包的腿：`./internal/repository/... ./internal/model/... ./internal/migration/...` **`rc=0 135s`，5 个 `ok` 行、0 个 `FAIL`**（repository 109.759s / migrations 17.337s）。<br>③ JS：bridge `npx vitest run` **`rc=0`、53 files、`Tests 739 passed \| 7 skipped (746)`** ⇒ **§7.26 挂账的"全套最终复跑"在此 discharge**：那次 7 条红（6 条 `[key,ts]` 旧形状断言 + 1 条 `b7-humanized-typing` 负载红）现在一条都不在；那 7 条 skip 住在 `test/p3h-e2e.test.js`，本批之前就存在、与本泳道无关。browser_automation `npx vitest run` **`rc=0`、17 files、`Tests 181 passed (181)`**（与 §7.25 记的那次同数 ⇒ 本批之后无回归）。`npm run build` 两侧各 `rc=0`；bridge `npm run lint` `rc=0`（68 warnings / 上限 80，其中 `downlink.test.js:114 prefer-const` 按 `git diff -U0` 的 hunk 位比对是本批之前的既有行，不是本泳道新增）。<br>④ 静态与文档门 `rc=0` 共 12 项：`check-date-bucket-tz.sh`、`check-unwired-assets.sh`、`check-doc-consistency.sh`（27 个关键文件全在、跳过 0 个工作区级条目）、`check-feature-doc.sh`、`check-enum-consistency.sh`、`check-no-xapptool.sh`（含其自测腿）、`audit-cross-package-ports.sh`、`check_model_migration.py`、`check-md-links-offline.py`、`check_component_types.py`、`check_workflow_refs.py`、`check-env-coverage.py`（按它自己的口径在 `--shared` 克隆 + `checkout 0a2032df` 的**只含已提交内容**树里跑，另拷 `.env` 进去）。<br>⑤ **三条红，逐条读文件路径后一律不归本泳道，也未代改**：`make fmt-check rc=2` 唯一违规文件 `user-server/internal/controller/wechat_batchf4_m01_inbound_test.go`；`check-architecture.sh rc=1` 一处 `[L4] service 直接调 db`，点名 `internal/service/dingtalk_media.go:191,204`；`npx markdownlint-cli2 rc=1` 2 issues 2 files（`docs/architecture/CHANNEL_INTEGRATION_AUDIT_2026-09.md:810`、`docs/superpowers/specs/2026-09-21-offline-deployment-design.md:957`，同族 MD004 行首 `+`）——**本批改过的那份 `2026-09-19-…-write-ledger-design.md` 不在红名单里**（本轮往 §8.2/§8.3 追加了六处状态块，MD004 一条没撞）。<br>⑥ 本轮**没有**再跑的：真机/设备腿（`#28` 那条伪造平台域名的桥接腿仍等用户的宿主与设备）、以及批20b 那只电池的常驻副本（见下一行）——**后者已在同日 discharge，读数即下两行；前者仍开** |
| 批20b 电池的常驻化 | 本批把 §7.24 引用在 `/tmp` 里的一次性脚本并成 `scripts/mut_command_log_ok_b20b.py`（六刀 M1–M6 逐字照抄，改两处：从"就地注共享工作树"改成"只在私有 `--shared` 克隆里注"；补上后面几批的口径——两个控制组分打 service/migrations、红集合要求**恰好等于**期望、`settled` 与 `skip` 逐格复算、还原逐文件比 md5） | 读数见本节末（本行由该脚本实跑回填）——**已 discharge，见下一行** |
| ↳ 常驻副本的实跑（2026-09-22，discharge 上一行） | `python3 scripts/mut_command_log_ok_b20b.py --clone /tmp/b20b_full3`（私有 `--shared` 克隆 + 本泳道脏文件覆盖 + 私有 `GOCACHE`，全量六刀一趟跑完） | **`FULL_RC=0`，判定行 `6/6 格，逐格被杀，无存活`。**控制组两条腿先自证干净：`[service] rc=0 settled=5 passed=5 skip=0 红名=[]`、`[migrations] rc=0 settled=4 passed=4 skip=0 红名=[]`。逐格读数（`settled` 全部等于所属包控制组、`skip` 全 0、红集合与期望逐格**恰好相等**）：M1 红 `TestB20BCommandFramesCarryNoVerdict`；M2 红 `TestB20BUnverifiedWriteConfirmFrameSaysSo`+`TestB20BWriteStepEmitsConfirmJudgeFrame`；M3 红 `TestB20BWriteStepEmitsConfirmJudgeFrame`；M4/M5/M6 红 `TestCommandLogOkTriStateMigration_UpAndBackfill`。**上一行"改两处"要补第三处**：M4 的注码形状没照抄 /tmp 那两份**复跑**脚本（它们摘整段 `for { … }`），常驻副本沿用 §7.24 记的形状——"把回填 `WHERE` 的 `direction` 换成不命中的值"（磁盘上的原始电池 `/tmp/b20b_battery.sh:60-61` 正是这一形：`a="WHERE direction = 'command' AND ok IS NOT NULL"` → `b="… AND 1 = 0"`）——摘循环会让 `Up()` 少掉唯一出口 return，Go 报 `missing return` ⇒ 该格 `settled=0`、一条用例名都点不出来。常驻副本首轮（注码还是摘循环那版）实跑就是这个形状：`/tmp/b20b_battery.log:9`（02:52）`M4 BROKEN=判不了 settled=0 红=—`，同文件 `:13` 判定行"1 格未杀/BROKEN"；改成 §7.24 形状后的 6/6 那次在 `/tmp/b20b_battery_full.log:9`（02:57，`杀掉`）。**编译红不是牙**，注码必须打在语义上而不是语法上；改的是注码、没动期望。**顺带把 §7.24 那条历史读数钉实**：它记的"范围不命中 ⇒ `command 帧无结论行数=1 want 2`"属于 09-21 那次原始电池，磁盘证据 `/tmp/b20b_logs/M4.log`（09-21 19:18）确有 `--- FAIL: TestCommandLogOkTriStateMigration_UpAndBackfill` ⇒ 那条结论没被推翻，漂的是后来的复跑脚本。<br>杀了的格也要读红因（电池只在未杀时打印）：在 `--keep` 留下的私有克隆 `/tmp/b20b_full2` 里手工注一次 M4、跑 `-run TestCommandLogOkTriState -v`、还原后 md5 仍 `18178bda923ed95a9e793fe92c9caa44`（与 §7.24 记录的回基线值同、也与共享工作树当前值同）。红因逐字：`v3_44_0_browser_command_log_ok_tristate_migration_test.go:119: command 帧无结论行数=1 want 2（1 条历史被回填 + 1 条新写入）` ⇒ **与 §7.24 那一行的 M4 描述与红因完全对得上**（漂移在 /tmp 脚本的注码形状，不在文档结论）。<br>同批还跑了 `--cells` 的窄口子（修单格后不必复跑全电池）。反向验证 `--cells M4,M99` ⇒ `neg_rc=1`（不经管道直取 `$?`）、报 `不存在的格：['M99']`；正向 `--cells M4` ⇒ `CELL_RC=0`、只印 1 条格线、判定行写 `1/6 格（--cells M4）`。**这条窄口子不是本轮的门**：本轮门禁认的是上面那次全量六刀 `FULL_RC=0`。改完本文件说明段后又原样复跑一次全量（`/tmp/b20b_battery_final.log`，`FINAL_RC=0`，六刀与判据一字未动）⇒ 本行读数对应的就是仓里现在这份脚本。<br>**本行以上引用的每一个 `/tmp` 路径都是一轮性产物、重启即失效**：写完后三分钟复核时 stat，`--keep` 留下的克隆 `/tmp/b20b_full2` 已经不在这台机器上了（`b20b_battery.log`、`b20b_logs/M4.log` 此刻还在，但同属会被清掉的一类）。按本轮立的协议（引用路径先 stat，不许指向不存在的东西），这里的账记法是：**常驻证据只有 `scripts/mut_command_log_ok_b20b.py` 那一份文件**——六格的形状、锚点与期望红集合全部硬编码在脚本里，克隆自己重建、控制组自己重测，重跑即复现同一组读数；`/tmp` 日志只是这一趟的事后誊本，不作为"可复核"的承诺 |

### 八、登记（看过、本批不做）

- `appendCommandLog` 的 `Ignore` 语义（写审计失败不阻断执行）仍不动（§8.3-13 同一条理由），
  但 §7.24 八里那句"整帧不落这一格今天仍无人守"**现在有了落点**：A6 之后，
  少落一条 command 帧会让该批的 `row_count / first_seq / last_seq` 与实际行数对不上，
  摘要自己把断号记下来（腿 `R_GAP`）。守的是"区间长度 ≠ 行数"，不是"帧数为 0"。
- `browser_audit_digests` 的**链完整性校验**（离线跑一遍 `chain = sha256(prev||batch)` 全链重算）
  本批不做：写侧每批都算一次已经保证"库里那条链自洽"，重算的价值在**读侧取证脚本**，
  那是 I5 导出消费方的活；在本仓再写一个只有测试会调的校验函数，等于给一个无人读的面记账。
- 裁剪的**界变更**没有留痕（`BROWSER_AUDIT_RETENTION_DAYS` 改小不会进任何表）。
  摘要能证明"删掉的那些行存在过、内容是什么"，证不了"按什么界放的行"。
  补法是把每次 `PruneBefore` 的界与来源（配置值 or 调用参数）落 `browser_audit_prune_runs`——
  本批已落了 `cutoff` 列，缺的是"这个 cutoff 从哪来"，登记为独立小项。
  **批23 已落（落法与牙见 §7.29 八）**：`prune_runs` 补 `cutoff_source` 列（`VARCHAR(64) NOT NULL`），
  来源为空即**拒绝裁剪**而不是留空行——一条说不清从哪来的界和没有界等价。

## 7.29 批23：二次审核线结论复验收口（§6 移交四项 + §8.3-9/10/17/18/19 + §7.28 八-3）

**本批的入口不是新发现，是前几批自己写下"移交 / 登记 / BLOCKED"的那几条。** §8.3 的 9/10/17/18 四行
以"落点在并行会话的在途文件"为由挂了几轮，§7.28 八-3 把自己登记的独立小项留给"下一批"，
§6-4 那句"没做的是把队列排水挂到 SSE 回调上"一直躺在移交清单里。二次审核协议
（`2026-09-22-second-review-protocol-design.md` §4 D–E、§8 验收判据）的口径是"本轮之后不允许遗留任何问题"，
于是这些条目各要有一句真落地、或一个**能被证伪的不做理由**——"落点在别人文件"从理由降级为**改动面**
（用户批准的口径是"全仓归零"）。本轮因此改了四支并行泳道正在写的 Go 文件与两支扩展文件，
逐处清单与"别人的行没动"的核法在 §十。

### 一、§6-1 + §6-3：入口那把键补会话维，且只在"没人断言过独立身份"时才按内容拦

缺陷形状（§6-1 原文）：入口 `SetNX` 的键只由 `channel + sender + content` 算出，命中即 `IsDup`
拦在入库**之前**——同一发送者在两个会话里说同一句话，五分钟内第二条既不入库也不回 AI，
客户侧表现还是"说了没回"。而这一层**没有唯一索引兜底**，所以 `webhook_dedup.go:128-130` 那句
"严禁加入 conversationID"（它管的是 `msg_id/dedup_hash` 两列的 DB 语义，那里有复合索引留位置）
不构成反对理由：同一个哈希被两种语义复用时，只有"命中即丢"那种是致命的。

- 落点：`inbox_ingress_ingest.go:157` 键尾拼 `+ ":" + event.ConversationID`；`:149-151` 注释写清
  "为什么这里的会话维是必需的、而 DB 那两层禁止它"，避免下一批人拿 `webhook_dedup.go` 那句话回退它。
- 同一段的优先级（§6-3 的"官方 id > (会话, 内容, 时间窗)"）落成形如
  `if s.cache != nil && chanMsgID == "" && !eventAssertsDistinctMessage(event.EventID)`（`:155`）：
  平台给了稳定消息 id（`channelMsgIDOf`，`:52-61`，并排掉 `wa-out-`/`tg-out-` 这类我们自造的出站 id）
  就**整个跳过内容窗口**，幂等交给钩子2 的 `msg_id` 精确判等与 DB 唯一索引；上报方用身份断言过
  独立一条（§五的 `mh:<hash>#<n>`）同样跳过。`officialEventID`（`webhook.go:396` 写进 `payload.EventID`，
  文件在 2026-09-20 已随 `1af6d28c` 入库、不再是在途件）走的是**前一条**通道，故本节不再把它接第二遍。
- 腿：`TestIngress_ContentDedupKeyCarriesConversation`（两个会话各说同一句 ⇒ 两条都不被拦）、
  `TestIngress_PlatformIDEventsSkipContentDedup`（带平台 id 的同内容连发 ⇒ 第二条不吞）。
- 牙：格 **K1**（键尾丢掉会话）与格 **L1**（摘掉"带平台 id 就跳过"那半句守卫）。

### 二、§6-2：hub 层三条内容嗅探各加时间界与会话界，且"调用方 / 仓储侧"两层各下一刀

缺陷形状：`GetByContentHash` / `GetByPlatformContent` / `GetByPlatformContentNormalized` 三条回看
"这条内容是不是我自己的出站回声"时**不看行多老**，也无会话界 ⇒ 历史里说过的原话再说一遍被静默吞。

- 落点：三个方法统一收 `since time.Time` 与 `conversationID`
  （`repository/message_hub_inbox.go:74/88/103`，SQL 里 `sent_at > ?` 分别在 `:79/94/109`，
  会话过滤在 `:81/96/111`）；调用方 `inbox_ingress_persist.go:279` 现算
  `echoSince := time.Now().Add(-InboxOutboundEchoWindow)`（2h），三条查找都传同一界。
- 腿：`TestPersistBridgeHistory_ContentDedupExpiresAfterWindow`（界外旧行不得算回声）、
  `TestPersistBridgeHistory_ContentDedupScopedToConversation`（跨会话同文本不得互吞）、
  `TestMessageHubRepository_ContentEchoScoping`（仓储侧直接喂三种界读回）。
- 牙：**同一条界拆两层**——格 K2（调用方把窗口折成零时刻）／格 K6（仓储侧接了 `since` 却不 used）
  打时间界；格 K3（调用方传空会话）／格 K7（仓储侧不接会话参数）打会话界。只拆一层，就看不出
  另一层"参数接住了但没用上"；而这在库里长得一模一样。
  形状账另记一笔：三处 `if conversationID != "" { q = q.Where(...) }` **文字完全相同**，
  锚点必须连各自那行 `Where(...)` SQL 一起取才命中一次（`--check` 就是为这种锚点存在的）。

### 三、§8.3-18：判重不再销毁记录——拦截只压 AI，唯一继续什么都不落的是自己的回声

缺陷形状：`decision.Blocked` 时单条与批次两条分支都在 `persistMessage` **之前** `return`，
于是"按内容判的重复"在库里查不到任何痕迹，而 HTTP 回执还是 `Accepted=true` 的好看形状。

- 落点：`inbox_ingress.go:516-537`（单条）与 `:980-1001`（批次）——拦截分支改成
  `if !decision.IsSelfEcho { persistMessage(...) }`，回声仍不落库（自己的话在库里已有本体，
  再落一行就是存两遍）。注释把这条不对称写在原地。
- 腿：`TestHandleIngress_DupDecisionPersistsWithAISuppressed`、
  `TestHandleIngressBatch_DupDecisionPersistsWithAISuppressed`（两条路径各一条，批里那条独立断言
  `PerEvent[i]`）、`TestHandleIngress_SelfEchoDecisionPersistsNothing`（反向半边：不许把回声也落进去）。
- 牙：格 **K4**（单条路径退回"拦截即销毁"）、格 **K5**（批次路径退回）＋ 反方向一刀由
  `TestHandleIngress_SelfEchoDecisionPersistsNothing` 承担——少了它，"改成全都落库"这种过度修法是绿的。
- 顺带把 §8.3-19 的**误判方向**消掉一半：留痕之后，`duplicate` 结论说的"已经存过了"名副其实
  （库里确有这句话的一行），"判重 ⇒ 客户端停重发"不再等于"证据消失"。

### 四、§8.3-9(c)：入口是"先占坑后落库"，落库失败必须把坑退回去

缺陷形状（这一条比原缺陷更难，是改完之后才出现的）：`SetNX` 先占坑、`persistMessage` 后执行，
Redis 与 PG 之间没有共同事务 ⇒ 第一次上报若因 DB 抖动/字段超长落库失败，扩展按重投队列原样再发一次，
第二次会被**自己上一轮留下的窗口键**判成 duplicate ⇒ 只留痕、不回 AI，而库里连一条失败痕迹都没有。

- 落点：占坑时把键记进决策（`inbox_ingress_ingest.go:154 claimedKey := ""` → `:163` 只在**真占到**时记），
  随 `IngressDecision.DedupKey`（`:28`）带回；两条路径在落库失败分支各调一次
  `releaseInboundDedup`（`inbox_ingress.go:540` 单条、`:1002` 批次，实现在 `inbox_ingress_ingest.go:174`）。
  `Delete` 失败只 Warn、不阻断：最坏退回改这条之前的形态，TTL 一过自愈，比把整条入站链 fail-close 划算。
- 可达性前提（不成立这条腿就是空转）：批次路径**不向上抛 error**，失败只折进
  `PerEvent[i].Reason` 且 `accepted=false`；HTTP 侧恒 200 + `accepted=false`，而
  `IsDuplicateReason` 按结论短语**前缀白名单**判重（`channelgw/protocol.go:357`），
  `batch handle error:` 不命中 ⇒ 扩展不确认、按队列重投。这条链写在
  `inbox_ingress_r23_dedup_release_test.go:82-86`，因为读返回值永远等不到 error。
- 夹具不给生产开门缝：往 `Extra` 塞一个 `json.Marshal` 编不了的值（函数），
  `message_hub.extra` 的 `JSONMap.Value()` 在 INSERT 时才报错 ⇒ 失败点确实落在"占坑之后、写库那一步"，
  而不是被前面的校验提前挡下；测试当场断言红因含"持久化消息失败"，夹具走错分支就停机。
- 腿：`TestHandleIngress_PersistFailureReleasesContentWindow`、
  `TestHandleIngressBatch_PersistFailureReleasesContentWindow`、
  反向半边 `TestHandleIngress_PersistSuccessKeepsContentWindow`（落库成功**不许**退坑）。
- 牙：格 **R1**（单条不退坑）、**R2**（批次不退坑）、**R3**（补偿写成无条件退坑）。
  R3 这一刀是反方向的：没有它，"成功也删键"这种看起来更保险的改法是绿的，代价是同内容连发两帧回两遍 AI。

### 五、§8.3-17：同一会话里同文本的第二条不再被吞（`mh:<hash>#<n>` 身份分层）

缺陷形状：`computeMsgID` 就是内容哈希 ⇒ 同会话第二条"好的"必然被吞，且这是 B 链路里比 Redis
更早、更永久的一层（吞进 DB 唯一索引，重启也回不来）。§8.3-17 的验收口径是"同会话两条同文本 ⇒ 2 行；
同一条重投（DOM 不变）⇒ 仍 1 行"，两条都要成立才算对。

- 落点分两端，且**扩展端不引入新配置文件**：`channel-adapter.js` 按**可见 DOM 序**给同一内容键的出现
  次数编号——`_bumpOccurrence`（`:139`）/ `_dedupKey`（`:150`）/ `_occurrenceInList`（`:159`）/
  `_canonicalMsgId`（`:179`），四个消费点（增量 `:247-250`、巡检批量 `:922-931`、单条回填 `:1005-1011`、
  另一批量入口 `:1031-1041`）全部走同一对函数。`occurrence == 0` 时 id 与旧版**逐字节相同**
  （裸 `mh:<8hex>`），只有第二条起才带 `#1`、`#2` ⇒ 存量幂等判定面不动。
- 服务端只豁免**内容维度**：`occurrenceMsgIDRe = ^mh:[0-9a-f]{8}#[1-9][0-9]*$`（`inbox_ingress_ingest.go:68`）
  两头锚死，`eventAssertsDistinctMessage`（`:73`）为真时放弃入口内容窗口（`:155`）与落库钩子2.5
  的三条内容查找（`inbox_ingress_persist.go:278`）；**钩子2 的 `msg_id` 精确判等与 DB 唯一索引照旧**，
  所以同一帧重发仍只有一行。正则为什么不能放宽，写在 `:64-67` 的注释里，并由格 K10/K11 各打一头。
- 腿（Go 6 条）：`TestIngress_OccurrenceSuffixSkipsContentWindowDedup`、
  `TestIngress_PlainContentHashStillContentDeduped`、
  `TestIngress_NonOccurrenceHashLikeIDSuffixStillContentDeduped`、
  `TestPersistBridgeHistory_OccurrenceSuffixPersistsSecondRow`、
  `TestPersistBridgeHistory_OccurrenceSuffixStillExactIdempotent`、
  `TestPersistBridgeHistory_ForeignIDStillCaughtByContentHash`。
  腿（扩展 4 条）：`test/adapter-r23-occurrence-identity.test.js`（增量两条、巡检批量两条、三条编号按
  DOM 序、以及**反向半边**"重渲染换掉全部节点后重扫，一条都不许多发"——编号若不稳定，幂等就退化成双发）。
- 牙：格 **K8**（入口只摘豁免那半句）、**K9**（落库只摘）、**K10**（判据恒真 ⇒ 内容嗅探对整个入站流失效）、
  **K11**（只看 `#数字` 结尾 ⇒ 任何以它收尾的外来 event_id 都能蹭豁免）。K8/K9 是"同一规则两个消费点"，
  K10/K11 是"判据本体的两头"——少了任一格，四腿里就有一条是没人守的断言。

### 六、§8.3-19：不做 outcome 枚举，理由要能被证伪

同行口径是"幂等层命中要回放首次结果、是否重复由结论而非文案决定"。批15 已把
`IsDuplicateReason` 收成**结论短语前缀闭集**（`channelgw/protocol.go:357`），并留了一条把两种方向
同时钉住的用例：`channelgw/dup_outcome_b17_test.go` 的 `wantFalse` 里逐字放着当年真在生产路径上出现的
`batch handle error: 持久化消息失败: duplicate key value violates unique constraint ...`
（PG 冲突原文天然含 "duplicate"），`wantTrue` 里放着四条真结论短语。**"改成正解仍是 outcome 枚举"这一句本轮判为不做**，
理由不是"半径大"而是三条可核事实：① 误判方向（把"没存进去"说成"存过了"）已由前缀闭集 + 该用例关死；
② §三 落地后 `duplicate` 结论对应的库里确有行，回放首次结果能省的东西已经不多；
③ 枚举要动 `InboxIngressResult` 及其全部产出点，换来的仍是"一个字符串字段换另一个字符串字段"，
而真正的牙（"失败文案不得判重"）已经在 `dup_outcome_b17_test.go` 上。
证伪方式写清楚：一旦出现"reason 里没有前缀白名单却需要判重"或反之的真实现场（即该用例的两张名单
需要同时改），就是恢复本条的时机——把它留在矩阵里而不是记忆里。

### 七、§6-4：纯 SSE 形态也必须排 `_pendingAck`，且排水只允许一份实现

缺陷形状：`claimDuePendingAck` 只被 `pollDownlink` 调用，而生产默认形态下轮询定时器**根本不启动**
（客户端探到 `sse_enabled` 即 return；服务端 `FF_ENABLE_SSE_BRIDGE` 默认 true）⇒ ack 失败入队的条目
在默认形态下永不重试，页面一刷新内存队列即清空。§6-4 当年把闭环挪回服务端（30s 可见性重推 +
命中 SentCache 补确认）作为替代，并诚实写明"没做的是挂到 SSE 回调上"。

- 落点：`downlink.js` 抽出**唯一的** `drainPendingAcks(channel, cred)`（`:219`，含退避、`attempts++`、
  "与正常下发同接口"、B2 会话归属分叉），轮询侧改为调用它（`:317`），SSE 侧新挂
  `setInterval(ackDrainTimer)`（`:1003`，间隔取 `handlers.pendingAckDrainIntervalMs` 否则
  `BRIDGE_THREE_CHANNEL.pendingAckDrainIntervalMs = 5000`，`constants.js:257`），`stop()` 里
  `clearInterval`（`:1022`）——不留悬空定时器。
- 腿（4 条 it，`test/downlink-r23-sse-ack-drain.test.js`）：到期条目在无人再推消息时被重发、
  `stop()` 之后排水器不再触发、**排水实现只有一份且 ack 请求带会话归属**、
  `pollDownlink` 仍走同一个排水函数（后两条是防"重构把轮询那条腿弄丢"和防"两处各写一份会漂移"）。
- 牙：格 **S1**（SSE 定时器空转不排队列）、**S2**（`stop` 不收定时器）、**S3**（轮询侧不再走共享函数）、
  **S4**（重试 ack 丢掉会话归属）。四格各红一条不同的 it ⇒ "四格 ≠ 一条腿的四次投影"。
- 反向验的是**电池自己**：把任一 it 改名后重跑，控制组立刻停机（"跑出来的用例名与期望名单不一致"），
  而不是"少跑一条还报全杀"——读数在 §十二 表末两行。

### 八、§7.28 八-3：裁剪界的来源进库（`cutoff_source`）

§7.28 登记的独立小项，本轮按同一张表补列：`browser_audit_prune_runs` 加 `cutoff_source`
（`model/audit_digest.go:83`，`VARCHAR(64) NOT NULL`；为什么只有 `cutoff` 不够，写在 `:78-82` 的注释里——
改小保留期与一次正常裁剪在库里长得一模一样）。

- 落点：仓储侧**空来源即拒绝裁剪**（`repository/command_log.go:33` sentinel
  `ErrPruneCutoffSourceRequired`、`:157` 早返、`:241` 写入）；服务侧来源字符串按列宽字节截断
  （`service/retention.go:25-28/48-53`），并每轮扫描现算 env（不缓存启动值），使
  `BROWSER_AUDIT_RETENTION_DAYS` 改动在库里可见。
- 腿（7 条）：`TestR23PruneRunRecordsCutoffSource`、`TestR23PruneRefusesEmptyCutoffSource`（repository，
  真影子库）＋ `TestR23RetentionCutoffSourceShape`、`TestR23RetentionReadsEnvPerSweep`、
  `TestR23PruneOncePassesSourceToRepo`、`TestR23PruneOnceDisabledByZero`、
  `TestR23RetentionBootGate`（service，无库）。
- 牙：`scripts/mut_retention_a6.py` 在同一批用例上扩到 **V1–V6 六格**（原 16 格 D 族一字未动），
  覆盖"空来源不拒 / 留痕行写空串 / 传死值不现算 / 不看禁用开关 / 列宽放开 / 启动门不看装配齐"。

### 九、把内容窗口 TTL 做成可注入字段："窗口有界"从口径变成断言

§6-1 的第三个子句是"窗口必须**有界**"，但 `InboxContentDedupTTL` 一直是常量 ⇒ "到期后不再拦"这条断言
要睡满 5 分钟，于是今天没有任何腿跨过这个界（电池 docstring 里自己登记过这条缺口，本轮补上而不是忘掉）。

- 落点：`inbox_ingress.go:137-148` 加字段 `contentDedupTTL` + 取値函数 `contentDedupWindow()`
  （零值回包级默认），`:158` 的 `SetNX` 改用它。**做成字段只为了可断言**，生产路径不注入＝行为与改之前逐字节相同。
- 腿：`TestIngress_ContentDedupWindowExpires`——注入 2s，先断言窗口内第二条**仍被拦**（下界，
  夹具失效即停机，否则"到期放行"那条等于没测），跨过 `window + 400ms` 后断言必须放行（上界）。
- 牙：格 **T1**（`contentDedupWindow()` 无视注入值、恒返回常量）。
- 诚实边界两条：① "注 0 ⇒ 永不过期"这种真 Redis 上会犯的错，在这份代码里**构造上不可达**
  （`> 0` 门决定注 0 取默认值），所以不给它造格子——那不是没测；② `releaseInboundDedup` 的
  **失败半边**仍无格子（测试用 MemoryCache，`Delete` 永不报错），这条继续写在电池 docstring 的
  "已知未覆盖"里，而不是被 R1–R3 顺带宣称覆盖。

### 十、新口径：`--shared` 克隆面 ≠ 工作树面（本轮踩到并写进常驻脚本）

变异电池在私有 `--shared` 克隆里注码，而克隆只带**已提交内容 + 本泳道显式覆盖的未跟踪文件**。
本轮实测到的后果：外域未跟踪文件（并行泳道的半成品，如 `internal/model/bill.go`、
`repository/help_center_public_list_test.go`）**不进克隆** ⇒ 同一族用例在"工作树整包"与"克隆电池"里
可以一个是红的、一个跑不到；克隆 build 绿也不证明工作树绿。所以：

- 常驻脚本在每趟开头打印被排除的外域文件名单（见 `battery-15cells-release-comp.log:3`），
  让"这格跑在哪份字节"成为读数的一部分，而不是靠人记。
- 台账的证据路径因此**必须落在仓内**（`docs/superpowers/specs/ledger/logs/...`），闭合门
  `check-review-closeout.py` 拒绝任何指向 `/tmp` 的证据字段（`EPHEMERAL_PREFIXES`）。

### 十一、越界改动清单（本轮动了别的泳道/别的域的文件）

| 文件 | 本轮改了什么 | 归属判断依据 |
|---|---|---|
| `user-server/internal/service/inbox_ingress_ingest.go` | 键尾会话维、`eventAssertsDistinctMessage` 豁免、`claimedKey`/`DedupKey`、`SetNX` 改用 `contentDedupWindow()` | 开工前 `git status` 即为 `M`（§8.3-9 的 BLOCKED 理由）；改动全在 `:28/:64-75/:154-163/:174` 这几处锚点内 |
| `user-server/internal/service/inbox_ingress.go` | 拦截分支改为"非回声即落库"、两处 `releaseInboundDedup`、`contentDedupTTL` 字段与取值函数 | 同上；`:41/:48` 常量一字未动 |
| `user-server/internal/service/inbox_ingress_persist.go` | 钩子2.5 三条内容查找传 `echoSince` + 会话、加豁免条件 | 同上（`diff` 只 +12/-6 行） |
| `user-server/internal/repository/message_hub_inbox.go` | 三方法加 `since`/`conversationID` 参数与 WHERE | 同上 |
| `user-web/bridge/src/core/channel-adapter.js` | 发生次数编号四函数 + 四消费点接线 | 本泳道文件（批20d 起持续在改） |
| `user-web/bridge/src/core/downlink.js` | 抽 `drainPendingAcks`、SSE 挂定时器、`stop()` 回收 | 同上；注码后逐字节还原 md5 `e8b3cb216cd12c0068949368e7662f35` |
| `user-server/internal/browser_automation/{model/audit_digest.go,repository/command_log.go,service/retention.go}` | `cutoff_source` 列 + 空来源拒绝 + 现算来源 | 批22（A6）本泳道文件，本轮在同一表上续刀 |

### 十二、证据（全部跑出来的）

| 层 | 跑法 | 读数 |
|---|---|---|
| 入口去重族变异电池（15 格，T1 之前） | `python3 scripts/mut_ingest_dedup_r23.py`（私有 `--shared` 克隆 + 私有 GOCACHE，一次命令跑完 15 趟） | `注码前置：15 格锚点各命中一次 + 注码后语法可解析`；控制组 `rc=0 total=21 passed=21 skip=0 FAIL=[]`；**15/15 逐格被杀、无存活**，`DEDUP_RC=0`，末行 `已全量还原（md5 一致）`。日志 `ledger/logs/R23dedup/battery-15cells-release-comp.log` |
| ↳ 同族前趟（12 格，含 §8.3-17 的 K8–K11） | 同一脚本在 occurrence 四格刚加完时 | 控制组 `rc=0 total=18 skip=0`、12/12 被杀、`DEDUP_RC=0`；日志 `.../battery-12cells-occurrence.log`。**"total 从 12→18→21 摆动"是放刀前现测的结果**，控制组不写死常量（共享树下别的泳道随时往同一批包里加用例） |
| ↳ 常驻副本首轮（九格） | 同一脚本首轮重建时的誊本 | `battery-full-8cells-resident.log`；更早那趟 /tmp 一次性脚本的九份逐格日志（`K1..K7/L1/CONTROL`）留在同目录，锚点与期望红名据以重建 |
| ↳ 18 格（含 T1 与批次回声侧 K12/K13）全量 | `python3 scripts/mut_ingest_dedup_r23.py`（私有 `--shared` 克隆 + 私有 GOCACHE，一次命令跑完 18 趟） | `注码前置：18 格锚点各命中一次 + 注码后语法可解析`；控制组 `rc=0 total=23 passed=23 skip=0 FAIL=[]`（放刀前现测，不写死常量）；**18/18 逐格被杀、无存活**，`rc=0`，末行 `已全量还原（md5 一致）`。日志 `ledger/logs/R23dedup/battery-18cells-run2-batch-echo-leg-in-clone.log`。**同一格号的上一趟（`battery-18cells-echo-half.log`）把 K13 判成「存活=洞」，红因却在克隆面**：新腿文件未跟踪 ⇒ 不进 `--shared` 克隆、控制组 `total=22`，那条腿根本没跑。"存活"有两种成因（判据真无牙 / 跑的不是那棵树），本轮把后者列进 `OWN_UNTRACKED` 后重跑，才由"没跑"变"真杀" |
| 保留期来源电池（22 格） | `python3 scripts/mut_retention_a6.py` | 控制组 `rc=0 total=23 passed=23 skip=0 FAIL=[]`；**D 族 16 格 + V 族 6 格共 22 格逐格被杀**，`FULL_RC=0`、还原 md5 一致；同族提示两组（`D3≈D4`、`D16≈D17`）照旧登记。日志 `ledger/logs/R23retention/battery-full-22cells.log` |
| SSE 排水电池（4 格，vitest） | `python3 scripts/mut_sse_ack_r23.py`（常驻副本；vitest 用不了 `--shared` 克隆 ⇒ 就地注码 + `cp` 备份 + 还原比 md5） | 控制组 `rc=0 settled=4 skip=0 红=—`；S1–S4 **逐刀被杀**，`SSE_RC=0`；`downlink.js` 逐字节还原 md5 `e8b3cb216cd12c0068949368e7662f35`（与首轮同值 ⇒ 两轮跑的是同一份字节）。日志 `ledger/logs/R23sseack/`（`CONTROL.log` + `S1..S4.log`），首轮誊本 `battery-final-run.txt` 与其一次性刀具留在 `R23sseack/first-run/` |
| ↳ 常驻副本的反向半边（控制组名单） | 把任一 it 改名后原样重跑 → 还原后 md5 回 `b5e2d6b06cde80826b8063a2b9e92d1a` | 立刻停机（`REV_RC=2`）并打印"控制组跑出来的用例名与期望名单不一致"，实际名单里那条带 `（改名）` ⇒ 名字身份是真判据不是装饰；**本轮刻意不写死 settled 的条数**（写死＝把别的泳道往同一 test 文件里加的 it 算成跑断）。日志 `.../reverse-name-identity-resident.log` |
| 扩展两条新用例文件 | `cd user-web/bridge && npx vitest run test/adapter-r23-occurrence-identity.test.js test/downlink-r23-sse-ack-drain.test.js` | `VITEST_RC=0`、`Test Files 2 passed (2)`、`Tests 8 passed (8)`（19ms / 627ms） |
| 静态 | `gofmt -l internal/service/`、`go vet ./internal/service/`、`python3 scripts/mut_ingest_dedup_r23.py --check` | `gofmt` 无输出；`vet rc=0`；`--check` 18/18 格锚点各命中一次且注码后语法可解析（`CHECK_RC=0`） |

### 十三、本轮没跑的 / 仍然开着的

原先挂在"没跑"里的第一条（入口去重族 16 格全量 + §九 那条新腿的绿）**已跑完**：读数上移进
§十二 那张表的那一行，本节因此少一条——台账的计数钉由 `scripts/ledger_r22.py` 现测，
不在这里手抄。

- §八 那 7 条腿与 22 格电池**已跑过**（读数见上表），但 `internal/service` 整包与
  `./internal/...` 全量门禁要等本轮合并门禁那趟（协议 §4 E 相）统一报，本泳道不拿子集当门。
- 设备腿照旧：`#28` 那条伪造平台域名的桥接真机腿仍属用户侧（要用户的宿主与设备），
  本轮不把它记成"已验"。
- §八/§九 之外没有新登记的未做项：本轮把"本批不做"清单里能做的都做掉了，
  剩下两条**构造性**做不到的（`releaseInboundDedup` 失败半边、归一化那条 SQL 的专属格子）
  写在电池 docstring 的"已知未覆盖"里并各指了替代腿，属于"下一刀的候选"，不是"这轮没跑"。

## 7.30 批24（+批24b）：B 链路出站回 DOM 复核（§8.3-8 落码）与"会话归属"六处同口径

**这一批的入口是 §8.3-8 那句"暂缓"。** 它挂了几轮，给的理由是"需每个平台各写一条『我的气泡』判据，半径在 5 个适配器"。
本轮按二次审核协议（`2026-09-22-second-review-protocol-design.md` §4 D–E）把它拉回重判，第一件事是去**量**那个半径，
结论在 §六——半径根本不在 5 个适配器，判据住的是共享层；那句"暂缓"从头到尾是一条没被量过的估计。

### 一、判据是"计数增加"，不是"我这条出现了"

`_countVisibleText(text)`（`channel-adapter.js:285-302`）在**活动消息容器内**数归一化后与待发文案相等的可见气泡；
`_verifySendLanded`（`:306-314`）拿它和**点击之前**的基线比，`> baseline` 才判落。

不取"文本命中"：同一句话在聊天窗口里有两层含义——客户先说过"好的"、我方随后也发"好的"，命中判据会把一次彻底
失败的发送报成已送达，**那比不判更糟**（它把结论位变成假阳，而结论位的意义正是"这条能不能被当作已交付"）。
基线取点同理：`rawSendText` 之前还有风控 `waitHintMs` 与全局最小间隔两段可睡到秒级的等待（`:1238-1247`），
基线若取在方法开头，睡眠期间客户自己发的那条会被算成我们发出去的。这两条各有一格：V1（判据退化成文本命中）、
V11（基线挪到点击之后），V11 的红因是一条时间戳序断言 `expected 2804 to be less than 2700` 而非文案比较。

### 二、两条红线：结论位永不参与 ack；回查永不写状态

`sendTextWithThrottle` 成功分支（`:1263-1279`）里回查只在 `ok===true` 之后跑，产出的 `sendVerified` **不回写** `ok`、
不参与 `markSent`、不参与 ack——`markSent()` 与回声帧照旧无条件走。理由与批20b 在 A 链路的取舍同一条：
双发是本链路唯一不可撤销的那一侧，把"没见着"折成重投等于把一次不可撤销的双发交回给一段渲染滞后。
所以 `false` 只读作"没见着"，永远读不出"没发出去"（这句写在 `:305` 的方法注释里，格 V4 专门守这条：
一旦让回查未见参与 ack，红的正是"平台静默吞→照常 ack 语义不变"那三条腿）。

第二条红线：`_countVisibleText` 是纯只读——不碰 `_sentKeys`、不推 occurrence、不进 `_convWindow`
（注释 `:281-284`）。回查若顺手"认领"了这些气泡，就把本该上行的消息吃掉了，与 §8.3-17 的发生次数身份直接冲突。
这一条由 `adapter-b24-send-verify.test.js` 的"红线②：回查是只读"腿守着，V11 注码时该腿与另两条一起红。

### 三、三态：没走到回查的出路不带结论位

`sendVerified` 是 `true | false | undefined` 三态，不是布尔。发送抛错、风控拦截、目标会话打不开这三条出路
在回查之前就返回，返回体里**没有**这个字段（`:1242`、`:1261`），只有走到回查那一步才盖章。
落帧与日志一律原样透传，不许 `!!` 洗成二态：`downlink.js:407-409` 写的是 `sendVerified: result && result.sendVerified`
——`result` 为假时整个表达式是 `undefined` 而不是 `false`，这正是三态。静态锁那一行由格 V10 守（把它改成
`!!result?.sendVerified` 立刻红），三态本身由格 V5 守（把没走到回查的出路折成 `false` 立刻红）。

### 四、预算：回查切片加在 sendText 预算之上，不是从里面切

`constants.js:261-262` 是 `sendVerifyMs: 2500` / `sendVerifyPollMs: 250`；`:277-279` 那把写步超时是
`humanSendTimeoutMs(text, baseMs) + BRIDGE_THREE_CHANNEL.sendVerifyMs`，注释写明了为什么可以加：
回查自身有界，所以加法不会无界放大。三条腿各钉一个方向——V6（外层预算丢掉回查切片：慢发送被判成没发）、
V7（回查不轮询，第一次没见着就判未见：一段 150ms 的渲染滞后就能翻结论）、V9（重试站点漂回不含切片的裸预算，
是静态锁，红因是 `not to contain 'humanSendTimeoutMs'`）。

### 五、批24b：把"五条已判、一条没判"收成六条同口径

批24 给回查加 `root.contains` 时顺手读码，数出这个类里一共**六处**在迭代 `getMessageItems()` 的返回值，
而会话归属判据只挂在其中五处：`getMessages`（`:254`）、`_countVisibleText`（`:294`）、`_collectUnseenText`（`:964`）
当时有，`_occurrenceInList`（`:177`）、`_handleIncremental`（`:1050`）、`_backfill`（`:1085`）没有。
三条缺的里前两条是真缺陷：增量路径每 3s 走一次，会把上一会话的残留私信按**当前**会话 id 上行；
`_occurrenceInList` 更隐蔽——残留节点排在真实节点前面时，真实那条的发生次数被 +1，规范键从 `mh:<hash>` 漂成
`mh:<hash>#1`，等于 §8.3-17 刚立的编号口径被同文件另一处判据不一致地打掉。

补齐时两处细节各自成立才收：`_handleIncremental` **不** `seenNodes.add`（`:1046-1050` 注释）——这个节点将来若真的
回到活动容器仍应可上报，顺手标记等于把一条真消息永久判掉；`_backfill` 则**要** `add`（`:1085` 与 `:964` 同语义），
因为它按整屏扫、不标记就会每轮重复并进历史帧。两条相反的取舍都来自同一句判据，差别只在调用方是否重复扫同一片节点。
另外 `getMessageRoot()` 传非 Node（如早期夹具里的 `{}`）会让 `start()` 里的 `MutationObserver.observe` 抛错被 catch
吞成一行日志——测试于是跑在"半启动/从未启动"的适配器上还能全绿，所以新文件的每条腿都先 `expect(adapter.start(cb)).toBe(true)`。

### 六、认知修正：那条"暂缓"的理由不成立，但代价是真的

写"半径在 5 个适配器"时没数过。实测：`src/channels/{douyin,kuaishou,tiktok,xhs,xianyu}.js` 五个渠道文件
全部是 `return new BaseAdapter({...})`（`douyin.js:678`、`kuaishou.js:355`、`tiktok.js:666`、`xhs.js:649`、
`xianyu.js:717`），没有一个 per-platform 子类，也没有一处渠道自己的气泡判据代码——判据只能住在共享层，
写一遍就是五家生效。所以"每平台各写一条"这句是假的。

代价是真的，但不在写码半径而在**判据强度**：`getMessageItems()`/`parseMessageItem()` 是各家 hook 给的，
共享层只能看到"归一化后的文本相等"，看不到"这条是不是我方发的"。于是本批复核的是"窗口里这条文本多了一条"，
不是"我这一条出现了"——同文本并发改动（客户和我方同一毫秒各发一句同样的话）在它眼里是一次。这一点写进
`channel-adapter.js:304-305` 而不是留给读者猜，也是取舍从"暂缓"变成"采纳"时唯一被保留下来的那半边担心。

### 七、消费端现状：结论位落了库，服务端没有一处读它

桥侧把 `send_verified` 放进 AGENT 回声帧的 `extra`，服务端 `event.Extra` 整包拷进 `hub.Extra`
（`inbox_ingress_persist.go:85-90`），所以它**会**进库、在导出面可见。但本轮不声称"已被消费"：
`send_verified` / `sendVerified` / `SendVerified` 在 `user-server/` 全树 **0 命中**（ripgrep 实测，
第一次用 `grep --include=*.go` 报出的 0 是 zsh 把 `--include=*.go` 当 glob 吃掉的假零，不作证据）。
下一刀若要它有用，落点是把结论位接进 A2 的 `write_confirm` judge 帧（§7.24），那是另一个决定。

### 八、两份旧夹具的真相：绿灯跑在从未启动的适配器上

判据一上，`adapter-history.test.js`（7 条）与 `dedup-list-noise.test.js`（2 条）当场红。逐条读红因后判归**夹具**：
它们的 `getMessageRoot` 返回 `{}`、消息节点是裸对象且从未 `appendChild` 进容器——`root.contains` 要么不是函数、
要么恒假。这不是判据过严，是这两份文件一直在"半启动"状态上跑绿（`start()` 抛错被吞），
`dedup-list-noise` 更是三条断言全绿却从未证明过它测的那个入口真的被接线过。修法方向选定为**改夹具不改判据**：
两份都换成真 jsdom 节点 + 真容器，`getConversationId` 从对象字面量的 getter（解构时会 eager 取值）改成
`() => state.convId` 惰性读，并各自补上 `expect(start()).toBe(true)`。判据正确性由下面第九节的 V12–V16 反证。

### 九、证据（全部跑出来的）

| 层 | 跑法 | 读数 |
|---|---|---|
| 静态前置 | `python3 scripts/mut_send_verify_b24.py --check` | `锚点前置：16 格各命中一次`，`rc=0` |
| 单格反向（改完驱动先跑一格） | `... --cells V16` | 控制组 `rc=0 settled=24 skip=0 红=—`；V16 **杀掉**，红因 `expected [ '切换后残留' ] to deeply equal []`（第一版红因是 `TypeError: Cannot read properties of undefined (reading 'text')`——断言取错了返回形状：`_collectUnseenText` 推的是 `parsed` 本体而非 `{parsed}` 包装，见 `:980`）。脚本自印"本次按 --cells 只跑了窄口子，门禁不认这一行" |
| 全 16 格 | `python3 scripts/mut_send_verify_b24.py`（vitest 面用不了 `--shared` 克隆 ⇒ 就地 `cp` 备份注码 + 逐格还原比 md5） | 控制组 `rc=0 settled=24 skip=0 红=—`；V1–V16 **逐刀被杀、无存活**，`BATTERY_RC=0`，末行四文件逐字节还原 `channel-adapter.js=6af6de63 constants.js=f17a0e34 downlink.js=712a0b00 types.js=d0b00bfd`。V12–V16 的红集合**精确等于各自绑定的那一条腿**（一处一格，不出现"摘一处五条全红"的整块形状） |
| 归属族 5 腿 | `npx vitest run test/adapter-b24b-conv-ownership.test.js --reporter=verbose` | `Test Files 1 passed (1)`、`Tests 5 passed (5)`（1.11s） |
| 桥侧全量 | `cd user-web/bridge && npx vitest run` | `VITEST_RC=0`、`Test Files 58 passed (58)`、`Tests 762 passed \| 7 skipped (769)`，`FAIL`/`Unhandled` 行数 0。计数较本批早先那趟 767 涨 2 **已归因**：b24b 文件从 3 腿长到 5 腿（`git show HEAD` 与工作树逐文件 `it(` 数对账，其余 7 个改动文件计数一字未动） |
| 日志 | `docs/superpowers/specs/ledger/logs/B24sendverify/` | 本轮 17 份逐格日志另存 `run2-16cells/`（18 文件，含 `battery-full-16cells.txt` 驱动整段读数，未过 `tail`）；首趟 11 格誊本在 `run1-11cells/`（12 文件），复跑不覆盖前轮 |

### 十、本批不做的

- 结论位进 `write_confirm`（§八 那段的落点）——需要先定"未见气泡"在 A2 三态轴里算 accepted 还是 confirmed，
  那是命令日志域的口径，不属本泳道随手改；本批只保证它**已经在库里**，不保证有人读。
- 设备腿照旧：真机上"平台静默吞"这条反例是否真的存在（以及 `sendVerifyMs=2500` 在慢机型上够不够）只能由
  `#28` 那条腿答，属用户侧。
- §8.3 row 8 的"同文本并发改动判成一次"这一已知代价**不修**：修它要 per-platform 的"我的气泡"选择器，
  而那正是 §六 证明本批不需要、下一批才需要的东西。

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

> 标题里的"仍然成立"是**批14 当时**的状态。三条现已全部落地（1→批17 + 批20c §7.11/§7.25；
> 2→批20 §7.21；3→批20b §7.24），各条末尾的「状态（批NN 后）」块记的就是与本文原口径的差集。
> 本节文字一律保留不删：差集本身是这一节的产出，抹掉它就看不出当初设计错了哪一处。

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
   > 状态（批17 后）：**两条都已落地，实证见 §7.11**。三处与本文原口径不同，按实测改：
   > ① `stable` 的节拍不能照抄"rAF ~16ms 轮询"——取证 tab 是隐藏页，隐藏页**不出 rAF 帧**，
   > 只等 rAF 会在隐藏页里永挂到注入超时；真机在跑的是 `setTimeout(fin,50)` 那条兜底分支
   > （ALIVE-3 把可见性读成证据）。② 复核必须跑在 CDP 的 `try` **之外**，否则"复核不过"会被
   > catch 误读成"CDP 不可用"→ DOM 兜底**再点一次**，正好把本条要修的假绿换成双发。
   > ③ 中心点判据在 `navigated` 时必须跳过（页面跳走≠元素挪位）。
   > 另记一条观测边界（原口径没写）：**同位置被换成同 selector 的另一个节点**无从分辨，
   > 除非页内埋身份令牌——本批如实留在注释与 §7.11 里，不声称覆盖。
   > 状态（批18 后）：**本条的 (a)(b) 只落在 `click`/`click_near` 上，`comment_send` 是同一形状却未被覆盖**
   > ——它同样是「注入算按钮坐标 → `cdpInput.clickAt(tabId, btn.x, btn.y)`」（`primitives.js:963`，与
   > `:823`/`:864` 同一族），所以「探测 → 真点之间页面挪动」的风险对它**同样成立**，但它既没有 `stable`
   > 门也没有点后身份复核。本批仍不扩半径，理由是它**有第二条通道兜住假绿**：Go 侧 `finalizeComment`
   > 的只读回查轮询（§7.7/批15）——点错元素的结果是"评论区查不到这条 ⇒ 步判红 + 台账
   > `sent`/`unattributed`"，而不是报绿。登记为**开放项**：给 `comment_send` 补 (a)(b) 的收益是"更早、
   > 更便宜地判红"，代价是一次额外页内注入。对照之下 `type + submit_on_enter` 既没有坐标点击也没有
   > 回查通路，它的"绿"只剩 `pressEnter` 肯不肯抛错一个信号——那正是批18 修掉的 `.catch(() => {})`，
   > 见 §7.12。（本条第一版曾把 `comment_send` 写成"走页面侧注入而非 clickAt"，读码 `:963` 后作废。）
   > 状态（批20c 后）：**上面那句登记作废——`comment_send` 的 (a)(b) 已落地，实证 §7.25**。
   > 本条原文说的"同一形状却未被覆盖"从此只对历史成立：第三份 probe（`injPostCommentSend`）补齐了
   > `settleBox` 双帧结算 + 落位后重判 + 点后身份复核，并回传 `selector`（复核对象）与
   > `jitter_radius`（批19 那一格的同族缺口）。判红由"回查查不到"（事后、跨进程）提前为"提交点不结算/接管"
   > （事前、一次注入内），兜底通道不再承担本条的证伪责任。
   > **半径记账要如实**：批18 说的"它还有第二条通道兜住假绿"当时是成立的，本条不是因为那条不成立才修的。
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
   > 状态（批20 后）：**已落地，实证 §7.21，形状与本文"最小形态"有两处不同，按实测改**：
   > ① 一次性消费没有走 `UPDATE approvals SET used_at=now() …` 那条 SQL——本仓**不存在 approvals 表**，
   > 放行是进程内 `confirmGate` 的 channel，等价的"只 redeem 一次"由**先摘后关**实现（第二次放行落
   > `VerdictNoGate`，不会 double close）。② 载荷哈希住在闸门与审计帧里（`payload_hash`，
   > `SignalConfirm` 三态 `granted`/`no_gate`/`payload_mismatch`），不是一列 `approved_payload_hash`。
   > 本文承诺的 fail-closed 与"留痕"都成立：不匹配即拒、闸门仍在、零帧下发，且每次出路都落 `d7_confirm`。
3. **`ok bool` 一列混装"跑了"和"成了"**（P1）。同行没有一家用单布尔：GitHub Checks `status` × `conclusion`
   两列、K8s `phase` × `conditions[]`、Stripe PI 七态（`processing` ≠ `succeeded`）、
   WhatsApp/Twilio `sent/accepted/delivered/read/failed`、OTel recording-errors 明令
   **"无错时 status MUST 留 Unset"**（OK 要调用方显式写）。我们 `browser_command_log` 只有 `ok bool`
   （`model/command_log.go:22`），而 §7.x 一路在防的"读页面自证成功"假绿，本质就是把 Unset 当成功。
   → 最小形态：命令日志加 `attempted/accepted/confirmed` 三态，`confirmed` 只能由**第二条通道**置位
   （换 selector / 新会话回读并匹配作者 + 内容哈希），扩展上报只允许写 `accepted`；
   回读前先比 `PageFingerprint(url, element_count, text_hash)` 式指纹，**指纹未变即判 `unverified`**。
   > 状态（批20b 后）：**已落地，实证 §7.24，两处与本"最小形态"不同，且第二处是有意不做**：
   > ① 三态不是新加三列，而是把已有的 `direction` 轴当轴（command=attempted / event=accepted /
   > judge=confirmed），`ok` 由二态常量改 `*bool`（NULL=此帧无结论），存量下发帧的假 `✓` 由 v3.44.0
   > 分批置 NULL；`confirmed` 由服务端回查这一**第二条通道**落 `write_confirm` judge 帧，扩展回执只到
   > `accepted`——这一条与本文完全一致。② **`PageFingerprint` 那一步没做，全仓 0 命中**：`confirmed`
   > 帧的收紧判据用的是已有的 `state ∈ {verified, unattributed}`（作者 + 内容哈希匹配），
   > 未引入"页面指纹未变即 unverified"这条新信号。代价如实记在账上：一次「评论确实发出去了、但页面
   > 指纹恰好没变」的形态分辨不出——现判据不依赖指纹，所以这一格不构成漏判；而它换来的是零 DDL、
   > 零端点改动。指纹方案留给真出现"回查判不出"的现场再议，不在没有见证时先建一道锁。

### 8.3 差距矩阵与取舍（21 行，每条左列都经本泳道读码复核，未复核的一律不进；#11 为「被子 agent 证伪故不进矩阵」的反例记录；#20、#21 为批16b/批16c 自审新增、左列不是同行口径而是本仓缺陷形状）

`采纳`=本批改；`拒绝`/`不采纳`=给出理由并留档；`不进矩阵`=同行口径里那条对本仓的断言被证伪，不记账（#11）。
历史上还有 `BLOCKED`（落点在并行会话在途文件）与 `暂缓`（"留作下一批候选"）两档，批23 与批24 之后矩阵里
**已无使用者**——按二次审核协议的口径，"缓"与"阻塞"都不是终态，只剩"落码"与"能被证伪的不做理由"两种。

| # | 同行口径（证据） | 本仓实测 | 取舍 |
|---|---|---|---|
| 1 | 派发前必须在**同一动作事务内**重算可点性，`stable` = 连续两帧 boundingBox 一致（Playwright `_retryPointerAction`: `scrollIntoViewIfNeeded`→`checkElementStates(['visible','enabled','stable'])`→`_clickablePoint`→`_checkFrameIsHitTarget`；Skyvern `classify_element_state` 的 `occluded` 用 `elementFromPoint(center)` 且 `top!==el && !el.contains(top)`） | `actionabilityCheck`（`primitives.js:12-26`）已有 visible / 零盒 / disabled / **遮挡**四项，**只缺 `stable`**（23-24 行注释自陈理由）。更关键：`injClick('probe')` 返回 `{x,y}` 后，真点击走 `cdpInput.clickAt(tabId, probe.x, probe.y)`（`:646-647`），**中间隔着拟人贝塞尔飞行时间**，这期间页面挪动即点到从未探测过的元素，返回值仍是 `{ok:true, channel:'cdp'}` | **采纳（P1）** A1（批14 落「闸门不被兜底绕过」+「兜底单发」并真机立证 `channel=cdp`；**批17 落 `stable` 三份内联 + 写步点后身份复核，实证 §7.11**；**批20c 把同一对闸门接到第三条 trusted 写通道 `comment_send`，"三份内联副本"至此齐备，实证 §7.25**） |
| 2 | "跑了"与"成了"永不共列：GitHub Checks `status`×`conclusion`、K8s `phase`×`conditions[]`、Stripe PI `processing`≠`succeeded`、OTel recording-errors "无错时 status MUST 留 Unset" | `browser_command_log` 只有 `Ok bool`（`model/command_log.go:22`） | **采纳（P1）** A2：**批20b 已落（§7.24）**——轴不靠加列，靠 `direction`（command=attempted / event=accepted / judge=confirmed）：`ok` 改三态 `*bool`（NULL=此帧无结论，存量 1888 条下发帧的假 `✓` 由 v3.44.0 分批置 NULL），写步补 `write_confirm` judge 帧（绑 `payload_hash`、带 `state`），前端同批改 `outcomeMark` |
| 3 | 重试必须有上界并落终态：Sidekiq `DEFAULT_MAX_RETRY_ATTEMPTS=25` → `dead` ZSET（`dead_timeout_in_seconds` 6 月、`dead_max_jobs=10000`）、SQS `redrivePolicy.maxReceiveCount` → DLQ、River `discarded` | 出站集合语义本身正确（`FetchOutboundUndelivered` 只取 `pending` 或 `inflight && claimed_at<now()-30s`，`delivered/failed` 自然离开；`handler_http.go:236` 证实 SSE 轮询定时器确走此函数，未注入时按 `:211` 显式 Warn 回退游标）。但**没有任何尝试计数列**，于是"扩展每次都发不出去"（目标会话已不存在等）的行会以 30s 周期**永久重推**，无人升级为终态 | **采纳（P1）** A3：**批20d 已落（§7.26）**——`MaxOutboundPushAttempts=20`，三条认领路径同口径计数，撞顶即终态化 `failed` 并写 `push_error`（不重复写计数）；回收 `inflight` 时**不重置**计数，否则 30s 一跳就永远撞不到顶 |
| 4 | 去重必须有界：SQS `MessageDeduplicationId` 5 分钟窗、Stripe 幂等键 24h、Azure "Retain each record at least as long as the broker can still redeliver" | 桥接扩展 `SentCache` 持久化到 `chrome.storage.local`（`downlink.js:24,48`，**非内存态**），但**只有条数上限 `sentCacheMax:2000`、无时间界**（`constants.js:248`），且 `add()` 命中已有 key 不刷新插入位（Set 语义）→ 淘汰纯按插入序 | **采纳（P2）** A4：加 **24h** TTL，不是 5min（服务端重推窗取决于浏览器离线时长，界必须 ≥ 上游仍能重投的时长，否则反而放大重复风险）。**批20d 已落（§7.26）**：`sentCacheTtlMs = 24h`（`constants.js:253`，`downlink.js` 读写两侧都过 TTL），界取 24h 而非 SQS 的 5min 的理由如上；插入命中已有 key 时刷新时间戳，淘汰从"按插入序"变成"按最后命中序 + 到期" |
| 5 | 审批绑载荷 + 一次性 redeem：GitHub `dismiss_stale_reviews` / "records the state of the diff at the point when a pull request is approved"、Salesforce "Approvers see the values at submission time"、Stripe `confirmation_token.expires_at`、LangChain `HumanInTheLoopMiddleware` 用 `tool_call["id"]` 回填、browser-use `_normalize_action_for_hash`（非 None 参数 + `sort_keys` + `sha256[:12]`）；反例：LangGraph `interrupt` 对 resume 值**完全不校验** | D7 放行不携载荷指纹（待读码定锚点后实现） | **采纳（P2）** A5：**批20 已落（§7.21）**——`confirmGate` 开闸即定死 `payload_hash`，`SignalConfirm` 三态（`granted`/`no_gate`/`payload_mismatch`），不匹配即拒且闸门仍在、零帧下发；"只 redeem 一次"由**先摘后关**达成（本仓无 approvals 表，故不是本文设想的 `UPDATE … WHERE used_at IS NULL` 那条 SQL） |
| 6 | 裁剪不等于证据消失：CloudTrail digest `logFiles[].{hashValue,hashAlgorithm}` + `previousDigestHashValue` + **`logFiles:[]` 空摘要可断言"该时段无事件"**；RFC6962 `inclusionProof{treeSize,rootHash,hashes}`；pg_partman `retention_keep_table=true`（detach 不 drop） | `PruneBefore` 整行删除（`command_log.go:45-60`），90 天界由 `BROWSER_AUDIT_RETENTION_DAYS` 定（`retention.go:20-27`）；删后**无任何自证手段** | **采纳（P2）** A6：删除前把 `(seq, row_hash, prev_row_hash)` 沉进永不裁剪的小表。**批22 已落（§7.28）**：摘要不是逐行 `(seq,row_hash,prev_row_hash)` 链（那是 RFC6962 的形状，本仓要证的是"裁掉的那批存在过"），而是**批级** `batch_digest`（批内各行内容 sha256 按 id 序聚合）+ `chain_hash = sha256(prev ‖ batch_digest)` 落 `browser_audit_digests`（无 `DeletedAt`、永不裁剪），且**与 DELETE 同事务**——摘要写不上就不裁；每次裁剪扫描（含 0 行那次）另落一行 `browser_audit_prune_runs`，计数与算术闭合 |
| 7 | 租约靠心跳续期而非固定 TTL：pg-boss `heartbeatSeconds`/`heartbeatRefreshSeconds=hb/2`、SQS `ChangeMessageVisibility`、Temporal `HeartbeatTimeout`；且"完成写必须与租约同事务，被抢即回滚" | 需扩展每 10s 回写 `claimed_at`，是**双端协议改动**（HTTP 端点 + 扩展定时器 + 权限面），而现 30s 认领 + 命中缓存补确认已收敛，收益只是"少重推几轮"的延迟 | **拒绝**（半径/收益不划算，留档） |
| 8 | 发送前先落本地台账、事后去 DOM 核对自己的气泡（"已发出但 ack 丢失"的同行正解） | `SentCache` 已是持久化台账且 `reAckSentDuplicates` 两条路径都挂（轮询 `downlink.js:358`、SSE `:851`，定义在 `:583`；行号为批24 后实测，此前记的 `:315/:804` 已随批23 抽 `drainPendingAcks` 漂掉），SW 重启场景已兜住；缺的只是"发成功后回 DOM 复核" | **采纳（P1）批24 已落（§7.30）**，且当年的取舍理由被读码否证：**半径不在 5 个适配器**——五个渠道文件全是 `return new BaseAdapter({...})`（`douyin.js:678`、`kuaishou.js:355`、`tiktok.js:666`、`xhs.js:649`、`xianyu.js:717`），零 per-platform 子类、零渠道自有判据，判据只能住共享层、写一遍五家生效。真代价在**判据强度**而不在写码半径：共享层只见"归一化文本相等"，故复核的是"窗口里这条文本多了一条"（计数增加，非"我这条出现"），同文本并发改动会判成一次——这一条写在 `channel-adapter.js:304-305` 而非留给读者猜。另两条取舍：结论位 `sendVerified` 三态、**永不参与 ack/markSent/重推**（双发是唯一不可撤销面，§7.30 二）；回查**纯只读**，不写 `_sentKeys`/`seenNodes`/occurrence。连带把 §7.30 五 数出的"六处迭代同一条判据、当时只挂五处"一并收齐（批24b，格 V12–V16 一处一格）。消费端现状如实登记：`extra.send_verified` 经 `inbox_ingress_persist.go:85-90` 原样进 `hub.Extra` 落库，但 `user-server/` 全树 **0 处读**它（§7.30 七） |
| 9 | 入口去重键必须带会话维度、窗口必须 ≥ 上游重投窗、且与副作用同事务（Stripe/Azure/企微/飞书一致否证现状） | 改前实测：`inbox_ingress_ingest.go` 键内无 `conversation_id`、`InboxContentDedupTTL=5min`；hub 层内容命中无时间界 | **采纳（P1）批23 已落（§7.29 一/二/四/九）**，三个子句分别落在三处，其中第二个子句**换了口径**：① 键尾带会话（格 K1）＋ hub 层三条查找各加时间界与会话界，且"调用方传界 / 仓储侧用界"两层各一刀（K2/K6、K3/K7）；② "窗口 ≥ 上游重投窗"这条约束的正确归属是**出站 SentCache 的 24h**（批20d A4 已按此口径取 24h 而不是 SQS 的 5min），入口这把内容窗口不负责兜"同一帧重投"——那一层由 `msg_id` 精确判等 + DB 唯一索引管，与窗口长度无关；它在入口侧的口径改成"**必须有界**"，并为此把 TTL 做成可注入字段补了跨界腿与格 T1（§7.29 九）；③ "与副作用同事务"在 Redis/PG 之间不存在共同事务，落成了**先占后写、写失败退坑**的补偿（K4/K5/R1–R3） |
| 10 | `officialEventID` 优先级高于内容哈希 | `webhook_event_key.go` 未跟踪（在途），且它含 `event_type` → 同一次推送多事件会算多条；`self/agent` 巡逻回环消息根本无 event_id，仍需内容兜底 | **采纳（P2）批23 已落，但**认知修正保留（§7.29 一）**：`webhook_event_key.go` 已于 2026-09-20 随 `1af6d28c` 入库，"在途"这个 BLOCKED 理由今天不成立；落下来的不是"把官方 id 接到所有面"，而是**优先级次序**——带平台稳定消息 id 的事件整个跳过内容窗口（守卫 + 格 L1），官方 id 走 `msg_id` 精确判等那一层。"正确方向"仍不等于"覆盖主要流量"：`self/agent` 巡逻与桥接回环没有平台 id，内容兜底是必需的最后一层 |
| 11 | agent 报告称 `BridgeOutboxMessage.Extra` 是 `json:"-"`（扩展拿不到 `dm_target`）、称存在 `reAckDeliveredOnCacheHit`/`InboxConversationID`/`ErrOutboundAckScopeMismatch` | 实测：`Extra map[string]any json:"extra,omitempty"`（`channelgw/protocol.go:157`）且 HTTP 侧 `handler_http.go:770` 真的带出 `extra`；后三个符号**全仓 0 命中** | **不进矩阵**：子 agent 对本仓的断言被证伪，本轮第 2 次。教训回灌记忆 |
| 12 | 「闸门所依据的记录本身可以静默失败」是同一类缺陷：K8s `sideEffects` 把 `Unknown` 与 `Some` 同等对待、admission `failurePolicy` 默认 `Fail`；审批/幂等建立在"可能没写进去的行"上等于没有 | `recordSubmitState`（`write_ledger.go:39-48`）写失败**只 Warn、不返回 error**，四个调用点（`executor.go:939/969/982`、`write_ledger.go:207`）拿不到失败事实 ⇒ 「send 已跨越但台账没落」时下一轮 `FindSubmitAttempt` 查空，双发闸门整体消失，而 §3.1 给这张表的定性是"重发闸门的唯一事实来源" | **采纳（P1）** A7：`recordSubmitState` 返回 error，写失败即给该 session 置 `writeLedgerBroken`，其后所有写步**在派发前**拒绝（零帧下发）、错误文案含"台账未落，本会话写能力已降级"。不对称决定取舍：写步失败**可见、可人工重跑**，双发**不可见、不可撤销**。**批16 已落（§7.8）**：另加两条定稿时才想清楚的收口——`sent` 写失败按「越点未落账」记进程内兜底（会话级标志挡不住同进程换 session 的自动重试），且台账没落成的写步**不得再报 success** |
| 13 | 闸门查询失败要 fail-close（同上，`failurePolicy: Fail` 就是这条默认值） | `guardResubmit`（`write_ledger.go:71-74`）`err != nil` 时 `Warnf` + `return nil` **放行**；`:61-62` 注释明写这是刻意的（"DB 抖动不该把整条自动化链路锁死"）⇒ 属**决策复审**而非隐藏 bug：同行口径下"抖动期闸门消失"正是它付不起的那一侧 | **采纳（P1）** A8：写步判定的这条分支改 fail-close，与 A2 的 `send_gate` 早返同形（步判 failed、不记尝试）；只改这一条，`appendCommandLog` 的 Ignore 语义不动（那是增强件不是闸门）。**批16 已落（§7.8）**：查询失败上抛「双发闸查询失败……拒绝下发」，走既有"步判 failed、台账不动"分支 |
| 14 | 审批必须进审计链：GitHub deployment review / CloudTrail 的"谁、何时、对哪份载荷" | `SessionService.Confirm`（`service/session.go:99-108`）只调 `SignalConfirm`，**一帧都不落**；而 `direction=judge` 这一类**早就存在**（`executor.go:527-529` 写 judge 帧、`controller/session.go:79` 已支持 `?direction=judge` 过滤、I5 导出取全量） ⇒ 缺口只是"没人给它写"，不是"没地方写" | **采纳（P2）** A9：放行成功补一帧 `judge`，payload **只放 `text_hash`**（`HashWriteText`）不放正文——I5 导出会把正文带进离线件。**批20 已落（§7.21）**：放行成功/被拒/超时三条出路各落一帧 `d7_confirm`（`decision` + `rejected_attempts`），挂起时先登记后落 `d7_wait`，两帧只带 `payload_hash`/`expires_at`/`step_index` |
| 15 | 挂起态要可跨进程查证：Temporal 的 signal 落 history、任何 worker 都能投递；LangGraph 的 interrupt 进 checkpoint store | D7 挂起是**进程内**裸 map（`executor.go:74-75` `confirmRegistry`）+ `ConfirmPending bool gorm:"-"`（`model/session.go:37` 不落库）⇒ 多副本时 confirm 落到别的副本只会得"没有待确认的提交点"，与"根本没开闸门"同一句文案；进程重启后挂起协程消失，靠 `stale_reconcile.go` 对账收敛（不砖化，但人看不到原因） | **采纳（P2）** A10：挂起时补一帧 `d7_wait{expires_at, step_index, text_hash}`，`SignalConfirm=false` 且库里存在**未到期** `d7_wait` ⇒ 回 `gate_on_another_instance`。**零 DDL**（`browser_sessions` 连 `updated_at` 都没有，加列会牵动 `completed_at` 口径）；`expires_at` 只当"写下的期望"，必须与内存真值 AND 起来用。**批20 已落（§7.21）**：读侧 `GET /sessions/:id/confirm-gate` 受归属校验约束，服务层四态 `granted`/`no_gate`/`payload_mismatch`/`gate_on_another_instance`，判别位放 `data.status` 不放错误码（`response.Error` 会把 409 一律折成 `DUPLICATE_ENTRY_3003`） |
| 16 | 「取不到副作用分类」不等于「无副作用」（承 12 的 K8s `Unknown`） | `isWriteStep`（`write_ledger.go:119-142`）在 `:123` 写作 `locs, _ := platformStepLocators(task)` **把错误丢掉**：平台适配器未注册/取表失败时三条推导全体不命中，只剩 `post_comment` 与显式 `is_write` 声明 ⇒ **retries=0、双发闸、D7 三道同时静默消失**。`:153-154` 注释只论证了"不会误判成写"，没论证漏判的代价 | **采纳（P1）** A11：取表错误升为第三态 `unknown`，处置等同 `irreversible`；**收窄条件**：只有"取表报错"才是 `unknown`，单纯没命中 locator 仍判 `none`（保住批7 刻意收窄的那条：交互搜索腿不得被判写而白丢重试能力）。**批16 已落（§7.8）**：`classifyStepEffect` 三态 + `stepCouldBeSubmit` 只升级"本来可能被推导出写"的形态，`is_write` 落库为真 |
| 17 | 重投要保留同一身份（Pub/Sub "A redelivered message retains the same message ID"），而**内容当身份**的前提是"同一条内容不会第二次真实出现"——这个前提在聊天里不成立 | `computeMsgID` 就是 `contentHash(channel\|conversationId\|content)`（`user-web/bridge/src/core/uplink.js` `enqueue` 里缺省填 `event_id`），服务端钩子2 按 `msg_id + conversation_id` 判等 ⇒ **同一会话里第二条"好的"必然被吞**，且这是"说了没回"在 B 链路里比 Redis 更早、更永久的一层 | **采纳（P1）批23 已落（§7.29 五）**：不取消"内容当身份"，而是在它上面加一层**发生次数身份**——扩展按可见 DOM 序给同一内容键编号，第二条起上报 `mh:<8hex>#<n>`（`channel-adapter.js::_canonicalMsgId`），`n==0` 时 id 与旧版**逐字节相同** ⇒ 存量幂等面不动；服务端 `occurrenceMsgIDRe` 只据此放弃**内容维度**的嗅探（入口窗口 + 钩子2.5），`msg_id` 精确判等与 DB 唯一索引照旧。§8.3-17 当年立的验收口径两条都成立：同会话两条同文本 ⇒ 2 行（`TestPersistBridgeHistory_OccurrenceSuffixPersistsSecondRow` + 扩展增量/批量四 it），同一条重投 ⇒ 仍 1 行（`..._OccurrenceSuffixStillExactIdempotent` + "重渲染后重扫不许多发"那条反向 it）。"严格同源单边改会裂成两套"这句担心的处理方式：正则两头锚死、注释与 `types.js` 契约同时写，且格 K10/K11 各打恒真与过宽两头 |
| 18 | "接受但不再执行"是默认档，"根本不落库"几乎没人这么做（sidekiq-unique-jobs 把**锁时机**与**冲突怎么办**拆成两个维度：`until_executing`/`while_executing`… × `on_conflict: :log/:raise/:reject/:replace/:reschedule`） | `decision.Blocked` 时两条分支（单条与批量）都在 `persistMessage` **之前** `return` ⇒ 消息**不入库**，只回一个 `Accepted=true` 的好看回执 ⇒ 排障时"客户说了没回"在库里查不到任何痕迹（证据消失） | **采纳（P1）批23 已落（§7.29 三）**：按当年给出的移交口径实现——`IsDup` 分支改"入库 + 抑制 AI"，`IsSelfEcho` 维持不落库（回声在库里已有本体，再落一行就是存两遍）。单条与批次**各一份分支各一刀**（格 K4/K5，因为批次路径有自己的一份 `Blocked` 代码，漏一边就是"单条半边绿、批里无痕"），反向半边由 `TestHandleIngress_SelfEchoDecisionPersistsNothing` 守着（防"改成全都落库"这种过度修法）。顺带把 #19 的误判方向消掉一半：留痕之后 `duplicate` 结论"已经存过了"名副其实 |
| 19 | 幂等层命中应**回放首次结果**而不是重新判定，且"是否重复"要由结论决定而非文案（Stripe 存首次 status+body 原样回放） | `IsDuplicateReason` 用子串嗅探 reason，而 reason 里混得进**落库失败原文**（§8.1-7）⇒ 误判方向是"永久停发" | **采纳（P0）批15 已落**：判定收成结论短语前缀 + 永久断言 + 两条反向腿（§7.7）。**批23 定案：不再往 outcome 枚举改**，理由与证伪方式在 §7.29 六——三条可核事实：误判方向已由前缀闭集加 `channelgw/dup_outcome_b17_test.go`（把当年真实出现的 PG 冲突原文逐字钉在 `wantFalse`）关死；#18 落地后 `duplicate` 对应的库里确有行，"回放首次结果"能省的本就不多；枚举要动 `InboxIngressResult` 及全部产出点，换来的仍是字符串换字符串。**恢复本条的时机写死**：一旦真实现场需要同时改该用例的 `wantTrue` 与 `wantFalse`（即"没前缀却要判重"或反之），本决定即刻作废 |
| 20 | 自审发现（非同行调研轴，批16b 二次审核 B4）：防不可逆动作的"先查后做"若两端都在应用层，两个并发执行流就都能查到"没做过"再各自做一遍——正解是把判定下推到存储层的**唯一约束**（Postgres 部分唯一索引天然具备，本仓该目录却只有 `model/cron.go:15` 一处 `uniqueIndex`） | 双发闸是**读后再写**：`guardResubmit` 先 `FindSubmitAttempt`（`repository/step.go`，一条普通 `First`，无 `FOR UPDATE`、无 advisory lock），通过后才由 `recordSubmitState` 写 `prepared`/`sent`；而同一时刻并发的另一条腿看到的是同一条"查不到"。同层的三条并发防护也全是同一形状：`t.Status == "running"`、`CountRunningByTask`、`CountRunningByUser`（`task.go:312-334`，v1 每用户同时 1 个 running session ⇒ 现实窗口很窄，但注释自陈"**靠 DB 唯一性兜底竞态**"，而 `browser_tasks`/`browser_sessions` 上并不存在那样一条约束） | **不采纳"再补一道 check-then-act"**：那只是把同一形状的闸门复制一遍。诚实收口是给台账加**部分唯一索引** `(task_id, text_hash) WHERE submit_state IN (sent, unattributed, verified) AND deleted_at IS NULL`，把"这次提交是否已被记过"交给插入语句本身判定（撞约束即拒发）——代价是它同时把软删语义变成契约问题（软删行参不参与唯一性、`PruneBefore` 类裁剪会不会腾出键位 ⇒ 与 #6 的"裁剪不等于证据消失"连体），属 DDL 决策而非本泳道随手改。**登记 A12（待拍板，不阻塞本批）**：本轮以文档记账，理由是"当前并发面被 per-user running 闸压到极窄"+"改法牵动软删与裁剪两处口径"。若将来放开并发（每用户 >1 session）或引入多副本 worker，A12 立即从"可缓"变为"必做"，届时必须重跑本批电池再谈。**批20f 已拍板并落地（§7.27）**：没有等"放开并发"这个触发条件，理由是这类 DDL 的收益只在真撞双发时兑现、而代价（软删口径 + 裁剪键位）必须一次想清楚，缓下去等于把两处口径原样带着走。落法与本文设想同形：`(task_id, text_hash)` 部分唯一索引 + `ON CONFLICT DO NOTHING` + 认领者回读，键不全即拒发；约束写在模型标签上（电池 C15 直接读 `pg_indexes` 验它在不在），`writeClaimRepo == nil` 走 fail-close |
| 21 | 自审发现（非同行调研轴，批16c 二次审核 B1）：**同一个判据符号被多处消费，测试却只覆盖其中一格**——评审里最常见的假绿形状是"摘掉整块会红"，它被当成了"每一格都有腿"的证据；正解是按消费点逐个下刀，让每条腿各自证明它有牙 | `classifyStepEffect` 的三态由一个符号 `writeStep := effect.needsWriteGate()` 同时喂四处（钳 retries、降级+闸门键+双发闸、D7 确认、成功/失败两路落账）。批16 只在"钳 retries"那一格有腿（`TestWSE2E_UnknownLocatorTableTreatedAsWrite` 断 click 只到线 1 次），把符号收窄回 `effect == effectWrite` 时另外三格（双发闸、降级拒绝、D7）在 unknown 步上一条断言都不走过 ⇒ 一次"不知道有没有副作用"的动作被原样重发 | **已落（批16c）**：三条腿逐格钉住 unknown 步的双发闸/降级拒绝/D7 闸门，电池不再打"一包"而是**逐消费点下刀**（M27 双发闸+降级、M28 D7、M29 只摘降级拦截、M30 只钳 retries、M31 只漏失败路径落账，M20 保留为"四道一起摘"的对照），从而证明每条新腿各自有牙。**通用口径回灌记忆**：一次"摘掉整块全红"不能登记为覆盖，必须按消费点拆刀 |

## 9. 二次审核协议落地实证（A–E 五相读数 + 越界改动清单）

协议正文在 `2026-09-22-second-review-protocol-design.md`；本节只记**这一轮跑出来的读数**，
不把协议搬过来（协议 §3 判据 8 的 96 字指针上界同样约束我自己）。

一句话结论：A–E 五相全部跑过，两本台账 84 条承诺全落四终态（`fixed` 70 / `refuted` 11 /
`blocked` 3）；B 相六路无上下文子代理报回 24 枚"可存活注码"，C 相逐刀复验后**真否掉 2 枚**
（lane 4 的结构性隐忧、lane 5 对承诺 4 的原始注法——同一路换一枚注法后成立），其余落成下表
**13 行**新落点；**我自己的一枚"非洞"判定被 lane 3 续刀实跑推翻**（§三 否决清单第一条，
它是本轮最贵的一次口径错——错的不是注码而是我的推理），复验过程另否掉**我自己**的四条二手结论
（§三 末）。

### 一、A 相：抽轴——承诺只从"代码对外说过的原文"里抽

| 台账 | 抽轴的六处承诺来源 | 计数钉（现测） | 条目 | 终态分布 |
|---|---|---|---|---|
| `ledger/R22.jsonl` | §8.3 矩阵 21 行 / §6 四项 / §7.24 §八 3 条 / §7.28 §八 3 条 / §7.29 §十三 3 条 / §7.30 §十 3 条 | 6 枚：`tablerow 21`、`numbered 4`、`bullet 3` ×4 | 43 | fixed 32 / refuted 9 / blocked 2 |
| `ledger/R22-protocol.jsonl` | 协议 §1 5 条 / §2 11 条 / §3 10 条 / §6 7 条 / §8 5 条 | 7 枚：`bullet 5`、`bullet 4`、`tablerow 7`、`bullet 1`、`numbered 9`、`bullet 7`、`numbered 5` | 41 | fixed 38 / refuted 2 / blocked 1 |

- 条目数大于计数钉之和（R22：43 > 37）的两处原因，逐条可查：§8.3 有 5 行本轮被**第二次**下刀
  （`R22-39`…`R22-43`，一条承诺两个攻击各记一行）；`R22-29` 钉的是 §7.29 §十二 证据表里
  "18 格全量已跑"那句，不在 §十三 那三条里。
- 计数钉的值由 `scripts/ledger_r22.py` 的 `pin()` **现测**，不是手抄：spec 一改（含并行 lane
  往同名节里塞条目）就先在这里显形，现测为 0 直接停机。
- coverage 归属（协议 §3 判据 9）：R22 23 个节、R22-protocol 9 个节，每个都带
  `ledger` 或 `out-of-scope` + 理由，没有一节留空。
- 本节（§九）自己也在 coverage 里，且理由与其他实证节不同：它是**协议五相的读数**，
  不是任何功能批的承诺，故归 `out-of-scope` 并单写一条理由（`ledger_r22.py` 的 `OOS_SEC9`），
  不与"某批验收叙述"共用那句泛化理由——否则本节改一个字都不会惊动任何人。

### 二、B 相：六路无上下文子代理找反例

- 派发纪律：每路只给"承诺原文 + 靶子文件"，**明令不许读台账、§七证据与我的结论**，
  只回答"哪条注码破坏了这条承诺而全套仍绿"。六份任务书与六份原样回报都在
  `ledger/logs/b-phase/`（恢复件，头部写明取自哪条逐字记录，正文一字未改）。
- 命中数：

| 路 | 攻击面 | 可存活注码 | 报回的"找不到"（负结论） |
|---|---|---|---|
| lane 1 写台账 / D7 派发 | §8.2-2、§8.3-12/13/16/20 | 2 | 3 |
| lane 2 D7 审批三轴 | §8.3-5/14/15（A5/A9/A10） | 4 | 0（总判断：核心谓词都有腿，缺的是"这条数据由谁生产"那一半） |
| lane 3 审计流 / 裁剪 | §8.3-2/6（A2/A6） | 5 | 3 + 一份"有牙、注不掉"清单 |
| lane 4 入站去重 | §8.3-9/10/17/18/19 | 3 | 2 + 1 条结构性隐忧 |
| lane 5 出站 / 回环 | §8.3-3/4/7/8 | 4 | 1 组（5 个面） |
| lane 6 门禁件自己 | 协议 §3 + 合并门禁 / 克隆门 / 闭合门 | 6 | 0 |
| **合计** | | **24** | **9 处**：lane1 3 ＋ lane3 3 ＋ lane4 2 ＋ lane5 1 组（该组本身含 5 个面，按组计 1）；lane2、lane6 各 0。lane3 另交一份"有牙、注不掉"清单（5 个面），那是**正面证据**不是负结论，不进这一列 |

- 每一路回报末尾都自陈"未验证：候选均未实际注码运行"（lane 6 例外：其中 4 刀在 `/tmp` 副本
  实跑过，另 2 刀是合成数据/静态推演）⇒ 这句话就是 C 相存在的全部理由。
- B 相自己纠正的两处**任务书**口径差，留在文件里没有抹掉：
  - lane 2：brief 写的 `user-server/internal/service/` 在这两轴上不存在，实际在
    `internal/browser_automation/service|controller/`。
  - lane 5：三条腿的坐标全是旧的（写作 `internal/browser_automation/service/…`，实在
    `internal/repository/` 与 `internal/bridge/`）。
  ⇒ 口径：任务书里的 `file:line` 从我写下它到子代理读它之间就可能已经不算数，
  下一轮 brief 要写**符号名 + 一句"坐标请按磁盘核"**。

### 三、C 相：逐刀复验（禁止按报告原文入库）

七份复验报告在 `ledger/logs/c-phase/`（同样带"恢复件"头）。24 枚刀逐条真注、真跑、读红因。

- **成立 → 补落点**（每条都要两半读数：注码下**既有**腿点名不红 + **新**腿在注码下红、还原后绿）：

| 刀 | 注码 | 新落点 |
|---|---|---|
| lane1-A §8.2-2 | `executor.go:1284` 超时支 `return err` → `Warnf` 后续流到 `sendOnce` | `…/service/d7_timeout_dispatch_b20g_test.go:26` |
| lane1-B §8.3-20 | `write_ledger.go:389` 认领报错吞成 `return nil` | `…/service/write_claim_error_a12b_test.go:35` |
| lane2 ×4 | 预览 `200→0`、judge 帧 `stepID/step_index→0`、`Seq >→<`、`expires_at` 写死 24h | `…/service/d7_gate_b20_test.go` 三条新腿 + 既有腿 `TestConfirmGateFramesBindPayloadAndOmitBody` 内两处断言 |
| lane3-A6 ×4 | 链头改接最旧摘要、`chain_hash` 退成 `batch_digest`、前驱读错列、摘要写在与 DELETE 不同的连接上 | `…/repository/retention_a6_chain_prune_test.go` 四条腿 |
| lane3 续刀 ×4（D20/D22/M7/M8） | `RowsBefore: rowsBefore`→`total`（`command_log.go:245`）、`CASE … WHEN ok THEN 't'` 折掉（`:83`）、`comment_send` 失败回包 `verdict(err == nil)`→`verdict(true)`（`executor.go:1308`）、步失败回包 `verdict(false)`→`verdict(true)`（`:1077`） | 四把刀在 19 条旧腿下**全部存活**（`a6a2-oldlegs-under-knife` 28-47 行）⇒ 补三条窄腿：`…/repository/retention_a6_test.go:667`、`:726`、`…/service/command_log_outcome_b20b_test.go:242`（M7/M8 两处站点共用一条腿的两侧断言，红因 `:280`/`:281` 各点一次，故不是重复格） |
| lane4-A 承诺10 | `dingtalk_app.go:177` 官方 msgId 换成空串 | `internal/service/dingtalk_channel_msg_id_test.go:28` |
| lane4-B 承诺18 | `inbox_ingress.go:991` `if !decision.IsSelfEcho` → `if true` | `internal/service/inbox_ingress_batch_echo_test.go:30`（正向"批里回声不落库"+ 反向"批里 dup 照样留痕"两半） |
| lane4-D 承诺17 | `channel-adapter.js:162` `occurrence > 0` → `>= 0` | `user-web/bridge/test/adapter-dedupkey-shape.test.js` 三条 it（期望键由 `adapter._hash` 独立复算，不让被测函数给自己当标准） |
| lane5-F1 承诺3 | `message_hub_inbox_outbound.go:71` 收口实参 `cutoff` → `time.Now()` | `internal/repository/message_hub_outbound_push_cap_b20d_test.go:307` |
| lane5-F2 承诺7 | `inbox_ingress_outbound.go:164` `30s` → `5s` | `internal/service/inbox_ingress_boundary_test.go:386+`（下界腿，数值来源写死成桥端 `sendOutboundTimeoutMs`，并写明既有两条 `== 常量` 的腿是自我参照） |
| lane5-F3 承诺4 | `downlink.js:51` 摘 `load()` 里的 `evict`；`:63` 摘 `add()` 的先删后设 | `user-web/bridge/test/downlink-b20d-sentcache-ttl.test.js` 两格（I / J） |
| lane5 承诺8 | `channel-adapter.js:1268` 把 `markSent` 包进 `if (sendVerified)` | `user-web/bridge/test/adapter-b24-send-verify.test.js` 结论位红线半边 |
| lane6 ×6 | 六处"摘掉判据后反向格照样全绿" | 三份 `--selftest` 各加格（12→15、7→9）+ 新电池 `scripts/reverse-test-gate-teeth.py`（12 刀）+ `reverse-test-l4-baseline.sh` 补第四向（7 格） |

- **报回被我否决的条目**（协议 §8 判据 5 要点名的这一类；否决也要留下注码复验原文，
  已写进两本台账的 `reverify`）：
  - lane 3 `RowsBefore: rowsBefore` → `total`：**本条否决已被推翻，留在原位当作反例。**
    我当时的理由是"run 行只在批次循环跑满后才写，任何中途分岔都会在写行之前 `return err`，
    两个值在已提交的行上不可能不同"⇒ 判**非洞**、不补腿（同 §7.28 §八-1 那个"不给无人读的面记账"的理由）。
    lane 3 的续刀取证（`logs/R22-lanes/a6a2-oldlegs-under-knife-r22lane3.log:28`）读数是
    **D20 在 19 条旧腿下存活、19/19 全绿、skip=0、包没编译红**——同文件 `:64` 还指出：B 相把这一格
    挂在那条两批次大腿上，而**那条腿自己的注释就写了它杀不掉这一刀**（入口计数谓词 == 窗口/删除谓词
    ⇒ 注码与实现恒同值）。⇒ 我的理由把"我现有腿构造不出这一幕"当成了"这一幕不可能发生"，
    前者的处置是补腿，不是否决。落下来的腿不去放宽期望，而是去造那一幕：入口 5 行、裁剪进行中由
    行级触发器补写第 6 行越界、实删 6 行分两批 ⇒ `rows_before` 与 `rows_pruned` 当场分家
    （`TestA6RunRowCountsEntryWindowNotLateCrossing`，`…/repository/retention_a6_test.go:726`；
    红因 `:811 rows_before=6 want 5` + `:816 相差 0 want 1`，见同目录 `a6a2-newlegs-fail-text` 39-40 行；
    行序前提当场自证在 `:747`，它挡住的是"越界那行被同一条语句吞掉 ⇒ 整批回滚 ⇒ 本腿无对象"）。
    同一次续刀把 `CASE … WHEN ok THEN 't' ELSE 'f'` 折掉 t/f 那一刀（D22，`:83`）也读成**存活**
    （旧腿集 19/19 全绿）并补了腿（`TestA6DigestSeparatesPassedFromFailedFrame`，
    `retention_a6_test.go:667`，红因 `:696`）——C 相表里那一行"lane3-A6 ×4"没把它算进去，
    它是在续刀取证里才成为一格的。
    ⇒ 口径回灌：**"存活"一律按"存活即补窄腿"处置**；否决一条存活候选只允许一种理由——
    能指出该注法在语义上打不到任何对外承诺，"全量包 0 失败"本身不是理由，它正是缺腿的定义。
  - lane 4 "闭集与真产出只靠两处手抄字面量对齐"：删 `protocol.go:340` 的前缀 ⇒
    `dup_outcome_b17_test.go:28`、`protocol_test.go:182`、`handler_http_ack_test.go:27` 三条即红；
    且 `handler_http.go:725` 的 `isIngestDuplicate` 整函数就是转发，不存在第二份手抄表
    ⇒ 缺口不成立，不补桥接腿、**也不改任何既有期望**。附带查出该表里 `"self-echo"` /
    `"duplicate"` 两项被**非产出文案**钉住（Go 侧产出方一律包 `intercepted by middleware: ` 前缀）
    ⇒ 属文案卫生，登记不动，不算缺腿。
  - lane 5 对承诺 4 的**原始注法**（"落盘把仍在 TTL 内的条目忘掉"）：`has()` 与 `evict()` 读同一个
    `sentCacheTtlMs`，服务端重推上界 `20 × 30s ≈ 10min ≪ 24h`（既有 E 格钉着）⇒ 能表达的该型注法
    全被既有腿杀。**但同一路换一枚注法打到真洞**（摘 `load()` 里的 `evict`，9 条既有腿全绿）
    ⇒ 否决它报的那一半、成立另一一半。
  - lane 1 报回的三条"找不到存活注码"（§8.3-12/13/16）也逐条复验过：注码即红 ⇒ **真负**
    （不是漏报）。原文与腿名清单在 `report-efc61a606f8.md`。
- **复验同时否掉了我自己的四条二手结论**（这一类比否决子代理更贵，因为没人会来复核它）：
  - 私有克隆的装架口径错过一次：`logs/R23dedup/run2-16cells/` 里 `lane_overlays()` 把并行 lane 的
    **未跟踪实现文件**（`payment.go` 等 4 个）连同它们的 `_test.go` 一起排除 ⇒ 克隆里
    `internal/service/integration.go:69: undefined: OrderPaymentSink` 等 10+ 处编译不过，
    控制组读出 `rc=1 total=4`、驱动自判"控制组不干净——后面所有红/绿都不可信"并以 `BATTERY_RC=1`
    停住。它没把红读成绿，但**那句写进脚本注释的口径是错的**；`run3-16cells-overlayfix/` 改成
    "外域未跟踪只排 `_test.go`"后控制组 `rc=0 total=22 passed=22`、16 格全杀。
    ⇒ 排半成品要按"是否参与编译"排，不是按"是否外域"排。
  - `K13` 的归属记反了：本轮上下文里一度把它记成"我否决掉的假洞"。盘上读数相反——
    `battery-18cells-echo-half.log` 里它 `存活=洞`（批次半边那条腿当时确实不存在），
    `battery-18cells-run2-batch-echo-leg-in-clone.log` 里补腿后 `杀掉`，控制组从 22 涨到 23。
    ⇒ 进"否决清单"的东西和进台账的一样要有出处；没出处的自我叙述不算复核过。
  - "7 条新腿被别人的 `collection_job.go` 重复方法挡住"这条**查无出处**：
    `git diff HEAD -- user-server/internal/service/collection_job.go` 为空，
    本轮全部逐字记录里 grep `duplicate method` / `包编译不过` 0 命中 ⇒ 不写进台账、
    也不当结论用（宁可留一条"我说过但核不出来"，也不把它抄成事实）。
  - 还有一条在**写本节时**现形：上下文里我"读到"过 §九 六 有一句"另一处匿名结构体 `:398` 仍留原样"，
    对着盘上文件复算——`grep -c 匿名结构体` 在 HEAD 版与工作树**都是 0**，上一轮逐字记录里 11 处命中
    全部来自本轮我自己打的字与命令 ⇒ 那句从没落进文件。这一条不是"结论被否"，是**叙述本身是空的**：
    它差点被当成"我披露过、只是丢了"补写回去，而正确处置是先证明它存在过。
    ⇒ 凡"我记得我写过 X"，先 `grep` 那个 X 在不在，再决定是"补写"还是"承认没写"。

- §九 全部对外引用的机械复量：现测 **12 处写成路径的 `file:line` ＋ 22 处裸文件名引用 ＋ 12 个证据
  文件名**，判据是"路径按字面解析、行号不越过目标文件末尾、证据文件名在盘上"，读数
  `UNRESOLVABLE=0 BEYOND_EOF=0 MISSING_EVIDENCE=0`。这一趟**常驻化**了，不再是 /tmp 里的一次性脚本：
  探针 `ledger/tools/check-spec-citations.py`（用法带两个参数：spec 相对路径 ＋ 该节标题前缀），
  门牙反向测 `ledger/tools/check-spec-citations-reverse.sh` 现跑 `4/4 种坏形态各自点名到该开火的那一格`
  （控制组绿 ＋ 路径写歪／裸名行号越界／证据查无各红一次；注码只打在 `/tmp` 副本上，仓里文件逐字节不动）。
  **反向测在这一版抓到的是探针自己的洞**：裸文件名那条分支原先只判"文件在不在"、不查行号，
  把 `migrate.go` 那条引用的行号改成九十九万注进去时 rc=0 全绿 ⇒ 本节的 22 处裸名引用
  （占多数）当时等于没被复量过行号；
  补上"行号 ≤ 同名站点里最长那份的末尾"后再放同一刀才红。⇒ 口径：**探针的绿要先证明它测得到这一类**。
  内容层面另外逐处读回原文对得上：`migrate.go:383/384/397`（三行分别是
  `&browsermodel.BrowserAuditDigest{}` / `BrowserAuditPruneRun{}` / `BrowserWriteClaim{}`）、
  `executor.go:1284`（`default: // confirmWaitTimedOut：确认预算或执行预算先到`）、
  `dingtalk_app.go:177`（`"channel_msg_id": msg.MsgID`）；
  裸文件名 `executor.go` / `dingtalk_app.go` 各有同名文件在别的包，靠同一行里的包名消歧，
  判据不靠消歧后的巧合——两处内容都点过。

### 四、D 相：修 + 回归（串行、私有克隆、逐刀核 md5）

- 协议 §5 纪律的执行读数：本轮注码涉及 **11 个生产文件**（Go：`executor.go`、`session.go`、
  `write_ledger.go`、`command_log.go`、`message_hub_inbox_outbound.go`、`inbox_ingress_outbound.go`、
  `dingtalk_app.go`、`inbox_ingress.go`、`channelgw/protocol.go`；JS：`downlink.js`、
  `channel-adapter.js`）**+ 5 个门禁件**（`merge-gate.py`、`check-review-closeout.py`、
  `run-gate-in-clone.py`、`build-review-ledger.py`、`check-architecture.sh`）。
  每刀留**三值 md5**（备份 / 注码态 / 还原后），逐刀写在各自报告里；收尾复扫：本轮注码没有留下 `.bak`
  与注码残留、`gofmt -l` 全树空。**"树里一条 `.bak` 都没有"这句我说大了**：复扫只对我动过的文件成立，
  树里另有一条 `user-server/bin/user-server.r17.bak`（69MB Mach-O、mtime 09-09 12:07、被 `.gitignore`
  吃掉所以 `git status` 不列），归属并行 lane 的旧产物，不在本轮清单里、也没被我当残留删掉。
- Go 侧注码一律走 `git clone --shared --no-checkout` 私有克隆 + 覆盖层（**未跟踪的新腿文件必须进
  覆盖层名单**——漏了它就把"克隆里没这条腿"读成"刀存活了"）；JS 侧才允许就地注、逐刀 `cp` 还原。
- 本轮新建/扩展的常驻电池与全量读数（日志在 `logs/R22-lanes/`、`logs/R22dedupkey/`、
  `logs/R23dedup/`、`logs/R23sseack/`、`logs/c-phase/`）：

| 电池 | 格 | 读数 |
|---|---|---|
| `mut_d7gate_r22lane.py` | G1–G4 | `Go 4 格（G1–G4）逐刀被杀、无存活`，两趟 rc=0 |
| `mut_d7verdict_b20g.py` | 3 | `Go 3 格逐格被杀，无存活`（同族 `G1≈G2≈G3` 已如实标注，不假装三格独立） |
| `mut_write_claim_a12.py` | 16 | `Go 16 格逐格被杀，无存活` rc=0 |
| `mut_outbound_claim_r22lane.py` | 3 | 第一趟**判出洞**（`O1a 存活=洞`，rc=1）⇒ 补轮询路径那条腿，第二趟 `Go 3 格逐格被杀` rc=0 |
| `mut_dingtalk_msgid_r22lane.py` | 2 | `Go 2 格逐格被杀，无存活` rc=0 |
| `mut_sentcache_r22lane.py` | 2 | `2 格逐刀被杀，无存活`，两枚 JS 文件逐字节还原 |
| `mut_dedupkey_shape_r22.py` | D1–D2 | 注码下恰好三条 it 红、无连带误伤；整包终局 `59 files / 769 passed` rc=0 |
| `mut_ingest_dedup_r23.py` | 18（含 K12/K13 两侧） | `Go 18 格逐格被杀，无存活（全电池）`，控制组 `total=23` |
| `reverse-test-gate-teeth.py` | 12 刀 | `门牙反向测：12/12 刀各自点名到该开火的那一格` |
| `mut_retention_a6.py`（lane 3 全量回跑） | 23→**27**（加 D19–D22） | `===== 电池判定：Go 27 格逐格被杀，无存活（全电池） =====`；控制组这一趟现测 `rc=0 total=29 passed=29 skip=0 FAIL=[]`（29＝本轮往该包加了 2 条腿后的真读数，不是写死常量）。日志 `logs/R22-lanes/mut_retention_a6-r22lane3-full27.log` |
| `mut_command_log_ok_b20b.py`（lane 3 全量回跑） | 6→**8**（加 M7/M8） | `===== 判定：8/8 格，逐格被杀，无存活 =====`；两个控制组 `[service] rc=0 settled=6 passed=6 skip=0 红名=[]`、`[migrations] rc=0 settled=4 passed=4 skip=0 红名=[]`（service 侧 5→6 也是加了新腿）。日志 `logs/R22-lanes/mut_command_log_ok_b20b-r22lane3-full8.log` |
| ↳ 收口轮把 A6 电池整族再跑一遍（不是只跑新格） | 27→**28**（加 D23：行数守卫短路） | `===== 电池判定：Go 28 格逐格被杀，无存活（全电池） =====`；控制组这一趟现测 `rc=0 total=30 passed=30 skip=0 FAIL=[]`（30＝本趟所在树比 lane 3 那趟又多一条腿），那 27 格**同趟复跑**、没有沿用上一趟的读数。日志 `logs/R22-lanes/mut_retention_a6-r22close-full28.log`；"改完驱动先跑 1 格反向"那一趟另存 `logs/R22-lanes/mut_retention_a6-d23cell2-r22close.log`（`杀掉 total=30 fail=1 skip=0` 只点名新腿），被否掉的第一版夹具另存 `logs/R22-lanes/mut_retention_a6-d23-cell-r22close.log` |
| ↳ 两趟之间的取证：旧腿集下先把刀放一遍 | D20/D22/M7/M8 四刀 | 补腿**之前**的读数：四刀在 19 条旧腿（repository）与 5 条旧腿（service）下**全部存活**，`PASS+FAIL==旧腿集数`、`skip=0`、红名单空——存活不是取证假象（`logs/R22-lanes/a6a2-oldlegs-under-knife-r22lane3.log:28,38,42,47`）。补腿后同目录 `a6a2-newlegs-fail-text-r22lane3.log` 逐格给「注码下红 + 还原后绿」两半 |

- 单包回归（**全量口径，不是 `-run` 子集**）：`internal/browser_automation/service`
  `ok 237.230s`，同包另两趟终局 `229.232s` / `225.982s`；`internal/service 604.115s` +
  `internal/channelgw 6.858s`（顶层 `--- PASS` 4072 / `--- FAIL` 0 / 顶层 `--- SKIP` 4，四条跳过均系既有无关用例；
  含子测试行的 `--- PASS` 总数是 5008，此处按顶层计）；
  `internal/browser_automation/repository ok 23.057s`；桥侧 `vitest` 整包 rc=0。
- 与并行 lane 的共处读数：本轮实测发生过一次"共享树上的瞬时假红"——某路跑 `downlink` 用例时
  中途读到 3 条失败，`diff`/`md5` 复读证明是**另一条泳道当时正把自家注码放在盘上**，
  不是它的腿（`report-45c150dcc56.md` 末段）。处置口径：读到红先比 md5 再归因，
  落定的树上复跑，不许拿瞬时读数入库。

### 五、E 相：收口（合并门禁 + 闭合门 + 反向自检）

收口的顺序是被门自己规定的，不是随手排的：合并门禁里有四步**读文档**（`markdownlint`、
`doc-consistency`、`closeout-r22`、`closeout-r22-protocol`），而台账的证据钉要从**跑完之后**的
全量转录里现取（门跑到 `closeout-*` 那一步时，本轮那份转录的末行还没写下）。所以这一段是
**先把文档定稿 ⇒ 再跑全量 ⇒ 再出台账 ⇒ 最后闭合门 ×2 加克隆门收口**；任何一步颠倒都会让门去
引用一份还没写完的文件——那就是 §七 里那条"引用没跑过的东西"的另一种形态。

#### 1. 前几趟全量跑为什么都不作数（红各自点名到文件行）

| 趟 | 末行 mtime | 读数 | 为什么不当末次取证 |
|---|---|---|---|
| 第 1 趟 | 08:14 | `===== 合并门禁：32/34 门步绿，红在：go-test-host-packages closeout-r22-protocol；诊断步红 1 道不门控：ci-step-coverage =====` | 存档 `logs/R22-closefinal/merge-gate-full-run1.log`，台账**不引用**它——引用只认末次全量跑那一份 |
| 第 2 趟 | 08:28 起跑，跑到第 4 步被我主动中止 | 部分转录 7 行（存档 `logs/R22-closefinal/merge-gate-full-run2-aborted.log`）：`go-build`/`go-vet` 红、`gofmt` 绿、`go-test-browser-automation` 绿 328.8s | 这一趟是**我自己作废的**：08:41:52 我改了 `scripts/check-review-closeout.py`，而这趟正在读它 ⇒ 它后面 31 步测的是一颗已被换掉的判据。发信号前先 `pgrep -P` 认出那是本趟的子进程，`MERGE_GATE_RC=143` |
| 第 3 趟 | 12:09 | `===== 合并门禁：33/34 门步绿，红在：go-test-browser-automation；诊断步红 1 道不门控：ci-step-coverage =====`（`merge-gate-full-run3.log:7` 那行红带着判据原文 `rc=1 ; 输出里出现了禁止行 '(^\|\\n)FAIL'`） | 红是本泳道自己的 b21 夹具抽签，修完才有第 4 趟；逐份取证见 §五 第 6 段 |
| 第 4 趟 | 12:32 | `===== 合并门禁：34/34 门步绿，无红；诊断步红 1 道不门控：ci-step-coverage =====`，`MERGE_GATE_RC=0` | 这一趟是**绿**的，但它是**临时台账**（`R22_PROVISIONAL=1` 那版）跑出来的：它之后我还要把三格引用接回本轮、并往本段散文里补第 3/4 趟这两行（＝改 §9 的条目形承诺 ⇒ 台账要重出）。台账引用的取证必须是**最终那份台账之后**那一趟 ⇒ 存档 `merge-gate-full-run4.log`（连同 `merge-run4/` 全部 35 份逐步日志；副本 md5 与当时活文件同值 `4660a540…`），末次留给第 5 趟 |
| 第 5 趟 | 13:22 | `===== 合并门禁：36/36 门步绿，无红；诊断步红 1 道不门控：ci-step-coverage =====`（逐步日志另存 `merge-run5/`，37 份） | 这一趟也是**绿**的，且步骤名单已经是 37 步。它不作数只因为**测的字节后来又被换了**：13:36–13:40 我按 §五 第 7 段那五处取证链缺陷里的后两处（④ 手写条件行、⑤ 汇编件不测自己）改了 `check-review-closeout.py` / `reverse-test-gate-teeth.py` / `run-gate-family-selftests.py` 三份，而这一趟的 `closeout-selftest`、`family-selftests-selftest` 两步正是读/跑这三份的。存档 `merge-gate-full-run5.log` |
| 第 6 趟 | 13:25 起跑，第 5 步（`go-test-host-packages`）在跑时被我 SIGTERM 中止 | 部分转录 7 行（存档 `logs/R22-closefinal/merge-gate-full-run6-aborted.log`；四步绿：`go-build 11.2s`、`go-vet 2.9s`、`gofmt 0.4s`、`go-test-browser-automation 250.6s`），**没有末行判定，也没有 rc**：中止时连 wrapper 一起收掉，`echo MERGE_GATE_RC=$?` 那句没执行。这四步的逐步日志在下一次全量跑原地刷掉 `merge/` 之前另存 `merge-run6-aborted/`（4 份） | 这一趟本来是当"交付那趟"起的，起到一半发现两件事没做完：§五/协议 §3 的读数回灌、以及刚抓到的手写条件行（§五 第 7 段缺陷 ④⑤）。让它跑完只会产出一份注定被原地覆盖的转录——而"半趟转录躺在被引用的路径上"正是第 2 趟记过的那个形状（那时靠末行判定缺失拦下；本轮改成"跑之前先确定没有待改的判据字节"） |
| 第 7 趟 | 14:08 起跑 → 末行 14:32 | `===== 合并门禁：36/36 门步绿，无红；诊断步红 1 道不门控：ci-step-coverage =====`，`MERGE_GATE_RC=0`（`logs/R22-closefinal/merge-gate-full.log:1` 的 `$ python3 scripts/merge-gate.py --round R22-closefinal` ＋第 2 行 37 步名单；慢步实测 `go-build 22.2s`、`go-test-browser-automation 314.9s`、`go-test-host-packages 786.9s`、诊断步 `ci-step-coverage 56.2s ｜ rc=1`） | **这一趟绿，但它是"绿但过渡"那一类（第 ③ 级）**：它跑完之后本段又长了两处**可数**内容——本行自身＋§五 第 3 段那条第 ④ 级阶梯的 bullet ⇒ §9 的可数条目从 94 长到 95，闭合门当场红 `节「9. 二次审核协议落地实证…」承诺总数实抽 95 条，声明 94 条`（我拿安装好的旧台账复算时实测 `rc=1`，协议那份 `rc=0`）⇒ 台账只能重出，重出后的台账只能由**再下一趟**全量跑证。取证仍全部留住：逐步日志 37 份 → `merge-run7/`，转录副本 `merge-gate-full-run7.log`（md5 与当时活文件同值 `53744ca6…`），两份 closeout 读数从 `merge/` 复制到轮次根（`closeout-r22.log` `d293d7b0…`、`closeout-r22-protocol.log` `4d32d5ba…`，与 `merge/` 那份逐字同 md5；两者与 13:23 那版的差异只有第 3 行的秒数 `12.9s→8.9s`／`2.2s→1.8s`，`rc=0` 未变）。它起跑前的字节对账（台账 14:01:30 落盘、五份门禁件最晚编辑 13:40:00）仍然成立，只是"跑完之后账又要重出"这一件事第 5 趟没碰上、第 7 趟碰上了 |
| 第 8 趟 | 起跑于台账按第 ④ 级约定重出并安装之后 | **本行是声明行，刻意不带读数**：命令 `python3 scripts/merge-gate.py --round R22-closefinal`（37 步、全程不带 `-run`），产物落在 `logs/R22-closefinal/merge-gate-full.log` 与 `merge/`；它测的字节＝落笔时刻盘上的字节，因为本行之后 §9 再没有多出一个可数条目 | 这一趟是交付那趟。按 §五 第 3 段那条**形状约定**（四级自指阶梯的第 ④ 级）：末次那趟的行先立、不带读数，跑完把读数写进表下方的散文段——散文行不带 `-`／`\|`／`>` 行首，不在门的三种可数形状里 ⇒ 记末次取证第一次变成不改变台账的动作。任何拿本表当"末次"的人请往下读那一段 |

各趟的红各自由**它自己那一趟的逐步日志**证着，且都在下一次全量跑会覆盖的 `merge/` 之外另存了一份
（实测份数：`merge-run1/` 35、`merge-run2-aborted/` 4、`merge-run3/` 4（只留那朵红的四方取证，全量那份在
`merge-gate-full-run3.log`）、`merge-run4/` 35、`merge-run5/` 37、`merge-run6-aborted/` 4、`merge-run7/` 37、
`merge-run8/` 37；`merge/` 里的同名文件会被末次跑原地刷掉，取证不能靠它）。

第 8 趟（交付那趟）的读数记在下面这段散文里：起跑 14:45、末行写下 15:07，命令
`python3 scripts/merge-gate.py --round R22-closefinal`（37 步、全程不带 `-run`、`DEVELOPER_DIR` 指向 CLT），
判定行 `===== 合并门禁：36/36 门步绿，无红；诊断步红 1 道不门控：ci-step-coverage =====`、
`MERGE_GATE_RC=0`（转录 41 行、md5 `db6a88f1…`，副本 `merge-gate-full-run8.log` 与之逐字同 md5）。
慢步实测 `go-test-host-packages 698.8s`、`go-test-browser-automation 241.4s`、`arch-l4-reverse 116.8s`、
`arch-gate 14.7s`、`go-build 14.5s`、`unwired-assets 34.0s`、诊断步 `ci-step-coverage 59.5s ｜ rc=1`。
逐步日志 37 份已另存 `merge-run8/`，且"早于起跑时刻的份数"实测为 0
（`find merge -maxdepth 1 -name '*.log' ! -newermt '14:45'` 零行）⇒ 那 37 份确属同一趟，不是两轮混存。
两份 closeout 读数从 `merge/` 复制到轮次根、与源同 md5（`closeout-r22.log 427f348f…`、
`closeout-r22-protocol.log 66dc96b3…`；与第 7 趟那版的差异只有第 3 行的秒数 `8.9s→9.2s`／`1.8s→2.2s`，
`rc=0` 与判定行未变）。

这一段散文本身是第 ④ 级约定第一次真的兑现：记末次取证这个动作**没有**改变台账——判据不是我自称的，
是"两份台账按记录之后的文档重新生成到 `/tmp`、与安装位逐字对比 diff 0"＋"两道闭合门各自单跑 `rc=0`"
这两条现测，读数记在本段末。第 7 趟之所以白跑，就是因为当时记一趟要往表里加一行（＝加一个可数条目）。

本趟起跑前的字节对账（第 5 趟正是栽在这件事上）：五份被 `gate-teeth-battery.log:2` 钉住 md5 的门禁件，
实盘值与表头逐字相同（`anchor-preflight.py d21fe6c7…`、`check-review-closeout.py 318ffb83…`、
`merge-gate.py 4de3352c…`、`run-gate-family-selftests.py 010b9344…`、`run-gate-in-clone.py a4f77d4f…`），
五份 mtime 最晚 `13:40`，全部早于本趟起跑的 14:45 ⇒ 本趟跑的判据与电池测的是同一颗。

留在轮次根、台账不引用的过渡件在此逐笔归因（免得下一位把"没被引用"读成"来路不明"）：
`merge-gate-only-9steps.log`（11:17）＝零遗留-P 那一格的现场，命令是 9 步 `--only` 子集、第 9 行印
`[红] closeout-r22 8.0s ｜ rc=1 ; 输出里出现了禁止行 '❌'`，留它的理由正是"子集跑不许当末次取证"；
克隆门在本轮的四次前跑＝`gate-in-shared-clone-provisional.log`（08:26，首行自报 `R22_PROVISIONAL=1`
的 bootstrap 读数）、`gate-in-shared-clone-run-pre-verdictfix.log`（HEAD `a63700a4`、覆盖 773 份、末行只有
"克隆已删除…"没有判决行＝判决行那一格还没加时的产物）、`gate-in-shared-clone-run-transitional-provisional.log`
（HEAD `fb80e634`、777 份）、`gate-in-shared-clone-run-2026-09-23-1324.log`（HEAD `d6eebfa6`、820 份，
末行已是判决行）。台账引用的那份克隆取证在本段写完后**为"记完读数之后的那棵树"重跑了一次**：旧那份
（14:07，HEAD `c985cdf3`、覆盖 829 份）先按取证时刻另存 `gate-in-shared-clone-run-2026-09-23-1407.log`
（md5 `3cd064fc…` 与原件同值，文件名带时刻、不带结论），重跑出的新那份落在根路径
`gate-in-shared-clone.log`（HEAD 仍 `c985cdf3`、覆盖 906 份＝本轮新增的取证件都跟着进克隆了，
`===== 克隆面：rc=0，门在私有克隆里跑得出与工作树逐字一致的读数 =====`）。
`closeout-r22-run-pre-ledger37.log`（12:40）与 `closeout-r22-protocol-run-pre-ledger37.log`（12:44）
＝台账尚未按 37 步名单重出时，两道闭合门各自的那一对读数。

第 ④ 级约定的现测读数（本段末，也就是上面"判据不是我自称的"所指向的地方）。改完这两段散文之后，
从**当前**文档重新生成两份台账到 `/tmp/L64-*.jsonl`，与安装位逐字对比：`R22.jsonl` 44 行对 44 行、
`R22-protocol.jsonl` 42 行对 42 行，`diff` 输出 **0 行**——"记末次取证"这个动作确实一个可数条目都没动，
台账不必重出（第一次算这条 diff 时被 zsh 的 `set --` 不拆词骗过一回：`diff` 报的是 usage、`grep -c`
数出 0，看着像"零差异"。上面那两个行数是先把两侧各 `wc -l` 打出来、再看 `diff` 明细为空得到的）。
再把所有读文档的门各单跑一遍（直接执行、不进驱动、不动 `merge/` 那 37 份）：`closeout-r22` `rc=0`、
`closeout-r22-protocol` `rc=0`、`closeout-selftest` `rc=0`
（`===== selftest：59/59 种坏形态各自隔离地红，合法快照 0 红 =====`，证明上面两朵绿不是门失牙）、
`doc-consistency` `rc=0`、`feature-doc` `rc=0`、`check-md-links-offline` `rc=0`、
`anchor-preflight` `rc=0`（`已登记的 5 份电池：锚点在工作树上各命中一次`）、`markdownlint` `rc=0`
（`Summary: 0 issues in 0 files`）。这一段落下后又按同一条命令再生成一次到 `/tmp/L65-*.jsonl`：
两份 `diff -q` 静默、md5 与安装位同值（`R22.jsonl 6a31baea…`、`R22-protocol.jsonl 13d30d6f…`），
两道闭合门与 markdownlint 各自复跑 `rc=0`——加一句自我说明也没能改动台账，这才是第 ④ 级算立住。

第 2 趟被中止后，那 7 行部分转录仍躺在台账引用的路径上，而它**没有**末行判定——这就是"证据在、
mtime 也新、内容却是另一趟（半趟）的读数"那一类，靠 §3 的内容钉拦：生成器现在从文件里现取判定行，
取不到就不肯出台账。

三朵红的归因，两条是并行 lane 的在飞窗口、一条是门按设计拦停：

- 第 1 趟 `go-test-host-packages`（`rc=1`，1047.1s）红因是 `internal/migration/migrations [build failed]`
  （`logs/R22-closefinal/merge-run1/go-test-host-packages.log:9`）：
  另一路在 08:00:56–08:05:49 把 `internal/service` 改到编译不过，08:06 复测该包 `go build` 已 `rc=0`
  ⇒ 在飞窗口，不是本仓红（口径同 §四 末条"读到红先比 md5 再归因，不许拿瞬时读数入库"）。
- 第 1 趟 `closeout-r22-protocol` 红在协议 spec §8 条目原文本轮被改 ⇒ 旧台账的 `claim.quote` 指针失效。
  **这一朵是门按设计该红**：改了承诺原文，引用它的台账必须重出（重出后消除，见本段末）。
- 第 2 趟 `go-build` / `go-vet` 红在
  `user-server/cmd/seed/main.go:190:115: syntax error: unexpected newline in argument list; possibly missing comma or )`
  （取证 `logs/R22-closefinal/merge-run2-aborted/go-build.log`、`go-vet.log`）⇒ 又是另一路正在写 seed。
  同趟 `gofmt` 之所以绿，是因为那条步的命令是 `gofmt -l user-server/internal`，**盖不到 `cmd/` 下**——
  这道步的覆盖面口径本就如此，不是漏判，但它意味着"`gofmt` 绿"永不蕴含"树能编译"。

#### 2. 收口途中机器换了环境（单记一条，免得下一位把环境红读成代码红）

第 2 趟之后机器重启过（`uptime` 于 10:12 前后读数 `up 33 mins`），且 Xcode 在这期间自动更新到 27.0：
`/Library/Developer/CommandLineTools/SDKs/` 里 `MacOSX.sdk → MacOSX27.0.sdk` 等符号链接的 mtime 是
10:59–11:00，`pkgutil --pkg-info com.apple.pkg.CLTools_Executables` 读 `27.0.0.0.1788430756`。
更新后**许可未接受**，于是默认 developer dir（`xcode-select -p` = `/Applications/Xcode.app/Contents/Developer`）
下任何需要 cgo 的编译都被拒：

```
$ go build ./...                          # rc=1
# runtime/cgo
You have not agreed to the Xcode license agreements. Please run 'sudo xcodebuild -license' ...
$ go vet ./cmd/routeinspect/               # rc=1，同一句
```

这与本轮改动零因果——第 1 趟 08:09 的 `go-build` 还是绿的（13.9s），那是在旧 Xcode 上跑的。
处置：末次全量跑在 `DEVELOPER_DIR=/Library/Developer/CommandLineTools` 下执行（同一份 Go 工具链、
同一版 SDK 27.0，只是 clang 走 CLT 那份），放刀前实测该环境下 `go build ./...` 与 `go vet ./...`
均 `rc=0`（唯一输出是 `github.com/shoenig/go-m1cpu` 的两条 `-Wgnu-folding-constant` 警告）。
**默认目录仍是红的，这台机器要 `sudo xcodebuild -license accept` 才恢复**——那是需要用户口令的系统级
动作，本轮不代做，已登记进 §七；这条环境变量属于"补环境前提"，不属于"改判据凑绿"：它不关任何一步、
不改任何期望，且 §五 这一段把它写在明处，下一位拿到同一台机器若没接受许可，会在这里看到红因。

#### 3. 台账引用面：从"核存在性"改成"生成时现取内容钉"（本轮收的一个洞）

`logs[]` 早先允许写成纯路径字符串——门只核"文件在、mtime 不早于 `first_seen`"。第 2 趟那 7 行
部分转录正好命中这个洞：文件在、mtime 最新、内容却是半趟。收口做法是在**生成时**钉住内容：

- `evidence_pin()` 打开被引用的那份文件，用闭合门自己的 `log_window` + `strip_ansi` 取窗口，
  抽出所有 `=====` 判定行（去表头 `|` 与前导空白、短于 12 字的丢掉）当 `must_say`；
  **抽出来逐条回查在不在窗口里**，不在就 `SystemExit`——我上一版手抄的三枚钉
  （`FAIL=hivemtk-user/internal/service`、D2 的 `TestSignalConfirmBindsPayload`、桥侧
  ` Test Files  1 failed (1)`）就是这样被自己拒掉的，磁盘上根本没这些行。
- 自指的三份取证（`gate-in-shared-clone.log`、`closeout-r22-protocol.log`、`merge-gate-full.log`）
  是**本轮这一趟正在写**的文件——闭合门不许打开自己正在写的那份（跑到第 33 步时末行还不存在），
  而克隆那趟又要求"工作树里的门全绿"才印得出它自己的判定。所以分两遍：第一遍
  `R22_PROVISIONAL=1` 把这三格引用成**上一轮**`logs/R22-protocol/` 里的旧取证——注意对旧文件
  照样是**内容钉**（本轮生成的 P-26 钉住那两份的判定行、P-38 钉住 `合并门禁：2/2 门步绿`），
  不是"只核存在性"放过；跑完再把引用接回本轮这一份。
- 接回来之后仍有一处只能钉**开头那两行**（`$ python3 scripts/merge-gate.py --round R22-closefinal`
  ＋`步骤名单（N 步，全程不带 -run）`，N 由生成器从 `merge-gate.py` 的 `STEPS` 活读，本轮从 35 长到
  37（新注册的两道步＝`clone-selftest` 与 `family-selftests-selftest`，见第 7 段缺陷 ③）⇒
  台账里那一句跟着长，没人手抄过它）：`merge-gate-full.log` 的末行判定是这一趟最后才写下的，
  生成台账时读不到。不许出现的串钉成 `红在：` 与 `--only`，于是**跨趟对账**成立——本趟跑到那一步时
  判定行还没写⇒绿；下一趟再核这份已写完的文件时，禁串才算数。
- 三份不自指的常驻电池（`selftest-closeout` / `gate-family-selftests` / `gate-teeth-battery`）走
  `VERDICT_PINNED` 强制升级成内容钉，防止"写成散文就免检"。
- 第四条自指陷阱是本轮最后一刻撞上的，形状和前三条不同：**台账 meta 里钉着 §9 的可数条目总数**，
  而 §9 就是"记这几趟跑"的那一节——所以往表里补一行读数＝台账漂一格＝必须重出＝只能由再下一趟全量
  跑证。第 7 趟绿着跑完后我只为本段补了它的读数行，闭合门当场红
  `节「9. …」承诺总数实抽 95 条，声明 94 条`（红落点＝计数钉，非内容钉）。
  收法不是把行数改回去，是给表的下限立一条**形状约定**：末次那趟的行**先立、不带读数**，
  跑完把读数写进表下方的散文段——散文行不带 `-`／`|`／`>` 行首，不在 `ENTRY_START` 的三种可数形状里，
  于是"记末次取证"这件事第一次变成**不改变台账**的动作。第 8 趟就是按这个形状写的：它的行在第 1 段
  那张表里，读数写在**那张表下方**的散文段（判定行本体不抄，仍只在 `merge-gate-full.log` 末行，见第 5 段）。

把上面几条合起来看，本轮立起来的其实是一条**四级自指阶梯**：① 门不许打开自己这一趟正在写的那份
文件（所以那三格只能钉语义串，判定行等下一趟再核）；② 临时档那一遍用**上一轮**的取证把本轮其余
读数跑绿，它买回的只有"引用面接回本轮"这一件事；③ 一趟**绿但过渡**的读数只准用一次——用完必须
重出台账，不许拿它当末次取证（第 1 段里第 4 趟与第 5 趟各踩过一次这条）；④ **记录末次取证这个动作
本身不许改变台账**（否则第 3 级会无解地打转：每记一次就再要求一趟新的全量跑），落法就是上一条的
"行先立、读数进散文"。四级都写在明处，是因为这条"用自己证明自己"的序错了不报错，只会静默产出一份
永远绿而什么都没证的账。

末次实测生成读数（第二遍＝非临时那版，三格引用已接回本轮）：
`…R22.jsonl：43 条 + meta，证据读数现取钉住 45 条`、
`…R22-protocol.jsonl：41 条 + meta，钉住 30 条`；按形状复算 =
**R22 字典式 45 / `must_say` 45 / 字符串式 0**，**协议台账字典式 44 / `must_say` 78 / 字符串式 0**。
第一遍（`R22_PROVISIONAL=1`）的对应读数是 `钉住 33 条 / must_say 79`——差的这一条不是承诺变了，是
**引用面换了文件**：上一轮那三份（`R22-protocol/{gate-in-shared-clone,gate-r22-protocol,merge-gate-full}.log`）
现取到 5 条判定行，本轮这三份（`R22-closefinal/` 同名件）现取到 4 条（`closeout-r22-protocol.log` 里
只有一条 `=====` 判定行，旧那份有两条）。逐文件复算的分配（终版那 78 条）：`selftest-closeout` 52 /
`closeout-r22` 11 / `gate-family-selftests` 7 / `gate-teeth-battery` 2 / `merge-gate-full` 2 /
`closeout-r22-protocol` 1 / `gate-in-shared-clone` 1 / `R23dedup/*` 2 ＝ 78。
"存在性引用"这一族在两份台账里都归零了。

这一改顺手证了它自己有用：文档定稿后先跑一次闭合门，两朵红当场出现，都不是门歪而是账旧——
`❌ 节「9. 二次审核协议落地实证…」承诺总数实抽 79 条，声明 66 条`（我这一轮往 §9 里加了 §五 一整段），
`❌ 第 39 行 id=P-37：claim.quote 在 '8. 验收判据（本轮什么算完）' 一节的任一条目里都找不到连续原文`
（§8 第 2 条的"57 种"改成"59 种"，旧引文就断了）。重出两份台账后两朵红各自消失，
`closeout-r22` 与 `closeout-r22-protocol` 均印出判定行。

#### 4. 反向自检末次读数（三份常驻电池，全在 `logs/R22-closefinal/`）

| 件 | 末次读数 | 上一轮存档 |
|---|---|---|
| `check-review-closeout.py --selftest` | `===== selftest：59/59 种坏形态各自隔离地红，合法快照 0 红 =====`，`SELFTEST_RC=0`；格数由 `--selftest` 自己现印（`len` 算出来的，台账与本文都不抄它当判据） | `selftest-closeout-run1-57cells.log`（57 格那一版）＋更早的 `selftest-closeout-run1.log`＋手写条件行那一版 `selftest-closeout-run2-handwritten-conditions.log`（第 7 段缺陷 ④） |
| `reverse-test-gate-teeth.py` | `===== 门牙反向测：22/22 刀各自点名到该开火的那一格 =====`，`rc=0`（首行 `### 门禁判据反向测（22 刀…）` 与末行分子分母都由脚本现算，编号＝`[刀 N]` 的打印序） | `gate-teeth-battery-run1-17cells.log`、`gate-teeth-battery-run2-19cells.log`、手写表头那一版 `gate-teeth-battery-run3-handwritten-header.log` |
| ↳ 摘"判读域两条界"的那两刀＝**第 21/22 刀** | 摘掉只回头部的 `log_window`、摘掉判读前脱色的 `body` ⇒ 对应那两格立刻"没红"，且汇总行同时掉成 `58/59`＋合法快照非 0 红＝**一处失效会两头红** | 同件日志内逐刀明细 |
| ↳ 摘"取证自己"的那三刀＝**第 10/11/12 刀** | 第 10 刀摘克隆门"判决行要进自己写的那份取证"，第 11/12 刀摘汇编件的"成员零输出要红"与"成员退非 0 要红" | 同上 |
| 四份门禁件自检族（生产方 `run-gate-family-selftests.py`，本文件由它整体覆写） | merge-gate `16/16`、克隆门 `11/11`、anchor-preflight `10/10`、**汇编件自己** `4/4`，汇总行 `===== 门禁族自检：4/4 份门禁件各自交出非空反向测读数 =====`；全表 `0` 个 `✗`、`0` 个"判错" | `gate-family-selftests-run1.log`、`-run2-pre-cloneselftest.log`、不含自己的那一版 `-run3-pre-selfmember.log` |

成员数与份数都不再手抄：名单是 `MEMBERS`，汇总行的分母与"每位成员都要在取证里现形"那格的断言都取
`len(MEMBERS)`——上一版那里写死 `== 3`，加第四位成员时它自己就成了第 7 段缺陷 ⑤ 的那一格。

新增的两格（`禁串只在尾部`、`禁串被 ANSI 遮挡`）各配一个合法快照正例（`ST-WINDOW` 钉一条只在尾巴上的
读数、`ST-ANSI` 钉一条被色码包住的串）——只有坏形状的话，"窗口放宽/判读前脱色"这个修法本身没被测过。

上面那四份读数在 §五 记完末次读数之后整族复跑过一次（16:37，`python3 scripts/run-gate-family-selftests.py
--round R22-closefinal`）：新产物与 13:40 那份的差异**只有首行的测量时刻**（`diff` 明细 1 处），
`merge-gate --selftest` 仍 `16/16`、克隆门仍 `11/11`、anchor-preflight 仍 `10/10`、汇编件仍 `4/4`。
`check-review-closeout.py --selftest` 也单跑过一次，仍 `59/59`、`rc=0`（与仓内 13:40 那份差 2 行＝时刻行
与命令行，判定行同值）。这一趟不算新的取证轮次，只确认"记末次读数"没把被测字节换掉——第 5 趟那朵红正是
"绿了以后字节又动"，所以每次记完都要复算一遍，哪怕复算出来的结论是"没变"。

#### 5. 末次全量跑与诊断步

末次全量跑的判定行**不抄进这段散文**——抄一次就多一处会过期、且没人能保证它和磁盘上那份一致。
它由台账那一侧钉住：`merge-gate-full.log` 的开头两行（跑的是哪条命令、名单里几步）是 `must_say`，
`红在：` 与 `--only` 是 `must_not_say`。要读结论直接开
`docs/superpowers/specs/ledger/logs/R22-closefinal/merge-gate-full.log` 末行。

诊断步 `ci-step-coverage`（`DIAG` 里唯一不门控的一道，读的是 GitHub 远端 run 历史）第 1 趟实测：
统计步骤 186 个 / 作业 23 个，`NEVER_RUN` 0、`NEVER_RUN_JOB` 0、`ALWAYS_RED` **3** 条——
`Lint / ESLint (user-web 主应用)`（执行 7 次，次次失败）、`user-server-ci / ESLint (user-web)`（2 次）、
`user-server-ci / Unit tests -race (user-server service)`（2 次）；另 1 条 `LICENSE Compliance Scan`
判为节奏门不计红。比 `2026-09-21-offline-deployment-design.md:1018` 记的 4 条少一条（markdownlint
那条已不在"次次失败"的集合里）。这三条全是 CI 侧既有失效面，与本批无因果，**登记不代改**
（口径：不替别人把红读成自己的）。

第 8 趟同一道诊断步的读数已经变了，按"读数必附出处"记在这里（取证 `merge-run8/ci-step-coverage.log:266`
起逐条列名，`merge/` 那份会被下一次跑刷掉，故引存档那份）：统计步骤 **208 个 / 作业 26 个**、
`NEVER_RUN` 0、`NEVER_RUN_JOB` 0、`ALWAYS_RED` **5 条**。多出的两条是 `user-server-ci / Unit tests -race
(user-server core)` 与 `user-server-ci / Coverage (user-server)`；原有三条名单不变，只有
`Lint / ESLint (user-web 主应用)` 的"执行次数"从 7 次读成 5 次——样本窗口随远端 run 历史移动，
这两处差异都不是本仓改动造成的。新增两条的红因也不必我去猜：并行泳道在
`docs/superpowers/plans/2026-09-20-coverage-heavy-low-packages.md:4380-4388` 拿一趟远端日志（run
`35765306304`、head `5b92525c`）逐朵归因过——②是对方在编的外部单号那条腿
（`integration_test.go:824`），④是被"包未产生结果／分片判据失效"两条完整性断言连带打红，
③才是 `sorry, too many clients already (SQLSTATE 53300)` 那朵容量红。CI 侧 postgres 容量那一刀
"仍不代做"的结论在同文件 `:4389-4391` 已写成两条实测否证（含 `git apply --cached` 绕法为何不成立）
⇒ 本泳道同样登记不代改，且这一条不是"我没做的待办"，是别人已拍板不做的决断。

#### 6. 第 3 趟全量跑那朵红是本泳道自己的：b21 夹具竞态（已修，抽中率是量出来的）

`go-test-browser-automation` 那一朵红落在批21 那条"写失败≠未上线"的用例上，红因是夹具自设的前置断言
（`host_offline_b21_test.go` 里 `夹具必须命中 writeJSON 失败分支` 那句 `t.Fatalf`），报错原文带着
"命令未上线"——**不是被测判据坏，是夹具没造出它要判的那一格**：

- 原写法 `conn.conn.Close()` 只关 socket，但那条评论里"注册表面依旧是「有连接」"并不成立：关闭会叫醒
  这条连接的 `readLoop`，它 `defer c.close()` → `unregister`，于是 `Request` 有概率先走到 `GetConn`
  取不到那一支（未上线），而不是被要求的写失败那一支。批21 当时抽不中（`-count=1` 绿），本轮抽中——
  实测**默认调度下 15/500、36/1000**（≈3%），一次全量跑就是一次抽签。
- 第一版修法（`Register` 之后再造一个 `HostConn` 占住同一个 key，靠 `unregister` 的指针相等判据让老连接
  跳过）**把竞态换成了另一个更贵的失效**：`Register` 起的 `probeServable` 与测试侧各持自己那份 `writeMu`，
  锁保护不了共用的那个 `*websocket.Conn` ⇒ gorilla `panic: concurrent write to websocket connection`，
  实测 1000 次抽中 1 次，且一条用例 panic 会带走整个二进制（那一趟只跑出 88 条 PASS）。
- 终版不 `Register`：`newB21Host` 多一个 `register` 开关，这一格手造一条**没有 goroutine** 的连接直接登记
  进表——没人 `unregister`、也没人并发写，`Request` 必定且只走到 `writeJSON`。

验证（取证目录 `logs/R22-closefinal/`，逐份读数见该目录 `README-b21-writefailure.txt`）：终版
`-count=2000 -v` **2000 PASS / 0 FAIL / 0 panic**；反向一格＝把 `host_registry.go` 写失败分支改成并
`ErrCommandNeverOnWire`，本条用例被后两句断言各杀一次、另三条 b21 用例仍绿（红因特异）；注码文件 `cp`
备份还原后 md5 与备份同值（`a5d732da…`）并复跑 `-count=50` 绿。

顺带清掉两条自己的假账：① 先前记的"`-cpu=1 -count=1` 确定性复现"不成立——zsh 不对未加引号的 `$spec`
拆词，整串被当成一个参数喂给 `-cpu`，那一趟是 `testing: invalid value … for -test.cpu`、rc=1 却一行
`--- FAIL` 都没有（**rc 非 0 是没跑过，不是红**），产物已从取证目录删掉；② 修之前存的那份日志被命名为
"red-before"而内容是一句 `ok 0.351s`——证据文件名不许携带结论，已按"参数＋轮次"重命名，读数一律从文件
里现数。

本节改动的落盘件 md5 现在为 `host_offline_b21_test.go cbc04495…`、`host_registry.go a5d732da…`；
批21 那格记的 `59c28caf…` 是批21 收口时那份的快照，已被本节取代（读它只能读成"当时那份"）。


#### 7. 取证链自己的五处缺陷（这一段的判据全部来自"读那份取证时读不通"）

前六段管的是"产码有没有被证着"。这一段是收口最后抓到的另一类：**取证链自己是缺陷源**——
文件在、mtime 新、判定行也对，可它证的不是它声称证的那件事。五处逐条列，每处都给"怎么发现的 /
现状 / 谁在拦它不再犯"：

| 缺陷 | 怎么发现的 | 现状 | 不再犯的拦手 |
|---|---|---|---|
| ① 克隆门的判决行不进它自己写的那份取证 | 抽查 `gate-in-shared-clone.log`：文件末尾是汇总，脚本 stdout 的 `===== 克隆面：… =====` 落在别处，读它的人只能信"它跑过" | `run-gate-in-clone.py` 现在由脚本把判决行写进自己那份取证；旧那版另存 `gate-in-shared-clone-run-pre-verdictfix.log` | 克隆门 `--selftest` 里两格（绿侧、红侧各一格：判决行必须出现在脚本自己写的文件里）＋门牙电池**第 10 刀**摘这两条判据 |
| ② 台账引用了一页没有生产方的取证 | 把 `gate-family-selftests.log` 的生成方式找回去，找到的不是仓内脚本而是 `/tmp` 里的一段 shell | 新增仓内生产方 `scripts/run-gate-family-selftests.py`，该文件由它整体覆写（表头第二行 `### 生产方 scripts/run-gate-family-selftests.py（本文件由它整体覆写）；…` 就是它自印的） | 合并门禁新增 `family-selftests-selftest` 步（缺陷 ③ 那一族），生产方自己的 4 格反向测见 §五 第 4 段末行 |
| ③ 门禁件的反向测有两句"没有执行者" | 逐个 `--selftest` 对步骤名单：四道自检里 `clone-selftest`、`family-selftests-selftest` **根本不在名单里**——判据写好了却没有任何一步真跑过它，改坏它不惊动任何门 | 两步注册进 `STEPS`，判据形状 `--selftest：([1-9]\d*)/\1 格` ＋ `nore "判错 ✗"`，步骤名单 35→37（第 3 段那条引用它的 `must_say` 随之变长） | 这两步本身；以及合并门禁的 `ci-step-coverage` 诊断步会把"名单里存在却从未执行"的步报出来 |
| ④ 取证文件里有**手写**的条件行 | 拿第 4 段的读数和文件正文对：门牙电池表头手写"第 20/21/22 刀"，而正文实际打印 `[刀 10] [刀 11] [刀 12]`（KNIVES 的声明序＝打印序）；`selftest-closeout.log` 顶部手抄的那句条件绑的是**台账版本**，这一趟跑根本测不到它 | 三份常驻电池的条件行改由生产脚本自印（第一行 `###` 带 `len()` 现算的刀数/份数与测量时刻，门牙那份另起一行列五件 md5），旧两版另存 `gate-teeth-battery-run3-handwritten-header.log`、`selftest-closeout-run2-handwritten-conditions.log` | **口径（本轮新增）：取证文件里只允许出现生产脚本自己打印的行**；条件、归属、"这一读数在什么前提下成立"一律写在 spec 里。手写数字必然漂，漂了没人看得见——它与协议 §3 判据 3（"内容仍是这条承诺的读数"）是同一族 |
| ⑤ 汇编件不测自己 | 为缺陷 ② 补生产方时，按"每份带 `--selftest` 的门禁件都要在取证里现形"复核那页：汇编件自己有 4 格反向测，而它产出的取证里**没有它自己那一格** | 它自己列为第 4 位成员；汇总行 `4/4 份门禁件`，旧那版另存 `gate-family-selftests-run3-pre-selfmember.log` | 成员名单 `MEMBERS` 是唯一事实源：汇总行的分母与"每位成员都要在取证里现形"那格的断言都取 `len(MEMBERS)`，写死的 `== 3` 已删——写死的那个数字本身就是这一处缺陷 |

①②③ 是"证据没有生产者／没有执行者"，④⑤ 是"生产者说的话不是它跑出来的"。五处的共同形状：
**取证文件的可读面比它的生产面大**——文件里可以出现没人跑过的句子。所以处置都不是"把这行订正掉"
而是"让这类句子不可能再出现"：①②⑤ 把断言搬回生产脚本、③ 把执行者注册进门、④ 把条件行的
出处钉死为脚本 stdout。旧读数一律**另存**而不是原地改写：被覆盖的取证和手写条件行是同一件事。

**③ 的边界要划清楚，别读成"所有反向件都进门了"**：`reverse-test-gate-teeth.py` 是这套件里唯一
**故意不注册成步**的一份——它的工作方式就是把注码写进这五份门禁件的源码再还原（`cp` 备份＋逐刀
md5 复核），共享树上并排跑别的步就会读到它正在注的那份坏字节（并且它若被中途 kill，残迹留在
被注码的树上）。所以它仍是手跑的常驻电池，防"它的取证过期"的抓手是它自印的表头：
`### 注码前被测件 md5：` 那五行。**复核配方**（一条命令，任何人可跑）：

```
$ cd hivemtk && python3 - <<'PY'
import hashlib, pathlib, re
txt = pathlib.Path("docs/superpowers/specs/ledger/logs/R22-closefinal/gate-teeth-battery.log").read_text()
head = next(l for l in txt.splitlines() if l.startswith("### 注码前被测件 md5"))
bad = [f"{n} 取证={m} 现值={hashlib.md5(pathlib.Path('scripts/'+n).read_bytes()).hexdigest()}"
       for n, m in re.findall(r"([\w.-]+\.py)=([0-9a-f]{32})", head)
       if m != hashlib.md5(pathlib.Path("scripts/" + n).read_bytes()).hexdigest()]
print(bad or "五件 md5 与取证表头逐字一致")
PY
```

这一族也有一次自指：本轮把 ④ 的口径立起来之后，那三份常驻电池的取证全部重跑过，
第 4 段的读数、协议 §3 的那条 bullet 与这里的五处叙述都是**重跑之后**按磁盘现读写的。

### 六、越界改动清单（我动了你哪一行）

协议 §6 立项时记的"越界三条红"，本轮对着树逐条复量。**立项那句"只剩一条是真的"到收口时点
也不再是真的**——红会再生，所以判定看末次读数而不是立项读数：

| 立项登记的越界红 | 复量读数 | 终态 |
|---|---|---|
| markdownlint"两处 MD004 + 一处 MD031" | 真红只有**一处** MD004（`CHANNEL_INTEGRATION_AUDIT_2026-09.md:810` 的 `  + ` 续行）；`.markdownlint.json` 里 **MD031 = false**，被引用的另两个行号抽出来是空行和 `-` | 已用仓里那版 `markdownlint-cli2`（v0.23.3 / markdownlint 0.41.1，走 `.markdownlint-cli2.jsonc` 的 glob，实测覆盖 170 个文件）复跑到 `0 issues` |
| `make fmt-check` 点名 `dingtalk_media.go` 与 `wechat_batchf4_m01_inbound_test.go` | 这两处由并行 lane 的 `257dfe5e` 随产码一起格式化了（**不是我修的**，读数被更新的读数取代）；但收口时点全树 `gofmt -l` 又点出**新的两处**：`internal/repository/bill.go`（104 行多一个空行，mtime 03:21，不在飞）与 `internal/service/collection_job_test.go`（`fakeCollectionScanner` 两行字段对齐，mtime 05:09，**在飞**） | 两处均按 §5 手法用 `Edit` 精确串改到 `gofmt -l` 全树空；在飞那处先留 `diff` 快照：`logs/R22-lanes/overlane-collection_job_test-gofmt-before.txt`（开工 md5 `0d540611…` → 改后 `ef5a14c7…`） |
| `check-architecture.sh`（`dingtalk_media.go:191,204` service 直连 `s.db`） | 该文件现在只剩 `s.hubRepo == nil` 守卫（`:202`）与 `s.hubRepo.SetInboundMediaURLs`（`:206`），两处直连已随批23 的回填下沉消失 | 复量为绿；最终判定仍归 §五 的合并门禁那一次实跑 |

本轮**保留**的越界改动（不还原、随本轮报告交给并行 lane）：

- `user-server/internal/pkg/db/migrate.go` +13 行：注册 `&browsermodel.BrowserAuditDigest{}`（`:383`）、
  `&browsermodel.BrowserAuditPruneRun{}`（`:384`）、`&browserWriteClaim` → `&browsermodel.BrowserWriteClaim{}`
  （`:397`），各带 4 行"为什么"注释；另把"与批6 的 `browser_steps` 两列同口径"那句改成显式区分
  **新表**走标签直建 / **存量表**加列走版本化迁移，免得有人拿这句话去省 DDL 文件。
- `user-server/internal/service/webhook_batchf2_n14_repeat_dedup_test.go:114-117`：**纯注释**，
  在并行 lane 已提交的测试文件里（`1af6d28c`）。旧文案"钉钉入站事件不带 channel_msg_id"与
  `dingtalk_app.go:177` 现状相反，留着会把那条腿读成反的；改后指名映射来源与新腿文件。
  这一处是 C 相子代理"顺手发现、按边界没动"的（`report-456671c1efa.md` 末段），由我按 §5 改掉。
- `.gitignore:248-254`：`!docs/superpowers/specs/ledger/**` 反选放在**文件末尾**，
  理由与实测代价写在注释里（`logs/`(23) 连目录吞、`*.log`(24) 与 `*.jsonl`(221) 再吞一遍，
  git 不进被忽略目录 ⇒ 中途一条 `!…/ledger/` 救不了里面的文件）。
  实测：`git status --porcelain -uall docs/…/ledger/` 现能列出 316 个未跟踪取证文件。
- **未做**：没动任何并行 lane 的期望或判据；没有 `git stash` / `checkout` / `restore`；
  本泳道全程不 commit（改动先不提交，等真机回归）。

### 七、本轮没跑的 / 仍然开着的

- **本机 Xcode 许可未接受**（§五 第 2 段）：默认 developer dir 下 `go build ./...` / `go vet` 红在
  `runtime/cgo: You have not agreed to the Xcode license agreements`。`blocked`，`owner`＝用户，
  `next`＝`sudo xcodebuild -license accept`（或 `sudo xcode-select -s /Library/Developer/CommandLineTools`），
  `due` 2026-09-30。本轮末次全量跑是在 `DEVELOPER_DIR=…/CommandLineTools` 下出的读数，
  **这不等于那条红被修掉了**——它只说明本轮取证没被环境卡住。
- **并行 lane 的树状态：两处报了不动**（本轮立项时记的那 18 个"已 `git add` 未 commit"文件
  现已不复存在——`internal/service/collection_job.go` 随 `23dae260` / `c730c432` 进了 HEAD，
  `git diff --cached` 现为空，那条"干净克隆里看不见"的洞自愈，不用我代报第二遍）。剩下的：
  `scripts/bulk_seed_industries.py` 在工作树里是删除态（` D`）而实体挪到了
  `scripts/seed/bulk_seed_industries.py`（mtime 10:43，另一路在飞），
  但 `docs/superpowers/specs/2026-09-21-offline-deployment-design.md:622`（"两份同内容副本"）与
  `docs/superpowers/plans/2026-09-21-offline-deployment.md:803` 还把**两个路径并列**当作现状——
  旧路径这一份在工作树里已经没有了。归属清楚（离线化那批），
  本轮**不代改**别人的文档与文件位置。
- `#28` 伪造平台域名的桥接真机腿：`blocked`，`owner`＝用户（要宿主与设备），`due` 2026-10-31，
  `next`＝接上宿主后跑 `scripts/e2e_browser_real.py` 的伪造域名档。它**不许**被写成"已覆盖"。
- 未提交批次的 commit/push：`blocked`，`owner`＝并行 task #59 与用户真机回归。
- `sendVerifyMs` 在慢机型上的摆动（§7.30 §十）：`blocked`，同上。
- A12 的"每用户 >1 session / 多副本 worker"触发条件仍未放开，部分唯一索引已兜住当下。
- 四十条 memory 口径：**本轮真的咬过一次的那些已下沉进门或电池断言**（§3 判据 8 的 96 字指针、
  门牙反向测、`reverse-test-l4-baseline.sh`、克隆装架口径、`lane_overlays` 只排 `_test.go`），
  其余仍是**"无执行者"的人读口径**——列在这里，不假装全自动化了。
- 曾登记为"最后一行"的两支电池（`mut_retention_a6.py` / `mut_command_log_ok_b20b.py`）**已 discharge**：
  全量读数在 §四 表（`28 格逐格被杀`，收口轮把 lane 3 那 27 格同趟复跑 / `8/8 格`），两趟都在合并门禁之前跑完，没有提前引用。
- 上一版登记在"仍然没跑的最后一行"的那一格——`command_log.go:220` 的
  `if res.RowsAffected != w.RowCount { return fmt.Errorf("…撤销本批") }`（摘要声明行数与实删不符即回滚）——
  **本轮已建格并跑掉**（电池格号 D23）。它的**第一版夹具被实测否掉**，形状与否决证据都留了痕：想让
  **同一条 DELETE** 少删一行，最直接的是 BEFORE DELETE 触发器在删掉首行时把末行的 `created_at` 推回未到期，
  实跑却红在**未注码的控制组**上，红因不是守卫而是数据库——
  `撤销原因不含 "摘要声明 3 行、实删 2 行"：ERROR: tuple to be updated was already modified by an
  operation triggered by the current command (SQLSTATE 27000)`（取证
  `logs/R22-lanes/mut_retention_a6-d23-cell-r22close.log`，它记的是**被否掉的形状**，故保留不覆盖）。
  即 PG 不许一条命令改动"已被本命令触发的操作改过"的元组 ⇒ 同一条语句内分家根本走不到行数比较那一步。
  第二版把分家挪到**同一事务的两条语句之间**（窗口 `ORDER BY session_id`、循环逐 session 先落摘要再删行）：
  actor 会话那次 DELETE 的 AFTER DELETE 触发器，把**另一个**会话里一条未到期、id 落在其区间中间且
  ≤ 入口高水位的行补写成超期 ⇒ victim 的摘要仍是"进事务之前"那次扫描的 2 行、它的 DELETE 实删 3 行
  ⇒ 守卫开火、整事务回滚（两条摘要与四行数据全回去，run 行不写）。新腿
  `TestA6PruneRollsBackWhenDigestUnderstatesDeletedRows`（`retention_a6_test.go:857`，注释里把 27000
  那笔实测一起写进去了），注法用 `cond && false`（`mut_retention_a6.py:232,415`）而不是删整块——
  `fmt.` 在 `command_log.go` 里只这一处用到，删块会让克隆编译不过（BROKEN 不算杀）。
  单格取证（`--cells D23`，日志 `logs/R22-lanes/mut_retention_a6-d23cell2-r22close.log`）：控制组现测
  `rc=0 total=30 passed=30 skip=0 FAIL=[]`（30＝加这条新腿后的真读数，不是写死常量），注码下
  `杀掉 total=30 fail=1 skip=0 ｜ TestA6PruneRollsBackWhenDigestUnderstatesDeletedRows`、末行"已全量还原（md5 一致）"。
  它与 D20 那条"分家发生在**批之间**"的腿（`TestA6RunRowCountsEntryWindowNotLateCrossing`）各钉一句、不重叠：
  那条管 rows_before 记的是入口那一瞬，这条管描述失实的那一批不许留凭据。整电池 28 格回跑读数见 §五 表末行。
