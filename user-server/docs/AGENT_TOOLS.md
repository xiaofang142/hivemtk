# Agent 工具系统完整参考（user-server）

> 适用代码： `internal/aiagent/agent/tooluse/*`、`internal/service/sales_engine*.go`、`internal/router/tool_executor_*.go`、`internal/dto/sales.go`
> 工具总数：**42**（customer 8 · knowledge 4 · business 6 · pm 3 · reach 20）

---

## 1. 架构总览：工具的生命周期

```
            ┌──────────────────── 注册期（进程启动，router.Setup） ───────────────────┐
            │  Provider 模式：ReachToolProvider / PrivateMessageToolProvider 自注册     │
            │  + RegisterCustomerTools / RegisterKnowledgeTools / RegisterBusinessTools │
            │  → MustRegister 进全局 ToolRegistry（重名 panic，防静默冲突）              │
            └────────────────────────────────────────────────────────────────────────┘
                                          │
                                          ▼
   LLM ──function_call──▶ Agent Loop (runAgentLoop) ──▶ ToolExecutorAdapter.GetToolsForLLM ──▶ 注入工具 schema
            ▲                                        │
            │                                        ▼
            │                          ToolExecutor（装饰器链：权限→限流→重试→超时→审计→计费）
            │                                        │
            │                                        ▼  ExecuteByName
            │                          ToolRouter（熔断器，运维/stats 用，不走主 loop）
            │                                        │
            │                             各具体 Tool.Execute（业务/客服/触达/物流）
            │                                        │
            └──────────── tool_result 回灌对话上下文，下一轮 LLM 可继续调用或终答 ┘
```

**双层权限防护**（本次改造核心）：

- **第一层·注入期过滤** `limitToolsForAgent`：决定 LLM *能看到*哪些工具。Agent 配置了 `Tools` 白名单则只注入白名单内工具；否则按默认优先级集（已扩展到 18 个电商客服关键工具）。
- **第二层·执行期检查** `WhitelistPermissionChecker`（`defaultAllow=true`，向后兼容）：Agent Loop 每轮按 `AgentContext.Tools` 覆盖式设置执行期放行名单；即使越权调用，权限检查器按白名单拒绝。两层的 `*tooluse.WhitelistPermissionChecker` 是**同一全局单例**（管理 API `/api/agent/tools/permission/agents/:agent_id` 亦设置它）。

---

## 2. 如何注册

每个工具的“工厂”把依赖（端口适配器 / DB）注入后产出 `[]tool.Tool`，经 `MustRegister(registry, tool)` 进全局注册中心。

| 类别 | 工厂 / 注册函数 | 依赖注入 | 注册入口 |
|---|---|---|---|
| customer | `BuildCustomerTools(deps)` | `CustomerPort` + DB | `RegisterCustomerTools` |
| knowledge | `BuildKnowledgeTools(deps)` | `RAGPort` + DB | `RegisterKnowledgeTools` |
| business | `BuildBusinessTools(deps)` | `FollowUpPort`/`OrderPort`/`AfterSalePort`/`LogisticsPort` + DB | `RegisterBusinessTools` |
| pm | `BuildPrivateMessageTools(deps)` | `PrivateMessagePort` + DB | `RegisterPrivateMessageTools` |
| reach | `BuildReachTools(deps)` | `ReachDeps`(Pipeline/Channel/Repo) | `ReachToolProvider.Provide` |

端口（Port）是依赖倒置点：`tooluse` 包只依赖 `portcontract.*` 接口，具体实现（`service.New*PortAdapter`）在 `service` 包，**避免 service↔tooluse 循环依赖**。

运行期可调参数（**全部存数据库，不存环境变量**）：

- Agent Loop 运行期调参 → `system_config_kv` 表（key=`agent.settings`），结构见 `service.AgentSettingsConfig`：
  - `max_loop_iterations`（默认 5，单轮对话最多工具迭代次数，必须 ≥ 2）
  - `max_tools`（默认 18，未配置白名单时注入工具数上限）
  - 后台接口：`GET/PUT /api/agent/settings`（保存即生效，无需重启）

- 工具依赖的外部集成配置（物流/售后回写） → `system_config_kv` 表（key=`agent.tool_integrations`），结构见 `service.ToolIntegrationConfig`：
  - `logistics`: enabled / base_url / key / secret
  - `after_sale`: enabled / base_url / key / secret
  - 后台接口：`GET/PUT /api/agent/tool-integrations`（保存即生效，无需重启）

