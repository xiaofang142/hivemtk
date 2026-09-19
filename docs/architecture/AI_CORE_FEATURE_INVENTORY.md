# HiveMtk AI 核心链路功能点清单（Feature Inventory）

> 生成时间：2026-08-25
> 范围：user-server `internal/aiagent/**` + `internal/service/**` AI 核心
> 方法：逐文件源码精读，所有数字为源码硬编码实测值
> 用途：AI 核心能力基线速查与功能事实来源

---

## 总览

| # | 功能域 | 子功能 | 核心文件（相对 user-server/） |
|---|--------|--------|-------------------------------|
| F1 | LLM 调度降级 | 7 场景路由 + 四级降级 + 集群熔断 + ReAct适配 + 自洽投票 | `internal/aiagent/llm/{dispatcher,fallback_tree,provider_failover,react_adapter}.go` |
| F2 | Agent Runtime 推理闭环 | 感知→对齐→危机门禁→规划→复核 五阶段 | `internal/aiagent/agent/runtime/*.go` |
| F3 | 工具集 + 护栏装饰器 | 42 工具 / 12 装饰器 / LoopGuard / MCP Server | `internal/aiagent/agent/tooluse/*`, `mcp/server.go` |
| F4 | RAG 检索 | 三层(实4层)短路 + Hybrid(RRF) + HyDE/MultiQuery(关) | `internal/aiagent/rag/retrieval/*.go` |
| F5 | 知识库管线 | 分块→embed→索引、外部导入、反馈回流 | `internal/aiagent/knowledge/service/*` |
| F6 | 入站编排 | SmartCSOrchestrator 九步 | `internal/service/smart_cs_orchestrator.go` |
| F7 | 生成管线 + AgentLoop | SalesEngine 12 步 + ReAct 5轮护栏 | `internal/service/sales_engine*.go`, `layer.go` |
| F8 | 意图识别 | 粗13类 + 细8大类27子类 + 投机识别 | `internal/service/intent_recognition*.go` |
| F9 | SOP 引擎 | 19 种节点 DAG + 调度器16worker + AB分流 | `internal/service/sop*.go` |
| F10 | 置信度/转人工 | 五信号加权 + 温度/Platt/Conformal 校准 + 六否决 + 动态阈值 | `internal/service/confidence/*.go` |
| F11 | 拟人化三层 | 文本润色 + 五维评估0.85门禁 + 行为模拟 | `internal/service/humanize*` |
| F12 | 异议处理 | 7 类关键词规则 + 话术库转化率学习 | `internal/service/objection_handler.go` |
| F13 | 自学习闭环 | 实时反馈 + 销冠蒸馏 + Prompt迭代 + SOP自动优化 + Thompson Bandit + trace调权 | `internal/service/feedback_loop/*`, `trace_learning/*` |
| F14 | 多智能体 | sales/cs/hybrid × passive/active | `internal/model/ai_agent.go`, `agent/lifecycle/` |
| F15 | 评测 | ChrF + LLM Judge | `internal/aiagent/eval/*` |
| F16 | 流失预警与挽回 | RFM/Churn 分层入队 + 到期消费外发（W-5，2026-09-19 接线） | `internal/service/customer_rfm.go`、`internal/service/recovery_queue*.go` |

---

## F1 LLM 调度与降级

### F1.1 场景化 Dispatcher
- **文件**：`internal/aiagent/llm/dispatcher.go`、`dispatcher_register.go`
- **机制**：7 场景 `intent_recognize/sop_reply/objection/friendly_chat/long_summary/high_quality/low_cost`；候选遍历 主Provider→Fallbacks→兜底；质量门禁 MinQuality 0.7~0.95；RPM=60；灰度 CanaryRoute；请求级缓存 CacheKey/TTL；审计表 `llm_routing_audit`
- **内置 Provider**：deepseek($0.001/Q0.85)、qwen-turbo($0.003/Q0.82)、qwen-max($0.02/Q0.92)、gpt-4o($0.03/Q0.95)、glm-4-plus($0.05/Q0.91)、moonshot($0.012/Q0.88)；本地 default Q0.99/800ms
- **生产路由本地优先**：所有场景默认 Provider=default

### F1.2 四级降级 FallbackTree
- **文件**：`fallback_tree.go`
- **链路**：Primary(7B) → Secondary(3B) → Cache → Template；键=`llm_fallback:{provider}:{sha256}:{前32字符}`
- ⚠️ 模板兜底文案单一无场景区分

### F1.3 ProviderFailover 熔断
- **文件**：`provider_failover.go`
- 连续失败≥5 → Down 熔断60s（Redis SETNX 多实例同步）；延迟>3000ms 判 Degraded；健康检查30s循环

### F1.4 ReAct 适配器
- **文件**：`react_adapter.go`
- NoFC Provider 时把工具清单注入系统提示词，Thought/Action/Observation 协议正则解析

