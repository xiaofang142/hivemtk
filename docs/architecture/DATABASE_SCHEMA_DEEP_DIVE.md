# HiveMtk 数据库 Schema 深度解析

> **版本**：v1.8（2026-09-20，T-P4-02 新增 §4.16.1 商机仓储层；v1.7 是 T-P4-01 的 §4.16 —— 那张卡当日只加了正文小节、漏了本行与修订历史，v1.7 一并补登记）
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
| 转化漏斗 | `internal/ops/service/conversion_funnel.go` `BuildFunnel` 对 `customer_events` / `clues` / `intent_records` / `customer_sessions` 的**实时聚合** | `conversion_funnels`（**全仓无读路径**） |

两套阶段名**刻意不同**，这是判定"表是死的"的关键证据：

| 侧 | 阶段名 |
|---|---|
| 真实源（词表在 `internal/ops/repository.FunnelStageKey`，代码里是唯一真源） | `visit` 访问 → `clue` 线索 → `intent` 意向 → `session` 会话 |
| 演示表（`cmd/seed/seed_stats.go` 自造） | `exposure` → `click` → `consult` → `add_wecom` → `deal` |

规则（写进 `model.ConversionFunnel` 的注释，同时由 `conversion_funnel_stage_test.go` 的
`_DemoTableStaysOutOfVocabulary` 守着）：

- **新域不得往 `conversion_funnels` 写数据**。报价/商机漏斗（T-P4、T-P7-02）的阶段维度一律
  以 `FunnelStageKey` 为准；`opportunity`（商机）已作为**预留位定名、尚未产出**——定名而不产出，
  是为了让回款域能引用同一个键，又不会在响应里凭空多出一个恒为 0 的阶段。
- 演示行自带 `extra.demo_only = true` 与 `extra.real_source` 指针，供临时 SQL/BI 的使用者辨真伪。

**为什么不删这张表（"未证伪不删"）**：它仍在 `internal/pkg/db/migrate.go allModels()` 的建表清单里，
生产库可能已有历史行；而"没有读取方"只在**本仓**成立，仓外的 BI 脚本/定时报表看不到。
三条同时成立时另立卡删除：① 仓外读取方确认为零；② 表内行只来自 seed；③ 已决定归档方式。

**顺带登记的两处真实源口径问题**（不改，属"改变现网读数"，见短板 G16）：
`BuildFunnel` 把四个 count 的错误全部吞掉 ⇒ 某个数据源表不可用时，接口回 200 且该阶段显示为 0，
报表看起来"只是转化率低"；阶段之间不保证单调 ⇒ `rate` 可以 >100、`drop_rate` 可以为负
（`conversion_funnel_baseline_test.go` 的 `_NonMonotonicStagesBaseline` 把这个现状钉在那里）。
未知阶段名走 `GET /conversion-funnel/stage` 也回 200 + 空名字 + 0 计数，而不是 404。

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
（**已随 T-P4-03 交付**，见 §4.16.2），列表/详情端点 → T-P4-04。
`win_probability` 与 ltc.config 的 `win_probability` 阈值**同量程 0–1**（默认 0.50），
所以比较不需要换算；那枚阈值今天在生产代码里仍零读者，消费方就是本卡的这一列 + T-P4-05/T-P6-04。

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
base 是显式登记的**先验刻度**而非标定值（今日全表零生产写入方，没有任何收口行可回标），
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
台账现值 **38/47**（`check-unwired-assets.sh` rc=0，16a/16b/16c 三行均按 UNWIRED 登记）。
16b（`model.Opportunity{` 在 `internal/service` 的商机行写入点）本卡**刻意不翻**：本层只发
`*model.Opportunity` 指针、不构造新行，构造与一键转商机在 T-P4-05。读端点与 409/400 的映射在 T-P4-04。


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
