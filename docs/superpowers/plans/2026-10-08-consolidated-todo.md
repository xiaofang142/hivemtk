# 全项目未完成功能/计划 · 汇总 TODO 清单（2026-10-08）

> 来源：①`docs/superpowers/plans/2026-09-30-outreach-p0-backlog.md`（自动触达 P0，进行中）
> ②`docs/replan-2026-09/新规划任务清单.md`（P0–P9 共 56 卡，T-P9-03/04 未收口）
> ③`docs/governance/78-OPTIMIZATION-TASKS.md`（86 条，未完成 11 + 部分完成 5）
> ④`docs/architecture/MASTER_FEATURE_INVENTORY.md`（实测缺口列）
> ⑤`.sisyphus/cycle_state.json`（known_issues 遗留）
> 逐项已用 grep/源码核实，非文档转述。

---

## A. 自动触达 P0（browser_automation × JEV）— 代码级缺口，本次全部收口

前置：`docs/superpowers/specs/2026-09-30-outreach-product-design.md`

### A1 Chunk3 剩余：触达去重接线 ✅ **本轮完成**
- [x] A1-1 `executeStepWithRetry` 加 `pageURL` 尾参（3 调用点：653 显式 `""`、926 brain 传 pageURL、test57 `""`）
- [x] A1-2 `dispatchStep` 加 `outreachCtx` 尾参（唯一调用方 1116）
- [x] A1-3 写步去重检查点：`writeKey` 之后、`guardResubmit` 之前，`resolveOutreachDedupe` 命中 → `finishStep skipped` + 审计帧绿返
- [x] A1-4 `verified` 分支插 `recordOutreachDedupeSend`（warn-only，不判红）
- [x] A1-5 迁移 `v3_50_0_browser_outreach_dedupe_migration.go`：建 `browser_outreach_dedupe`（四元组唯一键）+ 测试 + `initial_schema.go` 注册（现行最新 v3.49.0）
- [x] A1-6 routes 装配：`WithDB` + `SetOutreachDedupeRepository`

### A2 Chunk4：活动预算 + 时间线可观测 ✅ **本轮完成**
- [x] A2-1 task 模型加活动预算字段（现 `model/task.go` 只有 D7 确认/执行两条时间预算，无活动级 token/成本预算）
- [x] A2-2 dispatcher 调用点预算扣减（超预算 → 优雅停，非 panic）
- [x] A2-3 审计帧每步补 model / token / latency 三字段

### A3 Chunk5：触达回执留存 ✅ **本轮完成**
- [x] A3-1 DONE 收口：截图 + 帖子链接 + 文案快照入库
- [x] A3-2 前端：`user-web/src/views/browserAutomation/Detail.vue` 展示回执

---

## B. 死代码 / 未接线资产（已逐个 grep 核实）

| 项 | 实测 | 处置 |
|---|---|---|
| `NewAIAgentController` | defs=1，非测试调用 0，测试引用 0 | 删除 |
| `NewChannelAgentBindingController` | defs=1，调用 0/0 | 删除 |
| `NewCustomerServiceAgentController` | defs=1，调用 0/0（`*WithService` 在用） | 删除 |
| `NewCustomerServiceControllerWithService` | defs=1，调用 0/0（普通版在 `service_routes.go:74`） | 删除 |
| `NewPromptControllerWithService` | defs=1，调用 0/0（普通版在 `business_routes.go:324`） | 删除 |
| `NewUserController` | defs=1，非测试调用 0，测试引用 18 | 评估：测试自造，评估删或接线 |
| `NewChatWSHub` | defs=1，非测试调用 0，测试引用 18 | 同上 |
| `NewDashboardSSEController` | defs=1，非测试调用 0，测试引用 11；**3 个 handler（StreamEventStream/Snapshot/Metrics）无 `RegisterRoutes`，运行时 404** | **接线**（真实未接线资产） |
| `internal/aiagent/rag/customer_service/quality_assessor.go` `GetQualityMetrics` | 全 0.0 死桩，零调用方 | 删接口或接线 |
| `email_tracking.go` `ClickRedirect` 注释「取 query url」 | 实现只读 DB target，注释漂移 | 改注释 |

### B-对账（2026-10-11 现测，原表行不划掉，只登记实际落点）

上表 10 行里 **7 行已在 `7256777d`（2026-10-08）/ `516405fa`（2026-10-08）收口**，3 行按下面口径处理：

| 项 | 2026-10-11 现测 | 结论 |
|---|---|---|
| 5 个「删除」项（`NewAIAgentController` / `NewChannelAgentBindingController` / `NewCustomerServiceAgentController` / `NewCustomerServiceControllerWithService` / `NewPromptControllerWithService`） | 全树 Go 源码 grep 裸名：**零命中**。在用的是 `…WithService` 三胞胎（`router.go:708/:712/:715` 构造，定义在 `ai_agent.go:22/:457/:597`）与普通版（`NewCustomerServiceController`、`NewPromptController`）。普通版今天的位置：`service_routes.go:74` 与 `business_routes.go:342`（上表写的 `:324` 是 2026-10-08 的行号，此后该文件有别的注册在它前面） | ✅ 已删，`git log --all -S'func NewAIAgentController('` 命中 `7256777d` |
| `NewDashboardSSEController` | 已接线：`service_routes.go:262 setupSSEDashboardRoutes` → `:273` 构造 → `:274-276` 注册三个 GET。今天的路由表里 `GET /api/dashboards/{stream,snapshot,metrics}` 三行都在（在库事实源 `docs/superpowers/specs/ledger/logs/RouteTriage/20261011-030703/live_routes.tsv:388/:390/:391`，由 `user-server/internal/router/route_surface_dump_test.go` 现装配导出；此前引用的 `/tmp/r80-routes.tsv:387/:389/:390` 是同一路由表的无表头版，行号差 1 即表头那一行），消费档位 **weak**（无精确消费方，靠前缀匹配；名册行 `bucket-weak.tsv:37-39`，可单条否证：`python3 scripts/route-consumer-triage.py --routes <同上> --why "GET /api/dashboards/stream"`）。接线本身有常驻守护 `dashboard_sse_wiring_test.go:41` 断言源码里存在该构造 | ✅ 已接线（`7256777d`）；三行仍是 weak，属「有路由、无客户端」——不是僵尸候选，是待前端接入 |
| `GetQualityMetrics` | 全树零命中 | ✅ 已删（`516405fa`） |
| `ClickRedirect` 注释 | `email_tracking.go:69-76` 已改成不漂移的版本，并写明**为什么不能**接受 `?url=`（开放重定向出口），路由在 `:183` | ✅ 已改（附带把「缺失时兜底 query」这条路径判为不该存在） |
| `NewUserController`（def `user.go:17`） | 非测试构造点仍为 0，`user_test.go` 里 18 处构造（现测 `grep -c "NewUserController()"` = 18）。`UserController` 的 7 个 handler（`user.go:21/:38/:51/:68/:86/:99/:118`＝GetUserList/GetUser/CreateUser/UpdateUser/DeleteUser/UpdatePassword/Login）在 `internal/router/` 全树无任何注册点 ⇒ 整块是「测试自造的面」 | ⏸ 不删、不接线，理由见下 |
| `NewChatWSHub`（def `chat_ws_hub.go:140`） | 非测试构造点 0，两个测试文件合计 18 处调用（13 + 5）。**类型本身在生产代码里出现**：`chat_ws.go:73` 字段 + `NewChatWSController(hub *ChatWSHub, …)`，而该构造器唯一调用点 `router/ws.go:53` 所在的 `RegisterWSRoutes` **全树零调用方** ⇒ `GET /ws/chat` 从未注册，实测落到 SPA 的 NoRoute 兜底 | ⏸ 不删、不接线，理由见下 |