### F1.5 SelfConsistency / MultiModelVote
- selfconsistency 泛型库 N=5 多数投票（humanize 用 N=3）
- ⚠️ `dispatcher_dispatch.go:333` MultiModelVote 名不副实：只取 QualityScore 最高者结果，无一致性比对

## F2 Agent Runtime 推理闭环
- **文件**：`internal/aiagent/agent/runtime/inference_cycle.go` 等 5 stage
- 感知→对齐打分→危机门禁→任务规划→复核；单阶段超时2s、总超时8s；EarlyReturn 危机转人工、FAQ SkipLLM
- 危机关键词：高危23（退款/骗子/315/lawsuit…）直接人工；中危14；低危9；愤怒≥0.7 或共情≤2 升级中危
- **阶段边界断点续跑**（T-P1-01，2026-09-19 接线）：每阶段成功后 upsert `agent_checkpoints(thread_id,stage,state)`，`thread_id` 按载荷内容寻址；中断后重试只跑剩余阶段、已完成阶段的产出由快照回填。开关 `FF_LTC_CHECKPOINT` **默认关闭**，关时执行序列与接线前逐语句一致。游标按阶段序号取最远行（不按 `updated_at`，见 `service.LatestByStageOrder`）；快照带 `done` 标与版本+载荷指纹，二者任一不符即退化为整体重跑。适配器 `internal/app/agent_checkpoint_wiring.go`，装配点 `cmd/api/main.go`（须早于 `router.Setup`）

## F3 工具集 + 护栏

### F3.1 工具注册中心：42 工具
- rag/knowledge 4（rag.search/knowledge.feedback/add_doc/list_kb）
- customer 8（search/get/create/update/merge/tag×2/segment）
- 业务 6（follow_task×2/order.lookup/aftersale×2/logistics.track）
- reach 触达 20（11渠道send+card.send/sms.send/batch/schedule/recall/health/history/template.apply/account.list）
- 私信 pm.* 3 + 会话卡片 card.show（保底注入）

### F3.2 装饰器链（12）
permission → ratelimit → circuit → retry → timeout → audit → cost → feedback → dead_letter → result_cache → double_intercept → param_validator；LoopGuard 同指纹 3次/60s 拒绝
- 说明：上表是**全部已实现的装饰器**，单次工具调用的实际链更短。`ToolExecutor.buildHandler` 实跑为
  permission → ratelimit → circuit → retry → timeout → audit(+cost)，`FeedbackSink` 非空时再在外面套一层；
  `FF_LTC_APPROVAL_GATE` 非 `off` 时最外层再套一层审批门，`block` 态下该层会让拒绝真的传下去（见 W-1 条）；
  dead_letter / result_cache / double_intercept / param_validator / LoopGuard 挂在 ToolRouter 与 Agent Loop 上，不在 executor 链内
- **circuit 曾恒不生效（T-P1-04 于 2026-09-19 接线）**：`CircuitBreakerRegistry` 此前只在测试里构造，
  `ToolExecutorConfig.CircuitBreaker` 生产无赋值点 ⇒ 装饰器 nil 早退，链上那格是空的。现由
  `internal/app/tool_circuit_breaker_wiring.go:applyToolCircuitBreaker` 按 `FF_TOOL_CIRCUIT_BREAKER` 三态接线：
  `off`（默认，字段置 nil，行为与接线前逐字相同）/ `shadow`（跑同一套 `Allow()` 状态机但不拦任何请求，只落
  `event=tool_circuit_decision` 结构化日志 + 进程内 would-block 计数）/ `enforce`（真拦，返回 `ErrCircuitOpen`）。
  布尔式真值（`true`/`1`/`on`/`yes`）一律归 `shadow`，**不授予拦截能力**；无法解析的值归 `off` 并告警
- 两处"熔断"互不相干，排障别只看一套：① 本条的 executor 装饰链 registry（按工具累计连续失败，
  阈值 5 / 基础冷却 30s / 上限 5m / 2x 退避 / 半开 1 次）；② `ToolRouter` 内部 `r.circuit`
  （`RouterConfig{FailThreshold:5, CooldownDuration:30s}`）。`/api/agent/tools/circuit/reset` 此前只复位②，
  对运维却答"circuit breaker reset"——假复位，现已两侧同清并在响应里回 `executor_circuit_reset`
- 仍未接：`ToolAlertManager.AlertCircuitOpen`（注释写"由 CircuitBreakerDecorator 调用"，实为生产零调用；
  根因是 `NewToolAlertManager` 本身无生产调用方，属审计告警侧缺口，见基线项 4）
- 观测入口：`GET /api/agent/tools/circuit`（一次返回 mode / 生效配置 / executor 侧逐工具状态 / would-block 报告，
  `wired=false` 时其余字段皆空，不可读作"没有工具出问题"）
