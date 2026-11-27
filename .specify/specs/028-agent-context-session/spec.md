# Feature Specification: Agent Context, Session & Cache

**Created:** 2026-11-27  
**Status:** Draft — v0.4 targeted review pending; implementation is authorized to proceed automatically after GO  
**Design:** `docs/superpowers/specs/2026-11-27-agent-context-session-cache-design.md`  
**Research:** `docs/research/AGENT-CONTEXT-SESSION-CACHE-2026-11-27.md`

## Input

用户要求：LLM 调用携带消息总线提供的 `llm_session_id`、用户历史对话、用户信息与线索信息；意图、拟人、情感/NLP、销售 AgentLoop、线索 ReAct 全链路不得只看当轮；区分单聊与群聊；充分利用百万级输入与更大输出；区分 session 会话记忆、prompt 缓存与答案缓存；单聊长期跟进与群聊跨消息上下文必须同时通过。

用户已确认：默认直连、代理显式开启；总线 `LLMSessionID` 直接作为续接键；默认无状态、native session 白名单；迟到消息未回复则补、已回复则合并。用户授权“头脑风暴二次论证后自动实施”。

## User Scenarios & Testing

### US1 — 单聊长期跟进（P1）

**Independent Test:** 使用同一单聊 conversation 的多轮夹具，跨业务 SessionID、转人工和 Agent 切换后，仍能从 MessageHub 历史、客户/线索事实和对话记忆解析“就按上次说的”“不是这个，是另一个”。

- **Given** 前文包含预算、行业、异议和纠正后的实体  
- **When** 用户后续使用指代或延续表达  
- **Then** 意图、销售回复、拟人和线索判断使用同一 `AgentConversationContext`，不回到旧实体，不读取其他会话。

### US2 — 群聊跨消息上下文（P1）

**Independent Test:** A 提产品、B 补预算、C 问交付；当前发言者、成员角色、真实 MessageID 和 audience 可见范围进入模型，回复综合三人信息但不复述成员私有内容。

- **Given** 同群多成员、多线程消息及部分成员后入群  
- **When** 当前成员触发 AI  
- **Then** 使用 `group_id` 作为会话根，按可见区间过滤历史；无关噪声不成为当前意图，新成员不能读取入群前私有消息。

### US3 — 会话、缓存与供应商能力（P1）

**Independent Test:** 同一总线 `LLMSessionID` 在 intent、sales、lead_detect、lead_judge、lead_react 等执行场景下拥有独立状态行；fake provider 验协议形状，真实 provider/sandbox 验 continuation、cached tokens 与冷热时延。

- **Given** provider/model/protocol/region 能力已登记  
- **When** 多次调用或 ReAct 循环发生  
- **Then** session reuse、prompt cache hit、answer cache reuse 分别计量；未知能力保守降级；fallback 重新预算。

### US4 — 全链路上下文接入（P1/P2）

**Independent Test:** 生产路径的意图、精细意图、情感/NLP、异议、拟人、销售 AgentLoop、线索 Detect/Judge/ReAct、Agent Runtime 均携带 `ContextSnapshotID` 与 canonical session key。

- **Given** 一条入站消息  
- **When** 任一客户会话 LLM 阶段执行  
- **Then** 只消费该场景最小必要视图；通用 geo/browser/trace 等调用不自动接收客户上下文。

### US5 — 隐私、并发与可观测硬门（P1）

**Independent Test:** 并发/乱序入站、迟到消息、工具超时、输出截断、群聊新成员、跨账号同客户、日志/trace 负向夹具全部通过。

- **Given** 两实例并发处理同一会话或工具已产生副作用但结果未落账  
- **When** native continuation 或 high-write 工具执行  
- **Then** 租约/fencing 防止分叉，unknown 副作用不盲重试；PII/私有备注/工具原文不进入普通日志或 trace。

## Functional Requirements

### Context and identity

