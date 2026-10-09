# 假性未完成清单（2026-10）

## 0. 这份清单在数什么

口径（与 `scripts/check-unwired-assets.sh` 第 6 行那句项目自述一致：本项目的主缺口形态不是"没实现"，而是"实现了但没人调用"）：

一条能力算"假性未完成"，当且仅当它满足下面任一条：

1. 有代码、有路由、有 UI 入口、有参数中心的键，但生产路径上永远走不到（零装配点、零调用方、或拿到的依赖恒为 nil）；
2. 返回值或落库字段声称表达了某件事，实际是编造的（假回执、假统计、假状态）；
3. 注释/文档/函数名声称"调用方是 X"或"由 Y 接线"，而 X、Y 里根本没有那一行；
4. 失败被静默吞掉，读起来像成功。

"完成"只有三种收法：**接上线**、**补齐缺的那一段**、**删掉或去毒**（把谎报的形状改成会出声的形状）。
本清单里每条 A 类都带"处置动作 + 判据"，B 类都带"要谁拍什么板 + 接上线需要哪几行"。

所有结论都是跑出来的，不是读代码推出来的：命令与读数在第 5 节，标注测于哪棵树。

## 1. A 类：本轮已清零

按"用户能感知的伤害"从大到小排。

### A1 触达批量调度编造投递回执（最高价值）

- 表象：`ReachPipelineService.dispatchOutbound` 在没有注入真实发送器时，仍然
  返回 `msg_渠道_客户_纳秒` 这种自己拼的消息号并回 `nil` 错误；调用链上层把它当投递成功，
  `trackSendResult` 再把这个假号抄进 `job.Payload["_tracking"]`，运营台读到的是"已投递，平台消息号是 …"。
- 实测为什么是假的：`SetReachSender` 的唯一生产调用点在 `internal/router/service_routes.go:339`。
  没跑过那一行的实例（含全部单测构造的实例）里 `s.sender == nil`，此时除 bridge 那几条渠道
  自己走包级 `service.DeliverBridgeOutbound` 能真出网以外，其余渠道一个字节都没发出去。
- 处置：
  - `internal/service/reach_pipeline_dispatch.go:99` 改成
    `return "", fmt.Errorf("channel %s 没有装配真实发送器，本条没有出网", job.Channel)`；
    作业停在可重跑的失败态，不再有 `_last_send`、不再有假号。
  - 装配点删掉 `if sender != nil` 那段死分支（构造器永不返回 nil），直接
    `reachSvc.SetReachSender(app.NewPipelineReachSender(db))`。
  - `internal/app/reach_sender_wiring.go` 里"构造失败返回 nil，由调度器降级为占位发送"这条
    承诺一并删除：`NewIntegrationReachAdapterFromDB(nil)` 返回的是零值适配器，它的渠道方法
    各自回 sentinel 错误，不需要也没法"降级"。
- 判据：`internal/service/reach_pipeline_test.go` 的
  `TestDispatchOutbound_WithoutSenderRefuses`（:1851，断错误文案 + 断 `_last_send` 不存在）、
  `TestDispatchOutbound_SenderBackedChannels`（:1809，装上桩发送器后断回的是发送器交回的号）、
  `TestDispatchOutbound_BridgeChannelsAttemptOutboxWithoutSender`（:1830）、
  `TestDispatchOutbound_SenderMessageIDLandsInPayload`（:1866）；
  新增 `internal/app/reach_pipeline_sender_test.go` 三条，逐条走 `service.ReachChannels` 的枚举集合，
  断"任何渠道都不许交回 `(\"\", nil)`"。

### A2 card 渠道在批量管道里被 ReachChannels 宣告支持，却没有任何出口