**为什么不顺手删掉 `UserController` / `ChatWSHub`**：两者的删除面不一样。`ChatWSHub` 的方法（`Run/Stop/Register/Unregister/SendChunk/Broadcast/…`）是 `ChatWSController` 唯一的推送底座，删它等于删 `/ws/chat` 这条**只差一次注册**的路由的全部实现——这是一次产品决策（要不要上线 WS 流式对话），不是死代码清理；`ws.go:10-30` 与 `config_param_seeds.go:415/418/421` 已把这层「参数能改但没人读」的现状写进库内描述，三个 `chat_ws_*` 配置项被明确标注为不生效。`UserController` 则是另一类：它与 `SystemUserController`（def `auth.go:576`，注册在 `auth_routes.go:54`）功能重叠，删它要连带删 18 处用例，且「用户端要不要一套独立于 system_users 的用户 CRUD」同样是产品问题。
两者**都不是本轮可自行决定的「发现即修」**——删了碰 18 枚用例与一条待接线的路由，接线了要补鉴权（`HandleChatWS` 只校验 `session_id/customer_id`，`ws.go:30` 已写明）。故登记为待裁决，判据全文写在工作区根的 `ZOMBIE_API_TRIAGE.md`——**那份文档在仓库之外、不受版本控制**，
只读本仓的读者看不到它；上面两段已把裁决所需的证据就地写全（构造点计数、测试引用数、重叠实现的位置、
接线缺的那一步），本节自成一体。本仓内另有同形引用（规划文档路径 `docs/replan-2026-09/…`：在库 `.md` 里
现测 `git grep -l -F replan-2026-09` 命中 5 个文件，含本文件；与朴素 `grep -rl --include='*.md'` 计数一致），
属"规划面刻意留在工作区"的既有约定，不是本轮新造的缺口，已另记在册。

---

## C. 新规划剩余（`docs/replan-2026-09/新规划任务清单.md`）

P0/P1/P2…P8 共 52 卡均有「执行结果」回灌，仅剩：
- [x] **T-P9-03**（本轮核实：脚本里 `run_ltc` 早已完整落地，L0 总闸快照 → L12 链路事件共 29 条 `check()`，写操作均带【DB】断言，无需新写） `scripts/api_verify_full.py` 新增 `ltc` 子命令：线索→商机→报价→审批→webhook 回款→赢单→复购，每端点【入参】【返回】【预期】【DB】四维
  - **2026-10-09 订正上面那句「无需新写」**：括号里的三个读数在本次实跑中都不成立，且 AC① 的四维实际只有三维。现测 `run_ltc` 函数体 319 行、字面 `check(` 24 处（不是 29），一次实跑打出 38 条判定（HTTP 腿 17 条由 `req()` 内部产生，不在字面计数里）；`【预期】` 这一维在改动前的整轮输出里是 **0 行**——原先只有 docstring 写着三维，读者只能把 `【返回】` 的实际值当预期读。本轮补上 `【预期】` 独立成行（顺序按 CLAUDE.md 的 入参→返回→预期，第一版我放错在 返回 之前）、修掉失败分支 `sorted(want_codes)` 在 int+str 混合集合（L7 `allow_codes=(0, "UNKNOWN_1000")`）上的 `TypeError` 崩溃（两处改 `key=str`），并把复跑前置补齐（`_ltc_cleanup` 把旅程态打回 lost，否则 `Transition` 同阶段幂等短路会让 L11 一个字节都测不到）。改后连跑四轮 rc=0、38/38，`【入参】/【返回】/【预期】` 各 17 行、`【DB】` 13 行。产品侧顺带修掉一个真缺陷：`ListByStage` 只扫本实例 60s 读缓存 ⇒ 跨实例看不见，改为 L2 阶段索引＋权威回读＋排序，配 6 枚用例与五刀常驻电池 `scripts/mut_journey_stage_index_p903.sh`。读数与坐标见 `docs/replan-2026-09/新规划任务清单.md` 的 T-P9-03 执行结果块。
- [x] **T-P9-04** 第六步全量一次性回归（§2 的 12 步）

---

## D. 优化任务清单（`docs/governance/78-OPTIMIZATION-TASKS.md`）

### D1 本次可快速收口
- [x] **OPT-FE-07 残项①** `website` 锁文件 vite `8.1.4` → 与其余 6 个对齐 `8.3.0`
- [x] **OPT-FE-07 残项②** `scripts/check-vite-version.sh` 已建未注册 → 接入 `make audit` / CI
- [x] **OPT-DOC-EXT-4** `.github/workflows/dco.yml` 触发条件 `on: pull_request`，而本项目直推 master ⇒ 永不触发；改 `on: push`
- [x] **OPT-SEC-04 残项** ✅ 2026-10-08 收口轮闭环：`audit_logs` 表在本仓**不存在**（Go/migrations 命中全是别的审计表；测试库 `information_schema` 亦无）⇒ 该条是陈旧文档；顺链查出真实泄漏面 `config_param_audit_logs.old_value/new_value` 明文留痕 config_params 里的 API Key，已用 `internal/pkg/dbencrypt` 在三条写入分支加密、`AuditLogs` 读取出口解密透传，测试 `config_param_audit_encrypt_test.go` 覆盖密文/往返/存量明文三态

