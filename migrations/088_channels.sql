-- 088_channels.sql
-- 渠道主数据（预设表驱动）。T006 / data-model.md 渠道域。
-- 幂等：可重复执行，不删列不删表。
CREATE TABLE IF NOT EXISTS channels (
    id          BIGSERIAL    PRIMARY KEY,
    tenant_id   BIGINT       NOT NULL DEFAULT 1,
    name        VARCHAR(100) NOT NULL,
    code        VARCHAR(64)  NOT NULL,
    type        VARCHAR(32)  NOT NULL DEFAULT 'dm',
    enabled     BOOLEAN      NOT NULL DEFAULT true,
    remark      VARCHAR(500) NOT NULL DEFAULT '',
    created_at  TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ  NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_channels_tenant ON channels (tenant_id);
-- 渠道 code 是跨租户可读的预设主键语义，刻意保持全局唯一（global_uniq_ 前缀
-- 使 tenancyidx.Normalize 不会把它复合化成 (tenant_id, code)）。
CREATE UNIQUE INDEX IF NOT EXISTS global_uniq_channel_code ON channels (code);
