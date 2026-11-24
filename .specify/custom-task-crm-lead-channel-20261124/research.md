# Research: 四大部分定制任务关键决策

**Date**: 2026-11-24 | **Status**: 已完成（基于用户决策 + 代码基线调研）

## R1 — 渠道唯一识别码一次性替换 bartoken

- **Decision**: 新增每渠道唯一的 `channel_identify_code`（支持重置），一次性替换 bartoken；所有上报/桥接/hook/SageX 改用识别码。采用三步原子切换：① 双写识别码与 bartoken；② 发布切换读路径到识别码（旧 bartoken 不再被接受）；③ 后续版本移除 bartoken 写与字段。无长期兼容期。
- **Rationale**: 用户明确选择“一次性替换”。在途消息窗口需处理，故用三步原子切换而非保留双轨兼容。
- **Alternatives**: 长期双轨兼容（被否——用户要求一次性）；随机 UUID 无语义（否——需可读可重置）。

## R2 — 三条消息路径合并

- **Decision**: 统一抽象 `InboundMessage` 收件箱模型 + `ChannelResolver`（按识别码解析租户/账号）。bridge HTTP、统一 API 上报、hook 接收三条入口保留为适配器，仅做归一化投递；账号识别、自他识别、线索触发、自动回复触发全部收口到统一服务。
- **Rationale**: 用户要求“检查当前模块，准备合并，不要分散”。现状 `bridge/handler_http.go`、`channelgw`、`channelbot`、webhook 多通道各自处理识别，易漂移。
- **Alternatives**: 维持各自实现（否——与用户诉求相反）；仅前端合并（否——根因在后端识别分散）。

## R3 — CRM 建设方式

- **Decision**: 扩展现有 Customer/OneID/Clue，补齐 Company/Contact/Supplier/ServiceProvider 应用层；来源字段统一建模；不另起系统。
- **Rationale**: 用户明确选择“扩展现有+补缺失实体”；外层 `定制任务清单.md` 结论同为“融合进现有项目、复用存量”。
- **Alternatives**: 新建独立 CRM 系统（否——用户否决，且违反复用存量结论）。

## R4 — 线索三路检测与二次 LLM

- **Decision**: 关键词检测 + 语义关键词检测同步执行，LLM 检测异步；三路结果落 `lead_mining_hits`（独立证据），再由 `dispatcherJudge` 做二次 LLM 汇总判断，上下文取 ConversationContext 的多轮消息 + 历史消息。防抖去重复用现有 debounce 机制。
- **Rationale**: 用户第二部分明确“三方式并行 + 拿到结果后再调用一次 LLM 总结判断 + 考虑多轮与历史消息”。
- **Alternatives**: 仅关键词（否——漏语义）；先串行再判断（否——用户要求并行）。

## R5 — 智能体解耦与共享

- **Decision**: 自动回复智能体与线索发现智能体拆为独立模块，可独立启停；两者共享 ConversationContext 与用户数据；通过标签/备注/摘要做交叉引用，而非模块间直调。
- **Rationale**: 用户第三部分要求“剥离但数据透明、允许交叉引用”。“线索发现”改名为“线索发现智能体”。
- **Alternatives**: 保持单一 ingest 链路（否——互相干扰）；完全隔离无共享（否——用户要求交叉引用）。

## R6 — 权限设计

- **Decision**: 原 RBAC 模块不动；在渠道账号管理完成账号数据授权，新增“账号负责人 / 可见账号范围 / 可见行业范围”数据权限维度，所有列表接口强制范围过滤。
- **Rationale**: 用户 4.2.3 明确“原 RBAC 模块不动，在渠道账号管理完成渠道账号数据授权”。
- **Alternatives**: 改 RBAC（否——用户明确禁止）；无数据权限（否——无法满足按账号/行业隔离）。

## R7 — 基线勘察校准（2026-11-24，只读勘察结论，实施前必读）

主智能体派发只读勘察子智能体核实的真实基线，以下结论**覆盖** spec/plan/tasks 中任何与之矛盾的早期假设：