- **FR-001:** 每条入站消息生成不可变 `AgentConversationContext`，SnapshotID 由 MessageID、as-of cursor、内容 hash、事实版本、策略版本和 schema 版本确定。
- **FR-002:** 范围必须包含 tenant、platform、channel account、conversation/group；跨 scope 读取 fail-closed。
- **FR-003:** 消息总线 `LLMSessionID` 经校验后直接作为 canonical continuation key；缺失时服务端按稳定 scope 确定性派生一次并回传。
- **FR-004:** 群聊根键优先 `group_id`，conversation/thread 仅作子维度；私聊无稳定 SenderID 时禁止用 SenderName 派生身份。
- **FR-005:** 新增独立 `ExecutionScenarioID`；原 `DispatchScenario` 仅用于路由/计费。至少覆盖 intent、fine_intent、emotion、objection、humanize_eval、humanize_rewrite、sales_generate、lead_detect、lead_judge、lead_react、agent_runtime、summary。
- **FR-006:** `ContextMessage` 保留真实 MessageID、Origin、CorrelationID、SenderID/Name、Role/Direction、SentAt、ReplyTo/Mentions、HistorySource 和 SourceMetadata。
- **FR-007:** 群聊消息带 Visibility/Audience 和成员可见区间；无法证明可见性时 fail-closed，并在 Provenance 标记 `visibility_unproven`。
- **FR-008:** MessageHub 为渠道事实源；UnifiedMessageReader 以 MessageHub 优先、SessionMessage 补充未落 hub 的 AI/人工出站，按 CorrelationID 去重；出站持久化 parity 必须先完成。

### User, lead and memory facts

- **FR-009:** Customer/Lead 上下文带 `SubjectType`（dm_person/group/group_member）与 `SubjectID`；群级和成员级事实分离。
- **FR-010:** 注入事实分为 confirmed、inferred、derived_summary、suggestion、external_untrusted、legacy_unverified；旧备注不得升级为 confirmed。
- **FR-011:** 所有客户/线索/记忆块带来源、时间、确认状态和 scope；模型建议不得冒充事实。
- **FR-012:** 长期跟进以渠道 conversation/group 为时间轴，可跨业务 SessionID；跨账号合并必须显式授权。

### Session, concurrency and tools

- **FR-013:** `llm_session_state` 按 canonical key + ExecutionScenarioID + provider/model/protocol/region/generation 隔离；保存 continuation ref、cursor、retention、prefix/schema version 和 CAS version。
- **FR-014:** canonical key 原值加密；唯一索引使用密钥化 HMAC blind index 或确定性加密，并支持 key rotation、TTL 清理和删除残留处理。
- **FR-015:** native continuation 使用全 scope 分布式租约/队列，覆盖 provider 调用、工具执行和提交，带 fencing token；失败 fail-closed 或显式降级无状态。
- **FR-016:** 逐入站消息建立 disposition 台账（pending/processing/replied/skipped/merged）；只有连续且确认送达的出站推进回复水位。
- **FR-017:** 迟到消息在 `LateReplyWindow` 内未回复则按 cursor 顺序补处理；已有更新回复则只合并进后续上下文。
- **FR-018:** tool invocation ledger 记录 planned/authorized/dispatched/unknown/confirmed；high-write 工具使用工具级幂等键，unknown 禁止盲重试并须对账，恢复时重校授权/受众/资源版本。
- **FR-019:** 默认 `store=false`/无状态重放；native session 仅 provider+model+protocol+region 白名单可启用，带 TTL、删除、租户开关和数据区域约束。

### Provider, budget and cache

- **FR-020:** Provider 能力按 provider+model+protocol+region 登记 ContextWindow、MaxInputTokens、ProtocolInputLimit、MaxOutputTokens、ReasoningReserveTokens、OutputSemantics、CacheMode、CacheMinTokens、CacheTTL、SessionMode、RouteMode、MaxRequestBytes、RequestTimeout 和幂等支持。
- **FR-021:** 有效输入预算为 `min(ContextWindow - 实际输出 - reasoning - 安全余量, MaxInputTokens, ProtocolInputLimit)`；输出单独夹取；本地估算不是供应商计量。
- **FR-022:** prompt 渲染 canonical，工具/schema/成员/上下文块/历史稳定排序；prefix hash 必须基于 adapter 完成重排、metadata 渲染和 cache control 后的实际 provider 请求。
- **FR-023:** session、prompt cache、answer cache 使用独立 key、状态、失效和指标；个性化多轮/工具路径不得命中最终答案缓存。
- **FR-024:** cached tokens 仍占上下文窗口；usage 按厂商口径记录 cache read/create/hit，不能统一按普通输入计量。
- **FR-025:** `Source=platform` 且无本地 Key 时强制显式代理；自带 Key 才可直连。代理契约版本化 endpoint/body/header/HMAC，支持幂等重放和 cached usage 对账。
- **FR-026:** 代理缓存折扣默认不透传；计价契约完成前，代理路径不得宣称缓存成本收益。

### Integration, output and privacy