- 表象：`internal/service/reach_pipeline.go:83-95` 的 `ReachChannels` 把 `card` 和另外 10 条渠道
  并列宣告为管道支持的渠道，调度器会为它建作业、走限流、走内容准备；
  而装配好发送器的实例上，`SendReach("card", …)` 过去落到 switch 末尾那句
  `unsupported channel: card`。这句话本身是错的：`IntegrationReachAdapter.SendCard` 确实存在，
  单条卡片外发也在 `reach.card.send` 工具里真跑得通。
- 实测缺的是什么：不是实现，是参数。`SendCard` 要 `card_id`（卡片后台的数字 id），
  而 `ReachSender` 这条端口只递 (渠道, 账号, 收件人, 文本)，文本由 `prepareContent` 拼出来，装不了一个数字 id。
- 处置：`internal/app/reach_sender_wiring.go:56` 给 `card` 单独一格，错误里同时说出
  "缺 card_id 来源"和"单条卡片外发走 `reach.card.send` 工具"。
- 判据：`TestPipelineReachSender_CardRefusesWithReason`（断两个关键词都在文案里）。

### A3 工作流"子流程"节点被执行器记成"已完成"，实际什么都没跑

- 表象：运营台 `user-web/src/views/workflowOrchestrator/Editor.vue:224` 提供
  `{ value: 'subflow', label: '子流程' }`；后端 `SubflowNodeExecutor` 写得相当完整
  （深度上限 `MaxSubflowDepth = 5`、`_subflow_depth` 透传、副作用去重）。
- 实测为什么是假的：装配点 `internal/router/workflow_orchestrator_routes.go:24` 过去调
  `RegisterWorkflowNodeExecutors(registry)`，而注册函数里是 `reg(&SubflowNodeExecutor{})`
  空结构体字面量：注入用的构造器 `NewSubflowNodeExecutor(orch)` 全仓（含测试）引用数为 1，
  也就是除了它自己的声明没有任何地方用过。于是 `e.orchestrator` 恒为 nil，
  `Execute` 走到最后一行打一条 Warn 后返回 `NodeStatusCompleted` +
  `{"_subflow_invoked": id, "_skipped_reason": "orchestrator not injected"}`。
  执行记录里这个节点是绿的，子工作流一行都没跑。
- 处置（接线，不是删）：`RegisterWorkflowNodeExecutors` 加第二个参数
  `orch *WorkflowOrchestratorService`（`internal/service/workflow_node_executor.go:263`），
  注册处改成 `reg(NewSubflowNodeExecutor(orch))`（:278），装配点传下已经构造好的 `svc`（router:24）。
  不成环：`svc` 先于 registry 构造，且 `orch.Execute` 只在运行时被调，那时 `SetDispatcher` 早已跑过。
- 判据：`internal/service/workflow_dispatcher_test.go:41` 起的注册用例新增断言
  "注册进 registry 的那份实例 `orchestrator != nil`"（:67）；
  原先叫 `TestSubflowNodeExecutor_InvokesSubflow` 的用例其实验的是跳过分支，
  改名为 `TestSubflowNodeExecutor_WithoutOrchestratorSkips`（:211）并补断 `_skipped_reason` 必须在场。

### A4 打字意图预测的 SSE 端点：注释承诺了一个从未存在的推送机制

- 表象：`GET /api/chat/typing-predict/sse` 已挂路由（`business_routes.go:343`），
  注释写"后端通过 SSEPub 注入预测结果并推送"。
- 实测：`SSEPub` 这个符号全仓不存在（只有 bridge 域无关的 `GlobalSSEPublisher`）。
  端点实际只发两帧：`event: connected` 和每 30s 的 `event: ping`。
  `user-web/src` 里没有任何地方订阅它，所以今天没有受害者；但注释让下一个人以为结果会从这条流出来。
- 处置：注释改成把"没有第三个生产者"讲明白，并写清要真推送需要补什么
  （按 session_id 分组的发布端 + 下面 select 里加它的 channel）；
  `internal/controller/typing_predict.go:82` 顺手把 `connected` 帧从
  手写 `"...\"session_id\":\"%s\"..."` 改成 `json.Marshal`，
  避免 query 参数里的引号把响应帧劈坏。两处改动都刻意保持行数不变，
  因为 `docs/operations/reverse-proxy/README.md:48` 引用了本文件的 `:74`/`:88` 行号。