- **W-1 冷触达审批门接线（T-P1-05 接线 / T-P1-06 三态，均 2026-09-19）**：
  此前的缺口比"`SetGlobalApprovalChecker` 生产零调用"更深一层——`WithApproval` / `WithApprovalChecker`
  的非测试调用点同样为 0，即**没有任何工具被审批门包过**，只补注入点等于接一根没人插的线。现改在
  executor 唯一建链点挂载：`internal/app/approval_wiring.go:applyApprovalGate` 按 `FF_LTC_APPROVAL_GATE`
  三态装配，`tooluse.ApprovalGateDecorator` 只包 `IsColdOutreachTool` 命中的工具，位置在整条链之外
  （被拒的冷触达不消耗限流令牌、不进重试、不被 audit 记成"执行过一次外发"、不产生工具反馈）
  - 三态：`off`（默认，字段与全局 checker 显式归零，行为与接线前逐字相同）/ `shadow`（挂链、每次判定留痕，
    绝不拦）/ `block`（拒绝真的传下去：`ErrApprovalDenied` ⇒ `TOOL_APPROVAL_DENIED` ⇒ 不重试 ⇒
    `StopReasonApprovalDenied`）。**布尔真值不授予阻断**——`true`/`1`/`on`/`yes` 一律降 `shadow` 并告警，
    必须是字面量 `block|enforce|active`；这与熔断旗子刻意不同（熔断恢复即放行，审批会把客户的冷触达永久拒掉）
  - **刹车（block 态唯一不拦的情形）**：白名单旗子 `ai.safety.tool_approval_gate` 没开时，判定理由为
    `disabled_by_flag` 的调用不拦。此时 checker 根本没查过白名单，"允许"侧无路径可达，拦下去就是全量冷触达
    无差别失败，且运维在端点上放进的账号也不生效。白名单旗子一开，同一份代码立刻开始真拦。
    **不放刹车**的情形是"旗子已开但表为空"⇒ 全 `denied_default` ⇒ 全拦，这是 explicit-allow 的预期语义，
    只在启动日志与快照里把 `whitelist_active_entries=0` 报出来
  - **两把旗子不是一把**：`FF_LTC_APPROVAL_GATE` 决定闸门挂没挂链、以哪种模式挂，`ai.safety.tool_approval_gate`
    （env 名由 `featureflag.EnvNameOf` 推导，点号合法：`FF_AI.SAFETY_TOOL_APPROVAL_GATE`）决定白名单生不生效。
    ⇒ **阻断需要两把同时到位**，只开一把不会拦人。观察端点两把一起回显 + `blocks_when_denied` +
    `whitelist_active_entries`，否则 `would_deny=100%` 且 `by_reason` 全是 `disabled_by_flag` 会被读成"账号都没被批准"
  - 全局注入点按模式交出不同对象：shadow 交 `shadowApprovalChecker` 包装版（`WithApproval` 那条路拿到 false
    就硬拦、它不认识 `ApprovalShadow`，不包这层"不阻断"只对装饰器一条路成立；inner 仍被问，留痕一笔不少），
    block 交 `blockApprovalChecker`（带上面那道刹车），两条路共用同一个 checker 的 `Verdict`，判定与理由同源
  - 观测入口：`GET /api/agent/tools/approval`（mode / wired / global_checker_set / blocks_when_denied /
    两把旗子 / whitelist_active_entries / `brake_engaged`（block 但白名单旗子未开 ⇒ 实际等价 shadow，显式回显）/
    `decision_report{total, would_deny, would_deny_rate_pct, by_reason, per_tool}`；未接线时不出现
    `decision_report`，避免 0 读成"零次误拦"）；`POST /api/agent/tools/approval/whitelist`（admin，
    授权/撤权只写进程内存，响应回显 `persisted:false`，grant/revoke 各留一行带生效条目数的日志）；
    每次判定落一行 `event=tool_approval_decision`（`mode` / `allowed` / `would_deny` / `blocked` / `reason` /
    `whitelist_flag_on`；**`would_deny` 与 `blocked` 是两个字段**：前者模式无关，后者只在真拦下时为 true）