### D2 需跨日工程量（本次不吞，登记在册）
- OPT-FE-04 补 i18n 缺失 key（P1）
- OPT-FE-05 抽 `@hivemtk/http` 共享包（P2）
- OPT-FE-06 platform-web 补 i18n（P2）
- OPT-FE-12 user-web PWA 评估（4d，P3）
- OPT-FE-13 a11y 接入（P2）
- OPT-FE-14 首屏 LCP 监控（P2）
- OPT-DOC-04 平台端 Swagger 覆盖 100%（现 7/13 = 54%）
- OPT-DOC-05 用户端 Swagger 覆盖 80%+（现 31/208 = 15%）
- OPT-SEC-06 10w+ chunks RAG 压测（2d）
- OPT-SEC-08 secrets 支持 Vault / KMS（3d）

### D3 文档自身失真（需回写）
- [x] 「🚧 未完成清单（11 项）」表仍列 `OPT-FE-03`，但正文条目标 `[x] ✅ 2026-09-15 闭环` ⇒ 汇总表与正文互相矛盾。
  **本轮已回写**：从该表移除 `OPT-FE-03` 行并把表头计数从 11 改为 10。

---

## E. 功能盘点实测缺口（`docs/architecture/MASTER_FEATURE_INVENTORY.md`）— ✅ 2026-10-08 收口轮逐条对账

| 项 | 原文声称 | 2026-10-08 实测结论 | 处置 |
|---|---|---|---|
| 用户画像推导 | `Tags/Interests/RiskLevel/PreferredTime` **恒空串**；Clue Level 恒 `"warm"` | 四字段**已全部实现**（`customer_360.go:719 enrichUserProfile`：Tags 取 tagRepo 按 Confidence 前 5、Interests 取 aiTagger interest 词条、RiskLevel 取 RFM 流失映射、PreferredTime 取近 30 天消息小时直方图；旁证 `customer_profile_p349_test.go`）；Clue Level **确为真缺口**（两处硬编码 `"warm"`） | 前者改文档（陈旧）；后者改代码：新增 `clueLevelOf(*model.Clue)`（读库，非法/空串兜底 warm），`assembleClueInfo` 与 map 版两处接线 |
| 异议响应编排 | `UseLLM` 字段定义未实现 | **该字段在全仓 Go 代码中零命中**（`grep -rn "UseLLM\|use_llm\|UseLlm"` = 0），既无类型也无列 ⇒ 是陈旧条目而非「未实现」 | 改文档，删掉伪缺口，只留真实项「内存 sort 冗余」 |
| 分渠道验签 | `wechat` secrets 恒空 → 永远失败；抖音泛化 HMAC 与官方口径不同 | 两条均已闭环：wechat 缺 secret 时是 **fail-closed + 可诊断中文错误**（仅 `ALLOW_INSECURE_WEBHOOK=true` 才旁路并记 bypass 日志），`getWechatSecrets` 真实取库（accountID → `GetFirstActiveAccount` 回落）；抖音已换 `verifyDouyinWebhook`，按官方 `sha1(client_secret‖原始body)` 置 `X-Douyin-Signature` | 改文档为已闭环 + 列出 8 渠道各自信签口径 |
| 渠道标准化 | WA 只处理每批第一条其余丢弃；抖音 generic MsgID 含 UnixNano 天然不去重；WeCom 解密遍历所有账号试 key | 三条全部为陈旧：WA 是 `Entry→Changes→Messages` 三层嵌套全量遍历；抖音事件与 generic msgID 均走 `ContentHashMsgID` 内容哈希（`UnixNano` 在仓内只出现在 repository/websocket/app/controller/channelbot 等无关处）；WeCom 实为「按 AgentID/account **精确路由优先**，解密失败才回退全量遍历」 | 改文档为已闭环，WeCom 兜底遍历保留为已知取舍 |
| 消息下发 | `msg_id` 内容哈希致同会话同文案无法重发 | **真缺口**，机理已核到可执行粒度（见下） | 登记待人工裁决，本轮**未擅改** |

### E-1 出站 `msg_id` 缺口：机理与候选修法（本轮未实施，理由在册）

- **现象**：`ContentHashMsgID(channel, conversationID, content)` 的哈希输入只有 `channel|content`，**`conversationID` 形参未被使用**；而唯一索引 `uni_message_hub_platform_msg_conv` 是 `(platform, msg_id, conversation_id)` 三元组；`MessageHubRepository.Create` 是裸 `db.Create` 无 `OnConflict`，`DeliverOutbound` 在 `Create` 失败时直接返错 ⇒ **同会话同文案的第二次出站硬失败**（不同会话同文案因 conversation_id 不同可共存）。
- **不能直接修的约束**：`ContentHashMsgID` 是**跨语言回环去重契约**——`TestContentHashMsgIDCrossLanguageContract` 与前端 `types.js::contentHash` 逐字节锚定（锚值 `mh:00550fed`），`channelgw/protocol.go` 亦要求扩展端按同源算法生成。给哈希加序号会让回环去重全线崩。
- **关键事实**：入站防回环**完全走 SELECT 钩子**（`inbox_ingress_persist.go:261-266` 钩子2 msg_id 精确判等、`:277-290` 钩子2.5 contentHash 判等 + `eventAssertsDistinctMessage` 开关，另有 `GetByPlatformContent` / `GetByPlatformContentNormalized` 两级兜底），**不依赖该 DB 索引**。
- **候选修法**：① 首选 `docs/architecture/MASTER_COMPETITIVE_DECISIONS.md` L205/L301/L336 的 **B-4**：出站 msg_id 改业务幂等键 `bc_{convID}_{ulid()}`，内容哈希仅用于入站去重（要求扩展端把 msg_id 当不透明串，需先核实插件代码无格式假设）；② 备选：唯一索引改部分索引 `WHERE direction='inbound'`（会削弱入站库级兜底强度，`verifyUniqueIndex` 与相关测试需连带调整）。
- **本轮不实施的理由**：这是**安全权衡**（削弱防回环的库级兜底）+ 跨语言契约变更，两者都需人工拍板，不属于「发现即修」可自行决定的范围。

---