- 判据：`go build ./... && go vet ./internal/controller/`（见第 5 节）；
  该行号引用已复算：`grep -n "ticker := time.NewTicker" internal/controller/typing_predict.go` 仍是 88。

### A5 触达流水线的全局单例：`Get` 永远返回 nil

- 表象：`internal/service/reach_pipeline.go` 曾有 `reachOnce/reachInstance` +
  `InitReachPipelineService(db)` / `GetReachPipelineService()` 一对。
- 实测：两个函数在全仓（含测试、含文档、含 `.md`）零调用点；真实消费者
  （router/service_routes.go、router/frontend_aliases.go、app/reach_tool_wiring.go、
  app 的 6 处测试）全部直接 `NewReachPipelineService(db)`。
  `Get…` 没人 Init 过，任何调用者拿到的都是 nil。
- 处置：删掉这一对与两个包级变量，文件头注释改为"本服务没有全局单例"（`reach_pipeline.go:5`）。

### A6 `bridge.GlobalBridgeReachAdapter`：只写不读的全局，注释还点名了一个不存在的读者

- 表象：`internal/bridge/reach_adapter.go` 里
  `// GlobalBridgeReachAdapter 占位全局（reach_sender_wiring.go 引用）` + 一个包级 var。
- 实测：全仓两处引用，一处是声明，另一处是 `internal/router/router.go:552` 的赋值
  （赋给一个也只用了这一行的局部变量 `tooluseBridgeAdapter`）。
  被注释点名的 `reach_sender_wiring.go` 用的是自己 `bridge.NewBridgeReachAdapter(...)` 构造的实例，从不读这个全局。
  也就是说：装配点每次启动都在往一个没人读的格子里写。
- 处置：删掉该 var（bridge 侧 3 行）和 router 侧那段构造+赋值（6 行）。
  触达调度要 bridge 适配器时走 `NewPipelineReachSender` 自己构造的那条，行为不变。

### A7 私信工具注册的重复装配口，注释自称"调用方：router.Setup()"

- 表象：`internal/app/sales_engine_factory.go` 的 `RegisterAgentPrivateMessageTools(gormDB)`，
  doc 注释第三行写"调用方：router.Setup()"，成功日志还带 ✅。
- 实测：全仓含测试零调用点。pm.* 工具的生产装配是另一条：
  `app.PrivateMessageToolProvider.Provide()`（`tool_provider_wiring.go:45`）
  由 `registerAllAgentToolsViaProviders` 批量注册，入口是 `router.go:233` 的
  `app.RegisterAllAgentTools(gormDB)`。留着它等于宣称有两条装配路径，且其中一条从未跑过。
- 处置：删除该函数，原位置留一段说明"不在这里注册、真装配在哪"的注释（:154）；
  连带删除因此变成零调用点的 `tooluse.RegisterPrivateMessageTools`
  （`internal/aiagent/agent/tooluse/private_message_tools.go`，-11 行；
  活的构造器 `BuildPrivateMessageTools` 保留，provider 用的就是它）。
  判据：删除后 `go build ./... && go vet ./...` 全绿（第 5 节）。

### A8 触达账号只读工具链（`reach.account.list` / `reach.health`）

过去恒返回空清单/占位健康度。现已接真实读侧：`internal/app/integration_reach_adapter.go:572`（ListAccounts）
与 `:541`（AccountHealth），工具面在 `tooluse/reach_tools.go:994` / `:808`。
两条口径同时被钉住：

- `AccountHealth` 先判"这个渠道有没有账号这一层"，再判"这台实例配没配底座"
  （`integration_reach_adapter.go:550-552`），否则同一个问题在两种实例上给出两个指向不同修法的答；
