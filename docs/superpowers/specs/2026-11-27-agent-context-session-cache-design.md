# 智能体会话、长上下文与缓存设计

**日期**：2026-11-27  
**状态**：设计稿 v0.4（二轮对抗审查的 9 个阻塞项已回填；待针对性复核与自动实施）  
**输入**：用户「智能体处理时系列要求」——长上下文、消息总线 session、历史/用户/线索信息、全链路上下文、单聊/群聊差异化、主流方案调研。  
**验收边界**：单聊长期跟进与群聊跨消息上下文必须同时通过。

---

## 1. 意图与动机

当前系统不是“完全没有上下文”，而是上下文被拆散在多条链路里：

- 消息总线已经生成稳定业务会话键 `channel:account:conversation`（`internal/channelgw/protocol.go:345-378`），但它没有作为 LLM session 传到模型调用层。
- MessageHub 已保存发送者、方向、会话/群、时间、消息 ID 等完整事实；销售历史却仍从 SessionMessage 取有限窗口，并在转换时丢掉发送者和时间。
- 客户、线索、对话记忆、共享业务备注的数据模型已经存在，但意图、情感、拟人、销售生成、线索检测各自只取其中一小部分。
- 单聊/群聊元数据已经进入 `IncomingContext`，却在构造 `SalesRequest` 时丢失。
- LLM 调度层没有 session、provider response ID、prompt cache 能力或 cached-token 计量；销售预算仍硬编码 8192 上下文，与真实 provider 能力脱节。
- 线索二次判定已有较好的单群聊差异化和共享业务上下文，但历史证据 ID 仍被合成为 `history_N`，且没有 session/cache 元数据。

目标不是把更多文本一次性塞进 prompt，而是建立一套**可复用、可追踪、可预算、可缓存、租户隔离的会话上下文契约**，让整条智能体链路消费同一份事实。

---

## 2. 目标 / 非目标 / 反目标

### 目标

1. 每条入站消息生成一份不可变的 `AgentConversationContext`，包含当前消息、历史、参与者、客户画像、线索事实、对话记忆、共享业务上下文及来源信息。
2. 将消息总线稳定会话标识端到端贯通到 LLM 调度层，并形成用户端持有的 opaque `llm_session_id`。
3. 支持供应商原生 session、prompt prefix cache 和无状态兼容三种模式；不支持的供应商不硬塞字段。
4. 意图、精细意图、情感/NLP、异议、拟人、销售 AgentLoop、线索检测/Judge/ReAct、Agent Runtime 的模型调用都使用对应场景的上下文视图。
5. 单聊与群聊共用上下文契约，但使用不同来源策略、提示词和隐私规则。
6. 按真实 provider 的上下文/输出能力分配预算，逐步提高现有 256/500/512/4096 等硬编码上限。
7. 记录上下文版本、前缀版本、session 复用、缓存命中、截断、降级和 token 用量，能解释每次模型输入包含什么、为什么省略什么。

### 非目标

- 不把所有 provider 都改造成同一种 session 协议。
- 不把个性化多轮回复重新送入“最终答案缓存”。
- 不在平台仓存储会话历史、客户画像、线索或 conversation 内容。
- 不无上限读取群聊/私聊全部历史；长上下文仍受质量、时延、成本和隐私预算约束。
- 不推翻 MessageHub 作为原始消息事实源，也不把 SessionMessage 升级为新的消息主数据。
- 本设计不直接修改产品代码；批准后才进入 SDD specify/plan/tasks。

### 反目标（出现即失败）

- 跨租户、跨渠道账号、跨会话/群读取历史或复用 session。
- 把模型推断写成已确认线索事实，或使用不存在的证据消息 ID。
- 把群内某个成员的隐私/线索信息暴露给另一个成员。
- 把当前时间、随机 ID 等易变内容放在缓存前缀前面，导致缓存永远 miss。
- 平台代理变成第二份用户会话存储。
- 仅放大历史条数，却不解决来源元数据、预算、摘要、并发和缓存前缀稳定性。

---

## 3. 现状与差距

| 能力 | 当前事实 | 差距 |
|---|---|---|
| 消息总线会话键 | `bridgeSessionKey` 生成 `channel:account:conversation`；群聊优先 group/conversation | 未传入 LLMConfig/DispatchRequest/平台代理 |
| 原始历史 | MessageHub 有完整元数据；仓储支持单聊/群聊作用域和 cursor | 销售仍用 SessionMessage 最近 200 条、4096 token；线索窗口有限；部分直连渠道出站未落 MessageHub |
| 用户/线索信息 | Customer、Lead、DialogueMemory、SharedBusiness 模型已存在 | 没有统一快照；消费端注入不一致 |
| 单群聊来源 | MessageHub/IncomingContext 有 IsGroup/GroupID/SenderID | `IncomingContext → SalesRequest` 丢失来源和发送者 |
| 意图/精细意图 | session/customer 参数存在 | LLM 兜底只收当前文本，MaxTokens 500/300 |
| 情感/NLP/异议 | 有规则和 LLM 分支 | 大多只看当前 content；无历史/画像/来源 |
| 拟人 | 评估/重生有人设、意图、平台 | 无历史、用户/线索、单群聊；MaxTokens 256 |
| 销售 AgentLoop | 有记忆、RAG、SOP、有限历史 | 历史只剩 Role/Content；system prompt 未完整注入客户/线索/来源 |
| 线索链路 | 有 historyScope、群角色、共享业务、ReAct | 无 session；证据 ID 伪合成；各阶段上下文仍分散 |
| LLM 调度 | 有本地答案缓存、provider ContextWindow | 无 session/cache 能力模型；cached tokens 不可见；销售预算硬编码 8192 |
| 平台代理 | 瞬时转发 OpenAI body，query 带租户/provider/trace 等 | 无 opaque session/cache key；usage 无 cached tokens |
| 共享 ConversationContext | 有 summary/tags/member roles/messages | 无版本/CAS，先读后全量 Upsert，并发会覆盖 |

另一个需要修正的细节：`inbox_ingress.go:907-913` 当前把 `SharedBusiness.PromptBlock()` 直接拼到 `aiContent`，业务备注因此进入“用户消息”正文。目标态必须把它作为独立、带来源/建议属性的上下文块注入，不能继续伪装成客户发言。

---

## 4. 关键设计决策