## F. 技术调研已决策未开发（`docs/tech-research/DECISIONS.md`）— 2026-10-08 附二逐条对账 ＋ 2026-10-11 附三增量对账（实际共 D01–D23，23 条）

详见 `docs/tech-research/DECISIONS.md` 的「附二：2026-10-08 状态对账」＋「附三：2026-10-11 增量对账」两张表
（含逐条证据与可复跑判据；**下一轮按两张表合并后读，只看附二会读到已闭环项的旧状态**。
该文件与 `docs/architecture/ZOMBIE_API_TRIAGE.md` 同族：**在工作区根、仓库之外，克隆里没有这份文件**，
`find hivemtk -name DECISIONS.md` 零命中 ⇒ 下面这份摘要就是本仓读者能拿到的全部，逐条证据都写了本仓内的可复核坐标）。
口径与附三一致：

- **已闭环 13 条**（文档陈旧）：D01 置信度聚合（`smart_cs_orchestrator.go:1110-1122` Aggregate 优先 + 启发式降级，装配 `app/sales_engine_factory.go:66/:119`）/ D02 `executed_nodes`（`v3_29_0_sop_executed_nodes_migration.go`）/ D03 Saga 补偿（`sop_compensation.go:92` + `sop_dispatcher.go:110/:734`）/ D04 Bandit 回流（`bandit_reward_reflux_cron.go` 等 6 文件）/ D06 Checkpoint（`agent_checkpoint_repo.go` + 接线测试）/ D08 工具失败 `error_code`（主链 `tooluse/tool.go:59`，`agent/runtime/context.go:16` 那份漂移副本已删成类型别名 `= portcontract.ToolResult`，单一事实源在 `portcontract/tool_result.go:13`，`a65059b6`）/ D11 MultiModelVote+selfconsistency / D14 触达频控（`reach_gcra_limiter.go:88` → `reach_send_pipeline.go:249` → `app/reach_tool_wiring.go:34`，生产已接线）/ D15 WS seq+epoch / D16 HashEmbedding / D19 Conformal / D20 转人工条件门 / D22 BG/NBD。
- **部分落地 2 条**：D07 greeting 落的是**一致性门**（ENUM 门第 6 项锁 `IntentXxx` 常量 ↔ `DefaultIntents` 词条 `Type` 双向覆盖，`ce892c31`，CI 执行点 `.github/workflows/enum-consistency.yml:46`），**不是**把 54 处散落引用收进单一实体文件 ⇒ 按决策原文的"单一数据源"仍未闭环；D12 把两个点位（`provider_failover_policy`／`embedding_global`）的读路径从遗留 `system_config_kv` 迁进参数中心，后者带可回滚的一次性迁移 `v3_54_0_…_migration.go`，**"两套 KV 物理合并 + 热重载"未落地**（附二的不吞理由仍适用：破坏性重构，需与 D13 一并排期）。
- **确未落地 8 条**（各带实测证据与不吞理由）：D05 failsafe / D09 state_hash / D10 异议 LLM 兜底 / D13 Casbin / D17 sparse / D18 goldSet / D21 pg_search / D23 意图中间层。

**计数自证**：13 ＋ 2 ＝ 15 条已在本仓代码里核到落点，另 8 条确未落地，合计 23 ＝ 工作区根那份
`DECISIONS.md` 的 `grep -c '^## 决策 D'` 现数（2026-10-11 测于该文件，克隆里跑不出这条——它不入库）；
本摘要的对账判据是"每条都能在**本仓**指到一个文件或一条命令"，指不到的只许写在"确未落地"里并带不吞理由。

---

## 本轮执行范围裁定

本轮**一次做完** = A（3 Chunk 全收口）+ B（死代码删除 + DashboardSSE 接线 + 注释纠偏）
- C（T-P9-03 + T-P9-04）+ D1（4 项快速收口）。

D2 / E / F 登记在册不吞：E 中「分渠道验签」属安全项，与 A 的写步去重共处 browser_automation
热区，同批改动会让回归面失真；D2/F 均为跨日工程，须独立排期而非搭车。

---

## 本轮执行结果（2026-10-08）

### A 自动触达 P0 —— 三个 Chunk 全部收口

**Chunk3 触达去重接线**（`executor.go` + `outreach_dedupe.go` + `v3.50.0`）
- `executeStepWithRetry` / `dispatchStep` 各加尾参；检查点排在 `writeStepKey` 之后、`guardResubmit` 之前
- 命中 ⇒ `finishStep skipped` + `dedupe_hit/outreach_already_sent` 审计帧 + **绿返**（errMsg 空 ⇒ 不计成败、不中断）
- 语义修正一处（原实现的实质缺陷）：`resolveOutreachDedupe` 未命中时原返回 `nil`，导致「键未冻结 ⇒ verified 后无行可落 ⇒ 去重永不起作用」。改为命中与未命中**都**返回冻结的键，只有「压根没查成」才为 nil
- 迁移 `v3.50.0` 建 `browser_outreach_dedupe`（四元组唯一键 `uk_outreach_dedupe`），Down 不删表（去重行是「已触达」唯一持久证据）
- 测试 `outreach_dedupe_wiring_test.go` 5 例：判据打在**库里的步行 + 命令日志**上，不看 helper 返回值

**Chunk4 活动预算 + 时间线可观测**（`campaign_budget.go` + `audit_timeline.go` + `v3.51.0`）
- 口径定案：**「触达活动 = 任务本体」**（cron/loop/retry 的所有 session 累计到同一行 `browser_tasks`），预算因此是任务行上的持久累计额度。Executor 是进程级单例，硬约束「禁内存计数」在这里同样成立
- `BrowserTask` 加 4 字段（`TokenBudget` / `CampaignKey` / `CampaignActBudget` / `CampaignActUsed`，0/空 = 不限）
- 扣减是**一条条件更新语句**判出来的（`WHERE ... AND campaign_act_used < campaign_act_budget` → `RowsAffected`），不是 check-then-act：多副本超发一次 = 不可逆动作真发出去
- 三条 fail-open 各自有具体理由（未配置 / repo 未接线 / DB 报错）：预算的失败形态是多打扰几次，锁死整条触达线的失败形态更大
- `BrainService` 加 `LastPlanModel()`，每步 event 帧补 `llm_model` + `llm_tokens` + `latency_ms`；非 Brain 模式**不写该字段**（空串会被读成「模型名叫空」）
- 测试：service 11 例（含纯函数层与审计元数据层）+ repository 4 例（并发 8 腿抢 3 条预算，判 `granted==3 && used==3`，专门否掉 check-then-act）