- `account_id` 必填、整渠道清单必须回落到 `reach.account.list`（:543）。

判据：`internal/app/reach_account_reader_test.go`（该文件首行注释记录了"此前恒返回…"的账）。

### A9 真实撤回：不再拿合成 id 冒充可撤回

- `IntegrationReachAdapter.Recall`（`integration_reach_adapter.go:533`）如实返回
  `recall(%s): channel not implemented…（发送侧未保留平台消息 id，服务端无可寻址目标）`。
- `BridgeReachAdapter.Recall`（`internal/bridge/reach_adapter.go:114`）对 bridge 渠道显式拒，
  因为 bridge 的 msgID 是 `deliverToOutbox` 的合成键，透传给 inner 会拿假 ID 调平台撤回 API。
- 发送侧返回的 `tg-{account}-{纳秒}` 这类占位在方法注释里写明（:92/:118/:140），
  并说明真实 `message_id` 落在 message_hub（`tg-out-{account}-{平台消息号}`，
  见 `internal/service/feishu.go:615` 的 `telegramOutboundHubMsgID`）。
- 残留缺口进 B2：这两串 id 之间没有公共键，所以回执里的 message_id 今天无法 join 到任何库行。

### A10 三处忽略 kbID 的 `ListByKB`

`faq_entries` / `sop_templates` / `knowledge_documents` 上的三个同名方法把入参 kbID 吃掉、
返回全表，等于"按知识库隔离"这件事在读侧是假的。已全部删除
（`internal/repository/faq_entry.go`、`internal/repository/sop_template.go`、
`internal/aiagent/knowledge/repository/knowledge_document.go` 及配套用例，5 个文件净 -102 行）。
仍然保留的 `ListByKB` 只有 `AgentKBBindingRepository` 那一个（`internal/repository/agent_kb_binding.go:89`），
它真用 kbID 且有路由消费者（`controller/agent_kb_binding.go:36-37`）。

### A11 知识库冗余统计列：注释从"实际从内容表 COUNT"改成实测账

`internal/model/knowledge_base.go:24` 起原写"冗余统计字段 (用于列表展示, 实际从内容表 COUNT)"。
实测没有任何写入路径维护它们：仓储 `Update`（`repository/knowledge_base.go:161`）是字段透传，
`knowledgeBaseUpdateReq` 里没有这两列，内容增删从不回写；内容表与 `knowledge_bases` 行没有外键关系。
本机 `user_db` 现测：6 行 KB 的这两列全为 0，而内容表分别是 14489 / 342 / 34 行。
唯一消费者是 `service.GetKBStats`（`service/knowledge_base.go:136`）。注释已按实测改写（+6/-1）。

为什么不当场"补个 COUNT"：按类型 COUNT 会把 14489 条 faq 同时显示在 4 个 faq 型 KB 上，
那是把假数换个来源重做一遍。要么给内容表加真外键（B1），要么让读侧承认"没有归属口径"。

### A12 删掉从未被消费的 KB DTO

`user-server/internal/dto/knowledge_base.go` 整文件删除。证据：包内零引用；
`dto.KnowledgeBase` / `dto.AgentKBBinding` / `dto.ChannelBinding` / `dto.IsValidKBTypeDTO`
全仓零命中；`git log --all -S'dto.KnowledgeBase'` 零命中（从来没有过消费者）；
所有常量命中都是 `model.` 限定的；它第 29 行注释写"不暴露内部字段"，
而结构体里就带着 MemberCount/DocCount。

### A13 `email.go` 附件参数：把"这里挂不上附件"的前提算清