| # | 决策 | 理由 |
|---|---|---|
| D1 | MessageHub 是渠道消息的权威事实源；SessionMessage 继续承担 AI/业务台账。统一消息读模型以 MessageHub 优先、SessionMessage 补充未落 hub 的 AI/人工出站，并用 correlation ID 去重；同时补齐出站落 hub | 避免两套历史互相覆盖，也避免直连渠道丢失 AI 回复历史 |
| D2 | 消息总线 `SessionID` 是业务会话根身份；消息总线提供的 `LLMSessionID` 经 scope 校验后**直接作为 canonical continuation key**，全程携带到 dispatcher/provider adapter；缺失时才由服务端按 scope 确定性派生一次并回传，不由 scenario 重新派生。群聊根身份以 `group_id` 优先，conversation/thread 仅作子维度；私聊缺失稳定 SenderID 时拒绝用昵称派生身份 | 满足“直接作为续接键”，同时避免业务键泄露、跨 scope 复用、同群线程拆链和昵称碰撞 |
| D3 | 每条入站消息构建一次不可变上下文快照，各链路只读消费场景视图 | 保证意图、回复、拟人、线索看到同一事实 |
| D4 | session/长上下文默认由 user-server 直连已配置 provider；平台代理仅在部署显式开启时瞬时转发 body，且不持久化内容 | 遵守平台数据边界，同时避免把会话数据路径绑死在代理上 |
| D5 | provider 能力适配器区分 `stateless`、`prompt_cache`、`native_session` | 不向所有 OpenAI-compatible 厂商硬塞 `session_id` |
| D6 | 同一个消息总线 `LLMSessionID` 作为业务 continuation key；其下按独立 `ExecutionScenarioID` + provider/model/protocol/region 维护状态行；原 `DispatchScenario` 只负责路由/计费，不作 session 隔离维度 | 防止 Detect/Judge/ReAct 等共用低成本路由档位而串链 |
| D7 | 稳定提示前缀 + append-only 消息尾部；前缀版本独立管理 | 提高 prompt cache 命中率 |
| D8 | 长上下文按 provider 能力和场景预算启用，不默认无上限 | 控制成本、时延与质量退化 |
| D9 | 单聊/群聊使用同一上下文包、不同 SourcePolicy 和提示词块 | 同时满足两种场景 |
| D10 | 规则/embedding 快路径保留，但快路径也消费轻量 ContextView，用于指代补全、近期纠正/ negation 检查和冲突检测；所有 LLM 兜底和生成阶段必须上下文感知 | 不牺牲现有低时延路径，也不允许“高置信规则”绕过用户历史 |
| D11 | 客户/线索/备注必须带来源、时间、确认状态；模型建议不得冒充事实 | 可审计、防幻觉 |
| D12 | 共享上下文写入增加版本/CAS 或改为派生摘要；原始消息不再靠 JSON 数组全量覆盖 | 修复并发丢数据 |
| D13 | 新增 cached tokens、prefix hash、session reuse、fallback、truncation 观测 | 用证据证明优化生效 |
| D14 | 现有最终答案缓存资格守卫保持不变 | 个性化多轮不能复用旧回复 |
| D15 | 通过 provider/scenario 能力开关灰度，未知能力走保守兼容模式 | 避免一次性破坏现有渠道 |
| D16 | 当前输入、历史拉取、用户/线索事实分层组装；消息总线负责提供当前消息与元数据，user-server 按 scope 拉取授权历史 | 既携带消息总线信息，又避免把无界历史重复塞进入站事件 |
| D17 | 短回复也保留可解释上下文，只降低输出预算，不退回“只看当轮” | 上下文完整性与回复长度是两个独立维度 |
| D18 | 长短回复使用不同输出策略：短回复受控简洁，长回复按任务扩展；两者共享同一输入上下文 | 输出长度随任务提高，但避免所有回复无谓膨胀 |
| D19 | 百万级输入/输出按 provider/scenario 能力探测后灰度，不把厂商宣传上限直接当成已启用能力 | 控制质量、时延、成本与传输失败风险 |
| D20 | session 会话记忆、prompt 缓存、答案缓存严格分层，分别设计 key、TTL、失效和观测 | 避免把连续性、前缀计算和答案复用混为一谈 |
| D21 | MessageBus 未提供 `LLMSessionID` 时由服务端按 scope 确定性派生一次并回传；提供时经 scope 校验后直接作为 canonical continuation key，不按 scenario 重新派生 | 不伪造上游已有能力，也不要求所有渠道先升级才能工作 |
| D22 | 默认 `store=false`/无状态重放；仅 provider/model/protocol/region 白名单可启用 native session，并配置 TTL 与删除策略 | 避免供应商侧隐式长期保存对话 |
| D23 | 迟到消息按 `replied_through_cursor` 与 `LateReplyWindow` 处理：未回复且在时限内按 cursor 顺序补处理；已有更新回复则只合并进后续上下文 | CAS 只保证状态一致，不能代替用户可见的迟到消息策略 |
| D24 | 同一会话、同一 `ExecutionScenarioID` 的 native continuation 用全 scope 分布式租约覆盖 provider 调用、工具执行与提交，并按 MessageHub cursor 串行推进；租约失败时 fail-closed 或降级无状态 | 提交期 CAS 不能防止两实例同时用旧 continuation 发起请求；调用期 fencing 才能避免 response chain 分叉 |

---

## 5. 目标架构

```text
渠道 / Bridge / Webhook
  → MessageHub（原始消息、发送者、方向、时间、会话/群、平台消息 ID）
  → UnifiedMessageReader（MessageHub 优先 + SessionMessage 补充未落 hub 的 AI/人工出站，correlation ID 去重）
  → ConversationContextBuilder（user-server）
      ├─ 当前消息 + cursor 历史
      ├─ 参与者 / 群成员角色
      ├─ 客户画像
      ├─ 线索事实（confirmed / inferred / candidate + evidence）
      ├─ DialogueMemory / 历史摘要
      └─ SharedBusiness（备注、标签、来源、时间、是否建议）
  → AgentConversationContext（不可变、带 scope/version/provenance）
      ├─ Intent / FineIntent / Emotion / Objection ContextView
      ├─ Sales Prompt / AgentLoop ContextView
      ├─ Humanize ContextView
      └─ Lead Detect / Judge / ReAct ContextView
  → LLM Orchestrator
      ├─ ScenarioState（同一个 BusLLMSessionID 下按 scenario/provider/model/protocol 分状态行）
      ├─ ProviderCapability（context/output/cache/session）
      ├─ BudgetPlanner（稳定前缀 / 画像 / 摘要 / 历史 / 工具 / 输出）
      └─ ProviderAdapter（stateless / prompt cache / native session）
  → user-server LLMSessionState（BusLLMSessionID、provider continuation ID、cursor、version、expiry）
  → LLMRouter：会话/长上下文默认直连已配置 provider；平台代理仅在部署显式开启时承载瞬时 body 与 opaque key/usage
```

---

## 6. 统一上下文契约

### 6.1 `ConversationScope`

```go
type ConversationScope struct {
    TenantID          uint
    Platform          string
    ChannelAccountID  uint
    BusSessionID      string // channel:account:conversation
    BusLLMSessionID   string // 消息总线提供的 canonical continuation key；缺失时服务端按 scope 派生一次并回传
    SessionID         string // 业务会话 ID
    ConversationID    string
    IsGroup           bool
    GroupID           string
}
```

范围校验必须 fail-closed：租户、平台、渠道账号、会话/群至少能唯一确定；无法解析账号时只读精确来源历史，并禁用共享摘要/跨会话信息。

### 6.2 `AgentConversationContext`

```go
type AgentConversationContext struct {
    SnapshotID     string
    Scope          ConversationScope
    Current        ContextMessage
    Recent         []ContextMessage
    OlderSummary   *MemorySummary
    Participants   []Participant
    Customer       *CustomerContext
    Lead           *LeadContext
    SharedBusiness []BusinessFact
    LLMConversation LLMConversationRef
    InputBudget   ContextBudget
    OutputBudget  OutputBudget
    Provenance    ContextProvenance
}
```

`LLMConversationRef` 保存服务端 scope 关联键 `LLMConversationID` 与 scope hash；canonical continuation key 是 `Scope.BusLLMSessionID`，由消息总线提供或服务端在缺失时按 scope 确定性派生一次并回传。快照不绑定单个 provider response ID；dispatcher 在同一个 `BusLLMSessionID` 下按 scenario/provider/model/protocol 选择独立状态行，避免并行场景互相覆盖 native response chain。

`CustomerContext`/`LeadContext` 必须带 `SubjectType`（`dm_person` / `group` / `group_member`）与 `SubjectID`；群级客户/线索和成员级信息分开，成员级事实默认不进入群回复。

`ContextMessage` 至少包含：

- `MessageID`（优先 MessageHub.MsgID/PlatformMsgID）
- `Origin`（message_hub / session_message）与 `CorrelationID`（跨来源去重）
- `HistorySource`（message_bus / message_hub / session_message）与 `SourceMetadata`（渠道事件/回执来源，不存任意未授权字段）
- `Role`/`Direction`
- `SenderID`、稳定成员角色、显示名快照
- `Content`、媒体摘要
- `SentAt`
- `ReplyTo`、`Mentions`（渠道提供时才填，不编造）

`Provenance` 至少包含：上下文 schema 版本、prompt 版本、来源策略版本、包含/省略原因、截断位置、脱敏标记、摘要覆盖到的消息 cursor、快照时间。

