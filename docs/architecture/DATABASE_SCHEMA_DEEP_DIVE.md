# HiveMtk 数据库 Schema 深度解析

> **版本**：v1.18（2026-09-21，T-P6-01 **报价域两张表交付**：新增 §4.21 —— 一行 = 一个版本的 `quotes`（10 列 4 索引，`uq_quotes_quote_version` 是 AC① 的唯一硬保证，且**刻意没有**单列 `quote_id` 唯一索引）与 `(quote_row_id, line_no)` 复合主键、无代理键的 `quote_line_items`（"旧版不可变"因此是形状的结果而不是规矩），表头上合计/审批列/行内币种/租户列四样一律不建的理由，版本链的三条写路径与八个并发写者恰好一个赢的实测；上一版 v1.17 是 T-P5-04 的交付）
> **范围**：user-server + platform-server 所有数据表
> **数据库**：PostgreSQL 15 + pgvector
> **单租户**：私域部署无 `merchant_id` 字段

---

## 一、命名与基类规范

### 1.1 GORM 基类

所有业务表统一嵌入 `BaseModel`：

```go
// hivemtk/user-server/internal/model/base.go
type BaseModel struct {
    ID        uint           `gorm:"primaryKey" json:"id"`
    CreatedAt time.Time      `gorm:"index" json:"created_at"`
    UpdatedAt time.Time      `json:"updated_at"`
    DeletedAt gorm.DeletedAt `gorm:"index" json:"-"`
}
```

### 1.2 字段命名

| 类型 | 命名规则 | 示例 |
|------|----------|------|
| 主键 | `id` | `id BIGSERIAL` |
| 时间戳 | `xxx_at` | `created_at`, `published_at` |
| 布尔 | `is_xxx` / `has_xxx` / `can_xxx` | `is_active`, `has_2fa` |
| 外键 | `xxx_id` | `customer_id`, `agent_id` |
| 枚举 | `xxx_type` / `xxx_status` | `intent_type`, `order_status` |
| 关联表 | 复数 | `customers`, `orders` |

### 1.3 软删除

所有业务表使用 `gorm.DeletedAt`（软删除），**禁止硬删除**（除审计日志）。

---

## 二、ER 总览图

> 以下 ER 图覆盖 **34 张核心业务表**、**62 条关系线**。完整表清单见附录。
> 关系线标注的外键字段均经过 `model/` 目录 grep 确认,无臆造关系。

```mermaid
erDiagram

    %% ========== 用户/权限域 ==========
    USERS {
        uint id PK
        string username
        string password_hash
        string email
        string phone
        string status
        datetime created_at
        datetime updated_at
    }
    ROLES {
        uint id PK
        string code
        string name
        jsonb permissions
        datetime created_at
        datetime updated_at
    }
    USER_ROLES {
        uint user_id FK
        uint role_id FK
    }
    USER_MFA {
        uint id PK
        uint user_id FK
        string type
        string secret
        bool enabled
    }
    PASSWORD_RESET_TOKENS {
        uint id PK
        string user_id FK
        string token
        datetime expires_at
    }
    LOGIN_EVENTS {
        uint id PK
        uint user_id FK
        string ip
        string user_agent
        bool success
        datetime created_at
    }

    %% ========== 客户域 ==========
    CUSTOMERS {
        uint id PK
        string phone
        string name
        string source
        string rfm_segment
        datetime created_at
        datetime updated_at
    }
    CUSTOMER_EVENTS {
        uint id PK
        string customer_id FK
        string event_type
        jsonb payload
        datetime created_at
    }
    CUSTOMER_SESSIONS {
        uint id PK
        string session_id UK
        string customer_id FK
        string channel_id FK
        string agent_id
        datetime created_at
        datetime updated_at
    }
    CUSTOMER_RFM {
        uint id PK
        string customer_id FK
        int recency
        int frequency
        float monetary
        string segment
    }
    CUSTOMER_DO_NOT_CONTACT {
        uint id PK
        string customer_id FK
        string reason
        datetime expires_at
        datetime created_at
    }
    CUSTOMER_LONG_TERM_MEMORY {
        uint id PK
        string customer_id FK
        string memory_type
        text content
        float importance_score
        datetime created_at
    }
    CUSTOMER_TAG_ASSIGNMENTS {
        uint id PK
        string customer_id FK
        string tag
        string assigned_by
        datetime created_at
    }

    %% ========== 消息域 ==========
    MESSAGES {
        uint id PK
        string message_id UK
        string platform
        string direction
        string status
        string agent_id
        datetime created_at
    }
    SESSION_MESSAGES {
        uint id PK
        string session_id FK
        string customer_id FK
        string role
        string content
        datetime created_at
    }
    UNIFIED_MESSAGES {
        uint id PK
        string message_id UK
        string platform
        string customer_id FK
        string agent_id
        string direction
        datetime created_at
    }
    MESSAGE_EVENTS {
        uint id PK
        string event_id UK
        string session_id FK
        string message_id FK
        string event_type
        jsonb payload
        datetime created_at
    }
    MESSAGE_TRACES {
        uint id PK
        string trace_id UK
        string message_id FK
        string agent_id
        string stage
        int latency_ms
        datetime created_at
    }
    CHAT_CHANNELS {
        uint id PK
        string channel_id UK
        string platform
        string account_id
        string enabled
        datetime created_at
        datetime updated_at
    }

    %% ========== 知识库域 ==========
    KNOWLEDGE_BASES {
        uint id PK
        string kb_code
        string type
        string owner_type
        uint owner_agent_id FK
        datetime created_at
        datetime updated_at
    }
    KB_DOCUMENTS {
        uint id PK
        uint kb_id FK
        string doc_type
        string title
        string status
        datetime created_at
        datetime updated_at
    }
    AGENTS {
        uint id PK
        string code
        string name
        string agent_type
        string status
        datetime created_at
        datetime updated_at
    }
    AGENT_STATUS {
        uint agent_id PK_FK
        string state
        int total_conversations
        float avg_confidence
        datetime last_active_at
    }
    AGENT_KB_BINDINGS {
        uint id PK
        uint agent_id FK
        uint kb_id FK
        string role
        int priority
    }

    %% ========== SOP / 工作流域 ==========
    SOP_TEMPLATES {
        uint id PK
        string name
        jsonb steps
        string trigger_keywords
        uint agent_id FK
        datetime created_at
        datetime updated_at
    }
    SOP_EXECUTORS {
        uint id PK
        string session_id FK
        uint sop_template_id FK
        string current_step
        string status
        datetime created_at
        datetime updated_at
    }
    WORKFLOW_ORCHESTRATORS {
        uint id PK
        string workflow_id FK
        string execution_id
        string session_id
        string status
        datetime created_at
        datetime updated_at
    }

    %% ========== 线索/运营域 ==========
    CLUES {
        uint id PK
        string message_id FK
        string customer_id FK
        string intent
        float score
        string status
        datetime created_at
    }
    INTENT_LOGS {
        uint id PK
        string customer_id FK
        string session_id FK
        string intent
        float confidence
        string model
        datetime created_at
    }
    FAQ_ENTRIES {
        uint id PK
        string question
        string answer
        string category
        uint agent_id FK
        datetime created_at
        datetime updated_at
    }

    %% ========== LLM 域 ==========
    LLM_PROVIDERS {
        uint id PK
        string name
        string provider_type
        string base_url
        bool enabled
        datetime created_at
        datetime updated_at
    }
    LLM_ROUTING_RULES {
        uint id PK
        string scenario
        uint provider_id FK
        float weight
        bool canary_route
        datetime created_at
        datetime updated_at
    }

    %% ========== 系统域 ==========
    OPERATION_LOGS {
        uint id PK
        uint user_id FK
        string action
        string resource
        jsonb payload
        string ip
        datetime created_at
    }
    SECURITY_AUDITS {
        uint id PK
        uint user_id FK
        string action
        string target
        string severity
        string ip
        datetime created_at
    }
    SYSTEM_CONFIGS {
        uint id PK
        string config_key
        jsonb config_value
        string description
        datetime updated_at
    }

    %% ========== 关系线 ==========
    USERS ||--o{ USER_ROLES : "has"
    ROLES ||--o{ USER_ROLES : "bound_to"
    USERS ||--|| USER_MFA : "has"
    USERS ||--o{ PASSWORD_RESET_TOKENS : "issues"
    USERS ||--o{ LOGIN_EVENTS : "triggers"
    USERS ||--o{ OPERATION_LOGS : "writes"
    USERS ||--o{ SECURITY_AUDITS : "generates"

    CUSTOMERS ||--o{ CUSTOMER_SESSIONS : "has"
    CUSTOMERS ||--o{ CUSTOMER_EVENTS : "triggers"
    CUSTOMERS ||--|| CUSTOMER_RFM : "scored_by"
    CUSTOMERS ||--o{ CUSTOMER_DO_NOT_CONTACT : "blacklisted_in"
    CUSTOMERS ||--o{ CUSTOMER_LONG_TERM_MEMORY : "remembers"
    CUSTOMERS ||--o{ CUSTOMER_TAG_ASSIGNMENTS : "tagged_by"
    CUSTOMERS ||--o{ CLUES : "source_of"
    CUSTOMERS ||--o{ INTENT_LOGS : "generates"

    CUSTOMER_SESSIONS ||--o{ SESSION_MESSAGES : "contains"
    CUSTOMER_SESSIONS }o--|| CHAT_CHANNELS : "via"
    CUSTOMER_SESSIONS ||--o{ SOP_EXECUTORS : "executes"

    MESSAGES ||--o{ MESSAGE_EVENTS : "triggers"
    MESSAGES ||--o{ MESSAGE_TRACES : "instrumented_by"
    MESSAGES ||--o{ CLUES : "produces"

    CHAT_CHANNELS ||--o{ CUSTOMER_SESSIONS : "serves"
    UNIFIED_MESSAGES }o--|| CUSTOMERS : "addresses"
    UNIFIED_MESSAGES }o--|| AGENTS : "handled_by"

    KNOWLEDGE_BASES ||--o{ KB_DOCUMENTS : "contains"
    KNOWLEDGE_BASES ||--o{ AGENT_KB_BINDINGS : "bound_by"
    KNOWLEDGE_BASES }o--o| AGENTS : "owned_by"

    AGENTS ||--o| AGENT_STATUS : "has"
    AGENTS ||--o{ AGENT_KB_BINDINGS : "binds"
    AGENTS ||--o{ SOP_TEMPLATES : "creates"
    AGENTS ||--o{ FAQ_ENTRIES : "owns"

    SOP_TEMPLATES ||--o{ SOP_EXECUTORS : "executed_by"
    WORKFLOW_ORCHESTRATORS }o--o| AGENTS : "runs_for"

    LLM_PROVIDERS ||--o{ LLM_ROUTING_RULES : "routed_by"
    LLM_ROUTING_RULES }o--o| AGENTS : "used_by"
```

### 2.1 关系说明

| 关系类型 | 含义 | ER 符号 |
|---------|------|---------|
| 强关联 | 外键 NOT NULL,删除时级联/受限 | `||--o{` / `}o--||` |
| 弱关联 | 外键可空,删除时置 NULL | `||--o{` (FK nullable 在 Mermaid 中体现为 `}o--||`) |
| 一对一 | 一条记录对应唯一子表记录 | `||--||` / `||--o|` |

---

## 三、单租户与无 merchant_id

### 3.1 设计基线

- **私域部署**：每个企业独立部署一套完整系统
- **单租户**：无 `merchant_id` 字段（本文件 §3.2 列出彻底移除 `merchant_id` 的历史迁移；旧引用 ADR-003 已作废且编号不复用）
- **物理隔离**：数据通过独立的 PostgreSQL 实例物理隔离

### 3.2 历史迁移记录

> 注：项目早期版本曾使用 `merchant_id` 实现多租户，已通过以下迁移彻底移除：

| 迁移 | 变更 |
|------|------|
| `unmultitenant_migration.go` | 删除 `merchants` 表，从所有业务表移除 `merchant_id` 列 |
| `merchant_id_nullable_migration.go` | 兼容场景下允许 `merchant_id` 为 NULL（保留为可选字段） |
| `wecom_webhook_fields_migration.go` | 显式标注"独立部署：单租户，无 merchant_id" |

---

## 四、核心业务表

### 4.1 用户与认证

| 表 | 说明 | 关键字段 |
|---|---|---|
| `users` | 系统用户 | id, username, password_hash, email, phone, status |
| `user_mfa` | MFA 配置 | user_id, type (totp/sms), secret, enabled |
| `user_roles` | 角色绑定 | user_id, role_id |
| `roles` | 角色定义 | code, name, permissions (jsonb) |
| `password_history` | 密码历史 | user_id, password_hash, created_at |
| `login_events` | 登录审计 | user_id, ip, user_agent, success |

### 4.2 客户与对话

| 表 | 说明 | 关键字段 |
|---|---|---|
| `customers` | 客户主表 | id, phone, name, source, tags, rfm_segment |
| `customer_events` | 客户行为事件 | customer_id, event_type, payload, created_at |
| `customer_rfm` | RFM 分层 | customer_id, recency, frequency, monetary, segment |
| `clue_scores` | 线索评分 | customer_id, score, intent, confidence |
| `customer_oneid` | 客户身份合并 | customer_id, source, external_id, mapping |

### 4.3 消息中心

| 表 | 说明 | 关键字段 |
|---|---|---|
| `message_hub` | 消息统一表 | platform (enum), direction, content, status |
| `message_hub_inbox` | 收件箱 | customer_id, platform, last_message_at |
| `message_queue` | 发送队列 | platform, payload, status, retry_count |
| `chat_channel` | 渠道配置 | platform, account_id, app_key, enabled |

### 4.4 知识库与 RAG

| 表 | 说明 | 关键字段 |
|---|---|---|
| `knowledge_bases` | 知识库元数据 | kb_code, type, owner_type, owner_agent_id |
| `agent_kb_bindings` | 智能体绑定 | agent_id, kb_id, role (primary/reference), priority |
| `knowledge_documents` | 文档 | kb_id, doc_type, title, content, source_uri |
| `knowledge_chunks` | 文档切片 | doc_id, chunk_index, content, embedding (vector 1024) |
| `aliases` | 知识库别名 | kb_id, alias, type |
| `glossary` | 术语表 | source_term, target_term, lang, domain |

### 4.5 FAQ / SOP

| 表 | 说明 | 关键字段 |
|---|---|---|
| `faq_entries` | FAQ 条目 | question, answer, category, embedding |
| `sop_templates` | SOP 模板 | name, steps (jsonb), trigger_keywords |
| `integration_templates` | 集成模板 | platform, template_id, params |

### 4.6 AI 智能体

| 表 | 说明 | 关键字段 |
|---|---|---|
| `ai_agents` | 智能体 | code, name, agent_type, persona, knowledge_kb_ids |
| `agent_kb_binding` | 智能体↔知识库 | agent_id, kb_id, role, priority |
| `llm_routing_rules` | LLM 路由 | scenario, provider, weight, canary_route |
| `llm_routing_log` | 路由决策日志 | scenario, provider, model, latency_ms |
| `layer_decision_logs` | 分层决策日志 | layer, decision, reasoning, wall_ms |
| `humanize_scores` | 拟人度评分 | response_id, score, source, model |
| `champion_baselines` | 销冠基线 | agent_id, baseline_json, sample_count |

### 4.7 触达与营销

| 表 | 说明 | 关键字段 |
|---|---|---|
| `reach_pipelines` | 触达管线 | name, trigger_type, steps (jsonb), enabled |
| `reach_pipeline_steps` | 管线步骤 | pipeline_id, step_type, config, order |
| `notifications` | 通知 | channel, recipient, template_id, status |
| `asset_bundles` | 资产包 | name, type, version, manifest |

### 4.8 翻译

| 表 | 说明 | 关键字段 |
|---|---|---|
| `translation_jobs` | 翻译任务 | source_text, source_lang, target_lang, status |
| `translation_results` | 翻译结果 | job_id, translated_text, model, confidence |

### 4.9 系统与运维

| 表 | 说明 | 关键字段 |
|---|---|---|
| `audit_logs` | 审计日志 | actor, action, resource, payload, ip, ts |
| `security_alerts` | 安全告警 | severity, source, description, resolved_at |
| `system_stats` | 系统统计 | metric, value, ts |
| `feature_flags` | 功能开关 | flag_key, enabled, rollout_percent |
| `webhook_trace` | Webhook 追踪 | trace_id, source, payload, status |
| `sms_number_portability_logs` | 携号转网日志 | phone, original_carrier, new_carrier |

### 4.10 短链与活码

| 表 | 说明 | 关键字段 |
|---|---|---|
| `live_codes` | 活码 | code, target_url, type, rotation_strategy |
| `live_code_stats` | 活码统计 | live_code_id, scan_count, last_scan_at |


### 4.11 统计看板：真实源与演示表（勿混用）

看板的数字有两个来源，长得像但不是一回事。`conversion_funnels` 是 R-4 点名的**僵尸表**，
本文件把它写清楚，避免下一个人在 BI 里查到一套产品里不存在的漏斗。

| 指标 | 真实源（线上读数走这条） | 演示表（只有 `cmd/seed` 在写） |
|---|---|---|
| 转化漏斗 | `internal/ops/service/conversion_funnel.go` `BuildFunnel` 对 `customer_events` / `clues` / `intent_records` / `customer_sessions` / `opportunities` 的**实时聚合** | `conversion_funnels`（**全仓无读路径**） |

两套阶段名**刻意不同**，这是判定"表是死的"的关键证据：

| 侧 | 阶段名 |
|---|---|
| 真实源（词表在 `internal/ops/repository.FunnelStageKey`，代码里是唯一真源） | `visit` 访问 → `clue` 线索 → `intent` 意向 → `session` 会话 → `opportunity` 商机（末位自 T-P4-06） |
| 演示表（`cmd/seed/seed_stats.go` 自造） | `exposure` → `click` → `consult` → `add_wecom` → `deal` |

两套名字**到今天仍然互不相交**，这一点在 T-P4-06 之后更需要明说：真实源新加的键叫
`opportunity`、演示表最后一段叫 `deal`，中文都近似"商机/成交"，看中文会以为两套并起来了。
`_DemoTableStaysOutOfVocabulary` 守的就是这条（它把 `deal` 连同 `exposure/click/consult/add_wecom`
一起列成"不得进词表"，所以"判定表是死的"那把证据没因为接了商机段而失效）。

规则（写进 `model.ConversionFunnel` 的注释，同时由 `conversion_funnel_stage_test.go` 的
`_DemoTableStaysOutOfVocabulary` 守着）：

- **新域不得往 `conversion_funnels` 写数据**。报价/商机漏斗（T-P4、T-P7-02）的阶段维度一律
  以 `FunnelStageKey` 为准。`opportunity`（商机）在 T-P2-03 是"**定名而不产出**"的预留位，
  T-P4-06 已把它接成第五段（读 `opportunities`，见 §4.16.5）；预留机制本身留着
  （`reservedFunnelStages` 现为空），因为下次再出现"先定名、后接待"的阶段仍该登记在那里，
  而不是在服务层凭空多画一段。
- 演示行自带 `extra.demo_only = true` 与 `extra.real_source` 指针，供临时 SQL/BI 的使用者辨真伪。

**为什么不删这张表（"未证伪不删"）**：它仍在 `internal/pkg/db/migrate.go allModels()` 的建表清单里，
生产库可能已有历史行；而"没有读取方"只在**本仓**成立，仓外的 BI 脚本/定时报表看不到。
三条同时成立时另立卡删除：① 仓外读取方确认为零；② 表内行只来自 seed；③ 已决定归档方式。

**顺带登记的两处真实源口径问题**（不改，属"改变现网读数"，见短板 G16）：
`BuildFunnel` 把**前四个** count 的错误全部吞掉 ⇒ 某个数据源表不可用时，接口回 200 且该阶段显示为 0，
报表看起来"只是转化率低"；阶段之间不保证单调 ⇒ `rate` 可以 >100、`drop_rate` 可以为负
（`conversion_funnel_baseline_test.go` 的 `_NonMonotonicStagesBaseline` 把这个现状钉在那里）。
未知阶段名走 `GET /conversion-funnel/stage` 也回 200 + 空名字 + 0 计数，而不是 404。
（"前四个"是 T-P4-06 改的措辞：第五段商机**同样**回 200 + 0，但它是这条链上唯一会把取数失败
打进日志的一段，理由见 §4.16.5。吞错误的老四条腿本卡刻意没动。）

### 4.12 销售事件流 `sales_events`：给 LTC 开的预留列（T-P2-04）

`sales_events` 是一张**只增不改的事件表**，写入方按 `event_type` 区分五类事件
（`order` / `followup` / `ai_deal` / `order_draft` / `sales_profile`），读路径只有销售工作台与业绩聚合。
T-P2-04 / R-5 为它加了两列外键位，把原文件头自记的"H2 技术债：事件流里挂不住商机/报价"还掉：

| 列 | 类型 | 可空 | 回填 | 索引 | 生产者 |
|---|---|---|---|---|---|
| `opportunity_id` | `varchar(64)` | 是 | **不回填** | **暂无** | **暂无**（P4 商机事件写入） |
| `quote_id` | `varchar(64)` | 是 | **不回填** | **暂无** | **暂无**（P6 报价事件写入） |

三条口径写进 `model.SalesEvent` 的类型文档（a/b/c），这里说明**为什么长这样**：

- **只开列、不写生产者。** 这两列今天没有调用方（恒为空），凭空造一个 DTO 透传点等于给
  未来的商机域指定形状。§4.11 那条僵尸表就是这么来的。
- **`NULL` 与 `''` 是两层含义。** 列可空且不回填 ⇒ 迁移前已有的行是 `NULL`（这条事件发生在
  "商机"概念存在之前）；迁移后经 GORM 写入的行是 `''`（有这个概念、这次事件没有商机）。
  Go 侧用 `string` 而非 `*string`，省掉写入侧的 nil 判断，代价是 **GORM 读回时两层都塌成空串**，
  要区分只能在 SQL 里写 `IS NULL` / `= ''`。这条差异由
  `internal/repository/sales_event_ltc_columns_test.go` 的 `_NullVsEmptyStringAreTwoLayers` 钉住。
- **暂无索引是可验证的状态，不是遗漏。** `_NoIndexYet` 断言 `sales_events` 上不含这两列的索引；
  P4 落地第一个按 `opportunity_id` 检索的读路径时，随那张卡加 `idx_sales_events_opportunity`
  并**同步改掉**类型文档的 c) 与本节，届时该用例转红是预期中的红。
  提前建索引只会在只增表上加写放大，且没有查询方能验证它有用。

**同时必须说清楚的前置事实**：`sales_events` **今天在生产路径上零写入**——唯一的生产构造点
`NewSalesEventStatsService(` 在非测试代码里 0 命中，注入点 `SetStats(` 亦 0 命中，已接线的
`FollowUpService` 走 `if s.stats != nil` 保护（stats 恒 nil）。所以这两列今天不可能有真实数据，
该状态由 `scripts/check-unwired-assets.sh` **项 9** 按 `unwired` 登记盯梢；接线前不得对外宣称
"商机事件已入库"。

### 4.13 知识库版本与灰度：`knowledge_bases` 上的三列（T-P2-05）

| 列 | GORM 类型 | PG 实测类型 | 可空 | 默认 | 唯一读者 |
|---|---|---|---|---|---|
| `version` | `int` | **`bigint`** | 否（`not null`） | `1` | `service.KBAnswerVersionFor` |
| `canary_enabled` | `*bool` | `boolean` | 是 | `false` | 同上 |
| `canary_percent` | `int` | **`bigint`** | 否（`not null`） | `0` | 同上 |

三列由 `AutoMigrate` 补（`internal/pkg/db/migrate.go:232` 已注册该模型），**没有任何版本化
迁移文件**——仓内 `ExecuteUpgrade(ctx,"v1.0.0","v1.0.0")` 恒早退，迁移目录今天不执行（第四次确认）。

**这三列不改任何一张内容表，它们只决定 `rag_answer_cache.prompt_version` 用哪个命名空间。**
原因见 `AI_CORE_FEATURE_INVENTORY.md` 短板 **G18**：`knowledge_bases` 不在生产检索路径上
（FAQ 按 `agent_id`、chunk 按 `product_id` 取数，两张内容表连 kb_id 列都没有），所以"知识库版本"
唯一能真正生效的地方就是那张**按 kb_id 索引**的答案缓存表。版本号的物理载体是命名空间号
（`v1`/`v2`/…），不是行复制、也不是内容副本：

- `version <= 1 ⇒ "v1"`（`faqPromptVersion` 常量）。它兜住的是"还没被切过版本的号"：从没
  publish 过的 1，以及 `0` 与负数（**实测更正，两次**：先误判"`Select("*")` 会把零值显式写进
  列 ⇒ service 必须归一"，探针推翻；再误判"新行落 1 靠 DB `DEFAULT 1`"，把列默认值
  `DROP` 掉之后只走仓储 `Create` 仍落 1、且不违反 `NOT NULL` ⇒ 真相是 **GORM 按
  `gorm:"default:1"` 标签在客户端填值**。仓内正常通路造不出 `version=0`，"认 0" 是防御，
  证据链见 `user-server/internal/service/knowledge_base_version_start_test.go` 文件头）。
  这些一律与升级前**逐字节相同**（AC③，差分实跑见任务清单 r22）。别把它改成从 `v0` 起步或
  去掉 `<=1` 归并——那等于一次清空全量答案缓存。
- 灰度组用 `version + 1` 号而不是带后缀的临时号，于是"转正"退化成一次指针移动：灰度期焐热的
  行正好是新版的稳定流量行，**回滚和再放量都不需要重灌**（AC②，由
  `kb_canary_test.go` 的 `_TwoVersionsCoexistAndRollbackNeedsNoReingest` 拿真 pgvector 表实证）。
- 两道独立的锁：环境变量 `FF_LTC_KB_CANARY`（三态 `off|shadow|on`，默认 off，布尔式真值只到
  shadow，口径同 `tool_circuit_breaker_wiring.go`）与行上的 `canary_enabled + canary_percent`。
  旗子不为 `on` 时恒用 `v1` ⇒ **本卡上线当天对线上缓存零影响**，运维抬旗才生效。
- 分桶复用 `feature_flag.go` 的 `flagBucketHash`（FNV-1a，key = `"kb.{id}"`，`%100 < percent`），
  与 `script_ab.go` 同族 ⇒ 仓内不出现第四套分桶口径；分桶键带 kbID，不同 KB 的灰度人群互相独立。

**写入这三列必须走 `KnowledgeBaseRepository.UpdateVersionCanary`，不能走通用 `Update`。**
通用 `Update` 的固定列 map 不含这三列（写了也丢），而 GORM 的 `Updates` 会顺手 bump `updated_at`
——`updated_at` 正是答案缓存的失效信号（`cache/service.fresh()` 一见它前进就删行），那样切一次
版本就把另一版本的缓存行全删了，上面那条 AC② 当场不成立。`UpdateVersionCanary` 用
`UpdateColumns` 绕开自动时间戳，`kb_version_canary_test.go` 的 `_KeepsUpdatedAt` 以"通用 `Update`
会 bump / 专用方法不会"的**对照断言**把这条锁死。

**`not null` 是必需项而不是修饰**：可空的话一旦有行存成 `NULL`，GORM 读回 Go `int` 是转换错误而非
零值 ⇒ 整行连管理端列表都打不开。`_LegacyRowsDefaultToOne` 除断言折算外还直读
`information_schema`，钉住"存在 + `not null` + 默认值 `'1'`/`'0'` + 类型 `bigint`"四项；其中
`bigint` 这条容易写成 `integer` 而假绿，勿放宽断言。

### 4.14 审批检查点 `approval_requests`：一套状态机，两种退化形态（T-P3-01）

C2 的裁定是**只建一套**：A 文档的"同步二次确认"是本表在 `auto-approve` 快速路径下的退化形式
（入队即 `approved`，调用方原地拿结果、不轮询），B 文档的"异步审批检查点"是同一条记录处于
`pending` 的形状。因此本表**没有** `mode` / `sync` 这类列，区分只由 `status + decided_by` 表达。

列与索引全部为实测值（`information_schema.columns` + `pg_indexes` 直读，非从标签推断）：

| 列 | GORM 声明 | PG 实测 | 可空 | 索引 |
|---|---|---|---|---|
| `id` | `type:text;primaryKey` | `text` | **否** | `approval_requests_pkey` |
| `subject_type` | `varchar(32)` | `character varying` | 是 | `uq_approval_request_open` 前缀 1 |
| `subject_id` | `type:text` | `text` | 是 | 同上，前缀 2 |
| `policy_key` | `varchar(64)` | `character varying` | 是 | 同上，前缀 3 |
| `status` | `varchar(16)` | `character varying` | 是 | `idx_approval_requests_status` + 上面那个索引的谓词 |
| `resume_token` | `type:text` | `text` | 是 | `uq_approval_request_token`（部分） |
| `expires_at` | `*time.Time` | `timestamp with time zone` | 是 | `idx_approval_requests_expires_at` |
| `decided_by` / `decision_note` | `type:text` | `text` | 是 | 无 |
| `decided_at` | `*time.Time` | `timestamp with time zone` | 是 | 无 |
| `created_at` / `updated_at` | 时间 | `timestamp with time zone` | 是 | `idx_approval_requests_created_at`（仅 created_at） |

两条唯一索引的实际定义（由 `approval_request_test.go` 的 `_PartialIndexesExist` 断言"包含"而非整串
比对，PG 会把谓词规范化）：

```sql
CREATE UNIQUE INDEX uq_approval_request_open  ON approval_requests USING btree
  (subject_type, subject_id, policy_key) WHERE ((status)::text = 'pending'::text);
CREATE UNIQUE INDEX uq_approval_request_token ON approval_requests USING btree
  (resume_token) WHERE (resume_token <> ''::text);
```

**两个索引都必须是部分的**，这不是优化而是语义：

- 不带 `status='pending'` ⇒ 已裁决的行永久占着身份键位，被拒的报价修正后**再也申请不了**
  （同一 (subject, policy) 建不出第二条 pending），闸门把自己锁死。所以"重审 = 新建一条、留下
  两条记录"这条路依赖这个谓词。
- 不带 `resume_token <> ''` ⇒ 第二条 auto-approve 记录撞在空串上。auto-approve 的行永远不被
  恢复，凭证列就是 `''`，且可以有很多条。
- `policy_key` 必须进键：同一对象常同时要过两道策略（报价既过 `quote.send` 又过高折扣档），
  只按 subject 去重会让第一道审批顺手把第二道批了 —— 闸门被自己绕过。

**四列身份/状态位在 PG 里全部可空，这是实测事实，不是遗漏。** GORM 不给非指针 `string` 发
`NOT NULL`（除非显式写 `not null` 标签），而这里刻意不加：`NOT NULL` 拦得住 `NULL`、拦不住 `''`，
后者才是真实危险输入（一行 `subject_type=''` 既进不了待办中心也回不去幂等键），而它只能由入参校验
拦 —— 落点在 `service.ApprovalSubmitInput.normalize()`（空值、长度上限 32/64/256、字符集收窄到
`[a-z0-9_.-]`）。字符集这条与索引无关但同属键的卫生：`subject_type`/`policy_key` 会进入
`check-unwired-assets.sh` 按 `|` 分列的匹配面，留一个自由字符串进去，迟早有人拿 `"a|b"` 当策略名，
把一行登记劈成两行。