> ⚠️ 原环境变量 `MTK_AGENT_MAX_TOOLS` / `MTK_AGENT_LOOP_MAX_ITERATIONS` / `DS_API_LOGISTICS_*` / `DS_API_AFTERSALE_*`
> 均已废弃，凭证与运行期调参统一写入数据库。代码内默认值仍可由 `SetAgentLoop*` 注入（测试/内嵌场景）。

---

## 3. 如何使用（Agent Loop = ReAct）

`SalesEngine.runAgentLoop(ctx, scenario, prompt, req, intent, mem, customer, availableTools)`：

```
for iter := 0; iter < agentLoopMaxIterations; iter++ {
  sysPrompt  = buildAgentSystemPrompt(persona, rag, sop, hint)
  toolSchema = executor.GetToolsForLLM(limitToolsForAgent(availableTools, maxTools, req.AgentContext.Tools))
  llmResult  = llm.Chat(ctx, sysPrompt, history, toolSchema)   // 带 tools
  calls      = parseToolCalls(llmResult)
  if calls == nil { break }                                     // 纯文本终答
  for call in calls {
     res = executor.ExecuteToolCall(ctx, call)                  // → ExecuteByName → 装饰器链
     res = sanitize(res)                                        // 自杀防护/敏感词/截断1200字
     history = append(assistant(tool_calls) + tool(res))        // 结果回灌
  }
}
```

- 工具调用模式：LLM 返回 `tool_calls` → `parseToolCalls` → `ToolExecutorAdapter.ExecuteToolCall` → `ToolExecutor.ExecuteByName` → 具体 `Tool.Execute`。
- 结果 `ToolResult{Success, Data, Error, Hint}` 经 `sanitize` 后回灌上下文，循环直到无工具调用（终答）或达 `agentLoopMaxIterations`。

---

## 4. 如何与外部交互（三条路径）

| 路径 | 代表工具 | 机制 |
|---|---|---|
| **A. 本地同步镜像（只读）** | `order.lookup`、`aftersale.query` | 查 `model.ExternalOrder` 本地表（由外部电商/OMS 同步进库），**非实时外呼** |
| **B. SendPipeline 渠道适配（真外发）** | `reach.*`、`pm.*` | `sendViaPipeline` 走 9 步链路：ChannelResolver→ChannelAdapter(Telegram API/短信网关/Webhook…)→限流→去重→Sender→回执→重试→降级→审计 |
| **C. 本地落库 + 可选回写（写）** | `aftersale.create`、`logistics.track` | 本地落库；在数据库 `agent.tool_integrations` 配置售后回写后回写电商；`logistics.track` 本地订单状态兜底 + 配置物流接口后查实时轨迹 |

**关键结论**：`order.lookup`/`aftersale.query` 不实时外呼；真正触达用户的是 reach/pm 的 SendPipeline；`logistics.track` 是本次新增，填补“查快递”缺口。

---

## 5. 工具白名单（按智能体配置）

`dto.AgentContext.Tools []string`（智能体定义里配置，经 `AgentContextToSalesEngineConfig` 落到 `SalesEngineConfig.Tools`）。

- **空**（默认）：走 `limitToolsForAgent` 默认优先级集（见下表，覆盖电商客服主路径）。
- **非空**：Agent Loop 仅注入并放行名单内工具（按名单顺序，上限 30）。

默认优先级（值越小越优先，前 18 个默认可见）：

```
1 rag.search            2 customer.search       3 customer.get         4 customer.create
5 order.lookup          6 pm.session.open       7 pm.message.send     8 pm.session.read
9 knowledge.feedback    10 reach.web.send       11 reach.card.send    12 reach.sms.send
13 aftersale.create     14 aftersale.query      15 follow_task.create 16 follow_task.update
17 logistics.track      18 customer.update
```

> 旧版硬编码 top-10 把 `reach.card.send`/`reach.sms.send`/`aftersale.*`/`logistics.track` 砍掉，本次已修复（扩展默认集 + 每 Agent 白名单）。

---

## 6. 工具总清单