群聊历史在进入模型前按 `Audience` 与成员入退群区间过滤：新成员不得读取入群前消息，非公开/仅部分成员可见内容不得进入群级上下文。渠道无法证明可见性时 fail-closed，只保留当前发言者、当前消息和可证明可见的最小窗口，并在 `Provenance` 记录 `visibility_unproven`。输出隐私守卫不能替代输入 audience 过滤；验收必须包含“新成员触发回复时看不到入群前成员隐私”的负向夹具。

快照在单次入站处理内不可变。`SnapshotID` 由 `MessageID + as-of cursor + 内容 hash + 事实版本 + 策略版本 + ContextSchemaVersion` 确定性生成，不能只用消息 ID 和 schema 版本；重试/异步阶段只有 manifest 与输入水位完全一致时才复用，否则重建并记录原因。默认只持久化 manifest、hash、来源 cursor、预算决策和 session 水位，不默认复制整份 prompt/历史到日志或第二张消息表。

为保证 native session 失效、重试和工具编排可重建，user-server 增加**加密、限权、带 TTL 的 scenario transcript 与 tool invocation ledger**：记录模型步骤、tool call/result、reasoning/签名（协议要求时）、SnapshotID、幂等键和最后应用水位；工具记录使用 `planned/authorized/dispatched/unknown/confirmed` 状态机，high-write 工具必须带工具级幂等键。已执行但状态为 `unknown` 的副作用禁止盲重试，必须先对账；恢复时重新校验授权、受众与资源版本。平台不存这些内容，日志只留 hash/ID。

“长期跟进”以渠道 conversation/group 为时间轴，不以单个业务 `SessionID` 为边界：MessageHub 历史可跨业务会话重建；客户/线索使用 subject 级授权事实；`DialogueMemory` 保留会话级细节，`OlderSummary` 保留 conversation/group 级长期摘要。业务 SessionID 切换、转人工或 Agent 切换不能无故丢失历史，但跨渠道账号合并仍需显式身份授权。

### 6.3 事实分级

每个注入块必须标注：

- `confirmed`：客户明确陈述或系统已确认记录
- `legacy_unverified`：旧格式共享备注/无确认状态的历史数据，只能作为待核实背景，不得复述为既成事实
- `inferred`：模型/规则推断
- `derived_summary`：由历史压缩产生的摘要，必须带 source cursor/版本，不能自动升级为确认事实
- `suggestion`：人工/AI 建议，尚未发生
- `external_untrusted`：网页、工具返回、第三方内容

线索判定输出只能引用输入中真实存在的 `MessageID`；无法验证时标记 `evidence_complete=false`，不得自动转正。

---

## 7. LLM Session 设计

### 7.1 四层标识

1. `BusSessionID`：消息总线现有稳定业务会话键，用于定位 conversation/group，不直接充当模型 continuation key。
2. `BusLLMSessionID`：消息总线提供的 opaque `llm_session_id`，经 scope 校验后**直接作为 canonical continuation key**，全程携带到 dispatcher/provider adapter。当前代码尚无此字段，实施时在 `IngestMessage`/`TriggerInboundMeta`/`IncomingContext` 增加并贯通；缺失时服务端按 scope 确定性派生一次并回传，后续消息必须复用该值，不按 scenario 重新派生。
3. `LLMConversationID`：user-server 按租户、平台、账号、单聊 conversation 或群 GroupID 派生的内部 scope 关联键，形如 `llmconv_<opaque-hash>`，用于审计、授权和状态分组，不替代 `BusLLMSessionID`。
4. `ProviderContinuationRef`：adapter 按 provider 协议保存的 `previous_response_id`/`previous_message_id`/`previous_interaction_id` 等引用；它依附于同一个 `BusLLMSessionID` 下的 scenario/provider/model/protocol 状态行。

服务端必须重新校验 scope，不能信任客户端任意值。同一个 `BusLLMSessionID` 若已被另一 tenant/account/conversation/group 绑定，必须拒绝并告警，不能静默改绑或跨 scope 复用。对 DeepSeek Responses 等 stateless 协议，该 key 仍用于 user-server 的幂等、路由、transcript 与 prompt cache 关联，但 adapter 不得宣称供应商支持 native continuation。

### 7.2 状态存储（user-server）

新增 `llm_session_state`：

- tenant/platform/account/conversation/group/`execution_scenario_id`/`dispatch_scenario`（两者分离：前者隔离执行链，后者仅路由/计费）
- `bus_llm_session_id`（canonical continuation key）
- `llm_conversation_id`（scope 关联键）
- provider/model/protocol/region
- `generation`
- `provider_continuation_ref`（仅 native session 模式）
- `last_message_id`/`last_message_cursor`（使用 MessageHub 的稳定 cursor，不假设存在连续序号）
- `replied_through_cursor`（迟到消息决策水位）
- `prompt_prefix_version`
- `context_schema_version`
- `retention_mode`/`expires_at`/`last_used_at`
- `version`（CAS）

唯一键为 `(tenant_id, channel_account_id, bus_llm_session_id, execution_scenario_id, provider, model, protocol, region, generation)`；`dispatch_scenario` 只保存路由/计费档位，不参与执行链隔离。`llm_conversation_id` 与 scope hash 同时保存用于授权和审计。不同执行场景共用 `BusLLMSessionID`，但使用独立状态行，避免 Detect/Judge/ReAct 等共用低成本路由档位后串链。

不存原始消息、客户画像或线索内容。provider continuation ref 过期/失效时，从最新快照按该 provider 允许的模式重建并递增 generation。

新增 `conversation_message_disposition` 逐入站台账，至少记录 tenant/account/conversation/message ID、稳定 cursor、pending/processing/replied/skipped/merged、处理 ExecutionScenarioID、出站回执与更新时间；回复水位只由连续且确认送达的出站推进，不能由某个执行场景的完成状态代替。`bus_llm_session_id` 同时用于唯一索引与敏感元数据保护时，采用密钥化 HMAC blind index（或确定性加密）保存索引值，原值加密保存，并定义 key rotation、TTL 清理和删除残留处理。

### 7.3 并发与乱序

- 同一 `ExecutionScenarioID` 的 ReAct 循环串行推进；不同 `ExecutionScenarioID` 使用独立状态行，避免并行覆盖。
- native continuation 使用覆盖 **provider 调用 → 工具执行 → 状态提交** 的全 scope 分布式租约/队列，并携带 fencing token；租约键包含 tenant/platform/account/conversation-or-group/execution_scenario。租约竞争或 Redis 不可用时 fail-closed，或显式降级为无状态模式，不得继续用旧 continuation 发请求。
- 同一会话的 native continuation 按 MessageHub 稳定 cursor 串行提交；CAS 只负责提交期保护，不能替代调用期 fencing。旧消息的迟到响应不得覆盖新消息的 provider continuation ref。
- 每条入站消息以 MessageID 幂等；上下文快照不可变。用户可见回复状态改为逐入站消息 disposition 台账（pending/processing/replied/skipped/merged），只有连续且确认送达的出站才推进回复水位；`ExecutionScenarioID` 的处理水位不能代替回复水位。
- 迟到消息以逐消息 disposition 与 `replied_through_cursor` 共同判断：尚未回复且仍在配置的 `LateReplyWindow` 内，按 cursor 顺序补处理；若已有更新的回复覆盖该水位，则只把迟到消息合并进后续上下文，不补发，不阻塞当前回复。
- 原生 session 与无状态回退必须产生相同的结果契约，失败原因可观测。

---

### 7.4 Native session 不替代业务上下文

供应商原生 session/response chain 只复用模型侧已有计算和对话状态，不替代 user-server 的事实源。**native session 模式与无状态全量 replay 是两种主策略，不在同一调用里叠加完整历史**（OpenAI Agents SDK 也明确建议一个 conversation 选择一种主状态策略，避免本地 replay 与 server state 重复上下文）。每次调用仍必须携带：