**裁决写回走列白名单 + CAS，且 `Model()` 必须传空壳。** `approvalWriteColumns` 只含
`status/decided_by/decided_at/decision_note/updated_at` —— 身份三列与 `resume_token` 改不动
（否则"裁决 + 顺手改 subject"= 用一次合法批准给另一张报价开门）。实测踩到的坑是 GORM 的
`tx.Model(&a)`：它会从 `a` 的主键值再拼一条 `id = ?` 进 `WHERE`，而 `a` 是刚被 `fn` 改过的实例，
`fn` 一旦动了 `m.ID`，两条 `id` 条件互斥 ⇒ 更新命中 0 行 ⇒ `applied=false` ⇒ service 对一条
**仍然 pending** 的记录报出"已由他人裁决"。改成 `tx.Model(&model.ApprovalRequest{})` 后由
`_MutateCannotRepointIdentity` 实证。

**"单赢家"与"行锁"是两件事，本卡实测把它们分开了**：并发 8 协程同一行只有 1 个 `applied`
（`_ConcurrentMutateSingleWinner`），但**撑住它的是写回那句 `WHERE id=? AND status='pending'`，
不是 `FOR UPDATE`** —— 变异 Mu-R3 摘掉行锁后该用例仍绿（UPDATE 自己会排队，重判时行已不是
pending）。`FOR UPDATE` 买的是"`fn` 执行期间这一行不许被别人改"，其存在性由
`_MutateHoldsRowLockWhileFnRuns` 直接观测（让 `fn` 停在事务里，另一条连接改同一行必须阻塞）。
仓储与本测试文件原先都写着"没有 FOR UPDATE 时两边都会以为自己批成功"，那是**没跑过的推断**，
Mu-R3 把它推翻后已按实测改写。

**接线现状与边界（T-P3-02，2026-09-20）**：本表的写入方与读取方今天**都在**（§4.14 立表时登记的"零构造、零读取"已被本卡推翻）：`NewApprovalRequestService(` 由 `internal/app/approval_runtime_wiring.go` 构造、`ByResumeToken(` 由 `ApprovalResumeBridge.ResolveOnFire` 在定时器点火那一刻回读结论、`ExpireOverdue(ctx, limit)` 由 `ApprovalSweepWorker.RunOnce` 每 5 分钟按节拍调用 —— 闸门 **项 12** 随之由两行 `unwired` 扩为三行并全部翻 `wired`（防回退；`ExpireOverdue` 那条的匹配式按"两个实参"的形状与草稿侧同名方法分开，否则删掉清扫器不会红）。**但装配挂着旗子**：`FF_LTC_APPROVAL_RESUME` 默认 `off`，该档下运行时根本不构造（表仍零写入、图里的审批等待节点判失败），`shadow` 只清扫到期、不装挂起桥，只有显式 `on` 才启用挂起/恢复，布尔真值一律降档到 `shadow`。所以"这张表有生产写入方"只在把旗子推到 `on` 之后成立，今天的库里它仍是零写入。
同理，`AutoApprovalPolicy` 刻意**没有**接到旧的 `approval.WhiteListApprovalChecker`（`(subject_type, subject_id)` → `(tool_name, account_id)` 的映射是调用方的知识），`decorator_approval.go` 一个字节未改 —— 那是 C2 的硬约束。
本条竖"建立了什么、没建立什么"的清点（四件事）见 `AI_CORE_FEATURE_INVENTORY.md` 短板 **G20**。

---

### 4.15 统一人工待办 `human_tasks`：一张表三类事，三列 SLA 各占一档（T-P3-03）

C3 的裁定是**统一数据模型 + 分离视图**：会话转人工、等报价审批、催收升级在三条竖里都是
"某件事停下等人"，所以共用一张表与**同一条状态机**；而两个消费入口（坐席收件箱按会话组织、
SLA 以分钟计；待办中心按单据组织、SLA 以小时/天计）必须分开，否则"等报价审批 3 天"会被
算进坐席响应时长。本表因此**没有** `view` / `channel` 这类视图列，分开只由 `kind + 三个
独立 SLA 列` 表达。

列与索引全部为实测值（`information_schema.columns` + `pg_indexes` 直读，非从标签推断）：

| 列 | GORM 声明 | PG 实测 | 可空 | 索引 |
|---|---|---|---|---|
| `id` | `type:text;primaryKey` | `text` | **否** | `human_tasks_pkey` |
| `kind` | `varchar(32);not null` | `character varying(32)` | **否** | `idx_human_tasks_kind` |
| `status` | `varchar(16);not null` | `character varying(16)` | **否** | `idx_human_tasks_status` + 下面那个索引的谓词 |
| `subject_type` | `varchar(32)` | `character varying(32)` | 是 | `uq_human_task_open` 前缀 1 |
| `subject_id` | `type:text` | `text` | 是 | 同上，前缀 2 |
| `title` / `reason` / `payload_ref` / `assignee_user_id` / `cancel_reason` | `type:text` | `text` | 是 | 无 |
| `one_id` | `varchar(100)` | `character varying(100)` | 是 | `idx_human_tasks_one_id` |
| `claimed_at` / `completed_at` / `cancelled_at` | `*time.Time` | `timestamp with time zone` | 是 | 无 |
| `sla_first_response_at` | `*time.Time;index` | `timestamp with time zone` | 是 | `idx_human_tasks_sla_first_response_at` |
| `sla_decide_at` | `*time.Time;index` | 同上 | 是 | `idx_human_tasks_sla_decide_at` |
| `sla_escalate_at` | `*time.Time;index` | 同上 | 是 | `idx_human_tasks_sla_escalate_at` |
| `created_at` / `updated_at` | 时间 | `timestamp with time zone` | 是 | `idx_human_tasks_created_at`（仅 created_at） |

开放唯一索引的实际定义：

```sql
CREATE UNIQUE INDEX uq_human_task_open ON public.human_tasks USING btree
  (subject_type, subject_id)
  WHERE (((status)::text <> 'done'::text) AND ((status)::text <> 'cancelled'::text));
```

**它必须是部分的，而且谓词只能写成"非终态"这一侧。** 两点都是实测逼出来的：

- 不带谓词 ⇒ 处理完一条会话待办之后，同一条会话**再也转不了人工**（旧行永久占着
  `(subject_type, subject_id)`）。幂等要的是"同一件事的第二次投递回到同一条**开放**待办"，
  不是"永远只有一条"。
- 谓词写成 `status IN ('pending','claimed')` 语义等价，但**标签里放不进去**：GORM 用逗号
  切分 tag 段，`where:status IN ('a','b')` 会被当场截断成两段（本卡实测踩过）。所以这里只认
  "排除两个终态"的否定式写法 —— 代价是**加第五个状态时必须记得改这个谓词**，否则新状态会跟
  终态抢坑位；这一条由 `model.HumanTaskTerminalStatuses`、`TestHumanTaskOpenPredicateMatchesIndexSQL`
  与仓储侧 `TestHumanTaskRepo_OpenIndexIsPartial` 三处盯住。

**三列 SLA 互斥，且互斥不落在库里。** `conversation_handoff` 只写 `sla_first_response_at`、
`approval` 只写 `sla_decide_at`、`collection_escalation` 只写 `sla_escalate_at`；判据是
`model.HumanTaskSLAField(kind)` 给列名 + `humanTaskPlaceSLA` 落列 +
`model.HumanTaskSLAUnsupportedFields(task)` 在写入前拒绝越界列（判据是包级函数而不是
`*HumanTask` 的方法 —— 五层架构门禁止 model 携带业务方法，见 `check-architecture.sh` [4/10]；
本卡首版写成方法，被这道门当场判红）。**为什么不是一列 `sla_due_at`**：
AC④ 要的"指标隔离"必须能被查询表达 —— 逾期读数按类各查自己那一列
（`CountOverdueOpenByKind` 用 `HumanTaskSLAField(kind)` 动态选列），一列的话"会话首响超时"
与"审批超时"在同一列上不可分，而合并视图正是 C3 点名要防的事。为什么不在 DB 加 CHECK：
本仓 PG 侧不写 CHECK（`migrations/` 的版本化迁移在启动路径上固定空跑，见 §七），
库内约束只能靠 AutoMigrate 的列与索引，值域判据一律在 `service`。

**四个状态，刻意没有 `expired`**（与 §4.14 的 `approval_requests` 不同）：待办的逾期是
**读数**不是**状态**。加一个 `expired` 就得配一个把开放行改写成终态的清扫器，而"没人处理"
这件事一旦被系统自动落成终态，就会同时从收件箱、从 `total_open`、从逾期读数里消失 ——
指标把要暴露的问题自己抹平了。逾期只由 `sla_* < now` 与"仍开放"两件事算出来。

**长度上限分两档，处置不一样。** `subject_type`(32) / `subject_id`(256) / `payload_ref`(512) /
`one_id`(100) 越界**即拒**（身份字段裁断等于把待办挂到另一件事上）；`title`(200) / `reason`(2000)
越界**只裁断**，且按**字符**裁而不是字节（`varchar` 与列表页数的都是字符，按字节砍会留下半个
汉字）。落点在 `service.HumanTaskSubmitInput.normalize()`。可空性与 §4.14 同理：GORM 不给
非指针 `string` 发 `NOT NULL`，本表只有 `id/kind/status` 显式写了 `not null`，其余全靠入参校验。

**建表登记走 `allModels()`，卡面写的 `v3_47_0_human_task_migration.go` 不存在**（刻意）：
本仓启动时的版本化迁移固定空跑（`v1.0.0→v1.0.0`），生产 schema 由 GORM AutoMigrate 遍历
`internal/pkg/db/migrate.go` 的 `allModels()` 得出，因此登记处是那里 + `migrate_test.go` 的
`mustCover` 各一行。少这一行的失败面与 §4.14 同形：代码全对、表不存在，而转人工只在日志里
说一句"投递失败"。

**接线现状（T-P3-03，2026-09-20）**：`conversation_handoff` 有真实生产写入方 ——
`transferToHuman` 在会话状态落库后投递一条（幂等：同一次转人工被多个策略出口各叫一次只落
一行），会话 `resolved/closed` 时由 `cancelOpenHumanTaskForSession` 撤销那条开放待办，
坐席侧四个动作端点走 `/api/human-tasks/:id/{claim,release,complete,cancel}`；闸门 **项 13**
四行按 `wired` 登记（防回退）。本卡**没有旗子**：唯一的关闸是"拿不到 DB 句柄 ⇒ 全局服务为
nil ⇒ 编排器不挂生产者"，此时 `transferToHuman` 与本卡之前逐字一致。
`approval` / `collection_escalation` 两类今天**没有任何生产 Submit**（报价审批写的是
§4.14 那张表，催收竖未开工），所以"三类统一收口"这句话只成立一类，边界清点见
`AI_CORE_FEATURE_INVENTORY.md` 短板 **G21**。

### 4.16 商机 `opportunities`：一列"到哪一格"、一列"成没成"，两列不许合并（T-P4-01）

开工前实测：全仓对商机的表达只有 `clues.is_opportunity` 一个 0/1 布尔位，model 层对
`opportunity` 零命中 —— 所以本节不是"给已有表补字段"，而是商机第一次进 schema。
C5 裁定三套评分是三个不同的条件概率、**禁止合并**，落到本表就是：这张表只带
`win_probability`（"这单能不能成"），`confidence`（回答对不对）留在会话侧、
`lead_score`/RFM/churn（这个客户值不值得投入）留在客户侧。
`TestOpportunityCarriesOnlyWinProbability` 专门拦"往本表再加一列评分"，变异 M4 已证摘掉即红。

列与索引全部为实测值（`information_schema.columns` + `pg_indexes` 直读，非从标签推断）：

| 列 | GORM 声明 | PG 实测 | 可空 | 索引 |
|---|---|---|---|---|
| `id` | `type:text;primaryKey` | `text` | **否** | `opportunities_pkey` |
| `code` | `varchar(32);uniqueIndex` | `character varying(32)` | 是 | `idx_opportunities_code`（唯一，**不带谓词**） |
| `customer_id` | `varchar(64);index` | `character varying(64)` | 是 | `idx_opportunities_customer_id` |
| `one_id` | `type:text` | `text` | 是 | 无 |
| `clue_id` | `varchar(36)` | `character varying(36)` | 是 | 无 |
| `stage` | `varchar(32);index` | `character varying(32)` | 是 | `idx_opportunities_stage` |
| `status` | `varchar(16)` | `character varying(16)` | 是 | **无**（理由见下） |
| `amount` | `numeric(12,2)` | `numeric(12, 2)` | 是 | 无 |
| `currency` | `varchar(3);default:'CNY'` | `character varying(3)`，默认 `'CNY'::character varying` | 是 | 无 |
| `win_probability` | `numeric(5,2)` | `numeric(5, 2)` | 是 | 无 |
| `owner_user_id` | `varchar(64);index` | `character varying(64)` | 是 | `idx_opportunities_owner_user_id` |
| `expected_close_at` | `*time.Time` | `timestamp with time zone` | 是 | 无 |
| `lost_reason` | `type:text` | `text` | 是 | 无 |
| `version` | `bigint;not null;default:0` | `bigint` | **否**（本表除 `id` 外唯一） | 无 |
| `created_at` / `updated_at` | 时间 | `timestamp with time zone` | 是 | `idx_opportunities_created_at`（仅 created_at） |

**`stage` 与 `status` 必须是两列，这是 AC① 的全部落点。** 过程侧 `stage` 只有四格
（`qualification → needs_confirmed → proposal → negotiation`），结果侧 `status` 四态
（`open/won/lost/cancelled`），两族字面值互斥由用例逐字钉住（`OpportunityStageIndex` 对任何
status 值都回 `-1`）。把 won/lost 塞进 stage 的后果不是难看，是**漏斗最后一格与赢单率塌成
同一个数** —— 而 C6 的北极星（闭环完成率 = 完成回款的商机数 / 新建商机数）按 `status` 算、
T-P4-06 的漏斗按 `stage` 算，两者必须能同时成立（"停在 proposal 就成了"是合法的商机）。
`OpportunityClosed` 与 `OpportunityOutcomes` 的差别同样是被测出来的口径：closed 回答"还需要
有人推进吗"，outcome 回答"这单最后怎么样了"；**`cancelled` 只在前者里**，误建的商机不该进
丢单归因（P8 看板要拿它给销售团队定改进项）。

**`status` 刻意不建单列索引**（首版写了 `index`，被本卡自己的索引纪律用例当场判红后摘掉）：
四个取值的大表上规划器多半仍顺序扫，而"只看还在跑的商机"实际形状是
`WHERE owner_user_id = ? AND status = 'open'` —— 由 owner 索引取行、status 只做过滤。
真出现"全站按 status 捞"的读方时该建的是**复合**索引，随那张卡一起改这里。
`TestOpportunityIndexesOnlyForNamedQueries` 两侧都断：正向 6 个（`id/code/customer_id/stage/
owner_user_id/created_at`）逐个要点名下家，反向 9 列（含 status、amount、win_probability、
version）不得有任何索引（变异 M9 摘掉已命名下家、M10 凭空铺预留索引，各自被抓）。

**两处与相邻表结论相反、但都刻意的选择**：① `code` 的唯一索引**不带谓词**，与
`approval_requests.resume_token`（§4.14）恰好相反 —— 那里的空值是合法常态，这里"没编号的
商机"第二条就撞死，因为编号是对人承诺的键，不该悄悄积累。② `currency` 是本表**唯一带语义**
默认值的列（`'CNY'`）：金额不带币种不可算，而空串会让报表静默算错；`stage`/`status` 不设默认
也不矛盾 —— 那两列的空值是**可诊断的形状**，值域校验会抓住它。默认值顺带保住"将来补列能填老行"。
（T-P4-02 起 `version` 也带 `DEFAULT 0`，但那是**补列能过去**的前提而不是语义默认，
两者理由不同，别读成同一类。）

**键宽与"归属 ≠ 权限"**：`customer_id`/`owner_user_id` 定宽 64 是被下游抄写列（`sales_events`）
反推的，本表更宽 ⇒ 到事件写入那一步才炸、且 `CreateInBatches` 整批回滚；`clue_id` 取 `clues.id`
的真实宽度 36；`owner_user_id` 用 string 而非 uint，因为销售身份的真源是 `SalesProfile.SalesID`
（`feishu.go` 的 `OwnerUserID uint` 是另一族，不构成本列先例）。X3 单商户 ⇒ **无租户列**
（标签层与真库层各一条用例，变异 M3 塞列即两包同红），`owner_user_id` 只是归属不是权限。
可空性的现实与 §4.14/§4.15 同：GORM 不给非指针 `string` 发约束，值域一律在 `service` ——
T-P4-01 时实测只有 `id` 是 `NOT NULL`，T-P4-02 补 `version` 后是 `id` 与 `version` 两列
（后者是**显式**写了 `not null`，理由见下面那段补列）。

**建表登记走 `allModels()`，卡面写的 `v3_48_0_opportunity_migration.go` 不存在**（第 7 次撞上
同一形状）：本仓启动时的版本化迁移固定空跑（`v1.0.0→v1.0.0`），生产 schema 由 GORM AutoMigrate
遍历 `internal/pkg/db/migrate.go` 的 `allModels()` 得出，登记处是那里 + `migrate_test.go` 的
`mustCover` 各一行。少后一行的失败面与 §4.14/§4.15 同形：代码全对、表不存在。

**接线现状（T-P4-01，2026-09-20）**：本卡只有**列与值域**，今日**零生产写入方**（构造与分配在
T-P4-05），未接线台账 **项 16** 已按此登记旗子（现值 38/45，`&model.Opportunity{}` 在
`internal/pkg/db` 的是建表登记、不算写入路径，故被该项的搜索范围排除）。本卡欠三件、各有名主：
乐观锁版本列 → T-P4-02（**已随 T-P4-02 交付**，见下一段），跃迁合法表与赢率计算式 → T-P4-03
（**已随 T-P4-03 交付**，见 §4.16.2），详情与五条写入口**已随 T-P4-04 交付**（见 §4.16.3），
**列表端点仍欠** —— 仓储的 `List` 今日不返回 `total`，凑不出 `{list,total}` 那句业务判断。
`win_probability` 与 ltc.config 的 `win_probability` 阈值**同量程 0–1**（默认 0.50），
所以比较不需要换算；那枚阈值今天在生产代码里仍零读者，消费方就是本卡的这一列 + T-P4-05/T-P6-04。
（**状态迁移 2026-09-21**：上段的"今日零生产写入方"到 T-P4-05 结束 —— 转换层是这张表的第一个
生产写入方，见 §4.16.4；`win_probability` 阈值那一格**本卡没接**，兑现点仍在 T-P6-04/P7。）

### 4.16.1 `opportunities` 的仓储层：CAS 拒整份、白名单是 map、排序带兜底键（T-P4-02）

`internal/repository/opportunity.go` 是 `service.OpportunityService`（T-P4-03）与这张表之间唯一
一层，只做三件事：写进去、按已命名的三个维度读出来、改写时保证"后写不静默覆盖先写"。
**刻意没有内存版底座**（同 §4.14 的审批表）：商机一旦有内存影子，"库里那条已经被别人改到 v5"
这件事就永远测不出来，而 AC① 要的正是它；句柄不可用 ⇒ 每个方法明确报错（"句柄没了 ⇒ 列表回空"
会被上层读成"这个客户没有商机"，继而把在跑的商机重建一遍）。

**`version` 为什么写成 `bigint;not null;default:0` 三件套**：`not null` 与 `default` 是**成对**
才有意义的 —— 给存量表补一个 `NOT NULL` 且无默认的列，PG 那一步就直接失败，而本仓的生产建表走
AutoMigrate，"补列"是必然会发生的一次演进。判据不是推理：
`TestOpportunityAutoMigrate_BackfillsVersionOnExistingTable` 先按 **T-P4-01 的形状**手写一张没有
version 的表、塞两行存量数据，再跑 `AutoMigrate`，然后按 `information_schema` 断
`is_nullable='NO'` / `column_default` 以 0 开头 / `data_type='bigint'`，并用一个**独立零值 struct**
读回老行（复用插过值的实例读会因 GORM 把旧字段并入 WHERE 而假红）。有符号也是判据的一部分：
无符号回绕会让"很久以前读到的那版"与"新写入的那版"撞成同一个值。

**并发口径选 CAS 而不是 `SELECT … FOR UPDATE`**：卡面 AC① 写的是"版本控制"。行锁会让第二个
写者**等**到第一个提交、再拿着刚读到的新值改成功，于是"两人各改不同字段"变成两次都成功、
彼此的改动都在 —— 这要求每个调用方只改自己那一格，而"改了哪几格"没有任何一层能证明。
CAS 的口径更硬：手里那份不是最新就整个拒绝，丢改动**可见**，静默混合不可见。
`_ConcurrentUpdateSingleWinner` 是 8 协程打同一行、栅栏放行，恰好 1 个成功 7 个
`ErrOpportunityStaleVersion`、末态 `version=1`。

**改写白名单就是那个 map**（10 个键），map 之外的列改不动，各自对应一处真实破坏：`id` 被下游表
抄走（改它 = 让所有引用指向不存在的行）、`code` 出现在工单与口述里、`customer_id`/`one_id`/
`clue_id` 是客户身份与来源（要换客户只能 `cancelled` + 重建，"挪到另一个客户名下"这段历史必须
留下）、`created_at` 是北极星分母的时间尺、`version` 只由 `version + 1` 这一个表达式走。
两处**只有真跑才会暴露**的静默失效记在代码注释里：① 首版在这里另加了 `Select(白名单)`，
SET 语句被那份清单过滤了一遍 —— 不在清单里的 `version = version + 1` 被静默丢弃，版本永远停在
0，八个写者八个全赢，症状是"改写都成功、并发全通过"，正是 AC① 最坏的那种烂法；② struct 形式的
`Updates` **默认跳过零值字段**，"把金额改成 0""把输单原因清空"会当场失效而函数返回成功，
所以走 map。`_UpdateCannotTouchIdentityColumns` 切成两刀测（伪造版本号 → StaleVersion；
换主键 → NotFound 且不许出现新行），`_WritesZeroAndNull` 里 `lost_reason` 先写非空再清空 ——
从"本来就是空串"出发的话，"写不进去"和"写进去了"给出同一个读后值。

**空币种兜底只在空值上生效**：白名单会**强制**写 `currency` 列，照抄 struct 里的空串会把老行的
CNY 冲成空串（金额不带币种不可算）；`currencyOrDefault` 补这一格，同时用例反向钉"改成 USD 必须
落 USD" —— 把兜底写成无条件，一次正常的换结算币种会静默失败并返回成功。

**0 行有两种来路，标签分开**：`WHERE` 命中 0 行后补一次存在性探测，行还在 ⇒
`ErrOpportunityStaleVersion`（该重试）、不在 ⇒ `ErrOpportunityNotFound`（不该重试）。
分错的代价不对称，所以探测保留；它**不参与**并发正确性，决定权在那一条 UPDATE 上。

**读侧三条纪律**：① `statuses` 传空切片**报错**而不是回全表（本表同时装在跑的与已收口的，
"忘了传"如果读成"不过滤"，待推进列表会混进已赢单的行，不报错、不写日志，只在数字上多一截）；
② `limit<=0` 与 `offset<0` 由本层拒 —— 负 offset 的判据写的是**错误来自本层**，漏了守卫时 PG
也会报错（`OFFSET must not be negative`），只查"有没有报错"会绿，而调用方拿到的是听不懂的驱动错；
③ 排序 `created_at DESC, id DESC` 的兜底键由**并列行逐位比对**钉住：变异实测摘掉 `, id DESC`
后三行按插入顺序返回、`limit=1` 翻三页仍然不重不漏（并集那条断言照绿），只有次序断言红了。

**AC②（铁律：仓储无业务判断）** 的反面证明是 `TestOpportunityRepo_DoesNotValidate`：越界的
stage/status、负金额、赢率 42 全原样落库。它红了的含义是"有人在这层加了校验"，修法是把校验搬去
service，不是改掉用例 —— 跃迁表与赢率算法是 T-P4-03 的判据，抄两份迟早分家，而分家之后
"哪一份说了算"取决于请求先撞上哪个方法。

### 4.16.2 `opportunities` 的服务层：跃迁表、赢率式，以及"谁负责把值舍到两位"（T-P4-03）

`internal/service/opportunity.go`（597 行）是这张表上**唯一**的业务判断处：上一段那份"越界值
今天能进库"不是漏洞而是前提，守门从这一层才开始。它不改 schema，但它决定了 §4.16 那两列的
**值域由谁保证**、以及 §4.16.1 那个 `version` 的语义在业务侧怎么被用，故记在本文档。

**跃迁（AC①）**：`stage` 向前恰好一步、向后任意步、**同格不算跃迁**（允许同格改写＝谁都能靠
"再点一次"把 `version` 涨一格，而它的语义是"被成功改过几次"）；`status` 的边表按
**(来源, 起点) → 终点** 三键存成一张 map：`sales` 只放行 `open→{lost,cancelled}` 与
`lost→open`，`collection_completed`（回款完成，P7 才持有）只放行 `open→won`。
AC③"本层写 won 的唯一入口"靠这张表而不是靠"源码里只有一处赋值"——新方法想写 won 必须挑一个
cause，而放行它的只有回款完成；外部另有一条：架构门 [1/10] 禁止 controller 直引 repository。
`won` 与 `cancelled` 是死态（钱已经真实发生 / 这行根本不该存在），`lost` 可回退。
终态之后**不再重算赢率**：那一格留下的是"当时预测它能不能成"，P8 的校准要的正是这个预测与
结果的差；幂等只给"事实的重复上报"（同一条回款完成第二次到达＝成功且零改写，`version` 与
`updated_at` 都不动），不给"跃迁的重复请求"。

**赢率（AC② / C5）**：`p = round₂(base(阶段) − 无归属 0.10 − 已过预计关单日未收口 0.15)`，
下限 0.05；base 为 `qualification 0.10 / needs_confirmed 0.30 / proposal 0.55 / negotiation 0.75`。
**能被证明的只有形状**：本层的阶段跃迁要求逐格向前 ⇒ `{赢单} ⊆ {到达第 k+1 格} ⊆ {到达第 k 格}`
⇒ `P(赢|到第 k 格) = P(赢)/P(到第 k 格)` 随 k 单调不减。这条推导锁不死任何具体数字，所以四个
base 是显式登记的**先验刻度**而非标定值（T-P4-05 起这张表**有**生产写入方了，但转换层写进去的行
一律是 `status=open`，而回标要的是收口行 ⇒ 可回标的样本今日仍为 0；`won` 那一条路仍然只有
P7 的回款完成，不经 HTTP），没有任何收口行可回标，
P8 攒够 outcome 行之后回标改的是这四个数、式子的形状不动。惩罚只做常数、**不做逾期梯度**
（梯度需要一条"逾期时长→概率"的曲线，而它没有任何数据支撑，比常数先验更假）。
输入结构 `OpportunityWinInput` 只有三个字段且字段名不得含 `confidence/lead/score/churn/rfm/probability/value`
（用例用反射钉住），`OpportunityEdit` 里也没有赢率那一格 —— 派生量不许被手写。
**禁止与另两套评分跨域乘算**：三者的条件集不同（这一问一答 / 这个客户 / 这一单自身的阶段与事实），
相乘不对应任何事件。落库侧因此需要"先舍后写"，见下。

**谁舍这一列**（本层与 §4.16.1 的接口处，两处结论刻意不同）：
`win_probability` 与 `amount` 都在**交给仓储之前**舍到列的两位小数。判据不是"PG 会舍所以无所谓"，
而是**返回值与库里必须是同一个数**：真库用例分辨不出本层舍没舍（numeric 列自己会舍，
读回来两边一样），变异"摘掉落库前舍入"就是这么活过第一轮的，最后由一条绕开 PG、
直接断 `Update` 收到的那份浮点值的用例（`TestOpportunityService_HandsRepositoryColumnScaledValues`）
钉住。下限 0.05 而不是 0（0 会被下游读成"绝不可能成"而劝退所有跟进）；上限不写进代码，
四个 base 都 <1 且惩罚只减不加，值域由常量表本身保证。

**改写通道只有一条**（`transition`）：读 → 验现值 →（收口守卫）→（版本比对）→ 改 → 验结果 → 写 →
**回读**。现值与结果各验一次不是重复：前者拦"库里那条本来就坏了"（`ErrOpportunityStateInvalid`），
后者拦"这一刀把它改坏了"；而回读换来的东西是"调用方拿到的 `version` 可以直接当下一刀的期望版本"
——就地 +1 那个便宜不占，`updated_at` 由仓储落，谎时间在"多久没动过"这类排序键上会一路传下去。
四个 sentinel 各自对应一种**修法不同**的失败（改请求 / 改流程认知 / 先回退状态 / 去查数据），
所以 `TestOpportunityService_ClosedRowsRefuseEveryEdit` 除了断"是 `ErrOpportunityClosed`"，
还反向断"不许是 `ErrOpportunityTransitionIllegal`、不许是 `ErrOpportunityStaleVersion`"。

**接线现状（T-P4-03，2026-09-20 实跑）**：本层今日同样**零生产调用方**，未接线台账因此加了一行
**16c 商机服务的装配入口**（`NewOpportunityService(`，scope 只覆盖装配面），与 16a（谁构造仓储）
分开登记 —— 接线有两个断点（装配仓储 / 挂路由），只盯一个会让另一个断了也没人知道。
台账**当时**值 **38/47**（`check-unwired-assets.sh` rc=0，16a/16b/16c 三行均按 UNWIRED 登记；本行记的是 T-P4-03 收尾那一刻，现值见 §4.16.3 末段的 42/49）。
16b（`model.Opportunity{` 在 `internal/service` 的商机行写入点）本卡**刻意不翻**：本层只发
`*model.Opportunity` 指针、不构造新行，构造与一键转商机在 T-P4-05。读端点与 409/400 的映射在 T-P4-04。

### 4.16.3 `opportunities` 的 HTTP 出口：八条端点、一套分诊词表，以及"摘掉装配点"为什么两边都看不见（T-P4-04）

出口面三条路径（`internal/controller/opportunity.go` + `internal/router/opportunity_routes.go` +
`internal/app/opportunity_wiring.go`）：读两条（`GET /api/opportunity/{id}`、`.../moves`）、
规则一条（`GET /rules`）、写五条（`PUT /{id}` 整份改写、`POST /{id}/stage`、`/lost`、`/cancel`、
`/reopen`）。**没有一条能写 `won`** —— 这不是"少做了一个接口"，而是 §4.16.2 那张三元边表在
HTTP 侧的兑现：`won` 只能由 `collection_completed` 落下，所以它没有按钮。用例
`TestOpportunityRoutes_NoEndpointCanWriteWon` 按路由表逐条比对，加一条 `POST /{id}/won` 就红（变异 M24）。

四条判据值得单独记：