- **W-6 工具审计/计费 DB 持久化（T-P1-08，2026-09-19）**：`DBAuditLogger` / `CompositeAuditLogger` 自 v3.29
  就写完了并带单测，但生产从未构造 ⇒ executor 的 `AuditLogger` 恒是 `MemoryAuditLogger(10000)`，重启即丢、
  多副本各看各的。接线时实测出四件比"没接线"更麻烦的事，均已修：
  1. **表根本不存在**：`ToolCallAuditRecord` 定义在 tooluse 包内、没进 `internal/pkg/db` 建表清单，而它自带的
     `AutoMigrateAuditTable` 全仓零调用；`check_model_migration.py` 又因"文件里含 `.AutoMigrate(`"把它判成已登记
     （真库实测无 `tool_call_audits`）⇒ 只接 logger 不登记表的话第一批写入就整批静默降级，看着接了其实一行没落。
     现模型收敛进 `internal/model/tool_audit.go` 并登记 `allModels()`（tooluse 侧留类型别名），
     该启发式漏洞已收紧为"只认 `RegisterExtraModels`"，收紧后实测新增红项 0
  2. **摘要截断产出非法 UTF-8**：`summarizeArgs`/`summarizeResult` 原按字节硬砍（`s[:200]`），砍进中文字符中间；
     内存里无后果，落 PG 报 `invalid byte sequence for encoding "UTF8"`（真库探针实测），而 `CreateInBatches`
     整批提交 ⇒ 一条坏数据带走 100 条好审计。现走 rune 边界回退的 `cutBytes`，并给每个 varchar 列按列宽裁一刀
     （`trace_id` 由 `middleware/trace.go` 从上游头透传、长度不受本地约束）
  3. **降级通道重复计数**：把内存 logger 既作复合器的一腿、又作 `DBAuditLogger` 的 fallback 时，DB 每失败一次
     内存里就多一份重复行（实测降级 2 条后 `Count()=4`）⇒ 故障期 10000 条环形缓冲按 2 倍速被吃、`Count()` 翻倍。
     现 fallback 传 nil，"降级不丢"由复合器的内存腿兑现
  4. 告警按 60s 限速（一次 DB 故障否则能稳定产出 8.6 万行日志/天），并把 `enqueued/db_rows/fell_back/fail_batches`
     摊进快照——本 logger 对调用方永远返回成功，没计数的话"全部落库"与"一条没写"外面看是一模一样的
- 旗子 `FF_TOOL_AUDIT_DB` 刻意是**两态** `off|on`（默认 off），不同于另四把三态旗子：熔断/审批门/worker 会改变
  请求结果，需要"只看不拦"的中间档；本旗子只决定落不落库，`true/1/on/yes` 就认作 on。`TOOL_AUDIT_QUEUE_SIZE`
  默认 10000、上限 200000，越界回默认并告警。开旗但拿不到 DB 句柄 ⇒ 判 off 并告警，不留"看着开了其实没落"的中间态
- **建表落点无版本化迁移**：生产只跑 `db.AutoMigrate()`（启动期 `ExecuteUpgrade(v1.0.0, v1.0.0)` 是空跑），
  故 `allModels()` 登记即完成新老部署建表；再加一份永不执行的迁移只会多一个事实源（卡片原写"新增
  `v3_42_0_tool_audit_migration.go`"，实测 v3.42.0 已被并行会话占用，且该路径生产不执行 ⇒ 落点校正为不建）
- **计费不另建表**：`MemoryCostTracker` 的四样（次数/成功/失败/总耗时）是 `tool_call_audits` 按 `tool_name`
  聚合的真子集，再存一份必出两个事实源 ⇒ DB 口径走 `repository.ToolAuditRepository.CostAggregates`
- 观测入口：`GET /api/agent/tools/audit`、`GET /api/agent/tools/cost` 默认仍是内存（有界、快、不依赖 DB），
  `?source=db` 走持久化口径；两者**恒回显** `persistence{mode, db_wired, table, flag_env, db_handle, mem_entries
  [, queue_size, db_stats]}`，未接线时 `?source=db` 回 503 并指名旗子，而不是回一份空列表被读成"没有审计"。
  `/cost` 两种数据源 JSON 键逐一对齐 `tooluse.CostStats`，消费方不必因换源改解析
- ⚠️ **本表无保留期**：落库后从"最近 10000 条环形"变成每次工具调用一行、无上限增长。刻意不做自动清理——
  默认删审计证据比默认不删更糟；改由 `persisted_total` / `CountAll` 把规模摊开可见，保留期待运营拍板（同
  `reach_delayed_outbound` TTL 的处置口径）

### F3.3 MCP Server
零依赖 JSON-RPC 2.0（协议 2025-06-18），initialize/tools.list/tools.call/ping；仅 HTTP

## F4 RAG 检索

### F4.1 ThreeTier（实际4层，短路式）
L1 LRU热缓存(1024条/30min) → L2 温索引 → L3 冷索引 → L4 关键词(固定0.5)；命中即返回
- ⚠️ 层间短路非并行召回，可能漏冷库更优结果

### F4.2 HybridSearcher
向量 topK=50（pgvector 余弦 ef_search=128）+ BM25 topK=30（tsvector 自动探测，ILIKE 兜底）→ RRF k=60（0.7/0.3 权重）→ rerank 前20 → finalTopK=5
- 开关：EnableRerank=true、HyDE=false、MultiQuery=false、CandidatePool=100

### F4.3 配件
query_rewriter / multi_query_generator / hyde_generator / contextual_retrieval / incremental_indexer / translation_cache / redis+LRU 双缓存

## F5 知识库管线
EmbeddingDim=1024(BGE-M3 TEI localhost:8080)、TopK=5、相似阈值0.5、异步处理15min、SSRF校验5s、BM25扫描上限10000、失败回退 HashEmbeddingService

