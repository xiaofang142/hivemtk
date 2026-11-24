-- 094_lead_mining.sql
-- Phase 4 / US2：线索模块全链路审计与多规则并行检测。
-- 幂等；所有 ALTER 用 ADD COLUMN IF NOT EXISTS，表用 CREATE TABLE IF NOT EXISTS。
-- 与 Go 注册 v3.111.0（internal/migration/migrations/v3_111_0_lead_mining_migration.go）
-- 是同一份契约：SQL 供人工/测试执行，Go 迁移供启动链执行。

-- 1) lead_mining_rules：091 已建；此处仅幂等兜底，列定义与 091 严格一致。
CREATE TABLE IF NOT EXISTS lead_mining_rules (
    id                 BIGSERIAL    PRIMARY KEY,
    tenant_id          BIGINT       NOT NULL DEFAULT 1,
    scope              VARCHAR(16)  NOT NULL DEFAULT 'global',
    scope_id           BIGINT       NOT NULL DEFAULT 0,
    keywords           JSONB        NOT NULL DEFAULT '[]'::jsonb,
    semantic_keywords  JSONB        NOT NULL DEFAULT '[]'::jsonb,
    system_description TEXT         NOT NULL DEFAULT '',
    enabled            BOOLEAN      NOT NULL DEFAULT true,
    created_at         TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_at         TIMESTAMPTZ  NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_lead_mining_rules_tenant ON lead_mining_rules (tenant_id);
CREATE UNIQUE INDEX IF NOT EXISTS uq_lead_mining_rules_scope
    ON lead_mining_rules (tenant_id, scope, scope_id);

-- 2) lead_mining_hits：三路并行检测的独立证据，每路一行，不互相覆盖。
CREATE TABLE IF NOT EXISTS lead_mining_hits (
    id                  BIGSERIAL    PRIMARY KEY,
    tenant_id           BIGINT       NOT NULL DEFAULT 1,
    judgement_id        VARCHAR(32)  NOT NULL DEFAULT '',
    type                VARCHAR(16)  NOT NULL DEFAULT '',   -- keyword / semantic / llm
    payload             JSONB        NOT NULL DEFAULT '{}'::jsonb,
    evidence_message_ids JSONB       NOT NULL DEFAULT '[]'::jsonb,
    complete            BOOLEAN      NOT NULL DEFAULT true,  -- 证据是否完整
    degraded            BOOLEAN      NOT NULL DEFAULT false, -- LLM 超时/未初始化降级
    reason              TEXT         NOT NULL DEFAULT '',
    created_at          TIMESTAMPTZ  NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_lmh_tenant_judgement ON lead_mining_hits (tenant_id, judgement_id);
CREATE INDEX IF NOT EXISTS idx_lmh_tenant_type ON lead_mining_hits (tenant_id, type);

-- 3) lead_mining_judgements：二次 LLM 汇总结论扩展（保持 085/086 既有列不变）。
ALTER TABLE lead_mining_judgements ADD COLUMN IF NOT EXISTS is_clue            BOOLEAN      NOT NULL DEFAULT false;
ALTER TABLE lead_mining_judgements ADD COLUMN IF NOT EXISTS clue_type          VARCHAR(32)  NOT NULL DEFAULT '';
ALTER TABLE lead_mining_judgements ADD COLUMN IF NOT EXISTS summary            TEXT         NOT NULL DEFAULT '';
ALTER TABLE lead_mining_judgements ADD COLUMN IF NOT EXISTS context_message_ids JSONB       NOT NULL DEFAULT '[]'::jsonb;
ALTER TABLE lead_mining_judgements ADD COLUMN IF NOT EXISTS source_hit_ids     JSONB        NOT NULL DEFAULT '[]'::jsonb;
ALTER TABLE lead_mining_judgements ADD COLUMN IF NOT EXISTS evidence_complete  BOOLEAN      NOT NULL DEFAULT true;
ALTER TABLE lead_mining_judgements ADD COLUMN IF NOT EXISTS degraded           BOOLEAN      NOT NULL DEFAULT false;
ALTER TABLE lead_mining_judgements ADD COLUMN IF NOT EXISTS channel_account_id BIGINT       NOT NULL DEFAULT 0;
CREATE INDEX IF NOT EXISTS idx_lmj_tenant_is_clue ON lead_mining_judgements (tenant_id, is_clue);
CREATE INDEX IF NOT EXISTS idx_lmj_tenant_channel_account ON lead_mining_judgements (tenant_id, channel_account_id);