| 类别 | 工具 |
|---|---|
| **customer(8)** | `customer.search` `customer.get` `customer.create` `customer.update` `customer.merge` `customer.add_tag` `customer.remove_tag` `customer.segment` |
| **knowledge(4)** | `rag.search` `knowledge.feedback` `knowledge.add_doc` `knowledge.list_kb` |
| **business(6)** | `follow_task.create` `follow_task.update` `order.lookup` `aftersale.create` `aftersale.query` `logistics.track` |
| **pm(3)** | `pm.session.open` `pm.session.read` `pm.message.send` |
| **reach(20)** | `reach.sms.send` `reach.email.send` `reach.wecom.send` `reach.weixin.send` `reach.douyin.send` `reach.kuaishou.send` `reach.xhs.send` `reach.dingtalk.send` `reach.card.send` `reach.batch` `reach.schedule` `reach.recall` `reach.health` `reach.history` `reach.template.apply` `reach.account.list` `reach.telegram.send` `reach.whatsapp.send` `reach.feishu.send` `reach.web.send` |

---

## 7. 逐工具详细文档

> 字段说明：`配置/依赖`=该工具注入的端口或外部系统；`参数`=LLM 调用时传入；`响应`=`ToolResult.Data` 关键字段；`外部交互`=是否外发/外查。

### 7.1 customer（客户域，经 CustomerPort → CustomerService → DB）

#### customer.search — 按身份线索检索客户
- **功能**：用手机号/邮箱/各平台 open_id 模糊检索客户档案。
- **配置/依赖**：`CustomerPort`（本地 DB）。
- **参数**：`phone`(str) `email`(str) `wechat_open_id`(str) `douyin_open_id`(str) `xiaohongshu_id`(str) `limit`(int，默认 10)。至少一项。
- **响应**：`{count, customers:[{id,name,phone,tags,...}]}`。
- **外部交互**：无（本地）。

#### customer.get — 按 ID 取客户详情
- **功能**：精确获取客户档案。
- **参数**：`customer_id`(str，必填)。
- **响应**：客户实体（含标签/渠道绑定）。

#### customer.create — 新建客户
- **功能**：创建客户档案（首次接触时入档）。
- **参数**：`phone` `email` `wechat_open_id` `douyin_open_id` `xiaohongshu_id`（至少一项）。
- **响应**：`{customer_id, ...}`。

#### customer.update — 更新客户
- **功能**：更新客户字段。
- **参数**：`customer_id`(必填) + 待更新字段（phone/email/各 open_id）。
- **响应**：更新后客户实体。

#### customer.merge — 合并重复客户
- **功能**：把 secondary 合并进 primary，去重。
- **参数**：`primary_id`(str,必填) `secondary_id`(str,必填)。
- **响应**：合并结果。

#### customer.add_tag / customer.remove_tag — 打/去标签
- **功能**：给客户打标签（如 VIP/高风险）或移除。
- **参数**：`customer_id`(必填) `tag`(str,必填)。
- **响应**：标签列表。

#### customer.segment — 客群筛选
- **功能**：按标签/流失风险/创建时间分页筛选客群。
- **参数**：`tag` `churn_risk`(str) `created_after`(str,ISO) `created_before`(str) `page`(int) `page_size`(int)。
- **响应**：`{count, customers:[...], page, page_size}`。

---

### 7.2 knowledge（知识域，经 RAGPort）

#### rag.search — 知识库检索 ⭐最高频
- **功能**：向量+BM25 混合检索知识片段，回答产品/政策/FAQ。
- **配置/依赖**：`RAGPort`（向量库 + BM25）。
- **参数**：`query`(str,必填) `product_id`(str,可选) `top_k`(int,默认取 RAGTopK) `session_id`(可选)。
- **响应**：`{results:[{content, score, source, product_id}], ...}`。
- **外部交互**：无（本地向量检索）。

#### knowledge.feedback — 知识反馈闭环
- **功能**：记录用户对答案的评分（赞/踩），用于知识自进化。
- **参数**：`session_id`(必填) `query` `product_id` `rating`(str:up/down) `comment` `operator`。
- **响应**：反馈记录 ID。

#### knowledge.add_doc — 新增知识文档
- **功能**：写入知识库文档（运营补知识）。
- **参数**：`product_id` `title` `content` `source_type` `source_ref` `category` `operator`。
- **响应**：文档 ID。

#### knowledge.list_kb — 列出知识库
- **功能**：分页列出知识文档。
- **参数**：`product_id`(可选) `category`(可选) `page` `page_size`。
- **响应**：`{count, docs:[...]}`。

---

### 7.3 business（业务域，订单/售后/物流）

#### follow_task.create — 创建跟进任务
- **功能**：联动 FollowUpService + 客户旅程，登记待跟进事项。
- **配置/依赖**：`FollowUpPort`。
- **参数**：`contact`/`customer_id`(必填其一) `order_id`(可选) `title`(必填) `type` `due_at`(ISO) `priority` `note`。
- **响应**：`{task_id, status}`。

