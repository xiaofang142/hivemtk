-- 077_channel_tenant_id_bigint.sql
--
-- 三张渠道证据表（channel_route_rules / channel_event_audits / channel_interactions）的
-- tenant_id 历史口径是 VARCHAR(64) NOT NULL DEFAULT ''，与仓库其余业务表（BIGINT NOT NULL
-- DEFAULT 1）不一致：
--   * 租户 id 本身是 uint（internal/tenant.FromContext 返回 uint）；
--   * batch tenant_id 迁移（tenant_column_batch_migration.go）的形态校验要求
--     data_type='bigint' AND is_nullable='NO'，varchar 列在全新库跑链后直接被判「形态错」。
--
-- 本迁移对存量库做原地类型转换，对「已经是 bigint」的库同样安全（幂等）；
-- 空串/脏值归 1（平台事件在无租户上下文时写的兜底值就是 1）。
-- 索引在列类型变更后自动重建（PostgreSQL 会重写索引），无需手工 DROP/CREATE。

ALTER TABLE channel_route_rules   ALTER COLUMN tenant_id DROP DEFAULT;
ALTER TABLE channel_event_audits  ALTER COLUMN tenant_id DROP DEFAULT;
ALTER TABLE channel_interactions  ALTER COLUMN tenant_id DROP DEFAULT;

ALTER TABLE channel_route_rules
    ALTER COLUMN tenant_id TYPE BIGINT
    USING (CASE WHEN tenant_id::text ~ '^[0-9]+$' THEN tenant_id::text::bigint ELSE 1 END);
ALTER TABLE channel_event_audits
    ALTER COLUMN tenant_id TYPE BIGINT
    USING (CASE WHEN tenant_id::text ~ '^[0-9]+$' THEN tenant_id::text::bigint ELSE 1 END);
ALTER TABLE channel_interactions
    ALTER COLUMN tenant_id TYPE BIGINT
    USING (CASE WHEN tenant_id::text ~ '^[0-9]+$' THEN tenant_id::text::bigint ELSE 1 END);

ALTER TABLE channel_route_rules   ALTER COLUMN tenant_id SET DEFAULT 1, ALTER COLUMN tenant_id SET NOT NULL;
ALTER TABLE channel_event_audits  ALTER COLUMN tenant_id SET DEFAULT 1, ALTER COLUMN tenant_id SET NOT NULL;
ALTER TABLE channel_interactions  ALTER COLUMN tenant_id SET DEFAULT 1, ALTER COLUMN tenant_id SET NOT NULL;