-- 4) lead_mining_notify_routes：三级回退（账号维护人→渠道维护人→租户默认）+ 送达记录。
ALTER TABLE lead_mining_notify_routes ADD COLUMN IF NOT EXISTS channel_account_id BIGINT      NOT NULL DEFAULT 0;
ALTER TABLE lead_mining_notify_routes ADD COLUMN IF NOT EXISTS scope              VARCHAR(16) NOT NULL DEFAULT 'tenant';
ALTER TABLE lead_mining_notify_routes ADD COLUMN IF NOT EXISTS priority           INT         NOT NULL DEFAULT 0;
ALTER TABLE lead_mining_notify_routes ADD COLUMN IF NOT EXISTS notify_user_ids    JSONB       NOT NULL DEFAULT '[]'::jsonb;
ALTER TABLE lead_mining_notify_routes ADD COLUMN IF NOT EXISTS notify_types       JSONB       NOT NULL DEFAULT '[]'::jsonb;
ALTER TABLE lead_mining_notify_routes ADD COLUMN IF NOT EXISTS delivery_records   JSONB       NOT NULL DEFAULT '[]'::jsonb;
CREATE INDEX IF NOT EXISTS idx_lmnr_tenant_account ON lead_mining_notify_routes (tenant_id, channel_account_id);

-- 5) conversation_contexts：两智能体共享的多轮 + 历史对话上下文。
CREATE TABLE IF NOT EXISTS conversation_contexts (
    id                      BIGSERIAL    PRIMARY KEY,
    tenant_id               BIGINT       NOT NULL DEFAULT 1,
    channel_account_id      BIGINT       NOT NULL DEFAULT 0,
    external_conversation_id VARCHAR(128) NOT NULL DEFAULT '',
    member_roles            JSONB        NOT NULL DEFAULT '{}'::jsonb,
    messages                JSONB        NOT NULL DEFAULT '[]'::jsonb,
    tags                    JSONB        NOT NULL DEFAULT '[]'::jsonb,
    summary                 TEXT         NOT NULL DEFAULT '',
    created_at              TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_at              TIMESTAMPTZ  NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX IF NOT EXISTS uq_conv_ctx_tenant_account_conv
    ON conversation_contexts (tenant_id, channel_account_id, external_conversation_id);
CREATE INDEX IF NOT EXISTS idx_conv_ctx_tenant_conv ON conversation_contexts (tenant_id, external_conversation_id);

-- 6) clues：归档关联的用户信息/线索信息/证据引用/归档时间。
-- 角色语义复用已有 lead_role（085 已建），不新增重复列。
ALTER TABLE clues ADD COLUMN IF NOT EXISTS customer_id   VARCHAR(64) NOT NULL DEFAULT '';
ALTER TABLE clues ADD COLUMN IF NOT EXISTS user_info     JSONB       NOT NULL DEFAULT '{}'::jsonb;
ALTER TABLE clues ADD COLUMN IF NOT EXISTS source_hit_ids JSONB      NOT NULL DEFAULT '[]'::jsonb;
ALTER TABLE clues ADD COLUMN IF NOT EXISTS archived_at   BIGINT;
CREATE INDEX IF NOT EXISTS idx_clues_tenant_customer ON clues (tenant_id, customer_id);
CREATE INDEX IF NOT EXISTS idx_clues_tenant_archived ON clues (tenant_id, archived_at);