**Chunk5 触达回执留存**（`outreach_receipt.go` × 3 层 + `v3.52.0` + 前端）
- **不并进去重表、不复用 `extracted_data`**，三条理由写进 model 头注释：去重表唯一键 + `OnConflict{DoNothing}` 会静默吞插入 ⇒ 「已发出但无回执」与「这步没执行过」不可区分（而回执的唯一用途正是回答这个区分）；去重行是闸门输入、回执行闸门输出；JSONB 追加数组无法按帖子链接/文案检索
- 落点铁律：`captureSessionReceipts` 排在 `cleanupSessionTab` **之前** —— tab 一回收，`captureVisibleTab` 就只能截到用户自己的页面（静默假内容，比没有更坏）
- `verified=false` 的 unattributed **照样落回执**：回执只留绿的等于替平台做了裁决，而双发闸正是因为分不清才把人叫来
- 回执的帖子链接**不挂在去重层是否装配上**（去重键没冻结时自己拍一次只读快照兜底）—— 回执完整性不该被闸门层的接线状态决定
- 读侧 `GET /api/browser-automation/tasks/:id/receipts`，归属校验先过任务再查回执（回执行无 `user_id` 列）
- 测试：service 7 例 + repository 4 例

### B 死代码 / 未接线资产
- 删除 5 个零引用构造函数（`NewAIAgentController` / `NewChannelAgentBindingController` / `NewCustomerServiceAgentController` / `NewCustomerServiceControllerWithService` / `NewPromptControllerWithService`）；`*WithService` 版本在用，保留
- **接线 `DashboardSSEController`**：3 个 handler（`StreamEventStream` / `Snapshot` / `Metrics`）此前无任何装配点，实时驾驶舱 SSE 运行时一律 404。加签 `setupSSEDashboardRoutes(auth, gormDB)` + 三条路由；补 `dashboard_sse_wiring_test.go` 判「handler 名 + 具体 URL + 控制器实例化 + statsSvc 注入」四件事都在位
- 未动：`NewUserController` / `NewChatWSHub`（测试引用各 18 处，删构造会让测试编译不过，属另一决策面）

### D1 快速收口
- **OPT-FE-07**：website 锁文件 `8.1.4` → `8.3.0`（现 7/7 pin `^8.1.1`、7/7 lock `8.3.0`）；护栏 `check-vite-version.sh` 注册进 `make audit`
- **OPT-DOC-EXT-4**：`dco.yml` 补 `on: push: branches: [master]`。**阻断强度按事件分档**而非照抄 PR 侧硬阻断 —— 实测本仓现有历史 30 个 commit **零个**带 Signed-off-by，若 push 侧也硬阻断，第一次直推就会把这条线永久锁死；补触发面的目的是让闸跑起来，不是让闸变成没人能过的墙。同时新增 `range` step 统一解析两种事件的 commit 区间（PR 用 base..head，push 用 before..sha 且对「首推 before 全零」「before 被 GC」两种形态各留回落）

### 全量回归读数（T-P9-04）

| 项 | 读数 |
|---|---|
| `go build ./...`（CGO_ENABLED=0） | rc=0 |
| `go vet ./...` | rc=0 |
| `gofmt -l`（全仓 go 文件） | 0 行 —— **顺带修掉 HEAD 上预存的 5 个文件**（`model/profile_health.go`、`geo/service/{citation,opportunity,opportunity_test,search_probe}.go`，纯字段对齐空白，按规则2「发现即修复」当轮清零） |
| browser_automation 全包 / migration / router / controller | 全 ok（service 220s、repository 36s、controller 98s、migrations 23s、router 14s） |
| 前端 `lint:check` | rc=0（0 errors / 18959 warnings，warn 为项目基线，不阻断） |
| 前端 `vitest run` | **30 files / 413 tests 全通过** |
| 前端 `i18n:check` / `check-i18n-coverage.cjs` / `build` | 全部 rc=0 |
| `make audit` 19 道静态闸 | 全 rc=0 —— **期间修掉 2 处预存红灯**（见下） |

**回归期间按规则2 当轮修复的 2 处预存红灯**（均为存量缺陷，非本轮引入）：
1. `check-env-coverage.py` rc=1：6 个生产读取的 env 键未进文档面。**处置方式是补文档而不是登记进基线** —— 基线表自己在头部写明「本表的作用是挡住新增，不是给存量发合格证」，理由一律是「待补文档」。在 `docs/DEPLOYMENT_GUIDE.md` §6.2 补 `BROWSER_JEV_ENABLED` / `BROWSER_JEV_ENDPOINT` / `BROWSER_JEV_API_KEY` / `BROWSER_JEV_MODEL` / `BACKUP_BASE_DIR` / `RESTORE_TMP_DIR` 六行后转绿（读数：生产读取 192 · 已文档化 88 · 红 0）
2. `check-shell-cjk-expansion.sh` rc=1：`scripts/inference-host/start-laya.sh:41` 的 `$LAYA_DIR` 紧跟中文，`$VAR` 后接中文在 bash 3.2 下会按字节吞掉变量名。按闸门给的改法加花括号（`${LAYA_DIR}`，语义不变）后回到基线

### 未吞项（理由在册）
- **OPT-SEC-04 残项**（`audit_logs` 敏感字段加密）：schema 级改造，与 A 段 browser_automation 热区同批改会让回归面失真
- **D2**（OPT-FE-04/05/06/12/13/14、OPT-DOC-04/05、OPT-SEC-06/08）与 **E/F**：跨日工程量，须独立排期
- **E 的分渠道验签**（wechat secrets 恒空 / 抖音 HMAC 口径）：安全项，且与 A 段共处同一热区，本轮刻意不与去重改动同批
- `NewUserController` / `NewChatWSHub`：测试各引用 18 处，删构造会破测试编译，属独立决策面

## 收口轮执行结果（2026-10-08 二次复查 + 文档整理）

### (1) 对上一轮改动的头脑风暴式二次复查 —— 查出并修掉 4 个真问题