**① 绑定一律 `DisallowUnknownFields`，且请求体封顶 4KB。** 派生量（`win_probability`）与身份列
（`id`/`code`/`customer_id`）在服务层的入参结构里**根本没有格子**，"默认丢弃未知字段"会让
`{"win_probability":0.99}` 静默成功 —— 前端以为写了，服务层以为没让写，两边各持一套账。
体积上限也不是防打爆内存（4KB 打不爆），是因为不封顶的请求体让"改一行商机的金额"这个动作的
开销由调用方决定。`http.MaxBytesError` 单列一臂：混进"请求体形状不对"里，运维读到的是
"调用方字段写错了"，实际发生的是"这一格被封顶了"，两种情形的处置动作完全不同。

**② 写入口必须带 `version`，缺字段在本层判 400 而不是让它取零值。** 服务层签名收 `int64`，
传不进来只能是"调用方忘了"，而忘了的默认值 0 恰好是**新行的合法期望版本** —— 放行等于第一次
并发改写永远撞不上锁。乐观锁的"期望版本"必须是**调用方看到的那一格**，本层不重读、不采用最新值。

**③ `400/404/409/503` 共用一套分诊词表**：`input_invalid` / `not_found` / `stale_version` /
`closed` / `transition_illegal` / `state_invalid` / `unavailable` / `internal`。三种 409 分不开
就等于没有乐观锁（"重读再改"与"这单已经收口"与"库里那一行本身越界"是三种完全不同的动作）。
503 的判据不是"错误响应不许带 `data`"，而是**不许有看起来像结果的 `data`**：`{}`、`[]`、`null`
都会被前端长成一句"查过了，没有"，而此刻的事实是"一次都没查" —— 所以 503 带 `reason=unavailable`
而 `data` 整段缺席。

**④ `GET /rules` 在未装配时照样答。** 机器规则不碰库，前端渲染按钮要的就是这份规则；底座挂了
就把规则一起摘掉，等于用一次故障换来"销售流程不存在"的假象。清单里同时给出
`ServerOnlyOpportunityMoves()` —— "存在但不对 HTTP 暴露"那几条边（`collection_completed → won`），
否则这条规则在契约面上读成"产品没有赢单"。

**分层门决定的一个写法**：controller 不许 import repository（架构门 [1/10]），而"这一行不在了"
与"手里那份是旧的"这两种判定的事实来源就是仓储的 CAS 返回值 —— 于是在**服务层**给
`ErrOpportunityNotFound` / `ErrOpportunityStaleVersion` 做**别名**（同款先例见 `human_task.go`），
而不是在 HTTP 侧再造一组 sentinel（那要多一处映射，而映射才是会分家的地方）。

**本卡最值钱的产出是三把我自己写错、被真跑纠正的断言**（变异电池 38 刀，最终 **37 CAUGHT / 1 等价**，
控制组 router ran=29 skip=0 red=0）：

- **M11（摘掉体积上限）跑出来 MISSED，根因是夹具自己是个语法错误的 JSON**：`{"` 再拼一个
  `"version"` ⇒ `{""version"`，那句 400 来自"我打错了"而不是体积 —— 整条用例从第一天起就是假绿。
  现在夹具先自证（能 `Unmarshal`、且确实 >4KB），再断拒因里必须出现"上限"、不许出现 Go 的错误串。
  填充体只用**已声明的字段**重复堆量（JSON 允许同键重复）；若塞一个 `{"pad":"…"}`，
  `DisallowUnknownFields` 会替体积上限挡住那一刀，用例照样永远绿。
- **M17（摘掉控制器的空 id 关）照样全绿**：服务层 `Get` 自己也 `TrimSpace` 拒空，**读口**看不出差别；
  差别只在**写口** —— `transition` 不判空，摘关之后 `PUT /api/opportunity/%20` 会拿一个空白串去
  库里查一趟、落空后回 **404「这条商机不存在」**，而事实是"这次请求根本没给 id"。新增用例用
  `failingOpportunityRepo` 把"根本没查"断出来：一查就是 500，绝不会是 400。
- **M21（把 `closed` 与 `stale_version` 两条 case 换个顺序）是真正的等价变异**：服务层一次只返回
  一个 sentinel（§4.16.2 的 `opportunity.go:509` 收口、`:512` 版本撞车，且 `:506` 的先验校验在前），
  两条 `errors.Is` 永不同时为真，换序改不了任何输出。留着它只会让 MISSED 名单里混进一把本来没牙的刀，
  故换成同族里有牙的那把：把已收口的行贴成 `stale_version` 的 reason（那正是"刷新一下就好"的误导本身）。
- **M33（`router.go` 摘掉 `app.InitOpportunityRuntime`）Go 与台账两边都看不见**：装配函数、控制器、
  挂载函数三个字面量全都还在，端点也照样挂在树上，只是运行时全局句柄永远是 nil ⇒ 八个端点全部退成
  503。旧的两条路由用例为什么一起漏：`MountedBySetup` 只看 `engine.Routes()`（挂上 ≠ 活的），
  匿名探针判"非 2xx"（503 恰好也是非 2xx）。现在两把一起守：`TestOpportunityRoutes_LiveThroughRealSetup`
  带合法令牌真读得到那一行（并先把全局句柄洗成 nil —— 不清这一把的话，同进程前一条用例留下的实例
  会让摘刀在**全量跑**里照样读得到 200），台账加 **16e 启动装配点**。两把都反向验过。
- **M27（未装配分支只改一句启动日志）判为等价**，理由登记而不降级成"已覆盖"：两个分支都只是打日志，
  `return` 摘掉也不改变任何响应；本仓今日没有可断日志行的采集面，为一句话日志新开一个采集层不划算。

**台账现值 42/49**（`check-unwired-assets.sh` rc=0）：16a（仓储装配入口）与 16c（服务装配入口）
比 §4.16.2 的原计划**早一张卡**同时翻 wired —— 这一卡交付的是 HTTP 出口，而出口必须自带底座，
`app.InitOpportunityRuntime` 一行同时接上两个断点；判据仍分开跑，因为"摘掉仓储那一行"与"摘掉路由
那一行"是两种不同的破坏。新增 **16d 挂载入口**、**16e 启动装配点**。16b（商机行的生产写入点）
**仍按 UNWIRED 登记**，兑现点 T-P4-05。（**本段是 T-P4-04 收尾那一刻的值**；下一卡兑现之后
台账变成 **47/53**，四行新增绊线见 §4.16.4 末段。）

**列表端点刻意不交付**（登记为欠账而不是"已完成"）：仓储的 `List` 今日不返回 `total`，
而 CLAUDE.md 的列表契约要求 `{list,total}` —— 用 `len(list)` 凑一个 `total` 会让"这页是第 3 页、
一共多少条"这句业务判断从"查过"变成"猜的"。详情页因此也只给单行读口。

**swag 未随本卡重生成**（与前几张卡同一口径）：工作树里另有并行会话未审阅的注解，
一次重生成会把它们一并灌进 `docs/`。本卡的 Swagger 判据是静态的：`opportunity_routes_test.go`
逐条断言八条端点各有一行 `@Router` 注解，摘掉任一条即红。

**实跑口径（两种树分开记，别混读）**。本卡的验收数字一律取自 **`--shared` 克隆、HEAD `292c92e3`**：
router 150 / app 164 / controller 781 / service 3509，`TZ=America/Shanghai` 与 `TZ=UTC` **同数同绿、失败 0**，
`go build` / `go vet` rc=0，`git archive HEAD` 独立解包可编译 rc=0，八条结构门 rc=0。
**同一天的工作树**另有一组数（controller 797 / service 3722），差额 16 / 213 逐条对上并行会话**未提交**的测试文件
（工作树 `^func Test` 计数 controller 797、service 3741，克隆 781、3528）⇒ 引用任何一个数都要先说哪棵树；
本卡提交信息里那句"controller 797 / service 3722"是工作树口径而未标树，**以本段的克隆口径为准**。

**`-race` 四腿是本卡顺手扩的一表面，且量出一条既有缺陷**：router 150 / app 164 / controller 781 三腿 race=0；
service 腿 rc=1 pass=3507 fail=2 race=2。两处竞态同根：测试用 `db.SetTestDB()` **写包级全局 DB 句柄**
（`customer_session_blacklist_test.go:24`），而上一个用例留下的 **fire-and-forget goroutine** 正在读同一个句柄
（`customer_service_plus.go:487` 的 `MaybeSendAwayReply` → `office_hours.go:45` → `NewSystemConfigKVRepository`；
`customer_session.go:463` 的 `DispatchSessionEventAsync` → `session_chain.go:202` → `NewAutomationRuleRepository`）。
**判为既有、不是本卡引入**，且这条判断是量出来的不是推出来的：同一份全量 `-race` 在父提交 `11755c55`
（不含本卡八个文件）上跑出 **1 处 DATA RACE / 1 个 FAIL（`TestCreateSession_AnonymousUser`）**，写方与读方的栈
与本卡第二处**逐帧相同**。两处 vs 一处的差额**不解释成"本卡多引入一处"**：这两个读方都由 `CreateSession`
同一条路派出，检测器在一个用例里只报它先撞上的那一队，命中哪一队取决于调度。本卡**不修它** —— 涉及的四个文件
（`customer_session.go`、`customer_service_plus.go`、`office_hours.go`、`session_chain.go`）都不在本卡交付面里，
且这类"全局句柄 + 异步读"的修法要动测试底座、会撞上并行会话正在改的同一批文件；登记为**待处置项**
（与既有的"service 用例共享进程状态"同族）。另登记一条口径事实：**`-race` 不在本仓任何门禁脚本里**
（`scripts/` 下只有审计轨的记录文档提到 `-race`），所以这条红不会被默认门拦住 —— 第六步全量回归若沿用现有门，同样看不见它。


### 4.16.4 `clues → opportunities` 的转换层：三道判据、三条分配规则，以及"为什么一个字节都不写 `is_opportunity`"（T-P4-05）

**§4.16 那句"商机已入库"到这一节才成立**：前面三张卡交付的是表、仓储、状态机与 HTTP 面，
`opportunities` 在生产路径上的写入数一直是零（台账 16b 按 UNWIRED 登记就是为这件事留的绊线）。
本节交付的是那第一个写入方，以及它背后的两件判据：**什么算"达标到该建商机"**（AC①）与
**建出来归谁、凭什么归他**（AC③），外加一条本卡自己新增的向后兼容判据（AC②）。

**"一键"指的是运营的那一个开关，不是页面上的一个按钮**。依据是三方调研成果.md 对 AC1 的原话
"一键开启 LTC 后，新线索可**无人值守**走完…"，所以本层的调用方是线索挖掘那条写路径，人在链路里
不需要点任何东西。HTTP 侧的手工"一键转商机"入口今天**仍不存在**，登记在下面的欠账里。

#### 落点（五层，比 HTTP 面那一竖多出两个文件）

| 层 | 文件 | 职责 |
|------|------|------|
| 转换层 | `internal/service/opportunity_convert.go` | 量程校验 → LTC 闸门 → 幂等反查 → 分配 → 落库 → 审计；全局登记点 |
| 分配层 | `internal/service/opportunity_assign.go` | 三条规则的顺序裁决 + 每次分配自带解释（规则名、候选集、每人负载） |
| 名单层 | `internal/service/opportunity_roster.go` | "谁在册"的生产实现（`sales_events` / `sales_profile`） |
| 接缝 | `internal/service/lead_mining.go: persistLead` | 线索落库之后那一跳，新建支与更新支各一处 |
| 装配 | `internal/app/opportunity_wiring.go` | `InitOpportunityRuntime` 一次登记**两半**（读口服务 + 转换器） |

#### 判据一：闸门顺序是"量程 → 阶段 → 双阈值"，不是三个并列 if

1. **入参量程先判，越界一律 `ErrOpportunityInputInvalid` 且不进闸门**。本仓有**两个都叫
   `confidence` 的数**：`ClueScore.Confidence` 是 0–100 的维度覆盖度，`LeadJudgement.Confidence`
   是 0–1 的模型把握。量程错位如果被闸门判成"不达标"，症状是"每条线索都不达标"而那道闸门
   每天都绿 —— 比"错放"隐蔽得多，所以这里**硬拒而不归一**（归一就是把两个不同量程的事实合并成
   一个没人能追溯的数）。`lead_score` 同理，界外（负数、>100）直接拒。
2. **阶段闸门在阈值之前**：`cfg.StageActive(LTCStageOpportunity)`（nil 安全）先跑，再跑
   `cfg.LeadQualified(leadScore, confidence)`（nil **不**安全）。这个顺序不是风格，是"配置缺失时
   不能 panic"与"不能凭猜放行"两件事的唯一交集。
3. **配置读不到按"关"处理**：`DegradeReason != ""` 时那份是回落默认（默认全关）。把它读成
   "默认值挺宽松"，等于库里存储故障的当晚凭空多出一批商机。
4. 拒绝必须说清是**哪一道**拦的：`LTCReasonMasterOff / StageOff / UnknownStage / Degraded`
   透传配置层，本层只补两个配置层表达不出来的形状 —— `threshold_unmet`（门开着但分数不够）与
   `already_converted`（这条线索已经有商机了）。前者与后者的动作在值班手里是两件事：一个是
   "运营该调阈值"，一个是"该有人去推进那一条已存在的商机"。

#### 判据二（AC②）：转换那一步对 `clues.is_opportunity` 零写入

`model/opportunity.go` 的 `clue_id` 注释在 T-P4-01 写的是"不建索引：今天没有由线索反查商机的读方，
反查由 `clues.is_opportunity` 承担"。**本卡把这句判断就地改掉了**，理由是实测出来的两件事：

- 幂等键就是这条反查（同一条线索第二次投递必须先知道"已经转过了"），而 `is_opportunity` 是一个 0/1，
  它答不出"转成了哪一条"；
- 那一列的语义是挖掘侧按 `intent_score` 顺手打上的**热度标记**（`lead_mining.go` 与
  `lead_miner_unified.go` 两处都在写），与"已经变成商机"根本不是同一件事。两处都写 ⇒ 同一列承载
  两个判据、取值时刻还不同 ⇒ 旧读取方（列表筛 `COALESCE(is_opportunity,0)>=1`）看到的人群会凭空换一批。

这条"零写入"不是靠读代码保证的：`TestConvertNeverWritesClueIsOpportunity` 有**两臂**（线索原本
`is_opportunity=0` 与原本 `=1` 各一次），每臂都在转换之后**从库里读回那一行**比原值，并顺带钉住
`intent_score` 与 `level` 也没被"顺手"改掉 —— 只断 `is_opportunity` 的用例会放过"转换层开始替挖掘侧
打热度标记"这件事，而那正是本判据要防的那一步。

于是反查走 `opportunities.clue_id`，索引建了，但建的是**部分**唯一索引
（`ON opportunities (clue_id) WHERE clue_id <> ''`）：空串是本表的合法常态（手工商机就没有来源线索），
不带谓词的唯一索引会让第二条手工商机插不进去。GORM 标签表达不了 partial，所以它不在标签上，
而在 `internal/pkg/db` 的 `postMigrateOpportunityClueUniqueIndex()` —— 一条**只 Warn 不 panic、
且绝不清数据**的启动钩子，用例同时钉住"存量重复不会把启动路径断掉"和"建索引失败不许把表清空"。

#### 判据三（AC③）：三条规则的顺序是判据，分配结果是返回值而不是日志行

`customer_owner`（同一客户已有归属销售则沿用，哪怕他正忙到冒烟）→ `least_loaded`（在册销售里
**只数 `status=open`** 的在办数，平票按 `SalesID` 字典序，不随机）→ `no_roster`（真的一个在册销售
都没有 ⇒ 商机照样建，只是暂时无归属）。三条规则各有一句"为什么压过下一条"，写在文件头而不是
散在 if 里。

**与规则三相对的是故障**：名单读不到、负载读不到，一律原样上抛，绝不"退到规则三"。
把"我们不知道有没有人"伪装成"确实没人"，后果是一批商机带着空归属与一条**从未发生过的**
"名单为空"记录落库 —— 那条解释比故障本身更难排除。这条判据不是假设：本卡的名单适配器
（`opportunity_roster.go`）与它的同名先例（`SalesEventStatsService.allProfiles`）**唯一的差别就是这一条**
（那边把读失败吞成 nil，看板少几行没人出事；这边吞掉就直接喂给分配决策）。

**可解释性落成结构而不是文案**：`OwnerAssignment` 同时带出规则名、参与比较的候选集、每人被比较时的
负载数，并且候选集**必须按 `SalesID` 升序** —— 排序在这里是判据不是美观，因为 `leastLoaded` 靠
"先遇到者胜"实现平票裁决，那前提是输入已有序。这一条被变异验过（把 `sort.Strings` 换成倒序排，
`TestAssignOwnerCandidatesAreStableAndSorted` 报的是"候选第 0 位是 gamma"而**不是**"没人被选中"，
即它抓的是裁决前提，不是抓一个恰好可见的表面）。

**名单真源**：`sales_events` 里 `event_type='sales_profile'` 的事件，与业绩看板同一处。另一条候选
`sales_personas` 在本仓**零写入方**，拿它当名单会让分配永远走到 `no_roster` 那一支 —— 那是一支
"看起来正常工作"的错。代价一并记下：本仓没有停用类事件，所以"在册"只能定义为"注册过档案"，
离职销售不会自动出名单。这一条登记为边界而不是现场补一列 `status`：那个列今天没有任何写者，
只会被 AutoMigrate 建成恒为零值的死列。

#### 幂等：反查在写之前，且"读不到"不等于"没转过"

`GetByClueID` 空 `clueID` **直接报错而不是返回 nil** —— 本表允许 `clue_id` 为空，拿空串去查会命中
"所有手工商机里的第一条"，那是一个看着合理的错误答案。读失败原样上抛：把"读不到"读成
"这条线索还没转化过"，一次数据库抖动就变成同一条线索的两行商机。

#### 审计：记系统身份，且审计失败不回滚转换

成功转换写一行 `operation_logs`（`module=opportunity_convert`）。两个选择值得记：
① `actor` 写在 `username` 而不是 `user_id` 上 —— 无人值守的动作没有一个对应的人，塞某个真人的 id
等于把系统的行为记成他的操作，而 `users` 里 `id=0` 那一行本来不存在，join 过去是空的；
② **审计写失败时转换仍然算成功**（`res.AuditError` 非空并由调用方 Warn）—— 反过来做就等于
"日志表抖一下，客户的商机就没建"，那是把可观测性设施抬到了业务事实源的位置。

#### 接缝：为什么那一跳在 `Create` 之后，以及为什么未装配时它不出声

`s.tryConvertToOpportunity(...)` 在 `persistLead` 的**两条分支各一处**，且都在线索行**已经落库**之后：
`Create` 失败就 `return`，绝不带着一个没有下家的 `clueID` 去建商机 —— 本表刻意不建外键（T-P4-01 的
裁定），这条断链谁都不会发现。这一处形状是这条接缝最容易只接一半的地方（更新分支没有 `Create`，
它的"已落库"是那条 `UpdateByID` 成功），所以两支各有一条用例、且各配一把"摘掉这一跳"的变异。

未装配 ⇒ `Debugf` 一行就返回，不 `Warn`：默认配置下阶段是关的，每条达标线索都打一行 Warn 会把
日志刷成噪声，而"未装配"是一次部署状态、不是每条目事件。转换报错 ⇒ 只 `Warn`、不重试、
不回滚线索：这条路径的失败处理若升级为重试，就要引入投递语义（谁负责重投、幂等窗口多长），
那是另一张卡的事，今天先保证**不静默**。

#### 装配：一个函数登记两半，`db == nil` 时两半一起清

`InitOpportunityRuntime` 现在同时 `SetGlobalOpportunityService`（HTTP 读口/写口用）与
`SetGlobalOpportunityConverter`（挖掘接缝用）。**两半必须一起清**：只清一半的话，路由对着 503 告警
继续答，而挖掘侧还在往一张没人读的表里写。刻意**不做惰性构造** —— 惰性建要走全局 DB 句柄，
而第一个调用方是挖掘 worker 的协程，那等于让"建句柄的时刻"由一条后台消息的到达时间决定。
本竖仍不加旗子（与 T-P4-04 同一理由：写的是新表的新行，不改动任何既有读写路径；真正的总闸
是 `ltc.config`，它在数据里、改了立刻生效，不需要再套一层进程启动期才读的 env）。

#### 本卡被真跑纠正的三处

- **测试夹具的保真度**：`miningClueCreateFails.Create` 的第一版直接返回 error 而不填 `c.ID`，
  于是"线索没落库照样转"那把变异会以**错得多的理由**被抓住（转换器在空 `clueID` 处就拒了，
  根本走不到"断链"那一步）。真因：`model.Clue.BeforeCreate` 在 INSERT **之前**就赋了 uuid，
  所以真仓库里一次失败的 `Create` 照样留下一个填好的 id。夹具照真行为改掉之后，那把变异才真的
  打在接缝上。
- **台账的 `callpat` 会被"清空那半"命中**：16f（转换竖的启动登记点）第一版接线数=2 —— 因为
  `SetGlobalOpportunityConverter(nil)` 那一行也算调用。收紧到 `\(conv\)` 之后接线数=1，
  摘掉真登记行才会红。这是"定义行/无关行自匹配 ⇒ 假绿"这个老形状在本仓的又一次现身。
- **两条变异退化成了构建红**（事件类型常量换成不存在的字面量、`sort.Reverse` 的切片拼接写法不合法），
  补成可编译的同量程语义变异（`model.SalesEventTypeOrder` / `sort.Sort(sort.Reverse(sort.StringSlice(out)))`）
  之后，红因分别是"查询事件类型 `\"order\"`"与"候选第 0 位是 `gamma`" —— **一条只在编译期红的变异
  不能算行为捕获**，它只证明类型系统挡住了拼写错误，不证明用例认得这个业务结论。

#### 台账：16b 翻 wired，另新增四行（现值 **47/53**，`check-unwired-assets.sh` rc=0）

原计划"16b 由本卡翻 wired"兑现了（`model.Opportunity{` 在 `internal/service` 里有生产构造点）。
另加四行，各自盯一把前一行看不见的刀：

| 行 | 盯的是 | 摘掉之后的症状 |
|------|------|------|
| 转换竖的启动登记点 | `SetGlobalOpportunityConverter(conv)` 在 `internal/app` 里有没有人调 | 端点、读口、路由全正常，只有挖掘侧永远不转，且**不报错** |
| 挖掘侧到转换层的接缝 | `conv := GlobalOpportunityConverter(` 在 `internal/service` 里有没有人调 | 装配、转换层单测全绿，真实线索落库不再产生商机 |
| 在册销售名单适配器的装配点 | `NewSalesEventRoster(` 在 `internal/app` 里有没有人调 | 分配永远走 `no_roster`，每张商机都写着"在册销售为空" |
| `clue_id` 部分唯一索引的启动调用点 | `postMigrateOpportunityClueUniqueIndex(DB)` 在 `internal/pkg/db` 里有没有人调 | AutoMigrate 只建普通索引 ⇒ 同一条线索可以转出两行 |

五行（16b + 上面四行）**逐行反向验过**：把各自那一行的调用改成同文件里的非法符号 ⇒ 台账 rc=1 且
该行报"接线数=0"，控制组 rc=0，跑完工作树 `git status` 无残留（变异用 cp 备份 + md5 比对写回）。
`defpat`/`callpat` 的写法都避开了定义行与"清空那半"自匹配。

#### 本卡的欠账（登记，不当作已完成）

- **HTTP 手工"一键转商机"入口未交付**：今天唯一的转换方是挖掘链路自动那一跳。运营拿着一条
  达标线索想手建商机，仍然没有口。
- **`lead_miner_unified.go` 那条通用挖掘链未接本接缝**（刻意）：它只有关键词派生的 `intent_score`，
  **没有 confidence 生产者**。给它喂一个数才能过 C5 的双阈值，等于凭空造一个业务判据；
  等 bridge/统一链有真的把握度信号再接。
- **`ltc.config` 的 `win_probability` 阈值仍然零读者**（与 §4.16.3 同一格，本卡只接了
  `lead_score` 与 `confidence` 两个阈值）。
- **赢单概率与漏斗的 stage 读者未接**：T-P4-06。
- **`actor`（谁做的这次跃迁）不落表**：本卡的审计行记的是"系统转的"，而状态机那五次写入口
  仍无操作者列 —— 与 §4.16.2 那格同源，下家未指派。

#### 实跑口径（分树记，别混读）

本卡验收数字一律取自 **`--shared` 克隆、HEAD `478ef1c4`**（克隆里没有并行会话的未提交文件，
所以这才是"我这一个提交自洽吗"的答案）：`go build ./...` 与 `go vet ./...` rc=0；
app 234 / repository 1171 / router 177 / pkg-db 24 / controller 913 / service 4305，
`fail=0`（service 另有 `skip=3`，实跑点名：`TestAIAgent_AssetBundleBinding` 与 `TestAIAgent_FullChain`
跳过在 `login()` 那句"集成测试跳过：user-server 未运行"上，`TestPlatformAccountService_Login`
跳过在 chromedp/真浏览器门上 —— 三条都是既有环境门，与本卡无关）；**`TZ=UTC` 复跑同数同绿**。
**计数口径**：`-test.v` 下顶层与子用例一并计数（`^--- PASS` 加 `^    --- PASS`），
所以这些数**不能**与 §4.16.3 那组"顶层口径"的数（app 164 / router 150 / service 3509）直接比大小。

`-race` 五腿：app / router / pkg-db / repository 四腿 **race=0**；service 腿一共跑了**五次**，
结果分别是 `2 race / 2 fail`（门禁脚本那一轮）、`race=0 / fail=0 / pass=4305`（复跑第一轮）、
`1 race / 1 fail`（复跑第二轮，红的是 `TestCreateSession_AllowDifferentPlatform`，
`pass=4304`）、`1 race / 1 fail` 同一条用例（补跑第一轮，607.894s）、
`2 race / 2 fail`（补跑第二轮，602.926s，第二条红的是 `TestCreateSession_AnonymousUser`）——
**同一棵克隆、同一个 HEAD、五种跑法三种结局**，就是 §4.16.3 末段那句"命中哪一队取决于调度"的又一次实测。
补跑多出来的那条受害用例同时**否掉了"只有 `AllowDifferentPlatform` 这一条有问题"的读法**：
受害名不唯一 ⇒ 这是"谁恰好排在写句柄那条之后"的问题，不是某条用例自己的问题。
抓到的栈与上一卡逐帧相同：写方 `db.SetTestDB()`（`customer_session_blacklist_test.go:24`），
读方 `repository/system_config_kv.go:32` ← `office_hours.go:45/81/97/107` ←
`customer_service_plus.go:487` 的 `MaybeSendAwayReply.func1`，**本卡八个文件一个都不在里面**。
本卡不修，同一理由（修法要动测试底座、且会撞上并行会话正在改的同一批文件）。同一口径事实再记一次：
**`-race` 不在本仓任何门禁脚本里**，这条红不会被默认门看见。

**工作树**（同日，含并行会话未提交的文件）另有一组结果，且**红因不在本卡**：`internal/service`
全量 485s 一处 FAIL —— `TestValidPlatform_Unsupported`（`message_hub_test.go:64`），红因是一处
**未提交**的 `+ "wechat": true` 落在 `ValidPlatform` 里，而那条用例仍把 `wechat` 列在"应判非法"的名单中。
`check-architecture.sh` 在工作树报 1 处 `[L4] service 直接调 db`，同样来自未跟踪的
`internal/service/dingtalk_media.go`；`check-secrets.sh` 在工作树的 3 处命中也全在未跟踪的
`webhook_batchc_*` / `webhook_batchg2b_*` 测试文件上。**三处都不代修、不代提交**，
只在台账外登记：同一棵克隆里 `check-architecture.sh` 是 rc=0 的，这就是"红不属于本卡"的证据。

**门禁（克隆内）**：`check-architecture` rc=0 / `check-unwired-assets` **47/53** rc=0 /
`check-enum-consistency` rc=0（3 警告为既有）/ `check-date-bucket-tz` rc=0（21 处持平）/
`check-doc-consistency` rc=0（3 警告）/ `check-feature-doc` rc=0 /
`audit-cross-package-ports` Errors 0 / Warns 6。
`check-secrets.sh` 在克隆里是 **rc=2**（"找不到 `<克隆>/.env`"）—— 那是**环境前提不成立**而不是通过，
本卡的凭证面判据以工作树那次为准（本卡文件零命中）。

**变异电池（18 把，全部行为捕获）**：`捕获=18 / NOT-CAUGHT=0 / RED-BUILD=0`，
覆盖名单适配器 4 把、分配器 4 把、转换层 5 把、挖掘接缝 4 把、库级索引 1 把；
每把都 `cp` 备份 + `md5` 比对写回，还原失败会自报。第一版有 **2 把退化成构建红**
（见上文"本卡被真跑纠正的三处"第 3 条），补成可编译的语义变异后才计入这 18 把。
台账那五行另有独立的反向脚本（见上文台账段）。

### 4.16.5 漏斗的第五段：把 `opportunities` 接进实时聚合，以及"按位置取段"为什么会静默换口径（T-P4-06）

> 卡面（`docs/replan-2026-09/新规划任务清单.md:234`）：「漏斗接入商机阶段：扩展 **ops 实时聚合器**
> （R-4 裁定），**不写僵尸表**」。AC① `GET /conversion-funnel` 返回含商机阶段；AC②
> `conversion_funnels` 表写入路径仍为零（守 D-9）；AC③ seed 假数据不影响真实视图。
> 依赖 T-P4-03（商机服务）与 T-P2-03（阶段词表 + 黄金用例），二者都已在册。

#### 落点（ops 侧四改三增；前端一改一增、台账一行、文档两篇，全卡共 12 路径。没有一处写那张表）

| 文件 | 改了什么 |
|---|---|
| `ops/repository/conversion_funnel.go` | 加 `CountOpportunitiesByTimeRange`（**唯一**碰 SQL 处）；`liveFunnelStages` 末尾加 `StageOpportunity`，`reservedFunnelStages` 清空 |
| `ops/service/conversion_funnel.go` | `BuildFunnel` 追加第五段；`GetStageDetails` 加 `case StageOpportunity`；两条腿各自把取数失败打成 `Warn` |
| `ops/repository/conversion_funnel_stage_test.go` | 词表三条改期望（产出五段、保留清单为空、副本用例适配空表） |
| `ops/service/conversion_funnel_baseline_test.go` | 黄金串加第 5 段、夹具补 4 行商机、AC③ 新增一条"演示表灌 99999 读数不变"、新增一条"表没了仍回五段" |
| `ops/repository/conversion_funnel_opportunity_test.go`（新增） | 取数层三条：两侧窗口都读到差值、往演示表种巨量假行真实源不动、`err` 必须上抛（不吞成 `(0, nil)`） |
| `ops/service/conversion_funnel_opportunity_log_test.go`（新增） | 两条告警各锁一条：删掉 `Warnf` 必须有用例红 |
| `ops/controller/conversion_funnel_http_test.go`（新增） | AC①/②/③ 各一条打到真实 gin 路由 + `{code,message,data}` 外壳 |

（表内"四改三增"只数 `internal/ops/` 下的 Go 文件；`user-web/src/views/conversionFunnel/List.vue`
与 `user-web/tests/unit/conversionFunnel_summary.test.js`、`scripts/check-unwired-assets.sh`
两行台账、以及本仓这两篇文档，合计 12 个路径 —— 以 `git show --name-status d52434c2 cc065b02`
为准：4 个 `A` + 8 个 `M`。）