- 当前消息与真实 MessageID；
- 本次发生变化的客户/线索/记忆增量；
- 当前 SourcePolicy 与场景指令版本；
- 仅为证据/ grounding 所需的最小历史锚点；不重复发送供应商 session 已保存的完整历史。

无状态模式才按 `AgentConversationContext` 发送分层历史。发生 provider/model/protocol/prompt policy 切换、session 过期、证据纠错、历史撤回或长间隔恢复时，从最新快照全量重建并递增 generation。业务上下文始终以 user-server 快照为准，供应商 session 只是优化手段。

默认使用 `store=false`/无状态重放，避免供应商侧长期保存对话。只有显式白名单的 provider + model + protocol + region 才允许 native session/conversation，并必须配置 TTL、删除策略、租户开关和数据区域；未列入白名单时即使供应商支持也走无状态模式。白名单配置变化时递增 generation，停止复用旧 continuation ref。


## 8. 长上下文与缓存策略

### 8.1 Provider 能力模型

`ProviderConfig` 扩展：

- `ContextWindow`
- `MaxInputTokens`（模型/协议对纯输入上限，可能与 ContextWindow 不同）
- `ProtocolInputLimit`（如 Qwen Responses 的约 80% 输入上限）
- `MaxOutputTokens`
- `ReasoningReserveTokens` / `OutputSemantics`（输出是否包含 thinking/CoT）
- `CacheMode`: `none | automatic_prefix | explicit_cache`
- `CacheMinTokens`
- `CacheTTL`
- `SessionMode`: `none | native_response | provider_conversation`
- `NativeSessionAllowed`/`StoreMode`/`AllowedRegions`（默认 false/stateless；白名单启用）
- `SupportsIdempotencyKey`
- `MaxRequestBytes`/`RequestTimeout`
- `RouteMode`: `direct | platform_proxy`（会话/长上下文默认 direct，proxy 需显式开启）
- `ReliabilityTier`
- `Protocol`: `openai_chat | openai_responses | anthropic_messages | gemini | ...`

具体厂商参数与上限以配套官方调研为准；未知能力默认保守，不宣称支持百万上下文或缓存。已核实的差异包括：