#### follow_task.update — 更新跟进任务
- **功能**：完成/取消/改期。
- **参数**：`task_id`(必填) `status` `due_at` `priority` `note`。
- **响应**：更新后任务。

#### order.lookup — 查订单 ⭐
- **功能**：查客户订单（只读，客服侧对“订单”唯一读取入口）。
- **配置/依赖**：`OrderPort` → `ExternalOrderRepository`（**本地同步镜像表**，非实时外呼）。
- **参数**：`order_id`/`order_no`(必填其一) `phone`(可选，按手机号查)。
- **响应**：`{count, orders:[{order_id, platform, status, pay_amount, items:[...], ship_time, ...}]}`。
- **外部交互**：**A 路径（本地镜像，非实时）**。

#### aftersale.create — 发起售后 ⭐
- **功能**：发起退款/退货/换货（客服侧对“订单”唯一写入入口）。
- **配置/依赖**：`AfterSalePort` → `AfterSaleService`。在数据库 `agent.tool_integrations.after_sale` 启用后**回写电商**。
- **参数**：`order_id`(必填) `type`(refund/return/exchange) `reason`(必填) `customer_phone` `customer_name` `amount` `platform`。
- **响应**：`{after_sale_id, status, external_id}`。
- **外部交互**：**C 路径**（本地落库；配置后回写电商，返回 external_id）。未配置时 best-effort，状态待 Webhook/拉取刷新。

#### aftersale.query — 查售后进度
- **功能**：按平台+订单号 或 客户手机查售后单。
- **参数**：`platform` `order_id` `customer_phone`（order_id 与 phone 至少一项）。
- **响应**：`{count, after_sales:[{id, type, status, external_id, ...}]}`。

#### logistics.track — 查快递/物流轨迹 ⭐（本次新增）
- **功能**：回答“快递到哪了/何时发货/物流卡哪了”。优先用运单号查实时轨迹；无运单号则按订单号查本地发货状态兜底。
- **配置/依赖**：`LogisticsPort` → `LogisticsPortAdapter`（本地订单状态兜底 + 可选 `CourierClient`）。在数据库 `agent.tool_integrations.logistics` 启用实时轨迹。
- **参数**：`tracking_no`(str,主查询键) `carrier`(str,快递公司编码 SF/ZTO/YTO/EMS/JD，配合 tracking_no) `platform`(str,配合 order_id) `order_id`(str，无运单号时兜底)。`tracking_no` 与 `order_id` 至少一项。
- **响应**：
  ```json
  {
    "found": true, "realtime": false, "configured": false,
    "order_status": "已发货", "ship_time": "2024-01-02T10:00:00Z",
    "tracking_no": "SF123", "carrier": "SF",
    "tracks": [ {"time":"...","status":"in_transit","location":"杭州","description":"..."} ],
    "notice": "当前未配置实时快递接口，仅返回本地订单发货状态。在后台「工具集成配置」填写物流接口 base_url 后可查询实时物流轨迹。"
  }
  ```
- **外部交互**：**C 路径**。实时接口未配置时返回本地订单状态 + Notice，绝不报错中断对话。

---

### 7.4 pm（私信域，经 PrivateMessagePort → 平台会话）

#### pm.session.open — 开启私信会话
- **功能**：在指定渠道(抖音/小红书/微信等)开通/复用私信会话。
- **参数**：`platform`(必填) `account_id`(必填) `user_id`(必填) `user_name` `user_phone` `user_email`。
- **响应**：`{session_id, ...}`。

#### pm.session.read — 读取私信会话
- **功能**：分页读取历史消息。
- **参数**：`session_id`(必填) `page` `page_size`。
- **响应**：`{messages:[...], page, page_size}`。

#### pm.message.send — 发送私信消息
- **功能**：经平台会话发送一条消息（文本/媒体）。
- **参数**：`session_id`(必填) `content` `sender_type`(agent/user) `content_type`(text/image) `media_url` `sender_id` `sender_name`。
- **响应**：`{message_id, status}`。
- **外部交互**：**B 路径**（经平台会话发送）。

---

### 7.5 reach（触达域，经 SendPipeline → 渠道适配，真外发）