**这条端点上原先一条 controller 用例都没有**，而 AC① 说的是"接口返回"。`parseTimeRange`
只认 RFC3339、外壳的 `data` 带 `omitempty` —— 两处任何一处坏了，service 的黄金用例全都不会红。

> **本卡文档里被真跑纠正的一处（而且是第二次读码不足）**：上面这段的初稿还写着"路由挂在
> `/conversion-funnels` 而非 `/conversion-funnel`（卡面写的是后者）"，据此把卡面判成笔误。
> **读全之后：两条都在。** `internal/router/frontend_aliases.go:409-414` 用 `doReg` 同时注册了
> 复数与单数各三条（`/conversion-funnels`、`/conversion-funnels/list`、`/conversion-funnels/stage`
> 与 `/conversion-funnel`、`/conversion-funnel/list`、`/conversion-funnel/stage`），
> 所以 AC① 那句 `GET /conversion-funnel` **字面就能命中**，卡面没有错。
> 我错在拿了两条弱证据当结论：① 只看到自己测试里手写的那两条路径；② `api-inventory.md` 的
> **后端路由段里根本没有这两组路径** —— 而它漏这两组是**两种不同的漏法**（本卡现场读脚本读出来的）：
> 别名走 `doReg("GET", path)`，方法名是**参数**，脚本抽后端用的正则 `\.(GET|POST|PUT|DELETE|PATCH)\("`
> 压根不匹配；商机/待办那两族走 `controller` 里的 `g.GET("/rules")` + `router.Group("/opportunity")`，
> 正则匹配得上却只拿到**相对片段**，组前缀写在另一个文件里，全文拼不出 `/api/opportunity/...`
> （实测 `grep -c -i opportunit api-inventory.md` = **0**（**全文**都没有，因为前端还没调过这套 API）；
> `human-task` 出现 2 次，但两条都在**前端调用段**（1426、2241 行），后端路由段同样为零）。
> ⇒ 连带更正本卡"实跑"段那句"api-inventory 生成物无 diff ⇒ 本卡未动路由"：**它无 diff 只说明
> 前端调用与 `.GET("全路径")` 那一族没变**，别名与"控制器内相对路径"两族本来就不在它的眼界里，
> 这条证据比初稿写的弱一档。脚本自身在头部已声明它是非阻塞快照、真正的门是
> `audit_api_contract.py --strict`，本卡不动它，盲区登记在此。

#### 判据一：这一段答的是"这一期进了多少单"，不是"此刻各格停着几单"

`opportunities` 上有两个能数出"商机数"的东西：`created_at`（进表时间）与 `stage`（当前格）。
取前者，三条理由：① 其余四段全是**流量**口径（某窗口内发生了几次），混一格**状态**读数进去，
逐段相除算出的"阶段转化率"就失去意义（§4.11 那条 `_NonMonotonic` 登记的正是流量口径下的
非单调，状态口径连非单调都不是同一件事）；② 模型的 `idx_opp_created` 注释已经点名"要按它切
时间窗"，索引就在这一列上；③ 后者需要为看板新增一套"按 stage 分组"的口径，那是 T-P7-02
回款域的活，不是本卡。

口径写在方法名的注释里，不只写在代码里：`CountOpportunitiesByTimeRange` 的注释第一句就是
"统计时间范围内**新建**的商机数"，并明确"答的不是此刻各格停着几单"。

#### 判据二：错误上抛到仓储层为止，服务层降级为 0 + 一条 Warn

仓储那条新方法把 `err` 原样返回（可单测、可反证）；服务层两条腿各自吞成 0 并打
`logger.Warnf`。这与前四段"吞成 0 且不出声"不同，差别不在洁癖而在**blast radius 与歧义度**：

- 外抛会让整份 200 变 500，一份故障挪走四段的可见性（`_OpportunityLegFailureKeepsTheOtherFour`
  把这条锁住：删表之后 `err` 必须是 nil、五段必须在、其余四段数字必须是 4/3/1/4）。
- 但"商机=0"这一格**独有一种歧义**：T-P4-05 之后这张表只有一个生产者（挖掘达标那一跳），
  所以 0 既可能是"真没转化出商机"，也可能是"表没建起来/接缝没装配"。前四段答 0 时没人会拿它
  做经营决策，这一段答 0 会被读成"我们线索转不动"，而那条结论会一路走进 P8 的归因。
  告警是把这两种来路分开的唯一手段，因此它是行为而不是日志。

于是"未告警即视同成功"（验收口径 R-①）在这里落成一条用例：
`_OpportunityLegFailureIsWarned` 抓 stdout（照 `internal/app` 那套：换 `os.Stdout` 之后**必须**
重建全局日志器，`GetLogger` 会连当时 stdout 一起缓存），断两条告警各恰好 1 条、级别 `warn`、
内容含失败表名，且返回值不受影响。删任一条 `Warnf` 都把它打红（变异 M3/M7）。

#### 判据三：第五段追加在末位，因为"下标即契约"的读者不止一个

词表把 `opportunity` 加在末尾而不是插在 `session` 前，是为了一句不改的话：看板按下标绘制。
但**加一段本身就是口径变更**，因为有两处读者在按位置取段：

| 读者 | 位置式取法 | 加段之后实际读到 |
|---|---|---|
| `conversion_funnel_baseline_test.go` 的 `_NonMonotonic` | `Stages[len-1]` | 从会话（rate 400 / drop −300）变成商机（75 / 25）——**用例仍绿但断言的已不是它登记的那件事** |
| `user-web/.../conversionFunnel/List.vue` 摘要区 | `stages[0]`、`stages[stages.length-1]` | 标题「转化量(会话)」显示商机数、「端到端转化率」从 访问→会话 变成 访问→商机 |

两处都改成**按阶段名取**。后端那条的改动没有争议；前端这条要说清保留了什么口径：
三块 KPI 仍是 `访问` 与 `访问→会话`（与服务端 `total`/`conversion` 同向，那两个字段本卡没动、
黄金用例也新加了断言钉住），**没有**改成"访问→商机"。改成后者是一次产品口径变更，
不在本卡，登记进下面的欠账。

前端这条不是推断：把 `List.vue` 反向打回位置式取法（`cp` 备份 + md5 校验还原），
`tests/unit/conversionFunnel_summary.test.js` 五条里 **4 红 1 绿**，红因就是那两个数字——
`AssertionError: expected '1' to contain '2'`（把商机的 1 显示成了"会话数"）与
`expected '10%' to contain '20'`。绿的那条是"阶段明细列出第五段"——明细用 `map` 生成，
本来就不按位置取，这正说明**只有摘要区**有这把刀。

#### 本卡被真跑纠正的一处（写下来是因为它会让"登记"变成假安全）

台账最初把这条腿登记成**一行**（callpat = `CountOpportunitiesByTimeRange\(`），
反向验证时把汇总腿整个删掉，实测 **rc=0、行仍报 WIRED**：`wired` 只要求命中 ≥1，
而 service 里有**两个**消费点（`BuildFunnel` 与 `GetStageDetails`），删一个还剩一个。
那等于登记了一把永不变红的锁。按消费点拆成两行之后，四种删除口径
（删汇总腿 / 删详情腿 / 都删 / 定义改名）分别实测 rc=1 / rc=1 / rc=1 / rc=2。
拆行的代价也写进注释：两行的 callpat 各自锚在赋值左侧的局部变量名上（同包内两次调用文本
几乎相同，不锚名字分不开），**改局部变量名会让对应行报"未接线"**——方向是变红不是变哑，
红了回来改这两行即可。

#### 交付后复查再纠正两处（同一张卡的第三次、第四次；都在"自己写的登记"上）

双推之后按「彻底深入检查」重跑了一遍本卡，抓到两处：

1. **上面的落点表少登记一个文件，标题的计数也因此错**。`ops/repository/conversion_funnel_opportunity_test.go`
   （新增，三条取数层用例）当初既不在表里，"四改两增"这个数也不对 —— 实数按
   `git show --name-status d52434c2 cc065b02` 是 **4 个 `A` + 8 个 `M`**，ops 侧是四改三增。
   漏登记的代价不是"表格不齐"，是**下一次读这张表的人少找一个测试入口**：那三条里有一条
   正是"往演示表种巨量假行、真实源读数不动"，即 §4.11 判 D-9 的仓储侧证据。
2. **那个文件根本没通过 gofmt，而 gofmt 是真门**。`make fmt-check`（CI `Static gates` 里那一步，
   判据与本地同源）在 HEAD 上报两条红，其中一条就是这个新文件：包注释里 `①②③` 三条用了
   **四空格续行**，Go 1.19+ 的 gofmt 把注释里缩进 ≥4 空格判成代码块并重排。修法是把续行顶格
   （不跑 `gofmt -w`，它会把这段散文重排成 tab 代码块，读起来更差），改后
   `gofmt -l` 对该文件空、`git diff` 该文件**非注释改动行数 0**、`ops/repository` 包
   重跑 76 条全绿。另一条红 `internal/controller/wechat_batchf4_m01_inbound_test.go`
   属并行会话未提交文件，不代改。

⇒ 这两处合起来是一条口径：**"新增文件"这一步的门不是"测试绿"，是 `make fmt-check` 也绿**。
本卡的验证清单里当初列了 build/vet/六门/-race，唯独没列 fmt-check —— 因为它是 `make` 目标、
不在 `scripts/*.sh` 那一排里，于是**门的清单按"脚本目录"枚举就会漏掉按"make 目标"存在的那道门**。

同一轮复查里**复核成立**（不是推断，逐项重跑）：`ops/service`+`ops/repository` 在
`TZ=Asia/Shanghai` 与 `TZ=UTC` 各 **455 / 0 fail / 0 skip**、`ops/controller` **106 / 0 / 0**；
命名六门在 `--shared` 克隆（HEAD `cc065b02`，干净树）**六条全 rc=0**，同一批脚本在工作树是
**五绿一红**（红＝`check-architecture`）；那处红的归属这次是**直接证据**而不是"克隆转绿"的间接推断 ——
`git status --short` 显示 `internal/service/dingtalk_media.go` 是 `??`（未跟踪 ⇒ **HEAD 里没有这个文件**，
红只可能来自工作树未提交内容，而 `git log -- <该路径>` 亦无任何提交）。
`check-secrets.sh` rc=1 的三处命中文件名与提交清单里 12 个路径求交集为**空**（`comm -12` 实算，
不是"打印列表里没看到"）。台账 49/55、项17 两行各自 `接线数=1`；
`api-inventory.md` `grep -c -i opportunit` 全文 0、`human-task` 两条都在 1426/2241 的前端调用段；
`闭环完成率` 全仓 `*.go` 仍只命中 `internal/model/opportunity.go:85` 一句注释、前端零读者；
`frontend_aliases.go:409-414` 逐字重读确为单复数各三条；`List.vue` md5 与提交时一致
（`f31a7610a61da8143def69e4e839cfc7`）、`vitest run` 14 文件 / 238 用例全绿。

#### 台账：新增两行（现值 **49/55**，`check-unwired-assets.sh` rc=0）

| 行 | 盯的破坏 |
|---|---|
| 17a `漏斗汇总腿的商机取数调用点` | 摘掉 `BuildFunnel` 那一次调用 ⇒ 响应仍五段、名字全对，只有商机数永久 0 |
| 17b `漏斗详情腿的商机取数调用点` | 摘掉 `GetStageDetails` 的分支 ⇒ `stage=opportunity` 退化成"未登记阶段"的空名 0，与真 0 无法区分 |

两行都是 `defpat` 在 `internal/ops/repository`、`callpat` 只看 `internal/ops/service`
（定义自身不进账）；反向验证的四个删除口径见上一节，`expect` 记 `wired`（防回退）。

#### 本卡的欠账（登记，不当作已完成）

- **端到端转化率要不要走到商机**：三块 KPI 与服务端 `total`/`conversion` 现在都停在 访问→会话。
  商机已是第五段，"转化率"要不要改成末段是产品决策；改了会同时动服务端两个字段与前端两块卡片。
- `GetStageDetails` 的 `avg_duration_seconds` 与 `top_sources` 对商机段仍是零值 —— 详情里
  "商机平均停留多久""商机来自哪几个账号"两条都要 `sales_events` 的口径（T-P7 域）。
- 阶段非单调的老问题继续存在，且现在多了一格可能 >100%（本卡夹具里就是 会话 4 → 商机 3 与
  会话 1 → 商机 2 两种都测过）。`_NonMonotonicStagesBaseline` 只登记会话那一格。
- 演示表 `conversion_funnels` 仍未下线：本卡的 AC② 只证明"新代码零写入"，删表判据三条
  （§4.11）一条都没消掉。
- 真机/浏览器那条腿本轮**没跑**：8204 上活着的是 `bin/user-server.r39`（9-19 起的旧二进制，
  不含本卡改动、也不是本会话启动的），为它另起一套前后端会污染并行会话的证据，故 AC① 的
  HTTP 证据取自 `httptest` + 真路由 handler + 真 PG。看板截图口径待下一次真机回归补。

#### 实跑（分树记，别混读）

主工作树未提交态（HEAD `39be6824` + 本卡改动）。**同一棵树**上：

- 变异电池·第一轮（`-test.v` 计 `^--- PASS` 与缩进的子测试行）：控制组 **455 条全绿 / 0 skip**；
  M2 读演示表 red=8、M3 删汇总告警 red=1、M4 词表调序 red=2、M5 详情不认商机名 red=1、
  M7 删详情告警 red=1。**M1、M6 第一版是坏的**（ perl 替换把整行留成了半截语句 ⇒
  `[build failed]`，`--- FAIL` 计数为 0 看着像"没抓到"）—— 修变异不修期望。
- 第二轮（同两包）：M1r 摘掉取数调用点 red=4（黄金串、窗口过滤、"演示表灌 99999 读数不变"、
  以及那条告警用例全红）、M6r 仓储吞错误 red=2（仓储自己那条 + 服务侧告警那条，跨层同因）。
- 第三轮（只跑 controller，控制组 **106 条全绿**）：H1 摘汇总腿、H2 摘详情分支、
  H3 改读演示表三条各打红 `TestConversionFunnelHTTP_FunnelContainsOpportunityStage`；
  H4「取数时顺手往演示表写一行」打红 `TestConversionFunnelHTTP_DemoTableStaysEmpty`。
  ⇒ 新加的三条 HTTP 用例不是摆设，每条都被对应的破坏抓到。
- 三轮共 11 次变异，每次 `cp` 备份写回、收尾比 md5：全部一致，`MUT` 残留 0。
- 受影响包：`go test ./internal/ops/...` 四包 ok（controller/model/repository/service）；
  `TZ=UTC` 与 `TZ=Asia/Shanghai` 各跑一遍三包全 ok（日期分桶门 `check-date-bucket-tz.sh` rc=0，
  命中数与基线同为 21，本卡未新增分桶写法）。
- `go build ./...` 与 `go vet ./...` 各自单独取 rc：双 0、输出 0 字节。
- `-race` 跑 ops 三包：`DATA RACE` 计数 0，三包 ok（含新加的 stdout 管道用例；该包内
  `t.Parallel()` 计数 0，且它是唯一换 `os.Stdout` 的用例，还原放在 `t.Cleanup` 里）。
- 其余门：`check-enum-consistency`、`check-doc-consistency`、`check-feature-doc`（失败 0）、
  `api-inventory`（生成的 `api-inventory.md` **无 diff** ⇒ 只支持"前端调用与 `.GET(` 全路径那一族未变"，
  比初稿写的弱一档，见上文"被真跑纠正的一处"）、
  `audit-cross-package-ports` rc=0（Errors 0 / Warns 0）全绿；
  `check-architecture` rc=1 的**唯一红是并行会话的** `internal/service/dingtalk_media.go:191/204`
  （`[L4] service 直接调 db`），与本卡无关、非本会话引入。
  `check-secrets-artifacts.sh` 需要 `<env文件> <产物目录...>` 参数，单独跑必 rc=2（用法而非门禁）。
- 前端：`vite build` rc=0；`eslint` 对本文件 0 error，且改动前后**同为 53 warning**
  （把 HEAD 版临时复制成同目录下的 `_baseline_tmp.vue` 跑了一遍，计数一字不差 ⇒ 本卡零新增告警；
  跑完即删）；`vitest run` 全量 **14 文件 / 238 用例全绿**（本卡 +1 文件 +5 用例；改前基线 13/233）。

---

### 4.17 SOP 开工名单的三张权威表：圈选读的是谁，以及"0 人"为什么必须有两种（T-P5-01）

> 卡面（`docs/replan-2026-09/新规划任务清单.md:257`）：「**N-2 动态人群圈选**：`sop_scheduler.go`
> 现只读静态 `customer_ids`，为空回退到 SOP 创建者本人（误伤风险）。改为按 `TriggerConfig`
> 的圈选条件（churn/RFM/segment/tag）实时取名单」。落点 M `internal/service/sop_scheduler.go`
> ＋ N `internal/service/audience_selector.go`（**不新建人群表** C7）。
> AC① 空配置**不再**回退到创建者（坏例锁定）；AC② 圈选结果规模有上限；
> AC③ 首轮 DryRun 产出名单预览，人工确认前不外发（RK-6）。

#### 落点（四改两增，共 6 个路径；没有任何一张新表）

| 文件 | 改了什么 |
|---|---|
| `internal/service/audience_selector.go`（新增） | `AudienceConfig` / `AudienceSelection` / `AudienceSelector.Select` / `parseAudienceConfig`，上限常量与三类 reason |
| `internal/service/audience_selector_test.go`（新增） | 10 条用例：三源各一条（segment / tag / churn）、"死源不等于空结果"一条（2 子）、segment 侧诊断一条（3 子）、交集一条、上限夹取＋截断一条、无条件一条、报错上抛一条、配置形状解析一条 |
| `internal/service/sop_scheduler.go` | `tryExecute` 的名单来源换成 `resolveTargets`（删掉回退创建者那三行）、新增 `recordAudiencePreview`、本轮额度截断 |
| `internal/service/sop_scheduler_test.go` | 追加 9 条（原 15 条 → 现 24 条）：AC①②③ 七条 ＋ 本轮额度截断、预览跨 tick 存活各一条 |
| `internal/repository/customer_tag_assignment.go` | 加 `ListCustomerIDsByTag`（读方法，带 total）＋ `...WithDB` 构造器，六个既有方法改走 `r.database()` |
| `scripts/check-unwired-assets.sh` | 未接线台账加**项18 三格**（防回退登记，见下文「台账」小节）：49/55 ⇒ **52/58** |

#### 三个源、四道判据

| 条件键 | 读的表 | 排序（决定截断时留下谁） |
|---|---|---|
| `segments` | `customer_rfms.segment`（`determineSegment` 是唯一写入口，五值词表见 `model.RFMSegment*`） | `composite_score DESC, monetary_total DESC` |
| `tags` | `customer_tag_assignments.tag` | `created_at DESC` |
| `max_p_alive` | `churn_scores.p_alive` | `p_alive ASC` |

- **交集，不是并集**。勾了 `champion` 又勾了 `vip`，语义是"既是 champion 又打了 vip 标"；
  并集会让规模变成两拨人之和，而规模正是 AC② 要夹的那个东西。所有条件都会跑完再交，
  不因前面空手而提前 return —— 否则"另一个条件其实也没人"这条信息被藏掉。
- **`0 人` 有两种，必须分开报**：`no_match:<条件>`（改条件）与 `source_empty:<源>`（去查数据源）。
  这不是洁癖：`churn_scores` 的统计源 `defaultChurnStatsQuery` 现为 `return nil, nil`
  （`churn_score_job.go:115-117`），而周批**是装配着的**（`cmd/api/main.go:353`），
  `ComputeAll` 读到空统计就自己打一行"本轮空跑"退出、**一行都不写**。
  于是 churn 条件在真实环境永远圈不到人。如果只回"0 人"，运营读到的是"我条件写得太严"，
  真实原因是"这个域还没有生产者"—— 两种处置动作完全相反。
- **上限要夹两次，各防一件事**：`AudienceConfig.limit()` 把配置值夹到 `MaxAudienceLimit=500`
  （防一个手滑的 limit 把一轮变成几千条执行）；`tryExecute` 再按 `maxRunningPerSOP` 的**剩余额度**
  截一次（防"阈值只在上轮已跑满时才生效"—— 原实现里 49 在跑 + 名单 3 人 = 52 条并发）。
  夹取用 `Truncated` 留痕。
- **`ListBySegment` 的 `pageSize` 是个陷阱**：`pageSize>200` 会被它**静默改成 20**
  （`repository/customer_rfm.go:65-66`），所以"上限 500"绝不能直接当 pageSize 传，
  必须按 200 一页翻。第一次写成直传时，用例的 501 行夹具会拿到 20 而不是 500。

#### AC③ 的落法，以及它与卡面差在哪（不许声称复用了没调的代码）

卡面写「首轮 **DryRun**（复用 `proactive_reach.go` 既有能力）」。实测这条链路上
**没有 `ProactiveReachService` 可调**：调度器只做一件事 —— 建 `SOPExecution`；真正的出域发送发生在
执行体的工具节点里，那条腿的闸门是 T-P5-03 的范围。`ProactiveReachRequest.DryRun` 也存在，
但 `ProactiveReachService` 只在 `internal/app` 装配、无全局入口，调度器拿不到它。
⇒ 本卡实现的是 AC③ 的**语义**而非那句**调用**：`audience` 未经 `audience_confirmed` 时
零开工（`SOPExecution` 一条都不建，因此下游根本没有可发送的东西），名单写回
`trigger_config.audience_preview`（`generated_at / count / customer_ids / truncated / reasons`）。
这条偏差连同理由登记在此，比在 FEATURES 里写一句"已复用 DryRun"可信。

人工确认的通路是**既有**的 `PUT /sop-agents/:id`（`frontend_aliases.go:202` 与
`service_routes.go:300`，admin）：`sop.go:253` 把 `req.TriggerConfig` 整体覆盖回库。
⇒ 两个必须知道的后果：① 确认时要**整份回传**（`Update` 是覆盖不是合并，只塞一个
`audience_confirmed` 会把 `audience` 块 itself 抹掉）；② 任何一次不带 `audience` 的更新会让圈选条件
消失 —— 方向是"零开工"，不是"回退到某个人"，这个不对称是本卡故意留的安全侧。

#### 取数层的 customer_key 语义未证（接生产者前必须先对齐）

`churn_scores.customer_key` 被本卡**直接当 customer_id 用**。这个映射在仓库里**无法核对**：
生产者既然是桩，就没有一行真实数据能证明它写的是 `customers.customer_id` 而不是 oneid / 渠道键，
而列宽也各说各话（`varchar(120)` vs `customer_id` 的 `varchar(64)`）。
代码里那段注释就是留给接驳者的：先对齐，再启用 churn 条件。

#### 台账：项18 三格（`scripts/check-unwired-assets.sh`）

本卡给未接线台账加了三格，判据是"这一格能不能被 `internal/service` 自己的用例看见"：

| 格 | 锚点 | 摘掉之后的症状 | 用例看得见吗 |
|---|---|---|---|
| 18a | `cmd/api/main.go:291` 的 `InitSOPScheduler(` | auto 与 schedule 两类 SOP **一起**停摆，圈选根本不跑 | 看不见 —— 全仓 service 用例都就地 new 一个调度器，没有任何一条读得到 `main.go` |
| 18b | `sop_scheduler.go:77` 的 `audience:` 字段赋值 | 静态名单通道照常（那条用例照绿），只有声明 `audience` 的 SOP 永久零开工，症状是一行 Warn | 看得见，但**红得很晚**且不指名字段 —— 登记为防回退 |
| 18c | `audience_selector.go:150` 的 `s.tags.ListCustomerIDsByTag(` | **这一格与 18a/18b 不同类，登记时必须说清**：把那一行整个删掉会同时让 `TestAudience_SelectByTag` 转红（它断言打了 `vip` 的那两个人必须回来、没打标的 c-3 不许进来），所以它守的**不是**"静默停摆"。它守的是**换路** —— 谁把这一跳换成就地拼一条 `WHERE name = ?`，全部既有用例照绿，而 `ListCustomerIDsByTag` 就此变成零消费方的未接线资产，那正是本台账记的东西。附带一条读码事实：tags 腿**没有死源判据**（segment 腿查 `rfmHasAnyRow`、churn 腿查 `Count`，tags 腿空手只报 `no_match:tag=…`，:154-155）⇒ 换路之后它连"源死了"都说不出来，报出来的永远是"这个条件没人" | 看不见 —— 用例断的是"取回谁"，不是"经哪个仓储取" |

18c 与 §4.16.5 那一课同源，所以刻意不写成 `New.*RepositoryWithDB\(` 那种"三个源并一格"的宽式：
RFM 那副底座另有 `customer_360.go:57` 一个消费点，并格之后那一行永远命中 ≥1 —— 等于登记一把
永不变红的锁。三格各自锚在一个**只在此处出现**的名字上（18a 锚 `cmd/api` 里的调用、18b 锚结构体
字段的赋值左侧、18c 锚圈选器里的那一跳），`接线数` 实测三行恒为 1（见上面那次 rc=0 的复跑输出）。

#### 本卡刻意不交付的三件事（都带重启判据）

| 不交付 | 为什么 | 什么时候必须做 |
|---|---|---|
| churn 源的**生产者** | 桩在 T-P1-07 就是登记过的边界（`defaultChurnStatsQuery` 回 `nil, nil`），补它属数据管线不属圈选 | churn 条件要真正可用之前；在那之前 `source_empty:churn` 是**预期红**，不是缺陷 |
| 上限的**运营可配** | `MaxAudienceLimit` 是"误伤半径"，配置面一开就等于把 RK-6 的一半交给填表的人 | 外联投诉率（LTC-29）有 ≥2 周真实基线之后 |
| 条件的**并集**语义 | 交集规模 ≤ 每个源，并集规模 ≥ 每个源 —— 后者直接顶穿 AC② 想夹的那个东西 | 若产品明确要"任一命中即开工"，那是**改 AC② 的口径**（需分池＋逐池上限），不是在本函数里换个循环 |

#### 本卡的两处刻意欠账

1. **FEATURES.md 那条本卡没加**。不是漏了 —— 该文件在工作树里正被并行会话整表重写
   （`git status` 为 ` M`、`git diff` 的 32 行全在 webhook 渠道表上，与本卡零交集）。
   在它上面再叠一段圈选说明，等于把两拨未审阅的改动缝进同一个提交。
   ⇒ 这条功能登记**推后**到那份重写落地之后单独补一行；判据是"文档里出现的调用面，
   必须能在同一棵树里被 `grep` 命中"，而现在两条都还站不稳。
2. **两处 nil 守卫没有用例覆盖**：`resolveTargets` 的 `s.audience == nil` 与 `Select` 的
   `s == nil || s.rfm == nil ...`。**都没有生产构造路径可达** —— `NewSOPScheduler(svc, nil, ...)`
   同时把 `execRepo` 置 nil，而 `tryExecute` 第一行就 `if s.execRepo == nil { return }` 退出，
   根本走不到圈选。留着是因为构造器 `NewAudienceSelectorWithDB(nil)` 返回 nil 是**公开的**
   可判空契约（`Select` 遇 nil receiver 必须回错误而不是 panic）。登记为冗余守卫、
   **不为其写用例** —— 为一个只能靠手工组装结构体才可达的分支写测试，测的是夹具不是行为。

#### 本卡被实测纠正的五处

1. **"表不存在"不能靠"建库时少列一张表"来构造**。`testutil.NewTestDB` 是**同进程共用一个库**、
   只 Drop+AutoMigrate 自己列出的那几张模型 —— 前一个用例建的 `customer_rfm` 还在原地。
   第一版 `TestAudience_PropagatesQueryError` 就因此**直接绿了**（假绿，且绿得毫无痕迹）。
   两条同类用例（调度器侧那条也一样）都改成显式 `Migrator().DropTable(...)`。
2. **`setJSONMapValue` 只能塞字符串值**。预览是对象，走它会被写成 `""`。改用 `json.Marshal` 整体回写。
3. **schedule 型一次 tick 会回写两次**（`recordAudiencePreview` 一次、`last_run_at` 一次），
   而后者序列化的是**内存里那份 map** —— 预览若只落库不落 map，同一次 tick 内就被覆盖掉。
   修法是就地改那份 map；这一条有专门用例，且"只落库不落内存"这个变异被它打红。
4. **注释续行 4 空格又被 gofmt 判成代码块**（与 `7a7c99ad` 同一处坑的第二次）。差别只在于这次是
   **新写的文件**、在提交前被 `gofmt -l` 抓到，而不是等 CI 的 `make fmt-check` 报红。
   修法同前：顶格 `// ` 单空格，不跑 `gofmt -w`（它会把这段中文散文重排成 tab 代码块）。
5. **台账 18c 的"摘掉即静默停摆"是我写错的，而且是**跑不出来**的那类错**。初稿（同时写在
   `check-unwired-assets.sh` 的注释里和本节表格的"症状"列）说：拆掉 `ListCustomerIDsByTag`
   那一跳 ⇒ tags 条件永久空手、用例看不见。回读 `audience_selector.go:149-160` 与
   `TestAudience_SelectByTag` 后两头都不成立 —— 该用例断言"打了 `vip` 的两人必须回来、没打标的
   c-3 不许进来"，腿一拆它就**转红**；而 tags 腿**根本没有死源判据**（segment 腿查
   `rfmHasAnyRow`、churn 腿查 `Count`，tags 腿空手只报 `no_match:tag=…`，:154-155），所以"reason
   说谎"那句连触发条件都不存在。这一格真正守的是**换路**（就地拼一条 `WHERE name = ?` 会让全部
   用例照绿、同时把新仓储读法变成零消费方资产），措辞已按此改写。**为什么它躲过了本卡全部实跑**：
   反向验证证的是"grep 锚点会红"（rc=1、点名 1 行），这条为真、我也照它登记了，但我由它**推出**了
   一句关于运行时行为的陈述句写进文档 —— 门跑的是文本，我记录的却是语义，中间那一跳没人验。
   ⇒ 与"二手审查结论要自己复核"同一族，新登记一条口径：**台账每格"摘掉之后的症状"必须回读被测
   函数与对应用例各一遍，不得由反向验证的结果代推。**

> **一处口径**：第 1…3 条只有跑才知道；第 4 条能"提交前抓到"，靠的是把 `make fmt-check` 放进交付清单 ——
> 这个门在 `scripts/*.sh` 里**不存在**（它是 Makefile 目标），按脚本目录列门禁清单会整批漏掉它，
> 见 `新规划任务清单.md` r48 与项目记忆「门禁口径盲区」第 ⑪ 轴。第 5 条**不在任何门的覆盖面里**，
> 五条里只有它是"回读源码"抓到的：门能证明锚点会红，证明不了我替它写的那句症状。

#### 实跑（全部取自闭包后的最终态；HEAD `9859e2b9` ＋ 本卡未提交改动）

- 反向测试电池（`/tmp/p501_mutation_battery.sh`，九把行为变异，每把 `cp` 备份 ＋ 写回后比 md5）：
  **9 KILLED / 0 SURVIVED**，还原后 `sop_scheduler.go` 与 `audience_selector.go` 的 md5
  与基线一字不差。九把各对应一处"如果实现写歪了，谁会静默通过"：回退创建者、取消硬上限、
  死源判据、交集退化成取首集合、取数报错吞成空名单、无条件不报因、确认门旁路、
  预览只落库不落内存、本轮额度截断失效。
