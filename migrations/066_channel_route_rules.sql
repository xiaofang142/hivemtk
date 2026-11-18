-- 066_channel_route_rules.sql
-- specs/024 T301：入站事件路由规则表。
--
-- 背景：今天「入站即触发 AI」写死在 webhook_ai.go 的 shouldTriggerAI 里，
-- 想让某类事件只归档、或让某账号交给别的管线，只能改代码发版。
--
-- 口径：**无规则 = 完全保持今天的行为**（引擎无命中即放行）。
--   channel / account_id 空串 = 不限；
--   event_type 支持通配（* / message.*）；
--   match 是 JSON 对象（extra.<k> 取值 + {"$in":[...]}），非法 JSON 在引擎侧跳过；
--   priority 升序，首个命中生效（不叠加）。

CREATE TABLE IF NOT EXISTS channel_route_rules (
    id          BIGSERIAL PRIMARY KEY,
    tenant_id   BIGINT       NOT NULL DEFAULT 1,
    channel     VARCHAR(32)  NOT NULL DEFAULT '',
    account_id  VARCHAR(64)  NOT NULL DEFAULT '',
    event_type  VARCHAR(64)  NOT NULL DEFAULT '',
    match       TEXT         NOT NULL DEFAULT '',
    target      VARCHAR(32)  NOT NULL DEFAULT 'pipeline',
    target_ref  VARCHAR(128) NOT NULL DEFAULT '',
    priority    INTEGER      NOT NULL DEFAULT 100,
    enabled     BOOLEAN      NOT NULL DEFAULT TRUE,
    note        VARCHAR(255) NOT NULL DEFAULT '',
    created_at  TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ  NOT NULL DEFAULT now()
);

-- 引擎的关键查询：某租户某渠道某账号的启用规则，按优先级升序。
CREATE INDEX IF NOT EXISTS idx_channel_route_lookup
    ON channel_route_rules (tenant_id, channel, account_id, priority, enabled);

CREATE INDEX IF NOT EXISTS idx_channel_route_rules_tenant
    ON channel_route_rules (tenant_id, id);
