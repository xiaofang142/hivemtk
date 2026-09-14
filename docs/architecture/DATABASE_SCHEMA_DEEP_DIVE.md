# HiveMtk 数据库 Schema 深度解析

> **版本**：v1.0（2026-08-16）
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
- **单租户**：无 `merchant_id` 字段（参见 ADR-003）
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
| 知识库检索 | `(kb_id, doc_type, status)` | KB 维度优先 |

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
