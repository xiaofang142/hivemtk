-- 091_lead_rules_migrate.sql
-- T014：lead_mining_configs 由「每租户一行」（主键 = tenant_id，非全局 id=1）扩展为
-- 三粒度规则（global/channel/account），并给每个存量租户生成一条 global 规则。
-- 幂等：重复执行不丢存量、不重复生成。
ALTER TABLE lead_mining_configs ADD COLUMN IF NOT EXISTS scope VARCHAR(16) NOT NULL DEFAULT 'global';
ALTER TABLE lead_mining_configs ADD COLUMN IF NOT EXISTS scope_id BIGINT NOT NULL DEFAULT 0;

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

-- keywords 历史列是 TEXT（存 JSON 数组字符串）。空串/非 JSON 时降级为空数组，
-- 避免一次坏数据让整条迁移中断（不丢配置：原行仍留在 lead_mining_configs）。
INSERT INTO lead_mining_rules
    (tenant_id, scope, scope_id, keywords, semantic_keywords, system_description, enabled, created_at, updated_at)
SELECT
    c.tenant_id,
    'global',
    0,
    CASE WHEN c.keywords ~ '^\s*\[' THEN c.keywords::jsonb ELSE '[]'::jsonb END,
    '[]'::jsonb,
    COALESCE(c.requirement, ''),
    c.enabled,
    now(),
    now()
FROM lead_mining_configs c
WHERE NOT EXISTS (
    SELECT 1 FROM lead_mining_rules r
    WHERE r.tenant_id = c.tenant_id AND r.scope = 'global' AND r.scope_id = 0
)
ON CONFLICT DO NOTHING;

-- 存量配置行本身也打上 global 粒度标记，供旧读路径继续读同一行。
UPDATE lead_mining_configs SET scope = 'global', scope_id = 0
WHERE scope IS NULL OR scope = '' OR scope_id IS NULL;