## F6 入站编排（SmartCSOrchestrator 九步）
①查建会话（OneID 合并，群聊 `group:{id}`）→ ②存消息(5s去重窗) → ③在线座席直通 → ④AI连续回复上限 **10次** 转人工 → ⑤紧急词转人工 → ⑥智能体选择（挂载>绑定>默认）→ ⑦SalesEngine.HandleWithAgent → ⑧置信度门槛 0.7（有卡片视为达标）→ ⑨落库计数
- ⚠️ extractConfidence 启发式兜底（0.5+加分）与 confidence/ 五信号体系割裂未打通

## F7 生成管线 + Agent Loop

### F7.1 SalesEngine 12 步
1_resolve_customer → 2_recall_memory → 3_recognize_intent → 3.5_transfer_check → 4_match_sop → 5_recall_rag → 5.5_match_script → 5.6_playbook_suggest → 6_generate_candidate(AgentLoop或直调) → 6.5_behavioral → 7_polish → 7.5_humanize_eval(阈值0.85, fail_soft)
- 并行模式 errgraph SetLimit(4)；假流式 4字符/15ms；Layer1 快路径 FAQ≥0.6 / SOP模板≥0.65 直接答

### F7.2 ReAct Agent Loop 护栏（sales_engine_agentloop.go）

| 参数 | 值 |
|---|---|
| maxIterations | 5（最小2） |
| maxTools | 18（白名单封顶30） |
| 总超时 | 180s |
| 单轮超时 | 60s |
| token 预算 | 50000 |
| 历史 | 最近20条 |
| finish_reason=length | max_tokens 翻倍重试一次 |

工具优先级表 41 具名（rag.search=1…），card.show 保底不被挤掉

## F8 意图识别

### F8.1 粗粒度 Recognize：13 类
price_inquiry/objection_price/objection_need/objection_trust/objection_competitor/objection_timing/purchase/ask_product/ask_service/after_sale/churn/social/complaint（+greeting常量⚠️不在词典/unknown 兜底）
- 规则：示例句等值 conf=0.95；关键词累加 0.7+len×0.02 封顶0.92；≥0.85 high
- LLM 兜底：JSONMode max_tokens=500 temp=0.2 + 实体抽取 + 情感三分类
- conf≥0.7 自动启动匹配 SOP
- ⚠️ IntentGreeting 常量永远无法被规则识别

### F8.2 精细 RecognizeIntent：8大类27子类
consult3/price_inquiry3/objection4/after_sale4/complaint3/churn3/intent_buy3/ask_product3
- 规则 conf = 0.5+权重×len×0.03 封顶0.92；<0.6 且 dispatcher 可用 → LLM 二次（method=hybrid）
- **投机识别**：规则同步 + LLM 后台 goroutine 异步投递(buffer=1, 60s)，主流程先走再收割升级

## F9 SOP 引擎

### F9.1 19 种节点类型
结构2（start/end）；销售话术9（greeting/inquire/introduce/handle/close/invite/follow_up/activate/nurture）；控制3（condition/branch/wait[timer/customer_reply/external, 默认24h, sop_timers]）；智能2（llm/ai_decide[LLM选下一跳, temp=0.3]）；旧版兼容3（message/action/send_offer）
- 内容解析链：prompt模板{{var}} → config.content → LLM生成(max_tokens=200,temp=0.7,受全局信号量) → 类型默认话术11条
- 幂等：`message_sent:{exec_id}:{node_id}` 副作用键

### F9.2 Dispatcher
WorkerCount=16、QueueCapacity=1000背压拒绝、LLMConcurrency信号量=4、任务超时5min、重试3次指数退避1s×2封顶30s；waiting 挂起待 outbox 唤醒；事件日志唯一约束幂等
- Saga 补偿已接线（T-P1-02，2026-09-19）：`InitSOPCompensation` 按 `FF_LTC_SAGA_COMPENSATION`（默认关）注入
  `CompensationManager`；开旗后失败执行按 LIFO 撤销前序副作用（wait→删 pending 定时器，llm/ai_decide→清本节点产物键），
  内存计划受 `MaxPlansKept=512` 上界约束、退出前打 Summary。补偿动作只回滚内部状态、无出域 ⇒ 关旗行为与接线前一致
