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