`internal/service/email.go:157` 起。原注释只说"不支持附件"，读者无法判断这条路径今天
到底会不会收到附件。现写明：`Send` 的非测试调用点共 7 处，6 处传字面 `nil`，
第 7 处是 `proactive_reach.go` 的注册闭包原样透传形参，而它唯一产出方 `sendEmail`
（同文件 :615）也传字面 nil；并给出诚实路径（`internal/email/service/email_send.go`，
经 `internal/pkg/mail` 的附件解析器，挂不上会出声）。
`docs/superpowers/plans/2026-09-20-coverage-heavy-low-packages.md` 里那句"活调用点 4 处"
是漏数（另两处自 e38b6d0e 2026-08-29 与 fbe4b760 2026-09-02 就在，都是字面 nil），
已在该文档补订正块而不是改结论。

### A14 参数中心：给"改了也不生效"的键加上读侧账

`internal/service/config_param_seeds.go` 重排（±307 行）：给没有读取点的键在 Name 上标
"（未接线）"、Description 上写"【当前不生效，改了也没人读】"，并注册读取点校验门
`scripts/check-config-param-readpoints.py`（进 `Makefile` 的 audit 链）。
现测计数（命令见第 5 节）：参数 114 条，其中标"（未接线）"78 条。
"未接线"不是待办清单而是**停止谎报**：运营台上这些格子过去长得和生效的格子一样。

### A15 其余同批清零（逐条可在 diff 里核对）

- `tooluse` 的触达注册中心岛屿：`reach_service_registry.go` 从 190 行瘦到只剩一条真正需要的
  跨包注入（公众号发送服务，因为 tooluse 不能反向 import service）；
  其余渠道服务由 `NewIntegrationReachAdapterFromDB` 按 db 自己构造。
- `ProductionReachAdapter` 整文件删除（159 行，零构造点）。
- 5 处"未实现"注释订正：那些渠道其实由 bridge 层实现了，注释还在写"未实现"会让人重造一遍。
- 触达适配器评审用例（`internal/app/reach_adapter_review_test.go`）与操作日志订阅者用例
  （`internal/app/operation_log_subscriber_test.go`）改成断真实落库那一行、并按整行匹配数行数。
- 错误串命名订正到 golangci-lint 的 ST1005 口径（`errors.New`/`fmt.Errorf` 的首词不大写、
  不带标点尾巴），避免 `make lint` 在主干长期红。逐条落在第 6 节文件清单里的
  `internal/app/reach_sender_wiring.go`、`internal/service/reach_pipeline_dispatch.go` 等文件里，
  不在这里写死条数（条数随 lint 版本与主干状态漂移，写进文档就是一条会过期的断言）。

## 2. B 类：需要拍板（技术都能做，缺的是"要不要这么做"的决定）

### B1（最大一条）RAG/FAQ 语义答案缓存整条竖没有任何生产装配

现状是"零件齐全、独缺接线"：

| 段 | 位置 | 状态 |
| --- | --- | --- |
| 表 | `rag_answer_cache` | 本机库已存在，8 列形状正确，`count(*) = 0` |
| 存储 | `internal/aiagent/rag/cache/store_pg.go` `PGAnswerCacheStore` | 已实现，生产构造点 0 |
| KB 元读 | 同文件 `NewPGKBMetaReader` | 已实现，生产构造点 0 |
| 服务 | `NewFAQAnswerCacheService(store, kbMeta, threshold)` | 已实现，生产构造点 0（只有 2 个测试文件构造它） |
| 注入口 | `service.SetGlobalFAQAnswerCache(svc, embedder)`（`smart_cs_orchestrator.go:32`） | **零调用方** |
| 消费 | `smart_cs_orchestrator.go:348` 查缓存、`:472` 异步写缓存、`:598` 命中即跳过 LLM | 被 `o.faqCache != nil` 门住，恒假 |

`globalFAQCache` 的读点只有 `smart_cs_orchestrator.go:121-122`（构造时抄进实例字段），
而写点只有那个没人调的 setter ⇒ 编排器永远拿到 nil ⇒ 三层查询（精确/语义/回源）在生产里从没跑过。
连带后果：`knowledge_bases` 的 `Version / CanaryEnabled / CanaryPercent` 三列
（口径见 `internal/model/knowledge_base.go` 的"版本口径"块）唯一的生效面就是这张缓存表的
`(kb_id, prompt_version)` 键；缓存不挂，灰度路由就是在路由一个没人读的命名空间。