1. **迁移路径与编号**：迁移在**仓库顶层 `migrations/`**（非 `user-server/migrations/`）；当前最大 **087**，088~095 空闲。需 Go 注册的迁移同步在 `user-server/internal/migration/migrations/` 新增注册文件（参照 `v3_79_0_tenant_leading_index_migration.go`）。
2. **渠道账号表**：不存在 `custom_channel_accounts`；真实表为 `custom_accounts`（迁移 075、`model/im_platforms.go:117`）。已有表包括 wecom_cs_accounts / bridge_accounts / feishu_accounts / whatsapp_* / qq_accounts / telegram_accounts / channel_agent_bindings（`model/ai_agent.go:188`）。统一渠道主数据层为**新建**，但租户反查须对齐真实表。
3. **租户解析**：`ResolveTenantByChannelAccount` 在 `service/webhook.go:463`，按「渠道账号表+id」自证、歧义拒绝、失败返回 404 不回落默认租户——改造须保持 fail-closed。
4. **companies 不是空白**：已有迁移 053 + 079 演进 + `model/company.go` + `service/company.go` + `CustomerCompanyRelation`。规划为**扩展既有公司域**，禁止重复新建。
5. **Contact/Supplier/ServiceProvider 完全新建**：无历史 model/表/迁移可复用，需从零建五层。来源字段**已建模**（迁移 080 + `model/customer.go:66-67` + `model/clue.go:38-39` + companies 079），禁止重复建模，仅补缺口。
6. **lead_mining_configs 是每租户一行**（主键 = tenant_id，`repository/lead_mining_config.go:30-43`），不是全局 id=1 单例；实现成 id=1 会导致第二租户保存撞主键 500。
7. **Bridge token 不是单点替换**：全仓无 `bartoken` 字面量，实际是 Bridge 通道凭证 `X-Bridge-Token` / `?bridge_token=`。一次性替换须同步：①`middleware/bridge_ingress_guard.go` 候选源；②`service/bridge_token.go` 的 KV key；③扩展端 5+ 发送点（`user-web/bridge/src/core/http-ingest.js`、`sse-fetch-client.js`、`selector-health.js`、`background/http-relay.js`、`background/sse-client.js`、`popup/index.js`）；④CORS 允许头（`router.go:167`）；⑤脱敏清单（`api_logger.go:28`、`tracing/observation.go:170`）；⑥`docs/architecture/BRIDGE_TOKEN_PROTOCOL.md` 与测试（`mcp_guard_test.go`、`cors_bridge_preflight_test.go`、`audit_redact_test.go`）。否则「Go 编译通过但浏览器 401/预检失败」静默故障。
8. **DI 与入口**：入口 `user-server/cmd/api/main.go`；新增 service 沿用 `router.Setup` 内 setter 注入模式（如 `SetLeadMining`），不得写内联闭包；原 RBAC 守卫 `RoutePolicyGuard`/`TenantMiddleware` 挂载顺序不可动（fail-closed）。
9. **编译基线干净**：`cd user-server && go build ./...` 通过；测试须显式 `export POSTGRES_TEST_PASSWORD`，否则 `t.Skipf` 假绿。

---

## R8 — Phase 5 / US3 模块边界定义（T046，2026-11-24 只读勘察 + 边界固化）

> 本节是 T046 的边界说明附录。**不改任何业务契约、不改既有 API/表语义**，仅把 ingest 之后
> 「自动回复」与「线索发现」两条交织链路的归属、接口与开关口径写清楚，作为 T047–T054 的施工边界。

### R8.1 路径纠偏（tasks.md 与真实代码不符，以实际代码为准）

| tasks.md 写的路径 | 实际代码位置 | 说明 |
|---|---|---|
| `service/aiagent/` | `user-server/internal/aiagent/`（独立顶层包，不在 service 下） | 智能体运行时（runtime / tooluse / llm / knowledge / rag …） |
| `service/intent_recognition/`、`service/humanize/`（作为「包」） | 并非独立包：意图识别散落 `service/intent_recognition.go`、`intent_recognition_fine.go`；拟人工在 `service/humanize_polisher.go`、`behavioral_humanize.go` 与 `service/humanize/{ai,behavioral,service,rule_scorer,llm_scorer}` 子包 | 自动回复能力的构件，非独立业务模块 |
| `service/lead_mining_agent_service.go` / `..._controller.go` | 新建裸名 `service/lead_mining_agent.go`、`controller/lead_mining_agent.go` | 架构门禁 §6 禁止 `_service.go`/`_controller.go` 冗余后缀 |
| `service/auto_reply_service.go`、`service/lead_discovery_service.go`、`service/conversation_context_service.go` | 分别建裸名 `service/auto_reply.go`、`service/lead_discovery.go`、`service/conversation_context.go` | 同上门禁 |
| `views/agents/`、`views/channels/account-manage.vue` | 实际智能体管理在 `user-web/src/views/aiAgent/`；`views/channels/` 尚不存在（Phase 6 T062 才建渠道管理页） | T053/T054 在 `views/aiAgent/` 去重绑定逻辑，并新建 `views/channels/account-manage.vue` 静态骨架 |

### R8.2 ingest 后交织链路的真实走向（基线）

