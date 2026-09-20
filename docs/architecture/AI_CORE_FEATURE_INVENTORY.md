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

- **答案缓存的版本与灰度**（T-P2-05，2026-09-19）：`knowledge_bases` 新增 `version`/`canary_enabled`/`canary_percent`
  三列，唯一生效面是 `rag_answer_cache.prompt_version` 的命名空间选择（**不是**内容副本、**不是**行复制，理由见 G18）。
  决策口 `service.KBAnswerVersionFor(kb, oneID)`，调用点在 `SmartCSOrchestrator` 的 FAQ 缓存读/写处（读键与写键同源，
  中途抬比例不会把同一会话劈成两个版本）。开关 `FF_LTC_KB_CANARY` **三态、默认 off**：`off`/`shadow` 恒用挂载前的
  字面量 `v1`（shadow 只记日志不换键），只有 `on` 才允许 `version`/`version+1` 两个命名空间并存；
  行上灰度还要 `canary_enabled=true` 且 `0<percent<=100` 且 OneID 非空（匿名流量不放量，否则全塌成同一个桶）。
  分桶复用 `feature_flag.go:flagBucketHash`（FNV-1a，key=`"kb.{id}"`），不新造第四套口径。
  转正=灰度组指针前移，回滚=把 `version` 改回旧号，**两个方向的已缓存答案都不需要重灌**；三列写入只走
  `UpdateVersionCanary`（`UpdateColumns`，不 bump `updated_at`——那列是缓存失效信号）。管理端：
  `GET /api/knowledge-bases/:id/versions`、`POST /:id/version`、`PUT /:id/canary`

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
结构2（start/end）；销售话术9（greeting/inquire/introduce/handle/close/invite/follow_up/activate/nurture）；控制3（condition/branch/wait[timer/customer_reply/external/approval, 各档默认 24h, `sop_timers`]）；智能2（llm/ai_decide[LLM选下一跳, temp=0.3]）；旧版兼容3（message/action/send_offer）
- `wait` 的 `approval` 档（T-P3-02，2026-09-20）：`config.wait_event: "approval"` 必配
  `approval_policy_key`（**缺则节点判失败**，不默认任何策略名——默认等于替图作者做一次授权决定），
  可选 `approval_ttl_seconds`（默认 24h，越上限判错不夹取）。挂起=插一行 `sop_timers`
  （`wait_until` 与审批行 `expires_at` 同源、`max_wait_at` 刻意留 NULL：一行只允许一条到期路径），
  结论由点火时回读写进 `ExecutionData` 的 `_approval_status` / `_approval_allowed` / `_approval_by`
  / `_approval_note` / `_approval_id` / `_approval_derived`，分支条件按此路由
  （`_approval_status eq approved|rejected|expired`）；读不回结论落 `unreadable` 且**不放行**。
  `subject_type` 固定为 `sop_node`、`subject_id` 为 `<执行ID>--<节点ID>`，**不从图配置读**：
  填了别的值会出现"审批批了但没人被叫醒"。整档由 `FF_LTC_APPROVAL_RESUME` 定档（off=判失败、
  shadow=只清扫不挂起、on=真挂起），详见短板 G20
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
| G14 | `service/order_draft.go` / 表 `order_drafts` | 草稿持久化底座已落地（**T-P2-01，2026-09-19**）：内存 map 换成 `draftStore` 两副面孔（内存默认 / DB durable），新增 `model.OrderDraft` + `repository.OrderDraftRepository`；`ExpireOverdue` 由"只计数不翻转"改为落库翻转，并新增 `PurgeTerminal` 有界清理；`Confirm`/`Cancel`/`Edit` 的读—判—写收进 `FOR UPDATE` + 部分唯一索引 `uq_order_draft_pending`（原实现两个销售同时点确认会一单变两单）。**但整条销售草稿竖今天没有生产构造点**：`NewOrderDraftService*` 在 `internal/app`/`cmd/api` 零调用 ⇒ 今天不会有任何草稿真的写进 `order_drafts`（表会一直是空的）。该状态由 `check-unwired-assets.sh` 项8 按 `unwired` 登记盯梢，接线与观察端点属 **T-P2-06**。**（T-P2-06 已落地，2026-09-19：上面那句"零调用"就此失效——`internal/app/order_draft_wiring.go` 的 `InitOrderDraftRuntime` 是唯一装配入口，读 `FF_LTC_ORDER_DRAFT_DB`（`off|shadow|on`，默认 off）；抬旗前 `order_drafts` 仍一行不写，那是上线当天的现网形状而非缺陷。项8 四行随之转 `expect=wired` 防回退；装配后仍缺的读侧/售后侧注入点另登为项11，全貌见 G19。）** |
| G15 | `service/integration.go:1070` `UpsertOrderFromWebhook` / 表 `external_orders`、`webhook_events` | **签名门修好之后才看得见的下游缺陷（T-P2-02 交接：实测取证，本卡未动这套写入语义）**：① `external_orders.order_id` 带**全局 unique**（`model/integration.go` 的 gorm tag），而读写都按 `platform + order_id` 定位 ⇒ 两个平台用同一个订单号时第二条回调必撞 23505：实测 A 平台落库成功，B 平台同名 `order_id` 返回 `duplicate key value violates unique constraint "uni_external_orders_order_id"`、表里只有 1 行、接口回 500 **（一次签名完全合法的回调会静默丢单）**。收口要把唯一键改成 `(platform, order_id)`，而 `AutoMigrate` 不删旧索引 ⇒ 需要一次显式 drop，属 schema 变更，归 **T-P7-02**（该卡本就要给这张表扩展账单语义）。② `existing, _ := GetByOrderID(...)` 把读库错误吞成"未找到"，且该 repo 在 `ErrRecordNotFound` 时同样返回**非 nil 空结构体** ⇒ 判据只剩 `ID==0`，一次瞬时读故障会把"更新"变成"插入"。③ 无状态机守卫：实测 `paid` 会被后续 `created` 无条件覆盖回退（订单镜像可倒退）。④ `webhook_events` 侧：`_ = Create(...)` 吞错、`EventID` 含 `UnixNano` ⇒ 每次投递都是新行，重放（尤其 nonce 存储 fail-open 那一轮）不留去重痕迹，且 `Processed:true` 在真正处理之前就写死 |
| G16 | `ops/service/conversion_funnel.go` `BuildFunnel` / `GetStageDetails` | **漏斗真实源的两处读数口径（T-P2-03 实测登记，本卡刻意不改——改它等于改现网看板上的数字，需要先定"宁可少给数据还是要吵"）**：① 四个 count 的错误**全部**被 `_` 吞掉 ⇒ 任一数据源表不可用（`intent_records` 由迁移建表、被禁止进 AutoMigrate 清单，新库上它可能压根不存在）时接口仍回 200，该阶段显示为 0，报表侧读成"转化率偏低"而不是"取数失败"；② 阶段之间无单调性约定 ⇒ `rate` 可 >100、`drop_rate` 可为负（`conversion_funnel_baseline_test.go` 的 `_NonMonotonicStagesBaseline` 按现状钉住，另有一条 `_UnknownStageIsBaseline` 钉住"未知阶段回 200 + 空名字 + 0 计数、不判 404"）。两处都属"数据可信度"问题 ⇒ 归口建议：任何拿这份漏斗做经营决策的卡（T-P4 漏斗收口、T-P8 指标看板）开工前先定口径，本卡只保证**改动前后逐字节相同**（差分实跑见任务清单 r20） |
| G17 | `model/sales_event.go` / 表 `sales_events` | **整张销售事件流今日在生产路径上零写入（T-P2-04 实测登记）**：唯一的生产构造点 `NewSalesEventStatsService(` 在非测试代码里 0 命中，注入点 `SetStats(` 亦 0 命中，而已接线的 `FollowUpService` 走 `if s.stats != nil` 保护（stats 恒 nil）⇒ 销售工作台/业绩聚合读到的行只可能来自测试与 seed。该状态由 `check-unwired-assets.sh` **项 9** 按 `unwired` 登记盯梢，**T-P2-06 实测未覆盖它**（该卡只接草稿竖：装配入口、清扫节拍、观察端点，`NewSalesEventStatsService(` 与 `SetStats(` 在其改动后仍各为 0 生产调用点）⇒ 在有人认领前不得宣称"商机事件已入库"。**同卡新开两列 LTC 预留位** `opportunity_id` / `quote_id`（可空、不回填、暂无索引、暂无生产者），口径写在类型文档 a/b/c：`NULL`＝该行早于"商机"概念、`''`＝有概念但这次没商机，而 **GORM 读回时两层塌成同一个空串**（要区分只能裸 SQL，`sales_event_ltc_columns_test.go` 的 `_NullVsEmptyStringAreTwoLayers` 钉住）；`_NoIndexYet` 断言"暂无索引"，P4 加 `idx_sales_events_opportunity` 时它转红是**预期中的红**，须与类型文档 c) 和 schema 文档 §4.12 同步改 |
| G18 | `model/knowledge_base.go` / 表 `knowledge_bases` | **知识库主表不在生产检索路径上（T-P2-05 开工前实测，直接决定了本卡的落点形状）**，四条独立证据：① FAQ 取数按 `agent_id`（`faq_entries` **根本没有 kb_id 列**，`model/faq_entry.go:43-63`），RAG 取数按 `product_id`（`ragretrieval.VectorRetriever.SearchVector` 那个形参名叫 kbID 的入参，实际拼进 `AND product_id = ?` 打的是 `knowledge_chunks.product_id`，见 `vector_retriever.go:45-79`；该表同样没有 kb_id）⇒ **两张内容表与 `knowledge_bases` 之间没有任何外键或列级关联**，删掉 KB 行不会动到一行内容，反之内容检索也读不到 KB 行；② 唯一按 kb 维度过滤的入口 `FAQService.MatchByAgentKB(ctx, agentID, kbIDs, …)` 把 `kbIDs` 显式弃成 `_ = kbIDs`（`service/faq.go:223-241`），且在非测试代码里**零调用**；③ 唯一把 `kbID` 一路传到检索层的编排 `rag_customer.go` 整包无生产装配点；④ 全仓只有 `resolveFAQKB`（原 `resolveFAQKBID`）真的去读 `knowledge_bases` 行，而它读出的 ID 只喂 `rag_answer_cache`。⇒ **"给知识库加版本"若按字面做成分版本的内容副本，等于给一条没人读的路径加权重**，所以 T-P2-05 把版本落在唯一按 kb 维度生效的物理载体上＝`rag_answer_cache.prompt_version` 命名空间（生效面口径见 `model.KnowledgeBase` 类型文档 a）。**同轮实测出的独立缺陷（本卡只登记不修）**：答案缓存的失效信号是 `knowledge_bases.updated_at`（`cache/service.fresh()` → `pgKBMetaReader.GetKBUpdatedAt`，`store_pg.go:39-53`），而该列**只有 `KnowledgeBaseController.Update` 改元数据时才会 bump**——新增/修改 FAQ 条目、上传或删除文档、重灌 chunk 全都不经过 `UpdateKB`（实测：`UpdateKB(` 在非测试代码里只此一处调用点）⇒ **改内容不会让任何一条已缓存答案失效**，旧答案会一直按 Tier1/Tier2 命中回给用户，缓存对内容改动是永久失明的。修法要么在内容写路径上显式 touch 父 KB 行，要么把失效判据换成内容侧的指纹（chunk/entry 的最大 `updated_at`）；后者要新增一次按 kb 维度的聚合查询，属改变每次会话热路径开销的决策，**留给 P3 之后的缓存收口卡**，本卡确保的是：版本/灰度写入**不**走会 bump `updated_at` 的通用 `Update`（用 `UpdateColumns`），否则切一次版本就把另一版本的缓存行全删了，AC② 的"回滚不重灌"当场不成立（`kb_version_canary_test.go` 的 `_KeepsUpdatedAt` 用"通用 Update 会 bump、专用方法不会"的对照断言把这条锁死） |
| G19 | `internal/app/order_draft_wiring.go` / `internal/service/order_draft_sweep.go` / 表 `order_drafts` | **草稿竖"已装配"这句话的准确边界（T-P2-06，2026-09-19 实跑登记）**。抬 `FF_LTC_ORDER_DRAFT_DB` 之后接通的是**生产者侧三件事**：AI 回复→意向提取→建草稿（`BuildSmartOrchestrator` 注入）、到期/终态清扫按 `DefaultOrderDraftSweepInterval` 节拍跑（`ExpireOverdue` + `PurgeTerminal` 的定时调用方，本卡之前它是一个没人调的常量）、观察端点 `GET /api/agent/order-drafts/stats`。以下六件事**没有**随本卡成立，引用这条竖时必须按这个边界说：① 旗子默认 `off` ⇒ 上线当天现网形状不变、`order_drafts` 仍零写入；三态只在装配期读一次，改档须重启。② **档位切换不搬迁在途草稿**：内存副是进程内 map，`shadow→on` 之后旧进程内存里的 pending 草稿不会自己出现在库里（影子期写进去的镜像行只是对照数据，权威那份在切档那一刻仍只有内存有）⇒ 切档窗口内"库里查不到"不等于"没有草稿"。③ **读侧/售后侧/成单侧三个注入点仍零生产调用**（`SalesWorkbenchService.SetDraft`、`SalesActionTrigger.SetDraftService`、`OrderDraftService.SetOrderService`）⇒ 工作台待办里永远没有草稿项、售后触发器不产草稿、`Confirm` 只能回 "orderService 未注入"（实测：它**明确报错**，不会静默造一条假订单）。三行已登为闸门**项11** 按 UNWIRED 盯梢，兑现卡在清单里**未指派**。④ `StopOrderDraftRuntime` 有入口、**无生产调用方**：优雅停机属 `cmd/api/main.go`，而该文件与 `BuildSalesEngine` 同属并行会话正在改的路径、本卡改动集刻意不含它 ⇒ 进程退出时清扫 goroutine 由 OS 回收，不会留"正在收尾"的日志。⑤ **草稿不写 `sales_events`**：`OrderDraftService.SetStats` 生产零调用 ⇒ 五处 `if s.stats != nil` 全走假分支，"AI 谈单产出的草稿可复盘"今天只到表为止（同 G17）。⑥ 提取器在两条生产路径上**都不填 OneID** ⇒ 草稿行的 `oneid` 恒空，跨端识别只能靠 `customer_id`。观察端点的两个口径细节一并记此：`counts` 为 `null`＝本进程压根没读（旗子关着）、为 `{}`＝读了且真的一条都没有；装配在位而底座读不动回 **503** 而不是空计数（与 T-P1-08 审计读侧同口径） |
| G20 | `model/approval_request.go` / `repository/approval_request.go` / `service/approval_request.go` / 表 `approval_requests` | **C2 裁定的"只建一套"已经建好；T-P3-02 已把挂起端与恢复端接上（`sop_timers` 审批档 + 点火回读 + 到期清扫），但总开关 `FF_LTC_APPROVAL_RESUME` 默认 off ⇒ 不翻旗子时这张表在生产路径上仍零写入（表由 T-P3-01 建于 2026-09-19、接线由 T-P3-02 成于 2026-09-20，均实跑登记）**。成立的部分：四态状态机 `pending/approved/rejected/expired` 及其**唯一跃迁表**（三个终态各自出边为空 ⇒ 改判无路，要重审只能新建一条并留下两条记录说明过程）、幂等键 `(subject_type, subject_id, policy_key)` 配部分唯一索引 `uq_approval_request_open`（`WHERE status='pending'` ⇒ 裁决落定即让坑，被拒对象修正后可重审）、AC② 的 auto-approve 快速路径（**以 approved 落库**而非"先 pending 再改"，不留一个能被清扫撞见的窗口；`expires_at` 为 NULL、`resume_token` 留空，故凭证索引必须是部分索引 `<> 空串`）、`resume_token` 与 `id` 刻意分离（ID 进日志与列表，凭证是能力，须 crypto/rand 不可枚举）、TTL 默认 24h 上限 30d（越上限**判错不夹取**）、并发裁决走**带状态条件的列白名单写回**（实测 8 协程同一行只有 1 个赢家；`FOR UPDATE` 买的是"fn 执行期间这一行不许被别人改"，Mu-R3 实测摘掉行锁后并发用例仍绿 ⇒ 单赢家的功劳记在 CAS 上、行锁另有一条直接观测用例），列白名单的意义是**身份列改不动**（改得动就等于用一次合法批准给另一张报价单开门）。**接线现状（T-P3-02，2026-09-20 实跑登记）**：装配入口已有构造（`internal/app/approval_runtime_wiring.go`，由 `router.Setup` 按 `FF_LTC_APPROVAL_RESUME` 调用）、恢复读入口已有调用（`ApprovalResumeBridge.ResolveOnFire` 在定时器点火那一刻回读结论）、到期清扫已有节拍器（`ApprovalSweepWorker.RunOnce` 每 5 分钟调一次 `ExpireOverdue(ctx, 200)`）⇒ 闸门 **项12a/12b/12c** 已从 UNWIRED 翻成 **wired**（防回退：从有线掉回无线才算红）。但"接上了"不等于"开着"：off 档 `InitApprovalRuntime` 直接返回 nil（表仍零写入、图里的审批等待节点判失败），shadow 档只清扫到期、不装挂起桥（没有任何流程会因这次装配停下来等人工），只有显式 `on` 才启用挂起/恢复；开关在装配层、基线看不见它，三档语义由 `internal/app/approval_runtime_wiring_test.go` 逐档钉住。**以下四件事仍不随本条竖成立，引用这条竖时必须按这个边界说**：① `AutoApprovalPolicy` 只是判据**入口**，本卡刻意**没有**把它接到 `approval.WhiteListApprovalChecker`：从 `(subject_type, subject_id)` 映射到 `(tool_name, account_id)` 是调用方的知识（报价的 subject 是 quote_id、外发的 subject 是 reach_plan_id，账号来源各不相同），现在接等于替三个还没写的调用方各自代设计一次；② `decorator_approval.go` 一字未改（C2 明令禁止在其上叠加第二个布尔接口）⇒ 现网跑着的仍是 T-P1-06 那套"白名单 + `FF_LTC_APPROVAL_GATE` 三态"的**进程内**闸门，本表与它是**并存而非替代**，把旗子切到 `block` 的后果不经过这张表；③ 本表**没有保留期清理**（无 `PurgeTerminal` 对等方法），已裁决行只增不减 —— 它与草稿不同属证据表，"能不能删裁决记录"是未拍板的决策，须由认领 P3 收口的人先定，本卡不擅自加删除路径；④ 无 HTTP 端点、无待办中心视图，`AllowedTransitions` 只是给后续视图准备的只读出口。另记一处**写路径上的实测发现**（不是设计意图）：GORM 会从 `Model(&x)` 的主键值再拼一条 `id = ?` 进 WHERE，而裁决用的正是刚被 fn 改过的那个实例 ⇒ 若 fn 动了 `m.ID`，两条 id 条件互斥、更新静默命中 0 行，服务便会对一条**仍然 pending** 的记录报出"已由他人裁决"。仓储因此把写回目标改成空壳 `Model(&model.ApprovalRequest{})`，只认传进来的 id（`_MutateCannotRepointIdentity` 钉住） |
| G21 | `model/human_task.go` / `repository/human_task.go` / `service/human_task.go` / `controller/human_task.go` / `router/human_task_routes.go` / 表 `human_tasks` | **C3 裁定的"统一数据模型 + 分离视图"只落了前半句（T-P3-03，2026-09-20 实跑登记）**。成立的部分：一张表三类待办（`kind ∈ {conversation_handoff, approval, collection_escalation}`），四态状态机 `pending/claimed/done/cancelled` 配**唯一跃迁表**（`humanTaskActionTargets`）+ **动作×类别**允许表（`humanTaskActionKinds`：`claim`/`release` 只对会话类开，审批与催收只准 `complete`/`cancel` —— C3 那句"审批不可抢占只能裁决"落成了数据而不是散落的 if），三档 SLA **分列不分表**（`sla_first_response_at` 分钟级 / `sla_decide_at` 小时级 / `sla_escalate_at` 天级，填错列在写入口直接判拒，AC① 的隔离由此可测）；**刻意没有 `expired` 状态** —— 逾期不是终态，翻成终态会让它从待处理列表消失而底下那件事一件没少，正确表达是"SLA 列上的时刻已过去"；幂等键 `(subject_type, subject_id)` 配部分唯一索引 `uq_human_task_open`（谓词写成**终态否定式** `status <> 'done' AND status <> 'cancelled'`，既是"开放态整体占坑"的方向选择，也是硬约束：GORM 按逗号切 tag，`IN ('pending','claimed')` 会在 AutoMigrate 阶段炸成 42601，本卡实测）。生产投递方**已接线**：`transferToHuman` 在会话转人工后同步投一条（`internal/service/smart_cs_orchestrator.go` 的 `produceHandoffTask`，5s 上界 + `context.WithoutCancel`，AC② 的"不重复投递"实测两次转人工只 1 行、`Complete` 后再转则 2 行）；会话 `resolved`/`closed` 时经 `cancelOpenHumanTaskForSession` 自动撤销在途待办；读口七条端点挂在鉴权后的 `/api/human-tasks`（列表按 kind 过滤 + `/counts` 未读与逾期聚合，AC③）。**以下七件事不随本条竖成立，引用这条竖时必须按这个边界说**：① 三类 kind 今天**只有一类有生产 Submit** —— `approval` 类待办的事实源仍是 §4.14 那张 `approval_requests`（两张表、两套状态机、彼此不感知），`collection_escalation` 的催收竖未开工 ⇒ "三类统一收口"这句话只成立一类，闸门基线 **项13** 的注释里已预留 13e/13f 待补登；② **"分离视图"今天只是 API 侧的 kind 过滤参数**，没有两个前端视图（`user-web` 在本卡改动集之外），坐席看到的仍是收件箱那套 websocket 推送 + `customer_sessions.agent_id`，`human_tasks` **不推送**，未读数只能轮询 `/counts`；③ **待办认领与会话认领是两套不同步的归属事实**：`human_tasks.assignee_user_id` 与 `customer_sessions.agent_id` 各写各的，`release` 一条待办不会把会话从座席身上摘下来，会话改派（`AutoAssignAgentTx` 在 `transferToHuman` 里排在待办投递**之前**）也不会撤销已认领的待办 —— 谁在处理这件事目前有两个答案，本卡只保证第二个答案有记录，不做同步（同步要选权威侧，是 P3 收口的决策）；④ **没有 SLA 达成率/首响时长指标出口**：本表只提供原始时刻列（`sla_*` 与 `claimed_at`/`completed_at`），CS-32 那类"分桶 + 达成率"的聚合端点未做，AC④ 测的是"审批类待办不污染坐席 SLA 读数"这条**隔离**，不是"SLA 算得出来"；⑤ **投递失败非致命且无对账补投**：`transferToHuman` 只打一条 Error 日志（会话已提交，上抛会让整条消息处理失败），于是存在"会话已 `waiting` 而池子里无行"的窗口，且没有任何任务去检测它 —— 补投需要一个对账节拍器，本卡不擅自加（它会与 T-P3-02 的清扫器争同一张表的写口）；⑥ `Counts` 无历史快照，一次读数变不成趋势；⑦ 无批量端点，200 条待办只能逐条 `claim`/`complete`。另记一处**平行事实**（本卡实测发现，未合并）：转人工这条路上今天有三张表各记一份 —— `customer_sessions`（会话本体 + `handoff_at`/`handoff_reason`/`first_human_reply_at`）、`handoff_decisions`（规则链审计，由 `internal/pkg/cron` 的 `HandoffChainService.RunCron` 写入；而 `confidence.HandoffDecisionService` 走的是另一条写入口，实测 `NewHandoffDecisionService(` 生产零构造）、`human_tasks`（本卡新增的待办索引）。三者互不感知，`handoff_decisions` 自带一套 `accepted_at`/`resolved_at`/`sla_breached` 字段与 `human_tasks` 的 SLA 列语义重叠 —— 收口哪一份是权威属 P3 之后的决策，本卡只把新表钉在"索引谁该处理"这一职责上、不抄业务字段。**本卡刻意不加 `FF_*` 旗子**（与 T-P2-06/T-P3-02 的三态装配不同，理由写进 `internal/app/human_task_wiring.go` 头注释）：这条竖只往一张新表写新行、不改任何既有读路径，真正的开关是 `InitHumanTaskRuntime(nil)` ⇒ 全局服务置 nil ⇒ 生产者为 nil ⇒ 端点回 503，且 `Init(nil)` 是**清空**全局而不是 no-op（反复装配时留着旧实例，会让端点在一句"未装配"的启动日志下继续回 200）；该 off 形状由 `internal/app/human_task_wiring_test.go` 逐条钉住。卡面列的迁移文件 `v3_47_0_human_task_migration.go` **刻意不存在**（同 T-P3-01/T-P3-02：本仓启动期版本化迁移固定空跑，生产建表唯一真源是 `internal/pkg/db/migrate.go` 的 `allModels()`，已登记并由 `migrate_test.go` 的 `mustCover` 反向锁住） |
| G22 | `model/opportunity.go` / `repository/opportunity.go` / `service/opportunity.go` / `service/opportunity_convert.go` / 表 `opportunities` | **商机域五层：表、仓储、业务判断、HTTP 出口、转换与分配都已建好，且自 T-P4-05 起这张表**有了第一个生产写入方**（2026-09-21 实跑登记。本行开头那句"今天仍然零生产写入方"是 T-P4-04 收尾时的状态，自本卡起失效，就地标注于段末；"零生产调用方"从此只对**手工**这一侧成立 —— 自动那一跳通了，运营想手建一条仍然没有口）（T-P4-01 建模、T-P4-02 仓储、T-P4-03 跃迁表与赢率式、T-P4-04 八条端点与启动装配点、T-P4-05 转换层 + 分配 + 名单，`/api/opportunity/*` 自 T-P4-04 起在路由树上、启动自带装配；那句"没有任何生产者构造过一行商机"记的是 T-P4-04 收尾时的状态，到本卡结束）。开工前实测全仓对商机的表达只有 `clues.is_opportunity` 一个 0/1 位，model 层 `opportunity` 零命中，故本卡是把商机第一次放进 schema，而不是补字段。三条交付判据都由用例守着：① C5 三评分职责分离 —— 本表只带 `win_probability`（0–1，与 ltc.config 的 `win_probability` 阈值同量程，比较不需换算），`confidence`/`lead_score`/churn/RFM 出现在本表任一列即红（变异 M4）；② 无租户列（X3 单商户），标签层与真库层各一条（M3）；③ `stage`（四格过程）与 `status`（四态结果）**互斥且分列**，北极星按 status 算、漏斗按 stage 算，`OpportunityClosed` 含 cancelled 而 `OpportunityOutcomes` 不含，"误建"不会稀释赢率与丢单归因。**T-P4-02 补上的那一层**：`version bigint not null default 0`（三件套**成对**才有意义 —— `NOT NULL` 无默认 ⇒ 给存量表 `ADD COLUMN` 当场失败，而本仓生产建表只跑 AutoMigrate、补列是必然会发生的一次演进；判据不是推理：用例先按 T-P4-01 的形状手写一张没有 version 的表、塞两行存量数据，再跑 AutoMigrate，然后按 `information_schema` 断三件套并用独立零值 struct 读回老行）；并发口径选 **CAS 而不是 `SELECT … FOR UPDATE`**（AC① 写的是"版本控制"，而行锁会让第二个写者**等**到第一个提交、再拿着刚读到的新值改成功，于是"两人各改不同字段"变成两次都成功 —— 那要求每个调用方只改自己那一格，而"改了哪几格"没有任何一层能证明；CAS 更硬：手里那份不是最新就整个拒绝，丢改动**可见**、静默混合不可见，实测 8 协程打同一行恰好 1 成功 7 个 `ErrOpportunityStaleVersion`、末态 `version=1`）；改写白名单是那个 10 键 map，两条**只有真跑才会暴露**的静默失效记在代码注释并被变异钉住（① `Select(白名单)` 会把不在清单里的 `version = version + 1` 一并过滤掉 ⇒ 版本永远停在 0、八个写者八个全赢，症状是"改写都成功、并发全通过"，正是 AC① 最坏的那种烂法；② struct 形式的 `Updates` 默认跳过零值字段 ⇒ "把金额改成 0""把输单原因清空"返回成功而库里一切照旧，所以走 map）；读侧三条纪律各有用例（`statuses` 传空切片**报错**而不是回全表、`limit<=0` 与 `offset<0` 由本层拒 —— 负 offset 的判据写成"错误来自本层"，否则 PG 那句 `OFFSET must not be negative` 也会让"只查有没有报错"绿、排序带 `id DESC` 兜底键且在并列行上**逐位比对次序**，摘掉兜底键后三行按插入顺序返回而"不重不漏"照绿）。 **今日仍不成立的部分（本段是 T-P4-04 收尾时的清单，其中「整张表零写入」已由 T-P4-05 结掉，段末另给了本卡之后的新清单）**：整张表在生产路径上**零写入**（构造与一键转商机在 T-P4-05；未接线台账**项 16** 自 T-P4-02 起拆为 **16a 仓储装配入口 / 16b 商机行写入点**两行、各按 UNWIRED 登记 —— 拆的起因是实测"仓储文件自己提到自己的模型"把原来那一格推成 WIRED、门 rc=1，而那天依然没有任何人往这张表写业务数据；**T-P4-03 又加一行 16c 商机服务的装配入口**（`NewOpportunityService(`，scope 只覆盖装配面），与 16a 分开登记的理由是接线有**两个断点**（装配仓储 / 挂路由），只盯一个则另一个断了没人知道；16b 本卡刻意不翻 —— 服务层只发 `*model.Opportunity` 指针、不构造新行），**跃迁合法表与赢率计算式已随 T-P4-03 落在 `internal/service/opportunity.go`**：阶段"向前恰好一步、向后任意步、同格不算跃迁"，status 走 **(来源, 起点, 终点)** 三元边表（AC③"本层写 won 的唯一入口"由这张表守住：`sales` 只放行 lost/cancelled/回 open，只有 `collection_completed` 能落 won），赢率 `p = round₂(base(阶段) − 无归属 0.10 − 已逾期 0.15)`、下限 0.05，四个 base 是**先验刻度不是标定值**（可证明的只有"逐格向前 ⇒ 集合嵌套 ⇒ 随阶段单调不减"这个形状），惩罚只做常数、不做逾期梯度，**禁止与 `confidence`/`lead_score` 跨域乘算**（C5：三者条件集不同，输入结构 `OpportunityWinInput` 只有三字段、由反射用例钉住）；仓储那一层**仍然刻意不校验**（`TestOpportunityRepo_DoesNotValidate` 钉住越界值原样落库 —— 判据在 service 一份就够，抄两份迟早分家），所以"越界值今天能进库"是**前提**而不是漏洞，`TestOpportunityService_RefusesToComputeOnUnreadableRows` 钉住另一头：本层拒绝在看不懂的行上继续计算；**T-P4-04 已补上 HTTP 出口**：八条端点（读二 `GET /{id}` / `GET /{id}/moves` + 规则 `GET /rules` + 写五 `PUT /{id}` / `stage` / `lost` / `cancel` / `reopen`），**没有一条能写 `won`** —— 那是上面那张三元边表在 HTTP 侧的兑现（加一条 `POST /{id}/won` 就用例红）；`400/404/409/503` 共用一套分诊词表（`input_invalid`/`not_found`/`stale_version`/`closed`/`transition_illegal`/`state_invalid`/`unavailable`/`internal`），503 的判据是「不许有看起来像结果的 data」（`{}` `[]` `null` 都会被前端长成「查过了，没有」，而此刻的事实是一次都没查）；绑定一律 `DisallowUnknownFields` 且请求体封顶 4KB（派生量与身份列在服务层入参结构里根本没有格子，默认丢弃未知字段会让 `{"win_probability":0.99}` 静默成功）；写入口必须带 `version`，缺字段判 400 而不是让它取零值（0 恰好是新行的合法期望版本）；`GET /rules` 在未装配时照样答，并给出「存在但不暴露」的机器动作清单。台账：16a/16c 比原计划早一张卡同时翻 wired（出口自带底座），新增 16d 挂载入口 + **16e 启动装配点**（起因是「`router.go` 摘掉 `InitOpportunityRuntime`」这一把 Go 与台账两边都看不见：字面量全在、端点也在树上，只是运行时全局句柄永远 nil ⇒ 八条端点全退 503）；现值 **42/49**。**列表端点仍欠**：仓储 `List` 不返回 `total`，用 `len(list)` 凑数会把「一共多少条」从「查过」变成「猜的」，已登记为欠账。构造与一键转商机在 T-P4-05，`ltc.config` 的 `win_probability` 阈值在生产代码里**仍零读者**（本层的 proposal=0.55 跨过它、negotiation 只吃逾期惩罚后 0.60 仍在其上（吃满"无归属 + 逾期"两笔则正好落在 0.50＝阈值本身，比较取 `≥` 还是 `>` 是读方的事），但比较动作发生在读方、不在本层）；台账 `v3_48_0_opportunity_migration.go` 同 §4.14/§4.15 口径**刻意不存在**，建表真源是 `allModels()`。 **T-P4-05（2026-09-21）把本行开头那句"今天仍然零生产写入方"改掉了**：转换层是这张表的第一个生产写入方，落点 `internal/service/opportunity_convert.go` + `opportunity_assign.go` + `opportunity_roster.go`，接缝在 `lead_mining.go: persistLead` 的两条分支（都在线索行落库之后 —— `Create` 失败就不转，本表不建外键，断链没人会发现）。三条 AC 各落成一条可跑的判据：① 达标闸门是**量程 → 阶段 → 双阈值**三道串行（越界硬拒不归一，本仓两个同名 `confidence` 量程不同；`StageActive` 在 `LeadQualified` 之前因为后者 nil 不安全；配置降级按"关"）；② **转换那一步对 `clues.is_opportunity` 一个字节都不写**，反查改走 `opportunities.clue_id` + **部分**唯一索引（`WHERE clue_id <> ''`，空串是本表合法常态；GORM 标签表达不了 partial，故索引在 `postMigrateOpportunityClueUniqueIndex()` 这条只 Warn 不清数据的启动钩子里）；③ 分配结果**是返回值不是日志行**（规则名 + 候选集 + 每人负载一起出，候选必须按 `SalesID` 升序，因为 `leastLoaded` 靠"先遇到者胜"做平票裁决）。名单真源 `sales_events`/`sales_profile`（`sales_personas` 零写入方，用它 ⇒ 分配永远 `no_roster`），"在册"只能等于"注册过档案"（无停用事件，登记为边界）。台账：**16b 兑现翻 wired**，另新增四行（转换竖启动登记点 / 挖掘侧接缝 / 名单适配器装配点 / `clue_id` 索引启动调用点），现值 **47/53**。仍不成立的部分：HTTP 手工"一键转商机"口未交付；`lead_miner_unified.go` 那条通用链**刻意未接**（它没有 confidence 生产者）；`win_probability` 阈值仍零读者 |