- 台账三格（项18）的反向验证另跑一轮：逐格把调用点那一行注释掉，门 **rc=1** 且**恰好**点名
  被摘的那一行（`回退` 行数 1/1/1），三格还原后 md5 一致、`p501mut` 与 `.p501*` 残留均为 0；
  还原后台门复跑 **rc=0、52/58**（基线 49/55 ＋ 本卡三格）。静态锁与行为锁同一口径：
  没有牙的登记不算锁。
- 先红后绿：AC①②③ 那批 7 条用例在**未改生产代码前**跑，5 条红且红因逐字为
  `空配置应零开工, got [7]` / `期望 3 条执行, got 1 ([7])`（`7` 就是 `CreatedBy`），
  另 2 条（去重、静态名单通道）本就该绿 —— 它们是"零变化"的护栏，不是新行为的证明。
  第二批 2 条（额度截断、预览跨 tick 存活）在实现已就位后写，各自被电池里对应那把变异打红过。
- 受影响两包全量（`-count=1`，两时区各一遍，含并行会话的全部未提交改动）：
  `TZ=Asia/Shanghai` service **668.174s** ＋ repository **231.338s**、rc=0；
  `TZ=UTC` service **487.782s** ＋ repository **143.255s**、rc=0（时区裂脑门同口径复跑，见下条）。
  ⇒ 这一跑的树**就是**闭包后的最终树：期间本卡只动过 markdown 与台账脚本，两包 Go 源码零改动，
  编译产物未变 —— 所以不必为"数出在改动前"再烧一轮 670s。
- 本卡 34 条用例（`TestAudience_*` 10 ＋ `TestSOPScheduler_*` 24）单独 `-test.v` 计数：
  顶层 `--- PASS` **34**、子用例 `    --- PASS` **20**、FAIL **0**、SKIP **0**（4.539s）。
  计数按前缀正则取，34 与两文件里的 `^func Test` 实数（10 ＋ 24）逐一对上 ⇒ 不是"少跑了还全绿"。
- **提交后在 `--shared` 克隆（`/tmp/p501_clone`，`f281dc0c`，工作树 0 脏文件）复验自洽**：
  `make fmt-check` **rc=0**（工作树那次 rc=2 只剩并行会话未跟踪的 `wechat_batchf4_m01_inbound_test.go`
  一个 offender，克隆里没有它 ⇒ 证明本卡两条提交单独就过这道门，不是被别人的树救过的）、
  `go build ./...` **rc=0**、`go vet ./internal/service ./internal/repository` **rc=0**、
  台账门 **rc=0 / 52/58**（与工作树同数）。定向用例在克隆里换**宽**正则 `-run "Audience|SOPScheduler"`
  跑：**rc=0、顶层 PASS 36、FAIL 0** —— 比工作树那轮多 2 条，差的 2 条是既有
  `reach_pipeline_test.go:1006/1015` 的 `TestRunStep_Audience_*`（宽正则按子串命中，与我的 34 条
  无交集，`comm -13` 实测点名）；本轮数与上轮数不是同一把 ` -run` 的口径，**引用时先认正则**。
  该轮第一次跑成红是**取证脚本自身**造成的：`source /tmp/p501_env.sh 2>/dev/null` 里那个文件当时
  已不存在，`2>/dev/null` 把"没 source 上"吞了 ⇒ 全部用例以 `SQLSTATE 28P01` 认证失败收场，
  红因是环境不是代码（重建同长度口令后 rc=0）。⇒ 又一条：`source` 失败不许被静音。
- `-race` 两包：repository **ok 149.126s**；service **rc=1 / 653.562s**，log 里
  `WARNING: DATA RACE` **6** 处、`--- FAIL` **3** 条（`TestCreateSession_AllowDifferentPlatform`、
  `TestCreateSession_AnonymousUser`、`TestM01_QQFetchUsesRealAttachmentURLAndBytes`）。
  **0 帧**涉及本卡三个 Go 文件（全 log `grep -ac "audience_selector\|sop_scheduler\|customer_tag_assignment"` ＝ 0）。
  两组红各自归属：① 前两条同一地址 `0x…75bc60`，写方 `db.SetTestDB()`、读方
  `MaybeSendAwayReply` 的 `SafeGo` 后台 goroutine —— 涉事四个文件（`customer_session_blacklist_test.go`、
  `main_test.go`、`office_hours.go`、`customer_service_plus.go`）`git status` **全 clean** ⇒ HEAD 自带，
  即已登记的 `SetTestDB` 全局句柄那族；② 后四条属并行会话**未跟踪**的
  `webhook_batchf4_m01_qq_test.go:342`（`git log --` 该路径零提交）对 `qq_media.go` 的
  `FetchQQAttachment` / `persistQQMediaAsync` —— 与本卡不同文件、不同键。⇒ 均不代修。
- 门：`check-date-bucket-tz`（命中 **21**，与基线 21 同）、`check-enum-consistency`、
  `check-doc-consistency`、`check-feature-doc`、`check-unwired-assets`（52/58）、
  `api-inventory`（生成物 `git status` 无 diff ⇒ 本卡零新端点）、`audit-cross-package-ports`
  （Errors 0 / Warns 0）七条 **rc=0**；`make fmt-check` **rc=2**，未通过文件**恰 1 个**
  （`internal/controller/wechat_batchf4_m01_inbound_test.go`，`??` 未跟踪）—— 本卡 5 个 Go 文件
  `gofmt -l` 输出**空**，即这条红与本卡零交集（同判据独立复验；且上一条克隆复验里这道门 **rc=0**
  ⇒ 本卡两条提交单独就过它，不必等别人把那个文件改掉）；`check-architecture` **rc=1**
  唯一红仍是 `dingtalk_media.go:191/204`（该文件 `??`）；`check-secrets.sh` **rc=1** 三处命中文件名
  与本卡 7 路径 `comm -12` 交集 **0**；markdown lint（`npx -y markdownlint-cli2`，CI `Markdown Lint`
  的同一条）**rc=1 / 1 issue**，仍属并行会话正在改的 `CHANNEL_INTEGRATION_AUDIT_2026-09.md:808`，
  §4.17 全文 **0 命中**。`check-secrets-artifacts.sh` 需 `<env文件> <产物目录…>` 参数、无参必 rc=2，
  属用法而非门禁；`-race` **不是**任何命名门的一步（口径见 `新规划任务清单.md` r34，本卡只报数不扩大）。
- 前端零改动：本卡 7 个路径全在 `user-server/` 与 `scripts/` 与文档 ⇒ 无 `vite build` /
  `eslint` / `vitest` 腿，不是跳过而是没有对象。
- 闭包计数：任务清单 **2** 行（P5 表下的执行结果 ＋ 修订 r51）、本项目调研 **3** 处
  （§3.5 叙述 ＋ GAP-02 ＋ DRIFT-06）＋ 修订 r26、新规划 **3** 处（N-2 状态行 ＋ RK-6 缓解列
  ＋ LTC-07 现状列）＋ 修订 r15、本文件改 3 处（版本行 ＋ §4.17 ＋ 修订 v1.14 行）—— 三篇规划文档在 git 外，
  在仓库里 `grep` 不到属预期；**1** 个 git 内文档（本文件）。改完逐条 `grep` 命中数复验，
  两个数一起报：文件数 1（git 内）／7（含 git 外三篇与本卡全部落点）。
  **命中数复验跑出来的**：任务清单 `T-P5-01` **7** 处 ＋ `r51` **1** 处、本项目调研 `r26` **4** 处
  （三处就地标注 ＋ 修订行）、新规划 `r15` **4** 处（三处就地标注 ＋ 修订行）—— 三篇各用自己的
  修订号，别拿一个号去另一篇找。分两笔提交：代码 ＋ 台账脚本 `00aeff61`（6 路径），本文档
  `f281dc0c`；git 外三篇不进账（它们本来就不在版本管理内，见项目记忆「规划文档在 git 外」）。


---

### 4.18 `agent_mode` 的第一次真实读取：模式列怎么变成分派输入，以及归因写进 `text` 的代价（T-P5-02）

> 卡面（`docs/replan-2026-09/新规划任务清单.md:258`）：「**N-3 Active 生命周期实现**：
> `lifecycle.Resolver` 现零调用。实现 `ActiveAgentLifecycle`，编排"选材→决策→工具链→触达→归因
> OneID"」。落点 N `internal/aiagent/agent/lifecycle/active.go` ＋ M `internal/app/`（装配 Resolver
> 并按 `model.AgentMode` 分派，兑现 W-4）。AC① `agent_mode='active'` 的智能体走 Active、未知回退
> Passive；AC② Active 不新建自由 Agent 循环、**编排走 SOP**（C7 裁定）；AC③ Passive 行为零变化。

#### 落点（四新增 ＋ 五修改，共 9 个代码路径；没有一张新表、没有一个新列）

| 文件 | 改了什么 |
|---|---|
| `internal/aiagent/agent/lifecycle/active.go`（新增） | `PassiveAgentLifecycle` / `ActiveAgentLifecycle` 两个实现 ＋ 三个单方法依赖接口（`ConversationRunner` / `SOPExecutor` / `CustomerLookup`）＋ `firstParsableSOPID` |
| `internal/aiagent/agent/lifecycle/active_test.go`（新增） | 14 条：编排形状一条、会话键列宽一条、选材/决策/依赖的拒绝路径若干、模式常量稳定性一条、**import 静态锁**一条（go/parser 扫本包非测试文件）。另有既有 `lifecycle_test.go` 的 `TestResolver`（6 子用例）不动，整包 15 条顶层 |
| `internal/aiagent/agent/lifecycle/lifecycle.go` | `LifecycleRequest` 加 `SessionID`/`OneID`、`LifecycleResult` 加 `Mode`/`ExecutionID`/`OneID`；头部那段"两个实现还没有"的过期说明改成现状 |
| `internal/app/agent_lifecycle_wiring.go`（新增） | `AgentLifecycleRuntime`（`Resolver` 的**第一处**生产调用方）＋ `InitAgentLifecycles` 装配 ＋ `POST /api/agent/lifecycle/run` ＋ 三条编译期接口实现断言 |
| `internal/app/agent_lifecycle_wiring_test.go`（新增） | 10 条：分派表（含脏值 `"  Active  "`）、Active 未装配回退 Passive、按 `agent_id` 读取模式再分派、三条错误出口、路由在册、两种模式端到端、坏请求体 4 种、未装配回 503 |
| `internal/service/ai_agent.go` | `LoadContext` 的上下文构造里补 `AgentMode: agent.AgentMode` 一行 —— **W-4 的根因就在这一行不存在** |
| `internal/service/ai_agent_test.go` | ＋1 条：模式要能从上下文读回、缓存路径也读得到、`Update` 改模式后**立刻**生效（不等缓存 TTL） |
| `internal/router/router.go` | 两跳：`BuildSalesEngine` 之后 `app.InitAgentLifecycles(gormDB, engine)`；auth 组内 `app.SetupAgentLifecycleRoutes(auth)` |
| `scripts/check-unwired-assets.sh` | **项5** 由 `unwired` 转 `wired`（防回退）＋ 新增**项19 六格**：49/55 → 52/58 → 现 **55/64** |

#### `ai_agents` 这三列今天到底被谁读

| 列 | 物理形状（`internal/model/ai_agent.go`） | 本卡之前的真实读取方 | 现在 |
|---|---|---|---|
| `agent_mode` | `varchar(32) not null default 'passive'` ＋ **`index`**（:38） | **0**。写侧有两条（`controller/ai_agent.go:168` 建、:269 改），读侧一条都没有：`LoadContext` 压根不往 `AgentContext` 里抄这个字段，于是四处既有 `LoadContext` 调用点实测读回的都是 `""` | `AgentLifecycleRuntime.Run` 按它分派（`agent_lifecycle_wiring.go:86`）—— `AgentContext.AgentMode` 这个**字段**的非测试读点就此唯一 |
| `sop_ids` | `text[]`，无索引（:50） | 只被抄进上下文，没有任何执行方拿它决定"跑哪个 SOP" | Active 的决策输入：取**第一个可解析且非零**的项 |
| `decision_strategy_ids` | `text[]`，无索引（:54） | **0**，且 `LoadContext` 连抄都没抄 | 仍 0 —— 台账项19 按 `unwired` 登记，见下文「刻意不接的四格」 |

那行 `index` 要说清一句，免得下一个人以为"有索引＝按模式查过"：本卡读模式走的是**主键取行后在内存里比字符串**（`AIAgentRepository.GetByID` :54 → `agentCtx.AgentMode`，且 `agent.Status != 1` 时直接 `(nil, nil)`），没有任何 `WHERE agent_mode = ?`。这个索引仍然只为"将来按模式批量取智能体"预备着，本卡没消费它。

#### 归因落在 `execution_data` 里，代价要写在账上

`sop_executions` 侧本卡零列变更，归因全部塞进既有那列：

- `ExecutionData JSONMap` 的 gorm tag 是 **`type:text`**（`model/ai_sales_champion.go:146`），不是 `jsonb`；
  写值由 `JSONMap.Value()`（:467-472）做 `json.Marshal`，`nil` 时写 `"{}"`。⇒ 库里是**一串 JSON 文本**。
- 于是本卡写进去的三个键（`_trigger` / `agent_id` / `one_id`）的读法是：**逐行看得见，按值查不动**。
  要按 OneID 取回"这个客户被主动跑过哪些 SOP"得 `execution_data::jsonb ->> 'one_id' = ?`，
  而这列既没有 `jsonb` 类型也没有表达式索引 ⇒ 那是一次全表扫。
  P8 的"OneID 归因看板"若直接建在这个键上会踩到它 —— 本卡按 C7 不建列不建表，是**有意识的欠账**，
  不是"归因已经可查了"。这两句话的差别就是这块账接下来值不值一张表。
- `agent_id` 比 `one_id` 更必要：`sop_executions` 只有 `sop_id`（:140，`index;not null`），
  一张 SOP 被两个智能体挂上时，没有这个键就回不去"谁发起的"。
- `_trigger` **不是本卡发明的键**：`sop_scheduler.go:236` 写 `"scheduler"`、
  `intent_recognition.go:598` 写 `"intent"`，而 `StartExecutor`（`sop_node_executors.go:42`）会把它
  原样并进节点输出留痕（:51 的补偿说明就写着"删掉它反而抹掉证据"）⇒ 本卡写 `"active_lifecycle"`
  是让三条入口在执行记录里长得一样。**但值域没有任何门登记**（`check-enum-consistency` 不读这三个
  字面量），所以"按触发源统计"是纯文本约定，改一个拼写不会有任何门变红。

#### 归因值取自哪一列，以及 `one_id` 在本仓的三种宽度

进 `one_id` 的值取自**客户行**的 `customers.unified_id`（`varchar(128)`，`model/customer.go:36`），
**不是**请求里那个 `req.OneID`。理由：一次错填的归因值会把这轮外联挂到另一个人身上，
而执行记录里的归因是事后要拿来做经营判断的。这条边界由
`TestActiveRunOrchestratesAgentSOP` 的夹具钉住（fixture 里 `req.OneID` 为空、客户行是 `one-1`）：
**实跑变异**把生产代码改成读 `req.OneID` ⇒ 该包 **1 条红**（`active_test.go:86 归因 OneID 没进名单: <nil>`），
改回后整包 15 条顶层全绿（本卡 14 ＋ 既有 `TestResolver`；`active.go` 改动前后 md5 一致：`116d2683…`）。

`customer.UnifiedID == ""` 时**不写这个键**，而不是写空串：`unified_id` 对历史客户行确实可能为空，
而"键不存在"和"键为空串"在 JSON 里是两件事 —— 按空串归因会把所有无主执行并成同一个人。

顺带实测到的一处漂移（本卡只登记不统一）：物理列 `one_id` 在本仓有**三种宽度** ——
`customer_sessions` / `clues` / `human_tasks` 是 `varchar(100)`，`customer_channels` /
`customer_do_not_contact` / `csat_surveys` / `script_exposure_logs` 是 `varchar(128)`，`opportunities` /
`order_drafts` 是 `text`；而权威源 `customers.unified_id` 是 **128**（且 `script_exposure_logs`
的类型注释就写着"与 `customers.unified_id` 对齐为 varchar(128)：手机号型 OneID 长 70 字符，
varchar(64) 会写入溢出"—— 这条 100/128 的分裂不是抽象风险，本仓已经踩过一次）。
归因值今天躺在 `text` 里不受影响，但**任何"把 `one_id` 从 JSON 提成列"的后续卡必须先定一个宽度**：
按 100 建就会把 128 的源顶到 INSERT 报错（PG 是拒绝、不是截断）。

#### 合成会话键：为什么押在 `agent_id` 而不是 `agent_code`

- `sop_executions.session_id` 是 `varchar(120)` 且**无 index**（:142），`customer_id` 是
  `varchar(64) not null` **带 index**（:141）。
- 第一版键形如 `active-<agent_code>-<customers.id>`：最坏 `7+64+1+36 = ` **108 字节** ——
  今天不炸，但那 12 字节余量押在**另外两张表的列宽**上（`ai_agents.agent_code varchar(64)` :33
  ＋ `customers.id varchar(36)` :35）。谁把 `agent_code` 放宽到 128，"可读的键"就变成一次 INSERT 报错，
  而报错点在没人盯的主动链路上。
- 现用 `active-<agent_id>-<customer_id>`：`7 + ≤20 + 1 + 36`，与那两列彻底解耦，
  且与调度器同形状（`"scheduler-"+agent_id`，`sop_scheduler.go:231-238`）。
- `TestActiveSessionKeyStaysWithinColumn` 有**两条腿，有牙的是第二条**：
  第一条（`len ≤ 120`）在旧实现下也过，单独看它是条假锁；第二条断言"键里不许出现 `agent_code`"
  才是防回退那条。注释里写明了这一点，免得下一个人把整个用例读成"长度检查"。

#### 刻意不接的四格（台账项19 的另外四行）

本卡**没**接这四样，登记在此是因为"Active 现在真的能跑了"会让人以为这一族字段都通了：

| 格子 | 实测 | 为什么刻意不接 |
|---|---|---|
| `agent.ModeOf` / `agent.IsActive`（`agent/agent.go:35`） | 包外调用 0（唯一"消费"是同文件自调） | 运行期判模式走的是 app 侧 `Resolver`；这两个 helper 读的是 `*model.AIAgent` 整行，而分派点手上只有上下文 DTO |
| `SalesRequest.AutoExecute` | **写入 5 处、读取 0 处** | 真正的自动回复开关是编排器里的进程内 `o.enableAutoReply`。接上它等于给运营第二个开关，两个源必然漂移 |
| `AgentContext.DecisionStrategyIDs` | 读方 0，`LoadContext` 连抄都没抄 | 接进来就是凭空造一套"策略怎么选"的语义，超出卡面 |
| `sop_executions.one_id`（不存在） | —— | 见上文：按 C7 不建列，代价一起写在账上 |

#### 索引与迁移

本卡零新表零新列 ⇒ `allModels()`（`internal/pkg/db/migrate.go`，`&model.SOPExecution{}` 已在 :175）
无新增条目、`AutoMigrate` 无 diff、无迁移文件。唯一与 schema 有关的动作是**读**：`agent_mode` 从
"有列、有索引、零读取方"变成"有列、有索引、一个内存读点"。

#### 交付口径：三条 AC 各自被什么钉住

| AC | 用例 | 这一条到底在防什么 |
|---|---|---|
| ① `agent_mode='active'` 走 Active，未知回退 Passive | `TestAgentLifecycleDispatchByMode`（5 个输入：`active` / `passive` / `""` / `hybrid` / `"  Active  "`）＋ `TestActiveModeFallsBackWhenActiveNotAssembled` ＋ `TestRunByIDDispatchesOnLoadedMode` | 第二行比第一行重要：**Active 没装配时回退的是 Passive，而不是"报错"**。回退方向是刻意的 —— 装配失败时用户侧只是"没被主动联系"；反过来把被动请求当主动跑，就是拿一个没有入站消息的口子去外发。脏值先 `TrimSpace`＋`ToLower` 再比，因为这一列是运营手填的 `varchar(32)` |
| ② Active 不新建自由 Agent 循环，编排走 SOP | `TestActiveNeverImportsConversationEngine`（go/parser 读**本包所有非测试文件**的 import 集，按后缀禁 `/internal/service`、`/agent/runtime`、`/agent/bridge`）＋ `TestActiveRunOrchestratesAgentSOP` 断言唯一的编排出口是 `SOPExecutor.Execute` | 这是一条**静态锁**，防的是"以后有人图方便在 Active 里直接调会话引擎" —— 那样闸门（T-P5-03）与归因（P8）就多出第二条不经过 SOP 的外发路径，而它在测试里长得和正路一样。锁的边界也说清：它只保证"这一包不 import"，**不保证"这条路上的闸门真的生效"** |
| ③ Passive 行为零变化 | 实测口径：`LoadContext` 的非测试调用点本卡之前 **4** 处（`service/ai_agent.go:241` TestAgent、:416 渠道账号→绑定→上下文、:553 座席挂载、`controller/ai_agent.go:446` HTTP 直读；另有两处同名 `noopAssetLoader.LoadContext` 属另一个接口，不计），本卡 ＋1 处 ⇒ **5**；而这四处旧调用点里读 `AgentMode` 的**一处都没有**（`AgentContext.AgentMode` 非测试读点只剩新分派点 `agent_lifecycle_wiring.go:86`，同文件 :88 只是把同一个值打进错误消息） | 于是"补上 `LoadContext` 那一行"改变的是一个**没人读的字段**的值，既有行为逐字节不变；`PassiveAgentLifecycle` 今天也不在渠道入站的路上（它只服务 `/api/agent/lifecycle/run`）。这条 AC 是"本卡不破坏现网"的表述，不是"被动模式已收口到统一入口" |

#### 本卡之后仍不成立的四件事

1. **渠道入站一行未动**。被动模式在生产上仍走 `SmartCSOrchestrator`，本卡没把它改道，
   也没有任何一条路让入站消息经过 `AgentLifecycleRuntime`。
2. **新端点没有渠道上下文**。`POST /api/agent/lifecycle/run` 收 `agent_id` ＋ `customer_id`
   （`content` 刻意不 `binding:"required"`，因为主动模式没有入站消息），回 `mode` / `execution_id` /
   `one_id` / `reply` / `handoff` / `stop_reason` / `tools_called`；它不代表"从某个 IM 进来的一条消息"。
3. **Active → SOP 这条路上闸门是否生效，本卡零判据**。出域必经 `approval_request` ＋
   `checkCooldown` ＋ `checkDoNotContact`（X5 不得旁路）是 **T-P5-03** 的验收条件，
   那张卡的依赖里就写着本卡。本卡只保证"编排出口只有一个"，不保证"那一个出口上有闸门"。
4. **四格仍 UNWIRED**：`agent.ModeOf` / `agent.IsActive` / `SalesRequest.AutoExecute` /
   `AgentContext.DecisionStrategyIDs` —— 台账项19 那四行按 `unwired` 站着，它们红着才是对的。

#### 实跑（数字取自工作树最终态：HEAD `cc0166b4` ＋ 本卡未提交改动 ⇒ 即闭包后 `b7fcedf3` 的内容；每个数都点名测的是哪个对象）

- 三张包级全量：`internal/aiagent/agent/lifecycle` **ok 0.255s / rc=0**（TZ=UTC，带 `-test.v` 实数：
  顶层 `--- PASS` **15**、子用例 `    --- PASS` **6**（即既有 `TestResolver` 的 5 表项 ＋ 1 条独立 `t.Run`）、
  FAIL **0**、SKIP **0** ⇒ 15 与两文件 `^func Test` 实数（14 ＋ 1）逐一对上，不是"少跑了还全绿"）；
  `go test ./internal/app/ ./internal/router/ -p 1`（TZ=UTC）**rc=0**，app **55.278s** / router **20.009s**
  / lifecycle **0.509s**（同一条命令把 lifecycle 带进去重跑了一遍，三轮数不通用，引哪轮认哪轮）；
  `internal/service` 全量 **ok 744.237s / rc=0**（TZ=UTC，`-timeout 2400s`，**不带 `-v`** ⇒ 本轮 PASS 计数为 0，
  只报"整包绿"不报条数；log 里 `^--- FAIL` / `^FAIL` / `DATA RACE` 各 **0**）。这一跑的工作树**就是闭包后的最终树**
  （HEAD `cc0166b4` ＋ 本卡改动，含并行会话刚落的 R17/R18 迁移代码），跑前一次同口径是 **824.621s** ⇒
  两数都记，别只引一个：这一包的秒数**天然摆动**（见下条）。`go build ./...` **rc=0**。
  ⇒ **这一包的数前面有两次假红**：600s 默认超时那次 `FAIL 601.055s`、加 `-v` 那次跑到 1503.713s 才超时
  （342 条 PASS、0 FAIL，停在 `TestE2E_WebhookService_DispatchWhatsApp` 中间）—— 两次红因都是**旁边同时挂着
  golangci-lint 与 npx** 的负载，不是代码（项目记忆「Go 全量门禁耗时口径」的 450–880s 摆动区间，本轮两次实测
  744.237s / 824.621s 都落在区间内）。单独跑、给足超时才是这一包的口径。
- 台账两格 wired 的**有牙证明**（router.go 两跳各摘一行，`cp` 备份、写回后比 md5）：
  ① 注释掉 :244 `app.InitAgentLifecycles(gormDB, engine)` ⇒ `check-unwired-assets.sh` **rc=1**，
  log 第 **60** 行为 `[项19] 回退（登记为已接线却无调用点） 接线数=0 双模式运行时的启动装配点（摘掉即线上每次运行稳定回 503、app 用例全绿）`
  （列间是脚本的对齐空格；引号内文字与台账 `check-unwired-assets.sh:325` 那行的说明列原文一致），
  第 **67** 行按 `项19:<说明>` 再点名一次同一行；
  **同一份变异下** `go test ./internal/app/ ./internal/router/ -p 1`（TZ=UTC）**rc=0**（app **19.653s** / router
  **10.744s**，热缓存复跑；本轮之前那次冷缓存同判据是 82.667s / 27.410s —— 两轮的**结论**一致，秒数不可混引）
  ⇒ 这两句合起来才是这一格的代价陈述：**装配点断线时测试全绿、线上每次运行稳定回 503**，台账是**唯一**看得见它的东西。
  ② 注释掉 :640 `app.SetupAgentLifecycleRoutes(auth)` ⇒ **rc=1**，第 61 行点名「双模式运行入口的路由登记点」。
  两把还原后 md5 回到 `eea246b4f07f8d37b3620e5de82a87e5`、`grep -c "MUT:"` ＝ **0**、目录无 `.bak`/`p502r` 残留，
  门复跑 **rc=0 / 「✅ 与登记一致（exit 0）：55/64 行已接线」**。
- 归因边界那一条（"值取自客户行而非 `req.OneID`"）由**单测变异**证过：把生产代码改成读 `req.OneID`
  ⇒ 恰好 **1 条红**，红因逐字 `active_test.go:86: 归因 OneID 没进名单: <nil>`；改回后整包 **ok 2.035s**、
  `active.go` md5 与改动前一致（`116d26838a9e7047a45ca899c4dfbbf0`）。
  会话键那条用例的第二腿同样有牙（它断言"键里不许出现 `agent_code`"），第一腿（`len ≤ 120`）在旧实现下也绿 —— 已在上文写明，不重复。
- **本轮抓到的一处口径错误（写在这里，因为它改的是本文件既有的一句话）**：`api-inventory.sh:32` 只在
  `user-server/internal/router/**` 里 grep 路由字面量，**从 `internal/app` 注册的路由它看不见**。
  本卡真实新增 `POST /api/agent/lifecycle/run`，而 `api-inventory.md` **零 diff** ——
  同族的 `/agent/inference/run`、`/tools/permission/*` 也都不在那份清单里（`grep agent/inference` 命中 **0**）。
  ⇒ "清单无 diff ⇒ 本卡零新端点"这个推论**不成立**。§4.17 里那句"本卡零新端点"**结论为真**
  （已核：`git show --stat 00aeff61` 六路径、`git show` 里零路由注册），但它当时是靠这道**看不见证据**的门立起来的 ——
  本卡是第一张"如果照抄那条推论就会说错"的卡。门本身**没有改**（改共享门禁超出卡面），按欠账登记，
  与项目记忆「门禁口径盲区」同一族（第 ⑨ 轴：注册形状）。
- 三处**取证侧自身**的坑（红与绿都不由被测代码决定，故单列一条）：
  ① 第一批门写成 `out=$(cmd | tail -n); rc=$?` ⇒ 取到的是 `tail` 的退出码，十条门**齐刷刷显示 rc=0**；
  改成 `cmd >log; rc=$?` 后真实红才露出来（`check-architecture` / `check-secrets` / `make fmt-check`）。
  ② `pg_isready -h 127.0.0.1 -p 8232` 瞬时报"不接受连接"，同一分钟 `lsof` 显示 OrbStack 在听、立刻复检即 rc=0
  ⇒ 复检一次再判环境。（与「CLI 工具链陷阱」「取证脚本自身要审」两条记忆同一族。）
  ③ `go vet ./...` **rc=1** 报 `internal/browser_automation/service/executor.go:117`（`cannot slice make(map[string]*string)`），
  而同一分钟 `go build ./...` **rc=0** ⇒ 该文件是并行会话**正在编辑**的（状态 `M`，那一行现已是 `ledgerGaps: make(map[string]bool),`）。
  共享工作树的 vet 只能证"这一分钟没人改文件"，所以闭包判定留给 `--shared` 克隆。

- 门（每条都取真实退出码，方法见上）：`check-date-bucket-tz` rc=**0**（命中 **21**，与基线 21 同）、
  `check-enum-consistency` rc=**0**（3 条 warn，均非本卡字面量 —— `_trigger` 那三个值本来就**不在**它的读集里，
  上文"值域无门登记"说的就是这件事）、`check-doc-consistency` rc=**0**、`check-feature-doc` rc=**0**（通过 1 / 失败 0 / 跳过 1）、
  `check-unwired-assets` rc=**0**（**55/64**）、`api-inventory` rc=**0**（但见上条：它对端点看不见）、
  `audit-cross-package-ports` rc=**0**；`check-architecture` rc=**1**、`check-secrets.sh` rc=**1**、
  `make fmt-check` rc=**2** —— 三条红的归属逐条核过：前两条命中文件分别是 `??` 未跟踪的 `dingtalk_media.go`
  与三个 `webhook_batch*` 测试文件，与本卡 9 个路径 `comm -12` 交集 **0**；`fmt-check` 唯一 offender 也是
  `??` 未跟踪文件（零提交），本卡 9 文件 `gofmt -l` 输出**空**。
  markdown lint（`npx -y markdownlint-cli2`，CI 的同一条）**rc=1 / 活树 161 文件 2 issue、`--shared` 克隆 153 文件 1 issue**，
  两条都不在本文件：`CHANNEL_INTEGRATION_AUDIT_2026-09.md`（活树 `:808`／克隆 `:797` —— 同一个 MD004，那两个数差 11 行
  正因为这文件是并行会话**未提交**的 `M`）＋ 并行会话这一小时新加的 `docs/superpowers/specs/2026-09-21-offline-deployment-design.md:34`
  （`??` 未跟踪 ⇒ 克隆里根本没有它，这就是两棵树文件数差 8 的来源）。
  ⇒ **计数以 HEAD 克隆为准收口**（项目记忆「门禁口径盲区」第 ⑬ 轴）：活树多出来的未追踪 md 会把文件数顶高，
  而克隆那 1 条 issue 才是本卡要交代的红 —— 它不在本卡任何路径里，§4.18 全文 **0 命中**。
  `-race` 仍**不是**任何命名门的一步（口径见 `新规划任务清单.md` r34，本卡不扩大）。
