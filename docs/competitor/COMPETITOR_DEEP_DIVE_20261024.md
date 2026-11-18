# 竞品实现原理深度调研（2026-10-24 深度轮）

> **定位**：在 `COMPETITOR_ANALYSIS_202610.md`（模块级对比）之上补一层**实现原理级**对照——竞品各模块"怎么实现的"（数据结构/算法/执行引擎/存储选型），我方对应实现原理（源码坐标），以及原理层新暴露的差距与启示。
> **输入**：本轮四路调研（Dify / FastGPT / RAGFlow / Chatwoot 公开原理资料 + 我方源码梳理），竞品信息含置信标注，未获细节处明示"未获细节"，不编造。
> **我方原理详档**：`research/OURSTACK_IMPL_PRINCIPLES_20261024.md`（源码级十模块机制梳理）。

---

## 一、竞品实现原理纪要

### 1. Dify（Python/Flask + PG + Redis + Weaviate）

| 模块 | 实现原理 |
|---|---|
| 工作流引擎 | `graph_engine.py`：**队列式动态 DAG**——节点执行完把满足入边条件的下游节点入队（非静态拓扑排序），以支持分支/循环的动态路由；变量系统 `{{#节点ID.字段#}}` 经 VariablePool 传递，模板节点 Jinja2 渲染；执行事件流（node started/succeeded/failed）经 SSE 推前端；Chatflow（有状态）与 Workflow（无状态）共用节点系统 |
| 代码沙箱 | 独立 **dify-sandbox** HTTP 服务（CODE_EXECUTION_ENDPOINT），v1.13.3 收紧爆炸半径；兼容 E2B 协议（可换腾讯 Cube Sandbox，KVM MicroVM 隔离）；内部隔离实现未获细节 |
| RAG 索引 | Celery 任务（`document_indexing_task`，队列 `dataset`，付费租户独立 `priority_dataset` 队列）→ `IndexingRunner`：解析/清洗/分段/嵌入（批量 16/32，失败重试 3 次指数退避）→ 写向量库；分段策略含**父子分段**（小 chunk 检索、返回父 chunk 上下文） |
| RAG 检索 | 向量 / 全文（BM25，向量库或 ES 提供）/ 混合三模式；**融合提供加权打分（默认语义 0.7/关键词 0.3）与 RRF 两种**，可选 cross-encoder Rerank；向量库可插拔，compose 默认 **Weaviate** |
| 模型管理 | V1 后 provider 全部**插件化**到独立 Go 守护进程 dify-plugin-daemon（STDIN/STDOUT 管道通信，另有 TCP/HTTP 运行时）；Model Runtime 抽象（llm/embedding/rerank/…），凭据表单规则 YAML 前后端同源；多 API key 支持，但**负载均衡/fallback 算法未获权威细节** |
| 工具系统 | OpenAPI 解析器 `ApiBasedToolSchemaParser`：operationId→工具名 / description→描述 / parameters→JSON Schema，生成 ApiTool 运行时参数映射回 HTTP；已知短板：$ref/allOf 支持不全、apiKey 仅 header/query；凭据加密存 PG |
| Agent | `BaseAgentRunner` 两策略：FunctionCallAgentRunner（原生 FC，`_run_loop` 解析 tool_calls→ToolEngine）与 CotAgentRunner（ReAct 文本协议 Thought/Action/Observation，`cot_output_parser` 解析），受 max_iteration_steps 限制；多 Agent 编排无独立 orchestrator，经 Workflow Agent 节点嵌套 |
| 评测 | 评测集 + CSV 批量运行 + 人工/LLM-as-judge（Likert/二元）评分；内部表结构与批量执行源码机制**未获权威资料** |
| 异步 | Celery + Redis，约 15 个按用途划分的队列；**租户隔离队列**：Redis List `tenant_*_queue:{tenant_id}` + TTL key 限单租户并发索引；Beat 定时任务部分配 Redis SET NX 分布式锁 |
| MCP | v1.6.0 起双向：client 侧配 server URL 动态发现 tools；server 侧 workflow-as-MCP；支持 2025-06-18 协议（版本协商+结构化输出）；文件级路径未获一手确认 |

