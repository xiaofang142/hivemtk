-- 090_channel_identify_codes.sql
-- 渠道唯一识别码（一次性替换旧 Bridge token 凭证）。T008 / data-model.md。
-- code 唯一、可重置；version 每次重置 +1。
CREATE TABLE IF NOT EXISTS channel_identify_codes (
    id         BIGSERIAL    PRIMARY KEY,
    tenant_id  BIGINT       NOT NULL DEFAULT 1,
    channel_id BIGINT       NOT NULL,
    code       VARCHAR(128) NOT NULL,
    version    INT          NOT NULL DEFAULT 1,
    created_at TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ  NOT NULL DEFAULT now(),
    reset_at   TIMESTAMPTZ  NULL
);

CREATE INDEX IF NOT EXISTS idx_channel_identify_codes_tenant ON channel_identify_codes (tenant_id);
CREATE UNIQUE INDEX IF NOT EXISTS global_uniq_channel_identify_code ON channel_identify_codes (code);
-- 一个渠道只有一条权威识别码记录（重置走 version+1，不新增行）。
CREATE UNIQUE INDEX IF NOT EXISTS global_uniq_channel_identify_channel ON channel_identify_codes (channel_id);