- 当前默认 `gpt-4o` 只有 [128k context / 16,384 output](https://developers.openai.com/api/docs/models/gpt-4o)，不是百万模型；[GPT-5.6 Luna](https://developers.openai.com/api/docs/models/gpt-5.6-luna) 才是约 1.05M context / 128k output。
- [Anthropic 1M 型号](https://platform.claude.com/docs/en/build-with-claude/context-windows)单次 output 最高 128k，且 `input + cache_read + cache_creation` 都占窗口。
- [Gemini 4](https://ai.google.dev/gemini-api/docs/models/gemini-4-carbon) 默认同步 output 65,536；[implicit cache](https://ai.google.dev/gemini-api/docs/caching) 有新增 token 阈值，explicit cache 不是所有型号/API 都支持。
- [DeepSeek Responses](https://api-docs.deepseek.com/guides/responses_api/) 是 stateless，`previous_response_id`、`conversation`、`store`、`prompt_cache_key` 等不支持参数会被静默忽略；Chat Completions 必须客户端重放历史。
- [Qwen Responses](https://help.aliyun.com/en/model-studio/qwen-api-via-openai-responses) 最大输入约为 context window 的 80%，超限会**自动截断且不报错**；`previous_response_id` 有效 7 天，session cache 默认关闭。
- cached tokens 仍占上下文窗口；prompt cache 只降成本/时延，不减少输入预算。

完整矩阵、TTL、breakpoint/lookback、输出语义和官方引用见 `docs/research/AGENT-CONTEXT-SESSION-CACHE-2026-11-27.md`。

### 8.2 输入上下文与历史拉取

“携带消息总线历史/用户/线索信息”不等于把全部历史重复塞进入站事件。采用三层组装：

1. **MessageBus 当前输入**：当前消息、原始时间、方向、SenderID/Name、ConversationID/GroupID、IsGroup、平台消息 ID、可选 `BusLLMSessionID`；`IngestMessage.History` 若由消息总线提供，也按相同 scope 校验、MessageID 去重后作为授权历史增量，不能覆盖 user-server 权威事实。渠道若在受控 `Extra` 中提供用户/线索摘要，只作为带来源的增量。
2. **user-server 授权历史**：按 tenant/platform/account/conversation/group 作用域，用 MessageHub cursor 拉取跨业务 Session 的原始历史；SessionMessage 只补未落 hub 的 AI/人工出站。
3. **用户/线索/记忆事实**：Customer、Lead、DialogueMemory、SharedBusiness 按 subject 授权和事实分级注入；模型推断与建议不能升级为已确认事实。

百万级上下文是“可拉取、可分层、可预算”的能力，不是每次请求固定携带一百万 token。

消息总线提供的用户/线索增量必须使用 typed contract（字段白名单、subject、来源、更新时间、确认状态、scope hash），不能解析任意 `Extra` 文本为业务事实；冲突时 user-server 权威数据优先并记录差异。

### 8.3 分层预算

```text
L0 稳定系统指令 / 工具定义 / SourcePolicy
L1 参与者与客户/线索/记忆（缓慢变化）
L2 RAG、工具观察、共享业务上下文
L3 历史消息（较新原始窗口 + 较老结构化摘要）
L4 当前消息与当前时间
L5 输出预留
```

预算由选中 provider 的实际能力决定，而不是固定 8192。构建时先按场景产出“理想上下文 + 最小必需上下文”两份清单，dispatcher 依据 provider 能力选定路由后，再由 BudgetPlanner 夹取：

```text
有效输入上限 = min(
    ContextWindow - 实际输出预算 - ReasoningReserveTokens - 安全余量,
    MaxInputTokens,
    ProtocolInputLimit,
)
实际输出预算 = min(场景输出预算, MaxOutputTokens, ContextWindow - 最小输入 - 安全余量)
```

`MaxInputTokens`/`ProtocolInputLimit` 未配置时按最保守已知值处理，不能把 `ContextWindow` 直接当可输入窗口；输出是否包含 thinking/CoT 必须按 provider 语义计算。本地 token 估算不等于供应商真实计量，超限顺序：

1. 先降低低相关 RAG/工具观察；
2. 再压缩旧历史为摘要；
3. 再缩小群聊非相关线程；
4. 仍超限则 fail-visible 降级或切换 provider；
5. 绝不静默截断当前消息/证据后假装完整。

### 8.4 缓存前缀稳定性

- 系统指令、工具定义、来源策略等稳定内容放最前；跨场景共享的 base/system 前缀保持字节一致，场景差异块放在共享前缀之后，避免意图、销售、拟人各自从第一个 token 就开始分叉。
- 在租户/账号专属前缀起始处放 opaque scope hash，避免把供应商缓存隔离误当授权边界；跨租户即使提示文本相同也不得依赖缓存命中建立数据可见性。
- 当前时间、随机 trace、当前消息放尾部。
- 历史按时间顺序 append-only；不反复重写旧消息。
- 稳定属性/成员角色进入版本化前缀；经常变化的客户/线索/记忆以“新增量”追加到历史之后、当前消息之前，不每轮重写整份画像导致前缀失效。
- 成员显示名变化通过 roster version 管理；消息主体使用稳定 SenderID。
- `prompt_prefix_version` 与 `context_schema_version` 分开记录；策略变更显式使前缀失效。
- ReAct 工具观察按实际发生顺序追加。
- 供应商支持显式 cache control 时使用 adapter 参数；不支持时只依赖稳定前缀和 automatic cache。

**session ID 不是缓存命中保证**：它主要解决会话连续性、路由和 provider 原生状态复用；真正的 prompt cache 命中仍取决于 provider/model、相同 token 前缀、缓存 TTL 与最小可缓存长度。验收必须同时检查 prefix hash/version 与 cached tokens，不能只看到 session 复用就宣称缓存生效。

### 8.5 时延与成本门禁

- 每个 provider/scenario 分别记录冷调用与热调用 p50/p95 TTFT、输入/输出/cached tokens 和总成本；不能用单次命中代替性能结论。
- 支持缓存的场景，热调用必须保持相同 prefix hash/version 且出现 cached tokens；建议灰度门禁为热调用 p50 TTFT 较冷调用降低至少 20%，最终 SLO 在 plan 阶段按现网基线确认。
- 若长上下文使质量、时延或成本变差，自动降级到较小窗口/摘要模式，并记录降级原因，不静默继续烧 token。

### 8.6 输出预算

废除 256/500/512 等统一小上限，改为场景配置并受 provider 能力约束。建议初始值（可调）：

| 场景 | 建议 MaxOutputTokens |
|---|---:|
| 意图/精细意图/情感/异议结构化判断 | 1024 |
| 拟人评估/重生 | 1024 |
| 线索检测/Judge | 2048 |
| ReAct 单步 | 2048 |
| 销售短回复 | 512–2048 |
| 销售长回复/方案说明 | 4096–8192 |
| 会话摘要 | 1024 |

这是设计默认值，不是供应商上限声明；实施时由能力注册表夹取并记录实际值。短回复与长回复共享同一输入上下文，只改变输出预算和表达策略；长回复仅在用户明确要求“详细/方案/完整步骤”、任务本身需要长结构化输出或既有场景配置启用时使用，其余保持简洁。`finish_reason=length`、结构化输出不完整或证据缺失时必须显式降级/重试，不能把截断结果当完整答案。

### 8.7 与现有答案缓存的关系

- 供应商 prompt cache：复用输入前缀计算，不复用答案。
- 本地最终答案缓存：继续只服务纯知识新问；个性化、工具、追问、转人工、脚本/流程等仍然旁路。
- Messages 非空时本地响应缓存继续 bypass，不因新增 session 字段而意外命中旧答案。

### 8.8 三种机制必须分层

| 机制 | 解决什么 | key / 状态 | 失效条件 | 不能做什么 |
|---|---|---|---|---|
| session 会话记忆 | 多轮连续性、native response chain、业务上下文锚点 | `LLMConversationID + scenario + provider/model/protocol + generation`；user-server 存 opaque 状态 | 模型/协议/策略切换、过期、证据纠错、删除、长间隔恢复 | 不保证 prompt cache 命中；不替代权威历史 |
| prompt 缓存 | 复用相同输入前缀的 KV/计算，降低 TTFT 与成本 | provider/model + 稳定 token 前缀 + cache TTL/最小长度；prefix hash 仅用于观测 | 前缀任一 token 变化、TTL 过期、模型/缓存控制变化 | 不复用答案；不能作为租户授权边界 |
| 答案缓存 | 复用最终回复，避免重复推理 | 现有严格资格 key：同租户/账号/模型/提示/KB/版本等 | 任何个性化、工具、动态资产、追问、流程条件变化 | 个性化多轮、群聊、转人工、审核/承诺类回复不得命中 |

三者可以同时存在，但验收和计量必须分开：session reuse 不等于 cache hit，cache hit 不等于答案可复用。

---

## 9. 单聊 / 群聊差异化

### 9.1 单聊

- 明确双方身份与方向。
- 强调长期关系、客户画像、历史承诺、纠正、异议、情绪和未决问题。
- 当前消息是客户意图的主要来源；历史用于解析指代和连续纠正。
- 不注入群策略或无关成员信息。

### 9.2 群聊

- 必须有当前发言者、稳定 SenderID、成员角色、@/回复关系（若有）。
- 区分“对 AI/商家说的消息”和“群内环境聊天”；不能把所有群消息都当成当前用户意图。
- 使用群级摘要 + 相关线程/参与者原始消息，而不是无限读取全群。相关性选择必须确定性且记录原因；当前 @、回复、直接询问的对象以及最近原始窗口属于最小必需，不得因相关性打分被静默过滤，兜底时保留最近群消息并标记噪声压缩。
- 识别多人需求、供货、资源、撮合角色及冲突/共识。
- 成员级画像、线索和私人备注默认不进入群聊模型输入；只允许群级已确认事实和当前发言者必要公开元数据。成员级信息仅在明确私聊、授权或单独通知场景使用。
- 回复默认限定当前发言者/线程；未触发时不主动接管全群。未触发消息仍进入群级上下文，供后续触发消息理解背景，但不生成回复。
- 不把某成员的电话、订单、私人备注或线索细节广播给全群。

### 9.3 共用部分

两者共用同一上下文 schema、预算器、session 管理和观测。差异只由服务端 `SourcePolicy` 生成，不允许消息内容自行修改策略。

---

## 10. 全链路接入

| 链路 | 必须消费的上下文 | 改造要点 |
|---|---|---|
| 所有客户会话 LLM 调用 | scope、当前消息、授权历史、用户/线索摘要、来源/单群聊、当前发言者 | 统一携带 `ContextSnapshotID`/`LLMConversationRef`；各场景只选择最小必要视图，不得只看当轮 |
| 意图/精细意图 | 当前消息、近期历史、客户/线索摘要、来源、当前发言者 | 规则/embedding 快路径也接收轻量 ContextView，做指代补全、近期纠正/否定检查与冲突检测；LLM 兜底改为 `RecognizeWithContext` |
| 情感/NLP/转人工 | 当前消息、近期情绪轨迹、历史紧急/转人工事件、来源 | 规则输出结合近期轨迹，不能只看 6 字否定窗口；冲突或低置信时升级为上下文 LLM |
| 异议 | 当前消息、历史异议、客户阶段、已确认承诺 | `classifyByLLM` 接收 ContextView |
| 销售生成/AgentLoop | 完整分层上下文、客户/线索/记忆、RAG/SOP、单群聊策略 | 历史改走 UnifiedMessageReader；SalesRequest 保留来源元数据；dispatch 带 session |
| 拟人评估/重生 | 近期多轮、意图轨迹、人设、关系/来源 | HumanizeEvalInput 增加 ContextView；评估与重生共用 scenario session |
| 线索检测 | 当前消息、相关历史、发送者、真实 MessageID、共享业务 | DetectionInput 增加 scope/context；提示中显式带 ID |
| 线索 Judge/ReAct | 完整线索上下文、群角色、事实/建议、工具证据 | 复用同一快照；每轮 dispatch 带 session；工具观察保留来源 |
| Agent Runtime | SessionID、历史、资产/工具观察 | LocalLLMClient 复用统一能力适配，不再成为无 session 的独立通道 |
| 摘要/记忆生产 | 原始消息、既有摘要、事实/异议/意图轨迹 | 作为上下文构建的一部分，版本化并记录覆盖 cursor |

地理、浏览器、知识库、SOP、反馈、trace 等通用 LLM 调用不自动注入客户会话；只有场景白名单和最小必要字段进入上下文。

---

## 11. 数据与接口改动

### user-server

- `TriggerInboundMeta` 保留原始 `SentAt`、`PlatformMsgID`、`BusSessionID`、`BusLLMSessionID`，以及消息总线在受控合同中提供的 `History`/用户/线索增量；所有增量必须带 scope 与来源，停止用 `time.Now()` 覆盖原始时间。
- `IncomingContext` 增加 `ConversationID`、`BusSessionID`、`BusLLMSessionID`、`Direction`、`OriginalSentAt`、`PlatformMsgID`。
- `SalesRequest` 增加 `ConversationContext` 或 `ContextSnapshotRef`，并保留 IsGroup/GroupID/SenderID/MessageID。
- `HumanizeEvalInput`、`DetectionInput`、意图/异议接口增加 ContextView；旧签名可兼容保留，但生产路径必须走新接口。
- `DispatchRequest`/`LLMConfig` 增加 `LLMSessionRef`、`PromptPrefixVersion`、`ContextSnapshotID`、`CachePolicy`。
- 统一 `SystemPrompt` 与 `Messages` 的优先级契约：两者都给时只前置一次 system；Messages 已以 system 开头时不重复；禁止当前 `Messages` 非空时静默忽略 `SystemPrompt` 或同一提示重复注入。
- `ChatMessage` 增加可选 sender/message/time/source 元数据；旧调用只填 Role/Content 仍可工作。provider adapter 必须把元数据确定性、可转义地渲染为协议支持的结构或内容前缀，不能向不支持的厂商发送自定义 JSON 字段。
- prompt 渲染必须 canonical：工具、JSON schema、成员、上下文块、历史消息使用稳定排序和固定分隔符；禁止 Go map 随机遍历直接生成 prompt，否则同一会话每次前缀哈希都可能不同。
- 同一 conversation/group + `ExecutionScenarioID` 的 native continuation 使用全 scope 分布式租约/队列，持锁覆盖 provider 调用、工具执行和提交，并按 MessageHub 稳定 cursor 串行；不同执行场景仍可并行。租约带 fencing token，获取失败时 fail-closed 或显式降级无状态。LLM 请求增加 `RequestIdempotencyKey`（message + execution scenario + step），供应商支持时透传，避免超时重试重复追加 response chain。用户可见回复状态使用逐入站消息 disposition 台账，只有连续且确认送达的出站才推进回复水位。
- 响应必须校验 `finish_reason`、结构化 JSON/Schema、证据 ID 和输出长度；截断或校验失败走显式重试/降级，不把不完整结果发送给客户。
- 媒体只使用受控摘要或已授权代理；不得让模型/工具直接抓取任意 MessageHub URL，防 SSRF 与跨域数据泄露。
- `TokenUsage`/响应增加 cached input/cache creation/cache read tokens（供应商提供时）；流式响应也必须保留 session/provider response ID 与最终 usage，不能因 stream 模式丢失计量。
- `ConversationContext` 增加版本/CAS；原始渠道历史以 MessageHub 为权威，SessionMessage 仅补充未落 hub 的 AI/人工出站；SessionMessage 增加可选 `CorrelationID`（或由出站回执/请求 ID 确定性生成），统一读模型按 correlation ID 去重。
- 迁移时停止向 `ConversationContext.Messages` JSON 追加原始历史：保留 legacy 字段只读兼容，summary/tags/member roles 带 source cursor/version；新写入走 MessageHub 与 CAS 更新，待所有读者迁移后再退役 legacy JSON，避免迁移期间双写覆盖。
- 补齐所有直连渠道 AI/人工出站的 MessageHub 持久化与平台回执；在 parity 完成前不得丢失 SessionMessage 中的出站历史。
- 新配置同步 `.env.example` 与 `docs/operations/PROD_TOPOLOGY_CHANGES.md`。

### 平台代理

- **路由优先级**：`Source=platform` 的租户没有本地 provider key，必须强制走显式平台代理，不能被“默认直连”覆盖；只有 `Source=self`（现有自备 Key 来源）、`RouteMode=direct` 且租户允许时才直连。`direct` 是 RouteMode，不是新的 Source 枚举值；session/长上下文默认直连仅适用于后者。
- 显式代理模式下，opaque `llm_session_id`、`context_version`、`prompt_prefix_version` 走独立 header（纳入 HMAC 签名），不放 query；代理只做无状态协议转换和瞬时转发，不解析 session 映射、不记录原始 key/body。
- 代理不透传客户/对话/线索内容字段，不持久化 session 映射或 provider continuation ref。若部署策略不允许对话 body 经过平台代理，`Source=platform` 必须明确报不可用或由租户配置本地 key 后转直连，不能静默绕过。
- 直连模式下，user-server 自行记录 provider usage；显式代理模式下，代理回传 cached read/create tokens 等计量，不记录会话正文。
- P0 必须冻结版本化跨仓契约：endpoint、body、header、HMAC、协议能力与错误码。**仅当 provider 原生幂等协议可保证结果一致时才允许代理重放原响应**；否则返回 `outcome_unknown`，由 user-server 的加密 ledger 对账并从最新快照重建。平台不得为幂等重放存储响应正文；若未来要求原样重放，必须单独立项修改数据边界。
- 原生 Responses/Anthropic/Gemini 等协议需要能力化 endpoint/body adapter；当前固定 `/chat/completions` 不能覆盖所有模式。现有 Anthropic 分支使用 OpenAI body 的兼容问题必须在启用其缓存/session 能力前修正。
- **缓存折扣默认不透传**：代理路径在计价契约支持前按普通输入计费，只如实记录 cached tokens，不宣称租户已获得缓存折扣；实现折扣后再版本化开启，不能一边按全量收费一边报告成本收益。
- 当前 8MB 请求体上限可能挡不住百万级上下文（尤其中文、工具定义和媒体元数据）；显式代理模式下改为按 provider/scenario 配置 `MaxRequestBytes` 与超时，并保留安全上限，不能靠无限放大掩盖预算问题。
- 跨仓 JWT+HMAC 契约文档同步更新；默认直连不得改变现有平台数据边界。

---

## 12. 安全、隐私与可观测

- 所有历史/画像/线索/session 查询带 tenant + platform + account + conversation/group 作用域。
- 客户消息、工具返回、网页内容视为不可信数据，使用明确分隔，不得覆盖系统策略。
- 按场景脱敏；群聊默认最小化 PII 和成员私有信息。群聊输出在发送前还必须经过确定性隐私守卫（成员级标识、电话/邮箱/订单号/私有备注规则），不能只依赖模型提示词自律。
- 长上下文会增加外发数据量；必须沿用用户既有 provider 配置，不新增默认外发路径；provider/scenario 支持租户级关闭和数据区域约束。
- session/provider response ID 设置 TTL；客户/会话删除时同步清理派生状态。供应商提供删除 API 时同步删除 native session/response；不支持时停止引用并等待过期，记录为 best-effort，不谎称已从供应商侧删除。
- provider continuation ref、`BusLLMSessionID` 和 session 状态按敏感元数据加密/限权保存；日志只记录 hash，不记录可反推业务会话的原文。
- 每次调用记录：snapshot ID、scope hash、session ID hash、provider/model、prompt prefix hash/version、输入/输出/cached tokens、历史条数、摘要覆盖 cursor、截断/降级原因、TTFT、总时延。
- 不把完整 prompt 默认写入日志；排障需显式权限和脱敏。
- 客户会话场景的 shared notes、工具 args/output、AI 输入输出在进入应用日志、trace 与 message_trace 前默认 hash/脱敏；完整内容只进入加密、限权的 scenario ledger。真实 DB 负向测试必须断言 PII、私有备注和工具原文不出现在日志/trace。

---

## 13. 验收矩阵

### 13.1 单聊长期跟进（必须通过）

| ID | 场景 | 通过标准 |
|---|---|---|
| S1 | 多轮指代 | 前文出现预算/行业/异议，后文“就按上次说的”能被意图和回复正确解析 |
| S2 | 多轮纠正 | “不是这个，是另一个”后，上下文保留纠正后的实体，不回到旧值 |
| S3 | 用户/线索信息 | 回复使用授权范围内的客户画像和已确认线索事实，并区分 inferred/suggestion |
| S4 | session | 消息总线 `BusLLMSessionID`（或服务端缺失时派生值）直接作为 canonical continuation key 端到端贯通；同一单聊多次调用复用同一 key，并在其下按 `ExecutionScenarioID`/provider/model/protocol/region 维护独立状态行（原 `DispatchScenario` 仅路由/计费）；native 模式验 adapter continuation、过期/失效重建和 usage，无跨会话串用 |
| S5a | prompt cache | 仅在 `CacheMode != none` 且请求达到最小可缓存长度时，验 prefix hash/version 一致并出现 cached tokens；不支持时明确记录 unsupported，不伪造命中 |
| S5b | 时延 | 对同一 prompt-cache 模式比较冷/热 p50/p95 TTFT，达到 §8.5 灰度门禁；native session 的收益单独计量，不与 cache hit 混为一谈 |
| S6 | 来源策略 | 使用 DM SourcePolicy，不包含群策略 |
| S7 | 隔离 | 换租户/账号/会话后，历史、画像、线索、session 均不可见 |
| S8 | 输入/输出与缓存分层 | MessageBus 当前消息/元数据与 user-server 跨 Session 历史、用户/线索事实均进入上下文；长输入/长短输出按 provider 能力灰度；session reuse、prompt cache hit、answer cache 分别可观测且不混用 |

### 13.2 群聊跨消息上下文（必须通过）

| ID | 场景 | 通过标准 |
|---|---|---|
| G1 | 多人跨消息 | A 提产品、B 补预算、C 问交付，回复正确综合三人信息并标注来源 |
| G2 | 发言者归属 | 当前 SenderID/昵称/角色和 @/回复关系（若有）进入模型；证据引用真实 MessageID |
| G3 | 噪声隔离 | 无关群聊不被当作当前用户意图或客户需求 |
| G4 | 来源策略 | 使用 Group SourcePolicy，包含成员角色和多人意图规则 |
| G5 | 隐私 | 不把某成员私有 PII/备注/线索暴露给全群 |
| G6 | session/隔离 | 消息总线 `BusLLMSessionID`（或服务端缺失时派生值）直接作为 canonical continuation key；同群复用同一 key，并在其下按 `ExecutionScenarioID`/provider/model/protocol/region 维护独立状态行，`DispatchScenario` 只用于路由/计费；native 模式验 continuation/失效重建，换群/账号后不串历史 |
| G7a | prompt cache | 仅在 `CacheMode != none` 且达到最小可缓存长度时，验稳定前缀 + append-only 历史、prefix hash/version 与 cached tokens；成员/策略版本变化显式失效 |
| G7b | 时延 | prompt-cache 模式比较冷/热 p50/p95 TTFT 并通过 §8.5 门禁；native session continuation 收益单独记录 |
| G8 | 输入/输出与缓存分层 | MessageBus 当前消息/元数据与 user-server 跨消息历史、用户/线索事实均进入上下文；长输入/长短输出按 provider 能力灰度；session reuse、prompt cache hit、answer cache 分别可观测且不混用 |

### 13.3 全链路共同门禁

- 意图、精细意图、情感/NLP、异议、拟人、销售 AgentLoop、线索 Detect/Judge/ReAct 的生产 LLM 路径均带 `ContextSnapshotID`/`LLMConversationRef`，dispatch 时解析具体 `LLMSessionRef`。
- 使用黄金夹具做机械判据：实体纠正后旧值不得复现；群聊证据必须引用真实 MessageID；隐私禁止串与跨 scope 隔离用负向断言；另含超过旧 8192 输入/4096 输出上限且无 `finish_reason=length` 的长上下文夹具。
- 同一会话并发/乱序入站时，native session 按稳定 cursor 顺序提交；迟到响应不覆盖新状态；超时重试不重复追加 response chain。
- prompt 渲染 canonical，固定夹具重复生成的前缀 hash 完全一致；工具/schema/成员顺序变化受版本控制。
- 输出截断、JSON/Schema 失败、证据 ID 不存在时不得直接发送；群聊输出通过隐私守卫。
- provider 能力未知时走兼容模式；native session 失效可无状态重建；fallback 到较小 provider 时必须重新预算和渲染，不能原样转发超限 prompt。
- 会话/长上下文默认直连；只有显式代理模式才允许 body 瞬时经过平台，直连与代理两种路径的 usage/session 观测一致，平台均不存正文。
- 默认 `store=false`/无状态重放；native session 仅在 provider+model+protocol+region 白名单内启用，租户可关闭，白名单或数据区域变化时递增 generation 并重建。
- 迟到消息按 `replied_through_cursor` 与 `LateReplyWindow` 验收：未回复且窗口内按 cursor 顺序补处理；已有更新回复时只合并进后续上下文，不补发。
- 长上下文启用前后使用同一组单聊/群聊夹具对比意图、回复、线索证据准确率；质量不下降才扩大灰度，否则自动回到摘要/小窗口模式。
- 最终答案缓存不命中个性化多轮/工具路径；session、prompt cache、answer cache 的指标和失效分别可查。
- 平台无会话正文/画像/线索持久化。
- `go build`、`go vet`、gofmt、受影响包真库测试全绿；测试库密码缺失时如实报环境失败，不用 skip 冒充通过。

---

## 14. 实施切片（批准后拆 tasks）

| 切片 | 内容 | 独立验收 |
|---|---|---|
| P0a | 上下文/session/capability 契约；范围校验；MessageBus 入站字段与元数据保全；`BusLLMSessionID` 直接作为 continuation key；`ExecutionScenarioID` 与路由 scenario 分离；盘点 UnifiedMessageReader 与出站持久化差距 | 单聊/群聊 fixture 能构建同构快照，总线 key 端到端贯通，跨 scope 全拒绝；盘点项有明确责任与前置关系 |
| P0b | 最小可用 UnifiedMessageReader（MessageHub 优先 + SessionMessage 补充 + correlation 去重）与所有 AI/人工出站持久化 parity | AI 出站不丢失、跨来源不重复；P1 不得在 P0b 前开工 |
| P1 | cursor 历史、客户/线索/记忆/共享业务组装；ConversationContext CAS/版本化 | 并发 append 无覆盖；摘要带 source cursor/version |
| P2 | LLMSessionState、按 `ExecutionScenarioID` 隔离的状态/transcript/tool ledger、调用期租约与逐消息 disposition、最小预算器、`Source=platform` 强制代理/自带 Key 直连路由、默认无状态/native 白名单、dispatcher/provider adapter、cached-token 计量、canonical prompt 渲染、基础观测与日志/trace 脱敏、确定性群聊输出隐私守卫 | 会话复用/过期/乱序/重试通过；fixed 执行场景不串链；锁失败按策略降级；逐消息回复水位正确；前缀 hash 稳定；缓存/session 指标分开；平台不存内容；日志/trace 无 PII 原文；群聊隐私守卫先于任何真实群回复；长输入/长输出无静默截断 |
| P3 | 意图/NLP/情感/异议/拟人接入 | 所有 LLM 兜底带上下文；S1-S4、S6-S8 功能项通过；S5a 在支持的 provider 夹具上通过 |
| P4 | SalesEngine/SmartCS/AgentLoop 接入 | SalesRequest 不再丢来源；S1-S4、S6-S8 功能项通过；S5a 在支持的 provider 夹具上通过 |
| P5 | Lead Detect/Judge/ReAct + Agent Runtime 接入 | G1-G6、G8 功能项通过；G7a 在支持的 provider 夹具上通过；证据 ID 全部真实 |
| P6 | 完整 SLO/冷热性能、长上下文质量对比、灰度、文档与回归 | S5b/G7b 与质量门禁通过；三种缓存分层；双场景共同门禁全绿 |

---

## 15. 风险与推翻条件

- **长上下文质量退化**：若盲塞全历史导致指代准确率、时延或成本变差，回到分层摘要 + 相关原始窗口，不推翻统一上下文契约。
- **供应商能力差异**：若某厂商不支持 native session/cache，保留无状态兼容；不能为了统一字段破坏请求协议。
- **平台数据边界**：若平台方案要求持久化 conversation/session 内容，直接否决该方案。
- **群聊隐私**：若无法证明成员级信息不会泄露给全群，群聊只使用群级事实和最小必要字段。
- **出站历史不完整**：已核验 Telegram/企微等会落 MessageHub，但部分直连 webhook 渠道（如 DingTalk）发送成功路径未见 hub 持久化；若直接切成“只读 MessageHub”，AI 历史会凭空消失。必须先实现 UnifiedMessageReader + 出站 parity，不能静默丢 SessionMessage 出站。
- **共享上下文并发**：若 CAS/append 改造未完成，不允许在其 Messages JSON 上继续堆叠高并发写入。
- **缓存版本冲突**：若策略/工具定义变化导致前缀频繁失效，优先稳定公共前缀，将动态策略放尾部或按版本分支。
- **token 估算误差**：中文、特殊 token、工具 schema 与供应商 tokenizer 不一致时，本地估算可能偏小；BudgetPlanner 必须留安全余量，并用供应商 usage 回写校准，不能把估算值当硬上限。
- **长请求传输限制**：平台代理 8MB 上限、HTTP 超时和反向代理限制都可能先于模型上下文上限失败；上线前必须按真实长上下文夹具做端到端传输验证。
- **最终答案缓存污染**：若新增 session 字段扩大本地答案缓存命中范围，立即回滚该缓存键变更。

### 15.1 对抗审查补充问题（必须纳入设计与验收）

| # | 未显式提出但必须解决的问题 | 强制对策 / 验收 |
|---|---|---|
| A1 | Go map/切片顺序、JSON key 顺序、工具 schema 顺序不稳定，会让同一会话前缀每次不同 | canonical 渲染与稳定排序；固定夹具重复生成 prefix hash 必须完全一致 |
| A2 | 本地 token 估算与供应商 tokenizer 不一致，中文、emoji、工具 JSON 可能超限 | 安全余量 + usage 回写校准；超限时重新预算，不静默截断 |
| A3 | 同一会话多条入站并行时，native response chain 可能乱序 | 按 MessageHub cursor 串行提交；迟到响应 CAS 拒绝覆盖 |
| A4 | 请求超时但供应商已成功，自动重试可能重复追加 response chain | message+scenario+step 幂等键；支持时透传，不支持时谨慎重试并重建 |
| A5 | 长上下文 provider 失败后 fallback 到小窗口 provider，原 prompt 可能直接超限 | fallback 必须重新选择上下文、预算和渲染，记录丢弃内容 |
| A6 | 输出被 `finish_reason=length` 截断或 JSON/Schema 不完整 | 发送前校验；显式重试/降级，禁止把截断结果当完整回复 |
| A7 | 群聊隐私只靠提示词，模型仍可能复述成员 PII/私有备注 | 发送前确定性隐私守卫 + 群聊最小权限上下文 |
| A8 | MessageHub 媒体 URL 可能指向内网/恶意域名 | 只使用授权媒体代理或摘要；禁止任意 URL 抓取，防 SSRF |
| A9 | 历史摘要可能幻觉，被下游当成 confirmed 事实 | 摘要标为 `derived_summary`，带 source cursor/版本/证据，不自动升级为确认事实 |
| A10 | 本地删除客户/会话后，供应商 native session 可能仍存在 | 能删除则同步删除；不能则停止引用并等待 TTL，记录 best-effort |
| A11 | cached tokens 若仍按普通输入计费，会削弱收益甚至双重计量 | usage 与计价能力分离；按供应商 cached/read/create 口径计量并对账 |
| A12 | Agent Runtime checkpoint 恢复后可能沿用旧上下文/session | checkpoint 记录 SnapshotID/ContextSchemaVersion；恢复时校验并重建过期快照 |
| A13 | 模型、协议、工具定义或 prompt policy 静默升级，导致旧 session 缓存失真 | capability/config fingerprint 纳入 generation；变化时显式失效并全量重建 |
| A14 | 消息纠错、撤回、脱敏规则变更后，native session 仍“记得”旧内容 | 权威历史变更事件触发 session rebuild；不能只更新本地摘要 |

---

## 16. 用户已确认的关键决策

1. 会话/长上下文默认由 user-server 直连已配置 provider；平台代理仅在部署显式开启时承载瞬时 body，且不持久化内容。
2. 消息总线 `LLMSessionID` 经 scope 校验后直接作为 canonical continuation key；缺失时由服务端按 scope 确定性派生一次并回传。
3. 默认 `store=false`/无状态重放；native session 仅对显式白名单的 provider/model/protocol/region 开启，并受 TTL、删除与租户开关约束。
4. 迟到消息按 `replied_through_cursor + LateReplyWindow` 处理：未回复且窗口内按 cursor 顺序补处理；已有更新回复则只合并进后续上下文。
5. 长上下文按 provider/scenario 能力灰度开启，不默认对所有渠道使用最大窗口。
6. 群聊采用“群级摘要 + 相关线程/参与者原始消息”，不无限读取全群。
7. 不同 `ExecutionScenarioID` 使用独立状态行/continuation chain，但共享同一个总线 `LLMSessionID`；原 `DispatchScenario` 只负责路由/计费。

---

## 17. 配套调研

外部调研文档：`docs/research/AGENT-CONTEXT-SESSION-CACHE-2026-11-27.md`（已完成官方一手核对，包含厂商能力矩阵、框架模式、20 项遗漏问题与误区清单）。

---

## 18. Builder check

独立 reviewer 已完成两轮审查。第一轮提出预算、缓存验收、transcript 可重建性和切片依赖问题，已回填 v0.2/v0.3；第二轮结论为 **NO-GO**，指出 9 个实施阻塞项，包括执行场景隔离、群聊根键、audience 输入过滤、调用期 fencing、跨仓代理契约、切片依赖、旧备注事实分级、工具副作用幂等和日志/trace 泄露。v0.4 已逐项回填。用户已授权“二次论证后自动实施”；本稿需再做一次针对性复核，无新阻塞即自动进入 SDD 实施。

---

## 19. 二轮对抗审查的非阻塞风险与默认决策

以下风险不阻塞架构方向，但必须进入 SDD tasks 与验收：

| # | 风险 | 强制落点 |
|---|---|---|
| R1 | fake provider 只能验请求字段，不能证明真实缓存命中 | 单元/契约测试与真实 provider/sandbox 分层；后者必须给出 cold/hot、cached tokens、p50/p95；DB skip 不计通过 |
| R2 | `CacheMode` 过粗，无法表达 breakpoint/cached object/session-cache header | 能力模型细分 implicit/explicit/breakpoint/cached_object/provider_session_cache；history/cache/retention/replay 独立，未知能力保守降级 |
| R3 | prefix hash 若只 hash 领域 prompt，不能证明真实供应商前缀一致 | hash adapter 完成 tools/system/messages 重排、metadata 渲染、cache control 后的实际 provider 请求；计量分母按厂商定义 |
| R4 | cursor 与编辑/撤回语义不固定 | 明确不可变 cursor；edit/delete/recall 产生 tombstone，使 native generation 与旧摘要失效，并保留可追溯来源 |
| R5 | `bus_llm_session_id` 加密与唯一索引冲突 | 原值加密，唯一索引用密钥化 HMAC blind index 或确定性加密；覆盖 key rotation、TTL 清理与删除残留 |
| R6 | 现有 `Generate` 只返回 string，无法验截断/usage | 新增 checked output API，返回 finish reason/usage；发送前验 JSON Schema、证据 ID、fallback 重预算 |
| R7 | OneID 跨渠道归一会让同客户不同账号自动串历史 | 增加跨账号同客户负向测试；无显式授权时历史、画像、线索和 session 不得跨账号合并 |
| R8 | 群聊质量不能只看回复准确率 | 增加误触发率、噪声压缩率、跨成员证据归属正确率、隐私拦截率与 context rot 指标；单/群聊长上下文前后均通过才扩灰度 |

**代理缓存折扣默认决策**：v0.4 不向租户透传供应商缓存折扣，代理路径按现有输入计费，且在缓存计价契约完成前不得宣称代理路径获得缓存成本收益。未来若产品要求透传折扣，再作为计费合同变更单独评审。