- **提交后在 `--shared` 克隆（`/tmp/p502_clone`，`fe8b9e71`，工作树 **0** 脏文件）复验自洽**：
  `go build ./...` **rc=0**、`go vet ./...` **rc=0**（活树那次 vet 红命中的是并行会话**未提交**的
  `browser_automation/service/executor.go:117`，克隆里没有它 ⇒ 本卡三笔提交单独就过这道门，不是被别人的树救过的）、
  `make fmt-check` **rc=0**（活树 rc=2 的唯一 offender 是 `??` 文件，同理）、台账门 **rc=0 / 55/64**（与活树同数）；
  三包 `-test.v -p 1`（TZ=UTC）**rc=0**，顶层 `--- PASS` **341** / 子用例 **106** / FAIL **0** / SKIP **0**
  （lifecycle **0.702s**、app **22.349s**、router **11.134s**）。
  ⇒ 只有这一轮才算"本卡自洽"的证据：本卡三笔提交落地后，活树仍有 **162** 项 `M`/`??` 属并行会话
  （`git status --porcelain` 实测 **163** 条，减去本文件这一节自己的未提交编辑），它们**同时**污染活树各条门的计数
  （上一条 markdown lint 的 161 vs 153、vet 的那一发红，都是这么来的）。
- 前端零改动：本卡 9 个路径全在 `user-server/` 的 Go 源码、`scripts/` 与文档 ⇒ 无 `vite build` / `eslint` / `vitest` 腿。
- 闭包计数：任务清单 **2** 行（P5 表下的执行结果 ＋ 修订 r54，P5 进度 1/4 → 2/4）、本项目调研 **3** 处
  （判定 A 项5 ＋ 接线进度那行的 r27 追加 ＋ 修订 r27）、新规划 **3** 处（N-3 状态行 ＋ LTC-07 现状列 ＋ 修订 r16）、
  本文件 **3** 处（版本行 v1.15 ＋ §4.18 ＋ 修订历史一行）。三篇规划文档在 git 外，仓库里 `grep` 不到属预期。
  **命中数复验（最后一次编辑之后在磁盘上重数的，不是按写了几个 heading 推的；每个数点名文件）**：
  `新规划.md` 里 `r16` **3 行 / 5 处**、`T-P5-02` **5** 处；`本项目调研.md` 里 `r27` **2 行 / 4 处**、
  `T-P5-02` **4** 处；`新规划任务清单.md` 里 `T-P5-02` **7** 处（表行、T-P5-03 的依赖列、执行结果段、W-1/W-4 两张映射表、
  r17 修订行、本卡修订 r54 各一）；
  本文件里 `4.18` **7** 处、`T-P5-02` **7** 处（**这一句自己就是其中一处命中** —— 计数句含被数词，
  任何后续编辑都会让它漂，所以这两个数只在"最后一次编辑之后"成立）。三篇规划文档各用自己的修订号，别拿一个号去另一篇找。
  提交实际分 **4** 笔：代码 ＋ 台账脚本（8 个 Go 路径 ＋ 1 个 shell ＋ `AI_CORE_FEATURE_INVENTORY.md`）＝ `b7fcedf3`，
  本文档 `7679c9ce` ＋ `fe8b9e71` ＋ 本笔（§4.18 的 实跑／门／克隆复验／闭包计数 四段逐次收口）；
  `api-inventory.md` **不进账**（它对本卡零 diff，而理由就是上面那条门盲区 —— 不拿一个看不见证据的东西当交付物）。

### 4.19 `reach_send` 节点：图上多一步"先批准才出域"，零新表零新列，代价是 `execution_data` 里再多四个键（T-P5-03）

本卡给 SOP 图新增一个节点类型 `reach_send`（`internal/service/sop.go` 的常量 ＋ 受支持集合），把 Active 外联从"代码里的一段 if"变成"图上的一步"：没有已批准的结论就不出域，出域只经 `ReachByCustomer` 这一个出口。**库里没有新表、没有新列、没有新索引、没有任何迁移** —— 所有新事实都落在既有列里，所以这一节与 §4.18 同构：登记的重点不是 DDL，而是**这些值落在哪一列、谁能按值查它们**。

#### 落点（4 新增 ＋ 8 修改，共 12 个代码路径；`migrations/` 零变更）

| 路径 | 增/删 | 与库的关系 |
|---|---|---|
| `internal/service/sop_reach_send.go` | **新增** 308 行 | 节点执行器本体；写 `execution_data` 的三个产物键与一枚幂等键 |
| `internal/service/sop_reach_send_test.go` | **新增** 898 行 | 19 条顶层用例（＋3 子用例见下）＋ 那道静态出域锁 |
| `internal/router/reach_sender_assembly_test.go` | **新增** 53 行 | 装配点的源码形状锁（见"为什么运行时看不见"） |
| `scripts/mut_reach_p503.py` | **新增** 428 行 | 24 格变异电池 |
| `internal/service/sop.go` | ＋8 | 节点类型常量 ＋ 受支持集合（决定图能不能存进 `sop_definitions.graph_json`） |
| `internal/service/sop_node_executor.go` | ＋11 | `ExecutionContext.ApprovalOutcome` 字段（执行数据到节点的入口） |
| `internal/service/sop_node_executors.go` | ＋8 −1 | `reg(NewReachSendExecutor())` |
| `internal/service/sop_dispatcher.go` | ＋21 −11 | 点火时把审批结论递回执行器（11 处删除是 struct literal 的 gofmt 对齐，无语句删除） |
| `internal/service/sop_node_executors_test.go` / `sop_compensation_inventory_test.go` | ＋3 / ＋5 −1 | 登记表 19→20 类 |
| `internal/router/service_routes.go` | ＋5 | `service.SetSOPReachSender(proactiveSvc)` 一行 ＋ 注释 |
| `scripts/check-unwired-assets.sh` | ＋37 −2 | 项 20 四行 |
| `internal/service/proactive_reach.go` | **零 diff** | 卡面写的是"M 该文件（接入检查点）"，实测三判据已在 `ReachByCustomer` 内 ⇒ 不改其选路被执行到极端，偏差如实登记 |

#### 这一次外发在库里留下的痕迹（按列列，不按代码列）

| 列 / 键 | 类型 | 写入方 | 语义 |
|---|---|---|---|
| `sop_definitions.graph_json` → 节点 `config.content` / `config.preferred_channels` | JSON in text | 人工或 API 填图 | 模板串（渲染时以 `execution_data` 为变量表）与渠道偏好 |
| `sop_executions.execution_data._reach_skipped` | JSON in **text** | 节点（跳过时） | 值域 `dnc` / `cooldown` / `already_sent` / `approval:<状态>` —— 四种"没发出去"的后续处置动作完全不同，合并成一个 `skipped` 等于把三件不同的事混成一件 |
| `sop_executions.execution_data._reach_channel` / `_reach_message_id` | 同上 | 节点（发送成功时） | 发到哪个渠道、渠道回的单号 |
| `sop_executions.execution_data._side_effects` 数组里的 `reach_sent:<execution_id>:<node_id>` | 同上 | 节点（发送成功时） | 幂等键；沿用既有约定（`sop_node_executor.go:236/:268`），与消息类节点的 `message_sent:` 同一命名空间但不同前缀（撤销语义不同） |
| `approval_requests` 一行（`subject_type='sop_node'`，`subject_id='<execID>--<nodeID>'`） | 既有表 | 审批腿（T-P3-02 的桥） | **挂起期真的落库**，待办中心可见、可裁决 |
| `sop_timers` 一行（`wait_event=approval`） | 既有表 | 同上 | 到期时刻取自审批行自己的 `expires_at` |

#### 挂起期不是"一条 pending 记录"，而是四行四个状态的组合

`handleNodeWaiting`（`sop_dispatcher.go:585`）只写 `last_event_at` / `wait_event` / `attempt_count`，**从不写 `sop_executions.status`** —— 所以"这一步卡在哪儿"在单张表里读不出来，必须四行一起看：`approval_requests.status=pending` ＋ `sop_timers.status=pending` ＋ `sop_executions.status=running` ＋ `wait_event=approval`。给看板/运维的取数口径因此是**后者那一列**（`wait_event`），不是执行行的状态。这不是本卡造成的，但本卡是第一个让这张组合图成为"外发必经形状"的卡，所以写死在这里。

#### 一个 DB 约束顺手当了幂等兜底

`approval_requests` 上有部分唯一索引 `uq_approval_request_open (subject_type, subject_id) WHERE status = 'pending'`（`internal/model/approval_request.go:30-31`），而 `subject_id` 是 `EncodeApprovalSubject(executionID, nodeID)` 即 `<exec>--<node>`（分隔符 `--`，`sop_approval_resume.go:59`）⇒ **同一步在挂起期间不可能有两条 pending 审批行**。这是这一格的重复挂起防线来自数据库而不是应用层的实证；节点自己的"已发过"防线则是上面那枚 `reach_sent:` 键（跨重启有效，因为它落在执行行里）。两枚键各管一件事，去掉任一处都由电池的一格打红（M1 幂等、M16/M17 注册与类型集合）。

#### 对 §4.18 一句口径的收窄：值不是"查不动"，是"没有索引可走"

§4.18 写归因键落在 `execution_data`（`type:text`）时说了一句"按值查不动"。本卡登记同一族四个键时把这句话重核了一遍，发现它说重了：这些值在 **`sop_exec_events`** 里各有一份 jsonb 镜像 —— `Input` 每事件都塞整份 `execution_data`，`Output` 收节点产物，`SideEffects` 收幂等键（`sop_dispatcher.go:836-850`，三列 gorm tag 都是 `type:jsonb`，`model/sop_executor.go:14-16`）⇒ **可以按值查**。真正的代价是这三列**没有任何 GIN 索引**（`migrations/` 里 `sop_exec_events` 无索引 DDL）⇒ 每次按值查都是扫，且扫的是比执行表大一到两个数量级的事件表。所以"P8 看板若建在 `one_id` / `_reach_skipped` 上会撞全表扫"这句结论不变，措辞从"查不动"改成"查得动、但没有索引"。**顺带一条取数建议**：按渠道/跳过原因统计走 events（jsonb、带 `node_id`/`event_type`/`created_at` 维度），别走 `execution_data`。

#### 为什么两个审批主体不合成一枚键

同一次外发身上现在有两把键：图上腿的 `sop_node` 主题源自**执行行**（`<exec>--<node>`），服务侧 W-1 门源自**客户行**（`reachApprovalKey(customer.UnifiedID, customer.ID, channel, recipient)`）。不合成的理由：合成后"改图"就等于"改归因"（图里的数据说了不算，权威只有一处）；且挂起/恢复要按执行步读回结论，而门要按客户＋渠道＋收件人判白名单，两者生命周期不同（一个随执行结束而终态，一个跨执行长期有效）。这一格由 P1 变异格背书：把门键改成从执行数据取 ⇒ 恰好点名 `TestReachSend_GateKeyComesFromCustomerRowNotExecutionData` 那一条红。

#### `sop_executions.customer_id` 的列形状直接决定了一条测试前提

该列是 `varchar(64) NOT NULL` **且没有 default**（`internal/model/ai_sales_champion.go:141`）⇒ 空串是一个**合法的存量形状**（PG 只在缺列时报错，不阻止写空）。所以节点必须能在 `customer_id=""` 时靠 `execution_data.one_id` 找到身份，这不是防御性编程而是列定义允许的形状，用例 `TestReachSend_ResolvesIdentityFromOneIDWhenExecutionHasNoCustomerID` 就是照这一条建的。（与 §4.18 那条 `one_id` 三种宽度 100/128/`text` 的欠账同族：本卡不新建任何 `one_id` 列，所以不改这个债。）

#### 为什么运行时看不见装配点，以及那道锁看不见什么

装配点 `service.SetSOPReachSender(proactiveSvc)` 在 `router.Setup` 的触达装配步，而节点执行器在 `InitSOPExecutionDispatcher`（`cmd/api` 启动早期）注册 ⇒ 那一刻没有可注入的东西，只能事后注入包级全局。这类事实**运行时没有任何可观测差异**：漏一行 ⇒ 图上外发全部 fail-closed（"外发服务未装配"）而 `internal/service` 的用例带着自己的夹具一条都不红；改成"新建一个没装 T-P3-07 闸门的实例" ⇒ 编译过、路由通、发得出短信，只是那道门从此不在 Active 这条路上。所以补了一道**源码形状锁**（`reach_sender_assembly_test.go`：装配行存在且恰好一处、次序在 `AttachReachGate(proactiveSvc)` 之后、`NewProactiveReachService(` 构造点仍只一处）。它的边界写进注释：**看得见"这一行在不在"，看不见"这一行是否真被执行"** —— 后者属装配期事实，留给服务侧那三条 fail-closed 用例兜方向（未装配 ⇒ 不发，而不是放行）。

同一族的另一道锁是出域符号扫描（`TestNodeExecutorFiles_HaveNoCustomerOutboundBeyondReachByCustomer`）：按"有没有 `NodeType() string` 方法"现场判定扫描面（当前命中 5 个文件），禁止其中点名 9 个渠道私有发送器或自行构造渠道服务，唯一放行入口 `ReachByCustomer`。它自带两条防"退化成恒绿"的断言：扫描面非空 ＋ 面内至少一处 `ReachByCustomer` 调用。覆盖面要说清：拦不住"另起一个不被 `NodeType()` 认出来的执行器"，也拦不住反射/间接调用；前者的兜底是未注册 ⇒ `NoopExecutor`（**确实零外发，但会把节点按 `completed` 推进、只留一行 warn** ⇒ 只兜得住"多发"那一半，兜不住"这一步其实什么都没做"）。批量群发路 `dispatchOutbound → sender.SendReach`（`reach_pipeline_dispatch.go:44`）本就不在这道锁的扫描面内 ⇒ 已登记，不谎称被拦。

#### 交付口径：三条 AC 各被什么钉住

- **AC① DNC 客户零外发** —— 判 `skipped` 且回显 `dnc`，不判失败（失败会重试、重试会再挂一次审批）。
- **AC② 超频控零外发** —— 同上，回显 `cooldown`。
- **AC③ 未审批停在 pending 而非发出（卡面要求反向测试）** —— 挂起态四行组合见上；"绕过路径必须被测试抓到"落成 `scripts/mut_reach_p503.py` 的 **24 格**逐格注码：每格断言锚点命中恰好一次、每格带一个 expect 杀手（红了但杀手不在名单里＝判问题），最终 **KILLED=24 / SURVIVED=0 / BUILD-BROKEN=0 / ENV-BROKEN=0 / NO-RUN=0**，两个控制组各自 ran=登记数（service 26/26、router 1/1）、skip=0，还原后逐文件 md5 与基线一致。

#### 本卡之后仍不成立的事

- **block 态不可运营**：闸门授权只有 `POST /agent/tools/approval/whitelist`（`internal/router/tool_debug_routes.go:34`）一个写入点，且那张表只在进程内存、重启即空（`internal/app/approval_wiring.go:281` 自己写着"撑不起阻断"）⇒ 今天推 `FF_LTC_REACH_GATE=block` 等于把所有冷触达全拒。方向是 fail-closed，但没有可运营的中间态，这是 T-P5-04 的前置。
- **频控的列形状不对称**：`ReachByCustomer` 的 `req.Phone` / `req.Email` 两条显式收件人分支不过 `checkCooldown`（只过退订与闸门），且闸门键在那两条腿上取调用方传来的 `req.OneID` 而非客户行的 `unified_id` ⇒ 同一份请求两种归因口径。本卡节点**从不指定收件人**，绕开而不是在这条路上修。
- **没有前端编排入口**：`user-web/src/views/sopAgent/List.vue` 的节点行只有 type/name/action（`grep -n 'config'` 命中 **0**，`reach_send` 在 `src/` 命中 **0**）⇒ `config.content` 只能经 API/导入建图，这一格的运营面还没开。
- **节点没有 `subject` 键**：刻意不填（主题只被 email 渠道读，而没有任何用例走过 email）——留一条无人验的传参等于给运营一个不生效的字段。
- **`FEATURES.md` 登记仍推后**（该文件正被并行会话整表重写，与本卡路径交集 0）。

#### 实跑（数字全部点名测的是哪个对象；`--shared` 克隆基线 HEAD `4d9a13c5` ＋ 本卡 13 个覆盖文件、逐文件 md5 核过）

- `internal/service` 全量**两时区各 rc=0**：CST **397.062s** / UTC **618.313s**，均顶层 `--- PASS` **3594** / FAIL **0** / SKIP **3**；skip 三条为既有环境门控用例（`TestAIAgent_AssetBundleBinding` / `TestAIAgent_FullChain` / `TestPlatformAccountService_Login`），**两时区同一批**。
- `internal/router` ＋ `internal/app` `-p 1` **两时区各 rc=0**：顶层 PASS **330** / FAIL **0** / SKIP **0**（CST 11.192s / 17.706s，UTC 6.650s / 18.020s）。
- 本卡判据集（`-run` 五组名，带 `-test.v`）：**23 顶层 ＋ 3 子用例全 PASS、FAIL 0、SKIP 0，11.778s**（23 ＝ 本文件那 19 条 ＋ 既有的注册表与补偿清点各 1 条 ＋ T-P3-07 留下的两条同前缀 pipeline 用例；子用例 3 条全在 `TestReachSend_NonApprovedVerdictSendsNothing` 下：rejected / expired / unreadable）；router 源码锁 1 条 PASS（1.312s）。
- `go build ./...` **rc=0**、`go vet ./internal/service ./internal/router` **rc=0**。
- 门：`make fmt-check` / `check-unwired-assets`（**58/68**）/ `check-date-bucket-tz` / `check-enum-consistency` / `audit-cross-package-ports` / `check-architecture` / `api-inventory`（生成物**零 diff** ⇒ 本卡无新端点；但见 §4.18 那条"从 `internal/app` 注册的路由它看不见"的口径盲区）——**七条 rc=0**。`check-doc-consistency` rc=2、`check-feature-doc` rc=1、`check-secrets.sh` rc=2 在改名克隆里属**布局前提不成立**（前两条按 `scripts/../..` 定根并要求 `<workspace>/hivemtk/` 存在；第三条要仓根有 `.env`），提交后在合规命名克隆复跑收口。
- 台账**现值 58/68 的两个端点各实测一次**：同一棵克隆里分别跑 HEAD 版与工作树版脚本 ⇒ **55/64** 与 **58/68**（差值恰为项 20 四行、其中三行 wired）。四行各做一次反向摘装：rc=1 且恰好点名被摘的那一行，还原 md5 一致。
- 一条**自家工具**的口径修正留在原位：电池的控制组借用格子的分类函数打印，未注码基线被打成 `SURVIVED`（读起来像"有一格活下来了"）⇒ 改为独立的 `CLEAN/DIRTY` 标签，判据一字未动，改完**整条电池重跑一遍**取最终数：`控制组[service] CLEAN rc=0 ran=26/26 skip=0 FAIL=[]`、`控制组[router] CLEAN rc=0 ran=1/1 skip=0 FAIL=[]`、**KILLED=24 / SURVIVED=0 / RED-UNNAMED=0 / BUILD-BROKEN=0 / ENV-BROKEN=0 / NO-RUN=0**，还原后逐文件 md5 与基线一致，另打印 8 条 `[同族]` 提示（只报不判负）。
- **提交后复验（committed tree：`--shared` 克隆放在 `/tmp/p503gate/hivemtk`，HEAD `3f39f3be`，克隆工作树 0 脏文件）**：上面那三道被布局前提挡住的门，在"克隆路径以 `hivemtk` 结尾 ＋ 把仓根 `.env` 复制进去（只复制、不打印任何值）"之后**各 rc=0** —— `check-doc-consistency` rc=0（3 条警告全是同一 workspace 下 `hivemtk-platform/` 缺 README / LICENSE / NOTICE，与本卡交集 0）、`check-feature-doc` rc=0（通过 1 / 失败 0 / 跳过 1）、`check-secrets` rc=0；`check-unwired-assets` 在同一棵**已提交**的树里再次读出 **58/68**，与覆盖文件那棵树上的数一致 ⇒ 台账数不依赖未提交状态。`go build ./...` rc=0；本卡判据集在克隆内以 **TZ=UTC** 再跑 **26 行（23 顶层 ＋ 3 子用例）全 PASS、FAIL 0、SKIP 0，21.768s** —— 与覆盖文件那次的 11.778s 差的是机器负载，不是判据；router 源码锁 PASS（1.260s）。提交前的活树同样实测：判据集 **23 顶层 ＋ 3 子用例全 PASS、FAIL 0、SKIP 0，16.623s**，`gofmt -l` 对本卡 8 个 `.go` 文件**无输出**。两笔提交：`3fe3c9d9`（代码 ＋ 取证脚本 ＋ 台账，12 路径 **+1787 / −15**）、本行所在的那笔文档提交；双推 gitee 与 github，推后实测两处远端 ref 同为 `3f39f3be`（`c7402202..3f39f3be`，fast-forward）。
- 闭包计数：任务清单 **2** 处（P3 出口条件段 ＋ P5 表下执行结果，另加修订 r56）、本项目调研 **3** 处（X5 行 ＋ 接线进度行 ＋ 修订 r28）、新规划 **4** 处（X5 约束行 ＋ N-3 行 ＋ LTC-07 行 ＋ 修订 r17）、本文件 **3** 处（版本行 ＋ 本节 ＋ 修订历史一行）。

---
---

### 4.20 放量三档落在哪一列：`system_config_kv` 那一行 JSON 的 8KB 预算，与"送达率根本没有 join 键"的两张表（T-P5-04）

卡面两条 AC：①"每阶段有投诉率/送达率对比数据"；②"一键回滚 = 关开关，不回滚代码"。**schema 侧结论是零新表、零新列、零索引、`migrations/` 零变更** —— 档位落在既有那一行 KV 里。但 AC① 的两项指标今天**算不出来**，而缺的恰好就是"列"：一张表缺一列、另一张表缺一个写入方。所以这一节登记的既是"档位在物理上是哪一行的哪几个字节"，也是"要按档取数还差哪一列"。

#### 落点（3 新增 ＋ 7 修改，共 10 个代码路径；`migrations/` 零变更）

| 路径 | 增/删 | 与库的关系 |
|---|---|---|
| `internal/service/ltc_reach_rollout_test.go` | **新增** 333 行 | 15 条顶层判据：值域、归一化、四档各自的方向、degraded 取严、JSON 往返 |
| `internal/app/reach_rollout_wiring_test.go` | **新增** 517 行 | 11 条：闸门按档判定、两把旗子各管一段、观测面把"算得出来/算不出来"分开 |
| `internal/service/sms_unsubscribed_send_test.go` | **新增** 155 行 | 4 条：退订号码在首发与重发两个入口都不许被记成"已发送" |
| `internal/service/ltc_config.go` | ＋228 −1 | `reach_rollout` 节的解析/校验/归一化/判定与读数口径 |
| `internal/app/reach_gate_wiring.go` | ＋204 −8 | 闸门先问档位再问 W-1；LTC-29 观测面 |
| `internal/router/ltc_routes.go` | ＋16 −1 | GET 回显 `reach_rollout`（PUT 是整份替换，读不回来就改不了档位） |
| `internal/router/tool_debug_routes.go` | ＋12 | `/agent/tools/reach-gate` 载荷里恒带 `rollout_observation` |
| `internal/service/sms.go` | ＋26 −1 | 退订在两处发送口的处置与可注入 |
| 三个 `_test.go`（app 装配基线 / service 台账夹具 / router 两条视图判据） | ＋51 | 夹具与判据，不落库 |

#### 档位的物理形状：一行 `text`，不是十个开关

| 事实 | 实测出处 | 后果 |
|---|---|---|
| 表 `system_config_kv` 只有 `key varchar(100) PRIMARY KEY` ＋ `value text NOT NULL` ＋ 两个时间戳 | `internal/model/system_config_kv.go:6-11` | `reach_rollout` 不是列、不是行，是 `ltc.config` 那一行 JSON 里的一个节 ⇒ **没有任何 SQL 能按档位查历史** |
| 整份文档序列化后 ≤ `LTCConfigMaxBytes = 8 * 1024` | `ltc_config.go:157`、写侧 `Save` 超限即拒 | 8KB 是**整份 LTC 配置**的预算，白名单要从中自己挤；上限存在但不是"想加多少加多少" |
| 名单 ≤ `ltcReachWhitelistMaxEntries = 200` 条、单条 ≤ `ltcReachWhitelistMaxEntryBytes = 128` | `ltc_config.go:386/388`（128 那个数注释里写明取自 `one_id` 的最大宽度） | 白名单档天花板是 200 个对象，超了报错文案直接指路"该走 whitelist→full 这一档"；**它不是批量导入的入口** |
| 判定键形状 `one_id → customer_id → "渠道:收件人"` | `proactive_reach.go:296/310/370` 经 `reachApprovalKey` | 名单条目是**精确等值**比对的键 ⇒ 写侧把三种"看着一样但比不上"的形状全拒：空串、带首尾空格、重复条目（每条错误文案都在说"这样会让哪个数读错"） |
| 档位由闸门每次判定读一次 `Config(ctx)`，进程内缓存 `LTCConfigCacheTTL = 60 * time.Second`，写侧 `Save` 立即失效自己那份 | `ltc_config.go:149-151/577-590`、`reach_gate_wiring.go:290-320` | AC② 的准确表述是**"改档不需要重启、也不需要重新装配"**，不是"改完即全网生效"—— 多副本下对其余副本最迟 60s。这条口径本来就是 `ReadingHints` 的第一条（`ltc_config.go:499`），档位不能对它例外 |

#### 两把旗子各管一段，且这段不许越界

`FF_LTC_REACH_GATE`（env）决定**挂不挂钩子**，`ltc.config` 里的 `reach_rollout.mode`（DB）决定**拦谁**。合起来的四条判据，每条都对应一处"少一档就会读错"：

- **`env=shadow` 时档位只改变记账**（`approvalDenialBlocks` 那道刹车共用），因为三段灰度的对比数据只在"观察期不拦"这个前提下才存在 —— 一边观察一边拦，观察期就白过。
- **`halt` 盖过 W-1 里已有的授权**：判定顺序是"先档位、再 W-1"（`reach_gate_wiring.go:324-336`）⇒ 回滚位真的能把已经放出去那批停下来，而不是"从此不再新授权"。
- **degraded ⇒ 拒**。这是全份 `ltc.config` 里唯一一处"缺省与异常方向不一致"的地方，且是刻意的：读坏时这份内容恰恰就是默认值 `shadow`（= 放行），照默认判等于让一次存储抖动把"白名单灰度中"静默升级成"全量放行"（`ltc_config.go:411-441` 的注释写死了这个理由）。
- **档位挡下的那一笔自己补记一次决策**（W-1 没被问到，它的回调就不会触发），归因只记一次 ⇒ 观测面上 `rollout_not_whitelisted` 与 `denied_default` 是分开的两个理由，运营看到前者不会去灌授权。

#### 送达率不是"接错了线"，是"根本没有 join 键"

观测面把 `delivery_rate_by_cohort` 标成 `unavailable` 而不是给个 0，理由是两张表各缺一样（这一处口径在提交后又被逐条重验过一遍，因为初版把推断写成了事实，见"修订 v1.17"）：

| 事实 | 实测出处 |
|---|---|
| `sms_delivery_statuses.message_id` 是 `varchar(64) uniqueIndex NOT NULL`，全表唯一的归因键 | `internal/model/sms_tracking.go:41` |
| 这张表**只由 webhook 建行**，且入口先 `GetByMessageID` | `service/sms_tracking.go:60-90`（`message_id` 为空直接报错）；路由 `POST /api/sms/delivery/webhook` 由 controller 自注册（`controller/sms_delivery_tracker.go:202`）⇒ 走的是 app 层注册面，`api-inventory` 看不见（§T-P5-02 记过的那条盲区，同一族） |
| `sms_records` **没有存单号的列**（实测列：id/phone/content/provider/status/error_code/error_msg/send_time/created_at/updated_at/deleted_at） | `internal/model/sms.go:53-66` |
| `SmsService.SendSms` 的签名只回 `error` ⇒ 单号在这道边界上就没了 | `internal/service/sms.go:36` |
| 三家 provider 里只有一家解析了单号，**解析完原样丢弃**：`sendAliyun` 把 `BizId` 反序列化进 `result.BizID`，成功分支返回的却是 `time.Now(), Code, Message`；tencent / huawei 连解析都没有，成功分支返回硬编码 `"OK", "OK"` | `sms.go:346-358`、`:442`、`:492`（`dispatchToProvider` 的四元组是 `(sentTime, errCode, errMsg, err)`，第二三个是错误码不是单号） |
| 外发链路上行的是常量 `sms_out`，或 `"sms-"＋手机号` | `app/reach_sender_wiring.go:119`、`service/proactive_reach.go:754`；`app/integration_reach_adapter.go:224`（后者把手机号编进了"单号"，本卡登记未改） |

⇒ **外发链路一个字符都没往 `sms_delivery_statuses` 写过**；那个常量 `sms_out` 是"算不出来"的原因之一，不是"已经写坏"的证据。真要照它建行，唯一索引会把所有外发挤成同一行 —— 这一句是**对未来接线的警告**，不是对现网的描述，写在 unblocker 里而不是 reason 里。补的顺序是：provider 侧先把单号取出来 → `SendSms` 改回传签名 → `sms_records` 加单号列 → 才谈得上按单号 join 送达表。

另两项同理都缺"一行/一列"：`complaint_rate_by_cohort` 的分子**全仓没有持久化写入方**（complaint 只作为常量与瞬时的意图标签存在）；`sends_by_cohort` 没有发送账本，闸门判定只进进程内计数器（重启即空、多副本不合并 ⇒ 那格即便可算，口径也只是**下界**）。

#### 顺手补上的两处合规缺口：退订判据原来有两个入口漏了

这两处在 schema 上的意义是**"送达率的分子不该有它们"**：退订号码一条都不该发出去，因此也不该进任何按档对比的分母。

- `smsService.SendSms` 命中退订名单时原先 `return nil` ⇒ 每个调用方都读成"发成功了"（外发链据此记成功、烧幂等键、把它算进送达率分母）。改为返回既有哨兵 `ErrDoNotContact`（`sms.go:240-245`），**不需要新增任何下游分支**：队列 worker 与 SOP 节点早就按 DNC 处置（cancelled ／ `skip:dnc` 且**不烧** `reach_sent:` 幂等键）。错误串不带手机号 —— 这条串会落进队列台账。
- `ResendSms` **从来不查退订**（`sms.go:278` 起它直连 `dispatchToProvider`，绕过了首发那道检查），而"首发之后用户才回 TD"是常态 ⇒ 补同一条判据，且放在改状态**之前**：拦下时不该在 `sms_records` 上留一条"正在发"。
- 配套把退订查询做成可注入（`SetSmsUnsubscribe`）：包级单例在**第一次调用时**才解析全局 DB 句柄，于是这条判据读到的是"谁先跑"而不是"配了哪套库"（`sms.go:200-228`）。这是本仓既有测试口径老问题的一个具体实例，登记在此而不是当成风格改动。