技术栈总述：Python 3.12 / Flask + Gunicorn(gevent) / PG 主库 / Redis（broker+锁+pub-sub）/ Weaviate 默认 / Next.js + ReactFlow 前端；nginx → api+worker → pg/redis/weaviate/sandbox/plugin-daemon 拓扑。

### 2. FastGPT（TS/Node，pnpm monorepo）

| 模块 | 实现原理 |
|---|---|
| Flow 编排 | 节点+边 DAG 存储（`WorkflowNode{id,type,inputs,outputs}`），核心调度器 **dispatchWorkFlow**：按依赖调度节点、统一注入 variables（系统变量+外部+用户）；节点输入声明变量引用 `{sourceNodeId, outputKey, valueType}` 运行时解析；支持 ifElse/loop/interactive 暂停节点；SSE 事件区分 answer/toolCall/flowNodeStatus 等类型 |
| 知识库存储 | 三层 库>集合>数据，最小搜索单位=库；**PG pgvector（HNSW）存向量 + MongoDB 存原数据**；`indexes` 字段是数组——**一数据多向量映射**（含图片索引），任一向量命中即召回该数据 |
| Chunking | 直接分段（默认约 700 字）与 **QA 拆分**（分段喂 LLM 生成问答对再向量化——把"可能的提问"放进向量空间提高召回，代价是耗 token/受模型能力影响）；CSV 模板导入 |
| 检索 | 语义/全文（Milvus≥2.5.16 走 BM25，否则 Mongo `$text`+jieba）/混合三模式；**融合用 RRF**（向量/全文/rerank 统一合并）；ReRank 0-1 相关度精排过滤；**引用上限按 tokens 数而非 top-k**，最低相关度仅作阈值 |
| 评测 | 商业版独占（"应用评测 Beta"），**评测集结构与评分算法闭源未获细节** |
| 渠道发布 | 商业版内置飞书/公众号/企微（后端代处理 webhook 验证/事件回调/tenant_access_token/被动回复），社区版需云函数自转；回调签名细节未获细节 |
| 技术栈 | @fastgpt/global→service→web→app（Next.js）分层；Mongo 主库+GridFS+Redis+pgvector（可换 Milvus）；Apache 2.0 附加条件（商业功能闭源仅 API 扩展） |

### 3. RAGFlow（Python，检索引擎默认 ES 可切自研 Infinity）

| 模块 | 实现原理 |
|---|---|
| DeepDoc 解析 | 管线：PDF→图像/文本提取→版面分析→TSR→文本合并→结构化输出；**版面识别 YOLO 检测 10 类元素**（标题/正文/表格/图片/公式/页眉页脚等，输出类型+bbox）；OCR 底层 **PaddleOCR**（DB+CTC 轻量 ONNX）；PDF 优先内嵌文本，**双套乱码检测（PUA/CID、字体编码）触发则降级 OCR**；**TSR 表格结构识别支持跨行/跨列合并**，还原表头对应，输出 Markdown/HTML；跨块文本合并用 **XGBoost 二分类**判断相邻块是否同段落；naive 模板内 PARSERS 二级分发可切 deepdoc/mineru/docling/paddleocr |
| 分块策略 | `rag/app/` 14+ 模板，策略工厂 `FACTORY[parser_id].chunk()` 路由：naive 底座（token 分块+分隔符感知）、qa（Excel/CSV 问答对）、table（按行+保留表头语义）、paper（摘要/章节/参考文献）、book（章节层级）、laws（"第X条"边界检测+含上层条款文本）、one（全文单块）等；切分后 LLM 后处理（自动关键词/自动问题/元数据提取） |
| 检索融合 | 向量+BM25（ES/Infinity）多路召回可叠 rerank；GraphRAG 启用时**双检索器并行**（向量+图检索器：实体向量搜索→关系→N 跳邻居→社区）；**融合非 RRF 而是加权排序**，图结果置顶，评分 `P(E|Q)=PageRank×Similarity` |
| RAPTOR | 自研重写（ICLR2024）：chunk embedding→**UMAP 降维（min(12,len-2) 维）→BIC 自动选簇数→GMM 软聚类**（阈值 0.1，可切 AHC）→每簇 LLM 摘要为新 chunk→递归至单簇；**折叠树检索**——各层统一入向量库直接召回，不维护内存树 |
| GraphRAG | 自研（不 import 微软包）：Light/General 两模式；分块→LLM 抽实体关系（NetworkX 子图）→图合并去重**重算 PageRank**→可选实体解析（规则筛候选+LLM 判定）+Leiden 社区检测；**Redis 分布式锁保证图谱串行写入**；图谱元素以 chunk 形式入向量库，**支持单文档增量更新** |
| Agent/工作流 | v0.8.0 Canvas 图式 DSL（20+ 组件：分类/检索/Agent/SQL/ReAct 循环等），支持 MCP；Agentic RAG：Retrieval 可为组件或 Agent 工具，Self-RAG 形态（Evidence Grader 反思+条件路由+Query Rewriter） |
| 异步/存储 | Redis Streams 生产者-消费者解耦任务（`te.0.common` 等队列，Worker collect→FACTORY→embedding→写引擎）；MySQL 元数据 + MinIO 原始文件 + Redis 队列锁 |