为什么不当场接：命中即返回缓存答案，等于把客户收到的回复从"每次现生成"改成"语义相近就复用"。
这一改的爆炸半径是客户会话内容本身（错答、过期答案、以及每次入会话多一次 embedding 调用），
而且 `KBAnswerVersionFor` 的灰度语义要有人认领默认比例。
接线本身只要 6 行：在 `app.BuildSmartOrchestrator`（`internal/app/sales_engine_factory.go:113`，
唯一编排器构造点，`router.go:295` 调用）里构造 store/reader/service/embedder 并调
`service.SetGlobalFAQAnswerCache(...)`，或更好：给编排器加一个显式 setter 而不是走包级全局。
需要拍板的是：默认开还是关、阈值取多少、灰度从哪个百分比起、以及要不要先只写不读跑一周。

### B2 触达回执的 message_id 与 message_hub 行没有公共键

发送侧回 `tg-{account}-{纳秒}`，落库行是 `tg-out-{account}-{平台消息号}`。
两个串都真实存在，但没有 join 键，因此：运营台读到的 `_tracking.message_id` 查不到任何行，
`Recall` 也无法从回执反查平台消息号。根治需要 `SendMessageEx` 把平台 `message_id` 回传
（签名变更，涉及 `internal/service/feishu.go:623` 及其全部调用方），
或统一改用 hub 侧 id 作为对外回执。要拍板的是"回执号"这件事的合同归属：是平台号、还是 hub 行号。

### B3 知识库内容归属（A11 的另一半）

内容表（faq/sop/documents）与 `knowledge_bases` 行没有外键；`hit_count` 恒 0 且前端仍在传
无人读取的 `kb_id`。要么给内容表加 `kb_id` 外键并回填（数据迁移 + 归属口径评审），
要么把 KB 列表页的统计列摘掉。这是产品口径，不是补一个 COUNT 能诚实收掉的。

### B4 8 条 UNWIRED 台账行（原 9 条，项20d 已于 2026-10 转 wired）

`scripts/check-unwired-assets.sh` 现在按 UNWIRED 登记的格（项9 读/统计侧、项11 的两条草稿侧、
项14、项19 的四条）本身是诚实的：它们明写"未接线"，并且漂移会让门变红。
（原"项20"即 SOP 节点 `Tools` 字段：I5 已落地为 `validateGraph` 保存期 fail-closed 拒绝
`ErrSOPNodeToolsUnsupported`，该格转 wired，基线从 94/103 升至 95/103。）
但每一格都对应一个"UI 上有入口/文档里有说法，运行时长不到"的能力，需要逐条决定接还是撤。
本轮**没能**往里加防回归行：该脚本被另一泳道占着（`git status` 里是 `M`），
共享索引下不能再叠一次改动。待加的行列在 A1/A5/A6/A7 的判据里已经写成人能抄的形式。

### B5 78 条"未接线"参数里要接哪些

A14 只是停止谎报。逐条接线需要按业务优先级挑（例如 `confidence.persona_default_threshold`
背后是整条 `LLMPersonaEvaluator`，见 B6；`cache.faq_ttl` 背后是 B1）。

### B6 五个"整竖零消费者"的产码孤岛

每一个都验证过"全仓含测试零引用"（单 Go 模块、包在 `internal/` 下，不存在外部消费者）：