先核实了几条「疑似风险」并确认**无需改动**（结论同样写进历史，避免下轮重复调研）：`sessionFinalWriteBudget(120s) > handScreenshotTimeout(60s)` ⇒ 收口截图不会必然假超时；`ExecuteSession` 到收口之间无提前 return ⇒ 正常路径都走到 `captureSessionReceipts`；`hand.screenshot` 缺 `base64` 键时返空串、`e.feedback == nil` 时短路 ⇒ 测试不会真打 LocalDriver；gin v1.12 静态段优先于 `:id` ⇒ `/dashboards/stream` 与既有 `/dashboards/:id` 共存无启动期 panic。

| # | 问题 | 后果 | 修法 |
|---|---|---|---|
| 1 | `stepAuditMeta` 无条件写 `latency_ms`，而去重命中帧 / 预算耗尽帧传 `stepAuditMeta(0)` | 这两帧是**写步在 dispatch 之前就被闸门拦下**，延迟压根没被测量；写 `0` 会被读成「这一步瞬间完成」，与「没测」是两件事，审计面有歧义就会被人拿去做判断 | 新增 `latencyNotMeasured = int64(-1)`，`stepAuditMeta(durMs)` 仅在 `durMs >= 0` 时写该字段，两个调用点改传 `latencyNotMeasured` |
| 2 | 预算耗尽帧 `appendCommandLog(..., *seq, ...)` **漏了 `*seq++`** | 该帧与上一帧共用同一 seq，按 seq 排序时两帧并列，「预算耗尽发生在哪一步之前/之后」无解 | 补 `*seq++` + 注释说明理由 |
| 3 | `pendingReceipts`（`sessionID → []*receiptPending`）只在 `captureSessionReceipts` 里 delete | 会话中途 panic/提前退出 ⇒ entry 永不回收。Executor 是**进程级单例**，泄漏特征是「跑得越久越慢」，事后从日志看不出是哪次会话留下的 | `ExecuteSession` 顶部加 `defer e.discardPendingReceipts(session.ID)`（正常路径 capture 自己摘掉，此 defer 只兜异常路径）+ 新增该方法 |
| 4 | `steps` 存在 sessions 表里可被写入，畸形编排能让单会话 `pendingReceipts` 无限增长 | 内存无界 | 新增 `receiptPendingCap = 200`，超限只 `logger.Errorf` 不挂起（**不阻断触达**：回执是增强层，宁可少留证据也不能让触达线停摆） |

配套测试：新增 `internal/router/dashboard_route_tree_test.go`（9 条 `/dashboards` 路径同形注册，判静态段优先 + 注册不 panic；冲突的表现是**启动期 panic 整个服务起不来**，不是某个 404，必须在注册那一刻判）；`campaign_budget_test.go` 反转 1 条断言并新增 4 用例（seq 唯一性、`latencyNotMeasured` 区分、异常退出清理挂起项且不误删他会话、超限不阻断触达）。

### (2) 整理所有文档 + 汇总剩余未完成计划并执行

| 来源文档 | 处置 |
|---|---|
| `docs/architecture/MASTER_FEATURE_INVENTORY.md` | 5 行（用户画像 / 异议编排 / 分渠道验签 / 渠道标准化 / 出站下发）逐条核实回写；**其中 4 行是陈旧文档**，1 行（出站 msg_id）是真缺口但属安全权衡，已写明候选修法与不实施理由 |
| `docs/tech-research/DECISIONS.md` | 实际 **D01–D23 共 23 条**（非早前记录的 D1–D13）。追加「附二：2026-10-08 状态对账」小节（472 → 524 行）：已闭环 12 / 部分落地 2 / 确未落地 9，逐条给证据文件位置与不吞理由 |
| `docs/governance/78-OPTIMIZATION-TASKS.md` | OPT-SEC-04 残项闭环（`audit_logs` 表不存在 ⇒ 陈旧；真实泄漏面 `config_param_audit_logs` 已用 dbencrypt 加密）；新增「2026-10-08 收口轮」小节为剩余 9 项 + OPT-DOC-04 逐条写不吞理由；执行记录补一行 |
| `docs/architecture/TECH_DEBT_TODOLIST.md` | H1–H5 / M1–M12 / L1–L6 早已全勾，无遗留，未改动 |
| `docs/replan-2026-09/新规划任务清单.md` | 52 卡全部有执行结果回灌，T-P9-03 已核实落地、T-P9-04 已于上一轮完成，无遗留 |
| `.sisyphus/cycle_state.json` | known_issues 逐条对账（`GetQualityMetrics` 已删、`ClickRedirect` 注释已改、`GET /ws/chat` 保持故意不接线并写明理由） |

### (2a) 本轮新增的两处代码修复

- **删 `GetQualityMetrics` 全 0.0 死桩**（`aiagent/rag/customer_service/`）：defs=2（impl + 接口）、calls=0。它返回一组 0.0 且 `err=nil`，任何未来接线的人都会把「没实现」读成「质量分是 0」——留着比删掉更坏。同步删 `interfaces.go` 里的接口方法与 `QualityMetrics` 结构体（`QualityAssessmentInterface` 实际使用点只有 `rag_customer.go:199` 的 `EvaluateResponse`）。
- **`email_tracking.go` ClickRedirect 注释与实现不符**：`controller/email_tracking.go:73` 写「缺失时取 query 参数 url」，实现（`service/email_tracking.go:142`）只返回 `claim.Target`。**改注释而非加实现**——追踪端点一旦接受调用方指定的跳转地址就是任意域名的开放重定向出口，钓鱼链接可直接挂在可信路径下。
- **Clue Level 改读库**（`customer_360.go`）：见上表 E 段。
- **OPT-SEC-04 真实泄漏面闭环**（`internal/repository/config_param.go`）：见上表。

### (2b) 对账方法学（留给下一轮，DECISIONS.md 内同款记录）

1. **命中数只能证伪不能证真**。D14 一度因 `NewRedisGCRARateLimiter` 只有自身 + 测试引用被判「未接线」，打开调用链才发现真正的生产入口是同文件的 `NewGCRARateLimiterFromGlobalCache` ⇒ **判死资产前必须把整条调用链读完，不能只看构造器名**。本轮所有判「已落地」的项都要求确认到生产装配点（构造器 → 工厂/装配函数 → 生产调用方）。
2. **区分「陈旧文档」与「真缺口」**，两类都要改文档，不能只改代码——本轮 MASTER_FEATURE_INVENTORY 的 5 行里有 4 行属前者。
3. **确未落地项必须写不吞理由，且理由要核实到可执行粒度**，不能写「工程量大」这种下轮还得重新调研一遍的话。