#### 交付口径：AC 各被什么钉住

- **AC②（一键回滚 = 关开关，不回滚代码）** —— 由"`halt` 盖过已有授权"＋"档位每请求读（≤60s 缓存窗口）"两条判据合起来钉住；`TestLTCReachRollout_HaltDeniesEverythingWithItsOwnReason`、`TestLTCReachRollout_DegradedReadDeniesAndSaysWhy`、`TestReachRollout_*`（app 侧 11 条）。
- **AC①（每阶段有对比数据）** —— **交付的是口径而不是四个数**：`/agent/tools/reach-gate` 的 `rollout_observation` 明写"哪一条算得出来、哪一条算不出来、各缺哪一行代码"，`unavailable` 三项一律**不带值**（0 投诉率会被直接读成"这一档安全，可以进下一档"）。判据句由指标列表**推出来**（`rolloutComparisonVerdict`），不是写死的措辞 —— 否则补上一列之后那句话还在说"缺三项"。
- **读/写对称** —— `GET /manage/ltc/config` 必须回显 `reach_rollout`：PUT 是"读回来改一处再整体写回"的形状，这一节不在 GET 里 ⇒ 改档位会顺手把灰度名单清空（`TestLTCConfigView_ShowsRolloutModeAndCohort` 钉住，含"`gin.H` 存的是命名类型 `service.ReachRolloutMode`"这个断言坑）。

#### 实跑（`--shared` 克隆 `/tmp/p504gate/hivemtk`，HEAD `d94810d8`，克隆工作树 0 脏文件）

- `go build ./...` rc=0、`gofmt -l` 对本卡 3 个包无输出、`go vet ./internal/service ./internal/app ./internal/router` rc=0。
- `./internal/app ./internal/router -p 1` **两时区各 rc=0**（CST 27.156s／6.022s，UTC 17.431s／7.461s）。
- `./internal/service -run 'TestSms|TestMarketing|TestSop|TestRecovery|TestProactive|TestReach|TestLTC|TestDoNotContact'` **两时区各 rc=0，锚定 `^--- PASS` 各 232 条顶层**（27.086s／23.735s）。这 232 与静态同名集合逐条比对：活树与克隆**各 232、diff 为空**。（另记一次口径纠正：活树早前用**非锚定** `grep -c '--- PASS'` 量到 280，那是含缩进子用例行的另一个数，不能与本卡的 232 混引。）
- `./internal/controller -p 1` 两时区各 rc=0（125.451s／110.583s）。
- `./internal/service` **全量**：`TZ=Asia/Shanghai` **rc=0，pass=3617 fail=0 skip=3，451.778s**；`TZ=UTC` **rc=1，pass=3616 fail=1 skip=3**。唯一一条红**不是本卡的**：`TestAbExperiment_LogExposureFireAndForget`（`ab_experiment_test.go:50`）断"5 次写进 2 格缓冲 ⇒ 恰好丢 3"，而 `NewABExperiment` 在构造函数里就起了消费协程（`ab_experiment.go:79-90`）⇒ 消费方抢到一次时间片就少丢一个（实得 2）。归因证据：单跑 `-count=20` 全绿、**带并发负载 `-count=400` 复现 2 次同一条红**、该用例由 `8fecb6ad` 引入（与 P5 无交集）。登记为负载相关假红，修法要动生产构造入口（不该在这条路上顺手改别人的测试），见"仍不成立"。
- 十道门 **rc 全 0**：`make fmt-check`／`check-unwired-assets`（**58/68，与本卡开工前同值 ⇒ 本卡零台账行变更**）／`check-date-bucket-tz`（21/21）／`check-enum-consistency`（3 警告）／`audit-cross-package-ports`（E0／W6）／`check-architecture`／`check-doc-consistency`（3 警告全是同 workspace 下 `hivemtk-platform/` 缺文件）／`check-feature-doc`（通过 1／失败 0／跳过 1）／`check-secrets`／`api-inventory`（**生成物零 diff** ⇒ 本卡无新端点，两处新载荷都挂在既有端点上）。
- 变异电池（10 格，覆盖两条发送口、档位短接、判据推导、degraded 方向、halt 放行、白名单反向、router 少观测块）：**控制组各自 ran=登记数、0 fail 0 skip（service 4／app 11／router 11），KILLED=10／SURVIVED=0**，还原后逐文件 md5 与基线一致（独立复查：10 个文件全部 "restored"，`RESTORE_CHECK_BAD=0`）。

#### 本卡之后仍不成立（写清不藏着）

- **三项指标仍算不出来**，各自的 unblocker 就在 API 载荷里；补它们要动的是 `SmsService` 接口签名与三家 provider 的响应解析，属独立一张卡。
- **`one_id` 三种宽度**（100/128/`text`，§4.18 记的债）在本卡多了一处消费点：白名单条目上限取 128 就是照它定的，**但列本身没合流**，所以同一份名单在窄列那两桌上仍然装得下、在 `text` 那桌上口径最松。
- **没有前端编辑面**：`grep -rn reach_rollout user-web/src` 命中 **0** ⇒ 档位与名单只能经 `PUT /manage/ltc/config` 改，且那是整份替换的写口。
- `BlockFromPhone` 之外仍有一处 **PII 进"单号"**：`integration_reach_adapter.go:224` 返回 `"sms-"+手机号`。
- **aliyun 发送用的是硬编码测试模板** `TemplateCode = "SMS_000000001"`（`sms.go:305`）：不在本卡范围（改了要真号验证），但送达率这条链上它是"外发根本发不出去"级别的前提，登记不修。
- 台账 58/68 一字未动 ⇒ 本卡没有新增"接了线没人读"的资产，也没有把任何一格转成 wired。

### 4.21 报价的两张表：一行 = 一个版本，"旧版不可变"是主键形状的结果而不是一条规矩（T-P6-01）

卡面两条 AC：①"同一 `quote_id` 多版本共存、版本单调递增、旧版不可变"；②"客户还价 → 新版本由系统生成而非覆盖（LTC-12）"。本卡交付 **2 张新表、19 列、5 条索引、`migrations/` 零变更**；下面先给直读 `information_schema` / `pg_indexes` 的实测形状，再说清每一格为什么长成这样。

#### 落点（2 新增表 ＋ 5 个文件；建表登记走 `allModels()`）

| 路径 | 增/改 | 与库的关系 |
|---|---|---|
| `internal/model/quote.go` | **新增** 202 行 | 两张表的列、值域、索引标签 |
| `internal/model/quote_test.go` | **新增** 317 行 | 9 条标签层判据 |
| `internal/pkg/db/quote_migration_test.go` | **新增** 420 行 | 6 条真库判据（列集合、复合索引、主键、numeric） |
| `internal/repository/quote.go` | **新增** 436 行 | 版本链的唯一写路径 |
| `internal/repository/quote_test.go` | **新增** 665 行 | 12 条仓储判据（含 8 协程并发追加） |
| `internal/pkg/db/migrate.go` | ＋6 | `&model.Quote{}` 与 `&model.QuoteLineItem{}` 两处登记 |
| `scripts/check-unwired-assets.sh` | ＋14 | 项 21 两行，均按 `UNWIRED` 登记 |

`migrations/` 零变更：本仓生产建表只跑 GORM `AutoMigrate`（启动期版本化迁移固定 `v1.0.0→v1.0.0` 是空跑，T-P2-01/04/05/06、T-P3-01/03、T-P4-01 七次实测同源）。卡面写的 `v3_49_0_quote_migration.go` 因此**不产出**，与 T-P4-01 的 `v3_48_0_...` 同一处置。

#### `quotes` 实测形状（10 列 4 索引）

| 列 | PG 实测 | 默认 | 为什么是这个形状 |
|---|---|---|---|
| `id` | `text NOT NULL`（`quotes_pkey`） | — | 版本行的主键。它被明细表的 `quote_row_id` 抄走，所以必须是**自生成的稳定键**，不能是 serial |
| `quote_id` | `varchar(64)` | — | 一条链的对外身份。**故意不带唯一索引**（见下） |
| `opportunity_id` | `varchar(64)` + `idx_quotes_opportunity_id` | — | 宽度由下游 `sales_events.opportunity_id` 的 varchar(64) 定；索引给它是为了按商机取报价 |
| `version` | `bigint NOT NULL DEFAULT 1` | `1` | 与 `quote_id` 共一条复合唯一索引。`not null`+`default` 成对是**存量表补列**过得去的前提（PG 的 `ADD COLUMN … NOT NULL` 不带默认值会当场失败） |
| `status` | `varchar(16)`，**无索引** | — | 五个取值（draft/sent/accepted/rejected/expired）。不建索引的判据与 `opportunities.status` 同一条：本卡没有"全站按状态捞"的读方 |
| `source_id` | `text` + `idx_quotes_source_id` | — | 指向**基准版本行**的 `id`。它是 AC② 的物证：这一版是从哪一版长出来的 |
| `valid_until` | `timestamptz` 可空 | — | 指针类型：未设有效期必须是 `NULL`，不能是零值时间 —— 后者会被"过期报价"的聚合读成"公元 1 年就过期了" |
| `currency` | `varchar(3)` | `'CNY'::varchar` | 只在表头。行项目上**没有**币种列：一版里混两种币种时"合计"这个概念当场没有定义 |
| `created_at`/`updated_at` | `timestamptz` | — | — |

```
quotes :: quotes_pkey                 UNIQUE (id)
quotes :: uq_quotes_quote_version     UNIQUE (quote_id, version)     ← AC① 的唯一硬保证
quotes :: idx_quotes_opportunity_id   (opportunity_id)
quotes :: idx_quotes_source_id        (source_id)
```

四条索引，**没有一条**是单列 `quote_id` 唯一。那条索引是 AC① 的反面：它表达的是"一版一号"，装上之后同一张报价单的第二版永远插不进去。这也是本卡必须在**真库**里点名列集合的原因 —— GORM 对"两列各写一次同名 `uniqueIndex:`"的处理是它自己拼一个复合索引，两列的名字只要差一个字符，它就建成两个单列索引：**表能建、插入照样重复、测试全绿**。判据写在 `TestQuoteCompositeUniqueIndexIsReallyComposite` 的四臂（pg_index 列集合、v1/v2 共存、同版本第二次被拒且报的是这条索引名、不同 `quote_id` 复用版本号必须允许）。

#### `quote_line_items` 实测形状（9 列 1 索引）

```
quote_line_items :: quote_line_items_pkey  UNIQUE (quote_row_id, line_no)
```

| 列 | PG 实测 | 为什么 |
|---|---|---|
| `quote_row_id` | `text NOT NULL`（主键之一） | 指向**某一版**，不是指向"这张报价单"。所以新版必须重抄一遍自己的行 |
| `line_no` | `bigint NOT NULL`（主键之一） | 同一版内唯一。**不同版复用同一行号是必须的** —— 否则"第二版的第一行"插不进去，链直接断在明细上（这一臂是 `TestQuoteLineItemPrimaryKeyIsPerVersionRow` 的全部价值） |
| `product_id` | `varchar(64)` | 与 `rag_products` 那边的 `id size:64` 同宽，不另起口径 |
| `title` | `text` | 商品名的副本：报价要能重放**当时**的名称，不能靠 join 现值 |
| `quantity` | `numeric(12,2)` | 数量可以是 0.5 个套餐年，所以不是整数 |
| `unit_price` / `amount` | `numeric(14,2)` | 与 `order_drafts` 同宽（草稿与报价明细装的是同一类东西）；`quotes` 表头**没有**金额列 |
| `discount_percent` | `numeric(5,2)` | 上限 999.99 足够装折扣百分比 |
| `created_at` | `timestamptz` | — |

**这张表没有代理主键**，而这正是"旧版不可变"最省事的表达：一行明细的身份是"哪一版的第几行"，于是**原地改一行在物理上不成立**（改 = 换主键 = 换一行）。所以"旧版不可变"不是一条要人守的规矩，而是形状的直接结果；仓储接口里也没有任何 `Set*`/`Replace*`/`Delete*` 方法（`TestQuoteRepo_InterfaceHasNoContentRewriter` 钉住接口形状，唯一的 `Update` 是 `UpdateStatus`）。

#### 表头上刻意没有的四样东西

| 没建 | 理由（都是别的表已经有的东西，抄过来会立刻分家） |
|---|---|
| 任何合计列（`total`/`subtotal`/`amount`） | 唯一事实源是行项目之和（T-P6-02 AC③ 要断言"合计与落库一致"，两处各存一份就没有"一致"可言） |
| 任何审批列（`approval_id`/`approved_by`） | `approval_requests` 是审批状态唯一来源；在报价上再放一份就是 §G13/G20 那一类"两份判据谁说了算"的翻版 |
| 行项目上的 `currency` | 混币种时合计没有定义；换币种只能另起一版 |
| 租户列（`tenant`/`org_id`/`corp`/`workspace`/…） | X3 单商户（ADR-014 已 Simplified）。真库侧也有一条负判据守着（`TestQuoteHasNoTenantColumnAtDBLevel`） |

#### 版本链的写路径（`internal/repository/quote.go`）

三条且只有三条：`Create`（第一版，`Version` 由本层补成 1、`SourceID` 强制清空）、`Append`（`Version = 基准 +1`、`SourceID = 基准行 id`，两者都不取调用方那份）、`UpdateStatus`（`WHERE id = ? AND status = ?` 的 CAS，写集合只有 `status` + `updated_at`，**`version` 不动** —— 改状态不产生新内容）。

- 调用方自带版本号 ⇒ `ErrQuoteVersionReserved`（报错而不是静默换算：那说明它想做的是"覆盖"，本层不支持这件事）。
- 并发以同一基准追加 ⇒ 八个里恰好一个赢，其余七个收 `ErrQuoteVersionConflict`（`23505` **且** 索引名两个条件都命中才算，把主键冲突读成版本冲突会让重复提交变成链上多一版）。裁决权在库级索引而不是进程内锁：多实例部署下锁只管得住半个链。
- 跨链追加（`quote_id` 或 `opportunity_id` 与基准不符）整条拒；基准行不存在 ⇒ `ErrQuoteNotFound`，且被拒的调用**一行不留**。
- 明细的存在性探针在写入**之前**、整批一个事务：孤儿明细让报表显示"这张报价没有行项目、合计 0"，半截批次让合计变成一个**看起来合理**的错误数字。
- `Latest` 按 `version DESC` 选版，不按 `created_at` —— 补录一版旧内容时两者会分家，而"当前报给客户的是哪一版"只能有一个答案。

#### 值域（Go 侧，库里无 ENUM）

`draft / sent / accepted / rejected / expired` 五取一，`QuoteStatusKnown()` 逐字比、不做规范化。库里没有 CHECK 约束，与全仓取向一致（值域散在 PG ENUM、CHECK、Go 常量三处是 §八 记的债，本卡不再加第四种形状）。

#### 本卡实跑

- 三层 **27 条顶层判据全绿**：`internal/model` 9 条、`internal/pkg/db` 6 条、`internal/repository` 12 条（`-count=1 -v` 的锚定 `^--- PASS` 计数）。
- 三包全量在 **`TZ=UTC` 与 `TZ=Asia/Shanghai` 两条腿都绿**（db 81.3s / repository 83.1s；第二腿 70.0s / 79.2s），`go build ./...` rc=0。
- 变异电池 8 格（冲突判定、版本递增、存在性探针、CAS 条件、行序、选版依据、跨链守卫、白名单外溢）**全部 KILLED**，红因逐条点名到用例；还原后 `quote.go` md5 与基线一致。其中两格第一版是**变异脚本自己写坏**（`msg` 变量失去唯一引用 ⇒ 编译错；一处多敲的空格 ⇒ 语法错），修变异不修期望后重跑才拿到 KILLED。
- 台账 **58/70**（本卡 +2 行，均为 `UNWIRED`）；反向验证：把其中一格改成 `wired` 后门立即 rc=1 报"登记为已接线却无调用点"，改回 rc=0。

#### 本卡之后仍不成立（写清不藏着）

- **零生产写入方**：`quotes` 表今天只有测试在写。台账项 21 两格就是登记这件事的，翻 `wired` 的条件是 T-P6-02 的装配点出现。
- **没有 HTTP 出口、没有前端**：报价的读方要等 T-P6-03/04。所以"报价域已落地"这句话今天的准确形状是"schema 与仓储已落地"。
- **版本链的"作废"没有表达**：五个状态里没有 `withdrawn`/`obsolete`。当前口径是"被取代"由链本身表达（`Latest()` 之外皆历史），若 T-P6-03 判定需要一个显式状态，那是加一次值域、不是加一列。
- `quote_id` 与 `id` 的**生成器还没有**（本卡只交付容器）：宽度上限 64 由 `quotes.quote_id varchar(64)` 与 `quote_line_items.product_id varchar(64)` 定死，构造器落在 T-P6-02。
- 一版里行的**数量与金额区间**没有任何校验（仓储无业务判断，`TestQuoteRepo_DoesNotValidate` 钉住）：`discount_percent` 现在写得进 12.34 也写得进 500，阈值归 T-P6-04。

---

## 五、索引策略

### 5.1 单列索引

仅在高频过滤字段建：

| 字段 | 索引类型 | 理由 |
|------|----------|------|
| `status` | btree | 按状态筛选 |
| `created_at` | btree | 按时间范围筛选/排序 |
| `deleted_at` | btree | 软删除过滤 |

### 5.2 复合索引（最左前缀）

| 场景 | 索引 | 顺序 |
|------|------|------|
| 客户列表 | `(status, created_at DESC)` | status 选择性高 |
| 触达历史 | `(customer_id, platform, created_at DESC)` | 客户维度高 |
| 知识库内容检索 | 无复合索引；实为 `idx_knowledge_chunks_product_id` 等**单列** btree | 登记的 `(kb_id, doc_type, status)` 经 T-P2-05 实测**在库里不存在、列名也对不上**：`knowledge_documents` / `knowledge_chunks` 两张内容表都没有 kb_id 列（检索按 `product_id` 过滤，见 G18），2026-08-25 快照照抄了别的系统的索引口径。直读 `pg_indexes` 的实算是：这两张表上全部是 GORM tag 生成的单列索引（`product_id` / `document_id` / `embed_status` / `content_hash` / `source_language` …）加 `content_tsv` 系 GIN 与 `embedding` 的 HNSW，**零条复合索引** |
| 报价版本链（`quotes`） | `uq_quotes_quote_version (quote_id, version)` **唯一** | `quote_id` 定链、`version` 定链内第几版；两列**必须**一起唯一 —— 单列 `quote_id` 唯一等于"一版一号"（第二版插不进去），单列 `version` 唯一等于"全站版本号不许重复"（第二张单的第一版插不进去）。明细表 `quote_line_items` 反过来**没有**任何单列索引：它的 `(quote_row_id, line_no)` 复合主键就是全部，而"同版本重复行号被拒、不同版本复用行号必须允许"这两臂正是钉在这条主键上的（T-P6-01） |

### 5.3 JSONB 字段（必须 GIN 索引）

| 字段 | 索引 |
|------|------|
| `roles.permissions` | GIN(permissions) |
| `sop_templates.steps` | GIN(steps) |
| `reach_pipelines.steps` | GIN(steps) |
| `feature_flags.config` | GIN(config) |

### 5.4 向量索引（pgvector HNSW）

```sql
CREATE INDEX idx_knowledge_chunks_embedding 
  ON knowledge_chunks 
  USING hnsw (embedding vector_cosine_ops)
  WITH (m = 16, ef_construction = 64);
```

- 维度硬性 **1024**（与 bge-m3 一致）
- 算法：HNSW（召回率优于 IVFFlat）
- 距离：cosine

---

## 六、事务与并发

### 6.1 事务边界

- **业务事务**由 Service 层封装（`s.repo.Transaction(func(tx) { ... })`）
- **跨表一致性**通过事务保证
- **禁止** Repository 内部开启事务（应交由 Service 编排）

### 6.2 行级锁

- `SELECT ... FOR UPDATE` 用于"读-改-写"场景（如库存扣减）
- 软删除通过 `DeletedAt IS NULL` 过滤，不使用悲观锁

### 6.3 分布式锁

- 跨实例互斥通过 Redis `SETNX` 实现（如转人工熔断、活码轮询）
- TTL 根据业务场景设置（5s ~ 永久）

---

## 七、迁移管理

### 7.1 迁移目录

```
hivemtk/user-server/internal/migration/migrations/
├── initial_schema.go                    # 初始 schema
├── v3_xx_*.go                           # 各版本迁移
├── unmultitenant_migration.go           # 移除多租户
├── merchant_id_nullable_migration.go    # merchant_id 可空
└── check-migrations.sh                  # 迁移完整性检查
```

### 7.2 迁移原则

- **幂等**：每条迁移支持重复执行（`IF EXISTS` / `IF NOT EXISTS`）
- **回滚支持**：复杂迁移提供 `Down()` 方法
- **备份优先**：破坏性迁移（删除列/表）前先备份
- **灰度发布**：大表迁移分批执行（每次 ≤ 10000 行）

### 7.3 迁移检查清单

迁移前必须确认：

> 说明：以下为**每次执行迁移前逐项确认的运维检查单**，非待完成的开发任务，故常态保持未勾选。

- [ ] 不破坏已有索引
- [ ] 不删除仍在使用的数据
- [ ] 字段类型变更不丢数据
- [ ] 默认值设置正确
- [ ] CI 单测覆盖
- [ ] 灰度方案明确

---

## 八、ENUM 策略

### 8.1 选用 PG ENUM 的判断标准

满足**任一条件**即升级：

1. **高频字段**：单表 > 1 万行 + 索引
2. **枚举稳定**：6 个月内不会新增超过 2 个值
3. **写脏数据曾经发生**
4. **跨服务共享**：多个 service 都引用同一组枚举

### 8.2 命名规范

| 元素 | 格式 | 示例 |
|------|------|------|
| Type 名称 | `<scope>_type_enum` | `platform_type_enum` |
| 取值 | 小写下划线 | `xhs`, `customer_service` |

### 8.3 核心 ENUM 定义

```sql
-- 平台
CREATE TYPE platform_type_enum AS ENUM (
  'xhs','douyin','tiktok','kuaishou','xianyu',
  'wechat','whatsapp','telegram','wecom','feishu',
  'email','sms','system'
);

-- 意图类型
CREATE TYPE intent_type_enum AS ENUM (
  'purchase','inquiry','support','complaint',
  'greeting','objection','follow_up','negotiation',
  'closing','unknown'
);

-- 消息状态
CREATE TYPE message_status_enum AS ENUM (
  'pending','sent','delivered','read',
  'failed','withdrawn','blocked'
);

-- 文档类型
CREATE TYPE doc_type_enum AS ENUM (
  'faq','sop','product','policy','script','manual','other'
);
```

详见 [pg_enum_strategy.md](pg_enum_strategy.md)

---

## 九、修订历史