### 4. Chatwoot（Ruby on Rails + PG + Redis/Sidekiq）

| 模块 | 实现原理 |
|---|---|
| 渠道抽象 | Inbox 统一收件箱实体下挂**多态 Channel 模型**（Web/WhatsApp/FB/IG/Twitter/Telegram/Line/SMS/Email/API Channel——API Channel 供自建）；inbound 经供应商 webhook 落库归一，outbound 座席回复后经 provider 适配层异步发送（Sidekiq） |
| 收件箱 | Conversation 状态机 open/pending/resolved/snoozed（是否用 aasm gem 未获细节）；Message `message_type` 区分 incoming/outgoing/activity + `private:boolean` 私有笔记 + @mention；**实时用 Rails ActionCable**（WebSocket 订阅-广播，agent/客户各订阅 channel）；事件驱动（message.created 等事件，通知/未读计数由监听方执行） |
| 座席分配 | 按可用性+负载分配给对 Inbox 有权限的 agent；**CE 版 `member_ids_with_assignment_capacity` 是占位实现（返回全部成员），真正的容量上限是 EE 版特性**，经 `prepend_mod_with('Inbox')` 注入 Enterprise::Inbox 模块剔除超限座席实现；round-robin 算法实现未获细节 |
| CRM | Contact 带 profile/notes/segments；ContactInbox（channel 专属 source_id）关联多渠道；自定义属性 `custom_attribute_definitions` 定义（JSONB 扩展——存储格式未逐字确认）；**跨渠道 contact merge 算法未获细节** |
| Captain AI | Auto-reply（学 Help Center/历史对话/FAQ 的 **RAG 自动答**，带引用，低置信转人工携上下文）+ Copilot（座席旁路建议）；自托管接 OpenAI 兼容 API（可配 DeepSeek/Qwen/GLM）；embedding 存储/意图分类器/agent 循环闭源未获细节；新开源独立项目 faultline（FastAPI+Rails8+Vue3）佐证 AI 服务化走向 |
| 帮助中心 | Portal（多品牌）+ Article（多语言+AI 翻译）+ 分类；Captain 以 article 为 RAG 语料，portal 内自助搜索可"auto-resolved"；CSAT：conversation resolved 后自动发满意度调查入报表（表结构未获细节） |
| 技术栈 | Rails 7+/Puma/Sidekiq/PG/Redis；前端 Vue3+Vite（dashboard/widget/shared 分模块，可独立构建 SDK）；EE 经 `enterprise/` 目录 + prepend 模块注入；2026 年中约 v4.15.x，MIT |

---

## 二、我方实现原理 vs 竞品：原理层新发现

> 本节基于四路调研与我方源码梳理，列出**上一轮模块级对比没有暴露、原理层才看清**的差距与启示。

### 2.1 检索融合：我们与两家竞品都不同

- Dify 混合融合默认**加权打分**（0.7/0.3）、RRF 是可选；FastGPT 统一 RRF；RAGFlow GraphRAG 场景用**加权排序+PageRank×Similarity**、非图场景才是常规融合。
- 我方固定 RRF（topK50+topK30→RRF→Rerank 前 20 取 5）。**启示**：RRF 本身无问题，但我方缺"加权系数可调"的运营旋钮；P1-1 表格感知分块落地时，可考虑为结构化召回加分权重（竞品用加权融合正是为了这类场景）。

### 2.2 父子分段（parent-child chunk）：Dify 有、我们没有