1. 三路入口（bridge `handler_http.go` / 统一 API `channelgw/pipeline.go` / webhook）→ `InboundMessage` →
   `InboxIngressService.HandleIngressMessage`（`service/inbox_ingress.go`）。
2. 消息落库在 `service/inbox_ingress_persist.go:persistMessage`：写 `message_hub` + 同步 `inbox_conversations`。
3. **落库后的 defer 同时挂了两条腿**（这就是「交织」根因）：
   - `service/inbox_ingress_persist.go:154` 无条件 `s.leadMiningSvc.Enqueue(hub)` → 线索发现腿；
   - `service/inbox_ingress.go:765 triggerAIForEvent` → `s.aiTrigger.TriggerInboundAI(...)`（生产装配为 `webhookSvc`）→ 自动回复腿。
4. 两腿都读 `MessageHub` 历史（`lead_mining.go:fetchHistoryDB` / 智能体 runtime context），但**此前没有共享的可写交叉引用载体**，
   彼此不知道对方写了什么备注/摘要。

### R8.3 模块边界（T047–T054 的施工边界）

**A. 自动回复智能体模块（Auto Reply Agent）**
- 职责（FR-018）：意图识别 / NLP / 情感 / 拟人工润色 / 下发·返回 / 转人工。
- 归属文件：`internal/aiagent/**`（runtime/tooluse/llm/rag/knowledge）、`service/intent_recognition*.go`、
  `service/humanize*`、`service/humanize/**`、`service/smart_cs_orchestrator.go`、`service/webhook_ai.go`、
  `service/inbox_ingress.go:triggerAIForEvent`。
- 新模块入口：`service/auto_reply.go`（`AutoReplyService`）——唯一对外闸门：`Enabled(ctx, platform, accountID)`。
- 启停键：`channel_accounts.auto_reply_agent_id`（非空=启用指定智能体；空=关闭）。
- 交叉引用写入：转人工/情感等事件 → 通过 `ConversationContextService` 写 `notes`/`tags`（命名空间 `auto_reply:`）。

**B. 线索发现智能体模块（Lead Mining / Discovery Agent）**
- 职责（FR-019）：只做商机识别 / 类型判定 / 用户与线索归档 / 通知负责人，**不触发任何自动回复**。
- 归属文件：`service/lead_mining.go`（worker/dispatcherJudge）、`service/lead_detect*.go`、
  `service/lead_detector_*.go`、`service/lead_judge_context.go`、`service/lead_debounce.go`、
  `service/lead_clue_archive.go`、`service/lead_notify*.go`、`service/lead_evidence.go`。
- 新模块入口：`service/lead_discovery.go`（`LeadDiscoveryService`）——唯一对外闸门：`Enabled` + `Process`（仅在启用时委托既有 `Service.Enqueue`）。
- 独立注册（FR-026）：`service/lead_mining_agent.go` + `controller/lead_mining_agent.go`，复用 `ai_agents` 表 /
  `AIAgentRepository.List(ctx, "lead_mining", …)`，新增 `AgentTypeLeadMining` 子类型；不新建表。
- 启停键：`channel_accounts.lead_mining_agent_id`。**该腿的整条链路里没有 AITrigger 调用，天然不触发自动回复**；
  T049 用显式注释 + 测试契约固化「不回调 auto_reply」。

**C. 共享层（Shared Context）**
- 载体：`model/conversation_context.go`（T033 已建，含 `Messages` / `MemberRoles` / `Tags` / `Summary`），
  仓储 `repository/conversation_context.go`（已建，`Upsert` 按 tenant+account+conv 原子写）。
- 新服务：`service/conversation_context.go`（`ConversationContextService`）——两模块**唯一**共享读写口，
  提供追加消息、写/读 tags、notes、summary、群成员角色聚合。
- 模块间约定：**只经共享层间接引用，禁止 A 直接调 B 的 Service**（R5 决策）。

### R8.4 开关解析口径（T048/T049 共用，保持基线兼容）

- 由消息的 `account_id`（平台账号标识／识别码）经 `ChannelAccountRepository.GetByIdentifyCode` 反查 `channel_accounts`。
- **已绑定**渠道账号主数据：按对应 `*_agent_id` 判启停（nil=关闭该模块）。
- **未绑定**（legacy 账号／仓储不可用／测试无 DB）：视作「未纳入新配置」，维持既有行为（两模块仍按旧链运行），
  避免新表为空时把现网线索/自动回复全部关掉。此兼容口径只针对「无法定位主数据行」，定位到行后严格按字段启停（fail-visible）。
- 写入/解析失败不静默吞错：记 warn 日志，按上述口径放行/拒绝，便于从日志核对。