## 第四轮收口（2026-10-11 夜间：CI 读数回读 ＋ 路由消费面取证器 ＋ 配置面门的枚举盲区）

### (1) tip 上的 CI 全部转绿（任务「static-gates 的 golangci-lint 红」结案）

`d52dabd5` 这一笔的 run `38075916853`：`status=completed`／`conclusion=success`，
逐作业现读 **13 枚全 success**（`gh api /repos/xiaofang142/hivemtk/actions/runs/38075916853/jobs`，
判据取 `.jobs[].conclusion`——注意该端点的负载键是 `.jobs`，`.workflow_jobs` 会 `cannot iterate over: null`）。
此前挂着的最后两枚 `Unit tests -race (user-server core)` 与 `Coverage (user-server)` 都已回报成功。
`Static gates` 的修复证据在作业日志里：`:662-665` install-only、`:685` 「OK: .golangci.yml 通过 JSON Schema 校验（第 1 次尝试）」、
`:688` `install-mode: none`、`:728` 「golangci-lint found no issues」。
上一笔 `20ed9fbd` 的 run `38072310142` 是 failure（同一枚 lint 步骤），`dac68182`／`d52dabd5` 两笔 success ⇒ 修复方向被两侧读数夹住，不是"这轮碰巧绿"。

### (2) 僵尸接口四档读数做成可重跑的取证器（任务 #12）

原状况：四档读数（1048/135/209/348）出自 `/tmp/r80-consumer-scan2.py` ＋ `/tmp/r80-routes.tsv`——
两件都不在仓库里，`grep -rn r80-consumer-scan hivemtk/` 零命中 ⇒ **任何人换台机器都跑不出来**，
而那份名单是要拿去做删除决策的输入。

- **事实源进库**：`user-server/internal/router/route_surface_dump_test.go`（新增）。只在 `ROUTE_DUMP_FILE` 设了才干活，
  不设就 `t.Skip`——它不是门，只是给分诊器产字节，避免给守卫加第二条装配腿。导出后断言表里必须有 `GET /health`
  （`router.go:200`，`Setup()` 的无条件注册项），空事实源直接 `t.Fatalf`。
  现测：带 env `--- PASS`＋「导出 1740 行路由表」，不带 env `--- SKIP`，`gofmt -l` 空，`go vet` rc=0，
  同一次跑里原有三枚守卫用例仍 PASS。
- **分诊器进库**：`scripts/route-consumer-triage.py`（新增）。三面（client／ops／self-page）＋四档，
  `--self-check` 9 枚控制，`--why "METHOD PATH"` 可把单条路由的判档依据与证据出处摊开，全量跑会先执行控制、
  **任一失败就不写名册**。
- **现测读数**（轮次 `docs/superpowers/specs/ledger/logs/RouteTriage/20261011-030703/`）：
  client 1036 · ops/self-page 278 · weak 205 · none 221，合计 1740＝路由表行数（计数器对不上就退 1）；
  distinct (方法,处理器) 1454 · `/api/` 1721 行。与旧快照的差值是**口径差**（旧版把兄弟仓库 `hivemtk-platform/platform-web/src`、
  `platform-contributor/src` 记进 client、把工作区根 `scripts/`、`cold-start/` 记进 ops；新版只扫本仓库，
  否则 clean clone 跑不出同一份），**逐条归因本轮没做**，所以只报方向与量级。
  事实源内容两侧同一：`diff <(tail -n +2 live_routes.tsv) /tmp/r80-routes.tsv` 空输出、退 0，只差表头一行。
- **确定性**：02:58 与 03:07 两轮独立重跑，五份产物 `diff` 逐字相同（`live_routes.tsv` md5 `f2565b6919405ef65ef2d96aed1c184a`）。
  02:58 那一轮入库前已删除（四档名册与事实源逐字相同、且它不带 `00-provenance.log`，留两族重复产物只会让后来人分不清读哪一份），
  所以这条 diff 是删前的现测、不可再复验；能复验的是"同一棵树连跑两趟仍逐字相同"这一判据本身。
  两轮之间扫描面各多 1 份文件，是并行泳道在期间新建的 `user-web/src/utils/errorReporting.js`（birth 03:03:36）与
  `user-web/tests/unit/error_reporting_console_scope.test.js`（birth 03:06:25）⇒ 面文件数跟树走、档位不跟。
- **入库后在零脏树里复现（这一格才是"可重跑"的判据）**：`git clone --shared` 出自本笔的树，`git status --porcelain` 0 行，
  同一套命令重跑 ⇒ 事实源 `diff` 空、md5 仍 `f2565b6919405ef65ef2d96aed1c184a`；四档名册 `diff` 全空
  （client `d2ee804ef7246615b65a4357a0cd23be`／ops `7713fdff3df83284300075ddcdfa5637`／
  weak `86127985787dda83214c1f65f10cce71`／none `7d1b077cc0e2a623be239644a87430ca`）。
  两边的**扫描面并不相同**：本树带着并行泳道 76 份未提交文件，client 面 687 份／形状 1813 个、ops 面 297 份／1869 个，
  克隆里对应 684／1807 与 294／1863，**面有差而名册零差**（多出来那几份的形状已被别处覆盖）。
  顺带一条同类对账：入库后在主树再跑一趟（`--out` 指到 `/tmp`，不往证据树里落第二轮），四份名册与入库版 `diff` 仍逐字为空。
- **零脏克隆里的整链复跑**：同一棵克隆里 `make audit` ⇒ 日志 `读数: 通过 19 / 违规 0 / 检查对象缺失 0`＋`✅ 19 条判据全部成立`，
  `AUDIT_RC=0`（单独落文件，不取自管道末位），全日志 `❌` 0 处、`FAIL` 0 处；`go test ./internal/router -count=1 -v`
  ⇒ rc=0、`--- PASS` 220 行、无 FAIL/SKIP、`ok 9.922s`（新导出件与原有守卫同编同跑）。
  克隆里没有 `.env`（它不入库），DB 口令须从主树那份现取——这条属环境前提，不是仓库红。
- **本轮查出并修掉取证器自身的缺陷**：初版把三类「写了路径字面量但不是调用方」的文件算进消费面——
  `scripts/mut_*.py`（注码锚点）、取证器自己（文档串示例路径）、`user-server/tests/e2e/probe_result.tsv`（探针**输出**表）。
  是 `--why` 这一格逼出来的：`POST /api/livecode/:id/click` 的精确形状证据原本有 4 条，其中 2 条属此类。
  修法＝非消费方排除表＋第 9 枚控制；注码验证：清空 `NON_CONSUMER_NAMES`/`NON_CONSUMER_PREFIXES` 后
  `--self-check` 退 1 并点名 4 项「该排没排」，还原后 md5 与基线一致。
  **排除前后四档名册 `diff` 逐字未变** ⇒ 这条修复在这一版数据上只改证据出处、不改档位（数据性质，不是保证）。