- 补偿语义收口（T-P1-03，2026-09-19）：19 种节点类型按"有无内部状态可撤销"三分，由
  `TestNodeExecutor_CompensationInventory` 穷尽锁死（注册集合 + 可补偿分区 + 无沉默类型）：
  ① **可全量补偿** 3 种（`llm`/`ai_decide` 清本节点产物键、`wait` 删 pending 定时器）；
  ② **部分补偿** 12 种（9 销售话术 + 旧版 `message`/`action`/`send_offer`，均继承 `MessageNodeBase`）——
  清 `ExecutionData` 里的话术产物，但**有意保留 `message_sent` 幂等键**（防重跑二次发送）、
  **不撤回已出域消息**（出域须走 T-P3 审批闸门）；
  ③ **无可撤销状态** 4 种（`start`/`end`/`condition`/`branch` 只写时间戳与分支标签留痕，`Noop` 是未注册兜底）
  —— 不实现 `Compensable`，改为实现 `CompensationNoter` 自述理由，随 `CompensationRecord.Reason` 进入补偿计划与结构化日志
  （`node compensated ... reason=` / `node not compensable, skipped ... reason=`）；补偿计划目前仅存内存（受上界淘汰），
  DB 侧全量留档尚未落地，属 T-P1-08
- 口径登记：**接口断言型接线（`executor.(Compensable)` 运行时类型断言）不入 grep 基线**——
  `check-unwired-assets.sh` 探不到断言，这类接线的退化由上述契约测试盯梢
- 配套：scheduler(60s tick)/outbox_dispatcher(batch=100,StuckDetector)/abtest(Variant权重分流)/condition 表达式求值

## F10 置信度与转人工（confidence/ 15文件）
- 五信号加权：Intent 0.30 / Entity 0.15 / CtxRelev 0.15 / RAG 0.20 / Entropy 0.20（可热更归一化）
- 校准链：Temperature Scaling × Platt Scaling(0.5/0.5混合) → Conformal Predictor（覆盖率保证，超分位数升级人工）；另有 Beta Calibration、黄金分割寻优
- VetoChain 六否决按序：Explicit → Complaint → Loop → LowEntity(<0.2) → LowRAG → HighEntropy(<0.2)，触发即 conf=0 强制 handoff
- 动态阈值 T = base[intent] + 0.05×客户等级 + 0.05×时段 + 0.10×座席空闲度，clip [0.40,0.95]
- 四档决策带：<0.40 handoff / <0.60 llm_fallback / <0.75 review / ≥0.75 auto

## F11 拟人化三层

### F11.1 文本润色 HumanizePolisher
去开头客套/AI痕迹词/多余符号/平台emoji正则清除/长度截断/个性化称呼/场景 emojiPool

### F11.2 五维拟人度评估（humanize/）
Naturalness/Conciseness/Empathy/Professionalism/Persuasiveness
- 规则分（<1ms）：自然度 base0.85 −AI痕迹词0.30/个 −burstiness<0.3 再−0.15 +语气词≤0.10；简洁性按意图期望字数区间 0.85-1.00；投诉20关键词无共情词直接0.30；专业度 base0.50+专业词0.30+销售词0.20；说服力 base0.40+CTA0.30+利益词0.30
- 编排：阈值 **0.85**(PRD G6)、采样区[0.70,0.85)、LLM复评采样率10%、重生上限3次；LLM打分 N=3 temp=0.3 可对照销冠基线；fail_soft 不阻断下发 + 低质样本采集10类
- AI检测器：困惑度+熵 sigmoid

### F11.3 行为层 BehavioralHumanize（默认关闭，A/B灰度）
分条发送 >80 字符按标点切（句号优先，<8字符合并）；片段间隔 1.5s×jitter(0.8~1.2)；打字延迟 25字/s；思考停顿非首条+3s；错别字注入 3% QWERTY 邻键（默认关）

## F12 异议处理
7类 price/need/trust/timing/compare/feature/other；纯关键词首中即返回 conf 硬编码0.85；script_library 取5条按 UsageCount 降序；RecordUsage 形成转化率学习

## F13 自学习闭环

| 子系统 | 机制 | 关键参数 |
|---|---|---|
| FeedbackLearner（实时） | defer 上报 feedback_records + 内存 intentCache/sopCache | — |
| ChampionDialogueAnalyzer | 销冠对话 embedding 聚类 + LLM 提炼话术入库 | — |
| PromptIterator | 负样本→LLM生成prompt候选→自动建SOP节点A/B | 版本号递增 |
| SOPAutoOptimizer | 5类动作 branch_prune/node_merge/add_objection/add_empathy/timing_adjust | — |
| BanditAllocator | Thompson Sampling Beta(α,β) | 冷启动<30样本均匀随机、探索下限10%、单臂上限60%、收敛95%、晋升100样本、后验采样1000次 |
| trace_learning | 每小时批200条 LLM四维评审(relevance/accuracy/usefulness/safety 0-100) | bad→weight×0.85、≥85→×1.12、clamp[0.1,3.0]、向1.0均值回归10%、dry-run |
| abtest_loader | DB加载AB方案，回退 default-greeting-ab 2变体50/50/14天/min1000 | — |

## F14 多智能体
3 类型 sales/customer_service/hybrid × 2 模式 passive(SmartCSOrchestrator 已实现)/active(lifecycle 骨架就位待落地)；路由：座席挂载>渠道绑定>默认；工具授权双层（注入期过滤+执行期校验）