- Dify 的父子分段（小 chunk 检索命中、返回父 chunk 上下文）是解决"精度与上下文两难"的成熟方案。我方结构分块无此机制——检索命中块直接进 prompt，长文档场景上下文不足。
- **建议**：这比 DeepDoc 版面识别的实现成本低一个量级，可作为 C4 缺口（文档解析）的**先导小刀**：分块时记录 parent_id，检索命中后回拼父块。排进 P1-1 的前置或独立小任务。

### 2.3 QA 拆分（FastGPT）：语义级"可能的提问"索引

- FastGPT 把分段喂 LLM 生成问答对再向量化——把"用户可能的问法"放进向量空间。我方 T4 冷启动管道已有"URL→QA 自动拆分"，**方向一致**（C7 已判 ✅），但只覆盖爬取来源；手工上传文档不走 QA 增强。P2 排除清单里 C5"知识库内 QA 对"以"与 faq.go 语义重叠"为由不做——原理层看这个理由仍成立，但可把"上传文档可选 QA 增强"并进 T4 管道的开关，代价小。

### 2.4 评测：Dify 的 CSV 批量+LLM judge 形态可抄

- Dify 评测闭环=评测集（app 下数据集）+ CSV 模板批量跑 + 人工/LLM judge（Likert/二元结构化输出）。我方 P0-1 已有 gold 框架与 bad_case 管道，缺生产接线——**形态对齐 Dify 的 CSV 导入导出**（商户运维能自己整理用例）可以低成本并入 P0-1 的 HTTP 面，不必新立项。

### 2.5 渠道抽象：Chatwoot 的多态 Channel 模型值得对照

- Chatwoot Inbox→多态 Channel 的抽象把"新增渠道=新增一个 Channel 子类+webhook controller"模板化；我方 channelgw+Bridge+reach.* 三套并存是历史产物，功能上已覆盖，但**新增官方 API 渠道（如未来出海）时的接入模板化程度**不如 Chatwoot。属架构债观察项，不立项。

### 2.6 容量/分配：Chatwoot 容量上限是 EE 特性——我方人工优先已超 CE 基线

- Chatwoot CE 的 auto-assignment 容量上限是占位实现（EE 才有），我方人工优先+座席挂载路由+连续回复上限在功能上已超其社区版。A11 工单不做、E9 更深的结论在原理层再次确认。

### 2.7 RAPTOR/GraphRAG：暂不跟，理由更新

- RAGFlow 的 RAPTOR（UMAP+GMM 聚类递归摘要）与 GraphRAG 属高算力、重维护能力，其自研版也依赖 ES/Infinity+MySQL+MinIO 的重型栈，与我方"单商户 compose 开箱即用"形态冲突。维持 P2 不跟；但若 P1-1 外挂解析器路线走通，未来可在 sidecar 里选配 RAGFlow 同时获得这两项能力，零自研成本——**把这条写进 P1-1 的远期收益**。

### 2.8 租户队列隔离：Dify 的 Redis List+TTL 形态可借鉴

- Dify 用 Redis List（tenant 级 key）+TTL 限制单租户索引并发，防大租户饿死小租户。我方 TENANT_MODE=on 形态下 SOP 调度器 16 worker/队列 1000 背压是全局的，无按租户配额——**SaaS 化前的架构观察项**，暂不立项（TENANT_MODE off 单机形态下无此压力）。

---

## 三、成果对照表（原理层速查）

| 能力 | 我方原理 | Dify | FastGPT | RAGFlow | Chatwoot |
|---|---|---|---|---|---|
| 工作流执行 | SOP 19 节点 DAG + 16 worker 调度器 | 动态入队 DAG+VariablePool+Jinja2 | dispatchWorkFlow 依赖调度 | Canvas DSL | — |
| 混合检索融合 | RRF 固定 | 加权 0.7/0.3 或 RRF 可选 | RRF | 常规+图场景加权 PageRank×Sim | —（Captain 闭源） |
| 分块 | 结构分块 | 固定长度+父子分段 | 700 字+QA 拆分 | 14+ 模板策略工厂 | — |
| 文档解析 | etl.ExtractText 单级 | 常规（无版面） | 常规+图片知识库 | **YOLO 版面+PaddleOCR+TSR+XGBoost** | — |
| 实时推送 | WS（Origin 白名单） | SSE 事件流 | SSE 事件 | SSE | ActionCable WebSocket |
| 异步模型 | Go goroutine+队列背压 | Celery 15 队列 | Node 事件循环 | Redis Streams | Sidekiq |
| Agent 循环 | ReAct ≤5 轮/装饰器链 9 层+独立件 6 | FC/ReAct 双 runner | 节点化 | ReAct 组件 | Captain 闭源 |
| 向量存储 | pgvector | Weaviate 默认可插拔 | pgvector(HNSW)+Mongo | ES/Infinity | — |
| 评测 | gold 框架（未接线） | 评测集+CSV 批量+LLM judge | 商业版闭源 | — | — |
| MCP | Server 单向 | 双向（v1.6.0+） | 双向（商业） | 支持 | — |