### (3) 门的产物归属轴自己逮到了这一族（顺带补装架）

03:03 那次 `make audit` 唯一一处红是 `check-battery-identity.py` 的 A5：
「族 `RouteTriage` 在树里有轮次目录，却既无驱动归属、也未登记为非电池」。处理：
取证器改为每轮写 `00-provenance.log` 的 `基线字节` 身份行（HEAD＋取证器与事实源 md5＋控制枚数＋读数＋复现命令），
并在 `NOT_A_BATTERY` 写实登记（静态分诊器没有格子、不注码，驱动轴只 ast 解析 `mut_*.py`，本就不该认领它）。
复跑：`0 项不合格`／rc=0，`归属 A5：磁盘 43 族＝驱动 23 族＋非电池登记 20 族`（名单含 `RouteTriage`）。
反向注码：摘掉那条登记 ⇒ rc=1 并打回同一句 A5；还原 md5 一致。
`.gitignore` 三条例外逐行现测（`git check-ignore -v` 命中哪条）：目录行必需（少了它 `logs/*` 挡回整棵子树）、
`**/*.log` 必需（仓库根第 24 行 `*.log` 会命中 `00-provenance.log`）、`**/*.tsv` 多余（只留目录行时 .tsv 已不被忽略），
现数对齐 43＝43。

### (4) 配置面可发现性门的枚举盲区（`make audit` 上一轮的 rc=2 根因）

`7437684f` 把若干 env 读取搬进 `(ctx, "KEY", ...)` 形状的 helper 后，`scripts/check-env-coverage.py` 只认
**第一个实参**位置 ⇒ 门当场读出 3 条 STALE `WEBHOOK_*`（看着像"键没人读了"，其实是门看不见）。
按"门坏了修门"处理：`helper_call_args()` 取整张实参表（允许一层嵌套括号），表内标识符也解析回常量值。
现测：读取键 194 → **201**、经 helper 枚举到的键名 22 → **29**。
新露出的 4 个 `TOOL_*` 键**补文档而不是塞基线**（`docs/DEPLOYMENT_GUIDE.md` §6.2 新增
`TOOL_CIRCUIT_FAILURE_THRESHOLD`／`TOOL_CIRCUIT_HALF_OPEN_ATTEMPTS`／`TOOL_AUDIT_QUEUE_SIZE`／`TOOL_RISK_OBSERVED_RETAINED` 四行，
各写默认值、可用区间与判红坐标）。
反向注码：把枚举退回"只看第一个实参"，3 条 STALE 原样复现；还原 md5 一致。
另修同文件的汇总行自身会骗人：`红 {len(holes)}` 不含 stale ⇒ 纯 STALE 的红会印「红 0」却退 1。
现改为 `红 N（未文档化 X · 基线陈旧 Y）`；注码：往基线里塞一条不存在且带理由的键 ⇒ rc=1 且印「红 1（未文档化 0 · 基线陈旧 1）」，
撤码 md5 一致。门当前读数：读取键 201 · 已文档化 94 · 工具进程自动豁免 16 · 基线登记 91 · 红 0。

### (5) 文档失真回写（任务 #8）

- `docs/architecture/ZOMBIE_API_TRIAGE.md`：旧「2026-10-11 重测」一节改标题为**一次性快照**并写明它的脚本与事实源都在 `/tmp`、
  不可重跑；新增「二次重测：取证器进仓库」一节，含复现命令、现测读数、口径差异、本轮修掉的取证器缺陷与注码证据。
  标题（H1）里那句「API_PAGE_INVENTORY §4.2 · 287 个注册」换成现口径。
- `docs/architecture/API_PAGE_INVENTORY.md`：H1 的「自动生成」从未成立（无任何脚本产它或读它），
  加作废横幅：总览「1114」vs 现测「`/api/` 1721」＝少算 607 行，派生的「287 僵尸」「209 断链」不可作处置输入。
  `docs/INDEX.md` 对应行随改成 `⌛归档`／`archived`（`📋报告` 与 `current` 的判据是徽章图例第 32 行）。
- `docs/architecture/BACKLOG_TODOLIST.md`：P1 两行里的 348／135／209 换成可重跑读数 221／278／205，
  并保留「旧一次性快照读作 348/135/209、面集合不同」的说明。
- 工作区根 `CLAUDE.md` 规则1 写的远端名 `origin`／`github` 与本仓实测不符（本仓是 `gitee-upstream`＋`upstream`，
  在 hivemtk 里 `git push origin` 直接报 "The origin remote does not exist"）⇒ 换成现测名，并写明与仓库内 `hivemtk/CLAUDE.md` 规则0 冲突时以仓库内为准。

### (6) 登记未吞项（不是待办措辞，是判据不在本树的那几类）

- **僵尸判定的运行时腿**：静态三面扫不到地址栏直开口、渠道/ESP 回调、版本不可知的老前端构建三类消费方，
  删除动作必须另有访问日志证据；本机没有可用的线上日志源 ⇒ 属"要外部数据"的阻塞，名单与判据形状已进文档。
- **并行泳道的脏文件**（本轮入库后最后一次现数＝`git status --porcelain` 76 份：`user-web` 70／`user-server` 6，
  含 `user-web/src/utils/errorReporting.js`、`user-web/src/utils/clipboard.js` 等；这个数字随对方干活会漂，
  引用前先现取）：归属按 mtime 窗口与 `git status` 现读，不代签、不代改。
- **两枚同类慢测**（`user-web/bridge/test/humanize.test.js` 3701ms、`test/adapter-b24-send-verify.test.js` 文件级 13422ms）：
  与本轮报告的入口不同类不同泳道，只登记。
- **`sop_state_memories.session_id` 的 `not null`**：属 schema 取舍决策，`20ed9fbd` 里点名过，未擅改。
- **四刀「真杀」仍需要一个会编 Go 的 bash 3.2 执行点**、**`make audit` 整链在 CI 里无执行点**：沿用第五十六轮的在册判定，本轮没有新增执行点。
