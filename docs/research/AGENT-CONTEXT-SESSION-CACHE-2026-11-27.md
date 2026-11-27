# 智能体会话、长上下文与缓存调研

**日期**：2026-11-27  
**范围**：OpenAI、Anthropic、Gemini、DeepSeek、阿里云百炼；OpenAI Agents SDK、Claude Agent SDK、LangGraph、AutoGen；单聊/群聊工程推论。  
**方法**：只采用官方文档、官方 SDK 文档和官方 API 参考；框架实践与工程推论单独标注。模型上限是调研日快照，上线前必须按真实 model ID/protocol/region 重查。

## 1. 结论先行

1. **没有跨供应商通用的 session 字段。** OpenAI 的 `conversation`/`previous_response_id`、Anthropic beta 的 `thread.previous_message_id`、Gemini 的 `previous_interaction_id`、百炼的 `previous_response_id` 各自绑定自家服务端；DeepSeek Responses 明确不支持这些续接字段，且不支持项会被静默忽略。[OpenAI Conversation state](https://developers.openai.com/api/docs/guides/conversation-state)、[Anthropic Message threads](https://platform.claude.com/docs/en/build-with-claude/message-threads)、[Gemini Text generation](https://ai.google.dev/gemini-api/docs/text-generation)、[百炼 Responses 兼容](https://help.aliyun.com/en/model-studio/compatibility-with-openai-responses-api)、[DeepSeek Responses API](https://api-docs.deepseek.com/guides/responses_api/)
2. **“带 session id”不等于“自动命中缓存”。** session 解决连续性或服务端状态复用；prompt cache 仍要求 provider/model、token 前缀、TTL、最小长度等条件完全满足。百炼多轮示例即使续接成功，`cached_tokens` 仍可能为 0；Anthropic thread 本身也不提供 prompt cache。[OpenAI Prompt caching](https://developers.openai.com/api/docs/guides/prompt-caching)、[百炼 Responses 兼容](https://help.aliyun.com/en/model-studio/compatibility-with-openai-responses-api)、[Anthropic Message threads](https://platform.claude.com/docs/en/build-with-claude/message-threads)
3. **“百万上下文”不是所有模型/协议都有。** 当前默认 `gpt-4o` 只有 128k context、16,384 output；Anthropic 1M 模型 output 128k；Gemini 4 默认 output 65,536；DeepSeek 1M 模型 output 393,216；qwen3.8-max 为 1M context、131,072 output；Qwen Responses 只允许约 80% 输入。[GPT-4o](https://developers.openai.com/api/docs/models/gpt-4o)、[Anthropic Context windows](https://platform.claude.com/docs/en/build-with-claude/context-windows)、[Gemini 4 Carbon](https://ai.google.dev/gemini-api/docs/models/gemini-4-carbon)、[DeepSeek Models](https://api-docs.deepseek.com/api/list-models/)、[qwen3.8-max](https://help.aliyun.com/en/model-studio/qwen3-8-max)、[Qwen Responses](https://help.aliyun.com/en/model-studio/qwen-api-via-openai-responses)
4. **缓存不减少上下文占用。** cached tokens 仍占 context window，只改变计费或计算复用；预算必须同时扣 input、output、reasoning/CoT 和安全余量。[Anthropic Context windows](https://platform.claude.com/docs/en/build-with-claude/context-windows)、[OpenAI Conversation state](https://developers.openai.com/api/docs/guides/conversation-state)
5. **缓存优化的核心是稳定前缀。** 时间戳、随机 ID、工具顺序、JSON key 顺序、检索结果重排、历史改写和 compaction 都可能改变前缀；正确做法是稳定 instructions/tools/reference 在前，动态上下文在后，历史尽量 append-only。[OpenAI Prompt caching](https://developers.openai.com/api/docs/guides/prompt-caching)、[Anthropic Prompt caching](https://platform.claude.com/docs/en/build-with-claude/prompt-caching)、[百炼 Context Cache](https://help.aliyun.com/en/model-studio/context-cache)
6. **应用必须是事实源。** 供应商 continuation 可能过期、删除、 silently ignore 参数或改变保留策略；本地 MessageHub、客户/线索事实、对话记忆和授权范围不能依赖供应商 session 保存。
7. **单聊/群聊差异是工程问题，不是厂商现成 session 功能。** 群聊需要成员身份、audience/visibility、入退群区间、多人意图、噪声和隐私规则；单聊需要双方关系、长期纠正、承诺、异议和情绪轨迹。

## 2. 代表性厂商能力矩阵

| 厂商 / 协议 | 代表性模型 | 上下文 / 输入 | 最大输出 | continuation | prompt cache | 关键限制 |
|---|---|---:|---:|---|---|---|
| OpenAI Chat/Responses | `gpt-4o` | 128,000 context | 16,384 | Responses chain/conversation | implicit | 当前默认不是百万模型。[GPT-4o](https://developers.openai.com/api/docs/models/gpt-4o) |
| OpenAI Responses | `gpt-5.6-luna` | 1,050,000 context；922,000 max input | 128,000 | `previous_response_id`/conversation | implicit + explicit | GPT-5.6+ 最小缓存 1,024 visible tokens。[Model](https://developers.openai.com/api/docs/models/gpt-5.6-luna)、[Cache](https://developers.openai.com/api/docs/guides/prompt-caching) |
| Anthropic Messages | 当前 1M Claude 型号 | 1M；其他型号可能 200k | 128,000 | `thread` beta | automatic/explicit `cache_control` | thread 5 分钟活跃、最多 1 小时删除；thread 不缓存 prompt。[Context](https://platform.claude.com/docs/en/build-with-claude/context-windows)、[Threads](https://platform.claude.com/docs/en/build-with-claude/message-threads) |
| Gemini Interactions/GenerateContent | Gemini 4 | 1,048,576 共享 input/thinking/output | 默认 65,536；long decode 更长 | `previous_interaction_id` | Gemini 4 仅 implicit | explicit cache 不是全模型/全 API 支持。[Model](https://ai.google.dev/gemini-api/docs/models/gemini-4-carbon)、[Caching](https://ai.google.dev/gemini-api/docs/caching) |
| DeepSeek Responses | `deepseek-flash`/`deepseek-v4-pro` | 1,048,576 | 393,216 | **stateless** | implicit disk prefix cache | 不支持 `previous_response_id`/`conversation`/`store`，忽略不支持参数。[Responses](https://api-docs.deepseek.com/guides/responses_api/)、[Models](https://api-docs.deepseek.com/api/list-models/) |
| 百炼 OpenAI-compatible Responses | `qwen3.8-max` | context 1,000,000；Responses 输入约 80% | 131,072 | `previous_response_id` 7 天 | implicit/explicit/session-cache header | 超限可能静默截断；session cache 默认关。[Model](https://help.aliyun.com/en/model-studio/qwen3-8-max)、[Responses](https://help.aliyun.com/en/model-studio/qwen-api-via-openai-responses) |

> 表中数字是 2026-11-27 官方页面快照，不代表所有 region、protocol 或后续模型。生产配置必须锁定 `provider + model + protocol + region`。

## 3. OpenAI

### 3.1 Prompt caching

- 缓存的是可复用前缀的 KV 状态，不是答案，也不是原始 token 存储。[Prompt caching](https://developers.openai.com/api/docs/guides/prompt-caching)
- 整个渲染前缀必须匹配；model、tools 名称/描述/schema/顺序、结构化格式、reasoning、verbosity、context management 等变化会使变化点之后无法复用。
- 稳定 developer/system 指令、工具定义和共享历史放前；当前时间、随机 trace、当前用户内容放后。
- GPT-5.6+ 支持 implicit 与 explicit breakpoint，最小可缓存前缀 1,024 visible input tokens；默认 30 分钟，可选 12 小时。早一代模型语义不同，GPT-4o 只有 implicit。
- `prompt_cache_key` 帮助路由、计量和隔离提示，不 pin 机器，也不保证命中。
- usage 至少观察 `input_tokens_details.cached_tokens`、cache write/read tokens（若协议提供）、TTFT 和成本。

### 3.2 Conversation state

- 手工模式：客户端保存历史，把旧 response output 追加到下一次 `input`。
- Conversations API：durable `conversation` 对象保存 messages/tool calls/tool outputs。
- `previous_response_id`：response chain；即使使用它，链上历史仍按 input tokens 计费。
- response 默认保存 30 天；`store=false` 可关闭。Conversations/items 的保留语义不同，不能假设同一 TTL。[Conversation state](https://developers.openai.com/api/docs/guides/conversation-state)
- Agents SDK 一个 run 是一个 turn，循环 model→tool/handoff→final；常见策略有应用侧 history、SDK session、OpenAI conversationId、previousResponseId。官方建议一个 conversation 选一种主策略，避免本地 replay 与 server state 重复上下文。[Running agents](https://developers.openai.com/api/docs/guides/agents/running-agents)
- SDK session ID **不是用户认证**；应用必须做 tenant/subject/audience 授权。[Agents SDK Sessions](https://openai.github.io/openai-agents-python/sessions)

## 4. Anthropic

### 4.1 Prompt caching

- Messages API 通过 top-level `cache_control`（自动）或内容块 `cache_control`（显式）开启。
- 前缀顺序是 `tools → system → messages`，要求 100% identical；默认 5 分钟，可选 1 小时。
- 不同模型最小缓存长度 512/1,024/2,048/4,096 tokens；每请求最多 4 个 breakpoint；读取最多回看 20 个 block。
- 动态 timestamp 放入被缓存块会导致永远 miss；Go map/JSON key 随机顺序也会破坏缓存。
- usage 分为 `cache_creation_input_tokens`、`cache_read_input_tokens`、`input_tokens`；总输入必须按厂商定义合并。[Prompt caching](https://platform.claude.com/docs/en/build-with-claude/prompt-caching)

### 4.2 Context window 与 threads

- 多数当前 Claude 模型为 1M，部分为 200k；1M 模型单次 Messages 请求最高 128k output。
- system、messages、tool results、images/documents、tool definitions、output、thinking 都占窗口；cached tokens 也占窗口。
- 官方明确“更多上下文不自动更好”，存在 context rot；超长 input 返回 400 prompt too long。建议 curation、compaction/context editing。[Context windows](https://platform.claude.com/docs/en/build-with-claude/context-windows)
- `thread` 是 beta、best-effort：首轮全量，后续新消息 + `previous_message_id`；最后一次请求后 5 分钟过期，最迟 1 小时删除；404 后必须本地全量 replay。
- thread 本身不提供 prompt cache；每轮仍需一致的 `cache_control` 或 breakpoint。fixed fields 变化可能产生 `thread_fingerprint_mismatch`；每个 response ID 只能 continue 一次，并发/重试可能遇到 `thread_already_continued`/`thread_continue_in_flight`。[Message threads](https://platform.claude.com/docs/en/build-with-claude/message-threads)
- 工具定义和累积 tool_result 会耗尽上下文；prompt caching 只降成本，不减少占用。[Manage tool context](https://platform.claude.com/docs/en/agents-and-tools/tool-use/manage-tool-context)

## 5. Gemini

### 5.1 Context caching

- Gemini 2.5+ 默认 implicit caching；2.5 最小 2,048 tokens，Gemini 4/3.8/3.6 等最小 4,096。
- Gemini 4 multi-turn/agent loop 只有新增至少 2,050 tokens 才缓存完整前一请求，否则重算末尾 2,050 tokens。
- 命中看 `usage.total_cached_tokens`。
- Interactions API 只支持 implicit；explicit cache 仅支持的 Gemini 3/2.5 generateContent，当前 beta/`v1beta`；Gemini 4 不支持 explicit。
- explicit cachedContents 可创建、列出、更新 TTL、删除；默认 TTL 1 小时；cached tokens 仍占上下文窗口。[Context caching](https://ai.google.dev/gemini-api/docs/caching)、[GenerateContent caching](https://ai.google.dev/gemini-api/docs/generate-content/caching)

### 5.2 Multi-turn 与输出

- Interactions API 用 `previous_interaction_id` 管理多轮；也支持 `store=false` 客户端管理 steps。
- stateless 使用 thinking/tools 时必须原样保留模型生成 steps/签名。[Text generation](https://ai.google.dev/gemini-api/docs/text-generation)
- `input + max_output_tokens` 超过 1,048,576 时 API 自动夹输出上限。[Tokens](https://ai.google.dev/gemini-api/docs/tokens)
- Gemini 4 Carbon 统一 1,048,576 input/thinking/output 窗口；默认同步最大输出 65,536，long decode 可到窗口上限。[Gemini 4 Carbon](https://ai.google.dev/gemini-api/docs/models/gemini-4-carbon)

## 6. DeepSeek

### 6.1 Context caching

- 默认开启 disk context cache，无需改代码；前缀必须完整匹配已持久化 cache prefix unit。
- 支持请求边界、common prefix detection、长输入固定间隔切分。
- usage 有 `prompt_cache_hit_tokens`/`prompt_cache_miss_tokens`，chat 也有 `prompt_tokens_details.cached_tokens`。
- best-effort，不保证 100%；缓存构建需秒级，闲置通常几小时到几天清理；64 tokens 为存储单位。[KV cache](https://api-docs.deepseek.com/guides/kv_cache/)

### 6.2 Stateless Responses

- Responses API 是 stateless，**不支持** `previous_response_id`、`conversation`、`store`、`prompt_cache_key`/retention；不支持参数被静默忽略。
- Chat Completions 同样 stateless，每轮由客户端拼接完整历史。
- `deepseek-flash`/`deepseek-v4-pro` 模型 context 1,048,576、max output 393,216；`finish_reason=length` 可能表示截断。[Responses API](https://api-docs.deepseek.com/guides/responses_api/)、[Models](https://api-docs.deepseek.com/api/list-models/)、[Chat Completions](https://api-docs.deepseek.com/api/create-chat-completion/)、[Multi-round](https://api-docs.deepseek.com/guides/multi_round_chat/)

## 7. Qwen / 阿里云百炼

### 7.1 Context Cache

- 显式与隐式缓存均最小 1,024 tokens。
- 显式用 `cache_control`，5 分钟、hit 刷新、最多 4 markers、回看 20 blocks；隐式自动开启且不能关，命中不保证。
- 显式创建通常 125% input 价、命中通常 10%；隐式命中通常 20%，具体模型/region 有例外。
- 工具定义参与 system cache；工具顺序、JSON 字段顺序/结构必须稳定；并行 tool 结果应合并同 role 消息，避免超过 20-block lookback。[Context Cache](https://help.aliyun.com/en/model-studio/context-cache)

### 7.2 Responses / session cache

- 支持 `previous_response_id`，response 顶层 `id` 有效 7 天；`store=false` 则不能续接；也支持 `conversation`，与 `previous_response_id` 互斥。
- `x-dashscope-session-cache: enable` 默认关闭；开启后模型支持 explicit 用 explicit，否则 implicit。
- Responses 最大输入约为模型 context window 的 80%，超限可能**自动截断且不报错**；本地必须提前预算并验证。
- `max_output_tokens` 语义按模型系列不同；qwen3.8 系列计算 response+CoT 总输出，其他系列可能只算 response；达到上限 `status=incomplete`。
- `usage.input_tokens_details.cached_tokens` 可观测；流式最终 `response.completed` 带 usage。
- 只处理文档列出的参数，未列出的 OpenAI 参数会被忽略。[Create a response](https://help.aliyun.com/en/model-studio/qwen-api-via-openai-responses)、[Compatibility](https://help.aliyun.com/en/model-studio/compatibility-with-openai-responses-api)

## 8. 框架模式

### 8.1 OpenAI Agents SDK

- run 前取 history，run 后存新 items；同一 run 不能叠加 SDK session 与 `conversation_id`/`previous_response_id`/`auto_previous_response_id`。
- 支持 SQLite、Redis、SQLAlchemy、MongoDB、Dapr、Conversations、加密后端；可限制历史、callback 过滤、`pop_item` 纠正。
- compaction 有 generation 防陈旧覆盖；session ID 不认证用户。[Sessions](https://openai.github.io/openai-agents-python/sessions)

### 8.2 Claude Agent SDK

- session 保存 prompt、tool calls/results、responses；`continue` 恢复最近，`resume(id)` 恢复指定，`fork` 分支。
- 跨主机需要 SessionStore/镜像 transcript，或把必要结果沉淀为应用状态；session 不含文件系统 checkpoint。[Sessions](https://platform.claude.com/docs/en/agent-sdk/sessions)

### 8.3 LangGraph / AutoGen

- LangGraph checkpointer 保存单 thread graph state；store 保存跨 thread 长期用户偏好/事实/知识；生产用持久 checkpointer，需 TTL/prune，thread_id 建议 UUID/hash 且 <255。[Persistence](https://docs.langchain.com/oss/python/langgraph/persistence)
- AutoGen 可保存/加载 agents、teams、termination conditions；team state 由 participant agent ID + group chat manager 状态组成，适合无状态 endpoint 恢复。[Managing state](https://microsoft.github.io/autogen/stable/user-guide/agentchat-user-guide/tutorial/state.html)、[Teams](https://microsoft.github.io/autogen/stable/user-guide/agentchat-user-guide/tutorial/teams.html)

## 9. 单聊 / 群聊工程推论

以下不是供应商官方统一方案，而是基于多智能体状态、权限和消息语义的工程设计。

### 9.1 单聊

- 双方身份和方向稳定；当前消息是主要意图来源，历史用于指代、纠正、承诺、异议、情绪和未决问题。
- 客户画像、已确认线索、对话记忆按 subject 授权注入；跨业务 SessionID 不丢 conversation 历史。
- 不宜注入群策略、无关成员或其他账号信息。

### 9.2 群聊

- 必须有当前发言者、稳定 SenderID、成员角色、@/回复关系；显示名只是快照，不能做身份。
- 区分“对 AI/商家说的消息”和“环境聊天”；不能把所有群消息当作当前意图。
- 使用群级摘要 + 可证明可见的相关线程/参与者原始消息；audience 与入退群区间在读历史时过滤，输出脱敏不能补救输入泄露。
- 识别多人需求、供货、资源、撮合角色及冲突/共识；回复默认限定当前发言者/线程。
- 成员级电话、订单、私人备注、线索细节不得进入群回复；未触发消息可进上下文但不生成回复。

## 10. 三种机制必须分层

| 机制 | 作用 | 状态位置 | Key | 失效 | 不应做什么 |
|---|---|---|---|---|---|
| Session 会话记忆 | 多轮连续性、原生 continuation、恢复 | user-server ledger + 可选 provider native | BusLLMSessionID + ExecutionScenarioID + provider/model/protocol/region/generation | 过期、模型/协议/策略变化、tombstone、授权变化 | 不替代业务事实源；不当缓存命中保证 |
| Prompt cache | 复用稳定输入前缀计算 | provider 侧隐式/显式缓存 | 实际 provider 请求前缀 + model + cache key/control | 前缀变化、TTL、最小长度、模型能力变化 | 不复用答案；不减少上下文占用 |
| Answer cache | 复用最终答案 | user-server 本地 | 纯知识新问的租户/渠道/账号/模型/提示/KB/版本 | 配置变化、资格守卫失败 | 个性化、多轮、工具、追问、转人工、审核/承诺不得命中 |

## 11. 未显式提出但必须解决的问题（20 项）

1. **Canonical 顺序**：Go map、JSON key、工具 schema、成员列表不稳定会破坏缓存；必须对实际 provider 请求做 prefix hash 测试。
2. **Token 估算误差**：中文、emoji、工具 JSON、不同 tokenizer 可能超限；安全余量 + provider usage 校准。
3. **Cache TTL 与最小长度**：session 复用不代表 cache hit；能力模型必须记录 CacheMinTokens/CacheTTL。
4. **Context rot**：更多历史可能降低质量；长上下文必须与摘要/小窗口做同夹具质量对比。
5. **工具观察膨胀**：Anthropic/Qwen 20-block lookback、Gemini 4 2,050-token 阈值要求工具结果合并、摘要和 breakpoint。
6. **Session 过期与删除**：provider TTL 不一致；本地必须能从最新快照全量重建。
7. **调用期并发 fencing**：仅提交 CAS 不能防两实例同时用旧 continuation 发请求；需全 scope 租约和 fencing token。
8. **超时重复提交**：请求可能已成功但客户端超时；需 request/tool 幂等键，unknown 结果对账而非盲重试。
9. **输出截断**：`finish_reason=length`、Qwen `status=incomplete`、JSON/Schema 不完整必须 checked API 拦截。
10. **群聊 audience/visibility**：新成员不能读入群前消息；无法证明可见性时 fail-closed。
11. **日志/trace 二次泄露**：notes、tool args/output、AI prompt 不能原样进普通日志/trace；完整内容只进加密 ledger。
12. **工具副作用幂等**：ledger 需 planned/authorized/dispatched/unknown/confirmed；high-write unknown 必须对账。
13. **Legacy 事实升级**：旧备注可能包含 AI 建议或未核实信息，迁移为 `legacy_unverified`，不能变 confirmed。
14. **供应商侧删除**：本地删除不等于 provider native object 删除；能删则删，不能则停止引用并记录 best-effort。
15. **Cached-token 计费**：read/create/hit 口径不同；代理折扣默认不透传，不能宣称未实现的成本收益。
16. **能力漂移**：model/protocol/tool/prompt policy 静默变化要使 generation/prefix 失效。
17. **编辑/撤回/tombstone**：历史纠错必须使相关 native generation 和摘要失效，不能只改本地摘要。
18. **Checkpoint 陈旧**：Agent Runtime 恢复需校验 SnapshotID/schema/watermark，过期则重建。
19. **跨账号同客户**：OneID 归一不代表历史/画像/线索自动跨账号授权；负向测试必须覆盖。
20. **群聊质量指标**：除回复准确率，还需误触发率、噪声压缩率、跨成员证据归属、隐私拦截率和 context rot。

## 12. 实施建议（优先级）

1. **P0：先定身份与读模型。** `ExecutionScenarioID`、群 `group_id` 根键、私聊稳定 SenderID、UnifiedMessageReader、出站 parity、MessageBus 入站合同。
2. **P0/P1：先修事实与并发。** audience/visibility、membership、legacy_unverified、ConversationContext CAS、disposition、tombstone。
3. **P2：再做 session/cache/provider。** canonical key 加密/blind index、调用期租约、transcript/tool ledger、provider capability、checked output、日志脱敏、群聊输出守卫。
4. **P3–P5：逐链接入。** 意图/NLP/情感/异议/拟人 → 销售 → 线索/Agent Runtime；每链只消费最小必要 ContextView。
5. **P6：真实性能与质量门。** fake provider 只验形状；真实 provider/sandbox 验 cold/hot、cached tokens、p50/p95；双场景黄金夹具质量不下降才扩灰度。

## 13. 常见误区

- **误区：兼容 OpenAI Responses 的供应商字段语义一致。** DeepSeek 会静默忽略不支持的续接字段；Qwen 会静默截断超限输入。
- **误区：session ID 会固定缓存机器。** OpenAI `prompt_cache_key` 也不 pin、不保证命中。
- **误区：prompt cache 让上下文变大。** cached tokens 仍占窗口，只是计费/计算不同。
- **误区：百万上下文可以无脑塞全历史。** 官方明确 context rot；需要 curation、摘要、相关窗口和质量门。
- **误区：thread/session 一定提供 prompt cache。** Anthropic thread 明确不提供；百炼续接成功也可能 `cached_tokens=0`。
- **误区：平台代理可以存响应来做幂等重放。** 违反平台数据边界；只能 provider 原生幂等可保证时重放，否则 `outcome_unknown` 回 user-server 对账。
- **误区：Source 可以写 `direct`。** 现有代码 Source 是 `\"\"/self` 与 `platform`；直连是 `RouteMode=direct`，不是新 Source 枚举。
- **误区：输出脱敏能补救群聊输入泄露。** 输入阶段必须做 audience/visibility 过滤。
- **误区：SDK session ID 是认证。** OpenAI Agents SDK 明确不是；应用必须授权。
- **误区：本地删除等于供应商删除。** implicit cache/native object 可能继续存在，需 best-effort 删除和 TTL。

## 14. Sources

- OpenAI：[GPT-4o](https://developers.openai.com/api/docs/models/gpt-4o)、[GPT-5.6 Luna](https://developers.openai.com/api/docs/models/gpt-5.6-luna)、[Prompt caching](https://developers.openai.com/api/docs/guides/prompt-caching)、[Conversation state](https://developers.openai.com/api/docs/guides/conversation-state)、[Running agents](https://developers.openai.com/api/docs/guides/agents/running-agents)、[Agents SDK Sessions](https://openai.github.io/openai-agents-python/sessions)
- Anthropic：[Prompt caching](https://platform.claude.com/docs/en/build-with-claude/prompt-caching)、[Context windows](https://platform.claude.com/docs/en/build-with-claude/context-windows)、[Message threads](https://platform.claude.com/docs/en/build-with-claude/message-threads)、[Manage tool context](https://platform.claude.com/docs/en/agents-and-tools/tool-use/manage-tool-context)、[Agent SDK Sessions](https://platform.claude.com/docs/en/agent-sdk/sessions)
- Gemini：[Context caching](https://ai.google.dev/gemini-api/docs/caching)、[GenerateContent caching](https://ai.google.dev/gemini-api/docs/generate-content/caching)、[Text generation](https://ai.google.dev/gemini-api/docs/text-generation)、[Tokens](https://ai.google.dev/gemini-api/docs/tokens)、[Gemini 4 Carbon](https://ai.google.dev/gemini-api/docs/models/gemini-4-carbon)
- DeepSeek：[KV cache](https://api-docs.deepseek.com/guides/kv_cache/)、[Responses API](https://api-docs.deepseek.com/guides/responses_api/)、[Models](https://api-docs.deepseek.com/api/list-models/)、[Chat Completions](https://api-docs.deepseek.com/api/create-chat-completion/)、[Multi-round](https://api-docs.deepseek.com/guides/multi_round_chat/)
- 阿里云百炼：[Context Cache](https://help.aliyun.com/en/model-studio/context-cache)、[qwen3.8-max](https://help.aliyun.com/en/model-studio/qwen3-8-max)、[Create a response](https://help.aliyun.com/en/model-studio/qwen-api-via-openai-responses)、[OpenAI-compatible Responses](https://help.aliyun.com/en/model-studio/compatibility-with-openai-responses-api)
- 框架：[LangGraph Persistence](https://docs.langchain.com/oss/python/langgraph/persistence)、[AutoGen Managing State](https://microsoft.github.io/autogen/stable/user-guide/agentchat-user-guide/tutorial/state.html)、[AutoGen Teams](https://microsoft.github.io/autogen/stable/user-guide/agentchat-user-guide/tutorial/teams.html)