---

## 四、对既有缺口任务清单的回灌（增补规格）

> 主清单见 `COMPETITOR_GAP_TASKLIST_202610.md`（**v4**，2026-11-18 已删并入 `docs/未完成任务清单.md` §六/§七，原文查 git 历史），以下是本轮原理调研**新增/修订**的规格条目。
> ⚠️ **二次审核重要修正（2026-10-24）**：GAP_TASKLIST v3 的批 1 三项（P0-1/P0-2/P0-3）经源码复核**已在代码交付**（DialogueEvalCron 装配 / 前置门槛读 GetPolicy / G13 markFailed 分流），v4 已改验收车道；下述第 3 条 CSV 导入导出是 P0-1 唯一确认未落地的回灌项，其余按 v4「待验收残点」清单执行。

1. **P1-1 前置小刀（新增）——父子分段**：分块记录 parent_id，检索命中后回拼父块上下文（对标 Dify）。AC：长文档检索命中小块时 prompt 含父块；短文档行为不变；`make audit` 绿。预估 1-2 天，可先于外挂解析器落地。
2. **P1-1 规格补充**：解析器接口抽象时预留「引擎选择」二级分发（对标 RAGFlow PARSERS：deepdoc/mineru/docling 可切），而非只留单一外挂 seam；远期收益注记：sidecar 选配 RAGFlow 可顺带获得 RAPTOR/GraphRAG，零自研。
3. **P0-1 规格补充**：HTTP 面增加评测集 **CSV 导入/导出**（对标 Dify 批量评测形态），降低商户自建用例门槛；与 bad_case 管道并行。
4. **T4 管道观察项（不立项）**：上传文档可选 QA 增强（对标 FastGPT QA 拆分），作为 T4 爬取管道的开关扩展，收益待 P0-1 评测读数建立后再评估。
5. **架构观察项（不立项，入册）**：渠道接入模板化（对标 Chatwoot 多态 Channel）；SOP 调度器按租户配额（对标 Dify 租户队列，SaaS 化前评估）。

---

## 附：来源

- 本轮四路调研纪要：Dify / FastGPT / RAGFlow / Chatwoot（各含来源 URL 清单，见上表各节）
- 我方：`research/OURSTACK_IMPL_PRINCIPLES_20261024.md`（源码级十模块机制梳理，坐标实读源码所得）
- 上一轮：`COMPETITOR_ANALYSIS_202610.md`、`COMPETITOR_FEATURE_CHECKLIST_202610.md`、`COMPETITOR_GAP_TASKLIST_202610.md`

## 附：源码梳理带回的口径修正（回灌上一轮文档）

我方源码梳理（2026-10-24）发现上一轮两处描述与源码有出入：

1. **SOP 节点型数**：`sop.go:100-110` 的 `SOPNodeSupportedTypes` 实为 **19+reach_send（表内 20 个常量）**，上轮"19 节点型"少计一个。涉及 CHECKLIST E1 行。
2. **推理栈部署形态**：llama.cpp/MLX 是**宿主机直跑**（`docker-compose.yml:17-19` 端口映射，容器只跑 PG/Redis 数据层），上轮"推理栈三件套随 compose 起"表述不准；CDP 端口 8206、LLM :8207 / Embedding :8208 / Rerank :8209。涉及 CHECKLIST D6/H1 行。
3. 意图/转人工细节补正（非错，仅更精）：级联意图实为**三级**（规则→Embedding 锚点中间层→LLM 复判），上轮写"规则+LLM 复判"两级——中间层锚点预计算入 pgvector、失败 fail-open 退两级（`intent_recognition.go:174-272`）。