## F15 评测
ChrF 字符 n-gram + LLM Judge 主观评审；EvaluateBatch/EvaluateSingle

## F16 流失预警与挽回（W-5，2026-09-19 接线）
RFM/Churn 定时重算（`customer_rfm.go` 周批）→ `churn` 分层自动入队 `recovery_queue` → **消费侧 worker 发出**。
消费侧此前不存在：`ListReadyForAttempt` 只被只读路由调过，队列只进不出。

- **入队两条路**（语义不同，别混）：① `CustomerRFMService.enqueueRecovery` 自动入队，**只带 reason/strategy/priority，
  不带任何文案**（`meta_json` 走列默认 `'{}'`），`MaxAttempts` 不写 ⇒ 吃列默认 3；② admin `POST /recovery-queue/enqueue`
  显式入队，可带 `content/subject/template_id/params/preferred_channels/max_attempts`。
- **消费者** `internal/service/recovery_queue_worker.go`，装配点 `internal/app/recovery_worker_wiring.go`
  （由 `cmd/api/main.go` 触发）。开关 `FF_LTC_RECOVERY_WORKER` **三态**：`off`（默认，`Start` 直接 no-op）/
  `shadow` / `enforce`，**布尔真值降级为 `shadow`**（与熔断旗子同纪律）。节奏：`LTC_RECOVERY_WORKER_INTERVAL`
  默认 5m（下限 30s）、`..._BATCH` 默认 20（上限 500，即 AC④ 的单轮上限）、`..._BACKOFF` 默认 24h。
- **到期口径**（repo 既有，未改）：`stage='queued' AND attempts<max_attempts AND (next_attempt_at IS NULL OR <=now)`，
  排序 `priority ASC, next_attempt_at ASC NULLS FIRST`。
- **外发唯一出口是 `ProactiveReachService.ReachByCustomer`**，所以渠道可用性排序、全局退订（DNC）、60min 触达冷却
  全部自动继承，worker 自己不另写一套频控。DNC 命中 ⇒ `stage=cancelled`（不再重试）；冷却命中 ⇒ **只推后、不消耗 attempt**。
- **shadow 的副作用为零**：请求带 `DryRun=true`，走完整选路后在发送前返回，不取认领锁、不写台账，只累计
  `would_send/would_fail`。
- **认领锁** `mtk:recovery:claim:<id>`（`SetNX`，TTL 10min）：拿不到或出错一律**不发**（fail-**closed**）。
  这与 reach 侧的冷却/DNC 判定方向**刻意相反**且都写进注释——那两处依赖出错时 fail-**open**（Redis 抖动不该静默吞掉
  一次本该发的触达），而这里"判定不了就发"等于对同一客户重复外发。`cache.GlobalIsRedis()` 为假时锁退化为进程内，
  `Start` 显式告警"认领仅在单进程内有效"。
- **`uq_recovery_active` 在生产库不存在**（实测，非推断）：该部分唯一索引只写在 HP1 迁移 DDL 里，而生产建表走
  `db.AutoMigrate()`、启动期迁移固定 `ExecuteUpgrade(ctx,"v1.0.0","v1.0.0")` ⇒ 实测 `recovery_queue` 只有 4 条索引、
  重复活跃插入返回 `<nil>`。"一客户一条活跃"仅由 `recoveryQueueRepo.Create` 读后写保证（跨进程有竞态），
  这正是上面那道认领锁存在的理由。
- **发出去 ≠ 挽回来**：末次成功发送只把 stage 推到 `running`（等结果，既离开到期集合、又仍算活跃行），
  `succeed` 只能由成交侧经 `MarkRecovered` 判定，worker 永不自证成功。
- ⚠️ **`enforce` 不会轰炸未配文案的客户**：RFM 自动入队的行 `meta_json` 无正文 ⇒ 一律走"无内容跳过"分支
  （推后 `backoff`，不消耗 attempt）。真发需要先经 admin 入队带上内容，或给存量行补 `meta_json`。
- ⚠️ **本路径不经过 W-1 审批门**（见 G13）：冷触达审批门挂在工具 executor 建链点，`ProactiveReachService` 没有
  pre-send 钩子、且 sms/email 的 accountID 恒为空 ⇒ 结构上无处可挂，cron/worker 侧外发目前是闸门的盲区。
- 观测：每轮结束打一行 `[RecoveryWorker] 本轮结束 mode=… 到期=… 已发=… 无文案跳过=… 退订终止=… 冷却推后=…
  失败记账=… 锁被占=… 取锁不可用=… 台账写失败=…`（shadow 追加"预计可发/预计失败，均未发出未记账"）；
  日志**只含 customer_id 与渠道，不含手机号/邮箱**。计数目前只在日志里，未出口成端点（与 W-1/W-3 的
  `/api/agent/tools/*` 观察端点相比是缺口，登记在待办）。

---

## 已知短板汇总