- **FR-027:** 意图/精细意图规则与 embedding 快路径也消费轻量 ContextView；LLM 兜底必须上下文感知。
- **FR-028:** 情感/NLP/异议结合近期轨迹；低置信或冲突升级上下文 LLM。
- **FR-029:** SalesRequest 保留 ConversationID、IsGroup、GroupID、SenderID、MessageID、Direction、OriginalSentAt 和 ContextView；历史改走 UnifiedMessageReader。
- **FR-030:** 拟人评估/重生、线索 Detect/Judge/ReAct、Agent Runtime 使用对应 ExecutionScenarioID 和同一快照；证据 ID 必须真实存在。
- **FR-031:** 增加 checked output API，验证 FinishReason、JSON/Schema、证据 ID 和输出长度；截断或校验失败显式重试/降级。
- **FR-032:** 输出预算初始值：结构化判断/拟人/摘要 1024，线索 Judge/ReAct 2048，销售短回复 512–2048，销售长回复 4096–8192；受 provider 能力夹取。
- **FR-033:** 群聊发送前执行确定性隐私守卫；成员级画像/线索/私人备注默认不进模型输入。
- **FR-034:** 客户会话 shared notes、工具 args/output、AI 输入输出进入普通日志/trace 前默认 hash/脱敏；完整内容仅进入加密限权 ledger。
- **FR-035:** 媒体只使用受控摘要或授权代理，禁止任意 URL 抓取。
- **FR-036:** 历史 edit/delete/recall 产生 tombstone，并使相关 native generation 和摘要失效；cursor 语义在 data-model 固定。

### Migration and verification

- **FR-037:** `ConversationContext` 增加版本/CAS；停止向 Messages JSON 追加原始历史，legacy 只读兼容，读者迁移后再退役 JSON。
- **FR-038:** 新配置同步 `.env.example` 与 `docs/operations/PROD_TOPOLOGY_CHANGES.md`；跨仓契约文档同步。
- **FR-039:** fake provider 只验协议形状；真实 provider/sandbox 验 cold/hot、cached tokens、p50/p95；DB skip 不计通过。
- **FR-040:** `go build`、`go vet`、gofmt、受影响包真库测试和双场景黄金夹具全部通过；Converged 前不宣称完成。

## Success Criteria

- **SC-001:** 单聊 S1–S8 全部通过，包括跨 Session 指代、纠正、用户/线索、session、缓存、时延、来源、隔离和缓存分层。
- **SC-002:** 群聊 G1–G8 全部通过，包括多人跨消息、发言者归属、噪声、来源、隐私、session、缓存/时延和输入输出分层。
- **SC-003:** 新成员入群前隐私消息不会进入模型输入；跨租户、跨账号、跨群负向断言全部通过。
- **SC-004:** 同一 canonical key 下不同 ExecutionScenarioID 的 continuation/transcript 不互相覆盖；并发/乱序/重试不产生分叉或重复副作用。
- **SC-005:** 固定夹具重复渲染的实际 provider 请求 prefix hash 完全一致；支持缓存的真实 provider 出现 cached tokens，不支持时明确 unsupported。
- **SC-006:** 超过旧 8192 输入/4096 输出上限的长夹具无静默截断；质量、时延、成本不劣于基线才扩大灰度。
- **SC-007:** 普通日志/trace 不含 PII、私有备注或工具原文；完整内容仅存在于加密限权 ledger。
- **SC-008:** 平台无会话正文/画像/线索持久化；代理缓存计价与 usage 对账口径明确。

## Key Entities

- `AgentConversationContext`、`ConversationScope`、`ContextMessage`、`ContextProvenance`
- `CustomerContext`、`LeadContext`、`MemorySummary`、`BusinessFact`、`Participant`
- `LLMConversationRef`、`LLMSessionRef`、`ProviderContinuationRef`
- `LLMSessionState`、`ConversationMessageDisposition`、`ScenarioTranscript`、`ToolInvocation`
- `ProviderCapability`、`ContextBudget`、`OutputBudget`、`PromptPrefix`
- `UnifiedMessage`、`SourcePolicy`、`PrivacyGuardResult`

## Assumptions and Confirmed Decisions

- 默认直连；`Source=platform` 无本地 Key 时例外强制代理。
- 总线 `LLMSessionID` 直接作为 canonical continuation key。
- 默认无状态；native session 仅白名单。
- 迟到消息未回复则补、已回复则合并。
- 群聊采用群级摘要 + 可证明可见的相关线程/参与者原始消息。
- 代理缓存折扣默认不透传。

## Out of Scope

- 不把平台代理变成用户会话存储。
- 不无上限读取全群或全量重放所有历史。
- 不把个性化多轮回复送入最终答案缓存。
- 不修改与客户会话无关的通用 LLM 场景，除非其复用被修改的底层接口。