| 版本 | 日期 | 修订人 | 内容 |
|------|------|--------|------|
| v1.0 | 2026-08-16 | @data-platform | 初版数据库深度解析（合并散落文档） |
| v1.1 | 2026-09-19 | @backend | 新增 §4.11 统计看板真实源与演示表：标注 `conversion_funnels` 为僵尸表（R-4 / T-P2-03 收口），给出两套阶段名、"不得写入"规则、删表前三条判据，并登记真实源的两处口径问题（短板 G16） |
| v1.2 | 2026-09-19 | @backend | 新增 §4.12 销售事件流 `sales_events` 的 LTC 预留列（R-5 / T-P2-04）：给出两列的可空/不回填/暂无索引/暂无生产者四态，写明 `NULL` 与空串是两层含义且 GORM 读回会塌成同一空串，并把"整张表今日生产零写入"登记为 `check-unwired-assets.sh` 项 9；顺带把文档头版本号从 v1.0 对齐到修订历史 |
| v1.3 | 2026-09-19 | @backend | 新增 §4.13 知识库版本与灰度的三列（R-6 / T-P2-05）：给出 `version/canary_enabled/canary_percent` 的 PG 实测类型与"唯一读者是缓存命名空间折算"的定位，写明 `not null` 是必需项、写这三列必须走 `UpdateVersionCanary`（否则会 bump `updated_at` 而误删对侧缓存），并实算修正 §5.2 的知识库索引行（登记的复合索引在库里不存在）。**本行为 v1.4 补登记**：§4.13 落地时只改了文档头版本号，漏了本表 |
| v1.4 | 2026-09-19 | @backend | 新增 §4.14 审批检查点 `approval_requests`（N-4 / T-P3-01）：直读 `information_schema` + `pg_indexes` 给出列/索引实测形状，写明两条唯一索引**必须**是部分的（丢了谓词会把闸门锁死 / 让第二条 auto-approve 撞空串）、身份四列在 PG 可空而判据在 `normalize()`、裁决写回的列白名单与 CAS，以及本卡"零构造零读取、`ExpireOverdue` 暂无按节拍调用方"的前置事实（项 12 / 短板 G20）；同时补记 GORM `tx.Model(&a)` 会追加主键条件、必须传空壳这条实测坑 |
| v1.5 | 2026-09-20 | @backend | §4.14 的接线现状改写（N-4 / T-P3-02）：装配入口/恢复读入口/到期清扫三处生产调用点落地，`check-unwired-assets.sh` 项 12 由两行 `unwired` 扩为三行并全部翻 `wired`（`ExpireOverdue` 那条用"两个实参"的形状与草稿侧同名方法分开，否则删掉清扫器不会红）；同时写清旗子边界 —— `FF_LTC_APPROVAL_RESUME` 默认 `off`（该档下运行时不构造、表仍零写入），`shadow` 只清扫不挂起，布尔真值降档到 `shadow`，所以"这张表有生产写入方"只在推 `on` 之后成立。v1.4 那句"零构造零读取"作为历史事实保留在修订历史里，正文已按其被推翻的部分改写 |
| v1.6 | 2026-09-20 | @backend | 新增 §4.15 统一人工待办 `human_tasks`（N-9 / T-P3-03）：直读 `information_schema` + `pg_indexes` 给出 19 列 9 索引的实测形状，写明 C3"统一模型 + 分离视图"落到库里就是**一条状态机 + 三列互斥 SLA**（为什么不是一列 `sla_due_at`：AC④ 的指标隔离必须能被查询表达），开放唯一索引 `uq_human_task_open` 为什么**只能**写成"排除两个终态"的否定式（GORM tag 用逗号切段，`IN ('a','b')` 放不进去），四个状态里为什么刻意没有 `expired`（逾期是读数不是状态，落成终态等于指标把自己要暴露的问题抹平），以及长度上限为什么分"身份即拒 / 展示裁断"两档、且裁断按字符不按字节。同时登记一条与卡面的**偏离**：卡面要求的 `v3_47_0_human_task_migration.go` 不存在且不该存在 —— 本仓版本化迁移在启动路径上固定空跑，建表登记只有 `allModels()` + `migrate_test.go` 的 `mustCover` 两个落点 |
| v1.7 | 2026-09-20 | @backend | **补登记**（v1.3 那行的反向形状：那张卡只加了正文小节 §4.16，既没改文档头版本号也没进本表）：新增商机表 `opportunities`（N-1 / T-P4-01）。内容为直读 `information_schema` + `pg_indexes` 的列/索引实测形状，核心是 `stage`（四格过程）与 `status`（四态结果）**分列不许合并** —— 合成一列的后果是漏斗末格与赢单率塌成同一个数（C5 在字段层的翻版），`OpportunityClosed`（含 cancelled）与 `OpportunityOutcomes`（不含）因此拆成两个判据；另登记本卡唯一一处被测试逼出来的改设计：首版给 `status` 写了 `index`，被自家「每条索引必须点名一个已存在下家」的用例当场判红后摘掉 |
| v1.8 | 2026-09-20 | @backend | 新增 §4.16.1 商机仓储层（N-1 / T-P4-02）：补 `version bigint not null default 0`（§4.16 表随三处更新 —— 可空性由「只有 `id` 是 NOT NULL」改为「`id` 与 `version`」、`currency` 改称「唯一带**语义**默认值」的列并注明 `version` 的默认是补列前提、反向无索引列由 7 增至 9），写明并发口径选 **CAS 而不是 `FOR UPDATE`** 的失败面差别、改写白名单就是那个 10 键 map（附两条只有真跑才暴露的 GORM 静默失效：`Select(清单)` 会吃掉 `version + 1`、struct 形式 `Updates` 跳零值），以及读侧三条纪律（空状态集报错而非回全表 / `limit<=0`、`offset<0` 由本层拒且判据写成「错误来自本层」 / 排序带 `id DESC` 兜底键、并列行逐位比对次序）。变异电池 25/25 捕获、存活 0、坏变异 0；未接线台账项 16 拆为 16a/16b 两行（现值 38/46） |
| v1.9 | 2026-09-20 | @backend | 新增 §4.16.2 商机**服务层**（N-1 / T-P4-03）：§4.16「本卡欠三件」改为两件已交付（跃迁合法表与赢率计算式 → 已随 T-P4-03 交付）。三条实测口径进文档：① 跃迁规则「阶段向前恰好一步、向后任意步、**同格不算跃迁**」+ status 按 **(来源, 起点, 终点)** 三元边表存 —— AC③ 那句「本层写 won 的唯一入口」是靠这张表实现的，不是靠「源码里只有一处赋值」；终态不重算赢率，幂等只给「事实的重复上报」（同一条回款完成第二次到达＝成功且零改写）。② 赢率式 `p = round₂(base(阶段) − 无归属 0.10 − 已逾期 0.15)`、下限 0.05：**可证明的只有单调形状**（逐格向前 ⇒ 集合嵌套 ⇒ `P(赢|到第 k 格)` 递增），四个 base 是显式登记的**先验刻度而非标定值**（全表零生产写入方，没有可回标的收口行），惩罚只做常数不做逾期梯度，且禁止与 `confidence`/`lead_score` 跨域乘算（C5；`OpportunityWinInput` 三字段由反射用例钉住）。③「谁负责把值舍到两位」这条接口判据：真库用例**证不出**（numeric 列自己会舍，读回来两边一样），第一版变异电池里「摘掉落库前舍入」就是这么活下来的，补一条绕开 PG、直接断 `Update` 收到的浮点 payload 的用例后才杀掉。未接线台账项 16 加 **16c 商机服务的装配入口**（与 16a 分开登记：接线有两个断点，只盯一个则另一个断了没人知道），16b 本卡刻意不翻；现值与实跑数字见变更记录 r43。本卡用例 17 条、变异 31 条全部被杀 |
| v1.10 | 2026-09-21 | @backend | 新增 §4.16.3 商机 **HTTP 出口**（N-1 / T-P4-04）：八条端点（读二 + 规则一 + 写五），**没有一条能写 `won`** —— 那是 §4.16.2 三元边表在 HTTP 侧的兑现，由路由表逐条比对的用例守着（加一条 `POST /{id}/won` 即红）。四条判据进文档：① 绑定 `DisallowUnknownFields` + 请求体封顶 4KB（派生量与身份列在入参结构里根本没有格子，默认丢弃未知字段会让 `{"win_probability":0.99` 静默成功、两边各持一套账；`MaxBytesError` 单列一臂，不混进"形状不对"）；② 写入口必须带 `version`，缺字段判 400 而不是取零值（0 恰好是新行的合法期望版本）；③ 400/404/409/503 共用一套分诊词表，503 的判据是"不许有看起来像结果的 data"（`{} [] null` 都会被前端长成"查过了，没有"）；④ `GET /rules` 未装配时照样答，且给出"存在但不暴露"的机器动作清单，否则这套规则在契约面上读成"产品没有赢单"。controller 不 import repository（架构门 [1/10]），两种 404/409 判定的事实来源由**服务层别名**送出（同款先例 `human_task.go`）。**本卡三把被真跑纠正的断言**（变异电池 38 刀 → 37 CAUGHT / 1 把 M27 登记为等价）：M11 的夹具自己是个语法错误的 JSON，那句 400 来自"我打错了"而不是体积 ⇒ 用例从第一天起假绿，现在夹具先自证能 `Unmarshal` 且确实 >4KB；M17 摘掉控制器空 id 关在**读口**看不出差（服务层 `Get` 也拒空），差别只在写口（`transition` 不判空 ⇒ `PUT /api/opportunity/%20` 白查一趟并回 404"这条不存在"），改用 `failingOpportunityRepo` 断"根本没查"（一查就是 500）；M21（`closed` 与 `stale_version` 换序）查下来是**真等价变异**（服务层一次只返回一个 sentinel，两条 `errors.Is` 永不同时为真），换成同族里有牙的那把（把已收口的行贴成 `stale_version` 的 reason）。另登记一处两边都看不见的刀：`router.go` 摘掉 `app.InitOpportunityRuntime` 之后装配函数、控制器、挂载函数三个字面量全在、端点也在树上，只是运行时全局句柄永远 nil ⇒ 八条端点全退 503；旧路由用例漏它有两个原因（只看 `engine.Routes()` ⇒ 挂上≠活的；匿名探针判"非 2xx" ⇒ 503 也是非 2xx），现由带合法令牌真读一行的用例 + 台账新增 **16e 启动装配点** 双守，两把都反向验过。未接线台账：16a/16c 比 §4.16.2 的原计划**早一张卡**同时翻 wired（出口必须自带底座，一次装配接上两个断点，判据仍分开跑），新增 16d 挂载入口 + 16e 启动装配点，16b 仍按 UNWIRED 登记（兑现点 T-P4-05）；台账现值 **42/49**（rc=0）。**列表端点刻意不交付**：仓储 `List` 不返回 `total`，用 `len(list)` 凑数会把"一共多少条"从"查过"变成"猜的"，登记为欠账。`swag` 未随本卡重生成（前几张卡同一口径：工作树里有并行会话未审阅的注解，重生成会一并灌入），Swagger 判据为静态的八行 `@Router` 逐条锁。实跑（提交 292c92e3 的 --shared 克隆）：router 150 / controller 781 / app 164 / service 3509 全绿、失败 0，`TZ=UTC` 复跑四包同数同绿；`-race` 四腿：router 150 / app 164 / controller 781 三腿 **race=0**，service 腿 **rc=1 pass=3507 fail=2 race=2** —— 两处 DATA RACE 在**父提交 `11755c55` 的全量 `-race` 实跑里复现同一对栈**（那里 race=1 fail=1），判为既有缺陷、非本卡引入，取证与口径见 §4.16.3 末段 |
| v1.11 | 2026-09-21 | @backend | 新增 §4.16.4 **线索→商机的转换层与自动分配**（N-1 / T-P4-05）：`opportunities` 的第一个生产写入方落地，P4 出口条件里「商机已入库」那句话从此成立。三道串行判据（量程 → 阶段 → 双阈值；两个同名 `confidence` 量程不同 ⇒ 硬拒不归一；`StageActive` 排在 nil 不安全的 `LeadQualified` 之前；配置降级按「关」处理）；AC② 落成「转换那一步对 `clues.is_opportunity` **零写入**」，并**就地推翻 T-P4-01 写在自己代码注释里的那句「不建索引、反查由 is_opportunity 承担」**（0/1 答不出「转成了哪一条」，且那一列是挖掘侧的热度标记、不是转化事实）⇒ 反查走 `clue_id` + **部分**唯一索引 `WHERE clue_id <> ''`，DDL 落在只 Warn 不清数据的 `postMigrateOpportunityClueUniqueIndex()`；AC③ 落成返回值而不是日志行（规则名 + 候选集 + 每人负载一起出，候选必须按 `SalesID` 升序是平票裁决的前提），三条规则顺序与「故障绝不退到 `no_roster`」严格分开；名单真源 `sales_events`/`sales_profile`（候选 `sales_personas` 实测零写入方被否），「在册」只能等于「注册过档案」（无停用事件，登记为边界而非现场补一个没有写者的死列）；接缝只在 `lead_mining.persistLead` 两条分支、且都在线索行落库之后（`Create` 失败不转 —— 本表不建外键，断链无人发现），未装配静默、报错只 Warn 不重试不回滚；装配点一次登记**两半**、`db == nil` 时两半一起清，刻意不做惰性构造。三处被真跑纠正：夹具的 `Create` 失败不填 id（真行为是 `BeforeCreate` 在 INSERT 之前就赋 uuid，失败照样留下填好的 id）/ 台账 `callpat` 被「清空那半」命中致接线数=2 的假绿 / 两把变异退化成构建红后补成可编译的语义变异 —— **只在编译期红的变异不算捕获**。台账：16b 兑现翻 wired + 新增四行（现值 **47/53**，五行逐行反向验过、控制组 rc=0）。实跑分树记：克隆 `478ef1c4` 六包 `fail=0`、`TZ=UTC` 同数同绿（计数含子用例，不与上一卡的顶层口径比大小），`-race` 四腿 race=0、service 腿五跑三结局（最多 2 race / 2 fail，也可全绿）且**受害用例名不唯一** ⇒ 上一卡登记的既有 flaky 再证一次，并否掉"归因到单条用例"的读法；工作树的 `TestValidPlatform_Unsupported` 红、架构门 1 处红、secrets 3 处红**全部**落在并行会话未提交/未跟踪的文件上，不代修不代提交，同一棵树换成克隆后同门 rc=0 即为归属证据。刻意不交付：HTTP 手工转商机口、`lead_miner_unified.go` 那条链（无 confidence 生产者）、`ltc.config` 的 `win_probability` 阈值读者。 |
| v1.12 | 2026-09-21 | @backend | 新增 §4.16.5 **漏斗的第五段**（N-1 / T-P4-06）：`conversion-funnels` 从四段变五段，商机段接 `opportunities` 的实时聚合，**没写那张僵尸表**（AC② 由用例锁「取数时顺手往演示表写一行」必红，AC③ 锁「演示表灌 99999 读数不变」）。三条判据：① 口径取 `created_at`（流量）而非 `stage`（状态）—— 混一格状态读数进四条流量腿，逐段相除的「阶段转化率」失去意义，且 `idx_opp_created` 的注释本来就点名按它切时间窗；② 错误只上抛到仓储层，服务层降级成 0 **外加一条 Warn**（这条腿独有一种歧义：T-P4-05 之后 `opportunities` 只有一个生产者，0 既可能是「真没转出来」也可能是「表没建/接缝没装配」；四段老腿答 0 没人拿它做决策，这段答 0 会一路走进 P8 的归因），因此告警是行为不是日志，由抓 `os.Stdout` + 重建全局日志器的用例守着（只换 stdout 不够，`GetLogger` 连 writer 一起缓存）；③ 第五段**追加在末位**，因为「下标即契约」的读者有两处（后端 `_NonMonotonic` 的 `Stages[len-1]`、前端 `List.vue` 摘要区的 `stages[0]`/`stages[length-1]`），加段会让前者**绿着失去意义**、后者把「转化量(会话)」显示成商机数 —— 两处一并改按阶段名取，`total`/`conversion` 与服务端口径刻意仍停在 访问→会话（改成 访问→商机 是产品决策，登记为欠账）。**本卡被真跑纠正的一处**会让「登记」变成假安全：台账最初只加**一行**，反向删掉汇总腿实测 rc=0 仍报 WIRED —— `wired` 只要求命中 ≥1，而 service 里有两个消费点，删一个剩一个，等于装了一把永不变红的锁；按消费点拆成 17a/17b 后四种删除口径（删汇总/删详情/都删/定义改名）分别 rc=1/1/1/2。**第二处纠正落在本卡自己的文档上**：初稿把卡面的 `GET /conversion-funnel` 判成笔误（"真实路径是复数"），读全 `frontend_aliases.go:409-414` 后**推翻** —— 单复数各三条一起注册、卡面字面可命中；我错在拿"测试里只手写了复数两条"+"api-inventory 后端段没有这两组"当结论，而后者本身就是该脚本的盲区（详见 §4.16.5 那段更正与其下的两族漏法）。台账现值 **49/55**（rc=0）。**这条端点原先一条 controller 用例都没有**，而 AC① 说的正是「接口返回」，故新增 `conversion_funnel_http_test.go`（真 gin 路由 + `{code,message,data}` 外壳），H1–H4 四把变异各打红一条，证明不是摆设。实跑（主工作树未提交态，HEAD `39be6824`）：三轮 11 把变异，控制组 service+repository **455 全绿 / 0 skip**、controller **106 全绿**，red 计数逐把记在 §4.16.5；`go test ./internal/ops/...` 四包 ok，`TZ=UTC` 与 `TZ=Asia/Shanghai` 同数同绿，`-race` 三包 DATA RACE 计数 0，`go build` / `go vet` 双 rc=0 且输出 0 字节；`check-date-bucket-tz`（命中 21，与基线同）、`check-enum-consistency`、`check-doc-consistency`、`check-feature-doc`、`api-inventory`（生成物无 diff —— 本卡把这条**下调一档**：该快照抽后端只认 `.GET("全路径")` 形状，`doReg("GET", path)` 那族别名与"控制器内相对路径 + `Group` 前缀"那族都不进账，`grep -c -i opportunit api-inventory.md` 全文为 0）、`audit-cross-package-ports`（Errors 0 / Warns 0）、`check-unwired-assets` 全 rc=0，`check-architecture` 唯一红是并行会话的 `dingtalk_media.go:191/204`。前端：`vite build` rc=0，`eslint` 对本文件 0 error 且改前改后**同为 53 warning**（HEAD 版临时复制到同目录对拍计数，跑完即删），`vitest run` **14 文件 / 238 用例全绿**（本卡 +1 文件 +5 用例，改前 13/233；那份组件用例对改前的 `List.vue` 实跑过 **4 红 1 绿**，绿的那条是本来就不按位置取的明细表）。刻意不交付：端到端转化率延伸到商机、商机段的 `avg_duration_seconds`/`top_sources`（要 `sales_events` 口径，属 T-P7 域）、演示表下线（§4.11 三条删表判据一条未消）、真机看板截图（8204 上活着的是 9-19 起的旧二进制 `bin/user-server.r39`，不含本卡改动且非本会话启动，不为它污染并行会话的证据）。 |
| v1.13 | 2026-09-21 | @backend | **T-P4-06 交付后复查（双推之后按「彻底深入检查」重跑本卡）**，抓到并当场修掉两处，都在"自己写的登记"上：① §4.16.5 落点表**漏登记** `ops/repository/conversion_funnel_opportunity_test.go`（新增，三条取数层用例，含"往演示表种巨量假行而真实源读数不动"这条 D-9 的仓储侧证据），标题计数也随之下修 —— 实数按 `git show --name-status d52434c2 cc065b02` 为 **4 个 `A` + 8 个 `M`**（ops 侧四改三增、前端一改一增、台账一行、文档两篇），表下已把口径边界写清；② 那个文件**没过 gofmt**，而 gofmt 是真门（CI `Static gates` 调 `make fmt-check`，与本地同判据）—— 红因是包注释里 `①②③` 用了四空格续行，Go 1.19+ 把注释内 ≥4 空格缩进判成代码块。修法是把续行顶格而**不跑** `gofmt -w`（它会把散文重排成 tab 代码块，更难读）；改后 `gofmt -l` 对该文件空、`git diff` 该文件非注释改动 **0 行**、`ops/repository` 包重跑 **76 条全绿**。同批另一条 fmt 红 `internal/controller/wechat_batchf4_m01_inbound_test.go` 属并行会话未提交文件，不代改。**由此得一条门禁口径**：新增文件的验收清单不能只按 `scripts/*.sh` 枚举门，`make fmt-check` 这类**以 make 目标存在的门**会被整批漏掉（本卡当初的清单列了 build/vet/六门/-race，唯独没有它）。**同轮复核成立项（逐项重跑，非推断）**：`ops/service`+`ops/repository` 双时区各 455 / 0 fail / 0 skip、`ops/controller` 106 / 0 / 0；命名六门在 `--shared` 干净克隆六条全 rc=0，工作树五绿一红，那处架构红这次拿到**直接归属证据**（`dingtalk_media.go` 状态为 `??`、`git log -- <路径>` 零提交 ⇒ HEAD 无此文件）；`check-secrets.sh` rc=1 的三处命中文件名与本卡 12 路径求交集为空（`comm -12` 实算）；台账 49/55 且项17 两行各自 `接线数=1`；`api-inventory.md` 全文 `opportunit` 命中 0、`human-task` 两条均在 1426/2241 的前端调用段；`闭环完成率` 全仓 `*.go` 仍只命中 `internal/model/opportunity.go:85`；`frontend_aliases.go:409-414` 逐字重读确为单复数各三条；`List.vue` md5 与提交时一致、`vitest run` 14 文件 / 238 用例全绿。**另修规划文档（git 外）的表格完整性**：7 行表格行补回收尾管道、12 行两列修订条目里的裸 `\|` 转义（逐行按插入位置反删验证无损）；多列表格里**另有 14 行**"列数与表头不符"的**历史**条目**刻意未动**（12 行是列数比表头多、2 行少一格，行号与逐行判读留在任务清单 r48）—— 那种行分不清"多出的单元格分隔符"与"散文里的裸管道"，盲改会把真实列并掉，属渲染问题不属事实错误，留待人工逐行判读。**本文件其余内容零改动**。 |
| v1.14 | 2026-09-21 | @backend | 新增 §4.17 **SOP 开工名单的三个权威源**（N-2 / T-P5-01）：`sop_scheduler.go` 那条"名单为空就把 SOP 跑到创建者本人身上"的回退**删掉了**，换成按 `trigger_config.audience` 实时圈选，读 `customer_rfms.segment` / `customer_tag_assignments.tag` / `churn_scores.p_alive` 三张既有权威表 —— **没建人群表**（C7）。四条判据：① 条件是**交集**不是并集（并集规模直接顶穿 AC② 想夹的那个东西）；② **`0 人` 有两种、必须分开报** —— `no_match:<条件>` 让人去改条件，`source_empty:<源>` 让人去查数据源，而 churn 今天恰恰是后者（`defaultChurnStatsQuery` 是 T-P1-07 登记在册的桩，`ComputeAll` 读到空统计就一行不写），只报"0 人"会把"这个域还没有生产者"读成"我条件写得太严"，两种处置动作完全相反；③ 上限**夹两次**：`limit()` 夹到 `MaxAudienceLimit=500` 防手滑，`tryExecute` 再按 `maxRunningPerSOP` 的**剩余额度**夹一次防"阈值只在上轮已跑满时才生效"（原实现 49 在跑 ＋ 名单 3 人 ＝ 52 并发）；④ 翻页步长钉死 200，因为 `ListBySegment` 把 `pageSize>200` **静默改成 20**（`customer_rfm.go:65-66`），直传 500 实测只回 20 行。**AC③ 与卡面差一处、按实际落法登记**：这条链路上根本没有 `ProactiveReachService` 可调（调度器只建 `SOPExecution`，出域发送在执行体工具节点里、闸门属 T-P5-03），所以交付的是 AC③ 的**语义**（未经 `audience_confirmed` 时一条执行都不建、名单写回 `trigger_config.audience_preview`）而不是"复用 DryRun"那句**调用** —— 宁可留这条偏差，也不在 FEATURES 写一句代码里不存在的复用。**三处被真跑纠正、第四处被提交前的 `gofmt -l` 纠正、第五处由回读源码纠正（它躲过了全部实跑：反向验证只证明 grep 锚点会红，证明不了我替那一格写下的症状句）**：假绿那条最贵 —— `TestAudience_PropagatesQueryError` 第一版**没改生产代码就绿了**，因为 `NewTestDB` 同进程共用一个库、前一个用例建的 `customer_rfm` 还在原地，"表不存在"必须显式 `DropTable` 构造（调度器侧同类用例同改）；`setJSONMapValue` 只能塞字符串值，预览是对象会被写成 `""` ⇒ 改 `json.Marshal` 整体回写；schedule 型一次 tick 回写两次、后一次序列化的是**内存里那份 map** ⇒ 预览必须就地改那份 map，"只落库不落内存"这把变异被专门用例打红。第四处是**新写文件**的注释续行又用 4 空格、被 `gofmt -l` 在提交前抓到（与 `7a7c99ad` 同一坑的第二次，修法同前：顶格 `// ` 单空格、不跑 `gofmt -w`）。另抓到**自己文档里的数**：落点表写"追加 8 条用例"实为 **9** 条、selector 那格把 10 条函数与其子用例混成"11 条判据"。台账新增**项18 三格**（49/55 ⇒ **52/58**，`接线数` 恒为 1）：18a 调度器启动入口在 `cmd/api`（摘掉即两类 SOP 一起静默停摆、`internal/service` 全绿）、18b `audience:` 字段赋值、18c 标签腿那一跳（18c 与 17 同一课：刻意不写成 `New.*RepositoryWithDB\(` 那种并格宽式，因为 RFM 底座另有 `customer_360.go:57` 消费点，并格即死锁）。**两处刻意欠账按原样登记不粉饰**：FEATURES.md 那条**本卡没加**（该文件正被并行会话整表重写，`git diff` 32 行全在 webhook 渠道表、与本卡 7 路径 `comm -12` 命中 0），§4.17 两处 nil 守卫**无用例覆盖**（`NewSOPScheduler` 传 nil db 会连 `execRepo` 一起置 nil、`tryExecute` 第一行就返回 ⇒ 生产构造路径走不到；留它是为 `NewAudienceSelectorWithDB(nil)` 这个公开可判空契约，为"手工组装结构体才能触发"的分支写测试＝测夹具）。实跑（工作树未提交态，HEAD `9859e2b9`；全量跑完后本卡对这两包 Go 源码零再编辑，逐文件 `gofmt -l` 空 ⇒ 这些数就是最终态）：`go build` / `go vet` 双 rc=0；两时区全量 `internal/service` + `internal/repository` **各双 rc=0**（CST 668.174s / 231.338s；UTC 487.782s / 143.255s）；本卡 34 条用例 `-test.v` 实数顶层 **34**（＝ selector 10 ＋ scheduler 24，与 `^func Test` 逐字对上）＋ 子用例 **20**、FAIL 0、SKIP 0；反向电池 **9 把全 KILLED**（逐把 `cp` 备份、写回比 md5 一致），台账三格**逐格摘装 rc=1 且各点名 1 行**、还原后 md5 一致、`p501mut` 与 `.p501*` 残留 0；门 `check-date-bucket-tz`（命中 **21** ＝ 基线 21）/`enum`/`doc-consistency`/`feature-doc`/`unwired-assets`(52/58)/`api-inventory`（生成物无 diff ⇒ 零新端点）/`ports`（Errors 0 Warns 0）七条 rc=0；`make fmt-check` **rc=2** 未过文件**恰 1 个**（`wechat_batchf4_m01_inbound_test.go`，`??` 未跟踪）、本卡 5 文件 `gofmt -l` **空**；`check-architecture` **rc=1** 唯一红 `dingtalk_media.go:191/204`（`??`）、`check-secrets.sh` **rc=1** 三处命中与本卡 7 路径交集 **0**、markdown lint **rc=1 / 1 issue** 属并行会话正在改的 `CHANNEL_INTEGRATION_AUDIT_2026-09.md:808` 而 §4.17 全文 **0 命中**。`-race` **rc=1**：repository ok(149.126s)、service FAIL(653.562s)，`WARNING: DATA RACE` **6** 处、`--- FAIL` **3** 条 —— **0 帧**涉本卡三个 Go 文件，两组红各自归属：① `TestCreateSession_AllowDifferentPlatform` 与 `_AnonymousUser` 同一地址 `0x…75bc60`、写方 `db.SetTestDB()` 读方 `MaybeSendAwayReply` 的 `SafeGo` 后台 goroutine，涉事 4 文件 `git status` **全 clean** ⇒ HEAD 自带那族既有缺陷；② 另 4 处属并行会话**未跟踪**的 `webhook_batchf4_m01_qq_test.go:342` 对 `qq_media.go` 的 `FetchQQAttachment`/`persistQQMediaAsync` ⇒ 不同文件不同键，均不代修不代提交。前端**零改动**（7 路径全在 `user-server/` 与 `scripts/` 与文档），所以没有 `vite`/`eslint`/`vitest` 腿 —— 是没对象，不是跳过。 |
| v1.15 | 2026-09-21 | @backend | 新增 §4.18 **`agent_mode` 的第一次真实读取**（N-3 / T-P5-02）：`lifecycle.Resolver` 从零调用方变成有第一处生产调用点，`agent_lifecycle_wiring.go` 按 `agent_mode` 分派 passive/active，主动那侧**只编排 SOP**（C7 裁定，`TestActiveNeverImportsConversationEngine` 用 go/parser 静态钉住本包 import 集）。schema 侧本卡**零新表零新列**，所以登记的重点全在代价：① `ai_agents.agent_mode` 有 `index` 但没有任何 `WHERE agent_mode = ?`，本卡读的是主键取行后的内存字符串比对（那行索引仍然只是将来按模式批量取的预备）；② 归因三个键写进 `sop_executions.execution_data`，而那列 gorm tag 是 `type:text` 不是 `jsonb` ⇒ 逐行看得见、按值查不动，P8 看板若建在 `one_id` 上会撞全表扫（按 C7 不建列，**有意识欠账**）；③ `one_id` 在本仓实测**三种宽度**（100 / 128 / `text`，权威源 `customers.unified_id` 是 128，且 `script_exposure_logs` 的注释就记着 varchar(64) 曾溢出）⇒ 任何把 one_id 提成列的后续卡必须先定宽度，按 100 建会把 128 的源顶到 INSERT 报错；④ 合成会话键从 `agent_code` 换成 `agent_id`，因为 `session_id varchar(120)` 的 12 字节余量押在另外两张表的列宽上。另登记一处**本文件既有口径的错**：`api-inventory.sh:32` 只 grep `internal/router/**`，从 `internal/app` 注册的路由它看不见 ⇒ §4.17 那句清单无 diff ⇒ 零新端点的**结论**为真（已核 `00aeff61` 零路由）但**证据**是道看不见证据的门，本卡的新端点就是它漏掉的第一个。台账 49/55 → 52/58 → **55/64**，项5 转 wired、项19 六格（两 wired 各用一把 router.go 变异证过有牙，且摘掉装配点后 app＋router 测试全绿、线上稳定回 503 ⇒ 台账是唯一看得见它的东西） |
| v1.16 | 2026-09-21 | @backend | 新增 §4.19 **`reach_send` 节点：图上多一步"先批准才出域"**（T-P5-03）：Active 外联从"代码里的一段 if"变成"图上的一步"。**schema 侧零新表、零新列、零新索引、`migrations/` 零变更**，所以登记面全在"值落在哪一列、谁按值查得动"：① 四个新键全挤在 `sop_executions.execution_data`（`type:text`）里 —— `_reach_skipped`（值域 `dnc`/`cooldown`/`already_sent`/`approval:<状态>`，四种"没发出去"的处置动作完全不同，合并成一个 `skipped` 等于把三件事混成一件）、`_reach_channel`、`_reach_message_id`，以及沿 `message_sent:` 同族命名空间的幂等键 `reach_sent:<execution_id>:<node_id>`；② **挂起期在库里是四行四个状态的组合** —— `handleNodeWaiting` 从不写 `sop_executions.status`，所以"卡在哪儿"只能读 `wait_event=approval` 那一列，配 `approval_requests=pending` ＋ `sop_timers=pending` ＋ `sop_executions=running`；③ **一条既有 DB 约束顺手当了兜底**：`uq_approval_request_open (subject_type, subject_id) WHERE status='pending'` ＋ `subject_id=<exec>--<node>` ⇒ 同一步挂起期不可能有两条 pending 审批行（约束来自库、不是应用层），与节点自己那枚跨重启的幂等键各管一件事；④ **对 §4.18 一句口径的收窄**："归因/产物值按值查不动"说重了 —— 这些值在 `sop_exec_events` 的 `input`/`output`/`side_effects` 三列各有一份 **jsonb 镜像**（`writeExecEvent` 每次把整份 `execution_data` 塞进 `input`）⇒ 查得动，真正的代价是那三列**没有任何 GIN 索引**，按值查＝扫一张比执行表大一到两个量级的事件表；结论（P8 建在这些键上会撞全表扫）不变，措辞要换；⑤ 两个审批主体的键**刻意不合成一枚**（图上腿源自执行行 `<exec>--<node>`、服务侧 W-1 门源自客户行 `unified_id`＋渠道＋收件人），合成即"改图＝改归因"，由 P1 变异格背书；⑥ `sop_executions.customer_id` 是 `varchar(64) NOT NULL` **无 default** ⇒ 空串是合法存量形状，这一条列定义直接立了一条用例（无 customer_id 时靠 `one_id` 找回身份）。**卡面落点偏差如实登记**：卡上写"M `proactive_reach.go`（接入检查点）"，实际该文件**零 diff**（三判据早在 `ReachByCustomer` 里，缺的是图上那一腿）。另有两道静态锁各写明自己看不见什么（出域符号扫描看不见"不被 `NodeType()` 认出来的执行器"；装配点源码锁看不见"这一行是否真被执行"），台账项 20 四行、现值 **55/64 → 58/68**（两个端点在同一棵克隆里分别用 HEAD 版与工作树版脚本各实测一次），行为变异电池 **24 格全 KILLED、无未登记存活** |
| v1.17 | 2026-09-21 | @backend | 新增 §4.20 **放量三档落在哪一列**（T-P5-04）：**仍是零新表、零新列、零索引、`migrations/` 零变更**，但这一节的中心结论是"**没有一列可加**"式的缺：① 档位是 `system_config_kv` 里 `key='ltc.config'` 那一行 `value text` 内的一个 JSON 节（`model/system_config_kv.go:6-11`）⇒ 预算是整份文档 8KB、名单 ≤200 条且单条 ≤128 字节（对齐 `one_id` 最大宽度）、判定键精确等值所以空串/带空格/重复条目在写侧就被拒，**且没有任何 SQL 能按档位查历史**；② "改档不需要重启"要说准成"每请求读＋60s 进程内缓存 ⇒ 多副本对其余副本最迟 60s 生效"，这条本来就是 `ReadingHints` 第一条，写档位的注释不许对它例外；③ **送达率不是接错线、是根本没有 join 键**：`sms_delivery_statuses.message_id` 是唯一归因键且这张表**只由 webhook 建行**，而 `sms_records` 没有单号列、`SmsService.SendSms` 只回 `error`、`sendAliyun` 把 `BizId` 解析进 `result.BizID` 后在成功分支**原样丢弃**、tencent/huawei 成功分支返回硬编码 `"OK","OK"` ⇒ 外发上行的是常量 `sms_out`，它**一个字符都没进过送达表**（"照它建行会挤成一行"是给未来接线的警告，不是现网事实）；④ 退订判据在 SMS 域原有两个入口漏了 ——`SendSms` 命中退订 `return nil`（每个调用方读成"发成功了"：记成功、烧幂等键、进送达率分母）改回 `ErrDoNotContact` 哨兵（复用既有错误 ⇒ 队列/SOP 已有的 DNC 处置自动接上，零新分支），`ResendSms` **从来不查退订**（直连 `dispatchToProvider`）补同一道检查且放在改状态之前；⑤ **一处自家叙述的实测纠偏**：初版 reason 串写"外发链路写进去的 message_id 是常量 sms_out"，逐条重验后不成立（表只由 webhook 按运营商单号建行）⇒ reason/unblocker 与包注释改成实测形状，并给用例加一条"不许把推断写成实测事实"的反向断言（`6897d5c3`）；⑥ 实跑：`--shared` 克隆 `/tmp/p504gate/hivemtk`（HEAD `d94810d8`、0 脏文件）里 build/vet/gofmt rc=0、十道门 **rc 全 0**（台账 **58/68 与本卡开工前同值 ⇒ 本卡零台账行变更**、`api-inventory` 生成物零 diff ⇒ 无新端点），`app`＋`router` 两时区各 rc=0，`service` 本卡子集两时区各 **232 条顶层全 PASS**（该 232 与活树静态同名集合逐条比对 diff 为空；早前那个 280 是**非锚定** grep 含子用例的另一个数，不可混引），`service` 全量 CST **rc=0 pass=3617 fail=0 skip=3（451.778s）**、UTC rc=1 且唯一一条红是**负载相关假红**（`ab_experiment_test.go:50` 断"5 写 2 缓冲⇒恰丢 3"，而构造函数里就起了消费协程 ⇒ 单跑 20/20 绿、带负载 2 FAIL/400 复现，用例由 `8fecb6ad` 引入、与本卡无交集）；10 格变异电池 **KILLED=10／SURVIVED=0**、还原逐文件 md5 一致 |
| v1.18 | 2026-09-21 | @backend | 新增 §4.21 **报价的两张表：一行 = 一个版本**（N-5 / T-P6-01）：直读 `information_schema` + `pg_indexes` 给出 `quotes`（10 列 4 索引，`uq_quotes_quote_version (quote_id, version)` 是 AC① 的唯一硬保证，且**刻意不建**单列 `quote_id` 唯一索引 —— 那条索引表达的是"一版一号"，装上后同一张单的第二版永远插不进去）与 `quote_line_items`（`(quote_row_id, line_no)` 复合主键、**无代理键**，所以"原地改一行"在物理上不成立，"旧版不可变"是形状的结果而不是规矩）的实测形状；写明表头上合计列／审批列／行内币种／租户列四样一律不建的理由（各有既成事实源：行项目之和、`approval_requests`、混币种时合计没有定义、X3）、版本链只有三条写路径（`Create`/`Append`/`UpdateStatus`，后者的写集合是 `status`+`updated_at` 且 `version` 不动）、八个并发追加恰好一个赢且其余七个收 `ErrQuoteVersionConflict`（裁决权在库级索引而不是进程内锁：多实例下锁只管得住半个链），并登记本卡**零生产写入方**为台账项 21 的两行 `UNWIRED`。GORM 那条实测坑一并记下：两列各写一次同名 `uniqueIndex:` 时它自己拼复合索引，**名字差一个字符就退化成两个单列索引，表能建、插入照样重复、测试全绿** ⇒ 复合索引的唯一性只能在 pg_index 里点名列集合来断 |