> 通用机制：`sendViaPipeline` 走 9 步链路（ChannelResolver→ChannelAdapter→限流→去重→Sender→回执→重试→降级→审计）。大多 `*.*.send` 工具参数结构一致：`account_id`(渠道账号) + 收件标识 + `content` + `msg_type` + 媒体字段。

#### reach.sms.send — 发短信
- **参数**：`phone`(必填) `content` `template_id`(可选模板)。
- **响应**：`{message_id, status, channel}`。
- **外部交互**：**B 路径**（短信网关）。

#### reach.email.send — 发邮件
- **参数**：`to`(必填) `subject` `content`。

#### reach.card.send — 发结构化卡片 ⭐（电商客服高频：商品卡/订单卡）
- **功能**：向客户发送富媒体卡片（商品/订单/活动卡）。
- **参数**：`channel`(必填) `account_id`(必填) `external_user_id`(收件人) `card_id`(卡片模板) `variables`(可选，填充模板)。
- **响应**：`{message_id, status}`。
- **外部交互**：**B 路径**（渠道卡片适配）。**注意**：本工具已注册且本次已纳入默认优先级（11），Agent 默认可调用；此前因旧 top-10 被砍掉。

#### reach.web.send — 网页/WebEmbed 渠道发消息
- **参数**：`session_id`(必填) `content`。
- **外部交互**：**B 路径**（Webhook）。

#### reach.telegram.send — 电报发消息
- **参数**：`account_id` `chat_id` `content`。

#### reach.whatsapp.send — WhatsApp 发消息
- **参数**：`account_id` `to_phone` `content` `template_id`。

#### reach.wecom.send — 企业微信
- **参数**：`account_id` `external_user_id` `msg_type` `content`。

#### reach.weixin.send — 微信
- **参数**：`open_id` `msg_type` `content`。

#### reach.douyin.send / reach.kuaishou.send / reach.xhs.send — 抖音/快手/小红书私信
- **参数**：`account_id` `open_id` `msg_type` `content`。

#### reach.dingtalk.send / reach.feishu.send — 钉钉/飞书
- **参数**：`chat_id`/`open_id` `msg_type` `content`。

#### reach.batch — 批量触达（经 pipeline）
- **参数**：`pipeline_id`(必填) `channel`。
- **外部交互**：**B 路径**（批量 SendPipeline）。

#### reach.schedule — 定时触达
- **参数**：`pipeline_id` `channel` `customer_id` `account_id` `run_at`(ISO,必填)。
- **外部交互**：写入调度，到点经 SendPipeline 外发。

#### reach.recall — 撤回消息
- **参数**：`channel` `message_id`(必填)。

#### reach.health — 渠道健康探测
- **参数**：`channel` `account_id`。
- **响应**：渠道连通性/限流状态。

#### reach.history — 触达历史
- **参数**：`channel` `state`(sent/failed/...) `page` `page_size`。
- **响应**：历史触达记录。

#### reach.template.apply — 套用触达模板
- **参数**：`template`(模板名/ID)。
- **响应**：渲染后的内容/变量占位。

#### reach.account.list — 渠道账号列表
- **参数**：`channel`(可选,按渠道过滤)。
- **响应**：`{accounts:[...]}`。

---

## 8. 改造记录（本次）

1. **新增 `logistics.track`**：`portcontract.LogisticsPort`/`CourierClient` + `service.LogisticsPortAdapter` + `service.CourierClient`(配置驱动) + `business_tools.logisticsTrackTool`，默认纳入优先级集。填补“查快递”缺口。
2. **修复工具可见性**：`limitToolsForAgent` 由硬编码 top-10 改为「Agent 白名单优先 + 默认优先级扩展到 18」，使 `reach.card.send`/`reach.sms.send`/`aftersale.*`/`logistics.track` 默认对 LLM 可见。
3. **按智能体工具白名单**：`dto.AgentContext.Tools` + `SalesEngineConfig.Tools` + `AgentContextToSalesEngineConfig` 填充；`runAgentLoop` 按白名单过滤注入并覆盖式设置执行期放行名单。
4. **接线执行期权限**：执行器 `PermissionChecker` 由 `NoOpPermissionChecker` 改为 `GetGlobalPermissionChecker()`（全局 `WhitelistPermissionChecker`，`defaultAllow=true` 向后兼容）；`SalesEngine` 注入同单例，形成双层防护。
5. **售后回写电商**：`AfterSaleService` 按数据库 `agent.tool_integrations.after_sale` 配置按需构造 `AfterSaleExternalClient`；启用后回写电商并返回 `external_id`，替换原 `TODO(集成)`。