| 孤岛 | 位置 | 备注 |
| --- | --- | --- |
| 卡片访问 UV/PV 统计 | `internal/service/card_access.go`（接口+构造+实现）、`repository` 的两个 CardAccess 仓储 | 表 `card_accesses` / `daily_card_uv_stats` 本机库里都不存在（现测计数 0/2）；`user-web` 无任何 UV 展示消费点。卡片域另一泳道正在动（`card_routes.go` 为 `M`），所以只报不删 |
| AI 解决率统计 | `internal/service/ai_resolution_stats.go` + `repository/ai_resolution_stats.go` | 只有这条死服务消费仓储 |
| 抖音集成服务 | `internal/service/douyin_integration.go` | `db any` 签名；真实抖音外发已走 bridge。2026-09-22 的渠道审计批动过它（1af6d28c），`docs/superpowers/plans/…coverage…md:3778` 已记录它在丢弃面里 |
| LLM 人设评估 | `internal/service/persona_evaluator.go` 的 `NewLLMPersonaEvaluator` | 人性化评估已装配（`InitHumanizeEvalService`），人设评估没有；对应参数 `persona_default_threshold` 已按"未接线"标注 |
| 欢迎 SOP 模板 | `internal/service/sop.go:932` `NewWelcomeSOP()` | 返回一份 14 节点示范图，零调用；要么做成种子数据，要么删 |

处置建议分两类：卡片域那条等泳道收口（怕撞），其余四条可以直接删，
但删代码不等于删决定，故在此点名等一句"删"。

### B7 门禁的 CI 面

- 本轮新增/改动的门目前只有本地执行点（`make audit` 的 20 个静态门里没有 Go 测试、也没有 lint）。
- Go lint 在 CI 只有一个执行点：`user-server-ci` 的 `static-gates` 步骤；
  `lint.yml` 注释写着 golangci-lint，但那个 workflow 里没有对应 job。
- 新门（参数读取点、未接线标注）要进 CI 需要单独一步；否则它是"本地绿、CI 不知道"的门。

## 3. C 类：已诚实登记、不再谎报（保留资产，改掉说法）

- `IntegrationReachAdapter.Recall` 与 bridge 的撤回拒绝（A9）。
- `AccountHealth` 对没有配额来源的渠道如实回 `not_tracked` 与 0，而不是把"没测过"写成"没问题"。
- `SignUploadURL`（`internal/storage/local.go:171`）：注释自称"占位实现"，
  实际行为是响亮报错并指向 `UploadReader`。形状是对的，只有"占位"这个词误导；
  未改，因为它不产生假成功。
- `internal/bridge/handler_http.go:215` 的"注入的查询器未实现 FetchOutboundUndelivered"降级提示：
  实测生产注入的是 `MessageHubRepository`（`router.go:528`），它实现了该方法，
  所以这条分支今天不开火，是接口边界上的防御，不算孤岛。
- `kb_connector_pull.go:80`、`browser_publisher.go:73`、`platform.go:151`、
  `sop_compensation.go:204`、`dispatcher.go:572`：都点名了"谁支持、缺什么"，属正确形状。

## 4. D 类：门禁面（这轮拿到的新证据）

1. `make audit` 的 shell 形状门会被他泳道挡住：`scripts/mut_journey_stage_index_p903.sh:74`
   在 bash 3.2 + UTF-8 下会把 `$expect_fail` 紧跟的中文字节一起吃掉，
   `check-shell-cjk-expansion.sh` 因此报 `scanned=157 命中 2（基线 1）` 而整链 rc=2。
   该文件是未跟踪的新件（`??`，mtime 2026-10-09 00:37），属正在写的活。一行修法：改成 `${expect_fail}`。
2. 在 user-server 里 `go build ./...` 不带 `DEVELOPER_DIR` 会撞 Xcode 许可门，
   打印 `# runtime/cgo / You have not agreed to the Xcode license`；
   而这条错误如果经 `| head` 管道读码，`$?` 拿到的是 `head` 的 0 ⇒ 假绿。
   本轮所有 Go 门都在 `export DEVELOPER_DIR=/Library/Developer/CommandLineTools` 之后跑，
   且退出码一律用 `out=$(cmd); rc=$?` 形状取，不接管道。

## 5. 复测读数

（本节在最后一轮门禁跑完后填，命令与读数逐条对应上面的判据。）