> 口径提示：下表是 2026-08-25 源码精读快照。"已实现未接线"类条目（G1 等）的**当前接线状态以
> `scripts/check-unwired-assets.sh` 实跑输出为准**（退出码 0=与登记一致 / 1=状态已变，须回灌本表 / 2=登记符号已消失）；
> 该脚本每接完一条就把对应行的 `expect` 改为 `wired`，此后它反过来盯"已接线的别偷偷退化"。
> **例外**：接线形态是运行时类型断言（如 `executor.(Compensable)`）时 grep 探不到，该类条目不由本脚本盯梢，
> 改由穷尽性契约测试锁定（见 F9.2 的 T-P1-03 登记）。

| # | 位置 | 问题 |
|---|---|---|
| G1 | sop_dispatcher.go:696 | 补偿器未注入：`SetCompensationManager` 生产零调用（仅测试引用），单例 dispatcher 在 :840 构造时不带管理器 ⇒ `tryCompensate` 恒在 697 行 nil 早退，失败路径无 SAGA 回滚。原记根因"缺 executed_nodes 列"已于 v3.29.0 补列（`model/ai_sales_champion.go:159`，text 非 JSONB），现仅剩接线缺口 **【T-P1-02 已接线（2026-09-19）】`service.InitSOPCompensation` 在 `cmd/api/main.go` 按 `FF_LTC_SAGA_COMPENSATION`（默认关）注入；接线过程实测出机器本身两处缺陷并已修：① `plans` 只增不减（进程级单例每次失败泄漏一条计划，故障风暴下无界）→ 新增 `MaxPlansKept` 淘汰；② `Run` 无锁改写已发布计划 + `GetPlan` 交出活指针 → `-race` 报 3 处 DATA RACE，现改写走短临界区、`GetPlan` 返回快照。另有 `sop_compensation_integration_test.go` 那批"集成测试"是零断言空跑烟测（构造 exec 不落库 ⇒ 恒早退），真实失败路径断言补齐在 `sop_compensation_wiring_test.go` **【T-P1-03 语义收口（2026-09-19）】**卡面要求"给 `StartExecutor`/`EndExecutor`/`ConditionExecutor`/`NoopExecutor` 补 `Compensate`，6/6 实现"，实跑证明这四类无可撤销状态，补空实现会把记录从 `skipped` 伪造成 `completed` 并让 `Compensable` 断言失去判别力 ⇒ 落点改为"6/6 有可断言的补偿语义"；原 `sop_node_executors.go:681` 记 TODO 的 12 个消息类节点已实现部分补偿，四分区与理由见 F9.2 |
| G2 | dispatcher_dispatch.go:368 | MultiModelVote 无真实一致性投票 |
| G3 | intent_recognition.go:98 | greeting 不在词典，规则永远识别不出 |
| G4 | smart_cs_orchestrator.go:537 | extractConfidence 启发式与五信号体系割裂 |
| G5 | layer.go:111 | 空 if 死代码 |
| G6 | workflow_node_executors.go:328 | 嵌套工作流 TODO |
| G7 | behavioral 默认关 | 行为拟人未灰度上线 |
| G8 | objection_handler.go:96 | 分类置信度硬编码0.85，首中即返回 |
| G9 | three_tier.go | 短路式检索漏冷库更优结果 |
| G10 | fallback_tree | 模板兜底文案单一 |
| G11 | active 模式 | 主动触达骨架未落地 |
| G12 | eval 包 | ChrF+LLM Judge 较薄，无对话级端到端评测集 |
| G13 | `service/proactive_reach.go` | **审批门盲区**：闸门挂在工具 executor 建链点，`ProactiveReachService` 无 pre-send 钩子 ⇒ cron/worker/直接 API 调用三条非工具路径的外发一律不受 W-1 约束（T-P1-07 的挽回 worker 即此类）。修法是在 `ReachByCustomer` 发送前加一个显式 checker 钩子，而不是在 worker 里伪造一个键为空的判定 |
| G14 | `service/order_draft.go` / 表 `order_drafts` | 草稿持久化底座已落地（**T-P2-01，2026-09-19**）：内存 map 换成 `draftStore` 两副面孔（内存默认 / DB durable），新增 `model.OrderDraft` + `repository.OrderDraftRepository`；`ExpireOverdue` 由"只计数不翻转"改为落库翻转，并新增 `PurgeTerminal` 有界清理；`Confirm`/`Cancel`/`Edit` 的读—判—写收进 `FOR UPDATE` + 部分唯一索引 `uq_order_draft_pending`（原实现两个销售同时点确认会一单变两单）。**但整条销售草稿竖今天没有生产构造点**：`NewOrderDraftService*` 在 `internal/app`/`cmd/api` 零调用 ⇒ 今天不会有任何草稿真的写进 `order_drafts`（表会一直是空的）。该状态由 `check-unwired-assets.sh` 项8 按 `unwired` 登记盯梢，接线与观察端点属 **T-P2-06** |
